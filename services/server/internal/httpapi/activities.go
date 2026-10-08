package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
)

// activityFilter is the from/to/types/story filter shape shared by §4.3's tracks tiles and
// all three of §4.7's listing endpoints. Every field is optional and a nil field means "no
// restriction" — the convention both sections specify, so a caller that wants everything
// sends nothing rather than an explicit wide-open range.
type activityFilter struct {
	// From and To are local dates (YYYY-MM-DD), both inclusive, matched against each
	// activity's own local date (localStartedAt).
	From  *string
	To    *string
	Types []string
	// Story narrows to one Story's activities (§4.23). No ownership check is needed: the
	// queries are already scoped to the caller's own activities, and a Story only ever holds
	// its owner's, so another account's Story matches nothing — the same as an empty one.
	Story *string
}

// parseActivityFilter reads that filter off a query string. A `from`/`to` date is a local
// date, matched against each activity's own (IMPLEMENTATION.md §4.30): `?from=2026-03-10`
// means "the 10th where each activity happened," so a run at 07:00 in Tokyo is on the 10th
// whatever the account's or the browser's zone. The returned error is already phrased for the
// client; callers pass it straight to http.Error with a 400.
func parseActivityFilter(q url.Values) (activityFilter, error) {
	var f activityFilter
	if v := q.Get("from"); v != "" {
		if _, err := time.Parse(dateLayout, v); err != nil {
			return activityFilter{}, errors.New(`invalid "from" date, want YYYY-MM-DD`)
		}
		f.From = &v
	}
	if v := q.Get("to"); v != "" {
		if _, err := time.Parse(dateLayout, v); err != nil {
			return activityFilter{}, errors.New(`invalid "to" date, want YYYY-MM-DD`)
		}
		f.To = &v
	}
	// nil, not an empty non-nil slice, when the param is absent — pgx sends a nil []string
	// as SQL NULL, which is what the "$n::text[] IS NULL OR ..." clauses check for.
	if v := q.Get("types"); v != "" {
		f.Types = strings.Split(v, ",")
	}
	story, err := parseStoryParam(q)
	if err != nil {
		return activityFilter{}, err
	}
	f.Story = story
	return f, nil
}

// activityBBoxColumns is an activity's bbox as four columns, west, south, east, north, the
// shorter way round in longitude. A track across the antimeridian is stored continuing past
// ±180 (§4.1), so its plain bbox already is (179.9…180.1, east past 180, the form MapLibre
// fits as it is). One stored jumping back across the world, before that and with no raw
// payload to reprocess it from, spans −179.9…179.9; it's read off ST_ShiftLongitude's copy
// instead, which moves the western hemisphere's points past 180. The shifted copy is only
// made for a box over 180° wide, which no other track has.
var activityBBoxColumns = shorterWay("ST_XMin") + `, ST_YMin(trajectory), ` + shorterWay("ST_XMax") + `, ST_YMax(trajectory)`

func shorterWay(edge string) string {
	plainWidth := `ST_XMax(trajectory) - ST_XMin(trajectory)`
	shiftedWidth := `ST_XMax(ST_ShiftLongitude(trajectory)) - ST_XMin(ST_ShiftLongitude(trajectory))`
	return `CASE WHEN ` + plainWidth + ` > 180 THEN
           CASE WHEN ` + shiftedWidth + ` < ` + plainWidth + ` THEN ` + edge + `(ST_ShiftLongitude(trajectory)) ELSE ` + edge + `(trajectory) END
         ELSE ` + edge + `(trajectory) END`
}

// localStartedAt is an activity's start as the wall clock read where it was recorded: every
// day, week or month an activity is grouped or filtered by is this one's (§4.30). The zone is
// a name, so the tz database gives the offset in force on that date.
const localStartedAt = `(started_at AT TIME ZONE timezone)`

// inDateRange is the optional from/to clause on localStartedAt, both ends inclusive local
// dates (from and to name ::date parameters, NULL for no limit).
func inDateRange(from, to string) string {
	return `(` + from + `::date IS NULL OR ` + localStartedAt + ` >= ` + from + `::date)
  AND (` + to + `::date IS NULL OR ` + localStartedAt + ` < ` + to + `::date + 1)`
}

// sqlDate is t's calendar date in its own location, as a ::date parameter takes it.
func sqlDate(t time.Time) string { return t.Format(dateLayout) }

// parseStoryParam reads the optional `story` Story id, nil when absent.
func parseStoryParam(q url.Values) (*string, error) {
	v := q.Get("story")
	if v == "" {
		return nil, nil
	}
	if !uuidPattern.MatchString(v) {
		return nil, errors.New(`invalid "story", want a Story id`)
	}
	return &v, nil
}

// activityStoriesColumn is the Stories an activity is in (§4.23), newest first, as a JSON array
// of {id, name} — one indexed lookup per row (idx_story_activities_activity). A Story only ever
// holds its owner's activities, so it needs no user filter of its own.
const activityStoriesColumn = `COALESCE((
         SELECT json_agg(json_build_object('id', s.id, 'name', s.name) ORDER BY s.created_at DESC, s.id DESC)
         FROM story_activities sa JOIN stories s ON s.id = sa.story_id
         WHERE sa.activity_id = activities.id), '[]'::json)`

// listActivitiesQuery is §4.7's list, with §4.3's "$n IS NULL OR ..." optional-filter
// treatment. No pagination: the client that used to page through this (ActivitiesPanel)
// renders every matching row on the map and computes stats and type/distance facets across
// all of them at once, so a partial page was never actually usable — a caller wanting page
// 2 always had to have fetched page 1 first anyway. Phase 0/1 has one seeded user and
// activity counts in the hundreds to low thousands at most; revisit if that stops holding.
//
// The four ST_*Min/Max calls are the row's bounding box, so a client can fly the map to an
// activity without a follow-up request per row. They are cheaper than they look: PostGIS
// caches a geometry's bounding box in its header, so this reads that rather than walking
// the vertices. §3.3 deliberately has no bbox column and doesn't need one back — the note
// there about the removed BOX2D column applies just as well to adding a new one.
//
// Column order here has to match scanActivityRow's Scan call exactly — activityByIDQuery
// below shares that same order for the same reason.
var listActivitiesQuery = `
SELECT id, started_at, timezone, activity_type, name, distance_meters, duration_seconds, description,
       ` + activityBBoxColumns + `,
       edit_pending, track_edit IS NOT NULL, trajectory IS NULL, split_group, split_from, split_to,
       ` + activityStoriesColumn + `
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ` + inDateRange("$2", "$3") + `
  AND ($4::text[] IS NULL OR activity_type = ANY($4))
  AND ($5::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $5))
ORDER BY started_at DESC, id DESC`

