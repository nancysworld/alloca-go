-- +goose Up
-- +goose StatementBegin

-- User schedule non-overlap (docs/design/transaction-semantics.md §2.2).
--
-- The slot row is the authority for one slot's capacity, but it cannot protect one
-- identity booking two overlapping slots: those transactions lock different slot rows
-- and never contend. This table is the second authority, keyed by identity rather than
-- by slot.

-- Required for the scalar equality operators in the exclusion constraint below: GiST
-- has no native `=` operator class for text. btree_gist is not a trusted extension, so
-- this migration runs as a superuser (CI) or rds_superuser (AG-M3).
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- One row per *live* claim, so the constraint never needs a predicate over elapsed
-- holds — PostgreSQL could not index one anyway, since index predicates must be
-- immutable and "has this hold elapsed" changes with the wall clock.
--
-- Keyed by reservation_id, not by a synthetic id: a hold and the booking confirmed
-- from it are one logical claim, so confirm UPDATEs this row rather than inserting a
-- second one. That makes "a booking self-conflicts with the hold it came from"
-- unrepresentable rather than merely untested.
CREATE TABLE user_time_claims (
    reservation_id  text        PRIMARY KEY
        -- DEFERRABLE because the claim is written during precondition evaluation, so
        -- the conflict is decided before the outcome is recorded (§5.2 write
        -- ordering) — which places the insert before the reservation row it refers
        -- to. Checked at COMMIT, when both rows exist.
        REFERENCES reservations (reservation_id) DEFERRABLE INITIALLY DEFERRED,
    -- The identity's organisation (§1.1), NOT the slot's. A member of org_a booking a
    -- slot owned by org_b produces a claim keyed (org_a, user): keying by the slot's
    -- organisation would silently stop protecting an identity that books across
    -- organisations, which is the only case the slot lock does not already cover.
    organisation_id text        NOT NULL,
    user_id         text        NOT NULL,
    -- Grouping/telemetry and settlement only: never part of the conflict key.
    slot_id         text        NOT NULL REFERENCES slots (slot_id),
    -- Half-open [starts_at, ends_at): adjacent bookings do not overlap.
    claim_range     tstzrange   NOT NULL,
    -- The backing hold's expiry; NULL once confirmed, which is what makes a confirmed
    -- claim permanent and exempt from settlement.
    expires_at      timestamptz NULL,
    CONSTRAINT user_time_claims_no_overlap EXCLUDE USING gist (
        organisation_id WITH =,
        user_id         WITH =,
        claim_range     WITH &&
    )
);

-- Identity-scoped settlement (§2.2) deletes this identity's elapsed claims on the
-- reserve path. Partial, so the index stays proportional to unconfirmed claims.
CREATE INDEX user_time_claims_settlement_idx
    ON user_time_claims (organisation_id, user_id, expires_at)
    WHERE expires_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_time_claims;
DROP EXTENSION IF EXISTS btree_gist;
-- +goose StatementEnd
