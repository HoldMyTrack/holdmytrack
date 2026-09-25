package takeout

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

// segmentGap is how long a silence starts a new <trkseg> rather than a line drawn across it —
// pathify's default, which the GPX this writes has to keep to stay byte-identical.
const segmentGap = 120 * time.Second

// Activity is one exercise log's slice of the day files, as one GPX file.
type Activity struct {
	LogID int64
	// The activity type as the export names it — `Walk`, `Outdoor Bike`.
	Type string
	// When the activity started, in real UTC.
	Start time.Time
	// The recording device kept for it (chooseSource).
	Source string
	// Split wherever the recording went silent for longer than segmentGap.
	Segments [][]fix
}

// Extract pulls every activity of one type (matched case- and punctuation-insensitively, as
// Types names it) out of the export, in start order.
//
// One type at a time, not all at once, because the upload handler has always extracted per
// type and the wall-clock offset is detected from the activities being extracted: detecting it
// across a different selection could land on a different offset, and a different GPX.
func (a *Archive) Extract(activityType string) ([]Activity, error) {
	wanted := fold(activityType)
	var selected []Log
	for _, l := range a.logs {
		if l.HasGPS && fold(l.Name) == wanted {
			selected = append(selected, l)
		}
	}

	days := dayCache{archive: a, days: make(map[string][]fix)}
	offset, err := detectOffset(selected, &days)
	if err != nil {
		return nil, err
	}

	var activities []Activity
	for _, l := range selected {
		start, end := l.window(offset)
		// Only the days a window touches are decompressed, and nothing before this one is asked
		// for again (logs are in start order), so a year of walks isn't held in memory at once.
		days.forgetBefore(start.Format(time.DateOnly))

		var fixes []fix
		for _, day := range daysTouched(start, end) {
			dayFixes, err := days.get(day)
			if err != nil {
				return nil, err
			}
			fixes = append(fixes, windowOf(dayFixes, start, end)...)
		}
		if len(fixes) == 0 {
			continue // no day file, or nothing recorded inside the window
		}

		kept := chooseSource(fixes)
		var points []fix
		for _, f := range fixes {
			if f.source == kept {
				points = append(points, f)
			}
		}
		sort.SliceStable(points, func(i, j int) bool { return points[i].time.Before(points[j].time) })

		activities = append(activities, Activity{
			LogID:    l.ID,
			Type:     l.Name,
			Start:    start,
			Source:   kept,
			Segments: splitOnGaps(points),
		})
	}
	return activities, nil
}

// dayCache holds parsed day files, remembering absent days as absent (nil).
type dayCache struct {
	archive *Archive
	days    map[string][]fix
}

func (c *dayCache) get(day string) ([]fix, error) {
	if fixes, ok := c.days[day]; ok {
		return fixes, nil
	}
	var fixes []fix
	if f, ok := c.archive.days[day]; ok {
		data, err := readEntry(f)
		if err != nil {
			return nil, err
		}
		if fixes, err = readDay(data, day); err != nil {
			return nil, err
		}
	}
	c.days[day] = fixes
	return fixes, nil
}

func (c *dayCache) forgetBefore(day string) {
	for held := range c.days {
		if held < day {
			delete(c.days, held)
		}
	}
}

// daysTouched lists the UTC calendar days a window falls across: usually one, two for an
// evening ride that runs past midnight UTC, which would otherwise be cut off at the date line.
func daysTouched(start, end time.Time) []string {
	first := start.Truncate(24 * time.Hour)
	last := end.Truncate(24 * time.Hour)
	days := []string{first.Format(time.DateOnly)}
	for day := first; day.Before(last) && len(days) <= 366; { // a longer window is nonsense
		day = day.AddDate(0, 0, 1)
		days = append(days, day.Format(time.DateOnly))
	}
	return days
}

// windowOf is the fixes of a sorted day file inside [start, end).
func windowOf(fixes []fix, start, end time.Time) []fix {
	from := sort.Search(len(fixes), func(i int) bool { return !fixes[i].time.Before(start) })
	to := sort.Search(len(fixes), func(i int) bool { return !fixes[i].time.Before(end) })
	return fixes[from:max(to, from)]
}

