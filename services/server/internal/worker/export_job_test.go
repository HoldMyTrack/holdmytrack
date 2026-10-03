package worker

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

type sentMail struct {
	mu                sync.Mutex
	to, subject, body string
}

func (m *sentMail) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.to, m.subject, m.body = to, subject, body
	return nil
}

func TestExportJobBuildsMarksReadyAndEmails(t *testing.T) {
	pool, userID := testPool(t)
	ctx := context.Background()
	s3 := storagetest.New()
	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	mail := &sentMail{}
	notifier = &Notifier{Mailer: mail, BaseURL: "https://app.example/"}
	t.Cleanup(func() { notifier = nil })

	var exportID string
	if err := pool.QueryRow(ctx, `INSERT INTO exports (user_id, lang) VALUES ($1, 'en') RETURNING id`, userID).Scan(&exportID); err != nil {
		t.Fatal(err)
	}
	job := insertJobOfKind(t, pool, userID, "export", `{"export_id":"`+exportID+`"}`, "2000-01-01", 0, nil)
	log := slog.New(slog.DiscardHandler)

	// The main lane leaves it alone; the export lane takes it.
	if processed, err := claimAndRun(ctx, pool, store, log, laneMain); err != nil || readJob(t, pool, job).state != "pending" {
		t.Fatalf("main lane: processed %v, err %v, state %q", processed, err, readJob(t, pool, job).state)
	}
	if processed, err := claimAndRun(ctx, pool, store, log, laneExport); err != nil || !processed {
		t.Fatalf("export lane: processed %v, err %v", processed, err)
	}
	if got := readJob(t, pool, job); got.state != "done" {
		t.Fatalf("job %+v", got)
	}
	var parts int
	pool.QueryRow(ctx, `SELECT cardinality(part_sizes) FROM exports WHERE id = $1 AND ready_at IS NOT NULL AND expires_at > NOW()`, exportID).Scan(&parts)
	if parts != 1 || !s3.Has(export.PartKey(userID, exportID, 1)) {
		t.Fatalf("export not ready: %d parts", parts)
	}
	if !strings.HasPrefix(mail.to, "worker-test-") || !strings.Contains(mail.body, "https://app.example/settings#download-data") || !strings.Contains(mail.body, "1 zip archive") {
		t.Errorf("email to %q: %s", mail.to, mail.body)
	}

	// Once expired, the sweep removes its part and its row.
	if _, err := pool.Exec(ctx, `UPDATE exports SET expires_at = NOW() - interval '1 minute' WHERE id = $1`, exportID); err != nil {
		t.Fatal(err)
	}
	if err := sweepExports(ctx, pool, store, log); err != nil {
		t.Fatal(err)
	}
	var rows int
	pool.QueryRow(ctx, `SELECT count(*) FROM exports WHERE id = $1`, exportID).Scan(&rows)
	if rows != 0 || s3.Has(export.PartKey(userID, exportID, 1)) {
		t.Errorf("after the sweep: %d rows, part stored %v", rows, s3.Has(export.PartKey(userID, exportID, 1)))
	}
}
