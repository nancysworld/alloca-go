package postgres

import (
	"context"
	"fmt"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// This file holds the control-plane and reconciliation operations that are not part
// of the AG-M1 mutation ports. Slot creation is a control-plane concern, and counting
// rows directly is a verification concern: neither belongs on domain.Tx, which exists
// to express the booking mutations.

// SeedSlot inserts or replaces a slot. AG-M1 has no slot-management API — slots are
// created by the control plane or by a test setting up the world — so this is a
// concrete method on Repo rather than a Tx method, exactly as in the in-memory double.
func (r *Repo) SeedSlot(ctx context.Context, slot domain.Slot) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO slots
			(slot_id, organisation_id, resource_id, capacity, release_at, starts_at, ends_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (slot_id) DO UPDATE SET
			organisation_id = EXCLUDED.organisation_id,
			resource_id     = EXCLUDED.resource_id,
			capacity        = EXCLUDED.capacity,
			release_at      = EXCLUDED.release_at,
			starts_at       = EXCLUDED.starts_at,
			ends_at         = EXCLUDED.ends_at`,
		string(slot.ID), string(slot.OrganisationID), slot.ResourceID, slot.Capacity,
		slot.ReleaseAt, slot.StartsAt, slot.EndsAt)
	if err != nil {
		return fmt.Errorf("postgres: seed slot: %w", err)
	}
	return nil
}

// SlotCounts reports a slot's held reservations and active bookings by reading rows
// directly, bypassing the service entirely.
//
// It is the reconciliation primitive for tests: an invariant checked against the
// operation results the service returned would only prove the service is
// self-consistent. Checking against persisted rows is what proves capacity was never
// actually exceeded (measurement-contract §9).
//
// It performs no settlement, so a caller reconciling a slot with live holds must do
// so before those holds elapse.
func (r *Repo) SlotCounts(ctx context.Context, slotID domain.SlotID) (held, activeBookings int, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM reservations WHERE slot_id = $1 AND state = 'held'),
			(SELECT count(*) FROM bookings     WHERE slot_id = $1 AND state = 'active')`,
		string(slotID)).Scan(&held, &activeBookings)
	if err != nil {
		return 0, 0, fmt.Errorf("postgres: slot counts: %w", err)
	}
	return held, activeBookings, nil
}

// ReservationStates returns a census of reservation states for a slot, so a test can
// assert on the shape of the whole state machine rather than one row at a time.
func (r *Repo) ReservationStates(ctx context.Context, slotID domain.SlotID) (map[domain.ReservationState]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT state, count(*) FROM reservations WHERE slot_id = $1 GROUP BY state`,
		string(slotID))
	if err != nil {
		return nil, fmt.Errorf("postgres: reservation states: %w", err)
	}
	defer rows.Close()

	states := make(map[domain.ReservationState]int)
	for rows.Next() {
		var state domain.ReservationState
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("postgres: reservation states: %w", err)
		}
		states[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reservation states: %w", err)
	}
	return states, nil
}

// Truncate empties every domain table. It exists so an integration test starts from a
// known world; it is never called by the service.
func (r *Repo) Truncate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		`TRUNCATE idempotency_records, bookings, reservations, slots RESTART IDENTITY CASCADE`)
	if err != nil {
		return fmt.Errorf("postgres: truncate: %w", err)
	}
	return nil
}
