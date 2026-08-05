package service

import (
	"context"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/inmem"
)

// User schedule non-overlap, proven at the service layer against the in-memory
// reference (transaction-semantics §2.2). The division of labour is the repo's usual
// one: this file proves the *claim lifecycle* — that reserve, confirm, cancel, and
// expiry drive claims correctly when transactions are serialized — while
// internal/postgres proves that PostgreSQL actually serializes the different-slot race
// that no slot lock can catch.

// scheduleFixture seeds three slots on one timeline. The identity's schedule, not any
// slot's capacity, is what these tests exercise, so every slot has room to spare:
//
//	slotA  [11:00, 12:00)
//	slotB  [11:30, 12:30)   overlaps slotA
//	slotC  [12:00, 13:00)   adjacent to slotA
func scheduleFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &manualClock{t: baseNow}
	store := inmem.New(clock)
	for _, s := range []struct {
		id           domain.SlotID
		starts, ends time.Time
	}{
		{"slot-a", baseStart, baseStart.Add(time.Hour)},
		{"slot-b", baseStart.Add(30 * time.Minute), baseStart.Add(90 * time.Minute)},
		{"slot-c", baseStart.Add(time.Hour), baseStart.Add(2 * time.Hour)},
	} {
		store.SeedSlot(domain.Slot{
			ID: s.id, OrganisationID: org, Capacity: 5,
			ReleaseAt: baseRelease, StartsAt: s.starts, EndsAt: s.ends,
		})
	}
	ids := &seqIDGen{}
	return &fixture{svc: New(store, ids, testTTL, domain.Unsharded("authority-1")), store: store, clock: clock, ids: ids}
}

func (f *fixture) reserveSlot(t *testing.T, name, key string, slotID domain.SlotID) domain.Result {
	t.Helper()
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{
		UserRef: user(name), SlotRef: ref(slotID), IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("Reserve(%s,%s,%s): unexpected error %v", name, key, slotID, err)
	}
	return r
}

func (f *fixture) reserveAs(t *testing.T, orgID domain.OrganisationID, name, key string, slotID domain.SlotID) domain.Result {
	t.Helper()
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{
		UserRef:        domain.UserRef{OrganisationID: orgID, UserID: domain.UserID(name)},
		SlotRef:        ref(slotID),
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("Reserve(%s/%s): unexpected error %v", orgID, name, err)
	}
	return r
}

func assertClaims(t *testing.T, f *fixture, want int) {
	t.Helper()
	if got := len(f.store.Claims()); got != want {
		t.Errorf("persisted claims = %d, want %d", got, want)
	}
}

func TestOverlappingReserveForOneIdentityIsRefused(t *testing.T) {
	f := scheduleFixture(t)

	first := f.reserveSlot(t, "user-1", "k1", "slot-a")
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")

	// A different slot, with capacity to spare, but overlapping time.
	second := f.reserveSlot(t, "user-1", "k2", "slot-b")
	assertOutcome(t, second, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
	assertClaims(t, f, 1)
}

func TestAdjacentReserveForOneIdentitySucceeds(t *testing.T) {
	f := scheduleFixture(t)

	assertOutcome(t, f.reserveSlot(t, "user-1", "k1", "slot-a"), domain.OutcomeAdmittedSuccess, "")
	// [11:00,12:00) and [12:00,13:00) touch but do not overlap: half-open intervals.
	assertOutcome(t, f.reserveSlot(t, "user-1", "k2", "slot-c"), domain.OutcomeAdmittedSuccess, "")
	assertClaims(t, f, 2)
}

func TestOverlappingReserveForDifferentIdentitiesSucceeds(t *testing.T) {
	f := scheduleFixture(t)

	assertOutcome(t, f.reserveSlot(t, "user-1", "k1", "slot-a"), domain.OutcomeAdmittedSuccess, "")
	assertOutcome(t, f.reserveSlot(t, "user-2", "k2", "slot-b"), domain.OutcomeAdmittedSuccess, "")

	// Same user_id, different organisation: a different identity, and therefore a
	// different schedule. This is what lets user_id not be globally unique.
	assertOutcome(t, f.reserveAs(t, "org-2", "user-1", "k3", "slot-b"), domain.OutcomeAdmittedSuccess, "")
	assertClaims(t, f, 3)
}

// Confirming must keep exactly one claim: the hold's claim becomes the booking's. A
// second claim would overlap the first and the identity would conflict with itself.
func TestConfirmKeepsOneClaimAndKeepsBlocking(t *testing.T) {
	f := scheduleFixture(t)

	held := f.reserveSlot(t, "user-1", "k1", "slot-a")
	assertOutcome(t, f.confirm(t, "user-1", "k2", held.ReservationID), domain.OutcomeAdmittedSuccess, "")
	assertClaims(t, f, 1)

	// Still blocking after confirmation, and now permanent.
	assertOutcome(t, f.reserveSlot(t, "user-1", "k3", "slot-b"),
		domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)

	claims := f.store.Claims()
	if len(claims) == 1 && !claims[0].ExpiresAt.IsZero() {
		t.Error("confirmed claim still carries an expiry: settlement could remove a live booking's claim")
	}
}

func TestCancelReleasesTheIdentitysTime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		confirm bool
	}{
		{"held reservation", false},
		{"confirmed booking", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scheduleFixture(t)
			held := f.reserveSlot(t, "user-1", "k1", "slot-a")
			if tc.confirm {
				f.confirm(t, "user-1", "k2", held.ReservationID)
			}
			assertOutcome(t, f.cancel(t, "user-1", "k3", held.ReservationID), domain.OutcomeAdmittedSuccess, "")
			assertClaims(t, f, 0)

			// The interval is free again immediately, in the same committed state.
			assertOutcome(t, f.reserveSlot(t, "user-1", "k4", "slot-b"), domain.OutcomeAdmittedSuccess, "")
		})
	}
}

