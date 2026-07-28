//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// A slot's identity is the pair (organisation_id, slot_id), not slot_id alone
// (transaction-semantics §1.2).
//
// These tests exist because the previous schema keyed slots by slot_id, which quietly
// assumed identifiers are unique across every organisation. Nothing establishes that:
// they are unique *within* the organisation that owns the slot. Since the slot row is
// the aggregate lock, the assumption was a correctness one — two organisations minting
// the same identifier would either serialize unrelated slots against each other or
// resolve to the wrong organisation's slot.
//
// The discriminating property is simple: under the old single-column key, none of these
// tests can even set up. Seeding two slots that share an identifier fails on the primary
// key, so a rollback of 00002 turns these into setup failures rather than silent passes.

// sharedID is deliberately the same identifier in both organisations — the case the old
// schema could not represent.
const sharedID = domain.SlotID("evening-class")

// Two organisations may each own a slot called the same thing, and they are different
// slots with independent capacity.
func TestSlotIdentityIsScopedToItsOwningOrganisation(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)

	// Same identifier, different owners, and each with a single unit of capacity.
	h.seedWindow(t, testOrg, sharedID, 1, base, time.Hour, 2*time.Hour)
	h.seedWindow(t, otherOrg, sharedID, 1, base, 3*time.Hour, 4*time.Hour)

	first := domain.SlotRef{OrganisationID: testOrg, SlotID: sharedID}
	second := domain.SlotRef{OrganisationID: otherOrg, SlotID: sharedID}

	// Fill the first organisation's slot.
	r, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", first)
	if err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeAdmittedSuccess, "")

	// It is now sold out — for that organisation only.
	soldOut, err := h.reserveAs(context.Background(), testOrg, "user-2", "key-2", first)
	if err != nil {
		t.Fatalf("reserve first again: %v", err)
	}
	assertOutcome(t, soldOut, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)

	// The other organisation's identically-named slot is untouched. If capacity were
	// keyed by slot_id alone, this would refuse with no_capacity.
	other, err := h.reserveAs(context.Background(), otherOrg, "user-3", "key-3", second)
	if err != nil {
		t.Fatalf("reserve second: %v", err)
	}
	assertOutcome(t, other, domain.OutcomeAdmittedSuccess, "")

	assertSlotCounts(t, h, first, 1, 0)
	assertSlotCounts(t, h, second, 1, 0)
}

// Resolving a reservation back to its slot must yield the whole pair. Confirm and cancel
// look the slot up from the reservation and then lock it, so a resolution that dropped
// the organisation would take the wrong row's lock and mutate the wrong slot's capacity.
func TestReservationResolvesToItsOwnSlotNotASameNamedOne(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)

	h.seedWindow(t, testOrg, sharedID, 5, base, time.Hour, 2*time.Hour)
	h.seedWindow(t, otherOrg, sharedID, 5, base, 3*time.Hour, 4*time.Hour)

	decoy := domain.SlotRef{OrganisationID: testOrg, SlotID: sharedID}
	target := domain.SlotRef{OrganisationID: otherOrg, SlotID: sharedID}

	// A testOrg identity books the *other* organisation's slot: identity organisation
	// and slot organisation differ, so nothing in the reservation's own identity columns
	// could reconstruct the slot's owner.
	held, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", target)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	assertOutcome(t, held, domain.OutcomeAdmittedSuccess, "")

	confirmed, err := h.confirm(context.Background(), "user-1", "key-2", held.ReservationID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	assertOutcome(t, confirmed, domain.OutcomeAdmittedSuccess, "")

	// The booking landed on the target, and the same-named decoy never moved.
	assertSlotCounts(t, h, target, 0, 1)
	assertSlotCounts(t, h, decoy, 0, 0)
}

// The two identity rules compose: a schedule claim is keyed by the caller's identity and
// carries the slot's owner separately, so one identity still cannot double-book itself
// across two organisations whose slots happen to share an identifier.
func TestScheduleInvariantHoldsAcrossSameNamedSlotsInDifferentOrganisations(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)

	// Same identifier, different owners, overlapping windows.
	h.seedWindow(t, testOrg, sharedID, 5, base, time.Hour, 2*time.Hour)
	h.seedWindow(t, otherOrg, sharedID, 5, base, 90*time.Minute, 150*time.Minute)

	home := domain.SlotRef{OrganisationID: testOrg, SlotID: sharedID}
	guest := domain.SlotRef{OrganisationID: otherOrg, SlotID: sharedID}

	first, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", home)
	if err != nil {
		t.Fatalf("home reserve: %v", err)
	}
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")

	second, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-2", guest)
	if err != nil {
		t.Fatalf("guest reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
	assertNoOverlappingClaims(t, h)
}

func assertSlotCounts(t *testing.T, h *harness, ref domain.SlotRef, wantHeld, wantBookings int) {
	t.Helper()
	held, active, err := h.repo.SlotCounts(context.Background(), ref)
	if err != nil {
		t.Fatalf("slot counts for %s/%s: %v", ref.OrganisationID, ref.SlotID, err)
	}
	if held != wantHeld || active != wantBookings {
		t.Errorf("slot %s/%s: held=%d bookings=%d, want held=%d bookings=%d",
			ref.OrganisationID, ref.SlotID, held, active, wantHeld, wantBookings)
	}
}
