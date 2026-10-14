package httpapi

import (
	"testing"
	"time"
)

func TestLastSeenDue(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	at := func(v time.Time) *time.Time { return &v }
	for _, tc := range []struct {
		name     string
		lastSeen *time.Time
		now      time.Time
		want     bool
	}{
		{"never written", nil, now, true},
		{"written earlier the same day", at(now.Add(-time.Minute)), now, false},
		{"written at midnight, 23 hours later the same day", at(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)), time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC), false},
		{"written just before midnight, read just after", at(time.Date(2026, 10, 12, 23, 59, 0, 0, time.UTC)), time.Date(2026, 10, 13, 0, 1, 0, 0, time.UTC), true},
		{"written a month ago", at(now.AddDate(0, -1, 0)), now, true},
		{"same UTC day, read in another zone", at(now.Add(-time.Hour)), now.In(time.FixedZone("UTC+13", 13*3600)), false},
	} {
		if got := lastSeenDue(tc.lastSeen, tc.now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
