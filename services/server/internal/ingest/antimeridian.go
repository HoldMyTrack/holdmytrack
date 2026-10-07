package ingest

import (
	"math"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// unwrapLons returns points with each longitude within 180° of the one before, the first in
// −180…180: a track across the antimeridian (179.95, then −179.98) continues past 180
// (180.02) instead of jumping back across the whole world (IMPLEMENTATION.md §4.1). The stored
// trajectory keeps this form; what meets the world's −180…180 — tiles, masks, boundaries, a
// written GPX — wraps it back where it needs to. Idempotent, so it's safe after an edit that
// moved a point.
func unwrapLons(points []parse.Point) []parse.Point {
	if len(points) == 0 {
		return points
	}
	out := make([]parse.Point, len(points))
	copy(out, points)
	out[0].Lon = parse.WrapLon(out[0].Lon)
	for i := 1; i < len(out); i++ {
		// Whole turns of the world, so the recorded value comes back by wrapping it.
		lon := out[i].Lon
		out[i].Lon = lon + 360*math.Round((out[i-1].Lon-lon)/360)
	}
	return out
}
