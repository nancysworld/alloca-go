package domain

import "time"

// BookingState is the booking state machine (transaction-semantics §3.2). A booking
// is created active by confirm and may be cancelled before the slot starts.
type BookingState string

const (
	BookingActive    BookingState = "active"
	BookingCancelled BookingState = "cancelled"
)

// Booking is the durable confirmed commitment created when a reservation is
// confirmed (transaction-semantics §1.4). An active booking consumes one unit of
// slot capacity until cancelled. It is created only by confirm, atomically with the
// reservation's held → confirmed transition, under the slot lock.
type Booking struct {
	ID            BookingID
	ReservationID ReservationID
	// SlotRef is the slot's identity, owning organisation included (§1.2).
	SlotRef SlotRef
	// OrganisationID and UserID are the caller's identity, not the slot's owner (§1.1).
	OrganisationID OrganisationID
	UserID         UserID
	State          BookingState
	CreatedAt      time.Time
}
