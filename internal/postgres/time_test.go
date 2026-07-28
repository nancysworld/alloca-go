//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// The tests in this file discharge the authoritative-time contract carried into PR3
// (docs/design-notes/authoritative-time-in-a-scaled-service.md §6). They share one
// shape: a competing transaction holds the slot lock for a known interval, and the
// waiting transaction's decision must reflect the world at the end of that wait, not
// at its start. A pre-lock timestamp — transaction_timestamp(), or a value resolved by
// an API host before BEGIN — produces the opposite answer in every one of them, which
// is what makes them proofs rather than descriptions.

// Note §6 test 13 (and 1): the decision timestamp is evaluated after the row-lock
// wait. The batch's second statement cannot run until the FOR UPDATE has returned, so
// the waiting attempt's timestamp must be no earlier than the instant the holder
// released the lock.
func TestDecisionTimestampResolvedAfterLockWait(t *testing.T) {
	const lockHeld = 400 * time.Millisecond
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	// A timestamp resolved before the wait would be at or near this value.
	beforeWait := h.dbNow(t)
	release := h.holdSlotLock(t, testSlot, lockHeld)

	var attemptNow time.Time
	err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.LockSlot(ctx, slotRef(testSlot)); err != nil {
			return err
		}
		var err error
		attemptNow, err = tx.Now(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("contending transaction: %v", err)
	}
	releasedAt := release()

	if attemptNow.Before(releasedAt) {
		t.Errorf("attempt timestamp %v precedes the lock release at %v: "+
			"the decision time was evaluated before the lock wait", attemptNow, releasedAt)
	}
	// Guard against the test passing for the wrong reason — if the holder never
	// actually made the contender wait, the assertion above proves nothing.
	if waited := attemptNow.Sub(beforeWait); waited < lockHeld/2 {
		t.Errorf("contender waited only %v for a lock held %v: the test did not contend", waited, lockHeld)
	}
}

