// Package parse implements the three Path-3 file formats (IMPLEMENTATION.md
// §4.0/§4.1 step 2), each streaming rather than buffering a whole file into memory.
package parse

import (
	"encoding/json"
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
}

// Activity is the parsed result, before privacy trimming or simplification (§4.1 steps 3, 5).
type Activity struct {
	ActivityType string // 'run' | 'ride' | 'hike' | ... — best-effort from the source file
	Points       []Point
	// Name and Description are empty for every file-format parser (GPX/TCX/FIT never carry a
	// title FitMap trusts here) and for Health Connect/HealthKit sync. Path 2's JSON wire
	// format is the one source that can set them directly (`docs/IMPLEMENTATION.md` §4.0.4,
	// in-app GPS recording) — ingest.Process persists them at creation when non-empty, the
	// same NULLIF-on-empty convention `handleUpdateActivity` uses for an edit after the fact.
	Name        string
	Description string
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
	case ".json":
		return ParseJSON(r)
	default:
		return Activity{}, fmt.Errorf("parse: unrecognized extension %q", filepath.Ext(filename))
	}
}

// JSONPoint is one sample of Path 2's batched normalized-points wire format
// (`POST /v1/sync/activities`, IMPLEMENTATION.md §4.0.3) — exported so
// internal/httpapi can decode the incoming request body straight into the same shape this
// package re-marshals as the raw payload, with no separate wire type to keep in sync.
type JSONPoint struct {
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	ElevationM *float32  `json:"elevation_m,omitempty"`
	Time       time.Time `json:"time"`
	HeartRate  *int16    `json:"heart_rate,omitempty"`
}

// JSONActivity is one activity's worth of JSONPoint, the unit both the sync request body and
// the stored raw payload carry (`internal/httpapi.syncActivityRequest` embeds this).
//
// Name and Description are optional and empty for Health Connect/HealthKit sync, which never
// sends them. In-app GPS recording (`source = "recorded"`, `docs/IMPLEMENTATION.md` §4.0.4)
// sets them from its own Name/Description fields, captured before or during a recording, so
// the resulting activity carries them from the moment it's created rather than needing a
// separate edit afterward.
type JSONActivity struct {
	ActivityType string      `json:"activity_type"`
	Points       []JSONPoint `json:"points"`
	Name         string      `json:"name,omitempty"`
	Description  string      `json:"description,omitempty"`
}

// ParseJSON decodes Path 2's normalized-point wire format into the same Activity shape
// GPX/TCX/FIT parsing produces, so ingest.Process needs no Path-2-specific branch — the
// on-device app has already read raw samples out of the platform health store itself, so
// there is no format-specific parsing left to do here, only a field-for-field reshape.
func ParseJSON(r io.Reader) (Activity, error) {
	var a JSONActivity
	if err := json.NewDecoder(r).Decode(&a); err != nil {
		return Activity{}, fmt.Errorf("parse: decode json: %w", err)
	}
	points := make([]Point, len(a.Points))
	for i, p := range a.Points {
		points[i] = Point{
			Lat: p.Lat, Lon: p.Lon, Elevation: p.ElevationM, Time: p.Time,
			HeartRate: p.HeartRate,
		}
	}
	activityType := a.ActivityType
	if activityType == "" {
		activityType = "unknown"
	}
	return Activity{ActivityType: activityType, Points: points, Name: a.Name, Description: a.Description}, nil
}
