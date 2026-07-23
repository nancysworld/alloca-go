package domain

import (
	"testing"
	"time"
)

func TestSlotLifecycleBoundaries(t *testing.T) {
	release := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	start := release.Add(time.Hour)
	s := Slot{ReleaseAt: release, StartsAt: start}

	cases := []struct {
		name                         string
		now                          time.Time
		released, closed, reservable bool
	}{
		{"just before release", release.Add(-time.Nanosecond), false, false, false},
		{"exactly at release", release, true, false, true},
		{"mid window", release.Add(30 * time.Minute), true, false, true},
		{"just before start", start.Add(-time.Nanosecond), true, false, true},
		{"exactly at start (closed, inclusive)", start, true, true, false},
		{"after start", start.Add(time.Nanosecond), true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.Released(c.now); got != c.released {
				t.Errorf("Released = %v, want %v", got, c.released)
			}
			if got := s.Closed(c.now); got != c.closed {
				t.Errorf("Closed = %v, want %v", got, c.closed)
			}
			if got := s.Reservable(c.now); got != c.reservable {
				t.Errorf("Reservable = %v, want %v", got, c.reservable)
			}
		})
	}
}
