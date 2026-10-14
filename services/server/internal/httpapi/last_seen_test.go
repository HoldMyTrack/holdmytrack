package httpapi

import (
	"testing"
	"time"
)

func TestLastSeenDue(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	for _, tc := range []struct {
		name     string
		lastSeen *time.Time
		want     bool
	}{
		{"never written", nil, true},
		{"written a minute ago", at(time.Minute), false},
		{"written just under a day ago", at(lastSeenInterval - time.Second), false},
		{"written a day ago", at(lastSeenInterval), true},
		{"written a month ago", at(30 * 24 * time.Hour), true},
	} {
		if got := lastSeenDue(tc.lastSeen, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
