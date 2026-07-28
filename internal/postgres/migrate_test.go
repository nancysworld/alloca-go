//go:build integration

package postgres

import (
	"context"
	"testing"
)

// schemaTables is every table the migrations create, in no particular order. Kept in one
// place so a test asserting the schema exists and a test asserting it was fully removed
// cannot disagree about what "the schema" is.
var schemaTables = []string{"slots", "reservations", "bookings", "idempotency_records", "user_time_claims"}

// Migrations are applied once by TestMain, so this asserts the properties that make
// the schema trustworthy rather than re-running Up.
//
// Re-applying is checked because a rollout runs alloca-migrate on every deploy,
// including deploys that change no schema: if Up were not idempotent, an ordinary
// release would fail at the migration step.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("re-applying migrations: %v", err)
	}

	repo := newRepo(t, testBudget())
	for _, table := range schemaTables {
		if !tableExists(t, repo, table) {
			t.Errorf("table %s does not exist after migration", table)
		}
	}
}

// Every migration's Down block rolls the schema back, and Up then rebuilds it.
//
// An untested rollback is not a rollback: a Down block that has never run is a guess
// about what Up did, and the first time it matters is a failed deploy under time
// pressure. This rolls the whole schema back to nothing and rebuilds it, so each Down
// block is executed against the state its own Up produced — including the ordering
// constraint that user_time_claims must go before the reservations and slots it
// references.
//
// The rollback count comes from the embedded migration files rather than a literal, so
// adding a migration extends this test automatically instead of silently leaving the new
// Down block unexercised.
func TestMigrationsRollBackAndReapply(t *testing.T) {
	ctx := context.Background()

	// Every other test in the package expects a migrated database, and this one is
	// deliberately destructive. Restoring in Cleanup rather than at the end of the body
	// means an assertion failure below cannot cascade into unrelated failures.
	t.Cleanup(func() {
		if err := Migrate(ctx, databaseURL); err != nil {
			t.Errorf("restore schema after rollback test: %v", err)
		}
	})

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	repo := newRepo(t, testBudget())

	for i := range entries {
		if err := MigrateDown(ctx, databaseURL); err != nil {
			t.Fatalf("roll back migration %d of %d: %v", i+1, len(entries), err)
		}
	}
	for _, table := range schemaTables {
		if tableExists(t, repo, table) {
			t.Errorf("table %s survived a full rollback: the Down block does not undo its Up", table)
		}
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("re-apply migrations after rollback: %v", err)
	}
	for _, table := range schemaTables {
		if !tableExists(t, repo, table) {
			t.Errorf("table %s missing after re-applying migrations", table)
		}
	}

	// The rebuilt schema must carry its constraints, not merely its tables. The
	// exclusion constraint is the whole user schedule authority (§2.2), and it depends on
	// an extension that a rollback dropped — so a Down/Up cycle that restored the table
	// without it would leave the invariant silently unenforced.
	var constraint string
	err = repo.pool.QueryRow(ctx,
		`SELECT conname FROM pg_constraint WHERE conname = 'user_time_claims_no_overlap'`).Scan(&constraint)
	if err != nil {
		t.Errorf("exclusion constraint missing after rollback and re-apply: %v", err)
	}
}

func tableExists(t *testing.T, repo *Repo, table string) bool {
	t.Helper()
	var exists bool
	err := repo.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		table).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

// The scoped-key primary key is the concurrency backstop of transaction-semantics
// §5.3. It is asserted directly because the constraint's existence — not the Go code
// around it — is what makes a second insert for the same key fail rather than
// overwrite the first.
func TestIdempotencyScopeIsTheTableIdentity(t *testing.T) {
	repo := newRepo(t, testBudget())

	rows, err := repo.pool.Query(context.Background(), `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = 'idempotency_records'::regclass AND i.indisprimary
		ORDER BY a.attname`)
	if err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read primary key: %v", err)
	}

	want := []string{"key", "operation", "organisation_id", "user_id"}
	if len(columns) != len(want) {
		t.Fatalf("primary key columns = %v, want %v", columns, want)
	}
	for i := range want {
		if columns[i] != want[i] {
			t.Errorf("primary key columns = %v, want %v", columns, want)
			break
		}
	}
}
