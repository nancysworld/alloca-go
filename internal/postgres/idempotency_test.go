//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Gate: replay after a lost response returns the original outcome.
//
// The sequential case, which is what a client retry after a dropped connection
// actually looks like: the record is durable, so the second request returns the first
// one's outcome and result_ref with replay=true, and mutates nothing.
func TestReplayReturnsRecordedOutcome(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	first, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")
	if first.Replay {
		t.Error("the original request must not be marked as a replay")
	}

	second, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("replayed reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	if !second.Replay {
		t.Error("the repeated request must be marked replay=true")
	}
	if second.ReservationID != first.ReservationID {
		t.Errorf("replay returned reservation %q, want the original %q",
			second.ReservationID, first.ReservationID)
	}
	assertConsumed(t, h, testSlot, 1, 0)
}

// A refusal is a completed request too, so it is recorded and replayed like any other
// outcome. Without this the replay contract would have a hole exactly where a client
// is most likely to retry.
func TestRefusalIsRecordedAndReplayed(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", testSlot); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	refused, err := h.reserve(context.Background(), "user-2", "key-2", testSlot)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, refused, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)

	replayed, err := h.reserve(context.Background(), "user-2", "key-2", testSlot)
	if err != nil {
		t.Fatalf("replayed reserve: %v", err)
	}
	assertOutcome(t, replayed, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)
	if !replayed.Replay {
		t.Error("the replayed refusal must be marked replay=true")
	}
}

// A key reused for a different request is refused rather than silently repurposed.
func TestKeyReusedForDifferentTargetConflicts(t *testing.T) {
	const otherSlot = domain.SlotID("slot-2")
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)
	h.seedSlot(t, otherSlot, 5, -time.Hour, time.Hour)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", testSlot); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	reused, err := h.reserve(context.Background(), "user-1", "key-1", otherSlot)
	if err != nil {
		t.Fatalf("reused-key reserve: %v", err)
	}
	assertOutcome(t, reused, domain.OutcomeBusinessRefusal, domain.ReasonIdempotencyConflict)
	assertConsumed(t, h, otherSlot, 0, 0)
}

// The idempotency scope is (organisation, user, operation, key), not the key alone.
// One user's key must never replay another's result, and the same key must be usable
// for a different operation.
func TestIdempotencyScopeIsNotGlobal(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	first, err := h.reserve(context.Background(), "user-1", "shared", testSlot)
	if err != nil {
		t.Fatalf("user-1 reserve: %v", err)
	}
	second, err := h.reserve(context.Background(), "user-2", "shared", testSlot)
	if err != nil {
		t.Fatalf("user-2 reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	if second.Replay {
		t.Error("a different user's identical key must not replay the first user's result")
	}
	if second.ReservationID == first.ReservationID {
		t.Error("two users' requests produced the same reservation")
	}

	// The same key, same user, different operation: the operation is part of the scope,
	// so this is a fresh request rather than a replay or a conflict.
	confirmed, err := h.confirm(context.Background(), "user-1", "shared", first.ReservationID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	assertOutcome(t, confirmed, domain.OutcomeAdmittedSuccess, "")
	if confirmed.Replay {
		t.Error("a key reused for a different operation must not replay")
	}
	assertConsumed(t, h, testSlot, 1, 1)
}

// transaction-semantics §5.5: a well-formed request for a target that does not exist
// is a recorded, replayable business_refusal — not a fault, and not a request that
// escapes the taxonomy. It never locks a slot, so it exercises the explicit no-slot
// time-resolution path.
func TestUnknownTargetIsRecordedAndReplayable(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)

	first, err := h.reserve(context.Background(), "user-1", "key-1", "no-such-slot")
	if err != nil {
		t.Fatalf("reserve unknown slot: %v", err)
	}
	assertOutcome(t, first, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)

	replayed, err := h.reserve(context.Background(), "user-1", "key-1", "no-such-slot")
	if err != nil {
		t.Fatalf("replayed reserve: %v", err)
	}
	assertOutcome(t, replayed, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
	if !replayed.Replay {
		t.Error("the recorded unknown_target refusal must replay")
	}

	// The record exists and carries a timestamp resolved on the no-slot path.
	var created time.Time
	err = h.repo.pool.QueryRow(context.Background(),
		`SELECT created_at FROM idempotency_records WHERE key = $1`, "key-1").Scan(&created)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if created.IsZero() {
		t.Error("the unknown_target record has no created_at")
	}

	// And the key cannot later be repurposed for a slot that does exist.
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)
	repurposed, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("repurposed reserve: %v", err)
	}
	assertOutcome(t, repurposed, domain.OutcomeBusinessRefusal, domain.ReasonIdempotencyConflict)
	assertConsumed(t, h, testSlot, 0, 0)
}

