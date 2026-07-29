-- +goose Up
-- +goose StatementBegin

-- Schema for the AG-M1 transactional core (docs/design/transaction-semantics.md §1–§2).
--
-- This is the baseline, and migration compatibility begins here. It expresses the model
-- AG-M1 settled on rather than the sequence of corrections that arrived at it: AG-M1 has
-- no deployed database and no persisted data, so there was nothing for a transitional
-- migration to migrate. Every schema change after this baseline merges is forward-only
-- and numbered.
--
-- All timestamps are timestamptz. Correctness-sensitive values are written from the
-- attempt's authoritative timestamp (clock_timestamp() resolved after the slot lock),
-- never from an API host clock, so decisions do not depend on API-node clock skew (§1.5).
--
-- Two identities run through this schema and they are different dimensions (§1.1, §1.2):
--
--     (user_organisation_id, user_id)   the user  — where the user's identity is issued
--     (slot_organisation_id, slot_id)   the slot  — who owns the slot
--
-- Neither is derivable from the other, and they differ whenever a user books into another
-- organisation. Every column therefore names which one it carries; there is no bare
-- organisation_id anywhere, because conflating the two is exactly the mistake that
-- silently stops protecting a user who books across organisations.

-- Required for the scalar equality operators in the exclusion constraint on
-- user_time_claims below: GiST has no native `=` operator class for text. btree_gist is a
-- *trusted* extension (verified on PostgreSQL 16), so installing it needs the CREATE
-- privilege on the database rather than superuser — but it must be available on the
-- server, which on RDS means rds.allowed_extensions.
--
-- IF NOT EXISTS, because another schema or application in the same database may already
-- have installed it. That is also why Down does not drop it: this migration cannot tell
-- whether it created the extension, and removing a possibly-shared dependency is worse
-- than leaving an unused one behind.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Slots: the scarce resource and the write authority for its own capacity. The slot row
-- is the aggregate lock: every capacity-changing operation takes SELECT … FOR UPDATE on
-- it, so operations on one slot serialize while operations on different slots do not
-- contend (§2).
--
-- The primary key is the pair. Slot identifiers are unique only *within* the organisation
-- that owns the slot, so keying by slot_id alone would — once two organisations minted
-- the same identifier — either serialize two unrelated slots against each other or take
-- the lock on the wrong organisation's slot and mutate its capacity (§1.2).
CREATE TABLE slots (
    slot_organisation_id text        NOT NULL,
    slot_id              text        NOT NULL,
    -- Grouping/telemetry only: never part of the authority or the lock (§1.2).
    resource_id          text        NOT NULL,
    capacity             integer     NOT NULL CHECK (capacity > 0),
    release_at           timestamptz NOT NULL,
    starts_at            timestamptz NOT NULL,
    ends_at              timestamptz NOT NULL,
    PRIMARY KEY (slot_organisation_id, slot_id),
    CONSTRAINT slots_window_ordered CHECK (release_at < starts_at AND starts_at < ends_at)
);

-- Reservations: one-unit, expiring holds. State machine in §3.1; held is the only
-- non-terminal state.
--
-- Carries both identities: the slot it holds, and the user whose time is held. A user of
-- one organisation holding a slot owned by another is legitimate, which is why these are
-- separate columns rather than one shared organisation.
CREATE TABLE reservations (
    reservation_id       text        PRIMARY KEY,
    slot_organisation_id text        NOT NULL,
    slot_id              text        NOT NULL,
    user_organisation_id text        NOT NULL,
    user_id              text        NOT NULL,
    state                text        NOT NULL CHECK (state IN ('held', 'confirmed', 'cancelled', 'expired')),
    created_at           timestamptz NOT NULL,
    expires_at           timestamptz NOT NULL,
    FOREIGN KEY (slot_organisation_id, slot_id)
        REFERENCES slots (slot_organisation_id, slot_id),
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
    ON reservations (slot_organisation_id, slot_id)
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
    booking_id           text        PRIMARY KEY,
    reservation_id       text        NOT NULL UNIQUE REFERENCES reservations (reservation_id),
    slot_organisation_id text        NOT NULL,
    slot_id              text        NOT NULL,
    user_organisation_id text        NOT NULL,
    user_id              text        NOT NULL,
    state                text        NOT NULL CHECK (state IN ('active', 'cancelled')),
    created_at           timestamptz NOT NULL,
    FOREIGN KEY (slot_organisation_id, slot_id)
        REFERENCES slots (slot_organisation_id, slot_id)
);

CREATE INDEX bookings_active_by_slot_idx
    ON bookings (slot_organisation_id, slot_id)
    WHERE state = 'active';

