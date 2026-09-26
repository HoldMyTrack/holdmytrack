package ingest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

func TestFailureCode(t *testing.T) {
	// parsed runs a real parser, wrapped the way loadClippedPoints wraps its error.
	parsed := func(filename, body string) error {
		_, err := parse.ByExtension(filename, strings.NewReader(body))
		if err == nil {
			t.Fatalf("%s: parse succeeded, want an error", filename)
		}
		return parseError{err}
	}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unknown extension", parsed("ride.kml", "<kml/>"), FailUnsupportedFormat},
		{"malformed GPX", parsed("ride.gpx", "<gpx><trk><trkseg><trkpt lat="), FailUnreadableFile},
		{"GPX with no points", parsed("ride.gpx", "<gpx><trk><trkseg></trkseg></trk></gpx>"), FailNoTrack},
		{"FIT with a bad signature", parsed("ride.fit", strings.Repeat("\x00", 14)), FailUnreadableFile},
		{"no timestamps", errNoTimestamps, FailNoTimestamps},
		{"too few points", fmt.Errorf("ingest: %w (%d)", errTooFewPoints, 1), FailTooFewPoints},
		{"a later pipeline step", fmt.Errorf("ingest: persist activity: %w", errors.New("connection reset")), FailInternal},
	}
	for _, c := range cases {
		if got := FailureCode(c.err); got != c.want {
			t.Errorf("%s: FailureCode(%v) = %q, want %q", c.name, c.err, got, c.want)
		}
	}
}
