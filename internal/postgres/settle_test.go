//go:build integration

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Tests for Service.SettleSlot, the entry point PR5 added for the expiry worker.
//
// Whether a hold has elapsed, and against which clock, is proven in time_test.go. What is
// proven here is what out-of-band settlement does to persisted state: it returns the
// slot's capacity, and it leaves the schedule claims alone. The second half is the one
// that matters — user-scoped settlement is the sole claim reaper (transaction-semantics
// §2.2), and a worker that reaped claims per-slot would recreate the lock-ordering cycle
// PR4 removed.

// settleTTL is short enough that a hold elapses while the test watches, and long enough
// that the reserve which creates it commits first.
const settleTTL = 100 * time.Millisecond

// liveTTL is a hold that will not lapse during a test. It must stay well inside the seeded
// slot's start offset: a hold is only valid while expires_at <= starts_at, so a TTL equal to
// that offset is refused rather than held — the reserve returns a refusal with a nil error,
// which is why every test here asserts the outcome and not just the error.
const liveTTL = 30 * time.Second

// waitForElapsedHold blocks until the repository reports ref as holding an elapsed
// reservation.
//
// It polls the database rather than sleeping for a multiple of the TTL: "elapsed" is
// decided by clock_timestamp() inside the query, so this waits on the clock that actually
// owns the decision instead of assuming the test host's agrees with it. It also means the
// test proceeds as soon as the hold has lapsed rather than after a fixed guess.
//
// Reaching this point is itself an assertion: if ElapsedHoldSlots could not see the hold,
// the worker would never offer the slot to SettleSlot at all.
func (h *harness) waitForElapsedHold(t *testing.T, ref domain.SlotRef) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		refs, err := h.repo.ElapsedHoldSlots(context.Background(), 10)
		if err != nil {
			t.Fatalf("elapsed hold slots: %v", err)
		}
		if slices.Contains(refs, ref) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot %s never reported an elapsed hold; ElapsedHoldSlots returned %v", ref.SlotID, refs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The worker path returns capacity without touching the claim. The assertions are made
// against persisted rows before anything else reserves the slot, deliberately: every
// operation settles the slot it locks, so a *later* successful reserve would prove nothing
// about SettleSlot — it would have settled the hold itself.
func TestSettleSlotReturnsCapacityAndLeavesClaimsAlone(t *testing.T) {
	h := newHarness(t, testBudget(), liveTTL)
	slot := h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	reserved, err := h.reserveWithTTL(context.Background(), settleTTL, "user-1", "key-1", slot.Ref())
	if err != nil {
		t.Fatalf("seed hold: %v", err)
	}
	assertOutcome(t, reserved, domain.OutcomeAdmittedSuccess, "")
	assertConsumed(t, h, testSlot, 1, 0)
	assertClaimCount(t, h, 1)

	h.waitForElapsedHold(t, slot.Ref())

	expired, err := h.svc.SettleSlot(context.Background(), slot.Ref())
	if err != nil {
		t.Fatalf("settle slot: %v", err)
	}
	if expired != 1 {
		t.Errorf("SettleSlot reported %d expired holds, want 1", expired)
	}

	// The unit is free again: consumed capacity is derived from held reservations plus
	// active bookings, and both are now zero.
	assertConsumed(t, h, testSlot, 0, 0)

	states, err := h.repo.ReservationStates(context.Background(), slot.Ref())
	if err != nil {
		t.Fatalf("reservation states: %v", err)
	}
	if states[domain.ReservationExpired] != 1 || states[domain.ReservationHeld] != 0 {
		t.Errorf("reservation states = %v, want exactly one expired and no held", states)
	}

	// The decisive assertion. The claim outlives the hold it belonged to, and only the
	// owning identity's next reserve may remove it.
	assertClaimCount(t, h, 1)
	assertNoOverlappingClaims(t, h)
}

// Settlement is driven by the hold's own deadline, not by being asked. Without this, an
// implementation that expired every hold it found would pass the test above.
func TestSettleSlotExpiresNothingWhileTheHoldIsLive(t *testing.T) {
	h := newHarness(t, testBudget(), liveTTL)
	slot := h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	reserved, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("seed hold: %v", err)
	}
	assertOutcome(t, reserved, domain.OutcomeAdmittedSuccess, "")

	expired, err := h.svc.SettleSlot(context.Background(), slot.Ref())
	if err != nil {
		t.Fatalf("settle slot: %v", err)
	}
	if expired != 0 {
		t.Errorf("SettleSlot expired %d live holds, want 0", expired)
	}
	assertConsumed(t, h, testSlot, 1, 0)
	assertClaimCount(t, h, 1)
}

// A confirmed booking is not a hold and must survive settlement: it consumes capacity
// permanently, and expiring it would release a unit somebody is entitled to.
func TestSettleSlotLeavesConfirmedBookingsAlone(t *testing.T) {
	h := newHarness(t, testBudget(), liveTTL)
	slot := h.seedSlot(t, testSlot, 1, -time.Hour, time.Hour)

	reserved, err := h.reserve(context.Background(), "user-1", "key-1", testSlot)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	assertOutcome(t, reserved, domain.OutcomeAdmittedSuccess, "")

	confirmed, err := h.confirm(context.Background(), "user-1", "confirm-1", reserved.ReservationID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	assertOutcome(t, confirmed, domain.OutcomeAdmittedSuccess, "")
	assertConsumed(t, h, testSlot, 0, 1)

	expired, err := h.svc.SettleSlot(context.Background(), slot.Ref())
	if err != nil {
		t.Fatalf("settle slot: %v", err)
	}
	if expired != 0 {
		t.Errorf("SettleSlot expired %d reservations, want 0: a confirmed booking is not a hold", expired)
	}
	assertConsumed(t, h, testSlot, 0, 1)
	assertClaimCount(t, h, 1)
}

// SettleSlot reports a missing slot as an error rather than as a domain refusal. The
// worker only ever names slots ElapsedHoldSlots just returned, so there is no
// unknown_target outcome to produce here — and the worker's contract is to report a failed
// iteration, which needs the error channel.
func TestSettleSlotOnAnUnknownSlotFails(t *testing.T) {
	h := newHarness(t, testBudget(), time.Hour)

	_, err := h.svc.SettleSlot(context.Background(), slotRef("no-such-slot"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("SettleSlot on an unknown slot: err = %v, want domain.ErrNotFound", err)
	}
}
