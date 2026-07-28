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

// The user schedule non-overlap gates (docs/design-notes/user-schedule-non-overlap.md
// §10, transaction-semantics §2.2):
//
//	for one identity, no two active booking claims may overlap in time
//
// These live against real PostgreSQL because the mechanism *is* a PostgreSQL exclusion
// constraint. The in-memory reference proves the service drives the claim lifecycle
// correctly when serialized; only the database can prove that two transactions locking
// *different* slot rows — which therefore never contend on the existing aggregate lock —
// still cannot both commit.
//
// The negative control that makes these gates credible is at the bottom of the file.

const otherOrg = domain.OrganisationID("org-2")

// guestSlot is owned by otherOrg, so booking it from a testOrg identity is the
// cross-organisation case: the caller's identity organisation and the slot's owning
// organisation differ.
var guestSlot = domain.SlotRef{OrganisationID: otherOrg, SlotID: "guest-slot"}

// scheduleHarness sets up three slots on one shared time base so interval
// relationships are exact:
//
//	slotEarly  [+1h, +2h)
//	slotMid    [+90m, +150m)   overlaps slotEarly
//	slotLate   [+2h, +3h)      adjacent to slotEarly, so it must NOT conflict
func scheduleHarness(t *testing.T, ttl time.Duration) (*harness, time.Time) {
	t.Helper()
	h := newHarness(t, testBudget(), ttl)
	base := h.dbNow(t)
	h.seedWindow(t, testOrg, "slot-early", 5, base, time.Hour, 2*time.Hour)
	h.seedWindow(t, testOrg, "slot-mid", 5, base, 90*time.Minute, 150*time.Minute)
	h.seedWindow(t, testOrg, "slot-late", 5, base, 2*time.Hour, 3*time.Hour)
	return h, base
}

// Gates §10.1 1–7: interval semantics, evaluated one relationship at a time.
func TestScheduleIntervalSemantics(t *testing.T) {
	tests := []struct {
		name        string
		startsIn    time.Duration
		endsIn      time.Duration
		wantRefusal bool
	}{
		// The identity already holds [+1h, +2h).
		{"identical intervals conflict", time.Hour, 2 * time.Hour, true},
		{"partial overlap from the right conflicts", 90 * time.Minute, 150 * time.Minute, true},
		{"partial overlap from the left conflicts", 30 * time.Minute, 90 * time.Minute, true},
		{"a contained interval conflicts", 75 * time.Minute, 105 * time.Minute, true},
		{"a containing interval conflicts", 30 * time.Minute, 150 * time.Minute, true},
		{"an adjacent interval after does not conflict", 2 * time.Hour, 3 * time.Hour, false},
		// Ends exactly where the held interval begins. Still in the future: a slot
		// starting at base would already be closed, which would refuse for an unrelated
		// reason and prove nothing about adjacency.
		{"an adjacent interval before does not conflict", 30 * time.Minute, time.Hour, false},
		{"a disjoint interval does not conflict", 5 * time.Hour, 6 * time.Hour, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, testBudget(), 30*time.Second)
			base := h.dbNow(t)
			h.seedWindow(t, testOrg, "slot-held", 5, base, time.Hour, 2*time.Hour)
			h.seedWindow(t, testOrg, "slot-under-test", 5, base, tc.startsIn, tc.endsIn)

			first, err := h.reserve(context.Background(), "user-1", "key-1", "slot-held")
			if err != nil {
				t.Fatalf("first reserve: %v", err)
			}
			assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")

			second, err := h.reserve(context.Background(), "user-1", "key-2", "slot-under-test")
			if err != nil {
				t.Fatalf("second reserve: %v", err)
			}
			if tc.wantRefusal {
				// Note the reason: the slot itself has capacity 5 and is nearly empty. It is
				// the identity's own schedule that is full.
				assertOutcome(t, second, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
				assertClaimCount(t, h, 1)
			} else {
				assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
				assertClaimCount(t, h, 2)
			}
			assertNoOverlappingClaims(t, h)
		})
	}
}

