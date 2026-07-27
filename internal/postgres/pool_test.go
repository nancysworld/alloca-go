// This test is deliberately *not* behind the integration tag. It pins the precondition
// OpenPool enforces on its own arguments, which is a property of this package's code and
// is decided before any connection is attempted — so no database is needed to prove it.

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/config"
)

// unreachableDSN is syntactically valid and points at a port nothing listens on, so a
// build that skipped budget validation would get as far as the connectivity check and
// fail there instead. That is what makes the assertions below discriminating: they
// require the error to name the offending budget field, not merely to be an error.
const unreachableDSN = "postgres://alloca:alloca@127.0.0.1:1/alloca"

// TestOpenPoolRejectsUnvalidatedBudget covers the precondition that OpenPool cannot
// safely assume: that the budget it renders into session settings has already passed
// validation. A zero LockTimeout or StatementTimeout becomes "0ms", which PostgreSQL
// reads as "no timeout" — the pool would come up with its database bounds silently
// disabled rather than fail — so the check must reject the budget before the pool
// exists, and must do so ahead of the connectivity check that would otherwise mask it.
func TestOpenPoolRejectsUnvalidatedBudget(t *testing.T) {
	zeroLock := validBudget()
	zeroLock.LockTimeout = 0

	zeroStatement := validBudget()
	zeroStatement.StatementTimeout = 0

	inverted := validBudget()
	inverted.LockTimeout, inverted.StatementTimeout = inverted.StatementTimeout, inverted.LockTimeout

	cases := []struct {
		name   string
		budget config.RequestBudget
		want   string // substring identifying the field the error must blame
	}{
		{
			// The zero value of the struct: the state a caller reaches by forgetting to
			// populate the budget at all, and the one that disables every database bound.
			name:   "zero budget",
			budget: config.RequestBudget{},
			want:   "ClientDeadline",
		},
		{
			name:   "zero lock_timeout",
			budget: zeroLock,
			want:   "LockTimeout",
		},
		{
			name:   "zero statement_timeout",
			budget: zeroStatement,
			want:   "StatementTimeout",
		},
		{
			// Correctly positive but wrongly ordered: the statement bound would fire before
			// the lock bound, so a lock wait would surface as a generic statement timeout
			// and the innermost-layer-first property of §8.1 would not hold.
			name:   "inverted nesting",
			budget: inverted,
			want:   "lock_timeout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := OpenPool(context.Background(), unreachableDSN, tc.budget)
			if pool != nil {
				pool.Close()
				t.Error("OpenPool returned a pool for an invalid budget, want nil")
			}
			if err == nil {
				t.Fatal("OpenPool accepted an invalid budget, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("OpenPool error = %q, want it to blame %s", err, tc.want)
			}
		})
	}
}

// TestOpenPoolValidationPrecedesConnection pins the ordering the test above relies on:
// an invalid budget is rejected on its own terms, not as a side effect of the database
// being unreachable. A budget error must win even over an unparseable DSN, which proves
// nothing was attempted before the check.
func TestOpenPoolValidationPrecedesConnection(t *testing.T) {
	_, err := OpenPool(context.Background(), "not-a-dsn://", config.RequestBudget{})
	if err == nil {
		t.Fatal("OpenPool accepted an invalid budget and a malformed DSN, want error")
	}
	if strings.Contains(err.Error(), "parse dsn") {
		t.Errorf("OpenPool error = %q, want the budget rejected before the DSN is parsed", err)
	}
}

// validBudget is the smallest budget satisfying §8.1 clause 1, used as the base each
// case perturbs in exactly one way. It is deliberately not the integration tests'
// testBudget: those values are chosen so real lock waits complete, whereas these only
// have to be well-ordered, and nothing here ever reaches a database.
func validBudget() config.RequestBudget {
	return config.RequestBudget{
		ClientDeadline:   7 * time.Second,
		ServerDeadline:   6 * time.Second,
		AdmissionCap:     time.Second,
		DBAcquireCap:     time.Second,
		LockTimeout:      4 * time.Second,
		StatementTimeout: 5 * time.Second,
		TxnBudget:        5 * time.Second,
	}
}
