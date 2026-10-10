// cmd/holdmytrack is the one binary, two modes, per docs/ARCHITECTURE.md §1.2 and
// services/server/README.md: `serve`, `work`, `migrate` subcommands, not separate binaries.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
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
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
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
	// For packages that log without a logger passed in (internal/fog's render warnings).
	slog.SetDefault(log)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: holdmytrack <serve|work|migrate|seed-demo-customer|export-demo-activities|seed-admin-boundaries|seed-timezones|rerender-coverage|import-spots|set-admin>")
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
	// `work` runs several jobs at once in its main and render lanes, each rendering up to 16
	// tiles side by side (internal/fog's renderParallelism), so it gets a bigger pool than
	// pgx's default.
	var maxConns int32
	if os.Args[1] == "work" {
		maxConns = int32(4 * (cfg.WorkerConcurrency + cfg.WorkerRenderConcurrency))
	}
	var pool *pgxpool.Pool
	err = retry(ctx, log, "db connect", func() (err error) {
		pool, err = db.OpenSized(ctx, cfg.DatabaseURL, maxConns)
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
		mailer := newMailer(cfg, log, "serve")
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
		// No ReadTimeout or WriteTimeout: a 64 MiB upload, a data export's download or a photo
		// over a slow uplink is a legitimate request that takes minutes. ReadHeaderTimeout and IdleTimeout still free a connection
		// that sends nothing useful.
		httpSrv := &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           srv,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		metrics.RegisterQueue(pool, log)
		metrics.Serve(ctx, cfg.MetricsAddr, log)
		log.Info("serve: listening", "addr", cfg.ListenAddr)
		ln, err := net.Listen("tcp", cfg.ListenAddr)
		if err != nil {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
		if err := serveUntilDone(ctx, httpSrv, ln, 8*time.Second, log); err != nil {
			log.Error("serve", "err", err)
			os.Exit(1)
		}

	case "work":
		store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		metrics.Serve(ctx, cfg.MetricsAddr, log)
		log.Info("work: polling", "concurrency", cfg.WorkerConcurrency)
		notifier := &worker.Notifier{Mailer: newMailer(cfg, log, "work"), BaseURL: cfg.AppBaseURL}
		if err := worker.Run(ctx, pool, store, log, notifier, cfg.WorkerConcurrency, cfg.WorkerRenderConcurrency); err != nil {
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
		// The country/region outlines behind Fog/Heatmap's Country/Region zoom tiers, from the
		// extract scripts/boundaries-extract.sh makes (docs/DEPLOY.md §6), and a re-match of every
		// activity against them. Safe to re-run: the extract already loaded is skipped unless
		// --force. Reads geo.BoundariesKey from the app bucket, or --file's local copy. See
		// internal/geo.SeedAdminBoundaries's own doc comment.
		var file string
		force := false
		for i := 2; i < len(os.Args); i++ {
			switch {
			case os.Args[i] == "--force":
				force = true
			case os.Args[i] == "--file" && i+1 < len(os.Args):
				file = os.Args[i+1]
				i++
			default:
				fmt.Fprintln(os.Stderr, "usage: holdmytrack seed-admin-boundaries [--force] [--file <boundaries.csv.gz>]")
				os.Exit(2)
			}
		}
		var src io.ReadCloser
		name := path.Base(geo.BoundariesKey)
		if file != "" {
			src, err = os.Open(file)
			name = filepath.Base(file)
		} else {
			var store *storage.Store
			if store, err = storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket); err == nil {
				src, err = store.Get(ctx, geo.BoundariesKey)
			}
		}
		if err != nil {
			log.Error("seed-admin-boundaries", "err", err)
			os.Exit(1)
		}
		log.Info("seed-admin-boundaries: starting", "file", name, "force", force)
		stats, err := geo.SeedAdminBoundaries(ctx, pool, log, src, name, force)
		src.Close()
		if err != nil {
			log.Error("seed-admin-boundaries", "err", err)
			os.Exit(1)
		}
		if stats.Unchanged {
			log.Info("seed-admin-boundaries: already loaded, nothing to do (--force reloads it)", "file", name)
			break
		}
		log.Info("seed-admin-boundaries: done", "countries", stats.Countries, "regions", stats.Regions,
			"skipped_regions", stats.SkippedRegions, "whole_country_regions", stats.WholeCountryRegions,
			"activity_countries", stats.ActivityCountries, "activity_regions", stats.ActivityRegions)

	case "seed-timezones":
		// The timezone polygons each activity's own zone is looked up in (IMPLEMENTATION.md
		// §4.30), from timezone-boundary-builder's release zip (docs/DEPLOY.md §6), and a
		// re-match of every activity against them. Safe to re-run: the file already loaded is
		// skipped unless --force. Reads geo.TimezonesKey from the app bucket, or --file's local
		// copy. See internal/geo.SeedTimezones's own doc comment.
		var file string
		force := false
		for i := 2; i < len(os.Args); i++ {
			switch {
			case os.Args[i] == "--force":
				force = true
			case os.Args[i] == "--file" && i+1 < len(os.Args):
				file = os.Args[i+1]
				i++
			default:
				fmt.Fprintln(os.Stderr, "usage: holdmytrack seed-timezones [--force] [--file <timezones-with-oceans.geojson.zip>]")
				os.Exit(2)
			}
		}
		var src io.ReadCloser
		name := path.Base(geo.TimezonesKey)
		if file != "" {
			src, err = os.Open(file)
			name = filepath.Base(file)
		} else {
			var store *storage.Store
			if store, err = storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket); err == nil {
				src, err = store.Get(ctx, geo.TimezonesKey)
			}
		}
		if err != nil {
			log.Error("seed-timezones", "err", err)
			os.Exit(1)
		}
		log.Info("seed-timezones: starting", "file", name, "force", force)
		stats, err := geo.SeedTimezones(ctx, pool, log, src, name, force)
		src.Close()
		if err != nil {
			log.Error("seed-timezones", "err", err)
			os.Exit(1)
		}
		if stats.Unchanged {
			log.Info("seed-timezones: already loaded, nothing to do (--force reloads it)", "file", name)
			break
		}
		log.Info("seed-timezones: done", "zones", stats.Zones, "unknown_zones", len(stats.UnknownZones), "activities", stats.Activities)

	case "import-spots":
		// The load, and each quarterly refresh, of the Spots places from an OpenStreetMap extract
		// made off-box (docs/DEPLOY.md §6) — upserted, so safe to re-run. --planet says the file
		// is the whole planet, so the places it doesn't have are retired (ADR-0027). See
		// internal/spots.Import's doc comment.
		args := os.Args[2:]
		planet := len(args) > 0 && args[0] == "--planet"
		if planet {
			args = args[1:]
		}
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: holdmytrack import-spots [--planet] <file.geojsonseq>")
			os.Exit(2)
		}
		f, err := os.Open(args[0])
		if err != nil {
			log.Error("import-spots", "err", err)
			os.Exit(1)
		}
		log.Info("import-spots: starting", "file", args[0], "planet", planet)
		stats, err := spots.Import(ctx, pool, log, f, planet)
		f.Close()
		if err != nil {
			log.Error("import-spots", "err", err, "imported", stats.Imported, "changed", stats.Changed)
			os.Exit(1)
		}
		log.Info("import-spots: done", "imported", stats.Imported, "changed", stats.Changed,
			"retired", stats.Retired, "skipped", stats.Skipped)

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

// serveUntilDone serves on ln until ctx is cancelled, then shuts down, returning once requests
// still running have finished or drain has passed. Serve returns as soon as Shutdown starts,
// so this waits for the drain itself: returning then let main close the pool under requests
// still running, and the process exit cut them off. 8 s (main's drain) fits inside docker
// stop's default 10 s grace.
func serveUntilDone(ctx context.Context, srv *http.Server, ln net.Listener, drain time.Duration, log *slog.Logger) error {
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drain)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("serve: shutdown", "err", err)
		}
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-drained
	return nil
}

// newMailer is the account emails' sender, for `serve` (verification, reset) and `work` (a
// finished data export). Bodies go to the log only off a real deployment; on one, an unset
// SMTP_HOST is a misconfiguration worth a warning at start, not a reason to log live account
// links.
func newMailer(cfg config.Config, log *slog.Logger, mode string) mail.Sender {
	smtpFrom := cfg.SMTPFrom
	if smtpFrom == "" {
		smtpFrom = cfg.SMTPUsername
	}
	realDeployment := strings.HasPrefix(cfg.AppBaseURL, "https://")
	if realDeployment && cfg.SMTPHost == "" {
		log.Warn(mode + ": SMTP_HOST not set; account emails will not be sent")
	}
	return mail.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, smtpFrom, !realDeployment, log)
}