// Gate §10.1 7: the invariant is per identity, so two identities may overlap freely.
func TestOverlappingIntervalsForDifferentIdentitiesBothSucceed(t *testing.T) {
	h, _ := scheduleHarness(t, 30*time.Second)

	first, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	second, err := h.reserve(context.Background(), "user-2", "key-2", "slot-mid")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	assertNoOverlappingClaims(t, h)
}

// Gate §10.1 8: the identity is the *caller's* (organisation_id, user_id), never the
// slot's organisation.
//
// This is the case the design turns on. A member of org-1 books a slot owned by org-1
// and then an overlapping slot owned by org-2 — a guest visit. Both must be refused as
// one identity's schedule, even though the slots belong to different organisations. And
// (org-2, user-1) must be a *different* identity that is unaffected.
func TestScheduleIdentityIsTheCallersOrganisationNotTheSlots(t *testing.T) {
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)
	h.seedWindow(t, testOrg, "home-slot", 5, base, time.Hour, 2*time.Hour)
	h.seedWindow(t, otherOrg, "guest-slot", 5, base, 90*time.Minute, 150*time.Minute)

	home, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", slotRef("home-slot"))
	if err != nil {
		t.Fatalf("home reserve: %v", err)
	}
	assertOutcome(t, home, domain.OutcomeAdmittedSuccess, "")

	// Same identity, a slot owned by another organisation, overlapping time.
	guest, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-2", guestSlot)
	if err != nil {
		t.Fatalf("guest reserve: %v", err)
	}
	assertOutcome(t, guest, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)

	// A different identity that merely shares the user_id: unaffected. This is what makes
	// user_id not need to be globally unique.
	other, err := h.reserveAs(context.Background(), otherOrg, "user-1", "key-3", guestSlot)
	if err != nil {
		t.Fatalf("other-identity reserve: %v", err)
	}
	assertOutcome(t, other, domain.OutcomeAdmittedSuccess, "")

	// And the claim that exists is keyed by the caller's organisation, not the slot's.
	assertClaimOwner(t, h, home.ReservationID, testOrg)
	assertClaimOwner(t, h, other.ReservationID, otherOrg)
	assertNoOverlappingClaims(t, h)
}

