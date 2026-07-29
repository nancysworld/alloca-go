package domain

import "time"

// ScheduleClaim is one user's active claim on an interval of their own time
// (transaction-semantics §2.2). It is the unit the user schedule non-overlap invariant
// is expressed over:
//
//	for one user, no two active claims may overlap in time
//
// One claim exists per logical booking, from hold through confirmation: confirming
// updates the claim rather than creating a second one, so a booking can never conflict
// with the hold it was created from. ReservationID is therefore the claim's identity for
// its whole lifetime.
//
// The claim is deliberately *not* derived from reservation and booking rows. Confirming
// leaves the reservation at confirmed and inserts an active booking, so one logical
// claim is two lifecycle rows; a union over them would report a user as conflicting with
// themselves the moment they confirmed.
//
// UserRef is the conflict key — whose schedule the claim occupies — and its organisation
// is the user's, never the slot's (§1.1), so a user booking into another organisation is
// still protected against overlapping themselves. SlotRef is carried for settlement and
// telemetry only; keeping it out of the key is exactly why claims on *different* slots
// still conflict.
//
// StartsAt/EndsAt are the claimed interval, taken from the slot, and are half-open:
// [StartsAt, EndsAt), so adjacent bookings do not overlap. ExpiresAt is the backing
// hold's expiry: a claim whose hold has elapsed is no longer active and is settled away
// before any conflict decision, so an abandoned hold cannot block the schedule.
// Confirming clears it — a confirmed claim is permanent, and only cancellation removes
// it.
type ScheduleClaim struct {
	ReservationID ReservationID
	UserRef       UserRef
	SlotRef       SlotRef
	StartsAt      time.Time
	EndsAt        time.Time
	ExpiresAt     time.Time
}

// Overlaps reports whether two intervals overlap under half-open semantics. It is the
// domain statement of the rule the PostgreSQL exclusion constraint enforces, so the
// reference store and the authoritative adapter agree on the boundary:
//
//	10:00–11:00 and 11:00–12:00 are adjacent, not overlapping.
func (c ScheduleClaim) Overlaps(other ScheduleClaim) bool {
	return c.StartsAt.Before(other.EndsAt) && other.StartsAt.Before(c.EndsAt)
}

// SameUser reports whether two claims belong to the same user — the pair
// (user_organisation_id, user_id), which is globally unique without user_id having to be
// (§1.1). Claims of different users never conflict, however much they overlap.
func (c ScheduleClaim) SameUser(other ScheduleClaim) bool {
	return c.UserRef == other.UserRef
}

// Elapsed reports whether the claim's backing hold has lapsed at now, using the same
// boundary as Reservation.Elapsed (now >= expires_at). A confirmed claim has no expiry
// and is never elapsed.
func (c ScheduleClaim) Elapsed(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}
