-- +goose Up
-- +goose StatementBegin

-- A slot's identity is the pair (organisation_id, slot_id), not slot_id alone
-- (docs/design/transaction-semantics.md §1.2).
--
-- 00001 keyed slots by slot_id, which assumed slot identifiers are unique across every
-- organisation. Nothing establishes that: identifiers are unique *within* the
-- organisation that owns the slot. This is the aggregate lock's resolution path, so the
-- assumption is a correctness one — once two organisations mint the same identifier,
-- locking by slot_id alone either serializes unrelated slots against each other or
-- resolves to the wrong organisation's slot entirely.
--
-- Corrected before AG-M2 rather than at AG-M5 because the lock's resolution path is
-- precisely what AG-M2 measures the frontier of: changing it later would invalidate
-- those measurements.

-- The referencing constraints must go before the key they point at.
ALTER TABLE reservations DROP CONSTRAINT reservations_slot_id_fkey;
ALTER TABLE bookings     DROP CONSTRAINT bookings_slot_id_fkey;

ALTER TABLE slots DROP CONSTRAINT slots_pkey;
ALTER TABLE slots ADD CONSTRAINT slots_pkey PRIMARY KEY (organisation_id, slot_id);

-- Reservations and bookings need the *slot's* organisation as a distinct column: their
-- existing organisation_id is the caller identity's (§1.1), and the two differ whenever
-- an identity books into another organisation. Reusing organisation_id as the foreign
-- key would silently forbid exactly that case.
ALTER TABLE reservations ADD COLUMN slot_organisation_id text;
ALTER TABLE bookings     ADD COLUMN slot_organisation_id text;

-- Backfill: sound precisely because slot_id was the primary key until a moment ago, so
-- it is still unique across existing rows. This is the last point at which that lookup
-- is unambiguous, which is why it happens here rather than in a later migration.
UPDATE reservations r SET slot_organisation_id = s.organisation_id
    FROM slots s WHERE s.slot_id = r.slot_id;
UPDATE bookings b SET slot_organisation_id = s.organisation_id
    FROM slots s WHERE s.slot_id = b.slot_id;

ALTER TABLE reservations ALTER COLUMN slot_organisation_id SET NOT NULL;
ALTER TABLE bookings     ALTER COLUMN slot_organisation_id SET NOT NULL;

ALTER TABLE reservations ADD CONSTRAINT reservations_slot_fkey
    FOREIGN KEY (slot_organisation_id, slot_id) REFERENCES slots (organisation_id, slot_id);
ALTER TABLE bookings ADD CONSTRAINT bookings_slot_fkey
    FOREIGN KEY (slot_organisation_id, slot_id) REFERENCES slots (organisation_id, slot_id);

-- The per-slot hot-path indexes must key on the whole slot identity too, or a lookup
-- for one organisation's slot would scan another's rows.
DROP INDEX reservations_held_by_slot_idx;
CREATE INDEX reservations_held_by_slot_idx
    ON reservations (slot_organisation_id, slot_id)
    WHERE state = 'held';

DROP INDEX bookings_active_by_slot_idx;
CREATE INDEX bookings_active_by_slot_idx
    ON bookings (slot_organisation_id, slot_id)
    WHERE state = 'active';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bookings_active_by_slot_idx;
CREATE INDEX bookings_active_by_slot_idx ON bookings (slot_id) WHERE state = 'active';

DROP INDEX reservations_held_by_slot_idx;
CREATE INDEX reservations_held_by_slot_idx ON reservations (slot_id) WHERE state = 'held';

ALTER TABLE bookings     DROP CONSTRAINT bookings_slot_fkey;
ALTER TABLE reservations DROP CONSTRAINT reservations_slot_fkey;

ALTER TABLE bookings     DROP COLUMN slot_organisation_id;
ALTER TABLE reservations DROP COLUMN slot_organisation_id;

-- Restoring the single-column key can fail if two organisations already share a slot
-- identifier — which is the whole reason for this migration. A rollback that silently
-- dropped one of those rows would be worse than one that refuses.
ALTER TABLE slots DROP CONSTRAINT slots_pkey;
ALTER TABLE slots ADD CONSTRAINT slots_pkey PRIMARY KEY (slot_id);

ALTER TABLE reservations ADD CONSTRAINT reservations_slot_id_fkey
    FOREIGN KEY (slot_id) REFERENCES slots (slot_id);
ALTER TABLE bookings ADD CONSTRAINT bookings_slot_id_fkey
    FOREIGN KEY (slot_id) REFERENCES slots (slot_id);

-- +goose StatementEnd