// chooseSource picks which device's recording to keep for one activity: the one with the
// most fixes, the best available proxy for the one that dropped out least. Never both —
// they're the same journey, and interleaving them gives a zigzag at twice the distance.
// This is not what internal/ingest's cross-source dedupe does: that compares whole
// activities, and never sees two devices' points mixed inside one.
//
// A tie goes to the device seen last, matching Rust's Iterator::max_by_key in pathify.
func chooseSource(fixes []fix) string {
	var order []string
	counts := make(map[string]int)
	for _, f := range fixes {
		if _, ok := counts[f.source]; !ok {
			order = append(order, f.source)
		}
		counts[f.source]++
	}
	kept := order[0]
	for _, source := range order {
		if counts[source] >= counts[kept] {
			kept = source
		}
	}
	return kept
}

// splitOnGaps starts a new segment after any silence longer than segmentGap.
func splitOnGaps(points []fix) [][]fix {
	var segments [][]fix
	var current []fix
	for i, p := range points {
		if i > 0 && p.time.Sub(points[i-1].time) > segmentGap {
			segments = append(segments, current)
			current = nil
		}
		current = append(current, p)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

// offsetSamples is how many activities the offset is measured from, spread across the
// selection rather than the first few, since an export can span a move between timezones.
const offsetSamples = 8

// detectOffset works out how far the exercise logs' wall clock runs ahead of UTC.
//
// `startTime` carries no offset, and Fitbit has historically written local time there; the
// user's saved timezone isn't a substitute, since a history spans travel and DST changes. So
// it's measured: each 15-minute step across the inhabited range is tried, and the one that
// puts the most sample windows over recorded fixes wins, ties broken by how many fixes those
// windows cover (a small wrong shift can clip the edge of an unrelated outing earlier in the
// day, but only the right one covers a window end to end). Smaller shifts are tried first,
// UTC first of all, so a tie resolves to the least surprising offset. Zero when nothing lands.
func detectOffset(selected []Log, days *dayCache) (time.Duration, error) {
	samples := selected
	if len(selected) > offsetSamples {
		samples = make([]Log, offsetSamples)
		for i := range samples {
			samples[i] = selected[i*len(selected)/offsetSamples]
		}
	}
	if len(samples) == 0 {
		return 0, nil
	}

	// The day a window falls in depends on the offset being tried, so the neighbouring days
	// are read too — that covers every candidate.
	var times []time.Time
	for _, l := range samples {
		middle := l.Start.Truncate(24 * time.Hour)
		for _, d := range []int{-1, 0, 1} {
			fixes, err := days.get(middle.AddDate(0, 0, d).Format(time.DateOnly))
			if err != nil {
				return 0, err
			}
			for _, f := range fixes {
				times = append(times, f.time)
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	var candidates []int
	for step := -12 * 4; step <= 14*4; step++ {
		candidates = append(candidates, step*15)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ai, aj := abs(candidates[i]), abs(candidates[j])
		if ai != aj {
			return ai < aj
		}
		return candidates[i] < candidates[j]
	})

	bestWindows, bestCovered, best := 0, 0, time.Duration(0)
	for _, minutes := range candidates {
		offset := time.Duration(minutes) * time.Minute
		windows, covered := 0, 0
		for _, l := range samples {
			start, end := l.window(offset)
			from := sort.Search(len(times), func(i int) bool { return !times[i].Before(start) })
			to := sort.Search(len(times), func(i int) bool { return !times[i].Before(end) })
			if to > from {
				windows++
				covered += to - from
			}
		}
		if windows > bestWindows || (windows == bestWindows && covered > bestCovered) {
			bestWindows, bestCovered, best = windows, covered, offset
		}
	}
	return best, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// FileNames names each activity the way pathify's --per-activity output did:
// `20260711T113000Z-outdoor-bike.gpx`, the start in UTC then the type. Two activities that
// would share a name — a phone and a watch each filing the same walk — both take their log
// id as well. The name is stored as the activity's source detail.
func FileNames(activities []Activity) []string {
	stems := make([]string, len(activities))
	seen := make(map[string]int)
	for i, a := range activities {
		stems[i] = a.Start.Format("20060102T150405Z") + "-" + slug(a.Type)
		seen[stems[i]]++
	}
	names := make([]string, len(activities))
	for i, stem := range stems {
		if seen[stem] > 1 {
			names[i] = fmt.Sprintf("%s-%d.gpx", stem, activities[i].LogID)
		} else {
			names[i] = stem + ".gpx"
		}
	}
	return names
}

// slug turns an activity type into a filename fragment: `Outdoor Bike` to `outdoor-bike`.
func slug(name string) string {
	var b strings.Builder
	for _, c := range name {
		switch {
		case c < 128 && (c >= 'a' && c <= 'z' || c >= '0' && c <= '9'):
			b.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			b.WriteRune(c + 'a' - 'A')
		case !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	return "activity"
}

// GPX writes the activity exactly as pathify 1.4.0 did (through the Rust gpx crate), since
// the raw payload's content hash is the activity's identity: `name`/`desc` repeated in
// <metadata> and <trk>, the kept device as `creator`, shortest-form floats, and times cut to
// whole seconds but printed with nine fractional digits. No trailing newline.
func (a Activity) GPX() []byte {
	name := xmlText(a.Type + " " + a.Start.Format(time.DateOnly))
	desc := xmlText("Fitbit log " + strconv.FormatInt(a.LogID, 10))

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1" creator="` + xmlAttr(a.Source) + `">` + "\n")
	b.WriteString("  <metadata>\n")
	b.WriteString("    <name>" + name + "</name>\n")
	b.WriteString("    <desc>" + desc + "</desc>\n")
	b.WriteString("    <time>" + gpxTime(a.Segments[0][0].time) + "</time>\n")
	b.WriteString("  </metadata>\n")
	b.WriteString("  <trk>\n")
	b.WriteString("    <name>" + name + "</name>\n")
	b.WriteString("    <desc>" + desc + "</desc>\n")
	for _, segment := range a.Segments {
		b.WriteString("    <trkseg>\n")
		for _, p := range segment {
			b.WriteString(`      <trkpt lat="` + gpxFloat(p.lat) + `" lon="` + gpxFloat(p.lon) + `">` + "\n")
			if p.ele != nil {
				b.WriteString("        <ele>" + gpxFloat(*p.ele) + "</ele>\n")
			}
			b.WriteString("        <time>" + gpxTime(p.time) + "</time>\n")
			b.WriteString("      </trkpt>\n")
		}
		b.WriteString("    </trkseg>\n")
	}
	b.WriteString("  </trk>\n")
	b.WriteString("</gpx>")
	return []byte(b.String())
}

// gpxFloat is Rust's f64 Display: the shortest digits that round-trip, never an exponent.
//
// Go's shortest formatting agrees except on an exact tie — a value whose exact binary
// expansion sits precisely halfway between two shortest candidates, which happens with
// float32 elevations widened to float64 (237.600006103515625). Go rounds that half to even
// (…562), Rust up (…563), so the tie is resolved Rust's way here.
func gpxFloat(v float64) string {
	short := strconv.FormatFloat(v, 'f', -1, 64)
	if v == 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return short
	}
	digits, exp := decimalDigits(strconv.FormatFloat(math.Abs(v), 'e', -1, 64))
	exactDigits, exactExp := decimalDigits(new(big.Float).SetFloat64(math.Abs(v)).Text('e', 1100))
	if exp != exactExp || len(exactDigits) <= len(digits) {
		return short
	}
	tail := strings.TrimRight(exactDigits[len(digits):], "0")
	if tail != "5" {
		return short // not a tie: the nearest shortest candidate is unique, and Go found it
	}
	up := []byte(exactDigits[:len(digits)])
	i := len(up) - 1
	for ; i >= 0 && up[i] == '9'; i-- {
		up[i] = '0'
	}
	if i < 0 {
		return short // carry out of the leading digit; not a case real coordinates reach
	}
	up[i]++
	candidate := string(up[:1]) + "." + string(up[1:]) + "e" + strconv.Itoa(exp)
	if parsed, err := strconv.ParseFloat(candidate, 64); err != nil || parsed != math.Abs(v) {
		return short
	}
	fixed := fixedNotation(string(up), exp)
	if v < 0 {
		return "-" + fixed
	}
	return fixed
}

// decimalDigits splits "d.ddde±XX" into its significant digits and decimal exponent.
func decimalDigits(e string) (string, int) {
	mantissa, exponent, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(exponent)
	return strings.Replace(mantissa, ".", "", 1), exp
}

// fixedNotation places the decimal point in digits (d.ddd × 10^exp) without an exponent.
func fixedNotation(digits string, exp int) string {
	point := exp + 1
	var s string
	switch {
	case point <= 0:
		s = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		return digits + strings.Repeat("0", point-len(digits))
	default:
		s = digits[:point] + "." + digits[point:]
	}
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func gpxTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05.000000000Z")
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
)

func xmlText(s string) string { return textEscaper.Replace(s) }
func xmlAttr(s string) string { return attrEscaper.Replace(s) }
