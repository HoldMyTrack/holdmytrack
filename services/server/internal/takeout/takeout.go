// Package takeout reads GPS activities out of a Google Health / Fitbit Takeout export.
//
// A Go port of the Takeout reader in pathify 1.4.0 (github.com/np25071984/pathify,
// crates/pathify-core/src/takeout), which the upload handler used to run as a subprocess.
// Only what this backend needs came across: reading the zip already in memory, the join
// below, and writing each activity as the same GPX pathify wrote, byte for byte, so an
// export imported before the port and re-imported after it still dedupes on its content
// hash (testdata/ holds pathify's own output to hold that to).
//
// The export keeps two things apart that are only useful together:
//
//   - `gps_location_YYYY-MM-DD.csv` — every fix of one UTC calendar day, from every device
//     that was recording, interleaved. A day file is not an activity: it can hold three
//     unrelated outings, and a phone and a watch recording the same walk.
//   - `exercise-N.json` — the activity logs, which say a particular stretch of a day was a
//     walk. Their `startTime` is a wall clock with no offset (see detectOffset).
//
// So an activity is its log's window sliced out of the day files it touches, keeping one
// device's recording (chooseSource). Nothing else in the archive is read: an export also
// holds sleep, heart rate and menstrual health data, and only these two file families are
// ever opened.
package takeout

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ErrNoHealthData means the zip holds neither exercise logs nor GPS day files — an export of
// some other Google product, which this reader has nothing to say about.
var ErrNoHealthData = errors.New("takeout: no Google Health exercise logs or GPS day files in this archive")

// Log is one exercise log: an activity the user or the tracker recorded.
type Log struct {
	ID int64
	// Fitbit's own name for the activity — `Walk`, `Outdoor Bike`.
	Name string
	// The start as the export writes it, a wall clock with no offset, held in a UTC
	// time.Time only as a container: it is not UTC until detectOffset says what it is.
	Start    time.Time
	Duration time.Duration
	// Whether the tracker says a GPS trace should exist for this log.
	HasGPS bool
}

// window is the log's span in real UTC, given how far the export's wall clock runs ahead of
// UTC.
func (l Log) window(offset time.Duration) (time.Time, time.Time) {
	start := l.Start.Add(-offset)
	return start, start.Add(l.Duration)
}

// startTimeLayout is how exercise logs spell `startTime`: `MM/DD/YY hh:mm:ss`.
const startTimeLayout = "01/02/06 15:04:05"

type rawLog struct {
	LogID        *int64  `json:"logId"`
	ActivityName *string `json:"activityName"`
	StartTime    *string `json:"startTime"`
	// Milliseconds of actual activity, which is what the GPS trace covers; `duration` also
	// counts time the recording was paused.
	ActiveDuration *int64 `json:"activeDuration"`
	Duration       *int64 `json:"duration"`
	HasGPS         bool   `json:"hasGps"`
	// `tcxLink` is deliberately not read: it's a fitbit.com URL, and everything needed is
	// already inside the archive.
}

func readLogs(data []byte) ([]Log, error) {
	var raw []rawLog
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("exercise log: %w", err)
	}
	logs := make([]Log, 0, len(raw))
	for i, r := range raw {
		if r.LogID == nil || r.ActivityName == nil || r.StartTime == nil {
			return nil, fmt.Errorf("exercise log entry %d: missing logId, activityName or startTime", i)
		}
		start, err := time.Parse(startTimeLayout, *r.StartTime)
		if err != nil {
			return nil, fmt.Errorf("exercise log %d: startTime %q is not in MM/DD/YY hh:mm:ss form", *r.LogID, *r.StartTime)
		}
		ms := int64(0)
		if r.ActiveDuration != nil {
			ms = *r.ActiveDuration
		} else if r.Duration != nil {
			ms = *r.Duration
		}
		logs = append(logs, Log{
			ID:       *r.LogID,
			Name:     *r.ActivityName,
			Start:    start,
			Duration: time.Duration(max(ms, 0)) * time.Millisecond,
			HasGPS:   r.HasGPS,
		})
	}
	return logs, nil
}