// activityRow is one row of the list. The field set is exactly what the Activities panel
// renders, no more. Duration is `duration_seconds`, not `moving_seconds`. ingest.Process
// populates moving_seconds now (added for Trends, docs/SPEC.md FR-9), but only for activities
// ingested since — anything older still has it NULL, and serving a field that's populated
// for some rows and not others in the one list every activity shares would read as broken
// data rather than as what it is. duration_seconds has no such gap.
//
// Name (migrations/0002_activities.sql) is a user-entered title, nullable — a row with
// none has never had one set, and the client falls back to started_at for its primary line
// (§4.7 revised its earlier "no name column" decision to add exactly this, and nothing
// more: still no parser reads a name out of a source file). Description (§4.7.4,
// migrations/0002_activities.sql's activities.description) stays separate free-text, shown only
// as a hover tooltip — a row with nothing written there has never been edited, not "an
// empty description."
//
// The three metric fields are nullable in §3.3 and stay nullable here rather than being
// coerced to 0: a file that carried no distance is not a zero-distance activity.
type activityRow struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	// Timezone is the IANA zone it was recorded in (§4.30): a client shows StartedAt, and any
	// other time of this activity's, in it.
	Timezone        string   `json:"timezone"`
	ActivityType    string   `json:"activity_type"`
	Name            *string  `json:"name"`
	DistanceMeters  *float64 `json:"distance_meters"`
	DurationSeconds *int32   `json:"duration_seconds"`
	Description     *string  `json:"description"`
	// [minLon, minLat, maxLon, maxLat] — GeoJSON's bbox ordering — or null when the row has
	// no geometry. §3.3 allows a null trajectory, and a client must not fly the map nowhere.
	BBox []float64 `json:"bbox"`
	// Pending is true while an Edit track reprocess (§4.7.7) is queued or running — the row's
	// numbers and geometry are still the pre-edit ones. Edited is true once the track carries
	// a user edit, which is what makes "Reset to original track" meaningful.
	Pending bool `json:"pending"`
	Edited  bool `json:"edited"`
	// Private is an activity entirely inside the account's Private locations (FR-8.1): kept,
	// with no track, so no distance or duration worth showing — the panel badges it instead.
	// A NULL trajectory has no other cause (§3.3).
	Private bool `json:"private"`
	// Split is set on a piece of a split activity (§4.7.8): which split, and the stretch of the
	// recording it covers — what the Activities panel's Merge checks a group against.
	Split *splitRef `json:"split"`
	// Stories are the Stories it's in, newest first — the Activities panel's Story badge.
	Stories []storyRef `json:"stories"`
}

// splitRef places a piece within its split: From and To are unix-ms point timestamps, null
// at the recording's own ends. Pieces meet where one's To is the next one's From.
type splitRef struct {
	Group string `json:"group"`
	From  *int64 `json:"from"`
	To    *int64 `json:"to"`
}

// storyRef names a Story an activity is in.
type storyRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// rowScanner is the common surface of pgx.Row (QueryRow) and pgx.Rows (Query's iteration) —
// scanActivityRow works against either, so the single-row lookup in handleUpdateActivity and
// the list loop in handleListActivities share one place that knows the column order and the
// bbox-from-four-nullable-floats shape, rather than duplicating it.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanActivityRow(row rowScanner) (activityRow, error) {
	var a activityRow
	// Four nullable floats rather than one bbox type: they are null together, since a row
	// either has a trajectory or doesn't, but pgx has no reason to know that.
	var minLon, minLat, maxLon, maxLat *float64
	var splitGroup *string
	var split splitRef
	if err := row.Scan(&a.ID, &a.StartedAt, &a.Timezone, &a.ActivityType, &a.Name, &a.DistanceMeters, &a.DurationSeconds, &a.Description,
		&minLon, &minLat, &maxLon, &maxLat, &a.Pending, &a.Edited, &a.Private, &splitGroup, &split.From, &split.To, &a.Stories); err != nil {
		return activityRow{}, err
	}
	if splitGroup != nil {
		split.Group = *splitGroup
		a.Split = &split
	}
	if minLon != nil && minLat != nil && maxLon != nil && maxLat != nil {
		a.BBox = []float64{*minLon, *minLat, *maxLon, *maxLat}
	}
	return a, nil
}

type activitiesResponse struct {
	Activities []activityRow `json:"activities"`
}

