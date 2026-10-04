package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/garamsh/garam/internal/errorx"
)

// ownerMarker is the comment prepare sets on every database it
// creates. drop refuses a database without it, so a name that merely
// has the right shape is not enough to lose one.
const ownerMarker = "disposable database created by garam tests/testprincipal"

// databasePrefix and databaseName are the only names prepare creates
// and drop accepts.
const databasePrefix = "garam_principal_"

var databaseName = regexp.MustCompile(`^garam_principal_[0-9a-f]{16}$`)

// randomSuffix answers 16 random hex digits. CREATE DATABASE refuses
// a name already taken, so an existing database is never reused.
func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "random suffix")
	}
	return hex.EncodeToString(b[:]), nil
}

// createOwned creates name on the server admin reaches and marks it as
// this command's.
func createOwned(ctx context.Context, admin *pgxpool.Config, name string) error {
	conn, err := pgx.ConnectConfig(ctx, admin.ConnConfig)
	if err != nil {
		return errorx.Wrap(err, "connect to the Postgres server", errorx.KindUnavailable)
	}
	defer func() { _ = conn.Close(ctx) }()
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return errorx.Wrap(err, "create database "+name)
	}
	// The marker is a constant with no quote in it; COMMENT takes no
	// parameter.
	if _, err := conn.Exec(ctx, "COMMENT ON DATABASE "+ident+" IS '"+ownerMarker+"'"); err != nil {
		// Unmarked, drop would refuse it later; it was created a
		// statement ago, so it is this command's to remove now.
		_, _ = conn.Exec(ctx, "DROP DATABASE "+ident)
		return errorx.Wrap(err, "mark database "+name)
	}
	return nil
}

// dropOwned drops name, refusing one this command did not create.
// Without FORCE: a database still in use is refused by Postgres and
// stays.
func dropOwned(ctx context.Context, admin *pgxpool.Config, name string) error {
	if !databaseName.MatchString(name) {
		return errorx.New(errorx.KindInvalidArgument, "refusing to drop a database whose name prepare did not generate")
	}
	conn, err := pgx.ConnectConfig(ctx, admin.ConnConfig)
	if err != nil {
		return errorx.Wrap(err, "connect to the Postgres server", errorx.KindUnavailable)
	}
	defer func() { _ = conn.Close(ctx) }()
	var marker *string
	err = conn.QueryRow(ctx,
		"SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = $1", name).Scan(&marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return errorx.New(errorx.KindNotFound, "no database "+name+" on this server")
	}
	if err != nil {
		return errorx.Wrap(err, "read the marker of "+name)
	}
	if marker == nil || *marker != ownerMarker {
		return errorx.New(errorx.KindFailedPrecondition, "refusing to drop "+name+": it does not carry the marker prepare sets")
	}
	if _, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return errorx.Wrap(err, "drop database "+name+" (stop every process using it first)")
	}
	return nil
}

// migrateUp applies the migrations in dir — the directory `garam
// migrate` reads — to the database cfg reaches.
func migrateUp(cfg *pgxpool.Config, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return errorx.Wrap(err, "migrations directory", errorx.KindInvalidArgument)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return errorx.New(errorx.KindInvalidArgument, "-migrations must name the migrations directory of the Garam checkout this command was built from")
	}
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = db.Close() }()
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return errorx.Wrap(err, "migrations", errorx.KindUnavailable)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+abs, "postgres", driver)
	if err != nil {
		return errorx.Wrap(err, "migrations")
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errorx.Wrap(err, "migrations")
	}
	return nil
}