// fix is one row of a GPS day file.
type fix struct {
	time     time.Time
	lat, lon float64
	ele      *float64
	// The `data source` column, e.g. `Google Pixel Watch 3`.
	source string
}

// readDay parses one day file, sorted by time so every window landing in it can be found by
// bisection.
func readDay(data []byte, day string) ([]fix, error) {
	dayErr := func(format string, args ...any) error {
		return fmt.Errorf("gps_location_%s.csv: %s", day, fmt.Sprintf(format, args...))
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	header, err := r.Read()
	if err != nil {
		return nil, dayErr("%v", err)
	}

	lat, lon, ele, tm, src := -1, -1, -1, -1, -1
	setOnce := func(slot *int, i int) {
		if *slot < 0 {
			*slot = i
		}
	}
	for i, h := range header {
		normalized := strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(h)))
		switch normalized {
		case "data_source", "source":
			setOnce(&src, i)
		case "lat", "latitude", "y":
			setOnce(&lat, i)
		case "lon", "lng", "long", "longitude", "x":
			setOnce(&lon, i)
		case "ele", "elevation", "altitude", "alt":
			setOnce(&ele, i)
		case "time", "timestamp", "datetime":
			setOnce(&tm, i)
		}
	}
	if lat < 0 || lon < 0 || tm < 0 {
		return nil, dayErr("expected timestamp, latitude and longitude columns, found [%s]", strings.Join(header, ", "))
	}

	var fixes []fix
	for line := 2; ; line++ { // the header is line 1
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, dayErr("%v", err)
		}
		cell := func(i int) string {
			if i < 0 || i >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[i])
		}
		number := func(i int, column string) (float64, error) {
			s := cell(i)
			if s == "" {
				return 0, dayErr("line %d: no value for `%s`", line, column)
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, dayErr("line %d: `%s` is not a number", line, column)
			}
			return v, nil
		}

		stamp := cell(tm)
		if stamp == "" {
			return nil, dayErr("line %d: no value for `timestamp`", line)
		}
		recorded, err := parseRFC3339(stamp)
		if err != nil {
			return nil, dayErr("line %d: `%s` is not an RFC 3339 time", line, stamp)
		}
		la, err := number(lat, "latitude")
		if err != nil {
			return nil, err
		}
		lo, err := number(lon, "longitude")
		if err != nil {
			return nil, err
		}
		if math.IsInf(la, 0) || math.IsNaN(la) || math.IsInf(lo, 0) || math.IsNaN(lo) {
			return nil, dayErr("line %d: latitude and longitude must be finite", line)
		}
		if la < -90 || la > 90 {
			return nil, dayErr("line %d: latitude %v is outside -90..=90", line, la)
		}

		f := fix{time: recorded, lat: la, lon: normalizeLon(lo), source: "unknown"}
		if e, err := strconv.ParseFloat(cell(ele), 64); err == nil && !math.IsInf(e, 0) && !math.IsNaN(e) {
			f.ele = &e
		}
		if s := cell(src); s != "" {
			f.source = s
		}
		fixes = append(fixes, f)
	}
	sort.SliceStable(fixes, func(i, j int) bool { return fixes[i].time.Before(fixes[j].time) })
	return fixes, nil
}

