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
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/fog"
	"github.com/fitmap/fitmap/services/server/internal/parse"
	"github.com/fitmap/fitmap/services/server/internal/storage"
	"github.com/fitmap/fitmap/services/server/internal/tilemath"
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
	PrivacyTrimM  int    `json:"privacy_trim_m"`
	// ActivityType overrides whatever the parser itself reports, when set. Empty means "use
	// the parsed value" (the zero value already does the right thing for every existing
	// caller). This exists for the Google Takeout import path
	// (services/server/internal/httpapi/takeout_upload.go): `pathify takeout` is invoked once
	// per activity type into its own directory, so the caller already knows each extracted
	// file's real type — a plain per-activity GPX carries no `<type>` element of its own to
	// parse back out, and reconstructing one from pathify's per-file naming convention would
	// just be re-deriving something the caller already has for free.
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
	obj, err := store.Get(ctx, job.RawPayloadKey)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: fetch raw payload: %w", err)
	}
	defer obj.Close()

	act, err := parseByExtensionReader(job.SourceDetail, obj)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: parse: %w", err)
	}
	if job.ActivityType != "" {
		act.ActivityType = job.ActivityType
	}

	points := TrimEndpoints(act.Points, job.PrivacyTrimM)
	if len(points) < 2 {
		return Result{}, fmt.Errorf("ingest: fewer than 2 points survive privacy trim (%d before, %d after)", len(act.Points), len(points))
	}

	m := computeMetrics(points)

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
	simpLons, simpLats, err := simplifyXY(ctx, pool, lons, lats, simplifyToleranceDeg)
	if err != nil {
		return Result{}, err
	}
	simpTs := matchSimplifiedTimes(lons, lats, ts, simpLons, simpLats)

	elapsedS := make([]int32, len(points))
	elevM := make([]*float32, len(points))
	hr := make([]*int16, len(points))
	distM := make([]float32, len(points))
	t0 := points[0].Time.Unix()
	for i, p := range points {
		elapsedS[i] = int32(p.Time.Unix() - t0)
		elevM[i] = p.Elevation
		hr[i] = p.HeartRate
		distM[i] = float32(m.distM[i])
	}

	var activityID string
	var inserted bool
	err = pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO activities (
				user_id, source, source_detail, external_id,
				activity_type, distance_meters, duration_seconds, moving_seconds,
				elevation_gain_m, avg_speed_mps, started_at,
				trajectory, raw_payload_key
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11,
				ST_SetSRID(
					ST_MakeLine(ARRAY(
						SELECT ST_MakePointM(lon, lat, t)
						FROM unnest($12::float8[], $13::float8[], $14::float8[]) AS pt(lon, lat, t)
					)),
					4326
				),
				$15
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
		m.elevationGainM, m.avgSpeedMps, points[0].Time,
		simpLons, simpLats, simpTs,
		job.RawPayloadKey,
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

	_, err = pool.Exec(ctx, `
		INSERT INTO activity_streams (activity_id, point_count, elapsed_s, elevation_m, heartrate, dist_m)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, activityID, len(points), elapsedS, elevM, hr, distM)
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
	dedupeTiles, err := ResolveDuplicates(ctx, pool, job.UserID, act.ActivityType, points[0].Time, m.distanceM)
	if err != nil {
		return Result{}, err
	}
	if err := MarkFogTilesDirty(ctx, pool, job.UserID, mergeTiles(tiles, dedupeTiles)); err != nil {
		return Result{}, fmt.Errorf("ingest: mark fog tiles dirty: %w", err)
	}
	if err := EnqueueRenderFog(ctx, pool, job.UserID); err != nil {
		return Result{}, fmt.Errorf("ingest: enqueue render_fog: %w", err)
	}

	return Result{ActivityID: activityID, Persisted: true}, nil
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
// containing a point (see tilemath.SegmentTiles). Shared by RenderActivityMasks (which tiles
// to render a mask for) and MarkFogTilesDirty (which tiles to flag for aggregate rebuild) —
// the two need to agree exactly, or a rendered mask could sit in a tile never marked dirty
// (never composited in) or vice versa.
func computeTouchedTiles(points []parse.Point, zoom int) [][2]int {
	seen := map[[2]int]struct{}{}
	add := func(x, y int) { seen[[2]int{x, y}] = struct{}{} }

	if len(points) == 1 {
		x, y := tilemath.LonLatToTile(points[0].Lon, points[0].Lat, zoom)
		add(x, y)
	}
	for i := 1; i < len(points); i++ {
		for _, t := range tilemath.SegmentTiles(points[i-1].Lon, points[i-1].Lat, points[i].Lon, points[i].Lat, zoom) {
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
// computeTouchedTiles' result for a fresh ingest, or a deleted activity's own already-rendered
// tiles (`internal/httpapi`'s handleDeleteActivity, which has no raw points to recompute
// touched tiles from — it reads them back out of activity_tile_masks before the cascade
// removes that row). Exported for that second caller; the upsert itself doesn't care which.
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

// MarkAllFogTilesDirty flags every z14 tile a user already has a fog_tiles row for, dirty —
// unlike MarkFogTilesDirty, not in response to anything that changed, but so a scheduled
// re-render (internal/worker's heatmapRefresh) picks every one of them up. Fog's own
// aggregate is unaffected by this (it has no notion of "aging out"), so re-rendering it here
// is pure, harmless recomputation of the same result — this exists to slide Heatmap's rolling
// window's trailing edge on tiles nothing has touched recently enough to dirty on its own.
func MarkAllFogTilesDirty(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE fog_tiles SET dirty = true WHERE user_id = $1 AND zoom = $2`,
		userID, FogZoom,
	)
	return err
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
