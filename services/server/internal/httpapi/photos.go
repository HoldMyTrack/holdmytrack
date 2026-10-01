package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // image.DecodeConfig for an uploaded JPEG
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	_ "golang.org/x/image/webp" // image.DecodeConfig for an uploaded WebP

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// Activity photos (IMPLEMENTATION.md §4.27, ADR-0024) — the user's own pictures on an
// activity, each placed on its route. Stored in activity_photos (§3.22), the images under
// photos/{userID}/ in object storage. The browser resizes a photo and strips its EXIF before
// upload, sending the capture time and position it read as fields; the server keeps a moment on
// the track (route_at), never a position, and works the position out on every read. Every photo
// has one: a photo the server can't place is refused until the user says where it goes.
//
// Another account's photo or activity answers exactly like a missing one, `404`.

const (
	// The resized copy is at most 2048 px on its long side (the web's photoPrep.ts) and a
	// thumbnail 320 px. The limits leave room for another client's slightly different choice,
	// and refuse an original sent as it is: HoldMyTrack is not a photo backup.
	maxPhotoBytes      = 3 << 20 // 3 MiB
	maxPhotoThumbBytes = 256 << 10
	maxPhotoSide       = 2560
	maxPhotoThumbSide  = 640
	// maxPhotosPerAccount bounds the one storage line that grows with how many pictures someone
	// takes rather than how far they go (VISION.md §4.3) — not to monetise, but so one account
	// can't become a material share of the bill (§5.7).
	maxPhotosPerAccount = 2000
	maxPhotoCaptionLen  = 500
	// photoTimeSlack is how far before a track starts or after it ends a capture time still
	// places a photo, at that end — the picture at the trailhead before pressing Start.
	photoTimeSlack = 5 * time.Minute
	// photoSnapMaxM is how far from the track a photo's EXIF position may be and still place
	// it, when its capture time can't. Further off, it was taken somewhere else.
	photoSnapMaxM = 500
)

var allowedPhotoContentType = map[string]string{
	"image/jpeg": "jpeg",
	"image/webp": "webp",
}

var (
	errPhotoNotFound    = errors.New("photo not found")
	errPhotoNoTrack     = errors.New("activity has no track")
	errPhotoInvalidJSON = errors.New("invalid request body")
)

// photoNeedsPlaceCode is the error code of an upload the server couldn't place (photoNeedsPlace):
// the client asks the user where on the track it was taken and sends it again with route_at.
const photoNeedsPlaceCode = "photo_needs_place"

func photoKey(userID, photoID string) string      { return "photos/" + userID + "/" + photoID }
func photoThumbKey(userID, photoID string) string { return photoKey(userID, photoID) + "-thumb" }