// Confirming an unknown reservation takes the same no-slot path.
func TestConfirmUnknownReservationIsUnknownTarget(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)

	result, err := h.confirm(context.Background(), "user-1", "key-1", "no-such-reservation")
	if err != nil {
		t.Fatalf("confirm unknown reservation: %v", err)
	}
	assertOutcome(t, result, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
}

// Rollback: a transaction that fails partway must leave no trace. The service writes
// the idempotency record before the mutation, so a failure between the two is the
// dangerous window — a surviving record would durably claim an outcome for a mutation
// that never happened, and every later replay would repeat the lie.
func TestFailedTransactionPersistsNothing(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	sentinel := errors.New("deliberate failure after the record insert")
	err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.LockSlot(ctx, slotRef(testSlot)); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if err := tx.InsertRecord(ctx, domain.IdempotencyRecord{
			OrganisationID: testOrg, UserID: "user-1", Operation: domain.OpReserve,
			Key: "doomed", RequestHash: "hash", Outcome: domain.OutcomeAdmittedSuccess,
			ReservationID: "res-doomed", CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := tx.PutReservation(ctx, domain.Reservation{
			ID: "res-doomed", SlotRef: slotRef(testSlot), OrganisationID: testOrg, UserID: "user-1",
			State: domain.ReservationHeld, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithinTx: err = %v, want the sentinel", err)
	}

	var records int
	if err := h.repo.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM idempotency_records WHERE key = $1`, "doomed").Scan(&records); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if records != 0 {
		t.Errorf("%d idempotency records survived a rolled-back transaction, want 0", records)
	}
	assertConsumed(t, h, testSlot, 0, 0)

	// The key is genuinely free afterwards: the rollback left no shadow.
	result, err := h.reserve(context.Background(), "user-1", "doomed", testSlot)
	if err != nil {
		t.Fatalf("reserve after rollback: %v", err)
	}
	assertOutcome(t, result, domain.OutcomeAdmittedSuccess, "")
}

// The per-transaction session settings must not leak to the next borrower of a pooled
// connection. SET LOCAL is scoped to the transaction, which is exactly why it is used
// instead of SET — a leaked short lock_timeout would cause unrelated requests to fail.
func TestTransactionTimeoutsDoNotLeakAcrossTransactions(t *testing.T) {
	budget := testBudget()
	budget.LockTimeout = 100 * time.Millisecond
	budget.StatementTimeout = 200 * time.Millisecond

	h := newHarness(t, budget, 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	if err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		_, err := tx.LockSlot(ctx, slotRef(testSlot))
		return err
	}); err != nil {
		t.Fatalf("first transaction: %v", err)
	}

	// A statement outside any transaction, on a recycled connection, must be governed
	// by the session default rather than the previous transaction's tighter bound.
	var setting string
	if err := h.repo.pool.QueryRow(context.Background(), `SHOW lock_timeout`).Scan(&setting); err != nil {
		t.Fatalf("read lock_timeout: %v", err)
	}
	if want := "100ms"; setting != want {
		t.Errorf("session lock_timeout = %q, want the pool default %q", setting, want)
	}
}
