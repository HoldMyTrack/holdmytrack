package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/budget"
)

// What the day's R2 spend stops, stage by stage: at the adding stage an upload, a sync and an
// avatar are refused with a 503 naming when it resets, while Fog tiles and the database's own
// tiles are served; at the last stage Fog tiles are refused too, and the track tiles still
// served. Today's r2_ops rows are set to $4.50 of requests and put back after.
func TestBudgetStages(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()
	me := d.newAccount(false)
	today := time.Now().UTC().Format("2006-01-02")
	var saved []struct {
		class string
		count int64
	}
	rows, err := d.pool.Query(ctx, `SELECT class, count FROM r2_ops WHERE day = $1`, today)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct {
			class string
			count int64
		}
		rows.Scan(&r.class, &r.count)
		saved = append(saved, r)
	}
	rows.Close()
	t.Cleanup(func() {
		d.pool.Exec(context.Background(), `DELETE FROM r2_ops WHERE day = $1`, today)
		for _, r := range saved {
			d.pool.Exec(context.Background(), `INSERT INTO r2_ops (day, class, count) VALUES ($1, $2, $3)`, today, r.class, r.count)
		}
	})
	if _, err := d.pool.Exec(ctx, `DELETE FROM r2_ops WHERE day = $1`, today); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO r2_ops (day, class, count) VALUES ($1, 'A', 1000000)`, today); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)

	d.srv.SetBudget(budget.NewGate(d.pool, 5, log)) // 90%: adding stops, reads go on
	rec := d.uploadRaw(me, "day.gpx", []byte(splitGPX))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "midnight UTC") {
		t.Fatalf("upload: %d %q, want 503 saying when it resets", rec.Code, rec.Body.String())
	}
	if after, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || after < 1 || after > 86401 {
		t.Errorf("Retry-After %q, want the seconds to midnight UTC", rec.Header().Get("Retry-After"))
	}
	if rec := d.do(me, "POST", "/v1/sync/activities", map[string]any{"source": "healthconnect", "activities": []any{}}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("sync: %d, want 503", rec.Code)
	}
	if rec := d.do(me, "POST", "/v1/account/export", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("data download request: %d, want 503", rec.Code)
	}
	if rec := d.do(me, "GET", "/tiles/v1/fog/14/8800/5370", nil); rec.Code == http.StatusServiceUnavailable {
		t.Error("Fog tile refused before the last stage")
	}

	d.srv.SetBudget(budget.NewGate(d.pool, 4, log)) // 112%: the map's reads stop too
	if rec := d.do(me, "GET", "/tiles/v1/fog/14/8800/5370", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Fog tile: %d, want 503", rec.Code)
	}
	if rec := d.do(me, "GET", "/tiles/v1/tracks/14/8800/5370", nil); rec.Code == http.StatusServiceUnavailable {
		t.Error("track tile refused: it reads only the database")
	}

	d.srv.SetBudget(budget.NewGate(d.pool, 100, log)) // 4.5%: nothing stops
	if rec := d.uploadRaw(me, "day.gpx", []byte(splitGPX)); rec.Code != http.StatusAccepted {
		t.Errorf("upload under budget: %d %s", rec.Code, rec.Body.String())
	}
}
