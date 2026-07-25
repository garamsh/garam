// Command gaon wires the HTTP API to the Postgres stores and serves
// it. Configuration is entirely via environment variables:
//
//	GAON_DATABASE_URL     postgres connection string (required)
//	GAON_MIGRATIONS_DIR   migrations directory (default "migrations")
//	GAON_HTTP_ADDR        listen address (default ":8080")
//	GAON_SESSION_TTL      session lifetime (default "168h")
//	GAON_COOKIE_SECURE    "true" to mark cookies Secure (default true)
//	GOOGLE_CLIENT_ID      Google OAuth client ID (required)
//	GOOGLE_CLIENT_SECRET  Google OAuth client secret (required)
//	GOOGLE_REDIRECT_URL   OAuth callback URL (required)
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/httpapi"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	databaseURL := os.Getenv("GAON_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("GAON_DATABASE_URL is required")
	}
	if err := migrateUp(databaseURL, envOr("GAON_MIGRATIONS_DIR", "migrations")); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	google, err := authn.NewGoogleProvider(ctx, authn.GoogleConfig{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
	})
	if err != nil {
		return fmt.Errorf("google provider: %w", err)
	}

	ttl, err := time.ParseDuration(envOr("GAON_SESSION_TTL", "168h"))
	if err != nil {
		return fmt.Errorf("GAON_SESSION_TTL: %w", err)
	}
	srv := httpapi.NewServer(
		user.NewPostgresStore(pool),
		org.NewPostgresStore(pool),
		authn.NewPostgresSessionStore(pool),
		[]authn.Provider{google},
		httpapi.Config{
			SessionTTL:   ttl,
			CookieSecure: envOr("GAON_COOKIE_SECURE", "true") == "true",
		},
	)

	addr := envOr("GAON_HTTP_ADDR", ":8080")
	log.Printf("listening on %s", addr)
	return http.ListenAndServe(addr, api.Handler(srv))
}

// migrateUp applies all pending migrations.
func migrateUp(databaseURL, dir string) error {
	m, err := migrate.New("file://"+dir, databaseURL)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
