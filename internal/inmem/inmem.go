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
package inmem

import (
	"context"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Store is the in-memory repository. The zero value is not usable; construct with
// New. It is safe for concurrent use: all access is serialized by sem, a one-slot
// semaphore standing in for the store lock. It is a channel rather than a
// sync.Mutex so that a waiter can honour its context deadline while blocked.
type Store struct {
	sem          chan struct{}
	slots        map[domain.SlotID]domain.Slot
	reservations map[domain.ReservationID]domain.Reservation
	bookings     map[domain.BookingID]domain.Booking
	bookingByRes map[domain.ReservationID]domain.BookingID
	records      map[domain.ScopeKey]domain.IdempotencyRecord
}

// New constructs an empty Store.
func New() *Store {
	return &Store{
		sem:          make(chan struct{}, 1),
		slots:        make(map[domain.SlotID]domain.Slot),
		reservations: make(map[domain.ReservationID]domain.Reservation),
		bookings:     make(map[domain.BookingID]domain.Booking),
		bookingByRes: make(map[domain.ReservationID]domain.BookingID),
		records:      make(map[domain.ScopeKey]domain.IdempotencyRecord),
	}
}

// SeedSlot inserts or replaces a slot. Slot creation is a control-plane concern, not
// part of the AG-M1 mutation ports, so it is a concrete helper rather than a Tx
// method. Tests use it to set up the world before exercising the service.
func (s *Store) SeedSlot(slot domain.Slot) {
	s.lock()
	defer s.unlock()
	s.slots[slot.ID] = slot
}

// SlotCounts reports the slot's held reservations and active bookings by direct
// inspection of stored state. It is a reconciliation helper for tests: it verifies
// invariants against persisted state rather than trusting operation results
// (measurement-contract §9, scaled to the reference store). It does not settle
// elapsed holds, so callers that need settled counts must reconcile at a time before
// any hold elapses.
func (s *Store) SlotCounts(slotID domain.SlotID) (held, activeBookings int) {
	s.lock()
	defer s.unlock()
	for _, r := range s.reservations {
		if r.SlotID == slotID && r.State == domain.ReservationHeld {
			held++
		}
	}
	for _, b := range s.bookings {
		if b.SlotID == slotID && b.State == domain.BookingActive {
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
// call, while the store lock is held.
type tx struct {
	store *Store
}

func (t *tx) LockSlot(_ context.Context, id domain.SlotID) (domain.Slot, error) {
	slot, ok := t.store.slots[id]
	if !ok {
		return domain.Slot{}, domain.ErrNotFound
	}
	return slot, nil
}

func (t *tx) SlotIDForReservation(_ context.Context, id domain.ReservationID) (domain.SlotID, error) {
	r, ok := t.store.reservations[id]
	if !ok {
		return "", domain.ErrNotFound
	}
	return r.SlotID, nil
}

func (t *tx) Reservation(_ context.Context, id domain.ReservationID) (domain.Reservation, error) {
	r, ok := t.store.reservations[id]
	if !ok {
		return domain.Reservation{}, domain.ErrNotFound
	}
	return r, nil
}

func (t *tx) HeldReservations(_ context.Context, slotID domain.SlotID) ([]domain.Reservation, error) {
	var held []domain.Reservation
	for _, r := range t.store.reservations {
		if r.SlotID == slotID && r.State == domain.ReservationHeld {
			held = append(held, r)
		}
	}
	return held, nil
}

func (t *tx) ActiveBookingCount(_ context.Context, slotID domain.SlotID) (int, error) {
	count := 0
	for _, b := range t.store.bookings {
		if b.SlotID == slotID && b.State == domain.BookingActive {
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
