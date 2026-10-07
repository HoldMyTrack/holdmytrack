package geo

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A load puts each activity in the zone its track starts in, leaves a zone the database's tz
// data doesn't know out, falls back to the account's zone where no polygon covers the start,
// leaves an activity with no track as it is, and skips a file it has already loaded; one with
// too few zones is refused.
func TestLoadTimezones(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	// Boxes in the South Atlantic, where no real zone polygon of a test database's would be.
	box := func(lon1, lat1, lon2, lat2 float64) string {
		return fmt.Sprintf(`{"type":"Polygon","coordinates":[[[%[1]v,%[2]v],[%[3]v,%[2]v],[%[3]v,%[4]v],[%[1]v,%[4]v],[%[1]v,%[2]v]]]}`,
			lon1, lat1, lon2, lat2)
	}
	feature := func(tzid, geom string) string {
		return `{"type":"Feature","properties":{"tzid":"` + tzid + `"},"geometry":` + geom + `}`
	}
	release := func() *bytes.Buffer {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("combined-with-oceans.json")
		w.Write([]byte(`{"type":"FeatureCollection","features":[` + strings.Join([]string{
			feature("Asia/Tokyo", box(-41, -61, -40, -60)),
			feature("Etc/GMT+3", box(-40, -61, -39, -60)),
			feature("Mars/Olympus_Mons", box(-39, -61, -38, -60)),
		}, ",") + `]}`))
		zw.Close()
		return &buf
	}

	var userID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, timezone) VALUES ($1, 'America/New_York') RETURNING id`,
		fmt.Sprintf("geo-tz-%d@holdmytrack.invalid", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	activity := func(track string) string {
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO activities (user_id, source, activity_type, started_at, trajectory, timezone)
			VALUES ($1, 'upload', 'walking', now(), ST_GeomFromText(NULLIF($2, ''), 4326), 'UTC')
			RETURNING id`, userID, track).Scan(&id); err != nil {
			t.Fatalf("create activity: %v", err)
		}
		return id
	}
	// Starts in "Tokyo", ends at "sea"; starts at sea; starts in the unknown zone; starts where
	// no polygon is; and one with no track.
	tokyo := activity("LINESTRING M(-40.5 -60.5 0, -39.5 -60.5 60)")
	sea := activity("LINESTRING M(-39.5 -60.5 0, -40.5 -60.5 60)")
	unknown := activity("LINESTRING M(-38.5 -60.5 0, -38.4 -60.5 60)")
	nowhere := activity("LINESTRING M(-30.5 -60.5 0, -30.4 -60.5 60)")
	hidden := activity("")
	zone := func(id string) string {
		var tz string
		if err := tx.QueryRow(ctx, `SELECT timezone FROM activities WHERE id = $1`, id).Scan(&tz); err != nil {
			t.Fatal(err)
		}
		return tz
	}

	stats, err := loadTimezones(ctx, tx, log, release(), "test-1.zip", false, 3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stats.Zones != 2 || len(stats.UnknownZones) != 1 || stats.UnknownZones[0] != "Mars/Olympus_Mons" || stats.Activities != 4 {
		t.Errorf("stats %+v", stats)
	}
	for id, want := range map[string]string{
		tokyo: "Asia/Tokyo", sea: "Etc/GMT+3", unknown: "America/New_York", nowhere: "America/New_York", hidden: "UTC",
	} {
		if got := zone(id); got != want {
			t.Errorf("activity %s: zone %q, want %q", id, got, want)
		}
	}

	stats, err = loadTimezones(ctx, tx, log, release(), "test-1.zip", false, 3)
	if err != nil || !stats.Unchanged {
		t.Errorf("the same file again: %+v, %v", stats, err)
	}
	if _, err := loadTimezones(ctx, tx, log, release(), "test-2.zip", false, 4); err == nil || !strings.Contains(err.Error(), "fewer than") {
		t.Errorf("too few zones: %v", err)
	}
}

// The zone's name, not an offset, is stored: the same zone gives each date the offset in force
// then. Moscow kept summer time until 2011, then sat on UTC+4 until late 2014, and on UTC+3 since.
func TestTimezoneHistory(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	for at, want := range map[string]string{
		"2010-07-01T12:00:00Z": "2010-07-01 16:00:00",
		"2013-07-01T12:00:00Z": "2013-07-01 16:00:00",
		"2015-07-01T12:00:00Z": "2015-07-01 15:00:00",
	} {
		var got string
		if err := tx.QueryRow(ctx, `SELECT to_char($1::timestamptz AT TIME ZONE 'Europe/Moscow', 'YYYY-MM-DD HH24:MI:SS')`, at).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s in Moscow: %s, want %s", at, got, want)
		}
	}
}
