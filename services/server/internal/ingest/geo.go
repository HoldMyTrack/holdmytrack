package ingest

import "math"

const earthRadiusM = 6371008.8

// HaversineM returns the great-circle distance between two points in meters. Exported so
// internal/httpapi can reuse it for the track-metrics endpoint's per-vertex speed
// computation, rather than a second copy of the same math.
func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
