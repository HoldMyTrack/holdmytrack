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
	cad := make([]*int16, len(points))
	pw := make([]*int16, len(points))
	distM := make([]float32, len(points))
	t0 := points[0].Time.Unix()
	for i, p := range points {
		elapsedS[i] = int32(p.Time.Unix() - t0)
		elevM[i] = p.Elevation
		hr[i] = p.HeartRate
		cad[i] = p.Cadence
		pw[i] = p.PowerW
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
		INSERT INTO activity_streams (activity_id, point_count, elapsed_s, elevation_m, heartrate, cadence, power_w, dist_m)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, activityID, len(points), elapsedS, elevM, hr, cad, pw, distM)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: persist streams: %w", err)
	}

	if bests := computeBestEfforts(points, elapsedS, distM); len(bests) > 0 {
		metricsCol := make([]string, len(bests))
		windowsCol := make([]int32, len(bests))
		valuesCol := make([]float32, len(bests))
		for i, be := range bests {
			metricsCol[i] = be.metric
			windowsCol[i] = be.windowS
			valuesCol[i] = be.value
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO activity_best_efforts (activity_id, metric, window_s, value)
			SELECT $1, m, w, v FROM unnest($2::text[], $3::int[], $4::real[]) AS t(m, w, v)
		`, activityID, metricsCol, windowsCol, valuesCol)
		if err != nil {
			return Result{}, fmt.Errorf("ingest: persist best efforts: %w", err)
		}
	}

	if splits := computeSplits(elapsedS, distM); len(splits) > 0 {
		distancesCol := make([]int32, len(splits))
		secondsCol := make([]int32, len(splits))
		for i, sp := range splits {
			distancesCol[i] = sp.distanceM
			secondsCol[i] = sp.seconds
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO activity_splits (activity_id, distance_m, seconds)
			SELECT $1, d, s FROM unnest($2::int[], $3::int[]) AS t(d, s)
		`, activityID, distancesCol, secondsCol)
		if err != nil {
			return Result{}, fmt.Errorf("ingest: persist splits: %w", err)
		}
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
	if err := MarkFogTilesDirty(ctx, pool, job.UserID, tiles); err != nil {
		return Result{}, fmt.Errorf("ingest: mark fog tiles dirty: %w", err)
	}
	if err := EnqueueRenderFog(ctx, pool, job.UserID); err != nil {
		return Result{}, fmt.Errorf("ingest: enqueue render_fog: %w", err)
	}

	return Result{ActivityID: activityID, Persisted: true}, nil
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
	// distM[i] is cumulative distance in meters at points[i]; distM[0] is always 0. Read by
	// computeBestEfforts below for pace curves; personal bests (VISION.md §5.3, not
	// yet built) will need it too.
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

// bestEffortWindowsS is VISION.md §5.3's best-effort curve: the maximal average pace
// (or heart rate) sustained over each of these standard durations, 5 s to 1 h — the same
// handful of windows Strava/TrainingPeaks use, not a user-configurable list.
var bestEffortWindowsS = []int32{5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600}

// bestEffort is one (metric, window) result, ready to persist into activity_best_efforts.
type bestEffort struct {
	metric  string
	windowS int32
	value   float32
}

// computeBestEfforts finds, for each window in bestEffortWindowsS, the best (highest)
// average pace and average heart rate sustained over any span of at least that many seconds.
// Reuses elapsedS/distM — the same arrays already built for persisting activity_streams, not
// walked twice — and, only when every point has a HeartRate, a locally-built HR-weighted
// cumulative array (not persisted; heart-rate curves are the only consumer).
//
// Heart-rate curves are skipped entirely, not just the gaps, when any point in the activity
// has a nil HeartRate: a curve with silently-skipped points would understate effort rather
// than reporting nothing, and partial sensor coverage is realistically all-or-nothing per
// activity (a continuously-worn HR strap either recorded the whole thing or wasn't worn).
func computeBestEfforts(points []parse.Point, elapsedS []int32, distM []float32) []bestEffort {
	cumDist := make([]float64, len(distM))
	for i, d := range distM {
		cumDist[i] = float64(d)
	}
	results := bestEffortCurve("pace", elapsedS, cumDist)

	hasFullHR := len(points) > 0 && points[0].HeartRate != nil
	hrWeighted := make([]float64, len(points))
	for i := 1; hasFullHR && i < len(points); i++ {
		if points[i].HeartRate == nil {
			hasFullHR = false
			break
		}
		dt := float64(elapsedS[i] - elapsedS[i-1])
		hrWeighted[i] = hrWeighted[i-1] + float64(*points[i].HeartRate)*dt
	}
	if hasFullHR {
		results = append(results, bestEffortCurve("heartrate", elapsedS, hrWeighted)...)
	}
	return results
}

// bestEffortCurve is the two-pointer scan itself, shared by both metrics: `cum` is any
// cumulative value parallel to elapsedS (cumulative distance for pace, HR-weighted-by-time
// for heart rate) — "average over a window" is always (cum[r]-cum[l])/(elapsedS[r]-elapsedS[l])
// regardless of which cumulative quantity it is.
//
// For each window, the right pointer walks every point once; the left pointer only ever
// advances (never resets between right-pointer steps), because the tightest left bound
// satisfying "window >= w" is monotonically non-decreasing as the right pointer moves
// forward — so each window is one O(n) pass, not a nested O(n²) scan. A window longer than
// the activity's own duration finds no valid position and is simply omitted, not stored as
// zero.
func bestEffortCurve(metric string, elapsedS []int32, cum []float64) []bestEffort {
	var out []bestEffort
	n := len(elapsedS)
	for _, w := range bestEffortWindowsS {
		l := 0
		var best float64
		found := false
		for r := 0; r < n; r++ {
			for l+1 <= r && elapsedS[r]-elapsedS[l+1] >= w {
				l++
			}
			if elapsedS[r]-elapsedS[l] >= w {
				dt := float64(elapsedS[r] - elapsedS[l])
				avg := (cum[r] - cum[l]) / dt
				if !found || avg > best {
					best = avg
					found = true
				}
			}
		}
		if found {
			out = append(out, bestEffort{metric: metric, windowS: w, value: float32(best)})
		}
	}
	return out
}

// standardDistancesM is VISION.md §5.3's personal-bests distances — the same five
// Strava itself tracks as PRs, metric throughout (this app has no imperial distances
// anywhere else). No per-activity-type split: activity_type has no controlled vocabulary
// (IMPLEMENTATION.md §4.7), so bucketing by it would fragment the same
// achievement across e.g. "run"/"running"/"trail_running" rather than unify it.
var standardDistancesM = []float64{1000, 5000, 10000, 21097.5, 42195}

// split is one (distance, fastest time) result, ready to persist into activity_splits.
type split struct {
	distanceM int32
	seconds   int32
}

// computeSplits finds, for each distance in standardDistancesM, the fastest time this
// activity covered at least that far — the inverse question of bestEffortCurve (minimize
// time for a fixed distance, rather than maximize average for a fixed duration), so the
// two-pointer roles swap: the right pointer walks every point, the left pointer advances
// while the window still covers >= D meters after dropping one more point, monotonic in l
// for the same reason as bestEffortCurve (the tightest left bound covering D only moves
// forward as more distance accumulates to the right). An activity shorter than D simply
// never finds a candidate and produces no row for it. Unlike heart-rate curves, this needs
// no data-completeness gate — distance and elapsed time have no missing-sensor case.
func computeSplits(elapsedS []int32, distM []float32) []split {
	var out []split
	n := len(elapsedS)
	for _, d := range standardDistancesM {
		l := 0
		var best int32 = -1
		for r := 0; r < n; r++ {
			for l+1 <= r && float64(distM[r]-distM[l+1]) >= d {
				l++
			}
			if float64(distM[r]-distM[l]) >= d {
				dt := elapsedS[r] - elapsedS[l]
				if best < 0 || dt < best {
					best = dt
				}
			}
		}
		if best >= 0 {
			out = append(out, split{distanceM: int32(d), seconds: best})
		}
	}
	return out
}
