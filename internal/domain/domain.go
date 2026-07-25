// Package domain is the transactional core of Alloca-Go's booking model: the
// entities, the invariants over them, the outcome vocabulary, and the port
// interfaces the core needs from the outside world. It is the normative
// realisation of docs/design/transaction-semantics.md.
//
// Dependency rule (docs/design/project-structure.md §4): this package imports only
// the standard library. It owns its interfaces (Repository, Tx, Clock, IDGen);
// adapters such as the in-memory reference store and the future PostgreSQL adapter
// implement them. No transport (HTTP) or persistence (SQL, pgx) type ever appears
// here.
//
// The three entities are the Slot (the scarce resource and write authority), the
// Reservation (a one-unit hold), and the Booking (the durable confirmed
// commitment), plus a durable IdempotencyRecord. AG-M1 models single-unit holds
// only; reservation quantities and conserved balances are AG-M6.
package domain

// ContractVersion identifies the request/response contract the idempotency request
// hash is computed against (transaction-semantics §5.1). Bumping it means a stored
// request hash from an older contract will not match a new one, which is the
// intended behaviour when the semantically significant request shape changes.
const ContractVersion = "v1"

// Identity dimensions and entity identifiers. They are distinct string types so the
// compiler rejects passing, say, a UserID where a SlotID is expected.
type (
	// OrganisationID is the coarse authority / routing dimension (AG-M5) and part of
	// the idempotency scope. Each slot belongs to one organisation.
	OrganisationID string
	// UserID is the booking participant; it scopes idempotency and is a fairness
	// dimension for later milestones.
	UserID string

	// SlotID identifies a slot (the aggregate root).
	SlotID string
	// ReservationID identifies a reservation.
	ReservationID string
	// BookingID identifies a booking.
	BookingID string
)

// Operation names a mutating booking operation. It is part of the idempotency scope
// and the request hash, so a key issued for one operation cannot collide with the
// same key issued for another.
type Operation string

const (
	OpReserve Operation = "reserve"
	OpConfirm Operation = "confirm"
	OpCancel  Operation = "cancel"
)
