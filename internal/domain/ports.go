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
	// ErrScheduleConflict reports that inserting a schedule claim overlapped an
	// existing active claim for the same identity — the user schedule non-overlap
	// invariant (transaction-semantics §2.2). Unlike ErrConflict it is not a retry
	// signal: it is the authoritative answer that this identity's time is already
	// claimed, which the service turns into a business_refusal with
	// ReasonScheduleConflict. It must leave the transaction usable, so an adapter that
	// discovers it through a constraint violation has to roll back to a savepoint
	// rather than abort the whole transaction.
	ErrScheduleConflict = errors.New("domain: schedule claim overlaps an existing claim")
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
	LockSlot(ctx context.Context, ref SlotRef) (Slot, error)
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
	// SlotRefForReservation resolves the slot a reservation belongs to, without
	// locking, so the caller can then LockSlot that slot (transaction-semantics §2).
	// Returns ErrNotFound if the reservation does not exist. It establishes no
	// timestamp: only the subsequent LockSlot does.
	//
	// It returns the whole SlotRef rather than a bare identifier because the caller
	// cannot reconstruct the owning organisation: for a booking made into another
	// organisation it is neither the caller's nor derivable from the reservation's own
	// identity columns.
	SlotRefForReservation(ctx context.Context, id ReservationID) (SlotRef, error)
	// Reservation loads a reservation by ID. Returns ErrNotFound if absent.
	Reservation(ctx context.Context, id ReservationID) (Reservation, error)
	// HeldReservations returns every reservation on the slot currently in the held
	// state, for settlement and consumed-capacity derivation.
	HeldReservations(ctx context.Context, ref SlotRef) ([]Reservation, error)
	// ActiveBookingCount returns the number of active bookings on the slot.
	ActiveBookingCount(ctx context.Context, ref SlotRef) (int, error)
	// BookingForReservation loads the booking created from a reservation. Returns
	// ErrNotFound if none exists.
	BookingForReservation(ctx context.Context, id ReservationID) (Booking, error)
	// PutReservation inserts or updates a reservation.
	PutReservation(ctx context.Context, r Reservation) error
	// PutBooking inserts or updates a booking.
	PutBooking(ctx context.Context, b Booking) error
	// InsertClaim inserts an active schedule claim, returning ErrScheduleConflict if it
	// overlaps an existing active claim for the same identity (transaction-semantics
	// §2.2). The insert *is* the conflict check: a prior read cannot be authoritative,
	// because a concurrent transaction may commit an overlapping claim between the read
	// and the write. Implementations must leave the transaction usable after a
	// conflict, since the caller still has to record the refusal.
	//
	// It is called during precondition evaluation rather than in the mutation step, so
	// the outcome is known before the idempotency record is written.
	InsertClaim(ctx context.Context, c ScheduleClaim) error
	// ConfirmClaim makes a claim permanent by clearing its expiry: the hold became a
	// booking, so settlement must no longer remove it. It updates the existing row
	// rather than inserting a second one, so confirming cannot self-conflict.
	ConfirmClaim(ctx context.Context, id ReservationID) error
	// DeleteClaim removes a reservation's claim when it stops being active —
	// cancellation or expiry. Deleting an absent claim is not an error: expiry settles
	// reservations whose claims a previous identity-scoped settlement may already have
	// removed.
	DeleteClaim(ctx context.Context, id ReservationID) error
	// SettleClaims removes the identity's elapsed claims — those whose backing hold has
	// lapsed at now. It is the identity-scoped analogue of slot-scoped expiry
	// settlement, and carries the same guarantee: an abandoned hold stops blocking the
	// identity's schedule whether or not the expiry worker has run
	// (transaction-semantics §2.1, §2.2). Confirmed claims have no expiry and are never
	// settled.
	SettleClaims(ctx context.Context, org OrganisationID, user UserID, now time.Time) error
	// FindRecord loads an idempotency record by scoped key. Returns ErrNotFound if
	// absent.
	FindRecord(ctx context.Context, key ScopeKey) (IdempotencyRecord, error)
	// InsertRecord inserts an idempotency record. Returns ErrConflict if a record with
	// the same scoped key already exists (the unique-constraint backstop).
	InsertRecord(ctx context.Context, rec IdempotencyRecord) error
}
