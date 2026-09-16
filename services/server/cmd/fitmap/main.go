// cmd/fitmap is the one binary, two modes, per docs/ARCHITECTURE.md §1.2 and
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fitmap/fitmap/services/server/internal/config"
	"github.com/fitmap/fitmap/services/server/internal/db"
	"github.com/fitmap/fitmap/services/server/internal/httpapi"
	"github.com/fitmap/fitmap/services/server/internal/mail"
	"github.com/fitmap/fitmap/services/server/internal/storage"
	"github.com/fitmap/fitmap/services/server/internal/worker"
)

// gitSHA is set at link time via -ldflags "-X main.gitSHA=..." (services/server/Dockerfile's
// GIT_SHA build arg) — /healthz reports it so a browser tab can detect it's running against a
// stale build, and so a bug report can be tied to the exact commit that produced it.
var gitSHA = "unknown"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fitmap <serve|work|migrate>")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// db and minio are separate compose services with their own startup time; retrying
	// here means api/worker/migrate don't need a fragile healthcheck-based `depends_on`
	// condition on minio specifically (see compose.yaml's comment on that).
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
		if err := retry(ctx, log, "minio ensure bucket", func() error { return store.EnsureBucket(ctx) }); err != nil {
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
		srv := httpapi.New(pool, store, log, mailer, cfg.AppBaseURL, cfg.BasemapOrigin, gitSHA)
		httpSrv := &http.Server{Addr: cfg.ListenAddr, Handler: srv}
		log.Info("serve: listening", "addr", cfg.ListenAddr)
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(shutdownCtx)
		}()
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("serve", "err", err)
			os.Exit(1)
		}

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

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q; want serve, work, or migrate\n", os.Args[1])
		os.Exit(2)
	}
}

// retry gives dependency services (db, minio) room to finish starting up without requiring
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
