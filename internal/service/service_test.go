package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/inmem"
)

// --- test doubles -----------------------------------------------------------

// manualClock is a controllable Clock. It is safe for concurrent use so race tests
// can read the time from many goroutines.
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// seqIDGen mints deterministic, sequential IDs. Safe for concurrent use.
type seqIDGen struct {
	mu       sync.Mutex
	res, bok int
}

func (g *seqIDGen) NewReservationID() domain.ReservationID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.res++
	return domain.ReservationID(fmt.Sprintf("res-%d", g.res))
}

func (g *seqIDGen) NewBookingID() domain.BookingID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bok++
	return domain.BookingID(fmt.Sprintf("bk-%d", g.bok))
}

// --- fixture ----------------------------------------------------------------

var (
	baseRelease = time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	baseNow     = time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC) // released, open
	baseStart   = time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	testTTL     = 30 * time.Second
)

const (
	org  = domain.OrganisationID("org-1")
	slot = domain.SlotID("slot-1")
)

type fixture struct {
	svc   *Service
	store *inmem.Store
	clock *manualClock
	ids   *seqIDGen
}

func newFixture(t *testing.T, capacity int) *fixture {
	t.Helper()
	store := inmem.New()
	store.SeedSlot(domain.Slot{
		ID: slot, OrganisationID: org, Capacity: capacity,
		ReleaseAt: baseRelease, StartsAt: baseStart, EndsAt: baseStart.Add(time.Hour),
	})
	clock := &manualClock{t: baseNow}
	ids := &seqIDGen{}
	return &fixture{svc: New(store, clock, ids, testTTL), store: store, clock: clock, ids: ids}
}

func (f *fixture) reserve(t *testing.T, user, key string) domain.Result {
	t.Helper()
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: domain.UserID(user), SlotID: slot, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Reserve(%s,%s): unexpected error %v", user, key, err)
	}
	return r
}

func (f *fixture) confirm(t *testing.T, user, key string, res domain.ReservationID) domain.Result {
	t.Helper()
	r, err := f.svc.Confirm(context.Background(), ConfirmCommand{OrganisationID: org, UserID: domain.UserID(user), ReservationID: res, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Confirm: unexpected error %v", err)
	}
	return r
}

func (f *fixture) cancel(t *testing.T, user, key string, res domain.ReservationID) domain.Result {
	t.Helper()
	r, err := f.svc.Cancel(context.Background(), CancelCommand{OrganisationID: org, UserID: domain.UserID(user), ReservationID: res, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Cancel: unexpected error %v", err)
	}
	return r
}

func assertOutcome(t *testing.T, r domain.Result, outcome domain.Outcome, reason domain.Reason) {
	t.Helper()
	if r.Outcome != outcome {
		t.Fatalf("Outcome = %q (reason %q), want %q", r.Outcome, r.Reason, outcome)
	}
	if r.Reason != reason {
		t.Fatalf("Reason = %q, want %q", r.Reason, reason)
	}
}

// --- reserve ----------------------------------------------------------------

func TestReserveSuccessThenSoldOut(t *testing.T) {
	f := newFixture(t, 1)
	r := f.reserve(t, "user-1", "k1")
	assertOutcome(t, r, domain.OutcomeAdmittedSuccess, "")
	if r.ReservationID == "" {
		t.Fatal("success must carry a reservation ID")
	}
	// Capacity of 1 is now consumed; a distinct request is refused, not failed.
	assertOutcome(t, f.reserve(t, "user-2", "k2"), domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)
}

func TestReserveBeforeReleaseAndAfterStart(t *testing.T) {
	f := newFixture(t, 1)

	f.clock.set(baseRelease.Add(-time.Minute))
	assertOutcome(t, f.reserve(t, "user-1", "k1"), domain.OutcomeBusinessRefusal, domain.ReasonSlotNotReleased)

	f.clock.set(baseStart)
	assertOutcome(t, f.reserve(t, "user-2", "k2"), domain.OutcomeBusinessRefusal, domain.ReasonSlotClosed)
}

func TestReserveTTLOutsideWindow(t *testing.T) {
	f := newFixture(t, 1)
	// 10s before start with a 30s TTL: the computed hold would end after start.
	f.clock.set(baseStart.Add(-10 * time.Second))
	assertOutcome(t, f.reserve(t, "user-1", "k1"), domain.OutcomeBusinessRefusal, domain.ReasonOutsideWindow)
}

func TestReserveUnknownSlot(t *testing.T) {
	f := newFixture(t, 1)
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: "ghost", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)

	// The refusal is recorded: replaying the same key returns it with Replay=true.
	replay, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: "ghost", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("unexpected error on replay: %v", err)
	}
	assertOutcome(t, replay, domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
	if !replay.Replay {
		t.Error("unknown-target retry must be a replay")
	}
}

func TestReserveMissingFieldsInvalidRequest(t *testing.T) {
	f := newFixture(t, 1)
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: slot}) // no key
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Outcome != domain.OutcomeInvalidRequest {
		t.Fatalf("Outcome = %q, want invalid_request", r.Outcome)
	}
}

