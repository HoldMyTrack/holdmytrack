package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fitmap/fitmap/services/server/internal/ingest"
)

// activityFilter is the from/to/types filter shape shared by §4.3's tracks tiles and all
// three of §4.7's listing endpoints. Every field is optional and a nil field means "no
// restriction" — the convention both sections specify, so a caller that wants everything
// sends nothing rather than an explicit wide-open range.
type activityFilter struct {
	From  *time.Time
	To    *time.Time
	Types []string
}

// parseActivityFilter reads that filter off a query string, interpreting a bare `from`/`to`
// date as midnight *in loc* rather than UTC — loc is the caller's authenticated user's own
// timezone (locationFromContext, auth.go), so `?from=2026-03-10` means "the 10th, as that
// account experienced it," not always-UTC's 10th regardless of where the account actually is
// (docs/KNOWN_ISSUES.md's "UTC-day bucketing" entry). The returned error is already phrased
// for the client; callers pass it straight to http.Error with a 400.
func parseActivityFilter(q url.Values, loc *time.Location) (activityFilter, error) {
	var f activityFilter
	if v := q.Get("from"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			return activityFilter{}, errors.New(`invalid "from" date, want YYYY-MM-DD`)
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			return activityFilter{}, errors.New(`invalid "to" date, want YYYY-MM-DD`)
		}
		// End-of-day, so ?from=X&to=X (a single day) isn't an empty range.
		t = t.Add(24*time.Hour - time.Nanosecond)
		f.To = &t
	}
	// nil, not an empty non-nil slice, when the param is absent — pgx sends a nil []string
	// as SQL NULL, which is what the "$n::text[] IS NULL OR ..." clauses check for.
	if v := q.Get("types"); v != "" {
		f.Types = strings.Split(v, ",")
	}
	return f, nil
}

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
const listActivitiesQuery = `
SELECT id, started_at, activity_type, name, distance_meters, duration_seconds, description,
       ST_XMin(trajectory), ST_YMin(trajectory), ST_XMax(trajectory), ST_YMax(trajectory)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::timestamptz IS NULL OR started_at >= $2)
  AND ($3::timestamptz IS NULL OR started_at <= $3)
  AND ($4::text[] IS NULL OR activity_type = ANY($4))
ORDER BY started_at DESC, id DESC`