// Gates §10.2 9–11, 14: a hold and the booking confirmed from it are one claim.
func TestConfirmKeepsOneClaimAndKeepsBlocking(t *testing.T) {
	h, _ := scheduleHarness(t, 30*time.Second)

	held, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	confirmed, err := h.confirm(context.Background(), "user-1", "key-2", held.ReservationID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	assertOutcome(t, confirmed, domain.OutcomeAdmittedSuccess, "")

	// Confirming must not have inserted a second claim: a booking cannot conflict with
	// the hold it came from. If it had, this count would be 2 and the invariant check
	// below would find the self-overlap.
	assertClaimCount(t, h, 1)
	assertNoOverlappingClaims(t, h)

	// The confirmed claim still blocks, and it no longer expires.
	blocked, err := h.reserve(context.Background(), "user-1", "key-3", "slot-mid")
	if err != nil {
		t.Fatalf("overlapping reserve: %v", err)
	}
	assertOutcome(t, blocked, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
	assertClaimExpiry(t, h, held.ReservationID, false)
}

// Gate §10.2 12, 15: cancellation frees the identity's time atomically with the
// lifecycle transition — for a held reservation and for a confirmed booking.
func TestCancellationReleasesTheClaim(t *testing.T) {
	for _, tc := range []struct {
		name    string
		confirm bool
	}{
		{"cancelling a held reservation", false},
		{"cancelling a confirmed booking", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := scheduleHarness(t, 30*time.Second)

			held, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early")
			if err != nil {
				t.Fatalf("reserve: %v", err)
			}
			if tc.confirm {
				if _, err := h.confirm(context.Background(), "user-1", "key-2", held.ReservationID); err != nil {
					t.Fatalf("confirm: %v", err)
				}
			}
			cancelled, err := h.cancel(context.Background(), "user-1", "key-3", held.ReservationID)
			if err != nil {
				t.Fatalf("cancel: %v", err)
			}
			assertOutcome(t, cancelled, domain.OutcomeAdmittedSuccess, "")
			assertClaimCount(t, h, 0)

			// The freed interval is immediately reusable.
			again, err := h.reserve(context.Background(), "user-1", "key-4", "slot-mid")
			if err != nil {
				t.Fatalf("reserve after cancel: %v", err)
			}
			assertOutcome(t, again, domain.OutcomeAdmittedSuccess, "")
			assertNoOverlappingClaims(t, h)
		})
	}
}

// Gate §10.2 13: an elapsed hold stops blocking without the expiry worker having run.
//
// This is the gate that decides the schema. The abandoned hold is on a *different* slot
// from the new request, so the slot-scoped settlement never sees it: only the
// identity-scoped claim settlement can clear it. There is no worker in AG-M1 PR4 at all,
// so nothing else could.
func TestElapsedHoldStopsBlockingWithoutTheWorker(t *testing.T) {
	const ttl = 200 * time.Millisecond
	h, _ := scheduleHarness(t, ttl)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early"); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	// The client vanishes: no confirm, no cancel, and no worker exists to tidy up.
	time.Sleep(2 * ttl)

	second, err := h.reserve(context.Background(), "user-1", "key-2", "slot-mid")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	// The dead claim was removed rather than merely ignored, so the relation holds only
	// live claims.
	assertClaimCount(t, h, 1)
	assertNoOverlappingClaims(t, h)
}

// Gate §10.3 16: THE concurrency acceptance gate.
//
// Many concurrent reserves for one identity across a set of mutually overlapping slots.
// Every transaction locks a different slot row, so the existing aggregate lock never
// serializes any pair of them: without the schedule authority they would all commit.
func TestConcurrentOverlappingReservesForOneIdentityYieldOneSuccess(t *testing.T) {
	const contenders = 24

	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)
	// Every slot contains base+90m, so all of them mutually overlap, but each is its own
	// row and therefore its own lock.
	for i := range contenders {
		h.seedWindow(t, testOrg, domain.SlotID(fmt.Sprintf("slot-%d", i)), 5, base,
			time.Duration(i)*time.Minute+time.Hour, 2*time.Hour)
	}

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
			r, err := h.reserve(context.Background(), "user-1",
				fmt.Sprintf("key-%d", i), domain.SlotID(fmt.Sprintf("slot-%d", i)))

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failures = append(failures, err)
			case r.Outcome == domain.OutcomeAdmittedSuccess:
				succeeded++
			case r.Outcome == domain.OutcomeBusinessRefusal && r.Reason == domain.ReasonScheduleConflict:
				refused++
			default:
				failures = append(failures, fmt.Errorf("unexpected outcome %q/%q", r.Outcome, r.Reason))
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("reserve failed: %v", err)
	}
	if succeeded != 1 {
		t.Errorf("admitted %d overlapping reserves for one identity, want exactly 1 (refused %d)", succeeded, refused)
	}
	assertClaimCount(t, h, 1)
	assertNoOverlappingClaims(t, h)
}

// Gate §10.3 17: non-overlapping concurrent reserves for one identity all succeed. The
// invariant must refuse overlap, not serialize an identity's whole schedule.
func TestConcurrentNonOverlappingReservesForOneIdentityAllSucceed(t *testing.T) {
	const contenders = 12

	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)
	for i := range contenders {
		// Back-to-back hours: adjacent, never overlapping.
		h.seedWindow(t, testOrg, domain.SlotID(fmt.Sprintf("slot-%d", i)), 5, base,
			time.Duration(i)*time.Hour+time.Hour, time.Duration(i)*time.Hour+2*time.Hour)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []error
	)
	start := make(chan struct{})

	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := h.reserve(context.Background(), "user-1",
				fmt.Sprintf("key-%d", i), domain.SlotID(fmt.Sprintf("slot-%d", i)))

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
			} else if r.Outcome != domain.OutcomeAdmittedSuccess {
				failures = append(failures, fmt.Errorf("outcome %q/%q, want admitted_success", r.Outcome, r.Reason))
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("reserve failed: %v", err)
	}
	assertClaimCount(t, h, contenders)
	assertNoOverlappingClaims(t, h)
}

