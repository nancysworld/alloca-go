package domain

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by Tx. Callers match them with errors.Is so an adapter
// may wrap them with context.
var (
	// ErrNotFound reports that a requested slot, reservation, booking, or idempotency
	// record does not exist.
	ErrNotFound = errors.New("domain: not found")
	// ErrConflict reports that inserting an idempotency record violated the scoped-key
	// unique constraint — another transaction recorded the same scope first. It is the
	// concurrency backstop of transaction-semantics §5.3; the caller rolls back and
	// re-runs so the winning record is observed.
	ErrConflict = errors.New("domain: idempotency scope conflict")
)

// Clock is the source of authoritative service time (transaction-semantics §1.5).
// Booking decisions resolve now once at the trusted boundary through this port;
// tests inject a controllable clock for deterministic coverage.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock backed by time.Now.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// IDGen mints server-assigned identifiers. Identity is server-owned; clients never
// supply reservation or booking IDs.
type IDGen interface {
	NewReservationID() ReservationID
	NewBookingID() BookingID
}

// Repository is the domain's persistence port. All state-changing work runs inside
// WithinTx so a mutation and its idempotency record commit or abort together.
type Repository interface {
	// WithinTx runs fn inside a single serialized transaction, committing if fn
	// returns nil and rolling back otherwise. Implementations guarantee that
	// operations on the same slot are serialized (PostgreSQL: SELECT … FOR UPDATE;
	// the in-memory reference: a process-wide lock). The Tx handed to fn is the only
	// means of reading or mutating state for the duration of the transaction.
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}

// Tx is the transactional view of the store used inside Repository.WithinTx. Its
// read/lock methods take the slot's write lock where required so the capacity
// invariant is evaluated under single-writer serialization (transaction-semantics
// §2). Read methods return ErrNotFound when the entity is absent.
type Tx interface {
	// LockSlot loads a slot and takes its write lock for the remainder of the
	// transaction. Returns ErrNotFound if the slot does not exist.
	LockSlot(ctx context.Context, id SlotID) (Slot, error)
	// SlotIDForReservation resolves the slot a reservation belongs to, without
	// locking, so the caller can then LockSlot that slot (transaction-semantics §2).
	// Returns ErrNotFound if the reservation does not exist.
	SlotIDForReservation(ctx context.Context, id ReservationID) (SlotID, error)
	// Reservation loads a reservation by ID. Returns ErrNotFound if absent.
	Reservation(ctx context.Context, id ReservationID) (Reservation, error)
	// HeldReservations returns every reservation on the slot currently in the held
	// state, for settlement and consumed-capacity derivation.
	HeldReservations(ctx context.Context, slotID SlotID) ([]Reservation, error)
	// ActiveBookingCount returns the number of active bookings on the slot.
	ActiveBookingCount(ctx context.Context, slotID SlotID) (int, error)
	// BookingForReservation loads the booking created from a reservation. Returns
	// ErrNotFound if none exists.
	BookingForReservation(ctx context.Context, id ReservationID) (Booking, error)
	// PutReservation inserts or updates a reservation.
	PutReservation(ctx context.Context, r Reservation) error
	// PutBooking inserts or updates a booking.
	PutBooking(ctx context.Context, b Booking) error
	// FindRecord loads an idempotency record by scoped key. Returns ErrNotFound if
	// absent.
	FindRecord(ctx context.Context, key ScopeKey) (IdempotencyRecord, error)
	// InsertRecord inserts an idempotency record. Returns ErrConflict if a record with
	// the same scoped key already exists (the unique-constraint backstop).
	InsertRecord(ctx context.Context, rec IdempotencyRecord) error
}