// --- idempotency ------------------------------------------------------------

func TestReserveReplaySameKey(t *testing.T) {
	f := newFixture(t, 5)
	first := f.reserve(t, "user-1", "dup")
	assertOutcome(t, first, domain.OutcomeAdmittedSuccess, "")

	second := f.reserve(t, "user-1", "dup")
	assertOutcome(t, second, domain.OutcomeAdmittedSuccess, "")
	if !second.Replay {
		t.Error("second call with same key must be a replay")
	}
	if second.ReservationID != first.ReservationID {
		t.Errorf("replay reservation ID = %s, want %s", second.ReservationID, first.ReservationID)
	}
	// Only one hold exists despite two calls.
	if held, _ := f.store.SlotCounts(slot); held != 1 {
		t.Errorf("held = %d, want 1 (replay must not mutate)", held)
	}
}

func TestReserveKeyReuseDifferentTargetConflicts(t *testing.T) {
	f := newFixture(t, 5)
	f.store.SeedSlot(domain.Slot{ID: "slot-2", OrganisationID: org, Capacity: 5, ReleaseAt: baseRelease, StartsAt: baseStart})

	assertOutcome(t, f.reserve(t, "user-1", "shared"), domain.OutcomeAdmittedSuccess, "")

	// Same scope (org/user/op/key) but a different slot => different request hash.
	r, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: "slot-2", IdempotencyKey: "shared"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeBusinessRefusal, domain.ReasonIdempotencyConflict)
}

func TestReserveRefusalIsReplayable(t *testing.T) {
	f := newFixture(t, 1)
	f.reserve(t, "user-1", "k1") // consumes capacity
	sold := f.reserve(t, "user-2", "k2")
	assertOutcome(t, sold, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)

	replay := f.reserve(t, "user-2", "k2")
	assertOutcome(t, replay, domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)
	if !replay.Replay {
		t.Error("refusal retry with same key must replay the recorded refusal")
	}
}

// --- confirm ----------------------------------------------------------------

func TestConfirmSuccessKeepsCapacityConsumed(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	c := f.confirm(t, "user-1", "c1", res.ReservationID)
	assertOutcome(t, c, domain.OutcomeAdmittedSuccess, "")
	if c.BookingID == "" {
		t.Fatal("confirm success must carry a booking ID")
	}
	// held -> confirmed keeps one unit consumed: a new reserve is refused.
	assertOutcome(t, f.reserve(t, "user-2", "r2"), domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity)
	if held, active := f.store.SlotCounts(slot); held != 0 || active != 1 {
		t.Errorf("counts after confirm = held %d active %d, want held 0 active 1", held, active)
	}
}

