package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationsFS embeds the schema so a binary carries the migrations it expects. A
// deployment cannot then drift from a separately-shipped SQL directory, and the
// migration a build will apply is decided at build time, not at deploy time.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending migrations against dsn.
//
// It is a deliberate, separate step (ADR-0002): serving replicas never migrate on
// startup. Several replicas racing the same DDL at rollout is a classic way to take a
// service down, and it couples a routine restart to a schema change. Migration is run
// once by cmd/alloca-migrate before the new version serves traffic.
//
// goose needs a database/sql handle, so this opens one through pgx's stdlib driver
// rather than reusing the pgxpool the adapter serves from — the handle is short-lived
// and closed here.
func Migrate(ctx context.Context, dsn string) error {
	db, err := openStdlib(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("postgres: set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("postgres: apply migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration. It exists so the Down blocks are
// exercised rather than written and never run — an untested rollback is not a
// rollback. It is a development and test affordance, not a production procedure.
func MigrateDown(ctx context.Context, dsn string) error {
	db, err := openStdlib(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("postgres: set goose dialect: %w", err)
	}
	if err := goose.DownContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("postgres: roll back migration: %w", err)
	}
	return nil
}

func openStdlib(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	return stdlib.OpenDB(*cfg), nil
}
