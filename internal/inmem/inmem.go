// Package inmem is the in-memory reference implementation of domain.Repository and
// domain.Tx. It is a permanent test double — not a production adapter — used to prove
// every AG-M1 correctness gate that is expressible above the SQL layer
// (docs/planning/ag-m1-implementation-plan.md, PR2). The authoritative PostgreSQL
// adapter (PR3) implements the same domain interfaces.
//
// Concurrency model: a single process-wide lock is held for the whole of each
// WithinTx, so transactions serialize completely. This is the "one coarse lock as a
// correctness reference" — deliberately stronger than PostgreSQL's per-slot
// FOR UPDATE serialization, so the domain/service logic can be proven correct when
// serialized. Real per-slot concurrency, row locks, and the unique-constraint race
// are proven against PostgreSQL in PR3.
//
// The lock is context-aware: a caller whose context is already done, or which is
// still waiting for the lock when its deadline expires, gets ctx.Err() and never
// reaches fn. That mirrors the real adapter, where a lock wait that exceeds
// lock_timeout fails instead of mutating (transaction-semantics §6), and lets tests
// above this double exercise the cancellation and lock-wait paths rather than
// silently committing a mutation the caller has already abandoned.
//
// Because transactions never overlap here, WithinTx does not implement partial-
// failure rollback: the reference double is not a fault injector. The service orders
// its writes so the idempotency record is inserted before any entity mutation is
// persisted, so the only rollback-relevant path (a unique-constraint conflict)
// leaves the store consistent; real transactional rollback is a PR3 property.
//
// Authoritative time: this adapter honours the same contract as the PostgreSQL one
// (domain.Tx) — a successful LockSlot establishes and memoises the attempt's
// timestamp, and Now returns only that memoised value. Where PostgreSQL reads
// clock_timestamp() after the row-lock wait, the double reads its injected Clock at
// the same point in the sequence. That is what makes deterministic time available to
// tests without a second semantic time source living in the service
// (docs/design-notes/authoritative-time-in-a-scaled-service.md §4).
package inmem

import (
	"context"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Clock is the double's time source. It exists here, rather than as a domain port,
// because transaction-owned time retired the service-level clock from the production
// mutation path: the authoritative adapter reads PostgreSQL, so an injectable clock
// is now purely a property of this test double.
type Clock interface {
	Now() time.Time
}

// SystemClock is a Clock backed by time.Now, for callers that want the double to
// track real time.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// Store is the in-memory repository. The zero value is not usable; construct with
// New. It is safe for concurrent use: all access is serialized by sem, a one-slot
// semaphore standing in for the store lock. It is a channel rather than a
// sync.Mutex so that a waiter can honour its context deadline while blocked.
type Store struct {
	sem          chan struct{}
	clock        Clock
	slots        map[domain.SlotRef]domain.Slot
	reservations map[domain.ReservationID]domain.Reservation
	bookings     map[domain.BookingID]domain.Booking
	bookingByRes map[domain.ReservationID]domain.BookingID
	records      map[domain.ScopeKey]domain.IdempotencyRecord
	claims       map[domain.ReservationID]domain.ScheduleClaim
}

// New constructs an empty Store whose transactions take their authoritative time
// from clock. A nil clock panics rather than defaulting to real time: silently
// substituting a wall clock would make a test's timeline non-deterministic in
// exactly the boundary cases this double exists to pin down.
func New(clock Clock) *Store {
	if clock == nil {
		panic("inmem: clock must not be nil")
	}
	return &Store{
		sem:          make(chan struct{}, 1),
		clock:        clock,
		slots:        make(map[domain.SlotRef]domain.Slot),
		reservations: make(map[domain.ReservationID]domain.Reservation),
		bookings:     make(map[domain.BookingID]domain.Booking),
		bookingByRes: make(map[domain.ReservationID]domain.BookingID),
		records:      make(map[domain.ScopeKey]domain.IdempotencyRecord),
		claims:       make(map[domain.ReservationID]domain.ScheduleClaim),
	}
}

// Claims returns every stored schedule claim, for tests that assert the invariant
// against persisted state rather than against operation results. Like SlotCounts it
// does not settle: a caller wanting live claims only must reconcile at a time before
// any hold elapses, or check Elapsed itself.
func (s *Store) Claims() []domain.ScheduleClaim {
	s.lock()
	defer s.unlock()
	out := make([]domain.ScheduleClaim, 0, len(s.claims))
	for _, c := range s.claims {
		out = append(out, c)
	}
	return out
}

// SeedSlot inserts or replaces a slot. Slot creation is a control-plane concern, not
// part of the AG-M1 mutation ports, so it is a concrete helper rather than a Tx
// method. Tests use it to set up the world before exercising the service.
func (s *Store) SeedSlot(slot domain.Slot) {
	s.lock()
	defer s.unlock()
	s.slots[slot.Ref()] = slot
}

// SlotCounts reports the slot's held reservations and active bookings by direct
// inspection of stored state. It is a reconciliation helper for tests: it verifies
// invariants against persisted state rather than trusting operation results
// (measurement-contract §9, scaled to the reference store). It does not settle
// elapsed holds, so callers that need settled counts must reconcile at a time before
// any hold elapses.
func (s *Store) SlotCounts(ref domain.SlotRef) (held, activeBookings int) {
	s.lock()
	defer s.unlock()
	for _, r := range s.reservations {
		if r.SlotRef == ref && r.State == domain.ReservationHeld {
			held++
		}
	}
	for _, b := range s.bookings {
		if b.SlotRef == ref && b.State == domain.BookingActive {
			activeBookings++
		}
	}
	return held, activeBookings
}

// lock and unlock take and release the store lock without a context, for the
// control-plane helpers that are not part of a transaction.
func (s *Store) lock()   { s.sem <- struct{}{} }
func (s *Store) unlock() { <-s.sem }

// lockCtx takes the store lock, honouring ctx while waiting. It returns ctx.Err()
// if the context is already done or becomes done before the lock is granted, so an
// abandoned request never acquires the authority.
func (s *Store) lockCtx(ctx context.Context) error {
	// Checked first so an already-done context is deterministic: select would
	// otherwise choose freely between a free lock and a closed Done channel.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WithinTx runs fn under the process-wide lock. The *tx handed to fn operates
// directly on the store's maps for the duration of the call. A context that is done
// on entry, or that expires while waiting for the lock, returns ctx.Err() without
// running fn: a caller that can no longer receive the outcome must not mutate state
// (the deadline-expiry error the transport edge classifies as timeout_*).
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context, tx domain.Tx) error) error {
	if err := s.lockCtx(ctx); err != nil {
		return err
	}
	defer s.unlock()
	// The deadline can pass during the wait above; re-check so the transaction
	// either starts within budget or does not start at all.
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx, &tx{store: s})
}