func TestConfirmExpiredHold(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	// Advance past the hold's expiry but before slot start: settlement expires it.
	f.clock.set(baseNow.Add(testTTL + time.Second))
	assertOutcome(t, f.confirm(t, "user-1", "c1", res.ReservationID), domain.OutcomeBusinessRefusal, domain.ReasonReservationExpired)
}

func TestConfirmUnknownReservation(t *testing.T) {
	f := newFixture(t, 1)
	assertOutcome(t, f.confirm(t, "user-1", "c1", "ghost"), domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
}

func TestConfirmAlreadyCancelledIsInvalidState(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	f.cancel(t, "user-1", "x1", res.ReservationID)
	assertOutcome(t, f.confirm(t, "user-1", "c1", res.ReservationID), domain.OutcomeBusinessRefusal, domain.ReasonInvalidState)
}

// --- cancel -----------------------------------------------------------------

func TestCancelHeldReleasesCapacity(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	assertOutcome(t, f.cancel(t, "user-1", "x1", res.ReservationID), domain.OutcomeAdmittedSuccess, "")
	// Capacity returned: a fresh reserve now succeeds.
	assertOutcome(t, f.reserve(t, "user-2", "r2"), domain.OutcomeAdmittedSuccess, "")
}

func TestCancelConfirmedReleasesCapacity(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	f.confirm(t, "user-1", "c1", res.ReservationID)
	assertOutcome(t, f.cancel(t, "user-1", "x1", res.ReservationID), domain.OutcomeAdmittedSuccess, "")
	if held, active := f.store.SlotCounts(slot); held != 0 || active != 0 {
		t.Errorf("counts after cancel = held %d active %d, want 0/0", held, active)
	}
	assertOutcome(t, f.reserve(t, "user-2", "r2"), domain.OutcomeAdmittedSuccess, "")
}

func TestCancelAfterStartIsSlotClosed(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	f.confirm(t, "user-1", "c1", res.ReservationID)
	f.clock.set(baseStart.Add(time.Minute))
	// The booking is still active, but the slot has started: no capacity-returning cancel.
	assertOutcome(t, f.cancel(t, "user-1", "x1", res.ReservationID), domain.OutcomeBusinessRefusal, domain.ReasonSlotClosed)
}

func TestCancelTwiceIsInvalidState(t *testing.T) {
	f := newFixture(t, 1)
	res := f.reserve(t, "user-1", "r1")
	f.cancel(t, "user-1", "x1", res.ReservationID)
	assertOutcome(t, f.cancel(t, "user-1", "x2", res.ReservationID), domain.OutcomeBusinessRefusal, domain.ReasonInvalidState)
}

func TestCancelUnknownReservation(t *testing.T) {
	f := newFixture(t, 1)
	assertOutcome(t, f.cancel(t, "user-1", "x1", "ghost"), domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget)
}

// --- concurrency ------------------------------------------------------------

// TestReserveConcurrentCapacityNeverExceeded runs many distinct reserves against a
// scarce slot. Exactly capacity succeed; the rest are refused; persisted state
// reconciles to capacity. Run under -race.
func TestReserveConcurrentCapacityNeverExceeded(t *testing.T) {
	const capacity, n = 10, 60
	f := newFixture(t, capacity)

	var success, soldOut int64
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := f.svc.Reserve(context.Background(), ReserveCommand{
				OrganisationID: org, UserID: domain.UserID(fmt.Sprintf("user-%d", i)),
				SlotID: slot, IdempotencyKey: fmt.Sprintf("key-%d", i),
			})
			if err != nil {
				t.Errorf("goroutine %d: unexpected error %v", i, err)
				return
			}
			switch {
			case r.Outcome == domain.OutcomeAdmittedSuccess:
				atomic.AddInt64(&success, 1)
			case r.Outcome == domain.OutcomeBusinessRefusal && r.Reason == domain.ReasonNoCapacity:
				atomic.AddInt64(&soldOut, 1)
			default:
				t.Errorf("goroutine %d: unexpected result %+v", i, r)
			}
		}(i)
	}
	wg.Wait()

	if success != capacity {
		t.Errorf("successes = %d, want %d", success, capacity)
	}
	if soldOut != n-capacity {
		t.Errorf("sold-out = %d, want %d", soldOut, n-capacity)
	}
	if held, active := f.store.SlotCounts(slot); held+active != capacity {
		t.Errorf("reconciled consumed = %d, want %d", held+active, capacity)
	}
}

