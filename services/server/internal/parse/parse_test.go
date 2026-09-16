package parse

import (
	"strings"
	"testing"
)

const sampleGPX = `<?xml version="1.0"?>
<gpx><trk><trkseg>
<trkpt lat="39.9612" lon="-82.9988"><ele>240.1</ele><time>2026-01-01T12:00:00Z</time></trkpt>
<trkpt lat="39.9620" lon="-82.9990"><ele>241.0</ele><time>2026-01-01T12:00:10Z</time></trkpt>
<trkpt lat="39.9630" lon="-82.9995"><ele>242.5</ele><time>2026-01-01T12:00:20Z</time></trkpt>
</trkseg></trk></gpx>`

func TestParseGPX(t *testing.T) {
	act, err := ParseGPX(strings.NewReader(sampleGPX))
	if err != nil {
		t.Fatalf("ParseGPX: %v", err)
	}
	if len(act.Points) != 3 {
		t.Fatalf("want 3 points, got %d", len(act.Points))
	}
	p := act.Points[0]
	if p.Lat != 39.9612 || p.Lon != -82.9988 {
		t.Fatalf("point 0 lat/lon wrong: %+v", p)
	}
	if p.Elevation == nil || *p.Elevation != 240.1 {
		t.Fatalf("point 0 elevation wrong: %+v", p)
	}
	if p.Time.Unix() != 1767268800 {
		t.Fatalf("point 0 time wrong: %v (unix %d)", p.Time, p.Time.Unix())
	}
	if act.ActivityType != "unknown" {
		t.Fatalf("want activity type unknown (no <type> in this fixture), got %q", act.ActivityType)
	}
}

const sampleGPXWithType = `<?xml version="1.0"?>
<gpx><trk><type>Gravel Riding</type><trkseg>
<trkpt lat="39.9612" lon="-82.9988"><time>2026-01-01T12:00:00Z</time></trkpt>
<trkpt lat="39.9620" lon="-82.9990"><time>2026-01-01T12:00:10Z</time></trkpt>
</trkseg></trk></gpx>`

func TestParseGPXType(t *testing.T) {
	act, err := ParseGPX(strings.NewReader(sampleGPXWithType))
	if err != nil {
		t.Fatalf("ParseGPX: %v", err)
	}
	if act.ActivityType != "gravel riding" {
		t.Fatalf("want activity type %q, got %q", "gravel riding", act.ActivityType)
	}
	if len(act.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(act.Points))
	}
}

const sampleGPXWithExtensions = `<?xml version="1.0"?>
<gpx xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1"><trk><trkseg>
<trkpt lat="39.9612" lon="-82.9988"><time>2026-01-01T12:00:00Z</time><extensions>
  <gpxtpx:TrackPointExtension><gpxtpx:hr>140</gpxtpx:hr><gpxtpx:cad>82</gpxtpx:cad></gpxtpx:TrackPointExtension>
</extensions></trkpt>
<trkpt lat="39.9620" lon="-82.9990"><time>2026-01-01T12:00:10Z</time><extensions>
  <gpxtpx:TrackPointExtension><gpxtpx:hr>142</gpxtpx:hr><gpxtpx:cad>84</gpxtpx:cad></gpxtpx:TrackPointExtension>
</extensions></trkpt>
</trkseg></trk></gpx>`

func TestParseGPXWithExtensions(t *testing.T) {
	act, err := ParseGPX(strings.NewReader(sampleGPXWithExtensions))
	if err != nil {
		t.Fatalf("ParseGPX: %v", err)
	}
	if len(act.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(act.Points))
	}
	p := act.Points[0]
	if p.HeartRate == nil || *p.HeartRate != 140 {
		t.Fatalf("point 0 heart rate wrong: %+v", p)
	}
}

const sampleTCX = `<?xml version="1.0"?>
<TrainingCenterDatabase><Activities><Activity Sport="Running"><Lap><Track>
<Trackpoint>
  <Time>2026-01-01T12:00:00Z</Time>
  <Position><LatitudeDegrees>39.9612</LatitudeDegrees><LongitudeDegrees>-82.9988</LongitudeDegrees></Position>
  <AltitudeMeters>240.1</AltitudeMeters>
  <HeartRateBpm><Value>140</Value></HeartRateBpm>
</Trackpoint>
<Trackpoint>
  <Time>2026-01-01T12:00:10Z</Time>
  <Position><LatitudeDegrees>39.9620</LatitudeDegrees><LongitudeDegrees>-82.9990</LongitudeDegrees></Position>
  <AltitudeMeters>241.0</AltitudeMeters>
  <HeartRateBpm><Value>142</Value></HeartRateBpm>
</Trackpoint>
</Track></Lap></Activity></Activities></TrainingCenterDatabase>`

func TestParseTCX(t *testing.T) {
	act, err := ParseTCX(strings.NewReader(sampleTCX))
	if err != nil {
		t.Fatalf("ParseTCX: %v", err)
	}
	if act.ActivityType != "running" {
		t.Fatalf("want activity type running, got %q", act.ActivityType)
	}
	if len(act.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(act.Points))
	}
	p := act.Points[0]
	if p.Lat != 39.9612 || p.Lon != -82.9988 {
		t.Fatalf("point 0 lat/lon wrong: %+v", p)
	}
	if p.HeartRate == nil || *p.HeartRate != 140 {
		t.Fatalf("point 0 heart rate wrong: %+v", p)
	}
}

const sampleSyncJSON = `{
  "activity_type": "run",
  "points": [
    {"lat": 39.9612, "lon": -82.9988, "elevation_m": 240.1, "time": "2026-01-01T12:00:00Z", "heart_rate": 140},
    {"lat": 39.9620, "lon": -82.9990, "time": "2026-01-01T12:00:10Z"}
  ]
}`

func TestParseJSON(t *testing.T) {
	act, err := ParseJSON(strings.NewReader(sampleSyncJSON))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if act.ActivityType != "run" {
		t.Fatalf("want activity type run, got %q", act.ActivityType)
	}
	if len(act.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(act.Points))
	}
	p := act.Points[0]
	if p.Lat != 39.9612 || p.Lon != -82.9988 {
		t.Fatalf("point 0 lat/lon wrong: %+v", p)
	}
	if p.Elevation == nil || *p.Elevation != 240.1 {
		t.Fatalf("point 0 elevation wrong: %+v", p)
	}
	if p.HeartRate == nil || *p.HeartRate != 140 {
		t.Fatalf("point 0 heart rate wrong: %+v", p)
	}
	if act.Points[1].HeartRate != nil {
		t.Fatalf("point 1 heart rate should be absent, got %+v", act.Points[1].HeartRate)
	}
}

func TestParseJSONNoType(t *testing.T) {
	act, err := ParseJSON(strings.NewReader(`{"points": [{"lat": 1, "lon": 2, "time": "2026-01-01T12:00:00Z"}]}`))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if act.ActivityType != "unknown" {
		t.Fatalf("want activity type unknown (no activity_type in this fixture), got %q", act.ActivityType)
	}
}

func TestByExtensionJSON(t *testing.T) {
	act, err := ByExtension("activity.json", strings.NewReader(sampleSyncJSON))
	if err != nil {
		t.Fatalf("ByExtension: %v", err)
	}
	if len(act.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(act.Points))
	}
}
