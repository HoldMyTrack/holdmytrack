// Package ingest implements IMPLEMENTATION.md §4.1 steps 2, 3, 5, 6 — the
// work-mode half of the ingestion pipeline — plus step 4's dirty-marking and, since the
// per-activity fog/heatmap mask redesign, the actual per-activity rendering too (internal/
// fog.RenderActivityMasks): raw points are already in memory here, pre-simplification,
// exactly why §1.1 says coverage can't be derived from the simplified display trajectory,
// and exactly why rendering one activity's own masks belongs here rather than in a
// re-fetch-and-reparse step in internal/fog. The user-wide aggregate rebuild (compositing
// every activity's masks touching a tile, and the z13..z0 pyramid above it) still lives in
// internal/fog, run by a `render_fog` job this package enqueues but does not itself execute.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/geo"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/tilemath"
)

// simplifyToleranceDeg is ST_SimplifyPreserveTopology's tolerance for the display trajectory
// (§4.1 step 5), in degrees (the geometry's own units, WGS84). ~0.00003° is roughly 3 m at
// the equator — small enough to preserve trail shape, large enough to matter on long tracks.
// Not specified numerically in IMPLEMENTATION.md; chosen and documented here
// as the value to revisit if display tracks look over- or under-simplified.
const simplifyToleranceDeg = 0.00003

// Job is what the `ingest` job's payload carries — everything the worker needs that isn't
// already in the raw payload itself.
type Job struct {
	UserID        string `json:"user_id"`
	Source        string `json:"source"`
	SourceDetail  string `json:"source_detail"`
	ExternalID    string `json:"external_id"`
	RawPayloadKey string `json:"raw_payload_key"`
	// ActivityType overrides whatever the parser itself reports, when set. Empty means "use
	// the parsed value" (the zero value already does the right thing for every existing
	// caller). This exists for the Google Takeout import path
	// (services/server/internal/httpapi/takeout_upload.go): activities are extracted one type
	// at a time, so the caller already knows each one's real type — a per-activity Takeout GPX
	// carries no `<type>` element of its own to parse back out, and reconstructing one from its
	// display name would just be re-deriving something the caller already has for free.
	ActivityType string `json:"activity_type,omitempty"`
}

// Result reports what happened, distinguishing "persisted a new activity" from "this was
// already processed" — IMPLEMENTATION.md §4.0's idempotency invariant: a
// conflict at persist time is a successful no-op, not an error.
type Result struct {
	ActivityID string
	Persisted  bool // false means ON CONFLICT DO NOTHING fired: already processed.
}

