// These tests are deliberately *not* behind the integration tag: they pin failure
// classification and cleanup bounding, which are properties of this package's own
// mapping code rather than of PostgreSQL's behaviour. A real database can be made to
// exhibit a lock timeout (time_test.go does exactly that), but it cannot reliably be
// made to time out a COMMIT or to stall a rollback on demand, so the discriminating
// evidence for those paths has to come from the mapping functions directly.

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
)

func classifyRepo() *Repo {
	return &Repo{budget: config.RequestBudget{
		DBAcquireCap:     5 * time.Second,
		LockTimeout:      time.Second,
		StatementTimeout: 2 * time.Second,
		TxnBudget:        3 * time.Second,
	}}
}

// The cleanup context has to hold two properties at once, and the obvious
// implementations each hold only one: context.WithoutCancel alone survives the
// caller's cancellation but has no deadline, and context.WithTimeout(ctx, …) alone is
// bounded but is already expired on the path that matters most — the one where the
// transaction is being abandoned *because* ctx died. Each sub-test below fails against
// one of those two.
func TestCleanupContextOutlivesCancellationAndStaysBounded(t *testing.T) {
	r := classifyRepo()

	t.Run("survives a cancelled parent", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()

		cleanupCtx, cancelCleanup := r.cleanupContext(parent)
		defer cancelCleanup()

		if err := cleanupCtx.Err(); err != nil {
			t.Fatalf("cleanup context is already done (%v): the rollback would be refused "+
				"before reaching PostgreSQL, leaving the transaction open", err)
		}
	})

	t.Run("survives an expired parent deadline", func(t *testing.T) {
		parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		cleanupCtx, cancelCleanup := r.cleanupContext(parent)
		defer cancelCleanup()

		if err := cleanupCtx.Err(); err != nil {
			t.Fatalf("cleanup context is already done (%v), want a live context", err)
		}
	})

	t.Run("stays bounded", func(t *testing.T) {
		cleanupCtx, cancelCleanup := r.cleanupContext(context.Background())
		defer cancelCleanup()

		deadline, ok := cleanupCtx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline: a stalled rollback would block forever, " +
				"pinning the connection and eventually draining the pool")
		}
		if remaining := time.Until(deadline); remaining > r.budget.StatementTimeout {
			t.Errorf("cleanup deadline is %v away, want at most StatementTimeout (%v)",
				remaining, r.budget.StatementTimeout)
		}
	})
}

// A COMMIT the server rejects is a *definite* fault — but which definite fault matters.
// PostgreSQL applies statement_timeout to COMMIT like any other statement, so a commit
// that outruns its database bound must stay timeout_db; reporting it as a generic
// rejection would classify it as internal_failure and drop a timeout_db from the
// outcome mix precisely when the database layer was the binding bound (§6).
func TestClassifyCommitTimeoutSQLSTATEs(t *testing.T) {
	r := classifyRepo()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	live := context.Background()

	cases := []struct {
		name    string
		ctx     context.Context
		err     error
		wantIs  error // sentinel the error must wrap, nil for none of them
		wantOut domain.Outcome
	}{
		{
			name:    "statement_timeout on commit",
			ctx:     live,
			err:     &pgconn.PgError{Code: sqlstateQueryCanceled, Message: "canceling statement due to statement timeout"},
			wantIs:  domain.ErrDBTimeout,
			wantOut: domain.OutcomeTimeoutDB,
		},
		{
			name:    "lock_timeout on commit",
			ctx:     live,
			err:     &pgconn.PgError{Code: sqlstateLockNotAvailable, Message: "canceling statement due to lock timeout"},
			wantIs:  domain.ErrDBTimeout,
			wantOut: domain.OutcomeTimeoutDB,
		},
		{
			// 57014 is also what the server reports when it honours a client cancel
			// request. The database bound was not the binding one, so this stays on the
			// client line — the same distinction mapError draws for statements.
			name:    "caller cancellation the server honoured",
			ctx:     cancelled,
			err:     &pgconn.PgError{Code: sqlstateQueryCanceled, Message: "canceling statement due to user request"},
			wantIs:  context.Canceled,
			wantOut: domain.OutcomeTimeoutClient,
		},
		{
			// Any other server rejection is definite and not a timeout: it must not be
			// laundered into unknown_replayable, which would tell a client to replay a
			// commit that provably never happened.
			name:    "server rejected the commit",
			ctx:     live,
			err:     &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"},
			wantOut: domain.OutcomeInternalFailure,
		},
		{
			// No PgError: the server never answered, so whether it committed is genuinely
			// unknown and only the same idempotency key resolves it (§5.4).
			name:    "acknowledgement lost",
			ctx:     live,
			err:     errors.New("write tcp: broken pipe"),
			wantIs:  domain.ErrCommitUnknown,
			wantOut: domain.OutcomeUnknownReplayable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.classifyCommit(tc.ctx, tc.ctx, tc.err)
			if got == nil {
				t.Fatal("classifyCommit returned nil for a failed commit")
			}
			if tc.wantIs != nil && !errors.Is(got, tc.wantIs) {
				t.Errorf("err = %v, want it to wrap %v", got, tc.wantIs)
			}
			if out := domain.ClassifyFault(got); out != tc.wantOut {
				t.Errorf("ClassifyFault(%v) = %q, want %q", got, out, tc.wantOut)
			}
			// A definite rejection must never be reported as an ambiguous one: a client
			// told "unknown" replays, and replaying a commit that definitely failed is
			// only safe by accident.
			if tc.wantOut != domain.OutcomeUnknownReplayable && errors.Is(got, domain.ErrCommitUnknown) {
				t.Errorf("err = %v wraps ErrCommitUnknown, want a definite outcome", got)
			}
		})
	}
}
