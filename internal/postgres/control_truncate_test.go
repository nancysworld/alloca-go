//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestTruncateIsNotGovernedByTheRequestBudget pins the fix for a CI flake that presented as
// a leak and was not one.
//
// OpenPool sets the session statement_timeout from the caller's RequestBudget. Tests that
// exercise timeout behaviour deliberately choose a tiny one — and newHarness then truncates
// on that same pool, so the *setup* inherited a bound chosen to make a lock wait trip.
// TRUNCATE ... CASCADE over six tables is occasionally slower than that on a loaded runner,
// which is how TestTransactionTimeoutsDoNotLeakAcrossTransactions failed in CI with
// "canceling statement due to statement timeout" from its own truncate.
//
// A 2ms budget makes it deterministic rather than occasional: before the fix this fails
// every time with SQLSTATE 57014.
func TestTruncateIsNotGovernedByTheRequestBudget(t *testing.T) {
	budget := testBudget()
	budget.LockTimeout = 1 * time.Millisecond
	budget.StatementTimeout = 2 * time.Millisecond

	repo := newRepo(t, budget)

	// The session default really is the tiny one, so the test is exercising what it claims.
	var setting string
	if err := repo.pool.QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&setting); err != nil {
		t.Fatalf("read statement_timeout: %v", err)
	}
	if setting != "2ms" {
		t.Fatalf("session statement_timeout = %q, want 2ms; the premise of this test is gone", setting)
	}

	if err := repo.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate under a 2ms request budget: %v\n"+
			"control-plane setup must not inherit the caller's request budget", err)
	}

	// The override is scoped to the truncate's own transaction: the pooled connection must
	// come back with the session default intact, or this fix would have introduced the very
	// leak the neighbouring test guards against.
	if err := repo.pool.QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&setting); err != nil {
		t.Fatalf("re-read statement_timeout: %v", err)
	}
	if setting != "2ms" {
		t.Errorf("session statement_timeout = %q after truncate, want 2ms: the SET LOCAL leaked", setting)
	}
}
