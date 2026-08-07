package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/inmem"
)

// shardedFixture builds a service whose deployment splits organisations across two
// writable authorities: org-a and org-c share authority-1, org-b is on authority-2.
//
// The slot always belongs to slotOrg, so a test chooses the case it wants by choosing
// the *user's* organisation.
func shardedFixture(t *testing.T, slotOrg domain.OrganisationID) *fixture {
	t.Helper()
	placement, err := domain.ParsePlacement([]byte(
		`{"version":"routing-v1","homes":{"org-a":"authority-1","org-b":"authority-2","org-c":"authority-1"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}

	clock := &manualClock{t: baseNow}
	store := inmem.New(clock)
	store.SeedSlot(domain.Slot{
		ID: slot, OrganisationID: slotOrg, Capacity: 5,
		ReleaseAt: baseRelease, StartsAt: baseStart, EndsAt: baseStart.Add(time.Hour),
	})
	ids := &seqIDGen{}
	return &fixture{svc: New(store, ids, testTTL, placement), store: store, clock: clock, ids: ids}
}

func reserveAs(t *testing.T, f *fixture, userOrg domain.OrganisationID, slotOrg domain.OrganisationID, key string) domain.Result {
	t.Helper()
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{
		UserRef:        domain.UserRef{OrganisationID: userOrg, UserID: "u-1"},
		SlotRef:        domain.SlotRef{OrganisationID: slotOrg, SlotID: slot},
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("Reserve: unexpected error %v", err)
	}
	return r
}

// The Phase 1 support boundary. org-a's slot and org-b's user live on different
// writable authorities, so the booking would need a distributed commit Phase 1 does not
// have, and it is refused.
func TestReserveAcrossAuthoritiesIsRefused(t *testing.T) {
	f := shardedFixture(t, "org-a")

	r := reserveAs(t, f, "org-b", "org-a", "k-1")

	if r.Outcome != domain.OutcomeBusinessRefusal {
		t.Fatalf("outcome = %q, want %q", r.Outcome, domain.OutcomeBusinessRefusal)
	}
	if r.Reason != domain.ReasonCrossAuthorityUnsupported {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonCrossAuthorityUnsupported)
	}
	if r.ReservationID != "" {
		t.Errorf("a refused reserve minted reservation %q", r.ReservationID)
	}
}

// The case that distinguishes placement equality from organisation-identifier equality,
// and the reason the policy is written the way it is: org-c's user booking org-a's slot
// is a *cross-organisation* booking, and it succeeds because the two are colocated.
// A policy comparing identifiers would have refused it and broken INV-13.
func TestColocatedCrossOrganisationReserveSucceeds(t *testing.T) {
	f := shardedFixture(t, "org-a")

	r := reserveAs(t, f, "org-c", "org-a", "k-1")

	if r.Outcome != domain.OutcomeAdmittedSuccess {
		t.Fatalf("outcome = %q (reason %q), want %q — org-a and org-c share authority-1",
			r.Outcome, r.Reason, domain.OutcomeAdmittedSuccess)
	}
	if r.ReservationID == "" {
		t.Error("a successful reserve minted no reservation")
	}
}

// The refusal is a recorded terminal outcome like any other, so replaying the same key
// returns it rather than re-deciding (horizontal-database design §5.3).
func TestCrossAuthorityRefusalIsReplayable(t *testing.T) {
	f := shardedFixture(t, "org-a")

	first := reserveAs(t, f, "org-b", "org-a", "k-1")
	second := reserveAs(t, f, "org-b", "org-a", "k-1")

	if !second.Replay {
		t.Error("replaying the refusal's key re-decided instead of replaying the record")
	}
	if second.Outcome != first.Outcome || second.Reason != first.Reason {
		t.Errorf("replay returned %q/%q, want the recorded %q/%q",
			second.Outcome, second.Reason, first.Outcome, first.Reason)
	}
}

// "Reject and durably record on user-home before any slot-authority work" — so the slot
// must be untouched. If the refusal had locked or decremented anything, this reserve by
// a colocated user would see less capacity than the slot was seeded with.
func TestCrossAuthorityRefusalTouchesNoSlotState(t *testing.T) {
	f := shardedFixture(t, "org-a")

	for i, key := range []string{"x-1", "x-2", "x-3"} {
		if r := reserveAs(t, f, "org-b", "org-a", key); r.Reason != domain.ReasonCrossAuthorityUnsupported {
			t.Fatalf("refusal %d: reason = %q", i, r.Reason)
		}
	}

	// All five units must still be available to the organisations that can book them.
	for i, key := range []string{"k-1", "k-2", "k-3", "k-4", "k-5"} {
		r, err := f.svc.Reserve(context.Background(), ReserveCommand{
			UserRef:        domain.UserRef{OrganisationID: "org-a", UserID: domain.UserID("u-" + key)},
			SlotRef:        domain.SlotRef{OrganisationID: "org-a", SlotID: slot},
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		if r.Outcome != domain.OutcomeAdmittedSuccess {
			t.Fatalf("reserve %d after three refusals: outcome = %q, reason = %q; the refusals consumed capacity",
				i, r.Outcome, r.Reason)
		}
	}
}

// An organisation the map has never heard of shares an authority with nothing, so it is
// refused by the same policy rather than reaching a slot lookup that could only fail.
func TestReserveForAnUnplacedOrganisationIsRefused(t *testing.T) {
	f := shardedFixture(t, "org-a")

	r := reserveAs(t, f, "org-nowhere", "org-a", "k-1")

	if r.Reason != domain.ReasonCrossAuthorityUnsupported {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonCrossAuthorityUnsupported)
	}
}

// An unsharded deployment is every deployment before PR3a, and the policy must be inert
// there: a cross-organisation booking still succeeds, because one authority owns both
// organisations. This is the test that says PR3a changed no shipped behaviour.
func TestUnshardedDeploymentRefusesNothingForPlacement(t *testing.T) {
	clock := &manualClock{t: baseNow}
	store := inmem.New(clock)
	store.SeedSlot(domain.Slot{
		ID: slot, OrganisationID: "org-a", Capacity: 5,
		ReleaseAt: baseRelease, StartsAt: baseStart, EndsAt: baseStart.Add(time.Hour),
	})
	f := &fixture{svc: New(store, &seqIDGen{}, testTTL, domain.Unsharded("authority-1")), store: store, clock: clock}

	r := reserveAs(t, f, "org-b", "org-a", "k-1")

	if r.Outcome != domain.OutcomeAdmittedSuccess {
		t.Fatalf("outcome = %q (reason %q); an unsharded deployment must support cross-organisation booking",
			r.Outcome, r.Reason)
	}
}

// Before PR3a a caller holding a reservation identifier could confirm or cancel it
// while asserting someone else's identity: the UserRef scoped idempotency and nothing
// else. This is the correction, and it is a deliberate behaviour change.
//
// Sharding makes it necessary as well as right. Without the comparison, the answer to a
// wrong identity would depend on whether the two organisations happened to be colocated
// — absent on another authority, mutable on the same one — and a correctness property
// that varies with topology is not a property.
func TestConfirmAndCancelRejectACallerWhoIsNotTheOwner(t *testing.T) {
	for _, op := range []string{"confirm", "cancel"} {
		t.Run(op, func(t *testing.T) {
			f := newFixture(t, 5)
			held := f.reserve(t, "owner", "k-reserve")
			if held.Outcome != domain.OutcomeAdmittedSuccess {
				t.Fatalf("seeding a reservation: outcome = %q", held.Outcome)
			}

			var r domain.Result
			if op == "confirm" {
				r = f.confirm(t, "impostor", "k-op", held.ReservationID)
			} else {
				r = f.cancel(t, "impostor", "k-op", held.ReservationID)
			}

			if r.Outcome != domain.OutcomeBusinessRefusal || r.Reason != domain.ReasonUnknownTarget {
				t.Fatalf("%s by a non-owner returned %q/%q, want %q/%q",
					op, r.Outcome, r.Reason, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
			}

			// The reservation must be untouched: the owner can still act on it.
			after := f.confirm(t, "owner", "k-owner-confirm", held.ReservationID)
			if after.Outcome != domain.OutcomeAdmittedSuccess {
				t.Errorf("after a rejected %s the owner got %q/%q; the impostor mutated the reservation",
					op, after.Outcome, after.Reason)
			}
		})
	}
}

// The owner's own path is unchanged, which is the other half of the correction: this is
// a rejection of impostors, not a new obstacle for the caller who owns the row.
func TestOwnerConfirmAndCancelStillSucceed(t *testing.T) {
	f := newFixture(t, 5)

	first := f.reserve(t, "owner", "k-1")
	if got := f.confirm(t, "owner", "k-2", first.ReservationID); got.Outcome != domain.OutcomeAdmittedSuccess {
		t.Fatalf("owner confirm: %q/%q", got.Outcome, got.Reason)
	}

	second := f.reserve(t, "owner2", "k-3")
	if got := f.cancel(t, "owner2", "k-4", second.ReservationID); got.Outcome != domain.OutcomeAdmittedSuccess {
		t.Fatalf("owner cancel: %q/%q", got.Outcome, got.Reason)
	}
}

// refusingSlotRepo is a repository whose slot lock cannot be taken. Anything that reaches
// LockSlot fails loudly instead of returning a plausible answer.
//
// TestCrossAuthorityRefusalTouchesNoSlotState proves the refusal changes no slot *state*,
// which a policy check moved to *after* LockSlot would also satisfy — capacity would still
// be untouched, and the cited negative control would look stronger than the test. This
// proves the stronger thing the design actually requires: the refusal never enters the
// slot path at all ("no slot lookup, reservation, claim, booking, or other slot-authority
// work" — horizontal-database design §5.3).
type refusingSlotRepo struct {
	inner  domain.Repository
	locked bool
}

func (r *refusingSlotRepo) WithinTx(ctx context.Context, fn func(context.Context, domain.Tx) error) error {
	return r.inner.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		return fn(ctx, &refusingSlotTx{Tx: tx, repo: r})
	})
}

type refusingSlotTx struct {
	domain.Tx
	repo *refusingSlotRepo
}

func (t *refusingSlotTx) LockSlot(ctx context.Context, ref domain.SlotRef) (domain.Slot, error) {
	t.repo.locked = true
	return domain.Slot{}, errSlotPathEntered
}

var errSlotPathEntered = errors.New("the cross-authority refusal entered the slot path")

func TestCrossAuthorityRefusalNeverEntersTheSlotPath(t *testing.T) {
	placement, err := domain.ParsePlacement([]byte(
		`{"version":"routing-v1","homes":{"org-a":"authority-1","org-b":"authority-2"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}

	clock := &manualClock{t: baseNow}
	store := inmem.New(clock)
	store.SeedSlot(domain.Slot{
		ID: slot, OrganisationID: "org-a", Capacity: 5,
		ReleaseAt: baseRelease, StartsAt: baseStart, EndsAt: baseStart.Add(time.Hour),
	})
	repo := &refusingSlotRepo{inner: store}
	svc := New(repo, &seqIDGen{}, testTTL, placement)

	got, err := svc.Reserve(context.Background(), ReserveCommand{
		UserRef:        domain.UserRef{OrganisationID: "org-b", UserID: "u-1"},
		SlotRef:        domain.SlotRef{OrganisationID: "org-a", SlotID: slot},
		IdempotencyKey: "k-1",
	})
	if err != nil {
		t.Fatalf("Reserve returned a fault: %v", err)
	}
	if repo.locked {
		t.Error("the refusal called LockSlot; it must be decided before any slot-authority work")
	}
	if got.Reason != domain.ReasonCrossAuthorityUnsupported {
		t.Errorf("reason = %q, want %q", got.Reason, domain.ReasonCrossAuthorityUnsupported)
	}

	// The control: a *supported* booking must still reach the slot path, so the spy is
	// proved to be watching something reachable rather than a path nothing takes.
	repo.locked = false
	if _, err := svc.Reserve(context.Background(), ReserveCommand{
		UserRef:        domain.UserRef{OrganisationID: "org-a", UserID: "u-2"},
		SlotRef:        domain.SlotRef{OrganisationID: "org-a", SlotID: slot},
		IdempotencyKey: "k-2",
	}); !errors.Is(err, errSlotPathEntered) {
		t.Fatalf("a supported reserve did not reach LockSlot (err = %v)", err)
	}
	if !repo.locked {
		t.Error("a supported reserve must reach the slot path")
	}
}
