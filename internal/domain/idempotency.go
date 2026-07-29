package domain

import "time"

// ScopeKey is the idempotency scope and the database unique constraint
// (transaction-semantics §5.1). A client key is NOT global: scoping it by
// organisation, user, and operation prevents one caller replaying another's result
// or a key colliding across operations. It is deliberately all-comparable so it can
// be a map key in the in-memory reference store and a composite unique key in SQL.
//
// The scope does NOT include the target — target_id lives only in RequestHash. Two
// requests can therefore share a ScopeKey with different targets; that key-reuse
// case is resolved by the unique constraint plus request-hash comparison, not by the
// slot lock (transaction-semantics §5.3).
type ScopeKey struct {
	UserRef   UserRef
	Operation Operation
	Key       string
}

// IdempotencyRecord is the durable record of one logical mutation request and its
// recorded terminal outcome (transaction-semantics §5.1). It is written in the same
// transaction as the mutation (or refusal) it describes, so one scoped key records
// exactly one terminal outcome.
type IdempotencyRecord struct {
	UserRef   UserRef
	Operation Operation
	Key       string
	// RequestHash is computed by internal/idempotency over the semantically
	// significant request fields. It detects a key reused for a different request and
	// excludes server-generated values (now, expires_at) so ordinary retries hash
	// identically.
	RequestHash string
	// Outcome and Reason are the recorded terminal outcome replayed on a repeat.
	Outcome Outcome
	Reason  Reason
	// ReservationID / BookingID are the result_ref: enough to reconstruct the original
	// response on replay.
	ReservationID ReservationID
	BookingID     BookingID
	CreatedAt     time.Time
}

// Scope returns the record's scope key.
func (r IdempotencyRecord) Scope() ScopeKey {
	return ScopeKey{UserRef: r.UserRef, Operation: r.Operation, Key: r.Key}
}
