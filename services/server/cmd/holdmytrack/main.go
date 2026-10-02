// cmd/holdmytrack is the one binary, two modes, per docs/ARCHITECTURE.md §1.2 and
// services/server/README.md: `serve`, `work`, `migrate` subcommands, not separate binaries.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Embeds Go's own copy of the IANA timezone database into the binary — needed for
	// time.LoadLocation(user's stored timezone) to work regardless of whether the deployed
	// image (services/server/Dockerfile's distroless base) happens to carry
	// /usr/share/zoneinfo, rather than depending on that being true.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/config"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/db"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/geo"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/httpapi"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mail"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mapstyle"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/spots"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/worker"
)

// gitSHA is set at link time via -ldflags "-X main.gitSHA=..." (services/server/Dockerfile's
// GIT_SHA build arg) — /healthz reports it so a browser tab can detect it's running against a
// stale build, and so a bug report can be tied to the exact commit that produced it.
var gitSHA = "unknown"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: holdmytrack <serve|work|migrate|seed-demo-customer|export-demo-activities|seed-admin-boundaries|rerender-coverage|import-spots|set-admin>")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// db and the object store are separate compose services with their own startup time; retrying
	// here means api/worker/migrate don't need a fragile healthcheck-based `depends_on`
	// condition on the object store specifically (see compose.yaml's comment on that).
	var pool *pgxpool.Pool
	err = retry(ctx, log, "db connect", func() (err error) {
		pool, err = db.Open(ctx, cfg.DatabaseURL)
		return err
	})
	if err != nil {
		log.Error("db open", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	switch os.Args[1] {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		log.Info("migrate: up to date")

	case "serve":
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		if err := retry(ctx, log, "storage ensure bucket", func() error { return store.EnsureBucket(ctx) }); err != nil {
			log.Error("storage bucket", "err", err)
			os.Exit(1)
		}
		// SMTP_FROM falls back to SMTP_USERNAME (the common case: the account you're
		// authenticating as is also the one you're sending as) rather than requiring both to
		// be set for the same address.
		smtpFrom := cfg.SMTPFrom
		if smtpFrom == "" {
			smtpFrom = cfg.SMTPUsername
		}
		mailer := mail.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, smtpFrom, log)
		webFS, webReload := web.Embedded(), false
		if cfg.WebDevDir != "" {
			webFS, webReload = os.DirFS(cfg.WebDevDir), true
		}
		pages, err := web.New(webFS, webReload, gitSHA, cfg.AppBaseURL)
		if err != nil {
			log.Error("page templates", "err", err)
			os.Exit(1)
		}
		srv := httpapi.New(pool, store, log, mailer, cfg.AppBaseURL, cfg.BasemapOrigin, mapstyle.Satellite{
			Tiles: cfg.SatelliteTiles, TileSize: cfg.SatelliteTileSize, MaxZoom: cfg.SatelliteMaxZoom, Attribution: cfg.SatelliteAttribution,
		}, gitSHA, cfg.SkipEmailVerification, httpapi.GoogleOAuthConfig{
			ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret, RedirectURL: cfg.GoogleRedirectURL,
		}, httpapi.FacebookOAuthConfig{
			AppID: cfg.FacebookAppID, AppSecret: cfg.FacebookAppSecret, RedirectURL: cfg.FacebookRedirectURL,
		}, pages)
		// No ReadTimeout or WriteTimeout: a 512 MiB archive over a slow uplink is a legitimate
		// request that takes minutes. ReadHeaderTimeout and IdleTimeout still free a connection
		// that sends nothing useful.
		httpSrv := &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           srv,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		log.Info("serve: listening", "addr", cfg.ListenAddr)
		// ListenAndServe returns as soon as Shutdown starts, so main waits for the drain to end
		// before returning and closing the pool under requests still running. 8 s fits inside
		// docker stop's default 10 s grace.
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil {
				log.Error("serve: shutdown", "err", err)
			}
		}()
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
		<-drained

	case "work":
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		log.Info("work: polling")
		if err := worker.Run(ctx, pool, store, log); err != nil {
			log.Error("work", "err", err)
			os.Exit(1)
		}

	case "seed-demo-customer":
		// One-time (idempotent — safe to re-run on redeploy) seed of the persistent demo
		// account's activity history — httpapi.SeedDemoCustomer's own doc comment explains
		// why this runs here, out of band, rather than per "Try Demo" request. --reset wipes
		// the account's existing activities first, for when demo_data/ itself changed.
		reset := len(os.Args) == 3 && os.Args[2] == "--reset"
		if len(os.Args) > 3 || (len(os.Args) == 3 && !reset) {
			fmt.Fprintln(os.Stderr, "usage: holdmytrack seed-demo-customer [--reset]")
			os.Exit(2)
		}
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		if err := retry(ctx, log, "storage ensure bucket", func() error { return store.EnsureBucket(ctx) }); err != nil {
			log.Error("storage bucket", "err", err)
			os.Exit(1)
		}
		log.Info("seed-demo-customer: starting", "reset", reset)
		if err := httpapi.SeedDemoCustomer(ctx, pool, store, log, reset); err != nil {
			log.Error("seed-demo-customer", "err", err)
			os.Exit(1)
		}
		log.Info("seed-demo-customer: done")

	case "export-demo-activities":
		// Writes picked activities out of this deployment, as their owner sees them, as demo_data/ GPX files for
		// the Demo Customer's history — httpapi.ExportDemoActivities's doc comment has the
		// details. Writes only to the local directory given; the DB and storage are only read.
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: holdmytrack export-demo-activities <out-dir> <activity-id>...")
			os.Exit(2)
		}
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		files, err := httpapi.ExportDemoActivities(ctx, pool, store, os.Args[2], os.Args[3:])
		for _, f := range files {
			log.Info("export-demo-activities: wrote", "file", f)
		}
		if err != nil {
			log.Error("export-demo-activities", "err", err)
			os.Exit(1)
		}
		log.Info("export-demo-activities: done", "files", len(files))

	case "rerender-coverage":
		// One-off, on deploying a change to how Fog/Heatmap tiles are drawn (docs/DEPLOY.md):
		// queues a full re-render of every account's tiles, or of one account's with --user;
		// --masks redraws every activity's stored masks first, for a change to the stroke.
		// Idempotent. See internal/ingest.RerenderCoverage's doc comment.
		userID, masks := "", false
		for i := 2; i < len(os.Args); i++ {
			switch {
			case os.Args[i] == "--masks":
				masks = true
			case os.Args[i] == "--user" && i+1 < len(os.Args):
				userID = os.Args[i+1]
				i++
			default:
				fmt.Fprintln(os.Stderr, "usage: holdmytrack rerender-coverage [--masks] [--user <id>]")
				os.Exit(2)
			}
		}
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		if err := ingest.RerenderCoverage(ctx, pool, store, log, userID, masks); err != nil {
			log.Error("rerender-coverage", "err", err)
			os.Exit(1)
		}

	case "seed-admin-boundaries":
		// One-time (idempotent — safe to re-run on redeploy) load of the Natural Earth
		// country/region polygons backing Fog/Heatmap's Country/Region zoom tiers, plus a
		// backfill of activity_country/activity_region for every activity ingested before
		// this feature existed. See internal/geo.SeedAdminBoundaries's own doc comment.
		log.Info("seed-admin-boundaries: starting")
		if err := geo.SeedAdminBoundaries(ctx, pool, log); err != nil {
			log.Error("seed-admin-boundaries", "err", err)
			os.Exit(1)
		}
		log.Info("seed-admin-boundaries: done")

	case "import-spots":
		// One-time (upserted — safe to re-run) load of the Spots places from an OpenStreetMap
		// extract made off-box (docs/DEPLOY.md). See internal/spots.Import's doc comment.
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: holdmytrack import-spots <file.geojsonseq>")
			os.Exit(2)
		}
		f, err := os.Open(os.Args[2])
		if err != nil {
			log.Error("import-spots", "err", err)
			os.Exit(1)
		}
		log.Info("import-spots: starting", "file", os.Args[2])
		stats, err := spots.Import(ctx, pool, log, f)
		f.Close()
		if err != nil {
			log.Error("import-spots", "err", err, "imported", stats.Imported)
			os.Exit(1)
		}
		log.Info("import-spots: done", "imported", stats.Imported, "skipped", stats.Skipped)

	case "set-admin":
		// Grants or revokes the admin panel (/admin, FR-12) — deliberately a shell-only
		// operation: nothing on the web can make an account an admin.
		if len(os.Args) != 4 || (os.Args[3] != "true" && os.Args[3] != "false") {
			fmt.Fprintln(os.Stderr, "usage: holdmytrack set-admin <email> true|false")
			os.Exit(2)
		}
		admin := os.Args[3] == "true"
		if err := httpapi.SetAdmin(ctx, pool, os.Args[2], admin); err != nil {
			log.Error("set-admin", "err", err)
			os.Exit(1)
		}
		log.Info("set-admin: done", "email", os.Args[2], "admin", admin)

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q; want serve, work, migrate, seed-demo-customer, export-demo-activities, seed-admin-boundaries, import-spots, or set-admin\n", os.Args[1])
		os.Exit(2)
	}
}

// retry gives dependency services (db, object store) room to finish starting up without requiring
// a healthcheck for each one specifically. 15 attempts at 2s apart is 30s, comfortably past
// a cold `docker compose up` on this stack.
func retry(ctx context.Context, log *slog.Logger, what string, fn func() error) error {
	const attempts = 15
	const delay = 2 * time.Second
	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		log.Warn("retrying", "what", what, "attempt", i, "err", err)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("%s: giving up after %d attempts: %w", what, attempts, err)
}