// TestReserveConcurrentSameKeyOneMutation fires the identical request from many
// goroutines. Exactly one performs the mutation; the rest replay its outcome.
func TestReserveConcurrentSameKeyOneMutation(t *testing.T) {
	const n = 25
	f := newFixture(t, 5)

	results := make([]domain.Result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := f.svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: slot, IdempotencyKey: "same"})
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			results[i] = r
		}(i)
	}
	wg.Wait()

	fresh := 0
	var id domain.ReservationID
	for _, r := range results {
		if r.Outcome != domain.OutcomeAdmittedSuccess {
			t.Fatalf("unexpected outcome %+v", r)
		}
		if !r.Replay {
			fresh++
		}
		switch {
		case id == "":
			id = r.ReservationID
		case r.ReservationID != id:
			t.Errorf("reservation IDs diverged: %s vs %s", r.ReservationID, id)
		}
	}
	if fresh != 1 {
		t.Errorf("fresh (non-replay) mutations = %d, want 1", fresh)
	}
	if held, _ := f.store.SlotCounts(slot); held != 1 {
		t.Errorf("held = %d, want 1", held)
	}
}

// --- idempotency-insert race (unique-constraint backstop) -------------------

// conflictOnceRepo wraps a store and makes the first InsertRecord lose the
// unique-constraint race — committing a "winner" record first, then returning
// ErrConflict — so the service's rollback-and-retry path (transaction-semantics
// §5.3) is exercised without needing overlapping transactions.
type conflictOnceRepo struct {
	inner   *inmem.Store
	mu      sync.Mutex
	tripped bool
}

func (r *conflictOnceRepo) WithinTx(ctx context.Context, fn func(ctx context.Context, tx domain.Tx) error) error {
	return r.inner.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		return fn(ctx, &conflictOnceTx{Tx: tx, repo: r})
	})
}

type conflictOnceTx struct {
	domain.Tx
	repo *conflictOnceRepo
}

func (t *conflictOnceTx) InsertRecord(ctx context.Context, rec domain.IdempotencyRecord) error {
	t.repo.mu.Lock()
	first := !t.repo.tripped
	t.repo.tripped = true
	t.repo.mu.Unlock()
	if first {
		// A concurrent winner commits the same scoped key first, then our insert loses.
		if err := t.Tx.InsertRecord(ctx, rec); err != nil {
			return err
		}
		return domain.ErrConflict
	}
	return t.Tx.InsertRecord(ctx, rec)
}