// tx is the transactional view. It is valid only for the duration of one WithinTx
// call, while the store lock is held. now/established hold the attempt's
// authoritative timestamp; they are per-tx, so a re-run after the idempotency
// insert-race backstop gets a fresh value, matching the per-attempt rule.
type tx struct {
	store       *Store
	now         time.Time
	established bool
}

// establish records the attempt's authoritative timestamp. It reads the clock only
// once: a second call keeps the first value, so every decision in the attempt is
// made against one instant even if a caller locks more than once.
func (t *tx) establish() time.Time {
	if !t.established {
		t.now = t.store.clock.Now()
		t.established = true
	}
	return t.now
}

func (t *tx) LockSlot(_ context.Context, ref domain.SlotRef) (domain.Slot, error) {
	slot, ok := t.store.slots[ref]
	if !ok {
		// No timestamp is established on the not-found path: the caller is heading for
		// the unknown-target refusal and must say so explicitly via
		// ResolveTimeWithoutSlot.
		return domain.Slot{}, domain.ErrNotFound
	}
	t.establish()
	return slot, nil
}

// LockUserIdentity serializes one identity's claim-creating transactions in the real
// adapter. Here every transaction already runs under the process-wide lock, so there is
// no per-identity lock to take and no wait to resolve time after: the memoised instant
// is returned, exactly as InsertClaim returns it. The established guard is kept, though
// — it is what makes a violation of the normative slot → identity lock order
// (transaction-semantics §2.2) loud in the reference double rather than only in
// production.
func (t *tx) LockUserIdentity(_ context.Context, _ domain.UserRef) (time.Time, error) {
	if !t.established {
		return time.Time{}, domain.ErrTimeNotEstablished
	}
	return t.now, nil
}

func (t *tx) Now(_ context.Context) (time.Time, error) {
	if !t.established {
		return time.Time{}, domain.ErrTimeNotEstablished
	}
	return t.now, nil
}

func (t *tx) ResolveTimeWithoutSlot(_ context.Context) (time.Time, error) {
	return t.establish(), nil
}

func (t *tx) SlotRefForReservation(_ context.Context, id domain.ReservationID) (domain.SlotRef, error) {
	r, ok := t.store.reservations[id]
	if !ok {
		return domain.SlotRef{}, domain.ErrNotFound
	}
	return r.SlotRef, nil
}