func Process(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job Job) (Result, error) {
	act, points, err := loadClippedPoints(ctx, pool, store, job.UserID, job.SourceDetail, job.RawPayloadKey)
	if err != nil {
		return Result{}, err
	}
	if job.ActivityType != "" {
		act.ActivityType = job.ActivityType
	}
	// A track entirely inside Private locations is still persisted, just with no geometry:
	// dropping it would lose it for good, even after the location that hides it is deleted.
	hidden := points == nil
	startedAt := act.Points[0].Time

	var pp preparedTrack
	if !hidden {
		if pp, err = prepareTrack(ctx, pool, points); err != nil {
			return Result{}, err
		}
		startedAt = points[0].Time
	}
	m := pp.m

	var activityID string
	var inserted bool
	err = pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO activities (
				user_id, source, source_detail, external_id,
				activity_type, distance_meters, duration_seconds, moving_seconds,
				elevation_gain_m, avg_speed_mps, started_at,
				trajectory, raw_payload_key, name, description
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11,
				`+trajectorySQL("$12", "$13", "$14")+`,
				$15, NULLIF($16, ''), NULLIF($17, '')
			)
			ON CONFLICT (user_id, source, external_id) WHERE external_id IS NOT NULL DO NOTHING -- $15 = raw_payload_key
			RETURNING id
		)
		SELECT id, true FROM ins
		UNION ALL
		SELECT id, false FROM activities
		WHERE user_id = $1 AND source = $2 AND external_id = $4
		  AND NOT EXISTS (SELECT 1 FROM ins)
		LIMIT 1
	`,
		job.UserID, job.Source, job.SourceDetail, job.ExternalID,
		act.ActivityType, m.distanceM, m.durationS, m.movingS,
		m.elevationGainM, m.avgSpeedMps, startedAt,
		pp.simpLons, pp.simpLats, pp.simpTs,
		job.RawPayloadKey, act.Name, act.Description,
	).Scan(&activityID, &inserted)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: persist activity: %w", err)
	}

	if !inserted {
		// Idempotency requirement: a duplicate that reached persist is a no-op, not an
		// error, and the stream write is skipped along with it — the row from the first
		// successful ingest already has its streams. Fog is skipped too: nothing new
		// happened, so no tile needs marking dirty again.
		return Result{ActivityID: activityID, Persisted: false}, nil
	}
	if hidden {
		// No streams, masks, tiles or regions: there is nothing visible to derive them from.
		return Result{ActivityID: activityID, Persisted: true}, nil
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO activity_streams (activity_id, point_count, elapsed_s, elevation_m, heartrate, dist_m)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, activityID, len(points), pp.elapsedS, pp.elevM, pp.hr, pp.distM)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: persist streams: %w", err)
	}

	// §4.1 step 4: render this activity's own fog/heatmap masks and mark every z14 tile it
	// touches dirty, both from `points` (pre-simplification), not the simplified trajectory
	// just persisted — a straight line between two of Douglas-Peucker's surviving vertices
	// can cut across tiles the real GPS path never entered, which would render/reveal tiles
	// the user never actually visited. Rendering first, dirty-marking second: dirty is what
	// tells `render_fog` this tile's aggregate needs recompositing, so the mask it would
	// composite in should already exist by the time that runs.
	tiles := computeTouchedTiles(points, FogZoom)
	if err := fog.RenderActivityMasks(ctx, pool, store, activityID, points, tiles); err != nil {
		return Result{}, fmt.Errorf("ingest: render activity masks: %w", err)
	}

	// §4.6's cross-source deduplication, after the streams and masks above and not before:
	// richness is ranked off those rows, so a record judged ahead of its own streams would
	// lose every collision it entered. Masks are rendered for the loser too — they cost
	// nothing to keep, every read already excludes a superseded activity, and if the winner
	// is ever deleted the loser becomes live again with its coverage already on file.
	//
	// The tiles it reports back are added to the dirty set rather than replacing it: a
	// collision can change which activities a tile the *new* one never touched is composited
	// from, and rebuilding only the new one's tiles would leave the rest showing coverage
	// from a copy that no longer counts.
	dedupeTiles, err := ResolveDuplicates(ctx, pool, job.UserID, points[0].Time, m.durationS)
	if err != nil {
		return Result{}, err
	}
	if err := MarkFogTilesDirty(ctx, pool, job.UserID, mergeTiles(tiles, dedupeTiles)); err != nil {
		return Result{}, fmt.Errorf("ingest: mark fog tiles dirty: %w", err)
	}

	// Country/Region-tier unlocking (§4.2.4) — independent of the fog/heatmap raster pyramid
	// above, so it runs alongside it rather than inside fog's own dirty/render pipeline: there
	// is nothing here to mark dirty or recomposite, matching against activity_id is enough.
	if err := geo.MatchActivity(ctx, pool, activityID); err != nil {
		return Result{}, fmt.Errorf("ingest: match admin boundaries: %w", err)
	}

	if err := EnqueueRenderFog(ctx, pool, job.UserID); err != nil {
		return Result{}, fmt.Errorf("ingest: enqueue render_fog: %w", err)
	}

	return Result{ActivityID: activityID, Persisted: true}, nil
}

// loadClippedPoints is §4.1 steps 2–3: fetch the raw payload, parse it, and clip it against
// the account's Private locations, read now rather than when the job was enqueued. Shared by
// Process and reprocessActivity (edit.go) — an edit replays the user's spec on top of exactly
// the points a fresh ingest would have kept, never on hidden ones.
//
// The returned points are nil when the whole track lies inside Private locations; act still
// carries the parsed metadata (type, name, start time) the caller persists either way.
func loadClippedPoints(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, sourceDetail, rawKey string) (parse.Activity, []parse.Point, error) {
	obj, err := store.Get(ctx, rawKey)
	if err != nil {
		return parse.Activity{}, nil, fmt.Errorf("ingest: fetch raw payload: %w", err)
	}
	defer obj.Close()

	act, err := parseByExtensionReader(sourceDetail, obj)
	if err != nil {
		return parse.Activity{}, nil, parseError{err}
	}
	if act.Points, err = keepTimed(act.Points); err != nil {
		return parse.Activity{}, nil, err
	}
	if len(act.Points) < 2 {
		return parse.Activity{}, nil, fmt.Errorf("ingest: %w (%d)", errTooFewPoints, len(act.Points))
	}

	zones, err := LoadZones(ctx, pool, userID)
	if err != nil {
		return parse.Activity{}, nil, fmt.Errorf("ingest: load private locations: %w", err)
	}
	return act, ClipEnds(act.Points, zones), nil
}

// errNoTimestamps fails a file whose points carry no time at all — a planned route exported
// as GPX, typically. Every parser leaves a missing time as the zero time.Time, so such a
// track would otherwise be persisted as starting on 0001-01-01, with no duration, and be
// unreachable through the date range the whole map is filtered by.
var errNoTimestamps = errors.New("the file has no timestamps — a planned route, not a recorded activity")

// keepTimed drops the points with no timestamp, and fails when none has one. A few untimed
// points among timed ones (a stray waypoint) are dropped rather than failing the file; kept,
// each would read as a jump back to year 1 in the duration and pace arithmetic.
func keepTimed(points []parse.Point) ([]parse.Point, error) {
	timed := points[:0:0]
	for _, p := range points {
		if !p.Time.IsZero() {
			timed = append(timed, p)
		}
	}
	if len(timed) == 0 && len(points) > 0 {
		return nil, errNoTimestamps
	}
	return timed, nil
}

// LoadClippedPoints is loadClippedPoints for the track editor's point endpoint
// (httpapi's handleActivityTrackPoints), which needs the same post-clip points to show.
func LoadClippedPoints(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, sourceDetail, rawKey string) ([]parse.Point, error) {
	_, points, err := loadClippedPoints(ctx, pool, store, userID, sourceDetail, rawKey)
	return points, err
}

// trajectorySQL builds the display LineStringM from three parallel lon/lat/epoch-seconds
// array parameters, or NULL when they hold fewer than two points — an activity entirely
// inside Private locations (§3.3 allows a null trajectory, and every reader already copes).
func trajectorySQL(lons, lats, ts string) string {
	return `CASE WHEN cardinality(` + lons + `::float8[]) >= 2 THEN ST_SetSRID(
					ST_MakeLine(ARRAY(
						SELECT ST_MakePointM(lon, lat, t)
						FROM unnest(` + lons + `::float8[], ` + lats + `::float8[], ` + ts + `::float8[]) AS pt(lon, lat, t)
					)),
					4326
				) END`
}

// preparedTrack is everything §4.1 steps 5–6 persist, derived from the final point list.
type preparedTrack struct {
	m                          metrics
	simpLons, simpLats, simpTs []float64
	elapsedS                   []int32
	elevM                      []*float32
	hr                         []*int16
	distM                      []float32
}

func prepareTrack(ctx context.Context, pool *pgxpool.Pool, points []parse.Point) (preparedTrack, error) {
	pp := preparedTrack{m: computeMetrics(points)}

	lons := make([]float64, len(points))
	lats := make([]float64, len(points))
	ts := make([]float64, len(points))
	for i, p := range points {
		lons[i] = p.Lon
		lats[i] = p.Lat
		ts[i] = float64(p.Time.Unix())
	}

	// §4.1 step 5: simplify for display. Two round trips, not one — see simplify.go for
	// why ST_SimplifyPreserveTopology can't just be called inline on the LineStringM.
	var err error
	pp.simpLons, pp.simpLats, err = simplifyXY(ctx, pool, lons, lats, simplifyToleranceDeg)
	if err != nil {
		return preparedTrack{}, err
	}
	pp.simpTs = matchSimplifiedTimes(lons, lats, ts, pp.simpLons, pp.simpLats)

	pp.elapsedS = make([]int32, len(points))
	pp.elevM = make([]*float32, len(points))
	pp.hr = make([]*int16, len(points))
	pp.distM = make([]float32, len(points))
	t0 := points[0].Time.Unix()
	for i, p := range points {
		pp.elapsedS[i] = int32(p.Time.Unix() - t0)
		pp.elevM[i] = p.Elevation
		pp.hr[i] = p.HeartRate
		pp.distM[i] = float32(pp.m.distM[i])
	}
	return pp, nil
}

// mergeTiles unions two tile lists, dropping duplicates — MarkFogTilesDirty is an upsert, so
// a repeat is harmless, but the dirty set is also what bounds the rebuild's cost.
func mergeTiles(a, b [][2]int) [][2]int {
	if len(b) == 0 {
		return a
	}
	seen := make(map[[2]int]struct{}, len(a)+len(b))
	out := make([][2]int, 0, len(a)+len(b))
	for _, list := range [][][2]int{a, b} {
		for _, t := range list {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// FogZoom is exported alongside MarkFogTilesDirty/EnqueueRenderFog — a caller re-triggering a
// render (handleDeleteActivity) needs the same zoom level activity_tile_masks/fog_tiles are
// actually keyed at to query the right rows.
const FogZoom = 14

// computeTouchedTiles walks the raw, pre-simplification trajectory and returns every z14
// tile it passes through — including tiles between consecutive points, not just the tiles
// containing a point (see tilemath.SegmentTilesBuffered), and including any tile a point or
// segment never enters but sits close enough to (within fog.TileMarginPx) that the rendered
// stroke's own drawn width still reaches into. Shared by RenderActivityMasks (which tiles to
// render a mask for) and MarkFogTilesDirty (which tiles to flag for aggregate rebuild) — the
// two need to agree exactly, or a rendered mask could sit in a tile never marked dirty (never
// composited in) or vice versa.
func computeTouchedTiles(points []parse.Point, zoom int) [][2]int {
	seen := map[[2]int]struct{}{}
	add := func(x, y int) { seen[[2]int{x, y}] = struct{}{} }

	if len(points) == 1 {
		for _, t := range tilemath.SegmentTilesBuffered(points[0].Lon, points[0].Lat, points[0].Lon, points[0].Lat, zoom, fog.TileMarginPx, fog.TileSize) {
			add(t[0], t[1])
		}
	}
	for i := 1; i < len(points); i++ {
		for _, t := range tilemath.SegmentTilesBuffered(points[i-1].Lon, points[i-1].Lat, points[i].Lon, points[i].Lat, zoom, fog.TileMarginPx, fog.TileSize) {
			add(t[0], t[1])
		}
	}

	out := make([][2]int, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	return out
}

// MarkFogTilesDirty upserts a fog_tiles row (dirty = true) for every given z14 tile —
// computeTouchedTiles' result for a fresh ingest, a deleted activity's own already-rendered
// tiles (`internal/httpapi`'s handleDeleteActivity, which has no raw points to recompute
// touched tiles from — it reads them back out of activity_tile_masks before the cascade
// removes that row), or an activity that just aged out of Heatmap's rolling window
// (`internal/worker`'s heatmap_aging.go, via ActivityTiles below). Exported for those callers;
// the upsert itself doesn't care which triggered it.
func MarkFogTilesDirty(ctx context.Context, pool *pgxpool.Pool, userID string, tiles [][2]int) error {
	if len(tiles) == 0 {
		return nil
	}

	xs := make([]int32, 0, len(tiles))
	ys := make([]int32, 0, len(tiles))
	for _, t := range tiles {
		xs = append(xs, int32(t[0]))
		ys = append(ys, int32(t[1]))
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO fog_tiles (user_id, zoom, tile_x, tile_y, dirty)
		SELECT $1, $2, x, y, true
		FROM unnest($3::int[], $4::int[]) AS t(x, y)
		ON CONFLICT (user_id, zoom, tile_x, tile_y) DO UPDATE SET dirty = true
	`, userID, FogZoom, xs, ys)
	return err
}

