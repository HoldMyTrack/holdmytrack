package export

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"encoding/xml"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// WriteGPX writes points as a one-segment GPX 1.1 track with everything parse.ParseGPX reads
// back — position, elevation and time — and the activity's type, and nothing else: no creator
// device, no author metadata. Coordinates and elevation keep their full precision, so a
// re-ingest derives the same metrics and trajectory. The demo export (httpapi's
// ExportDemoActivities) and a user's archive (Build) both write their tracks with it.
func WriteGPX(w io.Writer, activityType string, points []parse.Point) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="HoldMyTrack" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	b.WriteString("  <trk>\n    <type>")
	xml.EscapeText(&b, []byte(activityType))
	b.WriteString("</type>\n    <trkseg>\n")
	for _, p := range points {
		fmt.Fprintf(&b, `      <trkpt lat="%s" lon="%s">`,
			strconv.FormatFloat(p.Lat, 'f', -1, 64), strconv.FormatFloat(parse.WrapLon(p.Lon), 'f', -1, 64))
		if p.Elevation != nil {
			fmt.Fprintf(&b, "<ele>%s</ele>", strconv.FormatFloat(float64(*p.Elevation), 'f', -1, 32))
		}
		fmt.Fprintf(&b, "<time>%s</time>", p.Time.UTC().Format(time.RFC3339Nano))
		b.WriteString("</trkpt>\n")
	}
	b.WriteString("    </trkseg>\n  </trk>\n</gpx>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

var slugUnsafe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Slug keeps a name readable as a filename: letters and digits, everything else collapsed to
// single spaces, bounded in length.
func Slug(s string) string {
	s = strings.TrimSpace(slugUnsafe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 60 {
		s = strings.TrimSpace(string(r[:60]))
	}
	if s == "" {
		return "Activity"
	}
	return s
}
