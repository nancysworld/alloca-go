package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
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

// ErrSchemaIncompatible reports that an authority's applied schema is not one this
// binary can serve against.
var ErrSchemaIncompatible = errors.New("postgres: schema incompatible with this binary")

// ExpectedSchemaVersion is the highest migration version this binary carries. Because
// the migrations are embedded (see migrationsFS), it is a property of the build rather
// than of whatever SQL directory happens to be on disk at deploy time.
func ExpectedSchemaVersion() (int64, error) {
	goose.SetBaseFS(migrationsFS)
	migrations, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return 0, fmt.Errorf("postgres: collect embedded migrations: %w", err)
	}
	last, err := migrations.Last()
	if err != nil {
		return 0, fmt.Errorf("postgres: no embedded migrations: %w", err)
	}
	return last.Version, nil
}

// CheckSchema verifies that this authority's applied schema can serve this binary, and
// returns the applied version so /meta can report a value that has been validated rather
// than merely read.
//
// It is a startup gate, not a diagnostic. ADR-0002 keeps migration out of the serving
// path, so a serving process cannot repair what it finds — which is exactly why it must
// refuse to start rather than discover the mismatch one failing query at a time, under
// load, on whichever request happened to touch the missing column first. A multi-authority
// deployment makes it sharper still: an authority left behind by a rollout would serve its
// organisations wrongly while its peers served theirs correctly, and the blast radius
// would follow the placement map (horizontal-database-authority §5.1).
//
// **Applied ahead of the binary is allowed.** ADR-0002's rollout migrates before the new
// version serves, so during a deploy every not-yet-replaced replica is running against a
// schema newer than itself; refusing that would make the gate reject the normal rollout.
// Additive migrations are what make it safe, and that constraint belongs to whoever writes
// the migration — this gate cannot check it, and says so rather than implying otherwise.
//
// **Applied behind the binary is refused**: the binary carries migrations that have not
// run, so it expects schema that does not exist.
// SchemaQuerier is the single read CheckSchema needs. It is an interface rather than the
// pool so the gate's refusal paths — unreadable, unapplied, behind — can be tested without
// a database; *pgxpool.Pool satisfies it.
type SchemaQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func CheckSchema(ctx context.Context, q SchemaQuerier) (applied int64, err error) {
	expected, err := ExpectedSchemaVersion()
	if err != nil {
		return 0, err
	}

	// A NULL max() — the table exists but nothing is applied — is as incompatible as a
	// missing table, so it is read into a nullable and rejected explicitly rather than
	// coerced to a zero that would compare as "very old" by accident.
	var version *int64
	if err := q.QueryRow(ctx,
		"SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		return 0, fmt.Errorf("%w: cannot read the applied schema version (expected %d): %w",
			ErrSchemaIncompatible, expected, err)
	}
	if version == nil {
		return 0, fmt.Errorf("%w: no migration is applied, but this binary expects version %d",
			ErrSchemaIncompatible, expected)
	}
	if *version < expected {
		return 0, fmt.Errorf("%w: authority is at version %d but this binary expects %d; run alloca-migrate before serving (ADR-0002)",
			ErrSchemaIncompatible, *version, expected)
	}
	return *version, nil
}
