package ingest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// editFixture is ten points one second apart, walking north.
func editFixture() ([]parse.Point, []int64) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	points := make([]parse.Point, 10)
	ms := make([]int64, 10)
	for i := range points {
		points[i] = pt(52.0+float64(i)*0.0001, 13.0, t0.Add(time.Duration(i)*time.Second))
		ms[i] = points[i].Time.UnixMilli()
	}
	return points, ms
}

func survivors(points []parse.Point, t0 int64) []int {
	out := make([]int, len(points))
	for i, p := range points {
		out[i] = int((p.Time.UnixMilli() - t0) / 1000)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTrackEditApply(t *testing.T) {
	points, ms := editFixture()

	cases := []struct {
		name string
		edit TrackEdit
		want []int
	}{
		{"empty keeps everything", TrackEdit{}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"chop keeps the range inclusive", TrackEdit{Keep: &[2]int64{ms[2], ms[7]}}, []int{2, 3, 4, 5, 6, 7}},
		// A Cut between knobs on points 3 and 7 removes 4..6 and keeps both knob points.
		{"cut removes the inner range", TrackEdit{Remove: [][2]int64{{ms[4], ms[6]}}}, []int{0, 1, 2, 3, 7, 8, 9}},
		{"drop removes single points", TrackEdit{Drop: []int64{ms[0], ms[5]}}, []int{1, 2, 3, 4, 6, 7, 8, 9}},
		{"all three combine", TrackEdit{
			Keep:   &[2]int64{ms[1], ms[8]},
			Remove: [][2]int64{{ms[3], ms[4]}},
			Drop:   []int64{ms[7]},
		}, []int{1, 2, 5, 6, 8}},
	}
	for _, c := range cases {
		got := survivors(c.edit.Apply(points), ms[0])
		if !equalInts(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The spec addresses points by time, so a different Private location clip — which removes a
// different number of points off each end — must leave the edit meaning the same points.
func TestTrackEditSurvivesDifferentTrim(t *testing.T) {
	points, ms := editFixture()
	edit := TrackEdit{Remove: [][2]int64{{ms[4], ms[5]}}}

	full := survivors(edit.Apply(points), ms[0])
	trimmed := survivors(edit.Apply(points[2:8]), ms[0])
	if !equalInts(full, []int{0, 1, 2, 3, 6, 7, 8, 9}) {
		t.Fatalf("untrimmed: got %v", full)
	}
	if !equalInts(trimmed, []int{2, 3, 6, 7}) {
		t.Errorf("trimmed: got %v, want the same cut applied to what's left", trimmed)
	}
}

func TestTrackEditValidate(t *testing.T) {
	if err := (TrackEdit{Keep: &[2]int64{5, 1}}).Validate(); err == nil {
		t.Error("backwards keep range must be rejected")
	}
	if err := (TrackEdit{Remove: [][2]int64{{9, 3}}}).Validate(); err == nil {
		t.Error("backwards remove range must be rejected")
	}
	if err := (TrackEdit{Keep: &[2]int64{1, 1}, Remove: [][2]int64{{2, 2}}}).Validate(); err != nil {
		t.Errorf("single-instant ranges are valid: %v", err)
	}
}

// An empty spec round-trips as {} and reads back as empty — the handler stores NULL for it.
func TestTrackEditJSON(t *testing.T) {
	b, err := json.Marshal(TrackEdit{})
	if err != nil || string(b) != "{}" {
		t.Fatalf("empty edit marshals to %s (%v)", b, err)
	}
	var e TrackEdit
	if err := json.Unmarshal([]byte(`{"keep":[1,2],"remove":[[3,4]],"drop":[5]}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.Keep == nil || e.Keep[1] != 2 || len(e.Remove) != 1 || e.Drop[0] != 5 {
		t.Errorf("unexpected decode: %+v", e)
	}
}

func TestEditableTimestamps(t *testing.T) {
	points, _ := editFixture()
	if err := EditableTimestamps(points); err != nil {
		t.Errorf("a forward-running track must be editable: %v", err)
	}

	repeated := append([]parse.Point(nil), points...)
	repeated[4].Time = repeated[3].Time
	if err := EditableTimestamps(repeated); err != nil {
		t.Errorf("repeated timestamps are tolerated: %v", err)
	}

	backwards := append([]parse.Point(nil), points...)
	backwards[4].Time = backwards[2].Time.Add(-time.Second)
	if err := EditableTimestamps(backwards); err == nil {
		t.Error("a clock running backwards must not be editable")
	}

	untimed := make([]parse.Point, 5)
	if err := EditableTimestamps(untimed); err == nil {
		t.Error("a track with no timestamps must not be editable")
	}
}

// A Cut's joining segment is walked distance like any other segment, but the time gap it
// spans is a stop, not movement.
func TestMetricsAfterCut(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	points := []parse.Point{
		pt(52.0, 13.0, t0),
		pt(52.0001, 13.0, t0.Add(5*time.Second)),
		// 20 minutes later and ~11 m on: what's left once a mall wander is cut out.
		pt(52.0002, 13.0, t0.Add(20*time.Minute)),
		pt(52.0003, 13.0, t0.Add(20*time.Minute+5*time.Second)),
	}
	m := computeMetrics(points)
	if m.durationS != 1205 {
		t.Errorf("duration = %d, want the full elapsed 1205 s", m.durationS)
	}
	if m.movingS != 10 {
		t.Errorf("moving = %d, want only the two 5 s walking segments", m.movingS)
	}
	if m.distanceM < 33 || m.distanceM > 34 {
		t.Errorf("distance = %.1f m, want the joining segment included (~33.4 m)", m.distanceM)
	}
}
