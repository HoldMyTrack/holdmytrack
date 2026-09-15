package ingest

import "github.com/fitmap/fitmap/services/server/internal/parse"

// TrimEndpoints drops points within trimM meters of walked distance from each end of the
// track — IMPLEMENTATION.md §7's endpoint trim, applied unconditionally
// (§7: "non-negotiable"). There's no privacy_zones data to clip against yet (no UI),
// so that half of §4.1 step 3 is a no-op for now; this half still applies.
//
// Exported so internal/fog can apply the exact same trim when re-parsing an activity's raw
// payload to render or re-render a tile (§4.2) — coverage must reflect the same privacy-
// clipped points the activity itself was persisted from, not the untrimmed raw file.
func TrimEndpoints(points []parse.Point, trimM int) []parse.Point {
	if trimM <= 0 || len(points) < 2 {
		return points
	}

	start := 0
	acc := 0.0
	for i := 1; i < len(points) && acc < float64(trimM); i++ {
		acc += HaversineM(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
		start = i
	}

	end := len(points) - 1
	acc = 0.0
	for i := len(points) - 1; i > start && acc < float64(trimM); i-- {
		acc += HaversineM(points[i].Lat, points[i].Lon, points[i-1].Lat, points[i-1].Lon)
		end = i - 1
	}

	if start >= end {
		// The whole track is shorter than 2x the trim radius. Keep the endpoints rather
		// than emit an empty activity — an over-aggressive trim on a short track is a
		// worse failure mode than under-trimming by a few meters.
		return points
	}
	return points[start : end+1]
}
