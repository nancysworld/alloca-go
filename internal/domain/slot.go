package domain

import "time"

// Slot is the scarce bookable unit and the write authority for its own capacity
// (the aggregate root; transaction-semantics §1.2). It is a time window: its
// lifecycle is derived from authoritative service time, not a mutable status flag.
type Slot struct {
	ID             SlotID
	OrganisationID OrganisationID
	// ResourceID groups slots of the same underlying resource (e.g. a recurring
	// class). It is an attribute for grouping/telemetry only — never part of the
	// authority or the lock.
	ResourceID string
	// Capacity is a fixed positive integer for AG-M1.
	Capacity int
	// ReleaseAt is the instant the slot becomes reservable.
	ReleaseAt time.Time
	// StartsAt is the instant the slot closes to new reservations and to
	// capacity-returning cancellation.
	StartsAt time.Time
	// EndsAt is the end of the window; not used in AG-M1 decisions but modelled for
	// completeness and telemetry.
	EndsAt time.Time
}

// Released reports whether the slot has reached its release time: release_at <= now.
func (s Slot) Released(now time.Time) bool {
	return !now.Before(s.ReleaseAt)
}

// Closed reports whether the slot has started: now >= starts_at. A closed slot
// accepts no new reservations and no capacity-returning cancellation. The boundary
// is inclusive of starts_at, matching the expiry boundary (a hold's expires_at is
// always <= starts_at), so at or after the start every held unit is elapsed.
func (s Slot) Closed(now time.Time) bool {
	return !now.Before(s.StartsAt)
}

// Reservable reports whether a new reservation may be created now:
// release_at <= now < starts_at (transaction-semantics §1.2).
func (s Slot) Reservable(now time.Time) bool {
	return s.Released(now) && !s.Closed(now)
}