// Gate §10.3 19: a reserve racing cancellation of the claim it conflicts with resolves
// to one committed outcome — either the cancel lands first and the reserve succeeds, or
// the reserve is refused. What must never happen is an overlap, or a fault.
func TestReserveRacingCancellationHasOneCommittedOutcome(t *testing.T) {
	h, _ := scheduleHarness(t, 30*time.Second)

	held, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	var (
		wg                       sync.WaitGroup
		reserveResult, cancelRes domain.Result
		reserveErr, cancelErr    error
	)
	start := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		reserveResult, reserveErr = h.reserve(context.Background(), "user-1", "key-2", "slot-mid")
	}()
	go func() {
		defer wg.Done()
		<-start
		cancelRes, cancelErr = h.cancel(context.Background(), "user-1", "key-3", held.ReservationID)
	}()
	close(start)
	wg.Wait()

	if reserveErr != nil {
		t.Fatalf("racing reserve: %v", reserveErr)
	}
	if cancelErr != nil {
		t.Fatalf("racing cancel: %v", cancelErr)
	}
	assertOutcome(t, cancelRes, domain.OutcomeAdmittedSuccess, "")
	switch {
	case reserveResult.Outcome == domain.OutcomeAdmittedSuccess:
	case reserveResult.Outcome == domain.OutcomeBusinessRefusal && reserveResult.Reason == domain.ReasonScheduleConflict:
	default:
		t.Errorf("racing reserve resolved to %q/%q, want success or schedule_conflict",
			reserveResult.Outcome, reserveResult.Reason)
	}
	assertNoOverlappingClaims(t, h)
}

// Gate §10.4 22, and the fault line: a schedule conflict is a *business refusal*, and it
// must stay distinguishable from a database timeout. A reserve that waits behind a
// conflicting uncommitted transaction until lock_timeout is timeout_db, not a refusal —
// the request never learned whether the identity's time was taken.
func TestScheduleConflictIsARefusalNotAFault(t *testing.T) {
	h, _ := scheduleHarness(t, 30*time.Second)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	r, err := h.reserve(context.Background(), "user-1", "key-2", "slot-mid")
	if err != nil {
		t.Fatalf("conflicting reserve returned an error, want a business refusal: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)

	// The refusal is idempotent like any other outcome: replaying the key returns the
	// recorded refusal rather than re-deciding it.
	replay, err := h.reserve(context.Background(), "user-1", "key-2", "slot-mid")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	assertOutcome(t, replay, domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict)
	if !replay.Replay {
		t.Error("replayed refusal not marked as a replay")
	}
}

// --- negative controls ------------------------------------------------------

// Gate §10.3 21 — the negative control that makes every gate above credible.
//
// A passing concurrency test proves nothing on its own: it may be passing because the
// race never actually occurred. This test removes the protection and asserts that the
// race then *does* produce overlapping committed state. If this test ever starts
// failing — if the overlap stops appearing once the constraint is dropped — then the
// acceptance gate above has stopped discriminating and is no longer evidence.
//
// The constraint is dropped and restored inside the test, so the removal cannot leak
// into another test.
func TestNegativeControlDroppingTheConstraintAdmitsOverlap(t *testing.T) {
	const contenders = 24

	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)
	for i := range contenders {
		h.seedWindow(t, testOrg, domain.SlotID(fmt.Sprintf("slot-%d", i)), 5, base,
			time.Duration(i)*time.Minute+time.Hour, 2*time.Hour)
	}

	dropScheduleConstraint(t, h)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Errors are not asserted: without the constraint this path is undefined, and
			// the point of the test is the persisted state below.
			_, _ = h.reserve(context.Background(), "user-1",
				fmt.Sprintf("key-%d", i), domain.SlotID(fmt.Sprintf("slot-%d", i)))
		}()
	}
	close(start)
	wg.Wait()

	overlaps, err := h.repo.OverlappingClaims(context.Background())
	if err != nil {
		t.Fatalf("overlapping claims: %v", err)
	}
	if overlaps == 0 {
		t.Error("dropping the exclusion constraint produced no overlapping claims: " +
			"the concurrency gate is not discriminating, because the race it claims to " +
			"catch is not actually happening")
	}
}

