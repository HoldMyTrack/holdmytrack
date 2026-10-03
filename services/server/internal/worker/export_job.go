package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mail"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// Notifier is what the export job needs to tell the owner their archive is ready: a way to
// send email, and the site's address to link to.
type Notifier struct {
	Mailer  mail.Sender
	BaseURL string
}

// notifier is Run's, read by runExport. Nil in tests that don't set it: no email is sent.
var notifier *Notifier

// exportPollInterval is how often the export lane looks for a request. Coarse: an export
// takes minutes, and the person is told by email, not by watching.
const exportPollInterval = 5 * time.Second

// exportHeartbeat refreshes a running export's claim: a big account can take longer than
// claimLease, and an export that looked abandoned would be claimed and built a second time.
const exportHeartbeat = 5 * time.Minute

// ExportJob is an `export` job's payload.
type ExportJob struct {
	ExportID string `json:"export_id"`
}

// runExport builds one requested export (export.Build), marks it ready for export.Lifetime,
// and emails the owner a link to Settings, where it is downloaded. A request whose account
// or row is gone by now is done with nothing to do. A failed email is logged, not retried:
// the archive is ready on the Settings page either way.
func runExport(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, jobID int64, payload []byte) error {
	var ej ExportJob
	if err := json.Unmarshal(payload, &ej); err != nil {
		return fmt.Errorf("unmarshal export job: %w", err)
	}
	var userID, lang, email, timezone string
	err := pool.QueryRow(ctx, `
		SELECT e.user_id, e.lang, u.email, u.timezone FROM exports e JOIN users u ON u.id = e.user_id
		WHERE e.id = $1 AND e.ready_at IS NULL AND u.deleted_at IS NULL`, ej.ExportID,
	).Scan(&userID, &lang, &email, &timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("export lookup: %w", err)
	}

	hctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		t := time.NewTicker(exportHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-hctx.Done():
				return
			case <-t.C:
				if _, err := pool.Exec(hctx, `UPDATE jobs SET locked_at = NOW() WHERE id = $1`, jobID); err != nil && hctx.Err() == nil {
					log.Error("export heartbeat failed", "job_id", jobID, "err", err)
				}
			}
		}
	}()

	sizes, err := export.Build(ctx, pool, store, userID, ej.ExportID, lang)
	if err != nil {
		return err
	}
	var expires time.Time
	if err := pool.QueryRow(ctx, `
		UPDATE exports SET ready_at = NOW(), expires_at = NOW() + make_interval(secs => $2), part_sizes = $3
		WHERE id = $1 RETURNING expires_at`, ej.ExportID, export.Lifetime.Seconds(), sizes,
	).Scan(&expires); err != nil {
		return fmt.Errorf("export mark ready: %w", err)
	}
	log.Info("export ready", "export_id", ej.ExportID, "user_id", userID, "parts", len(sizes))

	if notifier == nil || notifier.Mailer == nil {
		return nil
	}
	var total int64
	for _, s := range sizes {
		total += s
	}
	l := i18n.Get(lang)
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	link := strings.TrimRight(notifier.BaseURL, "/") + "/settings#download-data"
	body := l.T("email.export.body",
		"parts", l.N("email.export.parts", int64(len(sizes))),
		"size", web.FormatBytes(l, total),
		"date", web.ShortDate(l, expires.In(loc).Format("2006-01-02")),
		"link", link)
	if err := notifier.Mailer.Send(ctx, email, l.T("email.export.subject"), body); err != nil {
		log.Error("export email failed", "export_id", ej.ExportID, "err", err)
	}
	return nil
}

// sweepExports removes every export past its expiry — its parts, then its row — and every
// request whose job failed or vanished more than export.Lifetime ago, so a failure stays on
// the Settings page for a while and then goes.
func sweepExports(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `
		SELECT e.id, e.user_id FROM exports e LEFT JOIN jobs j ON j.id = e.job_id
		WHERE e.expires_at < NOW()
		   OR (e.ready_at IS NULL AND e.requested_at < NOW() - make_interval(secs => $1)
		       AND (j.id IS NULL OR j.state = 'failed'))
		LIMIT 100`, export.Lifetime.Seconds())
	if err != nil {
		return fmt.Errorf("export sweep: %w", err)
	}
	type gone struct{ id, userID string }
	var all []gone
	for rows.Next() {
		var g gone
		if err := rows.Scan(&g.id, &g.userID); err != nil {
			rows.Close()
			return fmt.Errorf("export sweep: %w", err)
		}
		all = append(all, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export sweep: %w", err)
	}
	for _, g := range all {
		if err := store.RemoveByPrefix(ctx, export.Prefix(g.userID, g.id)); err != nil {
			log.Error("export sweep: storage cleanup failed, will retry", "export_id", g.id, "err", err)
			continue
		}
		if _, err := pool.Exec(ctx, `DELETE FROM exports WHERE id = $1`, g.id); err != nil {
			log.Error("export sweep: delete failed", "export_id", g.id, "err", err)
		}
	}
	return nil
}
