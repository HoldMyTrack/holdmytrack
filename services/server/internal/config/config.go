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
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:  env("DATABASE_URL", ""),
		S3Endpoint:   env("S3_ENDPOINT", ""),
		S3Bucket:     env("S3_BUCKET", "fitmap-dev"),
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
	if c.DatabaseURL == "" {
		// Built from the POSTGRES_* Compose-interpolation vars in .env.example, the same
		// values db's compose service is configured with, rather than requiring a second
		// full DSN to be kept in sync by hand.
		user := env("POSTGRES_USER", "fitmap")
		pass := env("POSTGRES_PASSWORD", "fitmap")
		name := env("POSTGRES_DB", "fitmap")
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
