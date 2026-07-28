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

// A slot's identity is a pair too, and each half is checked independently: a ref missing
// its organisation would resolve no slot, and two such refs would compare equal.
func TestSlotRefIsValid(t *testing.T) {
	cases := []struct {
		name string
		ref  SlotRef
		want bool
	}{
		{"both halves present", SlotRef{OrganisationID: "org-1", SlotID: "slot-1"}, true},
		{"missing organisation", SlotRef{SlotID: "slot-1"}, false},
		{"missing slot", SlotRef{OrganisationID: "org-1"}, false},
		{"zero value", SlotRef{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.ref.IsValid(); got != c.want {
				t.Errorf("IsValid() = %v, want %v", got, c.want)
			}
		})
	}
}

// Slot.Ref must produce a valid ref from a slot that has both halves — the pairing helper
// and the validity rule have to agree, or one would accept what the other rejects.
func TestSlotRefRoundTrip(t *testing.T) {
	s := Slot{ID: "slot-1", OrganisationID: "org-1"}
	ref := s.Ref()
	if !ref.IsValid() {
		t.Error("Ref() of a fully-identified slot is not valid")
	}
	if ref.SlotID != s.ID || ref.OrganisationID != s.OrganisationID {
		t.Errorf("Ref() = %+v, want the slot's own halves", ref)
	}
}