// handleListActivities serves IMPLEMENTATION.md §4.7's
// `GET /v1/activities?from=&to=&types=`: the user's own history as rows, newest first,
// unpaginated — see listActivitiesQuery's doc comment for why. Reads the authenticated user
// from userIDFromContext (requireAuth, server.go) rather than a query parameter, same as
// every other data handler.
func (s *Server) handleListActivities(w http.ResponseWriter, r *http.Request) {
	filter, err := parseActivityFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := s.pool.Query(r.Context(), listActivitiesQuery, userIDFromContext(r.Context()), filter.From, filter.To, filter.Types, filter.Story)
	if err != nil {
		s.log.Error("activity list query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Non-nil zero-length, so an empty history serialises as [] rather than null.
	list := make([]activityRow, 0)
	for rows.Next() {
		a, err := scanActivityRow(rows)
		if err != nil {
			s.log.Error("activity list scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("activity list read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, activitiesResponse{Activities: list})
}

// maxActivityTypeLen matches activities.activity_type's own VARCHAR(50) — rejecting an
// over-length value here is a clearer 400 than letting Postgres reject the UPDATE.
const maxActivityTypeLen = 50

// maxActivityDescriptionLen is a generous free-text bound (about the length of a couple of
// paragraphs), not a claim that anyone needs that much — the same "reject an obvious mistake,
// not opine on reasonable length" reasoning.
const maxActivityDescriptionLen = 2000

// maxActivityNameLen matches migrations/0002_activities.sql's VARCHAR(200) — a single-line
// title bound, deliberately shorter than maxActivityDescriptionLen: anything longer belongs
// in the description field, not the row's primary line.
const maxActivityNameLen = 200

// activityByIDQuery is the single-row counterpart to listActivitiesQuery, same column order
// (scanActivityRow's Scan call is shared between both), scoped by id and owner exactly like
// trackMetricsQuery — a non-owned or nonexistent id is indistinguishable from "not found."
var activityByIDQuery = `
SELECT id, started_at, timezone, activity_type, name, distance_meters, duration_seconds, description,
       ` + activityBBoxColumns + `,
       edit_pending, track_edit IS NOT NULL, trajectory IS NULL, split_group, split_from, split_to,
       ` + activityStoriesColumn + `
FROM activities
WHERE id = $1 AND user_id = $2`

type updateActivityRequest struct {
	ActivityType string `json:"activity_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
}

// activityIDFromPath is the {id} of an /activities/…/{id} route, lowercased, or a 404 for one
// that isn't a UUID. Postgres refuses a malformed uuid with an error, which reached the client
// as a 500; and it accepts an uppercase one, which would then miss object keys built from the
// id (the delete's mask prefix) while still matching the row.
func activityIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		http.Error(w, "activity not found", http.StatusNotFound)
		return "", false
	}
	return strings.ToLower(id), true
}

// handleUpdateActivity serves `PATCH /v1/activities/{id}` (§4.7.4) — the "rename a
// mis-tagged upload" / "describe a non-sport GPS trace" feature. §4.7.2 already resolved
// activity_type as free-form, not a controlled vocabulary, so a manual rename here is
// architecturally identical to what ingest itself already writes — nothing to validate
// against an allow-list, just a length bound matching the column.
//
// Full-replace-on-save, not per-field PATCH semantics, matching account.go's
// handleUpdateSettings: one Save button in the edit dialog commits all three fields at once.
// Empty name/description means "clear it," written as NULLIF the same way every other
// optional text column here already is; activity_type has no such escape hatch since the
// column is NOT NULL — an empty value is rejected outright rather than silently kept
// unchanged.
func (s *Server) handleUpdateActivity(w http.ResponseWriter, r *http.Request) {
	activityID, ok := activityIDFromPath(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())

	var req updateActivityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ActivityType = strings.TrimSpace(req.ActivityType)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.ActivityType == "" {
		httpErrorT(w, r, http.StatusBadRequest, "error.activity_type_required")
		return
	}
	if len(req.ActivityType) > maxActivityTypeLen {
		httpErrorT(w, r, http.StatusBadRequest, "error.activity_type_too_long", "max", maxActivityTypeLen)
		return
	}
	if len(req.Name) > maxActivityNameLen {
		httpErrorT(w, r, http.StatusBadRequest, "error.activity_name_too_long", "max", maxActivityNameLen)
		return
	}
	if len(req.Description) > maxActivityDescriptionLen {
		httpErrorT(w, r, http.StatusBadRequest, "error.activity_description_too_long", "max", maxActivityDescriptionLen)
		return
	}

	ctx := r.Context()
	tag, err := s.pool.Exec(ctx, `
		UPDATE activities SET activity_type = $3, name = NULLIF($4, ''), description = NULLIF($5, '')
		WHERE id = $1 AND user_id = $2
	`, activityID, userID, req.ActivityType, req.Name, req.Description)
	if err != nil {
		s.log.Error("activity update failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "activity not found", http.StatusNotFound)
		return
	}
	// activity_type is in every tracks tile this activity crosses, and no render follows.
	if err := fog.BumpMapVersion(ctx, s.pool, userID); err != nil {
		s.log.Error("activity update: bump map version failed", "activity_id", activityID, "err", err)
	}

	row, err := scanActivityRow(s.pool.QueryRow(ctx, activityByIDQuery, activityID, userID))
	if err != nil {
		s.log.Error("activity lookup after update failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// handleDeleteActivity serves `DELETE /v1/activities/{id}` (§4.7.5) — a full purge, not a
// soft delete: the DB cascade already handles activity_streams/activity_tile_masks (both
// `ON DELETE CASCADE` back to activities.id), and Trends is live-aggregated over whatever
// rows remain, so neither needs any explicit cleanup here. Two things genuinely do:
//
//   - Object storage has no foreign keys (demo_purge.go's own reasoning), so the raw upload
//     and this activity's rendered fog/heatmap masks have to be removed explicitly.
//   - Fog and Heatmap both read a precomputed per-user, per-tile cache that ingest builds by
//     compositing activity_tile_masks rows together, but only ingest ever marks a tile dirty
//     or enqueues its re-render — deleting an activity would otherwise cascade its masks away
//     while leaving that cache's already-rendered PNGs stale indefinitely, still showing
//     coverage for an activity that no longer exists.
//
// Runs synchronously in the handler, like handleUpdateActivity/handleDeleteAvatar: unlike
// ingest's parsing, everything here (a row delete, a couple of small queries, an
// object-storage prefix removal) is cheap enough not to need the job queue itself — only the
// actual tile re-render at the end goes through it, the same render_fog job ingest already
// relies on.
func (s *Server) handleDeleteActivity(w http.ResponseWriter, r *http.Request) {
	activityID, ok := activityIDFromPath(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	// Read back what the cascade is about to delete out from under us — both are needed only
	// for cleanup below, and both would be gone (or unrecoverable) once the DELETE runs.
	var rawKey *string
	if err := s.pool.QueryRow(ctx,
		`SELECT raw_payload_key FROM activities WHERE id = $1 AND user_id = $2`, activityID, userID,
	).Scan(&rawKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}
		s.log.Error("activity lookup before delete failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tiles, err := s.activityFogTiles(ctx, activityID)
	if err != nil {
		s.log.Error("activity tile lookup before delete failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Best-effort storage cleanup first, same order and same "log and continue" treatment
	// demo_purge.go already established — an orphaned blob is a cleanup nuisance, never a
	// reason to leave the DB still pointing at data the user just asked to delete.
	if err := s.store.RemoveByPrefix(ctx, "activity-masks/"+activityID+"/"); err != nil {
		s.log.Error("activity delete: mask cleanup failed, deleting row anyway", "activity_id", activityID, "err", err)
	}
	// The activity's photos (§4.27) go with it; the cascade takes their rows, so their images
	// have to be removed explicitly — after the rows, since a copied Story's photo (§4.23) can
	// share them and keeps them.
	photoKeys, err := s.activityPhotoKeys(ctx, activityID)
	if err != nil {
		s.log.Error("activity delete: photo lookup failed, deleting row anyway", "activity_id", activityID, "err", err)
	}
	if rawKey != nil {
		// raw_payload_key is content-addressed (raw/{userID}/{sha256(bytes)}{ext}), not
		// scoped by source — two distinct activities can share one key if their raw bytes are
		// byte-identical (e.g. the same file re-ingested once via plain upload and once via a
		// Takeout import). Only remove it if this is the last activity actually pointing at
		// it, or a sibling activity's still-referenced raw file disappears with this one.
		var stillReferenced bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM activities WHERE raw_payload_key = $1 AND id != $2)`, *rawKey, activityID,
		).Scan(&stillReferenced); err != nil {
			s.log.Error("activity delete: raw key reference check failed, skipping raw cleanup", "activity_id", activityID, "err", err)
		} else if !stillReferenced {
			if err := s.store.Remove(ctx, *rawKey); err != nil {
				s.log.Error("activity delete: raw payload cleanup failed, deleting row anyway", "activity_id", activityID, "err", err)
			}
		}
	}

	// A representative of the copies this delete is about to release (§4.6). Each one overlapped
	// the deleted winner, so re-ranking around any one of them re-ranks its copies too — and
	// without that they would every one become live at once, leaving the same ride counted two
	// or three times in the totals and the fog.
	var releasedStart *time.Time
	var releasedDuration *int
	if err := s.pool.QueryRow(ctx,
		`SELECT started_at, duration_seconds FROM activities
		 WHERE superseded_by = $1 ORDER BY created_at LIMIT 1`, activityID,
	).Scan(&releasedStart, &releasedDuration); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Error("activity delete: superseded lookup failed", "activity_id", activityID, "err", err)
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM activities WHERE id = $1 AND user_id = $2`, activityID, userID)
	if err != nil {
		s.log.Error("activity delete failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "activity not found", http.StatusNotFound)
		return
	}
	s.removeUnreferencedPhotoFiles(ctx, photoKeys)
	// The track is gone from the tracks tiles now; the render below only bumps once it runs.
	if err := fog.BumpMapVersion(ctx, s.pool, userID); err != nil {
		s.log.Error("activity delete: bump map version failed", "activity_id", activityID, "err", err)
	}

	// The fix for the stale-cache risk above: re-trigger the exact same dirty-mark-and-render
	// machinery ingest uses, just from removal instead of addition. RenderUser's own render is
	// "idempotent and complete per tile, not incremental" — it fully recomposites a tile from
	// whatever activity_tile_masks rows currently exist, so simply re-triggering it against
	// the now-smaller set (this activity's rows already gone via the cascade above) produces a
	// correct result with no new compositing logic needed.
	// Re-rank before re-rendering, so the render below composites the set that actually wins.
	if releasedStart != nil && releasedDuration != nil {
		extra, err := ingest.ResolveDuplicates(ctx, s.pool, userID, *releasedStart, *releasedDuration)
		if err != nil {
			s.log.Error("activity delete: re-resolving duplicates failed", "activity_id", activityID, "err", err)
		} else {
			tiles = append(tiles, extra...)
		}
	}

	if len(tiles) > 0 {
		if err := ingest.MarkFogTilesDirty(ctx, s.pool, userID, tiles); err != nil {
			s.log.Error("activity delete: mark fog tiles dirty failed", "activity_id", activityID, "err", err)
		} else if err := ingest.EnqueueRenderFog(ctx, s.pool, userID); err != nil {
			s.log.Error("activity delete: enqueue render_fog failed", "activity_id", activityID, "err", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// activityFogTiles reads back the z14 tiles a (soon to be deleted) activity's own crisp masks
// cover — activity_tile_masks' primary key leads with activity_id, so this is an index-only
// lookup, not a scan. Has to run before the cascade removes these rows: there is no other way
// to recover which tiles need re-rendering once they're gone.
// Includes the tiles of any activity this one supersedes (§4.6), because deleting the copy
// that won a deduplication collision is exactly when those come back: `superseded_by` is
// `ON DELETE SET NULL`, so the displaced copy becomes live again the moment this row goes, and
// its own coverage has to be composited back in. Its tiles are not necessarily a subset of
// this row's — a Private location clip or a shorter recording can leave each copy touching tiles the
// other never did — so they are collected rather than assumed.
func (s *Server) activityFogTiles(ctx context.Context, activityID string) ([][2]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT m.tile_x, m.tile_y
		 FROM activity_tile_masks m
		 WHERE m.zoom = $2
		   AND (m.activity_id = $1
		        OR m.activity_id IN (SELECT id FROM activities WHERE superseded_by = $1))`,
		activityID, ingest.FogZoom,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tiles [][2]int
	for rows.Next() {
		var x, y int
		if err := rows.Scan(&x, &y); err != nil {
			return nil, err
		}
		tiles = append(tiles, [2]int{x, y})
	}
	return tiles, rows.Err()
}

// activitySummaryQuery is §4.7's range summary: one query, one row, the aggregate behind
// the panel's own "1,284 km · 186 activities · 42 h · 9,120 m up" line. Same filter shape as
// the list above, deliberately — the totals have to describe exactly the set of activities
// the list is showing, which only holds if both read the same filter.
//
// COALESCE to 0 here, unlike the per-row nullability the list preserves: SUM already skips
// NULL inputs, and a total over zero rows is legitimately zero rather than unknown. The
// distinction that matters is between "this activity recorded no distance" (a row value,
// null) and "these activities add up to nothing" (a total, 0).
var activitySummaryQuery = `
SELECT COUNT(*),
       COALESCE(SUM(distance_meters), 0),
       COALESCE(SUM(duration_seconds), 0),
       COALESCE(SUM(elevation_gain_m), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ` + inDateRange("$2", "$3") + `
  AND ($4::text[] IS NULL OR activity_type = ANY($4))
  AND ($5::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $5))`

type activitySummaryResponse struct {
	Count           int64   `json:"count"`
	DistanceMeters  float64 `json:"distance_meters"`
	DurationSeconds int64   `json:"duration_seconds"`
	ElevationGainM  float64 `json:"elevation_gain_m"`
}

// handleActivitySummary serves §4.7's `GET /v1/activities/summary`. No cursor and no rows:
// the whole point is that a client showing totals doesn't have to page through the history
// to add them up itself.
func (s *Server) handleActivitySummary(w http.ResponseWriter, r *http.Request) {
	filter, err := parseActivityFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var resp activitySummaryResponse
	if err := s.pool.QueryRow(r.Context(), activitySummaryQuery,
		userIDFromContext(r.Context()), filter.From, filter.To, filter.Types, filter.Story,
	).Scan(&resp.Count, &resp.DistanceMeters, &resp.DurationSeconds, &resp.ElevationGainM); err != nil {
		s.log.Error("activity summary query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// histogramWindowMonths is the *default* window length for the calendar-window mode, used
// when a caller sends no `days` and no `from`/`to`. Twelve months is what the original
// fixed-window mockup's Jan–Nov axis implied, and is still a reasonable first paint for a
// caller that has nothing else to go on — §4.8's year-grid activity graph is the one that
// still wants whole calendar windows.
const histogramWindowMonths = 12

// maxHistogramDays bounds the activity-day mode's LIMIT. A page is meant to be one strip of
// bars, not a whole history in one request, and an unbounded `days=` is an unbounded LIMIT.
const maxHistogramDays = 366

// activityHistogramQuery is §4.7's per-bucket aggregate over a calendar window.
// Deliberately unfiltered beyond the window, the user and the optional Story ($5): this chart
// is the backdrop a selected sub-range is highlighted against, so narrowing it by the same
// type filter as the list would defeat its purpose. A Story is different — inside one, the
// picker's whole world is the Story's activities (§4.23).
//
// Days with no activity are simply absent from the result rather than returned as zeroes —
// GROUP BY produces no row for them, and a client that has the window's start and end can
// place what it did get. That keeps the response proportional to how much a user actually
// recorded rather than to how long the window is.
//
// The bucket is each activity's own local day (localStartedAt, §4.30), and the window
// [$2, $3) is in local dates too.
const activityHistogramQuery = `
SELECT ` + localStartedAt + `::date AS day,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ` + localStartedAt + ` >= $2::date
  AND ` + localStartedAt + ` < $3::date
  AND ($4::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $4))
GROUP BY day
ORDER BY day`

// activityDayPageQuery is the same aggregate paginated by *activity-day* rather than by
// calendar range: the `days` most recent distinct days-with-activity, optionally restricted
// to those strictly before some day. It exists because the date slider has one slot per
// day-with-activity with the empty days removed entirely (apps/web/src/ui/DateRangeSlider.tsx,
// and the Android app's footer), so "one window of days" is a count of real days, not a width
// of calendar time. Asking for that with a calendar window would mean the client guessing a
// window and widening it until enough days came back — several round trips per page over a
// sparse history, and a guess that is wrong in a different direction for every user. One page
// here is exactly `days` days, every time.
//
// The anchor is a local date like the days themselves, so each activity is compared in its
// own zone; the account's live activities are read through idx_activities_live either way.
const activityDayPageQuery = `
SELECT ` + localStartedAt + `::date AS day,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::date IS NULL OR ` + localStartedAt + ` < $2::date)
  AND ($4::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $4))
GROUP BY day
ORDER BY day DESC
LIMIT $3`

type histogramBucket struct {
	Date           string  `json:"date"` // YYYY-MM-DD, the local day its activities happened on
	Count          int64   `json:"count"`
	DistanceMeters float64 `json:"distance_meters"`
}

type activityHistogramResponse struct {
	Bucket string `json:"bucket"` // "day"
	// The window the buckets came from, inclusive at both ends. In calendar-window mode it
	// is whatever was asked for, so a client can lay out the empty days in between; in
	// activity-day mode it is the first and last day the page actually returned, since the
	// page is defined by a bar count rather than by a window. Both are empty when a page
	// came back with nothing.
	From    string            `json:"from"`
	To      string            `json:"to"`
	Buckets []histogramBucket `json:"buckets"`
	// The local day of the account's very first activity, omitted when they have none. Not
	// bounded by From/To — it answers a different question ("how far back is there
	// anything at all") than the window does, so the client's date slider knows
	// when it has paged back as far as there is anything to page back to, however far
	// back `from` currently is.
	Earliest string `json:"earliest,omitempty"`
}

// handleActivityHistogram serves §4.7's `GET /v1/activities/histogram` in two modes, both
// returning the same body. The filter is deliberately not applied (no `types`) in either —
// this chart is the backdrop a selected sub-range highlights against, and narrowing it the
// same way as the list would defeat that. `story` is the exception: both modes, and
// `earliest`, then cover only that Story's activities, since inside a Story the picker spans
// the Story alone (§4.23).
//
//   - `?days=N[&before=YYYY-MM-DD]` — activity-day pagination: the N most recent distinct
//     days-with-activity, or the N most recent strictly before `before`. This is what the
//     range picker's strip pages through, one screen of bars per request.
//   - `?from=&to=` — a calendar window, both optional, defaulting to the trailing
//     histogramWindowMonths months. Still the endpoint's default behaviour, and still what
//     a caller laying days out against real elapsed time (§4.8's year grid) wants.
//
// There is no `after=` counterpart to `before=`: a client paging this way starts at the
// newest end and only ever walks backwards, so everything between its anchor and today is
// already in hand by construction.
func (s *Server) handleActivityHistogram(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loc := locationFromContext(r.Context())
	story, err := parseStoryParam(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var (
		buckets []histogramBucket
		from    string
		to      string
	)
	if q.Get("days") != "" {
		buckets, err = s.activityDayPage(r, q, story)
	} else {
		buckets, from, to, err = s.activityCalendarWindow(r, q, loc, story)
	}
	if err != nil {
		s.writeHistogramError(w, err)
		return
	}
	if q.Get("days") != "" && len(buckets) > 0 {
		from, to = buckets[0].Date, buckets[len(buckets)-1].Date
	}

	earliest, err := s.earliestActivity(r.Context(), userIDFromContext(r.Context()), story)
	if err != nil {
		s.log.Error("activity earliest-date query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := activityHistogramResponse{Bucket: "day", From: from, To: to, Buckets: buckets}
	if earliest != nil {
		resp.Earliest = earliest.Format(dateLayout)
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// earliestActivity is the local date the account's first activity started on — or the
// Story's, when story is set — nil when there is none: the histogram's `earliest`, and how far
// back the Profile page's year grids go. A date, at midnight UTC.
func (s *Server) earliestActivity(ctx context.Context, userID string, story *string) (*time.Time, error) {
	var earliest *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT MIN(`+localStartedAt+`)::date FROM activities
		 WHERE user_id = $1 AND superseded_by IS NULL
		   AND ($2::uuid IS NULL OR activities.id IN (SELECT activity_id FROM story_activities WHERE story_id = $2))`,
		userID, story,
	).Scan(&earliest)
	return earliest, err
}

// dailyTotals is the histogram's calendar-window query for the local dates [first, end): one
// bucket per local day with activity, ascending, over one Story's activities when story is
// set — the JSON endpoint's `?from=&to=` mode and the Profile page's year grids both read it.
func (s *Server) dailyTotals(ctx context.Context, userID string, first, end time.Time, story *string) ([]histogramBucket, error) {
	return s.scanHistogramBuckets(ctx, activityHistogramQuery, userID, sqlDate(first), sqlDate(end), story)
}

// badRequest marks an error as the client's fault, so the two mode helpers can return plain
// errors and let one caller decide the status code.
type badRequest struct{ error }

func (s *Server) writeHistogramError(w http.ResponseWriter, err error) {
	var bad badRequest
	if errors.As(err, &bad) {
		http.Error(w, bad.Error(), http.StatusBadRequest)
		return
	}
	s.log.Error("activity histogram query failed", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// activityDayPage is the `?days=N&before=` mode. Buckets come back newest-first from the
// LIMIT and are reversed here, so every response this endpoint sends is in ascending date
// order regardless of which mode produced it — a client should not have to check.
func (s *Server) activityDayPage(r *http.Request, q url.Values, story *string) ([]histogramBucket, error) {
	limit, err := strconv.Atoi(q.Get("days"))
	if err != nil || limit < 1 {
		return nil, badRequest{errors.New(`invalid "days", want a positive integer`)}
	}
	if limit > maxHistogramDays {
		limit = maxHistogramDays
	}

	// nil, not "": the query's "$2::date IS NULL OR ..." is what makes the anchor optional.
	var before *string
	if v := q.Get("before"); v != "" {
		if _, err := time.Parse(dateLayout, v); err != nil {
			return nil, badRequest{errors.New(`invalid "before" date, want YYYY-MM-DD`)}
		}
		before = &v
	}

	buckets, err := s.scanHistogramBuckets(r.Context(), activityDayPageQuery, userIDFromContext(r.Context()), before, limit, story)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(buckets)-1; i < j; i, j = i+1, j-1 {
		buckets[i], buckets[j] = buckets[j], buckets[i]
	}
	return buckets, nil
}

// activityCalendarWindow is the original `?from=&to=` mode, unchanged in behaviour: one
// bucket per day that has activity within the window, and the window itself echoed back so
// the caller can lay out the days that have none.
func (s *Server) activityCalendarWindow(r *http.Request, q url.Values, loc *time.Location, story *string) ([]histogramBucket, string, string, error) {
	today := time.Now().In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)

	last := today
	if v := q.Get("to"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			return nil, "", "", badRequest{errors.New(`invalid "to" date, want YYYY-MM-DD`)}
		}
		last = t
	}
	first := last.AddDate(0, -histogramWindowMonths, 0)
	if v := q.Get("from"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			return nil, "", "", badRequest{errors.New(`invalid "from" date, want YYYY-MM-DD`)}
		}
		first = t
	}
	if first.After(last) {
		return nil, "", "", badRequest{errors.New(`"from" must not be after "to"`)}
	}
	end := last.AddDate(0, 0, 1) // exclusive upper bound for the query

	buckets, err := s.dailyTotals(r.Context(), userIDFromContext(r.Context()), first, end, story)
	if err != nil {
		return nil, "", "", err
	}
	return buckets, first.Format(dateLayout), last.Format(dateLayout), nil
}

// scanHistogramBuckets runs either histogram query — they return the same three columns in
// the same order, and differ only in how they choose which days to return.
func (s *Server) scanHistogramBuckets(ctx context.Context, query string, args ...any) ([]histogramBucket, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := make([]histogramBucket, 0)
	for rows.Next() {
		var day time.Time
		var b histogramBucket
		if err := rows.Scan(&day, &b.Count, &b.DistanceMeters); err != nil {
			return nil, err
		}
		b.Date = day.Format(dateLayout)
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buckets, nil
}

// activityStatsAggregateQuery is count/distance/active-days over an optional [from, to) window
// — the same three numbers activitySummaryQuery above already computes, minus the `types`
// filter §4.8's graph has no use for (the grid and its cards describe a whole year, not a
// narrowed view of one), plus active-days, which nothing existing exposes as a single number:
// a caller wanting it today would have to count histogram buckets itself.
const activityStatsAggregateQuery = `
SELECT COUNT(*),
       COALESCE(SUM(distance_meters), 0),
       COUNT(DISTINCT ` + localStartedAt + `::date)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::date IS NULL OR ` + localStartedAt + ` >= $2::date)
  AND ($3::date IS NULL OR ` + localStartedAt + ` < $3::date)`

// activityLongestStreakQuery finds the longest run of consecutive calendar days with at least
// one activity, over the same optional window — a classic gaps-and-islands query, not
// something any existing query here computes. `day` minus its own row number (as whole days)
// is constant across a run of consecutive dates and changes at every gap, so grouping by that
// expression groups exactly the consecutive runs; the largest group is the longest streak.
const activityLongestStreakQuery = `
WITH days AS (
  SELECT DISTINCT ` + localStartedAt + `::date AS day
  FROM activities
  WHERE user_id = $1
    AND superseded_by IS NULL
    AND ($2::date IS NULL OR ` + localStartedAt + ` >= $2::date)
    AND ($3::date IS NULL OR ` + localStartedAt + ` < $3::date)
),
islands AS (
  SELECT day - (ROW_NUMBER() OVER (ORDER BY day))::int * INTERVAL '1 day' AS run
  FROM days
)
SELECT COALESCE(MAX(run_length), 0)
FROM (SELECT COUNT(*) AS run_length FROM islands GROUP BY run) AS runs`

// activityStatsBlock is one of §4.8's two stat-card rows (a selected year, or all-time) — the
// same four numbers repeat under each year's grid and again as the page's
// all-time header cards.
type activityStatsBlock struct {
	Count             int64   `json:"count"`
	DistanceMeters    float64 `json:"distance_meters"`
	ActiveDays        int64   `json:"active_days"`
	LongestStreakDays int64   `json:"longest_streak_days"`
}

// activityStats runs both queries above for one optional [from, to) window. Two round trips
// rather than one combined query: the aggregate is a plain GROUP-BY-free scan and the streak
// needs its own CTE, and combining them into one query would mean computing the window twice
// anyway (once per CTE) for no fewer round trips saved, since both still have to run to
// completion before a single response can be built either way.
func (s *Server) activityStats(ctx context.Context, userID string, from, to *string) (activityStatsBlock, error) {
	var b activityStatsBlock
	if err := s.pool.QueryRow(ctx, activityStatsAggregateQuery, userID, from, to).
		Scan(&b.Count, &b.DistanceMeters, &b.ActiveDays); err != nil {
		return activityStatsBlock{}, err
	}
	if err := s.pool.QueryRow(ctx, activityLongestStreakQuery, userID, from, to).
		Scan(&b.LongestStreakDays); err != nil {
		return activityStatsBlock{}, err
	}
	return b, nil
}

type activityGraphStatsResponse struct {
	Year int `json:"year"`
	// The requested calendar year's own numbers — repeated under each year's
	// grid.
	YearStats activityStatsBlock `json:"year_stats"`
	// Every activity ever recorded, regardless of year — the page's own header cards.
	AllTime activityStatsBlock `json:"all_time"`
}

// handleActivityGraphStats serves §4.8's `GET /v1/activities/graph-stats?year=YYYY`: the four
// stat-card numbers (count, distance, active days, longest streak) for one calendar year and
// for all time. The grid's cells themselves need no new endpoint — they are exactly
// handleActivityHistogram's existing `?from=&to=` calendar-window mode, year-scoped
// (`?from=YYYY-01-01&to=YYYY-12-31`), which already returns per-day count/distance buckets and
// this user's earliest activity date for bounding the year switcher.
//
// `year` defaults to the account's own current local year when omitted, so a bare request
// from the graph's first paint (before it knows what to ask for) still gets something
// sensible back.
func (s *Server) handleActivityGraphStats(w http.ResponseWriter, r *http.Request) {
	loc := locationFromContext(r.Context())
	year := time.Now().In(loc).Year()
	if v := r.URL.Query().Get("year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < 1 {
			http.Error(w, `invalid "year", want a 4-digit year`, http.StatusBadRequest)
			return
		}
		year = y
	}

	yearStart := sqlDate(time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC))
	yearEnd := sqlDate(time.Date(year+1, time.January, 1, 0, 0, 0, 0, time.UTC))
	userID := userIDFromContext(r.Context())

	yearStats, err := s.activityStats(r.Context(), userID, &yearStart, &yearEnd)
	if err != nil {
		s.log.Error("activity year-stats query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	allTime, err := s.activityStats(r.Context(), userID, nil, nil)
	if err != nil {
		s.log.Error("activity all-time-stats query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, activityGraphStatsResponse{Year: year, YearStats: yearStats, AllTime: allTime})
}

// activityTrendsQuery is docs/SPEC.md FR-9's "trends": count/distance/moving-time/
// elevation-gain per calendar bucket (week or month), over an optional [from, to) window.
// $1 (the bucket) is a bound parameter, not string-interpolated — date_trunc accepts its
// first argument as a plain value, so this is not an injection vector — but the handler
// still validates it against an allow-list first, so a bad value gets a clean 400 instead
// of a Postgres error.
//
// moving_seconds falls back to duration_seconds per row: ingest.Process only started
// populating moving_seconds once the movingS-from-stopped-time fix landed, so activities
// ingested before that have NULL there — COALESCE keeps old and new activities rendering
// consistently in the same trend line rather than silently undercounting older periods.
const activityTrendsQuery = `
SELECT date_trunc($1, ` + localStartedAt + `)::date AS period,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0),
       COALESCE(SUM(COALESCE(moving_seconds, duration_seconds)), 0),
       COALESCE(SUM(elevation_gain_m), 0)
FROM activities
WHERE user_id = $2
  AND superseded_by IS NULL
  AND ` + localStartedAt + ` >= $3::date
  AND ` + localStartedAt + ` < $4::date
GROUP BY period
ORDER BY period`

type trendPeriod struct {
	PeriodStart    string  `json:"period_start"` // YYYY-MM-DD, the bucket's own start day
	Count          int64   `json:"count"`
	DistanceMeters float64 `json:"distance_meters"`
	MovingSeconds  int64   `json:"moving_seconds"`
	ElevationGainM float64 `json:"elevation_gain_m"`
}

type activityTrendsResponse struct {
	Bucket  string        `json:"bucket"` // "week" or "month"
	From    string        `json:"from"`
	To      string        `json:"to"`
	Periods []trendPeriod `json:"periods"`
}

// activityTrends runs activityTrendsQuery for the local dates [first, end) — the JSON endpoint
// below and the Profile page's Trends chart both read it. bucket must already be "week" or
// "month".
func (s *Server) activityTrends(ctx context.Context, userID, bucket string, first, end time.Time) ([]trendPeriod, error) {
	rows, err := s.pool.Query(ctx, activityTrendsQuery, bucket, userID, sqlDate(first), sqlDate(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	periods := make([]trendPeriod, 0)
	for rows.Next() {
		var period time.Time
		var p trendPeriod
		if err := rows.Scan(&period, &p.Count, &p.DistanceMeters, &p.MovingSeconds, &p.ElevationGainM); err != nil {
			return nil, err
		}
		p.PeriodStart = period.Format(dateLayout)
		periods = append(periods, p)
	}
	return periods, rows.Err()
}

// handleActivityTrends serves `GET /v1/activities/trends?bucket=week|month&from=&to=`.
// `from`/`to` default to the trailing histogramWindowMonths months, the same default the
// calendar-window histogram mode already uses — there's no reason a trend line's default
// window should be a different length than the graph's.
func (s *Server) handleActivityTrends(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "week"
	}
	if bucket != "week" && bucket != "month" {
		http.Error(w, `invalid "bucket", want "week" or "month"`, http.StatusBadRequest)
		return
	}

	loc := locationFromContext(r.Context())
	today := time.Now().In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)

	last := today
	if v := q.Get("to"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			http.Error(w, `invalid "to" date, want YYYY-MM-DD`, http.StatusBadRequest)
			return
		}
		last = t
	}
	first := last.AddDate(0, -histogramWindowMonths, 0)
	if v := q.Get("from"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			http.Error(w, `invalid "from" date, want YYYY-MM-DD`, http.StatusBadRequest)
			return
		}
		first = t
	}
	if first.After(last) {
		http.Error(w, `"from" must not be after "to"`, http.StatusBadRequest)
		return
	}
	end := last.AddDate(0, 0, 1) // exclusive upper bound, matching activityCalendarWindow

	periods, err := s.activityTrends(r.Context(), userIDFromContext(r.Context()), bucket, first, end)
	if err != nil {
		s.log.Error("activity trends query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, activityTrendsResponse{
		Bucket:  bucket,
		From:    first.Format(dateLayout),
		To:      last.Format(dateLayout),
		Periods: periods,
	})
}

// trackMetricsQuery reads one activity's already-simplified display trajectory back out
// (ST_DumpPoints, same shape simplifyXY already produces at ingest — this just reads it
// instead of simplifying fresh). The M ordinate is epoch seconds per §3.3, which is all a
// per-vertex speed needs besides position.
const trackMetricsQuery = `
SELECT array_agg(ST_X(pt.geom) ORDER BY pt.path),
       array_agg(ST_Y(pt.geom) ORDER BY pt.path),
       array_agg(ST_M(pt.geom) ORDER BY pt.path)
FROM activities a
CROSS JOIN LATERAL ST_DumpPoints(a.trajectory) AS pt
WHERE a.id = $1 AND a.user_id = $2
GROUP BY a.id`

type trackMetricPoint struct {
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	SpeedMps float64 `json:"speed_mps"`
	// TimeS is the point's moment, epoch seconds — what the Edit window's photo slider (§4.27)
	// turns a place on the track into.
	TimeS int64 `json:"time_s"`
}

type trackMetricsResponse struct {
	ActivityID string             `json:"activity_id"`
	Points     []trackMetricPoint `json:"points"`
}

// handleActivityTrackMetrics serves `GET /v1/activities/track-metrics/{id}` — per-simplified-
// vertex speed, for the pace-colored bands the web and Android clients draw over whichever
// single activity is selected. Pace only: HoldMyTrack keeps no heart rate (VISION.md §1.1).
// Scoped to the caller's user_id in the query itself, same as every other per-activity lookup
// here: a non-owned or nonexistent id is indistinguishable from "not found," nothing new to
// invent.
func (s *Server) handleActivityTrackMetrics(w http.ResponseWriter, r *http.Request) {
	activityID, ok := activityIDFromPath(w, r)
	if !ok {
		return
	}

	var lons, lats, ms []float64
	err := s.pool.QueryRow(r.Context(), trackMetricsQuery, activityID, userIDFromContext(r.Context())).
		Scan(&lons, &lats, &ms)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}
		s.log.Error("track metrics query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	n := len(lons)
	speed := make([]float64, n)
	for i := 1; i < n; i++ {
		// Whole seconds, as the M ordinate's clock has always been read here.
		dt := float64(int64(ms[i]) - int64(ms[i-1]))
		if dt > 0 {
			speed[i] = ingest.HaversineM(lats[i-1], lons[i-1], lats[i], lons[i]) / dt
		}
	}
	if n > 1 {
		speed[0] = speed[1]
	}

	points := make([]trackMetricPoint, n)
	for i := range points {
		points[i] = trackMetricPoint{Lon: lons[i], Lat: lats[i], SpeedMps: speed[i], TimeS: int64(ms[i])}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, trackMetricsResponse{ActivityID: activityID, Points: points})
}

// duplicatesQuery lists the activities cross-source deduplication took out of circulation
// (§4.6), each alongside the copy that displaced it.
//
// This endpoint is the reason §4.6 marks duplicates rather than deleting them. Three ingest
// paths mean the same ride can genuinely arrive three times, and an activity that silently
// stopped existing is indistinguishable from one that failed to import — the user has to be
// able to see which happened. Both sides carry `source`, because that is the actual answer to
// "why is this gone": the same ride, already in from somewhere else.
//
// Not filtered by date or type, and not paginated: duplicates are by nature a small set
// beside the history they came from, and the question this answers ("what went missing, and
// why") is asked about all of them at once.
const duplicatesQuery = `
SELECT a.id, a.started_at, a.timezone, a.activity_type, a.distance_meters, a.source,
       w.id, w.source, w.started_at, w.timezone
FROM activities a
JOIN activities w ON w.id = a.superseded_by
WHERE a.user_id = $1
ORDER BY a.started_at DESC, a.id DESC`

// supersedingActivity is the copy that won, named well enough for a client to say which one
// it is without a second request.
type supersedingActivity struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	StartedAt time.Time `json:"started_at"`
	Timezone  string    `json:"timezone"` // the zone it was recorded in (§4.30)
}

type duplicateRow struct {
	ID           string    `json:"id"`
	StartedAt    time.Time `json:"started_at"`
	Timezone     string    `json:"timezone"` // the zone it was recorded in (§4.30)
	ActivityType string    `json:"activity_type"`
	// Nullable for the same reason every other per-row metric here is: a source that reported
	// no distance is not a zero-distance activity.
	DistanceMeters *float64            `json:"distance_meters"`
	Source         string              `json:"source"`
	SupersededBy   supersedingActivity `json:"superseded_by"`
}

type duplicatesResponse struct {
	Duplicates []duplicateRow `json:"duplicates"`
}

// handleListDuplicates serves `GET /v1/activities/duplicates`.
func (s *Server) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), duplicatesQuery, userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error("duplicates query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]duplicateRow, 0)
	for rows.Next() {
		var d duplicateRow
		if err := rows.Scan(&d.ID, &d.StartedAt, &d.Timezone, &d.ActivityType, &d.DistanceMeters, &d.Source,
			&d.SupersededBy.ID, &d.SupersededBy.Source, &d.SupersededBy.StartedAt, &d.SupersededBy.Timezone); err != nil {
			s.log.Error("duplicates scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		list = append(list, d)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("duplicates read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, duplicatesResponse{Duplicates: list})
}
