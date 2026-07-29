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
//
// SlotRef is the slot the booking consumes; UserRef is whose time it is. As on a
// Reservation, their organisations are different dimensions and differ whenever a user
// books into another organisation (§1.1, §1.2).
type Booking struct {
	ID            BookingID
	ReservationID ReservationID
	SlotRef       SlotRef
	UserRef       UserRef
	State         BookingState
	CreatedAt     time.Time
}
