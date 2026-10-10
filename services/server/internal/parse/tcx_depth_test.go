package parse

import (
	"strings"
	"testing"
	"time"
)

// Parsing time is linear in nesting depth: scanning the open-element stack on every text token
// made a 440 KB file of nested elements take a second, and a 16 MiB upload hold the worker for
// about half an hour.
func TestParseTCXDeepNestingIsLinear(t *testing.T) {
	const depth = 100_000
	var b strings.Builder
	b.WriteString("<TrainingCenterDatabase>")
	b.WriteString(strings.Repeat("<a>", depth))
	b.WriteString("<Trackpoint><Time>2026-01-01T12:00:00Z</Time><Position><LatitudeDegrees>1</LatitudeDegrees><LongitudeDegrees>2</LongitudeDegrees></Position>")
	b.WriteString(strings.Repeat("<b>x</b>", depth))
	b.WriteString("</Trackpoint>")
	b.WriteString(strings.Repeat("</a>", depth))
	b.WriteString("</TrainingCenterDatabase>")

	start := time.Now()
	act, err := ParseTCX(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("ParseTCX: %v", err)
	}
	if len(act.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(act.Points))
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v for depth %d", d, depth)
	}
}
