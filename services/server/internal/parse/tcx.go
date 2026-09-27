package parse

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ParseTCX streams Trackpoint elements. Garmin Training Center XML nests position and
// altitude under a Trackpoint — heart rate too, which is deliberately not read (VISION.md §1.1).
func ParseTCX(r io.Reader) (Activity, error) {
	dec := xml.NewDecoder(r)
	act := Activity{ActivityType: "unknown"}

	var cur *Point
	// path tracks nesting, so an element is only read inside a Trackpoint.
	var path []string

	inTrackpoint := func() bool {
		for _, p := range path {
			if p == "Trackpoint" {
				return true
			}
		}
		return false
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Activity{}, fmt.Errorf("parse tcx: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			path = append(path, t.Name.Local)
			switch t.Name.Local {
			case "Activity":
				for _, a := range t.Attr {
					if a.Name.Local == "Sport" {
						act.ActivityType = strings.ToLower(a.Value)
					}
				}
			case "Trackpoint":
				cur = &Point{}
			}
		case xml.CharData:
			if cur == nil || !inTrackpoint() {
				continue
			}
			text := strings.TrimSpace(string(t))
			if text == "" || len(path) == 0 {
				continue
			}
			leaf := path[len(path)-1]
			switch leaf {
			case "Time":
				if ts, err := time.Parse(time.RFC3339, text); err == nil {
					cur.Time = ts
				}
			case "LatitudeDegrees":
				cur.Lat, _ = strconv.ParseFloat(text, 64)
			case "LongitudeDegrees":
				cur.Lon, _ = strconv.ParseFloat(text, 64)
			case "AltitudeMeters":
				if v, err := strconv.ParseFloat(text, 32); err == nil {
					f := float32(v)
					cur.Elevation = &f
				}
			}
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			if t.Name.Local == "Trackpoint" && cur != nil {
				if cur.Lat != 0 || cur.Lon != 0 {
					act.Points = append(act.Points, *cur)
				}
				cur = nil
			}
		}
	}

	if len(act.Points) == 0 {
		return Activity{}, fmt.Errorf("parse tcx: %w", ErrNoPoints)
	}
	return act, nil
}
