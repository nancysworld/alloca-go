//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// INV-21 has sat in the invariant register's *not directly proven* section since AG-M1
// for one reason: no test killed a connection mid-`COMMIT`. This is that test.
//
// The fault is deliberately timed rather than incidental. A generic shutdown tears down
// every connection at once and proves only that the service notices a dead database. What
// INV-21 asserts is narrower and more useful: a commit whose outcome is *genuinely
// unknown* must be classified `unknown_replayable` rather than as a definite failure —
// because a client told "failed" may reasonably reissue under a new key, and a new key is
// exactly what breaks the one-key-one-mutation gate (transaction-semantics §5.4).
//
// **What this proves, and what it does not.** The transaction's own backend is terminated
// from a second pool with its work done and its `COMMIT` not yet sent, so the commit fails
// on a broken connection. That is the *did-not-commit* half of the ambiguity: the server
// rolled back, the client cannot know that, and the contract requires it be told the
// outcome is unknown rather than failed.
//
// The other half — the commit landing durably and only its acknowledgement being lost —
// is **not** proven here, and cannot be by a test that speaks to PostgreSQL directly:
// there is no instant between the server's durable write and its reply that a client can
// interpose on. Producing it needs a proxy that drops the reply. The register says so,
// rather than letting this test read as the whole invariant.
func TestCommitInterruptedMidFlightIsUnknownReplayable(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)
	h.seedWindow(t, testOrg, "fault-slot", 5, base, time.Hour, 2*time.Hour)
	ref := domain.SlotRef{OrganisationID: testOrg, SlotID: "fault-slot"}

	// A second pool, so the killer is never the connection being killed.
	killer, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("opening the killer pool: %v", err)
	}
	defer killer.Close()

	var wrote bool
	err = h.repo.WithinTx(context.Background(), func(ctx context.Context, txn domain.Tx) error {
		slot, err := txn.LockSlot(ctx, ref)
		if err != nil {
			return err
		}
		now, err := txn.Now(ctx)
		if err != nil {
			return err
		}
		if err := txn.PutReservation(ctx, domain.Reservation{
			ID: "res-fault", SlotRef: slot.Ref(), UserRef: userRef("user-1"),
			State: domain.ReservationHeld, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		}); err != nil {
			return err
		}
		wrote = true

		// The fault, timed. This test is in package postgres, so it can reach the
		// transaction's own connection to ask which backend it is — the alternative,
		// hunting pg_stat_activity for whoever holds the slot lock, would be guessing.
		var pid int32
		if err := txn.(*tx).conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		var killed bool
		if err := killer.QueryRow(context.Background(),
			"SELECT pg_terminate_backend($1)", pid).Scan(&killed); err != nil {
			return err
		}
		if !killed {
			t.Fatal("could not terminate the transaction's backend; the fault never happened")
		}
		return nil
	})

	if !wrote {
		t.Fatal("the transaction never reached its write, so no commit was interrupted")
	}
	if err == nil {
		t.Fatal("a commit whose connection was killed reported success")
	}
	if !errors.Is(err, domain.ErrCommitUnknown) {
		t.Fatalf("commit fault classified as %v, want ErrCommitUnknown; reporting an ambiguous "+
			"commit as a definite failure is the more dangerous of the two errors", err)
	}
	if got := domain.ClassifyFault(err); got != domain.OutcomeUnknownReplayable {
		t.Errorf("outcome = %q, want %q", got, domain.OutcomeUnknownReplayable)
	}

	// The second half of INV-21, in the case this fault produces: the interrupted commit
	// left nothing behind, so a replay under the same key performs the mutation exactly
	// once rather than a second time.
	verify := newRepo(t, testBudget())
	var count int
	if err := verify.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM reservations WHERE reservation_id = $1", "res-fault").Scan(&count); err != nil {
		t.Fatalf("counting the interrupted reservation: %v", err)
	}
	if count != 0 {
		t.Errorf("the interrupted commit left %d row(s); the server rolled back, so it must leave none", count)
	}
}
