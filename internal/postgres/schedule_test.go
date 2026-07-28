//go:build integration

package postgres

import (
	"context"
	"errors"
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

// Gate §10.1 8: the user is the caller's (user_organisation_id, user_id), never the
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
		`EXCLUDE USING gist (user_organisation_id WITH =, user_id WITH =, claim_range WITH &&)`

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
		`SELECT user_organisation_id FROM user_time_claims WHERE reservation_id = $1`, string(id)).Scan(&org)
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

// --- post-acquisition revalidation (§1.5) -----------------------------------

// holdConflictingClaim inserts a claim for the given user and interval in its own
// transaction, holds it uncommitted for d, then rolls back.
//
// It models the case that makes the claim wait observable: a reserve that blocks on a
// conflicting claim and *then succeeds*, because the transaction holding the range went
// away. The blocked transaction's slot-lock instant is stale by the length of the wait.
//
// The claim must be held on a *different* slot from the one under test. This transaction
// holds its slot's row lock for the whole wait, so a reserve targeting the same slot
// would block on LockSlot instead — and LockSlot already resolves its instant after that
// wait (§1.5), which is the very staleness this is meant to exhibit. Overlapping ranges
// on two different slot rows is the only shape that puts the wait at the claim.
func (h *harness) holdConflictingClaim(
	t *testing.T, user domain.UserRef, ref domain.SlotRef, resID domain.ReservationID,
	startsAt, endsAt time.Time, d time.Duration,
) (wait func()) {
	t.Helper()
	held := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		// A reservation row for the claim's deferred foreign key to satisfy at commit.
		// This transaction rolls back, so nothing it writes survives.
		err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
			if _, err := tx.LockSlot(ctx, ref); err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			if err := tx.PutReservation(ctx, domain.Reservation{
				ID: resID, SlotRef: ref, UserRef: user, State: domain.ReservationHeld,
				CreatedAt: now, ExpiresAt: startsAt,
			}); err != nil {
				return err
			}
			if _, err := tx.InsertClaim(ctx, domain.ScheduleClaim{
				ReservationID: resID, UserRef: user, SlotRef: ref,
				StartsAt: startsAt, EndsAt: endsAt, ExpiresAt: startsAt,
			}); err != nil {
				return err
			}
			close(held)
			time.Sleep(d)
			// Roll the whole thing back, releasing the range: the waiting reserve then
			// succeeds rather than conflicting.
			return errReleaseClaim
		})
		if err != nil && !errors.Is(err, errReleaseClaim) {
			t.Errorf("conflicting claim holder: %v", err)
			select {
			case <-held:
			default:
				close(held)
			}
		}
	}()

	<-held
	return func() { <-done }
}

// errReleaseClaim rolls the holder's transaction back once it has blocked the reserve
// under test for long enough.
var errReleaseClaim = errors.New("release conflicting claim")

// Gate: a reserve that waits on the claim authority and then succeeds must not commit a
// hold decided against the pre-wait instant.
//
// The slot closes *during* the wait. Under the pre-wait instant the slot is open and the
// TTL fits, so without re-evaluation this commits a hold on a slot that has already
// started — admitted_success for a booking that can never be confirmed.
func TestClaimWaitRevalidatesTheSlotWindow(t *testing.T) {
	const wait = 2 * time.Second

	h := newHarness(t, testBudget(), 500*time.Millisecond)
	base := h.dbNow(t)
	// Two slots sharing one window, so their claims overlap while their rows do not:
	// the blocker holds one, the reserve under test targets the other. The window opens
	// now and starts one second into the wait — open when the reserve begins, closed by
	// the time the claim authority is released.
	blocker := h.seedWindow(t, testOrg, "blocker-slot", 5, base, time.Second, time.Hour)
	slot := h.seedWindow(t, testOrg, "closing-slot", 5, base, time.Second, time.Hour)

	release := h.holdConflictingClaim(t, userRef("user-1"), blocker.Ref(), "res-blocker",
		slot.StartsAt, slot.EndsAt, wait)

	r, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", slot.Ref())
	release()
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// The slot started while the claim was contended, so the request must be refused.
	assertOutcome(t, r, domain.OutcomeBusinessRefusal, domain.ReasonSlotClosed)
	// And the provisional claim must not have outlived the decision that created it.
	assertClaimCount(t, h, 0)
	assertSlotCounts(t, h, slot.Ref(), 0, 0)
}

