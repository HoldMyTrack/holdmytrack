package export

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Morning Run":         "Morning Run",
		"  Lake / Loop #2!  ": "Lake Loop 2",
		"Прогулка у озера":    "Прогулка у озера",
		"???":                 "Activity",
		"":                    "Activity",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// A written GPX must parse back to exactly the points it was written from, or a re-ingest
// would drift from what the owner sees.
func TestWriteGPXRoundTrip(t *testing.T) {
	ele := float32(187.4)
	t0 := time.Date(2026, 9, 26, 14, 3, 7, 250_000_000, time.UTC)
	points := []parse.Point{
		{Lat: 41.3213457891234, Lon: -81.6123456789012, Elevation: &ele, Time: t0},
		{Lat: 41.32141, Lon: -81.61229, Time: t0.Add(time.Second)},
	}
	var buf bytes.Buffer
	if err := WriteGPX(&buf, "hiking & walking", points); err != nil {
		t.Fatal(err)
	}
	act, err := parse.ParseGPX(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if act.ActivityType != "hiking & walking" {
		t.Errorf("type = %q", act.ActivityType)
	}
	if !reflect.DeepEqual(act.Points, points) {
		t.Fatalf("got %+v, want %+v", act.Points, points)
	}
}