// parseRFC3339 accepts what the day files write (`…:17Z` and `…:17.328Z` alike), plus the
// lowercase and space-separated spellings RFC 3339 also allows.
func parseRFC3339(s string) (time.Time, error) {
	if len(s) > 10 && (s[10] == ' ' || s[10] == 't') {
		s = s[:10] + "T" + s[11:]
	}
	if strings.HasSuffix(s, "z") {
		s = s[:len(s)-1] + "Z"
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// normalizeLon wraps a longitude into -180..=180, with the same arithmetic pathify uses
// (Rust's rem_euclid), so a coordinate writes back out as exactly the same digits.
func normalizeLon(lon float64) float64 {
	r := math.Mod(lon+180, 360)
	if r < 0 {
		r += 360
	}
	wrapped := r - 180
	if wrapped == -180 && lon > 0 {
		return 180
	}
	return wrapped
}

// Archive is one Takeout export, opened: its exercise logs parsed, its day files indexed but
// not yet decompressed (each is megabytes, and only the days an activity touches are read).
type Archive struct {
	logs []Log
	days map[string]*zip.File // keyed by "YYYY-MM-DD"
}

// Open indexes a Takeout export already held as a zip.
//
// One zip only: Google splits a large export into `…-001.zip`, `…-002.zip`, and the exercise
// logs can land in a different part from the GPS days. Stitching parts isn't built — the
// upload endpoint takes one file per request.
func Open(zr *zip.Reader) (*Archive, error) {
	files := make([]*zip.File, 0, len(zr.File))
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/") {
			files = append(files, f)
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	a := &Archive{days: make(map[string]*zip.File)}
	found := false
	for _, f := range files {
		base := f.Name[strings.LastIndex(f.Name, "/")+1:]
		if isExerciseLog(base) {
			found = true
			data, err := readEntry(f)
			if err != nil {
				return nil, err
			}
			logs, err := readLogs(data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			a.logs = append(a.logs, logs...)
		} else if day, ok := gpsDay(base); ok {
			found = true
			a.days[day] = f
		}
	}
	if !found {
		return nil, ErrNoHealthData
	}
	sort.SliceStable(a.logs, func(i, j int) bool {
		if !a.logs[i].Start.Equal(a.logs[j].Start) {
			return a.logs[i].Start.Before(a.logs[j].Start)
		}
		return a.logs[i].ID < a.logs[j].ID
	})
	return a, nil
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	return data, nil
}

// isExerciseLog matches on the file name, not the folder: the folder is a Takeout product
// name that has been renamed before.
func isExerciseLog(name string) bool {
	number, ok := strings.CutPrefix(name, "exercise-")
	if !ok {
		return false
	}
	number, ok = strings.CutSuffix(number, ".json")
	if !ok || number == "" {
		return false
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// gpsDay is the calendar day a `gps_location_YYYY-MM-DD.csv` covers. The export also ships a
// `gps_location_readme.txt`, which this correctly doesn't match.
func gpsDay(name string) (string, bool) {
	day, ok := strings.CutPrefix(name, "gps_location_")
	if !ok {
		return "", false
	}
	day, ok = strings.CutSuffix(day, ".csv")
	if !ok {
		return "", false
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return "", false
	}
	return day, true
}

// Type is one activity type and how many of its logs claim a GPS trace.
type Type struct {
	Name       string
	WithGPS    int
	WithoutGPS int
}

// Types lists the export's activity types, the ones with GPS first — including types with
// none, so a caller can report them rather than silently drop them.
func (a *Archive) Types() []Type {
	var types []Type
	index := make(map[string]int)
	for _, l := range a.logs {
		i, ok := index[l.Name]
		if !ok {
			i = len(types)
			index[l.Name] = i
			types = append(types, Type{Name: l.Name})
		}
		if l.HasGPS {
			types[i].WithGPS++
		} else {
			types[i].WithoutGPS++
		}
	}
	sort.SliceStable(types, func(i, j int) bool {
		a, b := types[i], types[j]
		if a.WithGPS != b.WithGPS {
			return a.WithGPS > b.WithGPS
		}
		if at, bt := a.WithGPS+a.WithoutGPS, b.WithGPS+b.WithoutGPS; at != bt {
			return at > bt
		}
		return a.Name < b.Name
	})
	return types
}

// fold normalizes an activity name for matching, so `Outdoor Bike` and `outdoor_bike` are the
// same request.
func fold(name string) string {
	var b strings.Builder
	for _, c := range name {
		if unicode.IsSpace(c) || c == '_' || c == '-' {
			continue
		}
		b.WriteString(strings.ToLower(string(c)))
	}
	return b.String()
}
