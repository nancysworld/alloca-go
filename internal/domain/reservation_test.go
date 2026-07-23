package domain

import (
	"testing"
	"time"
)

func TestReservationElapsedBoundary(t *testing.T) {
	expires := time.Date(2026, 7, 23, 10, 0, 30, 0, time.UTC)
	r := Reservation{ExpiresAt: expires}

	cases := []struct {
		name    string
		now     time.Time
		elapsed bool
	}{
		{"before expiry", expires.Add(-time.Nanosecond), false},
		{"exactly at expiry (elapsed, inclusive)", expires, true},
		{"after expiry", expires.Add(time.Nanosecond), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.Elapsed(c.now); got != c.elapsed {
				t.Errorf("Elapsed = %v, want %v", got, c.elapsed)
			}
		})
	}
}

func TestReservationStateTerminal(t *testing.T) {
	if ReservationHeld.Terminal() {
		t.Error("held must not be terminal")
	}
	for _, s := range []ReservationState{ReservationConfirmed, ReservationCancelled, ReservationExpired} {
		if !s.Terminal() {
			t.Errorf("%s must be terminal", s)
		}
	}
}
