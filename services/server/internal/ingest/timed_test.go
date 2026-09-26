package ingest

import (
	"errors"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

func TestKeepTimed(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	t.Run("no timestamps at all fails", func(t *testing.T) {
		_, err := keepTimed([]parse.Point{pt(40, -83, time.Time{}), pt(40.001, -83, time.Time{})})
		if !errors.Is(err, errNoTimestamps) {
			t.Fatalf("err = %v, want errNoTimestamps", err)
		}
	})

	t.Run("untimed points among timed ones are dropped", func(t *testing.T) {
		in := []parse.Point{pt(40, -83, time.Time{}), pt(40.001, -83, t0), pt(40.002, -83, time.Time{}), pt(40.003, -83, t0.Add(time.Second))}
		got, err := keepTimed(in)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Lat != 40.001 || got[1].Lat != 40.003 {
			t.Fatalf("got %+v, want the two timed points", got)
		}
		if !in[0].Time.IsZero() || in[2].Lat != 40.002 {
			t.Fatal("input was modified")
		}
	})

	t.Run("fully timed track is unchanged", func(t *testing.T) {
		in := line(40, -83, 5, 10)
		got, err := keepTimed(in)
		if err != nil || len(got) != len(in) {
			t.Fatalf("got %d points, err %v; want %d, nil", len(got), err, len(in))
		}
	})
}