// Gate §10.2 13's negative control: without identity-scoped claim settlement, an elapsed
// hold on another slot goes on blocking the identity forever.
//
// Settlement is not something the schema can be stripped of, so this control removes the
// effect instead: it verifies that the elapsed claim is still present *before* the next
// reserve, which is precisely the state that would refuse the request if settlement did
// not run. If settlement were removed, TestElapsedHoldStopsBlockingWithoutTheWorker
// would fail with schedule_conflict.
func TestNegativeControlElapsedClaimSurvivesUntilSettled(t *testing.T) {
	const ttl = 200 * time.Millisecond
	h, _ := scheduleHarness(t, ttl)

	if _, err := h.reserve(context.Background(), "user-1", "key-1", "slot-early"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	time.Sleep(2 * ttl)

	// Nothing has run: no worker, no further request for this identity. The dead claim is
	// still there, and it is what settlement has to clear.
	assertClaimCount(t, h, 1)

	// A *different* identity reserving the overlapping slot does not settle it — proving
	// the claim really is still live in the table rather than already gone.
	if _, err := h.reserve(context.Background(), "user-2", "key-2", "slot-mid"); err != nil {
		t.Fatalf("other identity reserve: %v", err)
	}
	assertClaimCount(t, h, 2)

	// Now the owning identity reserves, and settlement clears its dead claim.
	second, err := h.reserve(context.Background(), "user-1", "key-3", "slot-mid")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	assertClaimCount(t, h, 2)
}

// --- helpers ----------------------------------------------------------------

func dropScheduleConstraint(t *testing.T, h *harness) {
	t.Helper()
	const drop = `ALTER TABLE user_time_claims DROP CONSTRAINT user_time_claims_no_overlap`
	const restore = `ALTER TABLE user_time_claims ADD CONSTRAINT user_time_claims_no_overlap ` +
		`EXCLUDE USING gist (organisation_id WITH =, user_id WITH =, claim_range WITH &&)`

	if _, err := h.repo.pool.Exec(context.Background(), drop); err != nil {
		t.Fatalf("drop exclusion constraint: %v", err)
	}
	t.Cleanup(func() {
		// Truncate first: the overlapping rows this test deliberately created would
		// otherwise make the constraint impossible to re-add.
		if err := h.repo.Truncate(context.Background()); err != nil {
			t.Errorf("truncate before restoring constraint: %v", err)
		}
		if _, err := h.repo.pool.Exec(context.Background(), restore); err != nil {
			t.Errorf("restore exclusion constraint: %v", err)
		}
	})
}

func assertClaimOwner(t *testing.T, h *harness, id domain.ReservationID, want domain.OrganisationID) {
	t.Helper()
	var org string
	err := h.repo.pool.QueryRow(context.Background(),
		`SELECT organisation_id FROM user_time_claims WHERE reservation_id = $1`, string(id)).Scan(&org)
	if err != nil {
		t.Fatalf("read claim owner for %q: %v", id, err)
	}
	if domain.OrganisationID(org) != want {
		t.Errorf("claim %q keyed by organisation %q, want %q", id, org, want)
	}
}

func assertClaimExpiry(t *testing.T, h *harness, id domain.ReservationID, wantSet bool) {
	t.Helper()
	var expiresAt *time.Time
	err := h.repo.pool.QueryRow(context.Background(),
		`SELECT expires_at FROM user_time_claims WHERE reservation_id = $1`, string(id)).Scan(&expiresAt)
	if err != nil {
		t.Fatalf("read claim expiry for %q: %v", id, err)
	}
	if gotSet := expiresAt != nil; gotSet != wantSet {
		t.Errorf("claim %q expires_at set = %t, want %t (a confirmed claim must be permanent)", id, gotSet, wantSet)
	}
}
