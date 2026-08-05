//go:build integration

package postgres

import (
	"context"
	"testing"
)

// The stubbed tests in schema_test.go prove the comparison; only this proves the query.
//
// A wrong table name, a wrong predicate, or a scan shape pgx rejects would pass every
// stubbed case and fail here — and would fail at *startup*, on a service that had just been
// deployed, which is the least convenient place to discover it. TestMain has already
// migrated this database, so the version it reports is the one this binary embeds.
func TestCheckSchemaAgainstTheMigratedDatabase(t *testing.T) {
	repo := newRepo(t, testBudget())

	applied, err := CheckSchema(context.Background(), repo.pool)
	if err != nil {
		t.Fatalf("the gate refused a database migrated by this very binary: %v", err)
	}

	expected, err := ExpectedSchemaVersion()
	if err != nil {
		t.Fatalf("reading the expected version: %v", err)
	}
	if applied != expected {
		t.Errorf("applied version = %d, want %d: TestMain migrated with these embedded migrations",
			applied, expected)
	}
	if applied <= 0 {
		t.Errorf("applied version = %d; a migrated database reports a positive version, so a "+
			"zero here means the query read something other than the goose bookkeeping", applied)
	}
}
