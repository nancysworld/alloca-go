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
	// ErrTimeNotEstablished reports that Tx.Now was called before the attempt's
	// authoritative timestamp existed — that is, before a successful LockSlot or
	// ResolveTimeWithoutSlot. It is a programming error on the fault line
	// (transaction-semantics §4), never a domain answer: returning a pre-lock time
	// here is exactly the staleness the authoritative-time contract exists to prevent.
	ErrTimeNotEstablished = errors.New("domain: authoritative time not established")
)

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
	//
	// No now is passed in: doing so would force the value to be resolved before the
	// closure could acquire the slot lock, structurally encoding the wrong decision
	// point. Authoritative time is established inside the transaction, by Tx.
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}

// Tx is the transactional view of the store used inside Repository.WithinTx. Its
// read/lock methods take the slot's write lock where required so the capacity
// invariant is evaluated under single-writer serialization (transaction-semantics
// §2). Read methods return ErrNotFound when the entity is absent.
//
// # Authoritative time (transaction-semantics §1.5)
//
// Each transaction attempt has exactly one decision timestamp, and it describes the
// point at which the mutation actually serializes — not when the request arrived or
// when the transaction began. A transaction may wait on the slot lock until
// lock_timeout; during that wait a hold can expire or the slot can cross starts_at,
// so a timestamp resolved before the wait is stale precisely when it matters.
//
// The contract is therefore: a successful LockSlot establishes and memoises the
// attempt's timestamp, resolved after the row-lock wait completes. Now returns that
// memoised value and never resolves a time source of its own. Calling Now first is
// ErrTimeNotEstablished, not an invitation to obtain pre-lock time by accident.
// Because it is per attempt rather than per request, a re-run after the §5.3
// insert-race backstop resolves a new timestamp; only the committed attempt's value
// becomes durable.
type Tx interface {
	// LockSlot loads a slot and takes its write lock for the remainder of the
	// transaction, then establishes the attempt's authoritative timestamp (readable
	// via Now) from the post-lock instant. Returns ErrNotFound if the slot does not
	// exist, in which case no timestamp is established — the caller is on the
	// unknown-target path and must use ResolveTimeWithoutSlot.
	LockSlot(ctx context.Context, id SlotID) (Slot, error)
	// Now returns the attempt's authoritative timestamp. It returns
	// ErrTimeNotEstablished if neither LockSlot nor ResolveTimeWithoutSlot has
	// succeeded in this attempt.
	Now(ctx context.Context) (time.Time, error)
	// ResolveTimeWithoutSlot establishes the attempt's authoritative timestamp on the
	// one path that legitimately has no slot to lock: recording the unknown_target
	// refusal for a target that does not exist (transaction-semantics §5.5). It is a
	// separate method rather than a fallback inside Now so that the no-slot path is
	// explicit at the call site and cannot be reached by forgetting to lock.
	ResolveTimeWithoutSlot(ctx context.Context) (time.Time, error)
	// SlotIDForReservation resolves the slot a reservation belongs to, without
	// locking, so the caller can then LockSlot that slot (transaction-semantics §2).
	// Returns ErrNotFound if the reservation does not exist. It establishes no
	// timestamp: only the subsequent LockSlot does.
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