// photoJSON is one photo as every photo endpoint returns it. Lon/Lat are null only for an
// activity with no track left at all.
type photoJSON struct {
	ID         string     `json:"id"`
	ActivityID string     `json:"activity_id"`
	TakenAt    *time.Time `json:"taken_at"`
	RouteAt    time.Time  `json:"route_at"`
	Lon        *float64   `json:"lon"`
	Lat        *float64   `json:"lat"`
	Caption    *string    `json:"caption"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	URL        string     `json:"url"`
	ThumbURL   string     `json:"thumb_url"`
}

type photosResponse struct {
	Photos []photoJSON `json:"photos"`
}

// photoSelect reads photos with their place on the map, in route order. The position is
// route_at's point on the trajectory — its M axis is epoch seconds (§3.3) — with route_at
// clamped to the track's own span, so a moment Edit track or a Private location has since cut
// away sits at the visible track's nearest end rather than nowhere: a photo is always on the
// track as it's drawn. That is also why no Private location check is needed here — the cut
// ends are gone from the trajectory, and a track that merely passes through one is drawn whole
// (VISION.md §7), so a photo on it shows nothing the track doesn't. %s is the rest of the WHERE
// clause; $1 is always the owner.
const photoSelect = `
SELECT p.id, p.activity_id, p.taken_at, p.route_at, p.caption, p.width, p.height,
       ST_X(pos.pt), ST_Y(pos.pt)
FROM activity_photos p
JOIN activities a ON a.id = p.activity_id
LEFT JOIN LATERAL (
	SELECT ST_GeometryN(ST_LocateAlong(a.trajectory,
	       LEAST(GREATEST(extract(epoch FROM p.route_at)::float8, ST_M(ST_StartPoint(a.trajectory))),
	             ST_M(ST_EndPoint(a.trajectory)))), 1) AS pt
	WHERE a.trajectory IS NOT NULL
) pos ON true
WHERE p.user_id = $1 AND %s
ORDER BY p.route_at, p.created_at`

func (s *Server) loadPhotos(ctx context.Context, where string, args ...any) ([]photoJSON, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(photoSelect, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	photos := []photoJSON{}
	for rows.Next() {
		var p photoJSON
		if err := rows.Scan(&p.ID, &p.ActivityID, &p.TakenAt, &p.RouteAt, &p.Caption, &p.Width, &p.Height, &p.Lon, &p.Lat); err != nil {
			return nil, err
		}
		p.URL = apiPrefix + "/photos/" + p.ID
		p.ThumbURL = p.URL + "/thumb"
		photos = append(photos, p)
	}
	return photos, rows.Err()
}

func (s *Server) loadPhoto(ctx context.Context, userID, photoID string) (photoJSON, error) {
	photos, err := s.loadPhotos(ctx, "p.id = $2", userID, photoID)
	if err != nil {
		return photoJSON{}, err
	}
	if len(photos) == 0 {
		return photoJSON{}, errPhotoNotFound
	}
	return photos[0], nil
}

// handleListPhotos serves `GET /v1/photos?activity={id}` — one activity's photos — and
// `?story={id}`, the photos of every live activity in a Story (§4.23), for its map.
func (s *Server) handleListPhotos(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()
	var where, id string
	var owns func(context.Context, string, string) (bool, error)
	switch {
	case q.Get("activity") != "":
		id, owns = q.Get("activity"), s.ownsActivity
		where = "p.activity_id = $2"
	case q.Get("story") != "":
		id, owns = q.Get("story"), s.ownsStory
		where = `a.superseded_by IS NULL AND p.activity_id IN (SELECT activity_id FROM story_activities WHERE story_id = $2)`
	default:
		http.Error(w, `missing "activity" or "story"`, http.StatusBadRequest)
		return
	}
	if ok, err := owns(ctx, userID, id); err != nil {
		s.log.Error("photo list: owner lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	photos, err := s.loadPhotos(ctx, where, userID, id)
	if err != nil {
		s.log.Error("photo list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, photosResponse{Photos: photos})
}

func (s *Server) ownsActivity(ctx context.Context, userID, activityID string) (bool, error) {
	if !uuidPattern.MatchString(activityID) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM activities WHERE id = $1 AND user_id = $2)`, activityID, userID,
	).Scan(&ok)
	return ok, err
}

