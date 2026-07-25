-- +goose Up
-- +goose StatementBegin

-- Schema for the AG-M1 transactional core (docs/design/transaction-semantics.md §1).
--
-- All timestamps are timestamptz. Correctness-sensitive values are written from the
-- attempt's authoritative timestamp (clock_timestamp() resolved after the slot lock),
-- never from an API host clock, so decisions do not depend on API-node clock skew.

-- Slots: the scarce resource and the write authority for its own capacity. The slot
-- row is the aggregate lock: every capacity-changing operation takes SELECT … FOR
-- UPDATE on it, so operations on one slot serialize while operations on different
-- slots do not contend (§2).
CREATE TABLE slots (
    slot_id         text        PRIMARY KEY,
    organisation_id text        NOT NULL,
    -- Grouping/telemetry only: never part of the authority or the lock (§1.2).
    resource_id     text        NOT NULL,
    capacity        integer     NOT NULL CHECK (capacity > 0),
    release_at      timestamptz NOT NULL,
    starts_at       timestamptz NOT NULL,
    ends_at         timestamptz NOT NULL,
    CONSTRAINT slots_window_ordered CHECK (release_at < starts_at AND starts_at < ends_at)
);

-- Reservations: one-unit, expiring holds. State machine in §3.1; held is the only
-- non-terminal state.
CREATE TABLE reservations (
    reservation_id  text        PRIMARY KEY,
    slot_id         text        NOT NULL REFERENCES slots (slot_id),
    organisation_id text        NOT NULL,
    user_id         text        NOT NULL,
    state           text        NOT NULL CHECK (state IN ('held', 'confirmed', 'cancelled', 'expired')),
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    -- §1.3: a hold is never created already-expired and never outlives the slot start.
    -- The starts_at half of that invariant is enforced in the service against the
    -- locked slot row; here we pin the part that is local to the row.
    CONSTRAINT reservations_hold_window CHECK (created_at < expires_at)
);

-- Consumed capacity is derived from rows, never a denormalised counter (§1.7), so
-- these two indexes are on the hot path: settlement and the consumed-capacity
-- derivation both scan a slot's held rows under the lock. Partial indexes keep them
-- proportional to live rows rather than to all history.
CREATE INDEX reservations_held_by_slot_idx
    ON reservations (slot_id)
    WHERE state = 'held';

-- Settlement selects held rows whose TTL has lapsed. Ordering by expires_at lets the
-- bounded, indexed settlement of AG-M2 replace the current full scan without a
-- schema change.
CREATE INDEX reservations_expiry_idx
    ON reservations (expires_at)
    WHERE state = 'held';

-- Bookings: the durable confirmed commitment, created only by confirming a held
-- reservation (§1.4, §3.2). One booking per reservation — the unique constraint makes
-- "confirm produced two bookings" unrepresentable rather than merely untested.
CREATE TABLE bookings (
    booking_id      text        PRIMARY KEY,
    reservation_id  text        NOT NULL UNIQUE REFERENCES reservations (reservation_id),
    slot_id         text        NOT NULL REFERENCES slots (slot_id),
    organisation_id text        NOT NULL,
    user_id         text        NOT NULL,
    state           text        NOT NULL CHECK (state IN ('active', 'cancelled')),
    created_at      timestamptz NOT NULL
);

CREATE INDEX bookings_active_by_slot_idx
    ON bookings (slot_id)
    WHERE state = 'active';

-- Idempotency records: one durable record per logical mutation request (§5.1). The
-- primary key IS the scope — (organisation_id, user_id, operation, key) — so the
-- unique constraint that backstops the key-reuse race (§5.3) is the table's own
-- identity rather than a separate index that could be dropped by accident.
--
-- A client key is deliberately NOT global: scoping it prevents one caller replaying
-- another's result. target_id is not stored; it lives only inside request_hash.
CREATE TABLE idempotency_records (
    organisation_id text        NOT NULL,
    user_id         text        NOT NULL,
    operation       text        NOT NULL CHECK (operation IN ('reserve', 'confirm', 'cancel')),
    key             text        NOT NULL,
    request_hash    text        NOT NULL,
    outcome         text        NOT NULL,
    reason          text        NOT NULL,
    -- result_ref (§5.1): enough to reconstruct the original response on replay.
    -- Nullable because a refusal creates no entity. Deliberately NOT foreign keys:
    -- the record is inserted before the mutation it describes (§5.2 write ordering,
    -- so a scoped-key race is detected before any entity is written), and an FK would
    -- reject a row referencing an entity that is written later in the same
    -- transaction. The pairing is enforced by the service's commit invariant instead.
    reservation_id  text        NULL,
    booking_id      text        NULL,
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (organisation_id, user_id, operation, key)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE idempotency_records;
DROP TABLE bookings;
DROP TABLE reservations;
DROP TABLE slots;
-- +goose StatementEnd
