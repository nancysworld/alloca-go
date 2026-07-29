package domain

import "time"

// ReservationState is the reservation state machine (transaction-semantics §3.1).
// held is the only non-terminal state; confirmed, cancelled, and expired are
// terminal.
type ReservationState string

const (
	ReservationHeld      ReservationState = "held"
	ReservationConfirmed ReservationState = "confirmed"
	ReservationCancelled ReservationState = "cancelled"
	ReservationExpired   ReservationState = "expired"
)

// Terminal reports whether the state is terminal (anything but held).
func (s ReservationState) Terminal() bool {
	return s != ReservationHeld
}

// Reservation is a temporary, expiring hold on exactly one unit of a slot's
// capacity (transaction-semantics §1.3). A held reservation is valid only when
// created_at < expires_at <= slot.starts_at.
//
// SlotRef is the slot the hold consumes; UserRef is whose time is held. Their
// organisations are different dimensions — who owns the slot (§1.2) versus where the
// user's identity is issued (§1.1) — and they differ whenever a user books into another
// organisation. That is why each is a pair of its own rather than one shared column.
type Reservation struct {
	ID        ReservationID
	SlotRef   SlotRef
	UserRef   UserRef
	State     ReservationState
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Elapsed reports whether the hold's TTL has lapsed: now >= expires_at. This is the
// single expiry boundary used everywhere settlement is evaluated
// (transaction-semantics §2.1). An elapsed held reservation is settled to expired
// under the slot lock before any precondition is evaluated.
func (r Reservation) Elapsed(now time.Time) bool {
	return !now.Before(r.ExpiresAt)
}
