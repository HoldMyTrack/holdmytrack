// Package parse implements the three Path-3 file formats (IMPLEMENTATION.md
// §4.0/§4.1 step 2), each streaming rather than buffering a whole file into memory.
package parse

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Point is one raw sample. Fields are pointers so "not present in this format/record" is
// distinguishable from zero.
type Point struct {
	Lat       float64
	Lon       float64
	Elevation *float32
	Time      time.Time
	HeartRate *int16
	Cadence   *int16
	PowerW    *int16
}

// Activity is the parsed result, before privacy trimming or simplification (§4.1 steps 3, 5).
type Activity struct {
	ActivityType string // 'run' | 'ride' | 'hike' | ... — best-effort from the source file
	Points       []Point
}

// ByExtension dispatches on the uploaded filename's extension and streams from r — callers
// must not read r into memory first (§5.2: a 100-mile ride at 1 Hz is 36,000+ points).
func ByExtension(filename string, r io.Reader) (Activity, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".gpx":
		return ParseGPX(r)
	case ".tcx":
		return ParseTCX(r)
	case ".fit":
		return ParseFIT(r)
	default:
		return Activity{}, fmt.Errorf("parse: unrecognized extension %q", filepath.Ext(filename))
	}
}