// ActivityTiles reads back the z14 tiles one activity's own crisp masks cover —
// activity_tile_masks' primary key leads with activity_id, so this is an index-only lookup.
// Shared by handleDeleteActivity's own tile lookup (which additionally merges in any
// activity this one supersedes — see its own activityFogTiles) and heatmap_aging.go's daily
// sweep, which has no supersede case to worry about: an activity aging out of the window is
// still live, just no longer eligible, so only its own tiles ever need re-rendering.
func ActivityTiles(ctx context.Context, pool *pgxpool.Pool, activityID string) ([][2]int, error) {
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT tile_x, tile_y FROM activity_tile_masks WHERE activity_id = $1 AND zoom = $2`,
		activityID, FogZoom,
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

// RenderFogJob is `render_fog`'s payload — just enough to say whose tiles to render, since
// which tiles is the dirty flag's job, not this payload's.
type RenderFogJob struct {
	UserID string `json:"user_id"`
}

// EnqueueRenderFog is one job per user, not one per tile or per ingest — the job's own
// query (`SELECT ... FROM fog_tiles WHERE user_id = $1 AND dirty`) is what actually decides
// what to render, so several ingests (or a delete, or both) landing before the worker gets to
// them coalesce into one render pass instead of redoing the same tiles repeatedly. Exported
// for handleDeleteActivity, which needs the exact same "something changed, re-render this
// user's dirty tiles" trigger ingest already has.
func EnqueueRenderFog(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	payload, err := json.Marshal(RenderFogJob{UserID: userID})
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO jobs (kind, user_id, payload) VALUES ('render_fog', $1, $2)`,
		userID, payload,
	)
	return err
}

