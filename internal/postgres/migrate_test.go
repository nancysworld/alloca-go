//go:build integration

package postgres

import (
	"context"
	"testing"
)

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
	for _, table := range []string{"slots", "reservations", "bookings", "idempotency_records"} {
		var exists bool
		err := repo.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after migration", table)
		}
	}
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
