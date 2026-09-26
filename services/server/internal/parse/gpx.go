package parse

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ParseGPX streams trkpt elements with a token-based decoder rather than xml.Unmarshal into
// a whole-document struct, so a large track never lives in memory as a parsed DOM either.
func ParseGPX(r io.Reader) (Activity, error) {
	dec := xml.NewDecoder(r)
	act := Activity{ActivityType: "unknown"}

	var cur *Point
	var curField string // which child element of the current trkpt we're inside, if any
	var inType bool     // inside <trk><type> — a sibling of trkseg, not a per-point field

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Activity{}, fmt.Errorf("parse gpx: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "trkpt", "wpt", "rtept":
				p := Point{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "lat":
						p.Lat, _ = strconv.ParseFloat(a.Value, 64)
					case "lon":
						p.Lon, _ = strconv.ParseFloat(a.Value, 64)
					}
				}
				cur = &p
			case "ele", "time", "hr":
				// hr is gpxtpx:TrackPointExtension's standard name. The decoder strips
				// namespace prefixes (Name.Local), so no prefix matching is needed — and
				// none of these names appear anywhere else inside a trkpt.
				if cur != nil {
					curField = t.Name.Local
				}
			case "type":
				// The optional GPX track type ("Hiking", "Running", ...) — IMPLEMENTATION.md
				// §4.7: activity_type is whatever the source reports, not a controlled
				// vocabulary. Only meaningful outside a point (cur == nil); some extension
				// schemas add their own per-point "type"-like fields this isn't after.
				if cur == nil {
					inType = true
				}
			}
		case xml.CharData:
			if inType {
				if v := strings.TrimSpace(string(t)); v != "" {
					act.ActivityType = strings.ToLower(v)
				}
				continue
			}
			if cur == nil || curField == "" {
				continue
			}
			text := string(t)
			switch curField {
			case "ele":
				if v, err := strconv.ParseFloat(text, 32); err == nil {
					f := float32(v)
					cur.Elevation = &f
				}
			case "time":
				if ts, err := time.Parse(time.RFC3339, text); err == nil {
					cur.Time = ts
				}
			case "hr":
				if v, err := strconv.Atoi(text); err == nil {
					hr := int16(v)
					cur.HeartRate = &hr
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "ele", "time", "hr":
				curField = ""
			case "type":
				inType = false
			case "trkpt", "wpt", "rtept":
				if cur != nil {
					act.Points = append(act.Points, *cur)
					cur = nil
				}
			}
		}
	}

	if len(act.Points) == 0 {
		return Activity{}, fmt.Errorf("parse gpx: %w", ErrNoPoints)
	}
	return act, nil
}