func parseByExtensionReader(filename string, r io.Reader) (parse.Activity, error) {
	return parse.ByExtension(filename, r)
}

type metrics struct {
	distanceM      float64
	durationS      int
	movingS        int
	elevationGainM float64
	avgSpeedMps    float64
	// distM[i] is cumulative distance in meters at points[i]; distM[0] is always 0.
	distM []float64
}

// movingSpeedThresholdMps is the speed below which a segment counts as stopped, not moving,
// for movingS below. ~1.8 km/h: fast enough to filter GPS jitter while genuinely stationary
// (a few meters of drift over several seconds), slow enough not to exclude real, deliberate
// slow walking.
const movingSpeedThresholdMps = 0.5

func computeMetrics(points []parse.Point) metrics {
	m := metrics{distM: make([]float64, len(points))}
	for i := 1; i < len(points); i++ {
		segM := HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
		m.distanceM += segM
		m.distM[i] = m.distanceM
		if points[i-1].Elevation != nil && points[i].Elevation != nil {
			d := float64(*points[i].Elevation - *points[i-1].Elevation)
			if d > 0 {
				m.elevationGainM += d
			}
		}
		dt := points[i].Time.Sub(points[i-1].Time).Seconds()
		if dt > 0 && segM/dt >= movingSpeedThresholdMps {
			m.movingS += int(dt)
		}
	}
	m.durationS = int(points[len(points)-1].Time.Sub(points[0].Time).Seconds())
	if m.durationS > 0 {
		m.avgSpeedMps = m.distanceM / float64(m.durationS)
	}
	if math.IsNaN(m.avgSpeedMps) || math.IsInf(m.avgSpeedMps, 0) {
		m.avgSpeedMps = 0
	}
	return m
}