// activityRow is one row of the list. The field set is exactly what the Activities panel
// renders, no more. Duration is `duration_seconds`, not `moving_seconds`. ingest.Process
// populates moving_seconds now (VISION.md §5.3's ingest-gap fixes), but only for activities
// ingested since — anything older still has it NULL, and serving a field that's populated
// for some rows and not others in the one list every activity shares would read as broken
// data rather than as what it is. duration_seconds has no such gap.
//
// Name (migrations/0015_activity_name.sql) is a user-entered title, nullable — a row with
// none has never had one set, and the client falls back to started_at for its primary line
// (§4.7 revised its earlier "no name column" decision to add exactly this, and nothing
// more: still no parser reads a name out of a source file). Description (§4.7.4,
// migrations/0001_init.sql's activities.description) stays separate free-text, shown only
// as a hover tooltip — a row with nothing written there has never been edited, not "an
// empty description."
//
// The three metric fields are nullable in §3.3 and stay nullable here rather than being
// coerced to 0: a file that carried no distance is not a zero-distance activity.
type activityRow struct {
	ID              string    `json:"id"`
	StartedAt       time.Time `json:"started_at"`
	ActivityType    string    `json:"activity_type"`
	Name            *string   `json:"name"`
	DistanceMeters  *float64  `json:"distance_meters"`
	DurationSeconds *int32    `json:"duration_seconds"`
	Description     *string   `json:"description"`
	// [minLon, minLat, maxLon, maxLat] — GeoJSON's bbox ordering — or null when the row has
	// no geometry. §3.3 allows a null trajectory, and a client must not fly the map nowhere.
	BBox []float64 `json:"bbox"`
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
	if err := row.Scan(&a.ID, &a.StartedAt, &a.ActivityType, &a.Name, &a.DistanceMeters, &a.DurationSeconds, &a.Description,
		&minLon, &minLat, &maxLon, &maxLat); err != nil {
		return activityRow{}, err
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
	filter, err := parseActivityFilter(r.URL.Query(), locationFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := s.pool.Query(r.Context(), listActivitiesQuery, userIDFromContext(r.Context()), filter.From, filter.To, filter.Types)
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
// not opine on reasonable length" reasoning maxPrivacyTrimM already uses in account.go.
const maxActivityDescriptionLen = 2000

// maxActivityNameLen matches migrations/0015_activity_name.sql's VARCHAR(200) — a single-line
// title bound, deliberately shorter than maxActivityDescriptionLen: anything longer belongs
// in the description field, not the row's primary line.
const maxActivityNameLen = 200

// activityByIDQuery is the single-row counterpart to listActivitiesQuery, same column order
// (scanActivityRow's Scan call is shared between both), scoped by id and owner exactly like
// trackMetricsQuery — a non-owned or nonexistent id is indistinguishable from "not found."
const activityByIDQuery = `
SELECT id, started_at, activity_type, name, distance_meters, duration_seconds, description,
       ST_XMin(trajectory), ST_YMin(trajectory), ST_XMax(trajectory), ST_YMax(trajectory)
FROM activities
WHERE id = $1 AND user_id = $2`

type updateActivityRequest struct {
	ActivityType string `json:"activity_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
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
	activityID := r.PathValue("id")
	if activityID == "" {
		http.Error(w, "missing activity id", http.StatusBadRequest)
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
		http.Error(w, "activity_type is required", http.StatusBadRequest)
		return
	}
	if len(req.ActivityType) > maxActivityTypeLen {
		http.Error(w, fmt.Sprintf("activity_type must be %d characters or fewer", maxActivityTypeLen), http.StatusBadRequest)
		return
	}
	if len(req.Name) > maxActivityNameLen {
		http.Error(w, fmt.Sprintf("name must be %d characters or fewer", maxActivityNameLen), http.StatusBadRequest)
		return
	}
	if len(req.Description) > maxActivityDescriptionLen {
		http.Error(w, fmt.Sprintf("description must be %d characters or fewer", maxActivityDescriptionLen), http.StatusBadRequest)
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
	activityID := r.PathValue("id")
	if activityID == "" {
		http.Error(w, "missing activity id", http.StatusBadRequest)
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

	// A representative of the copies this delete is about to release (§4.6). They all share
	// one dedupe window by construction, so re-ranking from any one of them re-ranks the lot —
	// and without that they would every one become live at once, leaving the same ride counted
	// two or three times in the totals and the fog.
	var releasedType *string
	var releasedStart *time.Time
	var releasedDistance *float64
	if err := s.pool.QueryRow(ctx,
		`SELECT activity_type, started_at, distance_meters FROM activities
		 WHERE superseded_by = $1 ORDER BY created_at LIMIT 1`, activityID,
	).Scan(&releasedType, &releasedStart, &releasedDistance); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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

	// The fix for the stale-cache risk above: re-trigger the exact same dirty-mark-and-render
	// machinery ingest uses, just from removal instead of addition. RenderUser's own render is
	// "idempotent and complete per tile, not incremental" — it fully recomposites a tile from
	// whatever activity_tile_masks rows currently exist, so simply re-triggering it against
	// the now-smaller set (this activity's rows already gone via the cascade above) produces a
	// correct result with no new compositing logic needed.
	// Re-rank before re-rendering, so the render below composites the set that actually wins.
	if releasedType != nil && releasedStart != nil && releasedDistance != nil {
		extra, err := ingest.ResolveDuplicates(ctx, s.pool, userID, *releasedType, *releasedStart, *releasedDistance)
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
// this row's — a privacy trim or a shorter recording can leave each copy touching tiles the
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
const activitySummaryQuery = `
SELECT COUNT(*),
       COALESCE(SUM(distance_meters), 0),
       COALESCE(SUM(duration_seconds), 0),
       COALESCE(SUM(elevation_gain_m), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::timestamptz IS NULL OR started_at >= $2)
  AND ($3::timestamptz IS NULL OR started_at <= $3)
  AND ($4::text[] IS NULL OR activity_type = ANY($4))`

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
	filter, err := parseActivityFilter(r.URL.Query(), locationFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var resp activitySummaryResponse
	if err := s.pool.QueryRow(r.Context(), activitySummaryQuery,
		userIDFromContext(r.Context()), filter.From, filter.To, filter.Types,
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
// Deliberately unfiltered beyond the window and the user: this chart is the backdrop a
// selected sub-range is highlighted against, so narrowing it by the same filter as the list
// would defeat its purpose.
//
// Days with no activity are simply absent from the result rather than returned as zeroes —
// GROUP BY produces no row for them, and a client that has the window's start and end can
// place what it did get. That keeps the response proportional to how much a user actually
// recorded rather than to how long the window is.
//
// The bucket is the account's own local day ($4, its stored users.timezone — auth.go's
// timezoneFromContext) — see docs/KNOWN_ISSUES.md's "UTC-day bucketing" entry for why this
// used to be a hardcoded 'UTC' and what broke because of it.
const activityHistogramQuery = `
SELECT date_trunc('day', started_at AT TIME ZONE $4::text)::date AS day,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND started_at >= $2
  AND started_at < $3
GROUP BY day
ORDER BY day`

// activityDayPageQuery is the same aggregate paginated by *activity-day* rather than by
// calendar range: the `days` most recent distinct days-with-activity, optionally restricted
// to those strictly before some day. It exists because the range picker's strip now packs
// one bar per day-with-activity with the empty days removed entirely
// (apps/web/src/ui/RangePicker.tsx), so "one screen of bars" is a count of real days, not a
// width of calendar time. Asking for that with a calendar window would mean the client
// guessing a window and widening it until enough bars came back — several round trips per
// pan step over a sparse history, and a guess that is wrong in a different direction for
// every user. One page here is exactly `days` bars, every time.
//
// The anchor filters `started_at`, not the grouped `day`, so it stays on
// idx_activities_user_time: a UTC day D's activities are exactly those with
// started_at < D 00:00:00 UTC when we want the days before D, which is the same predicate
// the planner can use to seek rather than scan.
const activityDayPageQuery = `
SELECT date_trunc('day', started_at AT TIME ZONE $4::text)::date AS day,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::timestamptz IS NULL OR started_at < $2)
GROUP BY day
ORDER BY day DESC
LIMIT $3`

type histogramBucket struct {
	Date           string  `json:"date"` // YYYY-MM-DD, the account's own local day
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
	// The account's own local day of its very first activity, omitted when they have none. Not
	// bounded by From/To — it answers a different question ("how far back is there
	// anything at all") than the window does, so the client's range-picker strip knows
	// when it has paged back as far as there is anything to page back to, however far
	// back `from` currently is.
	Earliest string `json:"earliest,omitempty"`
}

// handleActivityHistogram serves §4.7's `GET /v1/activities/histogram` in two modes, both
// returning the same body. The filter is deliberately not applied (no `types`) in either —
// this chart is the backdrop a selected sub-range highlights against, and narrowing it the
// same way as the list would defeat that.
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
	tz := timezoneFromContext(r.Context())

	var (
		buckets []histogramBucket
		from    string
		to      string
		err     error
	)
	if q.Get("days") != "" {
		buckets, err = s.activityDayPage(r, q, loc, tz)
	} else {
		buckets, from, to, err = s.activityCalendarWindow(r, q, loc, tz)
	}
	if err != nil {
		s.writeHistogramError(w, err)
		return
	}
	if q.Get("days") != "" && len(buckets) > 0 {
		from, to = buckets[0].Date, buckets[len(buckets)-1].Date
	}

	var earliest *time.Time
	if err := s.pool.QueryRow(r.Context(),
		`SELECT MIN(started_at) FROM activities WHERE user_id = $1 AND superseded_by IS NULL`, userIDFromContext(r.Context()),
	).Scan(&earliest); err != nil {
		s.log.Error("activity earliest-date query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := activityHistogramResponse{Bucket: "day", From: from, To: to, Buckets: buckets}
	if earliest != nil {
		resp.Earliest = earliest.In(loc).Format(dateLayout)
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
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
func (s *Server) activityDayPage(r *http.Request, q url.Values, loc *time.Location, tz string) ([]histogramBucket, error) {
	limit, err := strconv.Atoi(q.Get("days"))
	if err != nil || limit < 1 {
		return nil, badRequest{errors.New(`invalid "days", want a positive integer`)}
	}
	if limit > maxHistogramDays {
		limit = maxHistogramDays
	}

	// nil, not a zero time.Time: the query's "$2::timestamptz IS NULL OR ..." is what makes
	// the anchor optional, and a zero time is a real (very old) timestamp, not a NULL.
	var before *time.Time
	if v := q.Get("before"); v != "" {
		t, err := time.ParseInLocation(dateLayout, v, loc)
		if err != nil {
			return nil, badRequest{errors.New(`invalid "before" date, want YYYY-MM-DD`)}
		}
		before = &t
	}

	buckets, err := s.scanHistogramBuckets(r, activityDayPageQuery, userIDFromContext(r.Context()), before, limit, tz)
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
func (s *Server) activityCalendarWindow(r *http.Request, q url.Values, loc *time.Location, tz string) ([]histogramBucket, string, string, error) {
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

	buckets, err := s.scanHistogramBuckets(r, activityHistogramQuery, userIDFromContext(r.Context()), first, end, tz)
	if err != nil {
		return nil, "", "", err
	}
	return buckets, first.Format(dateLayout), last.Format(dateLayout), nil
}

// scanHistogramBuckets runs either histogram query — they return the same three columns in
// the same order, and differ only in how they choose which days to return.
func (s *Server) scanHistogramBuckets(r *http.Request, query string, args ...any) ([]histogramBucket, error) {
	rows, err := s.pool.Query(r.Context(), query, args...)
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
       COUNT(DISTINCT date_trunc('day', started_at AT TIME ZONE $4::text)::date)
FROM activities
WHERE user_id = $1
  AND superseded_by IS NULL
  AND ($2::timestamptz IS NULL OR started_at >= $2)
  AND ($3::timestamptz IS NULL OR started_at < $3)`

// activityLongestStreakQuery finds the longest run of consecutive calendar days with at least
// one activity, over the same optional window — a classic gaps-and-islands query, not
// something any existing query here computes. `day` minus its own row number (as whole days)
// is constant across a run of consecutive dates and changes at every gap, so grouping by that
// expression groups exactly the consecutive runs; the largest group is the longest streak.
const activityLongestStreakQuery = `
WITH days AS (
  SELECT DISTINCT date_trunc('day', started_at AT TIME ZONE $4::text)::date AS day
  FROM activities
  WHERE user_id = $1
    AND superseded_by IS NULL
    AND ($2::timestamptz IS NULL OR started_at >= $2)
    AND ($3::timestamptz IS NULL OR started_at < $3)
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
func (s *Server) activityStats(ctx context.Context, userID string, from, to *time.Time, tz string) (activityStatsBlock, error) {
	var b activityStatsBlock
	if err := s.pool.QueryRow(ctx, activityStatsAggregateQuery, userID, from, to, tz).
		Scan(&b.Count, &b.DistanceMeters, &b.ActiveDays); err != nil {
		return activityStatsBlock{}, err
	}
	if err := s.pool.QueryRow(ctx, activityLongestStreakQuery, userID, from, to, tz).
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
	tz := timezoneFromContext(r.Context())
	year := time.Now().In(loc).Year()
	if v := r.URL.Query().Get("year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < 1 {
			http.Error(w, `invalid "year", want a 4-digit year`, http.StatusBadRequest)
			return
		}
		year = y
	}

	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	yearEnd := yearStart.AddDate(1, 0, 0)
	userID := userIDFromContext(r.Context())

	yearStats, err := s.activityStats(r.Context(), userID, &yearStart, &yearEnd, tz)
	if err != nil {
		s.log.Error("activity year-stats query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	allTime, err := s.activityStats(r.Context(), userID, nil, nil, tz)
	if err != nil {
		s.log.Error("activity all-time-stats query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, activityGraphStatsResponse{Year: year, YearStats: yearStats, AllTime: allTime})
}

// activityTrendsQuery is VISION.md §5.3's "trends": count/distance/moving-time/
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
SELECT date_trunc($1, started_at AT TIME ZONE $5::text)::date AS period,
       COUNT(*),
       COALESCE(SUM(distance_meters), 0),
       COALESCE(SUM(COALESCE(moving_seconds, duration_seconds)), 0),
       COALESCE(SUM(elevation_gain_m), 0)
FROM activities
WHERE user_id = $2
  AND superseded_by IS NULL
  AND started_at >= $3
  AND started_at < $4
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
	tz := timezoneFromContext(r.Context())
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

	rows, err := s.pool.Query(r.Context(), activityTrendsQuery, bucket, userIDFromContext(r.Context()), first, end, tz)
	if err != nil {
		s.log.Error("activity trends query failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	periods := make([]trendPeriod, 0)
	for rows.Next() {
		var period time.Time
		var p trendPeriod
		if err := rows.Scan(&period, &p.Count, &p.DistanceMeters, &p.MovingSeconds, &p.ElevationGainM); err != nil {
			s.log.Error("activity trends scan failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		p.PeriodStart = period.Format(dateLayout)
		periods = append(periods, p)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("activity trends rows failed", "err", err)
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
// instead of simplifying fresh) alongside its raw, full-resolution activity_streams arrays.
// The M ordinate is epoch seconds per §3.3; converting it to elapsed seconds in Go is what
// lets a simplified vertex's time be compared against activity_streams.elapsed_s at all,
// since the two are on different but convertible clocks (absolute vs. activity-relative).
const trackMetricsQuery = `
SELECT a.started_at, s.elapsed_s, s.heartrate, s.elevation_m,
       array_agg(ST_X(pt.geom) ORDER BY pt.path),
       array_agg(ST_Y(pt.geom) ORDER BY pt.path),
       array_agg(ST_M(pt.geom) ORDER BY pt.path)
FROM activities a
JOIN activity_streams s ON s.activity_id = a.id
CROSS JOIN LATERAL ST_DumpPoints(a.trajectory) AS pt
WHERE a.id = $1 AND a.user_id = $2
GROUP BY a.id, a.started_at, s.elapsed_s, s.heartrate, s.elevation_m`

type trackMetricPoint struct {
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	SpeedMps float64 `json:"speed_mps"`
	// Cumulative distance from the first vertex, in meters — always present, unlike
	// HeartRate/ElevationM, which are gated on complete coverage. This is what lets a
	// consumer (TrackProfile.tsx) lay vertices out by real distance rather than by index,
	// which would visually distort spacing wherever Douglas-Peucker kept more or fewer
	// vertices than another stretch of the same route.
	DistanceM  float64  `json:"distance_m"`
	HeartRate  *float64 `json:"heartrate,omitempty"`
	ElevationM *float64 `json:"elevation_m,omitempty"`
}

type trackMetricsResponse struct {
	ActivityID         string             `json:"activity_id"`
	HeartRateAvailable bool               `json:"heartrate_available"`
	ElevationAvailable bool               `json:"elevation_available"`
	Points             []trackMetricPoint `json:"points"`
}

// handleActivityTrackMetrics serves `GET /v1/activities/track-metrics/{id}` — per-simplified-
// vertex distance, speed, and (when available) heart rate and elevation, for MapView.tsx's
// colored zone segments and TrackProfile.tsx's straight-line pace/HR + elevation profile, both
// for whichever single activity currently has row-click focus. Scoped to the caller's user_id
// in the query itself, same as every other per-activity lookup here: a non-owned or
// nonexistent id is indistinguishable from "not found," nothing new to invent.
func (s *Server) handleActivityTrackMetrics(w http.ResponseWriter, r *http.Request) {
	activityID := r.PathValue("id")
	if activityID == "" {
		http.Error(w, "missing activity id", http.StatusBadRequest)
		return
	}

	var startedAt time.Time
	var elapsedSRaw []int32
	var heartrateRaw []*int16
	var elevationRaw []*float32
	var lons, lats, ms []float64
	err := s.pool.QueryRow(r.Context(), trackMetricsQuery, activityID, userIDFromContext(r.Context())).
		Scan(&startedAt, &elapsedSRaw, &heartrateRaw, &elevationRaw, &lons, &lats, &ms)
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
	elapsedS := make([]int64, n)
	startEpoch := startedAt.Unix()
	for i, m := range ms {
		elapsedS[i] = int64(m) - startEpoch
	}

	speed := make([]float64, n)
	distance := make([]float64, n)
	for i := 1; i < n; i++ {
		segM := ingest.HaversineM(lats[i-1], lons[i-1], lats[i], lons[i])
		distance[i] = distance[i-1] + segM
		dt := float64(elapsedS[i] - elapsedS[i-1])
		if dt > 0 {
			speed[i] = segM / dt
		}
	}
	if n > 1 {
		speed[0] = speed[1]
	}

	// All-or-nothing: a track with silently-missing segments would misrepresent effort (or,
	// for elevation, shape) rather than just not offering the metric at all.
	heartRateAvailable := len(heartrateRaw) > 0
	for _, hr := range heartrateRaw {
		if hr == nil {
			heartRateAvailable = false
			break
		}
	}
	elevationAvailable := len(elevationRaw) > 0
	for _, e := range elevationRaw {
		if e == nil {
			elevationAvailable = false
			break
		}
	}

	points := make([]trackMetricPoint, n)
	for i := range points {
		p := trackMetricPoint{Lon: lons[i], Lat: lats[i], SpeedMps: speed[i], DistanceM: distance[i]}
		if heartRateAvailable || elevationAvailable {
			// Nearest raw point by elapsed time — simplified vertices don't fall on exact
			// raw timestamps the way they fall on exact raw coordinates (matchSimplifiedTimes'
			// own exact-match approach doesn't apply here), so this is a nearest-neighbor
			// search rather than an exact one. Shared between heart rate and elevation since
			// both are matched against the same raw elapsed_s array.
			idx := nearestElapsedIndex(elapsedSRaw, elapsedS[i])
			if heartRateAvailable && heartrateRaw[idx] != nil {
				hr := float64(*heartrateRaw[idx])
				p.HeartRate = &hr
			}
			if elevationAvailable && elevationRaw[idx] != nil {
				elev := float64(*elevationRaw[idx])
				p.ElevationM = &elev
			}
		}
		points[i] = p
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, trackMetricsResponse{
		ActivityID:         activityID,
		HeartRateAvailable: heartRateAvailable,
		ElevationAvailable: elevationAvailable,
		Points:             points,
	})
}

// nearestElapsedIndex finds the index in the (ascending) raw elapsed_s array closest to
// target, via binary search rather than a linear scan — the same reasoning every other
// per-point pass in this codebase gives for avoiding an O(n) scan when a sorted structure is
// already in hand.
func nearestElapsedIndex(elapsedSRaw []int32, target int64) int {
	i := sort.Search(len(elapsedSRaw), func(i int) bool { return int64(elapsedSRaw[i]) >= target })
	if i == 0 {
		return 0
	}
	if i == len(elapsedSRaw) {
		return i - 1
	}
	if target-int64(elapsedSRaw[i-1]) <= int64(elapsedSRaw[i])-target {
		return i - 1
	}
	return i
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
SELECT a.id, a.started_at, a.activity_type, a.distance_meters, a.source,
       w.id, w.source, w.started_at
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
}

type duplicateRow struct {
	ID           string    `json:"id"`
	StartedAt    time.Time `json:"started_at"`
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
		if err := rows.Scan(&d.ID, &d.StartedAt, &d.ActivityType, &d.DistanceMeters, &d.Source,
			&d.SupersededBy.ID, &d.SupersededBy.Source, &d.SupersededBy.StartedAt); err != nil {
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