-- Idempotency records: one durable record per logical mutation request (§5.1). The
-- primary key IS the scope — (user_organisation_id, user_id, operation, key) — so the
-- unique constraint that backstops the key-reuse race (§5.3) is the table's own
-- identity rather than a separate index that could be dropped by accident.
--
-- A client key is deliberately NOT global: scoping it prevents one caller replaying
-- another's result. target_id is not stored; it lives only inside request_hash.
CREATE TABLE idempotency_records (
    user_organisation_id text        NOT NULL,
    user_id              text        NOT NULL,
    operation            text        NOT NULL CHECK (operation IN ('reserve', 'confirm', 'cancel')),
    key                  text        NOT NULL,
    request_hash         text        NOT NULL,
    outcome              text        NOT NULL,
    reason               text        NOT NULL,
    -- result_ref (§5.1): enough to reconstruct the original response on replay.
    -- Nullable because a refusal creates no entity. Deliberately NOT foreign keys:
    -- the record is inserted before the mutation it describes (§5.2 write ordering,
    -- so a scoped-key race is detected before any entity is written), and an FK would
    -- reject a row referencing an entity that is written later in the same
    -- transaction. The pairing is enforced by the service's commit invariant instead.
    reservation_id       text        NULL,
    booking_id           text        NULL,
    created_at           timestamptz NOT NULL,
    PRIMARY KEY (user_organisation_id, user_id, operation, key)
);

-- User identities: the serialization authority for one user's schedule mutations
-- (§2.2). The row carries no schedule state — the schedule itself is proven by
-- user_time_claims below — it exists to be locked: reserve takes FOR UPDATE on it after
-- the slot lock and before touching claims, so one user's claim-creating transactions
-- run one at a time.
--
-- Why serialize, when the exclusion constraint below already rejects overlap? Because
-- of *how* PostgreSQL enforces an exclusion constraint: each inserter places its index
-- tuple first and then scans for conflicts, so concurrent overlapping inserts for one
-- user can each find another's uncommitted tuple and each wait for the other — a
-- deadlock the server resolves by aborting victims, turning valid requests into faults.
-- Serializing a user's claim inserts before they reach the GiST index makes that cycle
-- unreachable; the constraint remains the invariant's authority for any writer that
-- does not hold this lock.
--
-- Rows are created on a user's first reserve and never deleted. A FOR UPDATE on a
-- never-deleted row is what makes the insert-then-lock acquisition race-free.
--
-- The three relations divide the transactional model cleanly: the slot row owns
-- capacity, the user identity row serializes schedule mutation, and the claim relation
-- proves schedule validity.
CREATE TABLE user_identities (
    user_organisation_id text NOT NULL,
    user_id              text NOT NULL,
    PRIMARY KEY (user_organisation_id, user_id)
);

-- User schedule non-overlap (§2.2). The slot row is the authority for one slot's
-- capacity, but it cannot protect one user booking two overlapping slots: those
-- transactions lock different slot rows and never contend. This table is the second
-- authority, keyed by user rather than by slot.
--
-- One row per *live* claim, so the exclusion constraint never needs a predicate over
-- elapsed holds — PostgreSQL could not index one anyway, since index predicates must be
-- immutable and "has this hold elapsed" changes with the wall clock. Elapsed claims are
-- removed by settlement inside the transaction they would otherwise block.
--
-- Keyed by reservation_id, not by a synthetic id: a hold and the booking confirmed from
-- it are one logical claim, so confirm UPDATEs this row rather than inserting a second
-- one. That makes "a booking self-conflicts with the hold it came from" unrepresentable
-- rather than merely untested.
CREATE TABLE user_time_claims (
    reservation_id       text        PRIMARY KEY
        -- DEFERRABLE because the claim is written during precondition evaluation, so
        -- the conflict is decided before the outcome is recorded (§5.2 write ordering)
        -- — which places the insert before the reservation row it refers to. Checked at
        -- COMMIT, when both rows exist.
        REFERENCES reservations (reservation_id) DEFERRABLE INITIALLY DEFERRED,
    -- The user's organisation (§1.1), never the slot's. A user of org_a booking a slot
    -- owned by org_b produces a claim keyed (org_a, user): keying by the slot's
    -- organisation would silently stop protecting a user who books across
    -- organisations, which is the only case the slot lock does not already cover.
    user_organisation_id text        NOT NULL,
    user_id              text        NOT NULL,
    -- Settlement and telemetry only: never part of the conflict key, which is exactly
    -- why claims on *different* slots still conflict.
    slot_organisation_id text        NOT NULL,
    slot_id              text        NOT NULL,
    -- Half-open [starts_at, ends_at): adjacent bookings do not overlap.
    claim_range          tstzrange   NOT NULL,
    -- The backing hold's expiry; NULL once confirmed, which is what makes a confirmed
    -- claim permanent and exempt from settlement.
    expires_at           timestamptz NULL,
    FOREIGN KEY (slot_organisation_id, slot_id)
        REFERENCES slots (slot_organisation_id, slot_id),
    CONSTRAINT user_time_claims_no_overlap EXCLUDE USING gist (
        user_organisation_id WITH =,
        user_id              WITH =,
        claim_range          WITH &&
    )
);

-- User-scoped settlement (§2.2) deletes this user's elapsed claims on the reserve path.
-- Partial, so the index stays proportional to unconfirmed claims.
CREATE INDEX user_time_claims_settlement_idx
    ON user_time_claims (user_organisation_id, user_id, expires_at)
    WHERE expires_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Dropped in dependency order. btree_gist is deliberately left installed: CREATE
-- EXTENSION IF NOT EXISTS does not establish that this migration created it, and
-- dropping a possibly-shared extension is worse than leaving an unused one.
DROP TABLE user_time_claims;
DROP TABLE user_identities;
DROP TABLE idempotency_records;
DROP TABLE bookings;
DROP TABLE reservations;
DROP TABLE slots;
-- +goose StatementEnd
