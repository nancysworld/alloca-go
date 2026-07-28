package domain

import "time"

// SlotRef identifies a slot. A slot's identity is the pair
// (slot_organisation_id, slot_id), not slot_id alone (transaction-semantics §1.2):
// identifiers are unique *within* the organisation that owns the slot, and nothing
// requires them to be unique across organisations.
//
// This is the aggregate lock's resolution path, so it is a correctness concern rather
// than a modelling preference. Locking by slot_id alone would, once two organisations
// happened to mint the same identifier, either serialize unrelated slots against each
// other or — far worse — resolve to the wrong organisation's slot.
//
// The slot's organisation is the one that *owns the slot*. It is not the caller's
// identity organisation (§1.1), and the two differ whenever an identity books into
// another organisation.
type SlotRef struct {
	OrganisationID OrganisationID
	SlotID         SlotID
}

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

// Ref returns the slot's identity, so callers pass the whole key around rather than
// re-pairing the two halves at every call site and risking one that pairs them wrongly.
func (s Slot) Ref() SlotRef {
	return SlotRef{OrganisationID: s.OrganisationID, SlotID: s.ID}
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
