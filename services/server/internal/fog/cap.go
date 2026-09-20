package fog

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// minTouchedTilesForAdaptiveCap: below this many distinct z14 tiles with any coverage, an
	// account hasn't accumulated enough history for its own max touch count to mean anything
	// yet — two activities that happen to share one tile early on would otherwise set the cap
	// to 2, saturating almost immediately on the very next pass. Below the threshold,
	// RecomputeHeatmapCap leaves the account's current cap alone rather than deriving one from
	// too little data.
	minTouchedTilesForAdaptiveCap = 20

	// heatmapCapChangeThreshold: RecomputeHeatmapCap only reports a change when the newly
	// computed cap differs from the stored one by more than this fraction. Changing the cap
	// means re-rendering every one of the account's heatmap tiles (it's baked into the stored
	// PNG, not applied at request time) — without a threshold, a still-growing account's cap
	// would nudge on nearly every sweep, forcing a near-constant full re-render for a change
	// too small to see.
	heatmapCapChangeThreshold = 0.15
)

// RecomputeHeatmapCap computes what users.heatmap_cap *should* be for one account, from that
// account's own coverage, without changing anything — persisting the result and marking tiles
// dirty is the caller's job (internal/worker/heatmap_cap.go), keeping this a pure "what should
// the cap be" query, not a side-effecting sweep.
//
// The statistic: the single most-touched z14 tile's count — how many currently-in-window,
// non-superseded activities cross it. A percentile was tried first and rejected: live against
// the Demo Customer account (1,140 distinct touched tiles, 1,100 of them touched exactly
// once), any percentile at or below ~99.7th still lands inside that single-touch mass, since
// the genuinely hot tiles (591 and 498 touches) are under 0.2% of the touched-tile population
// — nowhere near reachable by a percentile in the 90th-95th range the way a less extreme
// distribution would allow. The max is simpler and hits the actual target directly: "the
// single most-used spot" is the account's own stated problem (this file's caller doc), so it's
// the one tile that should reach exactly full saturation only at its true busiest, with
// everything else scaled relative to it. It's also harder to spoof than it looks — the count
// is COUNT(DISTINCT activity_id), so a single activity dwelling or jittering in place can't
// inflate it; only genuinely many separate activities crossing the same tile can.
//
// Computed over tiles, not pixels — a GROUP BY over activity_tile_masks is cheap, where
// per-pixel intensity statistics across a whole account's coverage would cost far more for
// marginal extra accuracy. Scoped to in_heatmap_window activities specifically (not all-time,
// the way Fog's own aggregate is) because that's exactly the population compositeHeatmapMask
// sums when it builds the raster this cap normalizes — calibrating against a wider all-time
// set would drift out of sync with what's actually rendered as activities age out of the
// window over time.
func RecomputeHeatmapCap(ctx context.Context, pool *pgxpool.Pool, userID string) (newCap float64, changed bool, err error) {
	var currentCap float64
	if err := pool.QueryRow(ctx, `SELECT heatmap_cap FROM users WHERE id = $1`, userID).Scan(&currentCap); err != nil {
		return 0, false, fmt.Errorf("fog: load current heatmap cap: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT COUNT(DISTINCT m.activity_id)
		FROM activity_tile_masks m
		JOIN activities a ON a.id = m.activity_id
		WHERE a.user_id = $1 AND a.superseded_by IS NULL
		  AND a.in_heatmap_window AND m.zoom = $2
		GROUP BY m.tile_x, m.tile_y
	`, userID, Zoom)
	if err != nil {
		return 0, false, fmt.Errorf("fog: query tile touch counts: %w", err)
	}
	var counts []int
	for rows.Next() {
		var c int
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return 0, false, fmt.Errorf("fog: scan touch count: %w", err)
		}
		counts = append(counts, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("fog: rows: %w", err)
	}

	if len(counts) < minTouchedTilesForAdaptiveCap {
		return currentCap, false, nil
	}

	candidate := math.Max(maxCount(counts), minHeatmapCap)
	if math.Abs(candidate-currentCap)/currentCap < heatmapCapChangeThreshold {
		return currentCap, false, nil
	}
	return candidate, true, nil
}

// maxCount is the largest of counts, which is not modified.
func maxCount(counts []int) float64 {
	max := counts[0]
	for _, c := range counts[1:] {
		if c > max {
			max = c
		}
	}
	return float64(max)
}
