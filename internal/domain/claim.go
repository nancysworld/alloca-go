package domain

import "time"

// ScheduleClaim is one identity's active claim on an interval of its own time
// (transaction-semantics §2.2). It is the unit the user schedule non-overlap invariant
// is expressed over:
//
//	for one identity, no two active claims may overlap in time
//
// One claim exists per logical booking, from hold through confirmation: confirming
// updates the claim rather than creating a second one, so a booking can never conflict
// with the hold it was created from.
//
// The claim is deliberately *not* derived from reservation and booking rows. Confirming
// leaves the reservation at confirmed and inserts an active booking, so one logical
// claim is two lifecycle rows; a union over them would report an identity as conflicting
// with itself the moment it confirmed.
type ScheduleClaim struct {
	// ReservationID identifies the claim: one reservation, one claim, for its whole
	// lifetime.
	ReservationID ReservationID
	// OrganisationID is the *identity's* organisation, never the slot's (§1.1). The
	// claim is keyed by identity, so an identity booking into another organisation is
	// still protected against overlapping itself.
	OrganisationID OrganisationID
	UserID         UserID
	// SlotRef is carried for settlement and telemetry; it is not part of the conflict
	// key, which is exactly why claims on *different* slots still conflict.
	SlotRef SlotRef
	// StartsAt/EndsAt are the claimed interval, taken from the slot, and are half-open:
	// [StartsAt, EndsAt). Adjacent bookings therefore do not overlap.
	StartsAt time.Time
	EndsAt   time.Time
	// ExpiresAt is the backing hold's expiry. A claim whose hold has elapsed is no
	// longer active and is settled away before any conflict decision, so an abandoned
	// hold cannot block the identity's schedule (§2.2). Confirming clears it: a
	// confirmed claim is permanent and only cancellation removes it.
	ExpiresAt time.Time
}

// Overlaps reports whether two intervals overlap under half-open semantics. It is the
// domain statement of the rule the PostgreSQL exclusion constraint enforces, so the
// reference store and the authoritative adapter agree on the boundary:
//
//	10:00–11:00 and 11:00–12:00 are adjacent, not overlapping.
func (c ScheduleClaim) Overlaps(other ScheduleClaim) bool {
	return c.StartsAt.Before(other.EndsAt) && other.StartsAt.Before(c.EndsAt)
}

// SameIdentity reports whether two claims belong to the same identity — the pair
// (organisation_id, user_id), which is globally unique without user_id having to be
// (§1.1). Claims of different identities never conflict, however much they overlap.
func (c ScheduleClaim) SameIdentity(other ScheduleClaim) bool {
	return c.OrganisationID == other.OrganisationID && c.UserID == other.UserID
}

// Elapsed reports whether the claim's backing hold has lapsed at now, using the same
// boundary as Reservation.Elapsed (now >= expires_at). A confirmed claim has no expiry
// and is never elapsed.
func (c ScheduleClaim) Elapsed(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}
