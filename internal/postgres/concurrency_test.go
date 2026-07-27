//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// These tests cover the AG-M1 correctness gates that only real concurrent SQL can
// establish (implementation-plan §5). The in-memory reference serializes every
// transaction with one process-wide lock, so it can prove the domain logic is correct
// *when* serialized — but not that PostgreSQL actually serializes it. That is what is
// proven here.

// Gate: capacity is never exceeded.
//
// Many more requests than units, all racing for one slot. The invariant is checked
// against persisted rows, not against the results the service returned, so a service
// that reported correct answers while writing wrong state would still fail.
func TestCapacityNeverExceededUnderConcurrentReserves(t *testing.T) {
	const capacity = 7
	const contenders = 60

	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, capacity, -time.Hour, time.Hour)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		refused   int
		failures  []error
	)
	start := make(chan struct{})

	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release everyone at once, maximising real contention
			r, err := h.reserve(context.Background(), fmt.Sprintf("user-%d", i), fmt.Sprintf("key-%d", i), testSlot)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failures = append(failures, err)
			case r.Outcome == domain.OutcomeAdmittedSuccess:
				succeeded++
			case r.Outcome == domain.OutcomeBusinessRefusal && r.Reason == domain.ReasonNoCapacity:
				refused++
			default:
				failures = append(failures, fmt.Errorf("unexpected outcome %q/%q", r.Outcome, r.Reason))
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("contender failed: %v", err)
	}
	if succeeded != capacity {
		t.Errorf("admitted %d reserves, want exactly the capacity %d", succeeded, capacity)
	}
	if succeeded+refused != contenders {
		t.Errorf("accounted for %d of %d requests: every request must reach a terminal outcome",
			succeeded+refused, contenders)
	}
	assertConsumed(t, h, testSlot, capacity, 0)
}

// Gate: one idempotency key cannot produce two logical mutations.
//
// The same scoped key, issued concurrently for the same target — the ordinary
// duplicate-request case. All contenders resolve to the same slot and serialize on its
// lock, so exactly one performs the mutation and the rest replay it
// (transaction-semantics §5.3, first case).
func TestConcurrentDuplicateKeyProducesOneMutation(t *testing.T) {
	const contenders = 24

	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, testSlot, 10, -time.Hour, time.Hour)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		results  []domain.Result
		failures []error
	)
	start := make(chan struct{})

	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := h.reserve(context.Background(), "user-1", "same-key", testSlot)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			results = append(results, r)
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("contender failed: %v", err)
	}

	var originals int
	var reservationID domain.ReservationID
	for _, r := range results {
		if r.Outcome != domain.OutcomeAdmittedSuccess {
			t.Errorf("outcome = %q/%q, want every duplicate to resolve to the recorded success",
				r.Outcome, r.Reason)
			continue
		}
		if !r.Replay {
			originals++
			reservationID = r.ReservationID
		}
	}
	if originals != 1 {
		t.Errorf("%d non-replay results, want exactly 1: a key must yield one logical mutation", originals)
	}
	// Every caller — original and replays alike — must be told about the same entity.
	for _, r := range results {
		if r.ReservationID != reservationID {
			t.Errorf("result references %q, want the single created reservation %q",
				r.ReservationID, reservationID)
		}
	}
	assertConsumed(t, h, testSlot, 1, 0)
}

// The key-reuse case: the same scoped key used concurrently for *different* targets.
//
// The scope excludes the target, so these two requests may lock different slots and
// the slot lock does not order them. The scoped-key unique constraint is the only
// backstop, and it must hold: one wins, the loser rolls back its attempted mutation in
// full and returns idempotency_conflict (transaction-semantics §5.3, second case).
func TestConcurrentKeyReuseAcrossSlotsYieldsOneMutation(t *testing.T) {
	const slotA, slotB = domain.SlotID("slot-a"), domain.SlotID("slot-b")
	const attempts = 16

	h := newHarness(t, testBudget(), 30*time.Second)
	h.seedSlot(t, slotA, 10, -time.Hour, time.Hour)
	h.seedSlot(t, slotB, 10, -time.Hour, time.Hour)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		conflicts int
		failures  []error
	)
	start := make(chan struct{})

	for i := range attempts {
		target := slotA
		if i%2 == 1 {
			target = slotB
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := h.reserve(context.Background(), "user-1", "shared-key", target)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failures = append(failures, err)
			case r.Outcome == domain.OutcomeAdmittedSuccess:
				successes++
			case r.Reason == domain.ReasonIdempotencyConflict:
				conflicts++
			default:
				failures = append(failures, fmt.Errorf("unexpected outcome %q/%q", r.Outcome, r.Reason))
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("contender failed: %v", err)
	}
	if conflicts == 0 {
		t.Error("no idempotency_conflict observed: the key-reuse race did not exercise the constraint")
	}

	// The decisive assertion: across both slots, exactly one reservation exists.
	heldA, _, err := h.repo.SlotCounts(context.Background(), slotA)
	if err != nil {
		t.Fatalf("slot counts: %v", err)
	}
	heldB, _, err := h.repo.SlotCounts(context.Background(), slotB)
	if err != nil {
		t.Fatalf("slot counts: %v", err)
	}
	if heldA+heldB != 1 {
		t.Errorf("%d reservations persisted across both slots, want exactly 1: "+
			"a reused key must not survive as a second logical mutation", heldA+heldB)
	}
	if successes+conflicts != attempts {
		t.Errorf("accounted for %d of %d requests", successes+conflicts, attempts)
	}
}