// Note §6 test 2: Now cannot resolve time independently, and cannot succeed before the
// lock contract is satisfied. This is what stops a future caller from quietly
// obtaining pre-lock time.
func TestNowBeforeLockSlotIsAnError(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.Now(ctx); !errors.Is(err, domain.ErrTimeNotEstablished) {
			t.Errorf("Now before LockSlot: err = %v, want ErrTimeNotEstablished", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

// A slot that does not exist must leave the attempt with no timestamp, so the
// unknown-target path has to declare itself rather than inherit one.
func TestLockSlotNotFoundEstablishesNoTime(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)

	err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.LockSlot(ctx, slotRef("no-such-slot")); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("LockSlot: err = %v, want ErrNotFound", err)
		}
		if _, err := tx.Now(ctx); !errors.Is(err, domain.ErrTimeNotEstablished) {
			t.Errorf("Now after failed LockSlot: err = %v, want ErrTimeNotEstablished", err)
		}
		// The explicit no-slot path is the only way forward, and it works.
		now, err := tx.ResolveTimeWithoutSlot(ctx)
		if err != nil {
			t.Fatalf("ResolveTimeWithoutSlot: %v", err)
		}
		if now.IsZero() {
			t.Error("ResolveTimeWithoutSlot returned the zero time")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

// Note §6 test 3: a transaction that waits while a hold expires settles that hold
// using the post-wait timestamp.
//
// Capacity is 1 and the slot's only unit is held when the contender arrives. With a
// pre-lock timestamp the hold still looks live and the reserve is refused no_capacity;
// with the correct post-lock timestamp the hold has elapsed, is settled, and the unit
// is free.
func TestHoldExpiringDuringLockWaitIsSettled(t *testing.T) {
	const holdTTL = 200 * time.Millisecond
	const lockHeld = 500 * time.Millisecond

	h := newHarness(t, testBudget(), holdTTL)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	first, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")

	release := h.holdSlotLock(t, testSlot, lockHeld)
	second, err := h.reserve(context.Background(), "user-2", "key-2", testSlot)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	release()

	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")

	// The first hold was settled to expired, not left to consume capacity forever.
	states, err := h.repo.ReservationStates(context.Background(), slotRef(testSlot))
	if err != nil {
		t.Fatalf("reservation states: %v", err)
	}
	if states[domain.ReservationExpired] != 1 {
		t.Errorf("expired reservations = %d, want 1 (the elapsed hold): %v",
			states[domain.ReservationExpired], states)
	}
	if states[domain.ReservationHeld] != 1 {
		t.Errorf("held reservations = %d, want 1 (the new hold): %v",
			states[domain.ReservationHeld], states)
	}
	if second.ReservationID == first.ReservationID {
		t.Error("the settled hold and the new hold are the same reservation")
	}
}

// Note §6 test 4: a transaction that waits while the slot crosses starts_at cannot
// reserve after the booking window has closed. With a pre-lock timestamp the slot
// still looks open and the reserve would succeed — creating a hold on a slot that has
// already started.
func TestSlotClosingDuringLockWaitRefusesReserve(t *testing.T) {
	const startsIn = 250 * time.Millisecond
	const lockHeld = 600 * time.Millisecond

	h := newHarness(t, testBudget(), 50*time.Millisecond)
	h.seedSlot(t, testSlot, 5, -time.Hour, startsIn)

	release := h.holdSlotLock(t, testSlot, lockHeld)
	result, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	release()

	assertOutcome(t, result, domain.OutcomeBusinessRefusal, domain.ReasonSlotClosed)
	assertConsumed(t, h, testSlot, 0, 0)
}

// Note §6 test 5: a request whose TTL fit on arrival but no longer fits after the lock
// wait is refused as outside_window and creates no reservation.
//
// The window is arranged so the slot is still open at the decision point — this is
// specifically the TTL no longer fitting, not the slot having closed. Refusing is the
// deliberate choice: a silently shortened hold would be a hold whose duration the
// client never agreed to.
func TestTTLNoLongerFittingAfterLockWaitRefuses(t *testing.T) {
	const ttl = 300 * time.Millisecond
	const startsIn = 600 * time.Millisecond
	const lockHeld = 400 * time.Millisecond

	h := newHarness(t, testBudget(), ttl)
	h.seedSlot(t, testSlot, 5, -time.Hour, startsIn)

	// On arrival the TTL fits: now + 300ms < starts_at at +600ms. After the wait the
	// decision point is ~+400ms, so the hold would end at ~+700ms, past starts_at.
	release := h.holdSlotLock(t, testSlot, lockHeld)
	result, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	release()

	assertOutcome(t, result, domain.OutcomeBusinessRefusal, domain.ReasonOutsideWindow)
	assertConsumed(t, h, testSlot, 0, 0)
}

// Note §6 test 6: one attempt, one instant. The reservation's created_at, its
// expires_at, and the idempotency record's created_at must all derive from the same
// decision timestamp — otherwise "the recorded outcome" and "the mutation" would
// describe subtly different moments.
func TestAttemptTimestampIsUsedForEveryPersistedValue(t *testing.T) {
	const ttl = 45 * time.Second
	h := newHarness(t, testBudget(), ttl)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	result, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	assertOutcome(t, result, domain.OutcomeAdmittedSuccess, "")

	var resCreated, resExpires, recCreated time.Time
	err = h.repo.pool.QueryRow(context.Background(), `
		SELECT r.created_at, r.expires_at, i.created_at
		FROM reservations r, idempotency_records i
		WHERE r.reservation_id = $1 AND i.key = $2`,
		string(result.ReservationID), "key-1").Scan(&resCreated, &resExpires, &recCreated)
	if err != nil {
		t.Fatalf("read persisted timestamps: %v", err)
	}

	if !resCreated.Equal(recCreated) {
		t.Errorf("reservation created_at %v != idempotency record created_at %v: "+
			"the mutation and its record used different instants", resCreated, recCreated)
	}
	if got := resExpires.Sub(resCreated); got != ttl {
		t.Errorf("expires_at - created_at = %v, want exactly the TTL %v", got, ttl)
	}
}

// The TTL is measured from the post-lock decision point, so a contended request still
// receives a full-length hold: lock-wait time does not erode the hold a client is
// granted (design note §5).
func TestTTLMeasuredFromDecisionPointNotArrival(t *testing.T) {
	const ttl = 30 * time.Second
	const lockHeld = 400 * time.Millisecond

	h := newHarness(t, testBudget(), ttl)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	arrival := h.dbNow(t)
	release := h.holdSlotLock(t, testSlot, lockHeld)
	result, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	release()
	assertOutcome(t, result, domain.OutcomeAdmittedSuccess, "")

	var created, expires time.Time
	err = h.repo.pool.QueryRow(context.Background(),
		`SELECT created_at, expires_at FROM reservations WHERE reservation_id = $1`,
		string(result.ReservationID)).Scan(&created, &expires)
	if err != nil {
		t.Fatalf("read reservation: %v", err)
	}

	if got := expires.Sub(created); got != ttl {
		t.Errorf("hold duration = %v, want the full TTL %v", got, ttl)
	}
	// The hold started after the wait, not at arrival.
	if !created.After(arrival.Add(lockHeld / 2)) {
		t.Errorf("created_at %v is not meaningfully after arrival %v: the hold was dated pre-lock",
			created, arrival)
	}
}

// Note §6 test 7 (and transaction-semantics §6): a lock wait that exceeds lock_timeout
// is a database-layer timeout. It must surface as timeout_db — never as a
// business_refusal, which would report an infrastructure failure as a valid domain
// answer and quietly corrupt the outcome mix.
func TestLockTimeoutClassifiesAsDBTimeout(t *testing.T) {
	budget := testBudget()
	budget.LockTimeout = 150 * time.Millisecond
	budget.StatementTimeout = 5 * time.Second
	budget.TxnBudget = 10 * time.Second

	h := newHarness(t, budget, 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	release := h.holdSlotLock(t, testSlot, 1500*time.Millisecond)
	result, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	release()

	if err == nil {
		t.Fatalf("reserve returned result %+v, want a lock-timeout error", result)
	}
	if !errors.Is(err, domain.ErrDBTimeout) {
		t.Errorf("err = %v, want it to wrap domain.ErrDBTimeout", err)
	}
	if got := domain.ClassifyFault(err); got != domain.OutcomeTimeoutDB {
		t.Errorf("ClassifyFault = %q, want %q", got, domain.OutcomeTimeoutDB)
	}
	// A timed-out reserve mutates nothing.
	assertConsumed(t, h, testSlot, 0, 0)
}

// A caller whose own context is cancelled must not be reported as a database timeout:
// timeout_client and timeout_db are different lines in the outcome table, and the
// whole point of the layered budget is that they stay distinguishable.
func TestCallerCancellationIsNotADBTimeout(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 5, -time.Hour, time.Hour)

	release := h.holdSlotLock(t, testSlot, 800*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	_, err := h.reserve(ctx, "user-1", "key-1", testSlot)
	release()

	if err == nil {
		t.Fatal("reserve succeeded, want a cancellation error")
	}
	if errors.Is(err, domain.ErrDBTimeout) {
		t.Errorf("err = %v, want a cancellation rather than a db timeout", err)
	}
	if got := domain.ClassifyFault(err); got != domain.OutcomeTimeoutClient {
		t.Errorf("ClassifyFault = %q, want %q", got, domain.OutcomeTimeoutClient)
	}
	assertConsumed(t, h, testSlot, 0, 0)
}