func (t *tx) Reservation(_ context.Context, id domain.ReservationID) (domain.Reservation, error) {
	r, ok := t.store.reservations[id]
	if !ok {
		return domain.Reservation{}, domain.ErrNotFound
	}
	return r, nil
}

func (t *tx) HeldReservations(_ context.Context, ref domain.SlotRef) ([]domain.Reservation, error) {
	var held []domain.Reservation
	for _, r := range t.store.reservations {
		if r.SlotRef == ref && r.State == domain.ReservationHeld {
			held = append(held, r)
		}
	}
	return held, nil
}

func (t *tx) ActiveBookingCount(_ context.Context, ref domain.SlotRef) (int, error) {
	count := 0
	for _, b := range t.store.bookings {
		if b.SlotRef == ref && b.State == domain.BookingActive {
			count++
		}
	}
	return count, nil
}

func (t *tx) BookingForReservation(_ context.Context, id domain.ReservationID) (domain.Booking, error) {
	bkID, ok := t.store.bookingByRes[id]
	if !ok {
		return domain.Booking{}, domain.ErrNotFound
	}
	return t.store.bookings[bkID], nil
}

func (t *tx) PutReservation(_ context.Context, r domain.Reservation) error {
	t.store.reservations[r.ID] = r
	return nil
}

func (t *tx) PutBooking(_ context.Context, b domain.Booking) error {
	t.store.bookings[b.ID] = b
	t.store.bookingByRes[b.ReservationID] = b.ID
	return nil
}

// InsertClaim inserts a claim unless it overlaps an existing claim of the same user.
//
// The scan is the reference statement of the rule the PostgreSQL exclusion constraint
// enforces: same user, overlapping half-open intervals. Note what it deliberately
// does *not* consider — whether the existing claim has elapsed. The constraint cannot
// know: its index predicate would have to depend on the wall clock, which PostgreSQL
// does not allow. Any stored row conflicts, elapsed or not.
//
// So an elapsed claim blocks here exactly as it would in PostgreSQL, and it is
// settlement's job — not the insert's — to remove it first. Skipping elapsed rows here
// would make this double more permissive than the authority it stands in for, and the
// difference would only surface in production.
func (t *tx) InsertClaim(_ context.Context, c domain.ScheduleClaim) (time.Time, error) {
	for _, existing := range t.store.claims {
		if existing.ReservationID == c.ReservationID {
			continue
		}
		if existing.SameUser(c) && existing.Overlaps(c) {
			return time.Time{}, domain.ErrScheduleConflict
		}
	}
	t.store.claims[c.ReservationID] = c
	// The double serializes every transaction under one lock, so a claim insert never
	// waits and the attempt's instant cannot have gone stale. Returning the memoised
	// value keeps the port's shape honest without inventing a second clock read: where
	// PostgreSQL reports the post-wait instant, here there was no wait to be after.
	return t.now, nil
}

func (t *tx) SetClaimExpiry(_ context.Context, id domain.ReservationID, expiresAt time.Time) error {
	c, ok := t.store.claims[id]
	if !ok {
		return domain.ErrNotFound
	}
	c.ExpiresAt = expiresAt
	t.store.claims[id] = c
	return nil
}

func (t *tx) ConfirmClaim(_ context.Context, id domain.ReservationID) error {
	c, ok := t.store.claims[id]
	if !ok {
		return domain.ErrNotFound
	}
	// Clearing the expiry is the whole transition: the row, and therefore the interval
	// it protects, is otherwise untouched.
	c.ExpiresAt = time.Time{}
	t.store.claims[id] = c
	return nil
}

func (t *tx) DeleteClaim(_ context.Context, id domain.ReservationID) error {
	delete(t.store.claims, id)
	return nil
}

func (t *tx) SettleClaims(_ context.Context, user domain.UserRef, now time.Time) error {
	for id, c := range t.store.claims {
		if c.UserRef == user && c.Elapsed(now) {
			delete(t.store.claims, id)
		}
	}
	return nil
}

func (t *tx) FindRecord(_ context.Context, key domain.ScopeKey) (domain.IdempotencyRecord, error) {
	rec, ok := t.store.records[key]
	if !ok {
		return domain.IdempotencyRecord{}, domain.ErrNotFound
	}
	return rec, nil
}

func (t *tx) InsertRecord(_ context.Context, rec domain.IdempotencyRecord) error {
	key := rec.Scope()
	if _, exists := t.store.records[key]; exists {
		return domain.ErrConflict
	}
	t.store.records[key] = rec
	return nil
}

// Static assertions that Store and tx satisfy the domain ports.
var (
	_ domain.Repository = (*Store)(nil)
	_ domain.Tx         = (*tx)(nil)
)