func TestReserveIdempotencyInsertRaceReplaysWinner(t *testing.T) {
	store := inmem.New()
	store.SeedSlot(domain.Slot{ID: slot, OrganisationID: org, Capacity: 5, ReleaseAt: baseRelease, StartsAt: baseStart})
	svc := New(&conflictOnceRepo{inner: store}, &manualClock{t: baseNow}, &seqIDGen{}, testTTL)

	r, err := svc.Reserve(context.Background(), ReserveCommand{OrganisationID: org, UserID: "user-1", SlotID: slot, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOutcome(t, r, domain.OutcomeAdmittedSuccess, "")
	if !r.Replay {
		t.Error("losing the insert race must resolve to a replay of the winning record")
	}
	if r.ReservationID != "res-1" {
		t.Errorf("reservation ID = %s, want res-1 (the winner)", r.ReservationID)
	}
}

// --- property-style invariant (measurement-contract §9 item 1) --------------

// TestCapacityInvariantUnderRandomOperations applies a randomised stream of
// reserve/confirm/cancel operations and asserts the capacity invariant holds after
// every step: consumed (held + active bookings) is always within [0, capacity]. The
// clock stays inside the open window so no settlement occurs and reconciliation is
// exact.
func TestCapacityInvariantUnderRandomOperations(t *testing.T) {
	const capacity = 3
	f := newFixture(t, capacity)
	rng := rand.New(rand.NewSource(1))
	ctx := context.Background()

	// Track reservations we have created so confirm/cancel can target real ones.
	var reservations []domain.ReservationID
	key := 0
	nextKey := func() string { key++; return fmt.Sprintf("k%d", key) }

	for step := range 400 {
		switch rng.Intn(3) {
		case 0: // reserve
			r, err := f.svc.Reserve(ctx, ReserveCommand{OrganisationID: org, UserID: domain.UserID(fmt.Sprintf("u%d", step)), SlotID: slot, IdempotencyKey: nextKey()})
			if err != nil {
				t.Fatalf("step %d reserve: %v", step, err)
			}
			if r.Outcome == domain.OutcomeAdmittedSuccess {
				reservations = append(reservations, r.ReservationID)
			}
		case 1: // confirm a known reservation
			if len(reservations) == 0 {
				continue
			}
			res := reservations[rng.Intn(len(reservations))]
			if _, err := f.svc.Confirm(ctx, ConfirmCommand{OrganisationID: org, UserID: "u", ReservationID: res, IdempotencyKey: nextKey()}); err != nil {
				t.Fatalf("step %d confirm: %v", step, err)
			}
		case 2: // cancel a known reservation
			if len(reservations) == 0 {
				continue
			}
			res := reservations[rng.Intn(len(reservations))]
			if _, err := f.svc.Cancel(ctx, CancelCommand{OrganisationID: org, UserID: "u", ReservationID: res, IdempotencyKey: nextKey()}); err != nil {
				t.Fatalf("step %d cancel: %v", step, err)
			}
		}

		held, active := f.store.SlotCounts(slot)
		consumed := held + active
		if consumed < 0 || consumed > capacity {
			t.Fatalf("step %d: capacity invariant violated: consumed=%d capacity=%d", step, consumed, capacity)
		}
	}
}

// TestCommitInvariantGuard exercises the commit-time assertion that a mutation
// exists exactly when the outcome is admitted_success. No legitimate operation can
// violate it — result and persist are always set as a pair — so it is driven
// directly to prove the guard turns a would-be silent bad record into an aborting
// fault (the §4 fault line), and that the record is never inserted when it fires.
func TestCommitInvariantGuard(t *testing.T) {
	scope := domain.ScopeKey{OrganisationID: org, UserID: "u", Operation: domain.OpReserve, Key: "k"}
	noopPersist := func(context.Context, domain.Tx) error { return nil }

	cases := []struct {
		name    string
		outcome domain.Outcome
		persist func(context.Context, domain.Tx) error
		wantErr bool
	}{
		{"success without persist", domain.OutcomeAdmittedSuccess, nil, true},
		{"refusal with persist", domain.OutcomeBusinessRefusal, noopPersist, true},
		{"success with persist", domain.OutcomeAdmittedSuccess, noopPersist, false},
		{"refusal without persist", domain.OutcomeBusinessRefusal, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 1)
			result := domain.Result{Outcome: tc.outcome}
			err := f.store.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
				_, e := f.svc.commit(ctx, tx, scope, "hash", result, baseNow, tc.persist)
				return e
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("commit: expected invariant violation, got nil")
				}
				// The guard fires before InsertRecord, so no record is written.
				_ = f.store.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
					if _, e := tx.FindRecord(ctx, scope); !errors.Is(e, domain.ErrNotFound) {
						t.Errorf("commit: record present after invariant violation (err=%v), want none", e)
					}
					return nil
				})
				return
			}
			if err != nil {
				t.Fatalf("commit: unexpected error %v", err)
			}
		})
	}
}