// Gate: cancellation/confirmation races have exactly one valid winner.
//
// Both operations target one held reservation and serialize on its slot lock. The
// first commits and defines the terminal transition; the second observes it under the
// same lock and returns an explicit refusal — never a silent double mutation.
func TestConfirmCancelRaceHasOneWinner(t *testing.T) {
	const rounds = 12
	h := newHarness(t, testBudget(), 30*time.Second)

	for round := range rounds {
		slotID := domain.SlotID(fmt.Sprintf("slot-race-%d", round))
		h.seedSlot(t, slotID, 1, -time.Hour, time.Hour)

		reserved, err := h.reserve(context.Background(), "user-1", fmt.Sprintf("res-key-%d", round), slotID)
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		assertOutcome(t, reserved, domain.OutcomeAdmittedSuccess, "")

		var (
			wg                     sync.WaitGroup
			confirmRes, cancelRes  domain.Result
			confirmErr, cancelsErr error
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			confirmRes, confirmErr = h.confirm(context.Background(), "user-1", fmt.Sprintf("c-key-%d", round), reserved.ReservationID)
		}()
		go func() {
			defer wg.Done()
			<-start
			cancelRes, cancelsErr = h.cancel(context.Background(), "user-1", fmt.Sprintf("x-key-%d", round), reserved.ReservationID)
		}()
		close(start)
		wg.Wait()

		if confirmErr != nil {
			t.Fatalf("confirm: %v", confirmErr)
		}
		if cancelsErr != nil {
			t.Fatalf("cancel: %v", cancelsErr)
		}

		confirmed := confirmRes.Outcome == domain.OutcomeAdmittedSuccess
		cancelled := cancelRes.Outcome == domain.OutcomeAdmittedSuccess

		// Both may succeed only in the legitimate order confirm-then-cancel, which is
		// not a race failure but a valid sequence: the confirm creates the booking and
		// the cancel then releases it. What must never happen is a mutation with no
		// valid predecessor, or neither operation reaching a terminal answer.
		switch {
		case confirmed && cancelled:
			// confirm won, then cancel released the booking it created.
			assertConsumed(t, h, slotID, 0, 0)
		case confirmed && !cancelled:
			assertConsumed(t, h, slotID, 0, 1)
		case !confirmed && cancelled:
			assertConsumed(t, h, slotID, 0, 0)
		default:
			t.Errorf("round %d: neither confirm nor cancel succeeded (confirm %q/%q, cancel %q/%q): "+
				"a race must have a winner", round,
				confirmRes.Outcome, confirmRes.Reason, cancelRes.Outcome, cancelRes.Reason)
		}
	}
}

// Gate: expiry cannot release confirmed capacity.
//
// A short-TTL hold is confirmed, then left well past its original expires_at. Lazy
// settlement runs on the next operation and must not touch it: expiry acts only on
// held rows, so the confirmed unit keeps consuming capacity and the slot stays full.
func TestExpiryCannotReleaseConfirmedCapacity(t *testing.T) {
	const ttl = 200 * time.Millisecond
	h := newHarness(t, testBudget(), ttl)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	reserved, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	confirmed, err := h.confirm(context.Background(), "user-1", "key-2", reserved.ReservationID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	assertOutcome(t, confirmed, domain.OutcomeAdmittedSuccess, "")

	// Well past the original hold's expires_at.
	time.Sleep(2 * ttl)

	// This reserve triggers settlement under the lock; the confirmed unit must survive
	// it, so the slot is still full.
	blocked, err := h.reserve(context.Background(), "user-2", "key-3", testSlot)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, blocked, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)

	states, err := h.repo.ReservationStates(context.Background(), testSlot)
	if err != nil {
		t.Fatalf("reservation states: %v", err)
	}
	if states[domain.ReservationExpired] != 0 {
		t.Errorf("%d reservations expired: expiry must never act on a confirmed reservation", states[domain.ReservationExpired])
	}
	assertConsumed(t, h, testSlot, 0, 1)
}

// Gate: abandoned holds do not permanently consume capacity, without the worker
// having run. Correctness must not depend on the background expiry worker (PR4) —
// this is the lazy settle-under-lock path doing it alone.
func TestAbandonedHoldIsSettledLazily(t *testing.T) {
	const ttl = 200 * time.Millisecond
	h := newHarness(t, testBudget(), ttl)
	h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", testSlot); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	// The client vanishes: no confirm, no cancel.
	time.Sleep(2 * ttl)

	second, err := h.reserve(context.Background(), "user-2", "key-2", testSlot)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	assertConsumed(t, h, testSlot, 1, 0)
}
