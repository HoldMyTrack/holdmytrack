package httpapi

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/ingest"
	"github.com/fitmap/fitmap/services/server/internal/storage"
)

// DemoCustomerUserID is the one persistent, shared demo account every "Try Demo" visitor's
// session is pointed at (handleDemoStart) — not a fresh row created and re-seeded per visitor.
// Must match migrations/0017_demo_customer_user.sql's fixed id. Its demo_expires_at is a
// far-future timestamp, not NULL, so it stays isDemo == true (read-only, verification-exempt)
// without ever being swept by internal/worker/demo_purge.go.
const DemoCustomerUserID = "22222222-2222-2222-2222-222222222222"

// demoActivityPrivacyTrimM matches the real-account default (server.go's signup path), not
// zero: unlike the old hand-written presets' invented coordinates, these are real recorded GPS
// tracks around a real location, so they get the same endpoint privacy trim a real account's
// own uploads would.
const demoActivityPrivacyTrimM = 200

//go:embed demo_data/*.gpx
var demoData embed.FS

// demoActivityNames overrides the display name for a handful of seeded files that read
// better with a real destination/errand name than the generic "Car Ride - <date>" every
// other file gets — the four out-of-town trips (each originally its own subfolder before
// being flattened into demo_data/) and two identifiable local errands. Applied after every
// ingest.Process call, keyed by embedded filename, regardless of whether that call actually
// persisted a new row or found one already there (ON CONFLICT DO NOTHING still returns the
// existing row's id) — so re-running the seed always leaves these names correct rather than
// only setting them the one time a row is first inserted.
var demoActivityNames = map[string]string{
	"Car Ride 2026-03-14.gpx": "Niagara Waterfall",
	"Car Ride 2026-05-22.gpx": "Chicago",
	"Car Ride 2026-06-05.gpx": "Cider Point",
	"Car Ride 2026-07-13.gpx": "Ocean City",
	"Car Ride 2026-06-04.gpx": "Walmart",
	"Car Ride 2026-08-17.gpx": "The Home Depot",
}

// SeedDemoCustomer ingests every embedded GPX file in demo_data/ into DemoCustomerUserID,
// through the same real pipeline (internal/ingest.Process — parse, persist, fog/heatmap mask
// render) a real upload uses. Meant to run once, out of band (the `seed-demo-customer` CLI
// subcommand), not per visitor — see docs/ROADMAP.md's "Email verification + demo without real
// ingest" for why re-running this per "Try Demo" click would be far too slow at this file
// count. ingest.go's `ON CONFLICT (user_id, source, external_id) DO NOTHING` makes re-running
// this safe: already-ingested files are skipped, not duplicated.
func SeedDemoCustomer(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, log *slog.Logger) error {
	entries, err := fs.ReadDir(demoData, "demo_data")
	if err != nil {
		return fmt.Errorf("seed demo customer: read embedded demo_data: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gpx") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var ingested, skipped, failed int
	for _, filename := range names {
		b, err := demoData.ReadFile("demo_data/" + filename)
		if err != nil {
			log.Error("demo customer seed: read embedded file failed", "err", err, "file", filename)
			failed++
			continue
		}

		externalID := "demo-customer-" + strings.TrimSuffix(filename, ".gpx")
		rawKey := "raw/" + DemoCustomerUserID + "/demo-history/" + externalID + ".gpx"
		if err := store.Put(ctx, rawKey, bytes.NewReader(b), int64(len(b))); err != nil {
			log.Error("demo customer seed: upload failed", "err", err, "file", filename)
			failed++
			continue
		}

		job := ingest.Job{
			UserID:        DemoCustomerUserID,
			Source:        "demo-customer-history",
			SourceDetail:  filename,
			ExternalID:    externalID,
			RawPayloadKey: rawKey,
			PrivacyTrimM:  demoActivityPrivacyTrimM,
		}
		result, err := ingest.Process(ctx, pool, store, job)
		if err != nil {
			log.Error("demo customer seed: ingest failed", "err", err, "file", filename)
			failed++
			continue
		}
		if result.Persisted {
			ingested++
		} else {
			skipped++
		}

		if name, ok := demoActivityNames[filename]; ok {
			if _, err := pool.Exec(ctx, `UPDATE activities SET name = $1 WHERE id = $2`, name, result.ActivityID); err != nil {
				log.Error("demo customer seed: name override failed", "err", err, "file", filename)
				failed++
			}
		}
	}

	log.Info("demo customer seed: done", "ingested", ingested, "already_present", skipped, "failed", failed, "total", len(names))
	if failed > 0 {
		return fmt.Errorf("seed demo customer: %d of %d files failed", failed, len(names))
	}
	return nil
}
