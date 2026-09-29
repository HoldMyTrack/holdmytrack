package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// Tests that need a real Postgres+PostGIS read TEST_DATABASE_URL and skip without it; CI's Go
// job and `make test-go` both set it (docs/DEVELOPMENT.md). They migrate that database, and
// every row they write hangs off accounts they create and delete again, so a shared dev
// database is fine too.

// dbTest is a real Server over the test database, with an object store that holds nothing.
type dbTest struct {
	t    *testing.T
	pool *pgxpool.Pool
	srv  *Server
}

func newDBTest(t *testing.T) *dbTest {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	s3 := httptest.NewServer(http.HandlerFunc(emptyS3))
	t.Cleanup(s3.Close)
	store, err := storage.New(s3.URL, "test", "test", "test")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	pages, err := web.New(web.Embedded(), false, "test", "https://app.example")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	srv := New(pool, store, slog.New(slog.DiscardHandler), nil, "https://app.example", "", "test", false, GoogleOAuthConfig{}, FacebookOAuthConfig{}, pages)
	return &dbTest{t: t, pool: pool, srv: srv}
}

// emptyS3 answers the S3 calls an activity delete makes as a bucket with nothing in it.
func emptyS3(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	if _, ok := r.URL.Query()["location"]; ok {
		io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	io.WriteString(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test</Name><IsTruncated>false</IsTruncated></ListBucketResult>`)
}

// account is a verified account (or a demo one) with a live session, past first-run Settings
// (a Country, metric).
type account struct {
	id, session string
}

func (d *dbTest) newAccount(demo bool) account {
	d.t.Helper()
	ctx := context.Background()
	var demoExpires *time.Time
	if demo {
		e := time.Now().Add(time.Hour)
		demoExpires = &e
	}
	var a account
	email := fmt.Sprintf("test-%d@holdmytrack.invalid", time.Now().UnixNano())
	if err := d.pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified, demo_expires_at, country) VALUES ($1, true, $2, 'DE') RETURNING id`,
		email, demoExpires).Scan(&a.id); err != nil {
		d.t.Fatalf("create account: %v", err)
	}
	d.t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, a.id); err != nil {
			d.t.Errorf("delete account: %v", err)
		}
	})
	if err := d.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, expires_at) VALUES ($1, NOW() + INTERVAL '1 hour') RETURNING id`,
		a.id).Scan(&a.session); err != nil {
		d.t.Fatalf("create session: %v", err)
	}
	return a
}

// testActivity is the part of an activities row the tests care about.
type testActivity struct {
	activityType   string
	distanceMeters float64
	movingSeconds  *int
	durationSecs   int
	startedAt      time.Time
	supersededBy   string
	// at, when set, gives the activity a short track starting at this [lon, lat].
	at *[2]float64
}

func (d *dbTest) newActivity(owner account, a testActivity) string {
	d.t.Helper()
	if a.startedAt.IsZero() {
		a.startedAt = time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	}
	var track *string
	if a.at != nil {
		lon, lat, m := a.at[0], a.at[1], a.startedAt.Unix()
		wkt := fmt.Sprintf("LINESTRINGM(%f %f %d, %f %f %d)", lon, lat, m, lon+0.001, lat+0.001, m+60)
		track = &wkt
	}
	var id string
	if err := d.pool.QueryRow(context.Background(), `
		INSERT INTO activities (user_id, source, activity_type, distance_meters, moving_seconds, duration_seconds, started_at, superseded_by, trajectory)
		VALUES ($1, 'upload', $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, ST_GeomFromText($8, 4326)) RETURNING id
	`, owner.id, a.activityType, a.distanceMeters, a.movingSeconds, a.durationSecs, a.startedAt, a.supersededBy, track).Scan(&id); err != nil {
		d.t.Fatalf("create activity: %v", err)
	}
	return id
}

// do sends one request as as (the zero account is signed out), with body JSON-encoded when
// it isn't nil.
func (d *dbTest) do(as account, method, path string, body any) *httptest.ResponseRecorder {
	d.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			d.t.Fatalf("encode body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if as.session != "" {
		req.Header.Set("Authorization", bearerPrefix+as.session)
	}
	rec := httptest.NewRecorder()
	d.srv.ServeHTTP(rec, req)
	return rec
}

// decode reads rec's JSON body into v, failing the test on a status other than want.
func (d *dbTest) decode(rec *httptest.ResponseRecorder, want int, v any) {
	d.t.Helper()
	if rec.Code != want {
		d.t.Fatalf("status %d, want %d: %s", rec.Code, want, strings.TrimSpace(rec.Body.String()))
	}
	if v != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			d.t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
}