// Gate: the hold's TTL is recomputed from the post-acquisition instant, so a hold is
// never committed already expired or silently shortened (§1.6).
func TestClaimWaitRecomputesTheHoldTTL(t *testing.T) {
	const wait = 2 * time.Second
	const ttl = 30 * time.Second

	h := newHarness(t, testBudget(), ttl)
	base := h.dbNow(t)
	// Same shape as above: overlapping windows on two different slot rows, so the wait
	// falls on the claim rather than on the slot lock.
	blocker := h.seedWindow(t, testOrg, "blocker-slot", 5, base, time.Hour, 2*time.Hour)
	slot := h.seedWindow(t, testOrg, "open-slot", 5, base, time.Hour, 2*time.Hour)

	release := h.holdConflictingClaim(t, userRef("user-1"), blocker.Ref(), "res-blocker",
		slot.StartsAt, slot.EndsAt, wait)

	before := h.dbNow(t)
	r, err := h.reserveAs(context.Background(), testOrg, "user-1", "key-1", slot.Ref())
	release()
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeAdmittedSuccess, "")

	var createdAt, expiresAt time.Time
	err = h.repo.pool.QueryRow(context.Background(),
		`SELECT created_at, expires_at FROM reservations WHERE reservation_id = $1`,
		string(r.ReservationID)).Scan(&createdAt, &expiresAt)
	if err != nil {
		t.Fatalf("read reservation: %v", err)
	}

	// Stamped from the instant the claim was acquired, which is after the wait — not
	// from the slot-lock instant before it. The two readings are ~wait apart versus ~0,
	// so a generous threshold separates them without depending on scheduling jitter.
	if gap := createdAt.Sub(before); gap < wait/2 {
		t.Errorf("created_at is only %v after the pre-wait instant, want most of the %v wait: "+
			"the hold was stamped before the claim authority was acquired", gap, wait)
	}
	// The full TTL is granted from that instant, not eroded by the wait.
	if got := expiresAt.Sub(createdAt); got != ttl {
		t.Errorf("hold length = %v, want the full %v TTL", got, ttl)
	}

	// The claim agrees with the hold, or settlement would act on one and not the other.
	var claimExpiry time.Time
	err = h.repo.pool.QueryRow(context.Background(),
		`SELECT expires_at FROM user_time_claims WHERE reservation_id = $1`,
		string(r.ReservationID)).Scan(&claimExpiry)
	if err != nil {
		t.Fatalf("read claim: %v", err)
	}
	if !claimExpiry.Equal(expiresAt) {
		t.Errorf("claim expires_at = %v, reservation expires_at = %v: they must agree",
			claimExpiry, expiresAt)
	}
}

// Gate: concurrent reserves for one user whose elapsed claims sit on different slots must
// not deadlock.
//
// Before user-scoped settlement became the sole expiry-removal path, each transaction's
// slot settlement deleted — and so locked — its own slot's claim row, and the following
// user-scoped delete then wanted the other transaction's row. That is a cycle, and
// PostgreSQL resolves it by aborting one transaction, turning a valid reserve into an
// internal failure.
func TestConcurrentSettlementOfOneUserDoesNotDeadlock(t *testing.T) {
	const ttl = 200 * time.Millisecond
	const rounds = 6

	// Two slots whose windows do not overlap, so one user may hold both. Each reserve
	// targets the slot its own elapsed hold sits on: that is what made the two paths
	// collide, because slot-scoped settlement would delete (and so lock) *this* slot's
	// claim before user-scoped settlement asked for the other's.
	for round := range rounds {
		h := newHarness(t, testBudget(), ttl)
		base := h.dbNow(t)
		a := h.seedWindow(t, testOrg, "deadlock-a", 5, base, time.Hour, 2*time.Hour)
		b := h.seedWindow(t, testOrg, "deadlock-b", 5, base, 3*time.Hour, 4*time.Hour)

		for i, ref := range []domain.SlotRef{a.Ref(), b.Ref()} {
			if _, err := h.reserveAs(context.Background(), testOrg, "user-1",
				fmt.Sprintf("stale-%d", i), ref); err != nil {
				t.Fatalf("round %d: seed stale hold: %v", round, err)
			}
		}
		time.Sleep(2 * ttl) // both holds elapse; nothing has settled them

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			failures []error
		)
		start := make(chan struct{})
		for i, ref := range []domain.SlotRef{a.Ref(), b.Ref()} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				r, err := h.reserveAs(context.Background(), testOrg, "user-1",
					fmt.Sprintf("key-%d", i), ref)

				mu.Lock()
				defer mu.Unlock()
				switch {
				case err != nil:
					failures = append(failures, fmt.Errorf("%s: %w", ref.SlotID, err))
				case r.Outcome != domain.OutcomeAdmittedSuccess:
					failures = append(failures, fmt.Errorf("%s: outcome %q/%q", ref.SlotID, r.Outcome, r.Reason))
				}
			}()
		}
		close(start)
		wg.Wait()

		for _, err := range failures {
			t.Fatalf("round %d: concurrent reserve failed, want both admitted "+
				"(a deadlock surfaces here as an error, not a refusal): %v", round, err)
		}
		// Both elapsed claims settled, both new holds claimed.
		assertClaimCount(t, h, 2)
		assertNoOverlappingClaims(t, h)
	}
}
