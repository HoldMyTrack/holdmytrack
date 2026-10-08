package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// Coverage status reads "rendering" while any job that will change the account's rasters is
// pending — an accepted .zip's unpack included, before it has produced a single ingest job.
func TestCoverageStatusRendering(t *testing.T) {
	d := newDBTest(t)
	me := d.newAccount(false)
	rendering := func() bool {
		var resp coverageStatusResponse
		d.decode(d.do(me, "GET", "/v1/coverage/status", nil), http.StatusOK, &resp)
		return resp.Rendering
	}
	if rendering() {
		t.Fatal("rendering with no jobs")
	}

	var id int64
	if err := d.pool.QueryRow(context.Background(),
		`INSERT INTO jobs (kind, user_id, payload) VALUES ('unpack', $1, '{}') RETURNING id`, me.id,
	).Scan(&id); err != nil {
		t.Fatalf("insert unpack job: %v", err)
	}
	if !rendering() {
		t.Error("not rendering while an unpack is pending")
	}

	if _, err := d.pool.Exec(context.Background(), `UPDATE jobs SET state = 'done', finished_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if rendering() {
		t.Error("still rendering after the unpack finished")
	}
}
