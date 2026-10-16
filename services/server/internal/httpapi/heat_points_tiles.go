package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/platform/services/server/internal/budget"
	"github.com/HoldMyTrack/platform/services/server/internal/fog"
)

// heatCellsQuery is the account's ($1) zoomed-out cells in a heat-points tile: how many
// in-window, non-Pending activities pass through each, from their masks' rows (§3.11), with the
// zoom's scale — the account's heatmap_cap and its busiest cell at zoom $7 — on every row, so a
// tile is one query. $2 is how far the z14 tile numbers shift to give a cell's, $3..$6 the
// tile's z14 tiles. It starts from
// the account's own activities, materialized, and reaches their masks by the primary key, which
// leads with the activity: left to itself the planner, unable to estimate a shift, scanned every
// account's masks.
const heatCellsQuery = `
	WITH acts AS MATERIALIZED (
		SELECT id FROM activities WHERE user_id = $1 AND in_heatmap_window AND NOT edit_pending
	), cells AS (
		SELECT m.tile_x >> $2::int AS cx, m.tile_y >> $2::int AS cy, count(DISTINCT m.activity_id) AS n
		FROM acts JOIN activity_tile_masks m ON m.activity_id = acts.id AND m.zoom = 14
		WHERE m.tile_x >= $3 AND m.tile_x < $4 AND m.tile_y >= $5 AND m.tile_y < $6
		GROUP BY 1, 2
	)
	SELECT c.cx, c.cy, c.n, u.heatmap_cap, u.heat_point_caps[$7::int + 1]
	FROM cells c CROSS JOIN users u
	WHERE u.id = $1`

// handleHeatPointsTile serves the Heatmap (IMPLEMENTATION.md §4.2.2): weighted points the
// clients draw as MapLibre heatmap circles, at every zoom. Up to fog.HeatActivityMaxZoom a cell
// is scored by the activities through it, read live from their masks' rows; past it by its
// busiest spot, from the stored count tile one zoom up. Either way the weights are worked out
// per request, so neither a new scale nor a new look needs a re-render, and a tile is one
// database query, its vector tile encoded here (fog.EncodeHeatPoints).
func (s *Server) handleHeatPointsTile(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := tileCoords(w, r, ".mvt")
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	if z > fog.HeatPointsMaxZoom {
		setTileCacheControl(w, r)
		return
	}
	var points []fog.HeatPoint
	var err error
	if z <= fog.HeatActivityMaxZoom {
		points, err = s.heatCellPoints(r.Context(), z, x, y)
	} else {
		// Only these read the store, so only these stop with the day's reads (ADR-0044); the
		// zoomed-out tiles are the database's alone and keep working.
		if s.budget.Stage(r.Context()) >= budget.ReadsPaused {
			w.Header().Del("Content-Type")
			w.Header().Set("Retry-After", strconv.Itoa(int(budget.UntilReset(time.Now()).Seconds())+1))
			httpErrorT(w, r, http.StatusServiceUnavailable, "error.daily_capacity")
			return
		}
		points, err = s.heatCountPoints(r.Context(), z, x, y)
	}
	if err != nil {
		s.heatPointsFailed(w, r, err, z, x, y)
		return
	}
	setTileCacheControl(w, r)
	_, _ = w.Write(fog.EncodeHeatPoints(points))
}

// heatCellPoints is a zoomed-out tile's points: its cells' activities, against the zoom's own
// busiest cell.
func (s *Server) heatCellPoints(ctx context.Context, z, x, y int) ([]fog.HeatPoint, error) {
	span := 1 << (fog.Zoom - z) // the tile's width in z14 tiles
	rows, err := s.pool.Query(ctx, heatCellsQuery, userIDFromContext(ctx), fog.HeatActivityMaxZoom-z,
		x*span, (x+1)*span, y*span, (y+1)*span, z)
	if err != nil {
		return nil, fmt.Errorf("cells: %w", err)
	}
	defer rows.Close()
	var cells []fog.HeatCell
	var heatmapCap float64
	var zoomCap *int
	for rows.Next() {
		var c fog.HeatCell
		if err := rows.Scan(&c.CX, &c.CY, &c.N, &heatmapCap, &zoomCap); err != nil {
			return nil, fmt.Errorf("cells: %w", err)
		}
		cells = append(cells, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cells: %w", err)
	}
	stored := 0
	if zoomCap != nil {
		stored = *zoomCap
	}
	return fog.HeatCellPoints(cells, x, y, fog.HeatScale(stored, heatmapCap)), nil
}

// heatCountPoints is a close-in tile's points: its cells' busiest pixels, from the stored count
// tile one zoom up, against the account's heatmap_cap.
func (s *Server) heatCountPoints(ctx context.Context, z, x, y int) ([]fog.HeatPoint, error) {
	sz, sx, sy := fog.HeatPointsSource(z, x, y)
	var objectKey *string
	var heatmapCap float64
	err := s.pool.QueryRow(ctx, `
		SELECT f.heatmap_object_key, u.heatmap_cap
		FROM users u LEFT JOIN fog_tiles f
		  ON f.user_id = u.id AND f.zoom = $2 AND f.tile_x = $3 AND f.tile_y = $4
		WHERE u.id = $1`,
		userIDFromContext(ctx), sz, sx, sy,
	).Scan(&objectKey, &heatmapCap)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("row: %w", err)
	}
	stored, err := loadMaskPNG(ctx, s.store, objectKey)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	counts, err := fog.DecodeCounts(stored)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return fog.HeatPoints(counts, x, y, fog.HeatScale(0, heatmapCap)), nil
}

func (s *Server) heatPointsFailed(w http.ResponseWriter, r *http.Request, err error, z, x, y int) {
	if clientGone(w, r) {
		return
	}
	s.log.Error("heat points tile failed", "err", err, "z", z, "x", x, "y", y)
	writeAPIError(w, "internal error", http.StatusInternalServerError)
}