// The gate that decides the schema: an elapsed hold must stop blocking the identity
// without any worker having run.
//
// The abandoned hold is on slot-a and the new request is for slot-b, so the slot-scoped
// settlement never sees it — only identity-scoped claim settlement can. Removing the
// SettleClaims call from Service.Reserve makes this test fail with schedule_conflict,
// which is what makes it evidence rather than decoration.
func TestElapsedHoldStopsBlockingWithoutTheWorker(t *testing.T) {
	f := scheduleFixture(t)

	assertOutcome(t, f.reserveSlot(t, "user-1", "k1", "slot-a"), domain.OutcomeAdmittedSuccess, "")
	// The client abandons the flow and the hold's TTL lapses.
	f.clock.set(baseNow.Add(testTTL + time.Second))

	assertOutcome(t, f.reserveSlot(t, "user-1", "k2", "slot-b"), domain.OutcomeAdmittedSuccess, "")
	// Settled, not merely ignored: the relation holds only live claims.
	assertClaims(t, f, 1)
}

// Expiry settlement on the slot path deliberately leaves the claim alone.
//
// Removing an elapsed claim is user-scoped settlement's sole job (§2.2), so no
// transaction ever locks a claim row belonging to a user other than the one it acts for.
// That is what makes claim-row deadlock unreachable — two reserves for one user with
// elapsed claims on different slots would otherwise each hold the other's next row.
//
// The surviving claim is harmless: it can only block its own user.
func TestSlotSettlementLeavesTheClaimToItsOwner(t *testing.T) {
	f := scheduleFixture(t)

	assertOutcome(t, f.reserveSlot(t, "user-1", "k1", "slot-a"), domain.OutcomeAdmittedSuccess, "")
	f.clock.set(baseNow.Add(testTTL + time.Second))

	// A different user reserving the same slot expires user-1's hold, but must not touch
	// user-1's claim row.
	assertOutcome(t, f.reserveSlot(t, "user-2", "k2", "slot-a"), domain.OutcomeAdmittedSuccess, "")
	assertClaims(t, f, 2)

	// And it never blocked user-2, whose interval overlaps it: claims are per user.
	owners := map[domain.UserID]bool{}
	for _, c := range f.store.Claims() {
		owners[c.UserRef.UserID] = true
	}
	if !owners["user-1"] || !owners["user-2"] {
		t.Errorf("claim owners = %v, want both user-1 (elapsed, awaiting settlement) and user-2", owners)
	}

	// user-1's own next reserve settles it, so the relation does not grow without bound.
	assertOutcome(t, f.reserveSlot(t, "user-1", "k3", "slot-c"), domain.OutcomeAdmittedSuccess, "")
	assertClaims(t, f, 2)
}

// A schedule conflict is an ordinary business refusal, so it is recorded and replayed
// like any other outcome rather than being re-decided against changed state.
func TestScheduleConflictReplaysAsARefusal(t *testing.T) {
	f := scheduleFixture(t)

	held := f.reserveSlot(t, "user-1", "k1", "slot-a")
	refused := f.reserveSlot(t, "user-1", "k2", "slot-b")
	assertOutcome(t, refused, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)

	// Free the time, then replay the refused key: it must return the recorded refusal,
	// not re-evaluate and succeed.
	f.cancel(t, "user-1", "k3", held.ReservationID)
	replay := f.reserveSlot(t, "user-1", "k2", "slot-b")
	assertOutcome(t, replay, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
	if !replay.Replay {
		t.Error("replayed refusal not marked as a replay")
	}
	assertClaims(t, f, 0)
}
