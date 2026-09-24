// Package config reads the environment variables compose.yaml's backend services pass in.
// See services/server/README.md's "Open decisions -> Config": env vars, not a config file,
// matching the convention already settled for the web client (root .env.example).
package config

import (
	"fmt"
	"os"
)

// Config is everything serve/work/migrate need to reach Postgres and the object store.
type Config struct {
	DatabaseURL string
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	// ListenAddr is only read by `serve`.
	ListenAddr string
	// SMTP* are all optional — an empty SMTPHost is what selects internal/mail's log-only
	// sender instead of a real one (see its own doc comment for why that's a valid default,
	// not a placeholder). Only read by `serve`, same as ListenAddr.
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	// AppBaseURL is the frontend origin a password-reset email's link points back into —
	// the same address corsAllowedOrigins (httpapi/server.go) already has to know, just on
	// the sending side instead of the receiving one. Only read by `serve`.
	AppBaseURL string
	// BasemapOrigin is where the basemap archive, fonts and sprites actually live — the
	// origin the served style document (internal/mapstyle, ARCHITECTURE.md §2.1) resolves
	// its asset URLs against. Mirrors the web client's VITE_BASEMAP_ORIGIN
	// (apps/web/src/map/config.ts's basemapOrigin) and defaults the same way it does: the
	// app's own origin, since every deployment that bundles the basemap alongside itself
	// serves it from there. Set it only for a deployment reading the archive from object
	// storage or a CDN. Only read by `serve`.
	BasemapOrigin string
	// SkipEmailVerification bypasses docs/ROADMAP.md's email-verification gate entirely —
	// every new signup is created already verified, and no verification email is sent. Off
	// by default; compose.yaml's `test` profile is the one place this is turned on, since
	// apps/web/tests/smoke.mjs and build.mjs sign up a fixture account and expect the map to
	// mount immediately with no token to fetch out of a mailbox that doesn't exist in CI.
	// Never set this in a real deployment. Only read by `serve`.
	SkipEmailVerification bool
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:  env("DATABASE_URL", ""),
		S3Endpoint:   env("S3_ENDPOINT", ""),
		S3Bucket:     env("S3_BUCKET", "holdmytrack-dev"),
		S3AccessKey:  env("S3_ACCESS_KEY", ""),
		S3SecretKey:  env("S3_SECRET_KEY", ""),
		ListenAddr:   env("LISTEN_ADDR", ":8080"),
		SMTPHost:     env("SMTP_HOST", ""),
		SMTPPort:     env("SMTP_PORT", "587"),
		SMTPUsername: env("SMTP_USERNAME", ""),
		SMTPPassword: env("SMTP_PASSWORD", ""),
		SMTPFrom:     env("SMTP_FROM", ""),
		AppBaseURL:   env("APP_BASE_URL", "http://localhost:5173"),
	}
	c.SkipEmailVerification = env("SKIP_EMAIL_VERIFICATION", "") == "true"
	if c.BasemapOrigin = env("BASEMAP_ORIGIN", ""); c.BasemapOrigin == "" {
		// Deliberately derived from AppBaseURL rather than given its own default, for the
		// same reason auth.go derives the session cookie's Secure flag from it: two env
		// vars that must agree are two env vars that can disagree.
		c.BasemapOrigin = c.AppBaseURL
	}
	if c.DatabaseURL == "" {
		// Built from the POSTGRES_* Compose-interpolation vars in .env.example, the same
		// values db's compose service is configured with, rather than requiring a second
		// full DSN to be kept in sync by hand.
		user := env("POSTGRES_USER", "holdmytrack")
		pass := env("POSTGRES_PASSWORD", "holdmytrack")
		name := env("POSTGRES_DB", "holdmytrack")
		host := env("POSTGRES_HOST", "db")
		port := env("POSTGRES_PORT_INTERNAL", "5432") // container-internal, not the published host port
		c.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, name)
	}
	if c.S3Endpoint == "" {
		return c, fmt.Errorf("config: S3_ENDPOINT is required")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