func (s *Server) ownsStory(ctx context.Context, userID, storyID string) (bool, error) {
	if !uuidPattern.MatchString(storyID) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM stories WHERE id = $1 AND user_id = $2)`, storyID, userID,
	).Scan(&ok)
	return ok, err
}

// photoPart is one uploaded image, checked.
type photoPart struct {
	data          []byte
	contentType   string
	width, height int
}

// readPhotoPart reads multipart field name and checks it the way setAvatar checks an avatar —
// the content type sniffed, never the client's claim, since it's what the image is served back
// as — plus that it decodes as that type and is no larger than maxSide on its long side.
func readPhotoPart(r *http.Request, name string, maxBytes int64, maxSide int) (photoPart, error) {
	file, _, err := r.FormFile(name)
	if err != nil {
		return photoPart{}, accountFailure(http.StatusBadRequest, "error.photo_missing")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return photoPart{}, accountFailure(http.StatusBadRequest, "error.photo_missing")
	}
	if int64(len(data)) > maxBytes {
		return photoPart{}, accountFailure(http.StatusRequestEntityTooLarge, "error.photo_too_large")
	}
	contentType := http.DetectContentType(data)
	format, ok := allowedPhotoContentType[contentType]
	if !ok {
		return photoPart{}, accountFailure(http.StatusUnsupportedMediaType, "error.photo_type", "type", strconv.Quote(contentType))
	}
	cfg, decoded, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || decoded != format {
		return photoPart{}, accountFailure(http.StatusUnsupportedMediaType, "error.photo_type", "type", strconv.Quote(contentType))
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide {
		return photoPart{}, accountFailure(http.StatusUnprocessableEntity, "error.photo_dimensions", "max", maxSide)
	}
	return photoPart{data: data, contentType: contentType, width: cfg.Width, height: cfg.Height}, nil
}

// handleUploadPhoto serves `POST /v1/photos` — multipart: `activity_id`; `file`, the resized
// copy, and `thumb`, its thumbnail; and what the browser read from the original's EXIF before
// resizing, all optional: `taken_at` (RFC 3339, when the file says its time zone or carries a
// GPS time) or `taken_local` (`2006-01-02T15:04:05`, a camera clock with no zone), and
// `lat`/`lon`; and `route_at` (RFC 3339), where on the track the user put it. Answers 201 with
// the photo, placed by placePhoto. One it can't place is refused with 422 and the error code
// photo_needs_place, and the client sends it again with the user's `route_at`.
func (s *Server) handleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes+maxPhotoThumbBytes+1<<20) // +1MiB of multipart overhead
	if err := r.ParseMultipartForm(maxPhotoBytes + maxPhotoThumbBytes); err != nil {
		s.writeAccountError(w, r, "photo upload", accountFailure(http.StatusRequestEntityTooLarge, "error.photo_too_large"))
		return
	}
	activityID := r.FormValue("activity_id")
	if ok, err := s.ownsActivity(ctx, userID, activityID); err != nil {
		s.log.Error("photo upload: activity lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "activity not found", http.StatusNotFound)
		return
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM activity_photos WHERE user_id = $1`, userID).Scan(&count); err != nil {
		s.log.Error("photo upload: count failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if count >= maxPhotosPerAccount {
		httpErrorT(w, r, http.StatusConflict, "error.photo_limit", "max", maxPhotosPerAccount)
		return
	}

	photo, err := readPhotoPart(r, "file", maxPhotoBytes, maxPhotoSide)
	if err != nil {
		s.writeAccountError(w, r, "photo upload", err)
		return
	}
	thumb, err := readPhotoPart(r, "thumb", maxPhotoThumbBytes, maxPhotoThumbSide)
	if err != nil {
		s.writeAccountError(w, r, "photo upload", err)
		return
	}

	clock, err := parsePhotoClock(r.FormValue("taken_at"), r.FormValue("taken_local"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	at, err := parsePhotoPosition(r.FormValue("lon"), r.FormValue("lat"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var chosen *time.Time
	if v := r.FormValue("route_at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `invalid "route_at", want RFC 3339`, http.StatusBadRequest)
			return
		}
		chosen = &t
	}
	takenAt, routeAt, err := s.placePhoto(ctx, activityID, clock, locationFromContext(ctx), at, chosen)
	if errors.Is(err, errPhotoNoTrack) {
		httpErrorT(w, r, http.StatusConflict, "error.photo_no_track")
		return
	}
	if err != nil {
		s.log.Error("photo upload: placement failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if routeAt == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":   photoNeedsPlaceCode,
			"message": i18n.Get(requestLang(r)).T("error.photo_needs_place"),
		})
		return
	}

	// The row first, for its id, and committed only once both images are stored — a failed Put
	// leaves no row pointing at an image that isn't there.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.log.Error("photo upload: begin failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)
	var photoID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO activity_photos (user_id, activity_id, taken_at, route_at, content_type, thumb_content_type, width, height, bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id
	`, userID, activityID, takenAt, routeAt, photo.contentType, thumb.contentType, photo.width, photo.height,
		len(photo.data)+len(thumb.data)).Scan(&photoID); err != nil {
		s.log.Error("photo upload: insert failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.store.Put(ctx, photoKey(userID, photoID), bytes.NewReader(photo.data), int64(len(photo.data))); err != nil {
		s.log.Error("photo upload: store failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.store.Put(ctx, photoThumbKey(userID, photoID), bytes.NewReader(thumb.data), int64(len(thumb.data))); err != nil {
		s.log.Error("photo upload: thumbnail store failed", "err", err)
		s.removePhotoObjects(ctx, userID, photoID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.log.Error("photo upload: commit failed", "err", err)
		s.removePhotoObjects(ctx, userID, photoID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	p, err := s.loadPhoto(ctx, userID, photoID)
	if err != nil {
		s.log.Error("photo upload: read back failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// photoClock is what the browser read of a photo's capture time: an instant, when the file
// said its zone (or carried a GPS time, which is UTC), or a wall-clock reading with no zone.
type photoClock struct {
	at    *time.Time
	local *time.Time // the wall clock's fields, in time.UTC only as a carrier
}

func parsePhotoClock(takenAt, takenLocal string) (photoClock, error) {
	var c photoClock
	if takenAt != "" {
		t, err := time.Parse(time.RFC3339, takenAt)
		if err != nil {
			return c, errors.New(`invalid "taken_at", want RFC 3339`)
		}
		c.at = &t
	} else if takenLocal != "" {
		t, err := time.Parse("2006-01-02T15:04:05", takenLocal)
		if err != nil {
			return c, errors.New(`invalid "taken_local", want 2006-01-02T15:04:05`)
		}
		c.local = &t
	}
	return c, nil
}

// parsePhotoPosition reads an optional lon/lat pair; one without the other is no position.
func parsePhotoPosition(lonStr, latStr string) (*[2]float64, error) {
	if lonStr == "" || latStr == "" {
		return nil, nil
	}
	lon, err1 := strconv.ParseFloat(lonStr, 64)
	lat, err2 := strconv.ParseFloat(latStr, 64)
	if err1 != nil || err2 != nil || math.Abs(lon) > 180 || math.Abs(lat) > 90 {
		return nil, errors.New(`invalid "lon"/"lat"`)
	}
	return &[2]float64{lon, lat}, nil
}

// trackSpan is an activity's track's first and last moment, epoch seconds; ok is false when
// it has no track.
func (s *Server) trackSpan(ctx context.Context, activityID string) (start, end float64, ok bool, err error) {
	var m0, m1 *float64
	err = s.pool.QueryRow(ctx, `
		SELECT ST_M(ST_StartPoint(trajectory)), ST_M(ST_EndPoint(trajectory)) FROM activities WHERE id = $1
	`, activityID).Scan(&m0, &m1)
	if err != nil || m0 == nil || m1 == nil {
		return 0, 0, false, err
	}
	return *m0, *m1, true, nil
}

// placePhoto works out a new photo's capture time and its moment on the track (ADR-0024). The
// user's own choice (chosen, clamped to the track) wins. Otherwise the capture time places it
// when it falls within the track, photoTimeSlack either side clamped to that end; failing that,
// its EXIF position places it at the nearest point of the track, if that's within
// photoSnapMaxM. With none of these, routeAt is nil: the user has to say. errPhotoNoTrack for an
// activity with no track, which can't hold a photo.
//
// A wall-clock time with no zone is read in the account's own zone first; if that misses the
// track, in whichever UTC offset puts it on the track, the nearest to the account's own.
func (s *Server) placePhoto(ctx context.Context, activityID string, clock photoClock, home *time.Location, at *[2]float64, chosen *time.Time) (takenAt, routeAt *time.Time, err error) {
	start, end, hasTrack, err := s.trackSpan(ctx, activityID)
	if err != nil {
		return nil, nil, err
	}
	if !hasTrack {
		return nil, nil, errPhotoNoTrack
	}
	onTrack := func(t time.Time) bool {
		sec := float64(t.Unix())
		return sec >= start-photoTimeSlack.Seconds() && sec <= end+photoTimeSlack.Seconds()
	}

	takenAt = clock.at
	if clock.local != nil {
		takenAt = resolveWallClock(*clock.local, home, onTrack)
	}
	if chosen != nil {
		t := clampToSpan(*chosen, start, end)
		return takenAt, &t, nil
	}
	if takenAt != nil && onTrack(*takenAt) {
		t := clampToSpan(*takenAt, start, end)
		return takenAt, &t, nil
	}
	if at != nil {
		routeAt, err = s.snapToTrack(ctx, activityID, at[0], at[1], photoSnapMaxM)
		return takenAt, routeAt, err
	}
	return takenAt, nil, nil
}

// clampToSpan puts t within the track's first and last moment (epoch seconds), to the second.
func clampToSpan(t time.Time, start, end float64) time.Time {
	sec := math.Min(math.Max(float64(t.Unix()), start), end)
	return time.Unix(int64(math.Round(sec)), 0).UTC()
}

// resolveWallClock turns a zoneless camera clock into an instant: in home if that lands on
// the track (or nothing does), else in the UTC offset nearest home's that does. Offsets step by
// 15 minutes from −12:00 to +14:00, which covers every zone in use.
func resolveWallClock(local time.Time, home *time.Location, onTrack func(time.Time) bool) *time.Time {
	inHome := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, home)
	if onTrack(inHome) {
		return &inHome
	}
	_, homeOffset := inHome.Zone()
	var best *time.Time
	bestDiff := math.MaxInt
	for offset := -12 * 3600; offset <= 14*3600; offset += 15 * 60 {
		t := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.FixedZone("", offset))
		diff := offset - homeOffset
		if diff < 0 {
			diff = -diff
		}
		if onTrack(t) && diff < bestDiff {
			best, bestDiff = &t, diff
		}
	}
	if best != nil {
		return best
	}
	return &inHome
}

// snapToTrack is the moment of the track's point nearest lon/lat, or nil when that point is
// further than maxM away. ST_InterpolatePoint reads it from the M axis at the nearest point.
func (s *Server) snapToTrack(ctx context.Context, activityID string, lon, lat float64, maxM float64) (*time.Time, error) {
	var m, dist *float64
	err := s.pool.QueryRow(ctx, `
		SELECT ST_InterpolatePoint(trajectory, pt),
		       ST_Distance(ST_Force2D(trajectory)::geography, pt::geography)
		FROM activities, ST_SetSRID(ST_MakePoint($2::float8, $3::float8), 4326) AS pt
		WHERE id = $1 AND trajectory IS NOT NULL
	`, activityID, lon, lat).Scan(&m, &dist)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errPhotoNoTrack
	}
	if err != nil || m == nil {
		return nil, err
	}
	if dist != nil && *dist > maxM {
		return nil, nil
	}
	t := time.Unix(int64(math.Round(*m)), 0).UTC()
	return &t, nil
}

// handleGetPhoto serves `GET /v1/photos/{id}` and handleGetPhotoThumb `…/thumb` — the stored
// image, to its owner only. A photo's images never change under its id, so they cache for good.
func (s *Server) handleGetPhoto(w http.ResponseWriter, r *http.Request) {
	s.servePhotoImage(w, r, false)
}

func (s *Server) handleGetPhotoThumb(w http.ResponseWriter, r *http.Request) {
	s.servePhotoImage(w, r, true)
}

func (s *Server) servePhotoImage(w http.ResponseWriter, r *http.Request, thumb bool) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()
	photoID := r.PathValue("id")
	if !uuidPattern.MatchString(photoID) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	var contentType, thumbContentType string
	err := s.pool.QueryRow(ctx,
		`SELECT content_type, thumb_content_type FROM activity_photos WHERE id = $1 AND user_id = $2`, photoID, userID,
	).Scan(&contentType, &thumbContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("photo lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	key := photoKey(userID, photoID)
	if thumb {
		key, contentType = photoThumbKey(userID, photoID), thumbContentType
	}
	obj, err := s.store.Get(ctx, key)
	if err != nil {
		s.log.Error("photo read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := io.Copy(w, obj); err != nil {
		s.log.Error("photo stream failed", "err", err)
	}
}

// handleUpdatePhoto serves `PATCH /v1/photos/{id}`. Each field is optional and only a field
// present changes: `caption` (empty clears it), and `route_at` — RFC 3339, the photo's new
// moment on the track (the Edit window's slider), clamped to the track. A photo can't be taken
// off the track: null is refused.
func (s *Server) handleUpdatePhoto(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()
	photoID := r.PathValue("id")
	if !uuidPattern.MatchString(photoID) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	var req map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, errPhotoInvalidJSON.Error(), http.StatusBadRequest)
		return
	}

	var activityID string
	err := s.pool.QueryRow(ctx,
		`SELECT activity_id FROM activity_photos WHERE id = $1 AND user_id = $2`, photoID, userID,
	).Scan(&activityID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("photo update: lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if raw, ok := req["caption"]; ok {
		var caption *string
		if err := json.Unmarshal(raw, &caption); err != nil {
			http.Error(w, errPhotoInvalidJSON.Error(), http.StatusBadRequest)
			return
		}
		var value *string
		if caption != nil {
			if trimmed := strings.TrimSpace(*caption); trimmed != "" {
				value = &trimmed
			}
		}
		if value != nil && utf8.RuneCountInString(*value) > maxPhotoCaptionLen {
			httpErrorT(w, r, http.StatusBadRequest, "error.photo_caption_too_long", "max", maxPhotoCaptionLen)
			return
		}
		if _, err := s.pool.Exec(ctx, `UPDATE activity_photos SET caption = $2 WHERE id = $1`, photoID, value); err != nil {
			s.log.Error("photo update: caption failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	if raw, ok := req["route_at"]; ok {
		var at *time.Time
		if err := json.Unmarshal(raw, &at); err != nil || at == nil {
			http.Error(w, `invalid "route_at", want an RFC 3339 time`, http.StatusBadRequest)
			return
		}
		start, end, hasTrack, err := s.trackSpan(ctx, activityID)
		if err != nil {
			s.log.Error("photo update: track lookup failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !hasTrack {
			httpErrorT(w, r, http.StatusConflict, "error.photo_no_track")
			return
		}
		if _, err := s.pool.Exec(ctx, `UPDATE activity_photos SET route_at = $2 WHERE id = $1`, photoID, clampToSpan(*at, start, end)); err != nil {
			s.log.Error("photo update: route_at failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	p, err := s.loadPhoto(ctx, userID, photoID)
	if err != nil {
		s.log.Error("photo update: read back failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePhoto serves `DELETE /v1/photos/{id}`: the row, then both images, best-effort —
// an orphaned blob is a cleanup nuisance, never a reason to keep a photo the user removed.
func (s *Server) handleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()
	photoID := r.PathValue("id")
	if !uuidPattern.MatchString(photoID) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM activity_photos WHERE id = $1 AND user_id = $2`, photoID, userID)
	if err != nil {
		s.log.Error("photo delete failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}
	s.removePhotoObjects(ctx, userID, photoID)
	w.WriteHeader(http.StatusNoContent)
}

// activityPhotoIDs lists an activity's photos, for removing their images before the activity's
// own delete cascades their rows away.
func (s *Server) activityPhotoIDs(ctx context.Context, activityID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM activity_photos WHERE activity_id = $1`, activityID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// removePhotoObjects removes a photo's two images, logging rather than failing.
func (s *Server) removePhotoObjects(ctx context.Context, userID, photoID string) {
	for _, key := range []string{photoKey(userID, photoID), photoThumbKey(userID, photoID)} {
		if err := s.store.Remove(ctx, key); err != nil {
			s.log.Error("photo object removal failed", "key", key, "err", err)
		}
	}
}
