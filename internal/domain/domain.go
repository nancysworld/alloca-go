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
// v2 (2026-07-28): a slot is identified by the pair (slot_organisation_id, slot_id) rather
// than by slot_id alone, so reserve carries the slot's owning organisation and the
// request hash covers both halves. A v1 hash for the same logical request will not
// match a v2 one — which is the point: under v1 the target was ambiguous.
const ContractVersion = "v2"

// Identity dimensions and entity identifiers. They are distinct string types so the
// compiler rejects passing, say, a UserID where a SlotID is expected.
type (
	// OrganisationID is the coarse authority / routing dimension (AG-M5). It appears in
	// two different roles that must not be conflated (transaction-semantics §1.1): a
	// UserRef's is where a user's identity is issued; a SlotRef's is who owns the slot.
	// Neither is derivable from the other, so every stored column names its role —
	// user_organisation_id or slot_organisation_id — and this type is never used bare
	// where both are in scope.
	OrganisationID string
	// UserID identifies whoever or whatever the booked time belongs to, within its
	// organisation. It need not be a person: a pet whose grooming slot is reserved, or
	// a child booked in by a parent, is the user, because it is their time the slot
	// consumes. The account that arranges or pays for a booking is a separate concern
	// AG-M1 does not model. It scopes idempotency and is a fairness dimension later.
	UserID string

	// SlotID identifies a slot *within its owning organisation*. It is not globally
	// unique on its own: a slot's identity is the SlotRef pair
	// (OrganisationID, SlotID). Reservation and booking identifiers, by contrast, are
	// server-assigned and global.
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

// UserRef identifies a user: the pair (user_organisation_id, user_id), scoped to the
// organisation the user's identity is issued under (transaction-semantics §1.1).
//
// It is the twin of SlotRef, and the pairing is the same idea both times — an identity
// is a pair scoped to an organisation — but the organisations are different dimensions.
// A user's is where their identity is issued; a slot's is who owns the slot. They differ
// whenever a user books into another organisation, which is exactly the case the user
// schedule invariant exists to protect, so the two are never interchangeable. Every
// column that stores one says which it is: user_organisation_id or slot_organisation_id.
//
// The pair is globally unique without user_id having to be, which is why the schedule
// invariant needs no separate global identifier.
type UserRef struct {
	OrganisationID OrganisationID
	UserID         UserID
}
