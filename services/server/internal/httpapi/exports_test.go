package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
)

func TestRequestExportOnceWhilePreparing(t *testing.T) {
	d := newDBTest(t)
	a := d.newAccount(false)

	var st exportStatus
	d.decode(d.do(a, "GET", "/v1/account/export", nil), http.StatusOK, &st)
	if st.State != "none" {
		t.Fatalf("before any request: %q", st.State)
	}
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, &st)
	if st.State != "preparing" || st.ID == "" {
		t.Fatalf("after a request: %+v", st)
	}
	first := st.ID
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, &st)
	if st.ID != first {
		t.Error("a second request while preparing made a second export")
	}
	var jobs int
	d.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE user_id = $1 AND kind = 'export'`, a.id).Scan(&jobs)
	if jobs != 1 {
		t.Errorf("%d export jobs", jobs)
	}

	demo := d.newAccount(true)
	if rec := d.do(demo, "POST", "/v1/account/export", nil); rec.Code != http.StatusForbidden {
		t.Errorf("demo: status %d", rec.Code)
	}
}

func TestDownloadExportPart(t *testing.T) {
	s3 := newMemS3()
	d := newDBTestWithS3(t, s3)
	ctx := context.Background()
	a, other := d.newAccount(false), d.newAccount(false)

	var st exportStatus
	d.decode(d.do(a, "POST", "/v1/account/export", nil), http.StatusAccepted, &st)
	sizes, err := export.Build(ctx, d.pool, d.srv.store, a.id, st.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	part := export.PartKey(a.id, st.ID, 1)
	// Not downloadable until it's marked ready.
	if rec := d.do(a, "GET", exportPartURL(st.ID, 1), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("before ready: status %d", rec.Code)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE exports SET ready_at = NOW(), expires_at = NOW() + interval '7 days', part_sizes = $2 WHERE id = $1`, st.ID, sizes); err != nil {
		t.Fatal(err)
	}

	d.decode(d.do(a, "GET", "/v1/account/export", nil), http.StatusOK, &st)
	if st.State != "ready" || len(st.Parts) != 1 || st.Parts[0].URL != exportPartURL(st.ID, 1) || st.TotalSize != sizes[0] {
		t.Fatalf("ready: %+v", st)
	}

	rec := d.do(a, "GET", st.Parts[0].URL, nil)
	want, _ := s3.Object(part)
	if rec.Code != http.StatusOK || rec.Body.String() != string(want) || rec.Header().Get("Content-Disposition") != `attachment; filename="`+st.Parts[0].Name+`"` {
		t.Fatalf("download: status %d, %d bytes, disposition %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Disposition"))
	}
	// A resumed download: the rest from a byte offset.
	req := httptest.NewRequest("GET", st.Parts[0].URL, nil)
	req.Header.Set("Authorization", bearerPrefix+a.session)
	req.Header.Set("Range", "bytes=10-")
	ranged := httptest.NewRecorder()
	d.srv.ServeHTTP(ranged, req)
	body, _ := io.ReadAll(ranged.Body)
	if ranged.Code != http.StatusPartialContent || string(body) != string(want[10:]) {
		t.Errorf("range: status %d, %d bytes, want %d", ranged.Code, len(body), len(want)-10)
	}

	if rec := d.do(other, "GET", st.Parts[0].URL, nil); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's export: status %d", rec.Code)
	}
	if rec := d.do(a, "GET", exportPartURL(st.ID, 2), nil); rec.Code != http.StatusNotFound {
		t.Errorf("a part past the last: status %d", rec.Code)
	}
}
