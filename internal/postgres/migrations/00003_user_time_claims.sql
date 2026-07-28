-- +goose Up
-- +goose StatementBegin

-- User schedule non-overlap (docs/design/transaction-semantics.md §2.2).
--
-- The slot row is the authority for one slot's capacity, but it cannot protect one
-- identity booking two overlapping slots: those transactions lock different slot rows
-- and never contend. This table is the second authority, keyed by identity rather than
-- by slot.

-- Required for the scalar equality operators in the exclusion constraint below: GiST
-- has no native `=` operator class for text. btree_gist is a *trusted* extension
-- (verified on PostgreSQL 16: pg_available_extension_versions.trusted = true), so
-- installing it needs the CREATE privilege on the database, not superuser. It must be
-- available on the server — on RDS that means it is in rds.allowed_extensions.
--
-- IF NOT EXISTS, because another schema or application in the same database may already
-- have installed it. That is also why the Down migration deliberately does not drop it:
-- this migration cannot tell whether it created the extension, and removing a shared
-- dependency is worse than leaving an unused one behind.
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
    -- The user's organisation (§1.1), never the slot's. A user of org_a booking a slot
    -- owned by org_b produces a claim keyed (org_a, user): keying by the slot's
    -- organisation would silently stop protecting a user who books across
    -- organisations, which is the only case the slot lock does not already cover.
    user_organisation_id text   NOT NULL,
    user_id         text        NOT NULL,
    -- Grouping/telemetry and settlement only: never part of the conflict key. The
    -- slot's identity is the pair (slot_organisation_id, slot_id) (§1.2), whose
    -- organisation is the slot's owner — not the user's above, which is why it is a
    -- column of its own.
    slot_organisation_id text   NOT NULL,
    slot_id         text        NOT NULL,
    FOREIGN KEY (slot_organisation_id, slot_id) REFERENCES slots (slot_organisation_id, slot_id),
    -- Half-open [starts_at, ends_at): adjacent bookings do not overlap.
    claim_range     tstzrange   NOT NULL,
    -- The backing hold's expiry; NULL once confirmed, which is what makes a confirmed
    -- claim permanent and exempt from settlement.
    expires_at      timestamptz NULL,
    CONSTRAINT user_time_claims_no_overlap EXCLUDE USING gist (
        user_organisation_id WITH =,
        user_id              WITH =,
        claim_range          WITH &&
    )
);

-- User-scoped settlement (§2.2) deletes this user's elapsed claims on the reserve path.
-- Partial, so the index stays proportional to unconfirmed claims.
--
-- Created before the backfill below, not after: the claim's foreign key to reservations
-- is DEFERRABLE, so the INSERT leaves deferred trigger events pending, and PostgreSQL
-- refuses any further DDL on the table while they are (SQLSTATE 55006). Nothing may
-- follow that INSERT.
CREATE INDEX user_time_claims_settlement_idx
    ON user_time_claims (user_organisation_id, user_id, expires_at)
    WHERE expires_at IS NOT NULL;

-- Backfill: an upgraded database already holds live reservations and bookings, and a
-- claim table that starts empty would silently exempt every one of them. Two failures
-- follow from an empty start: existing active bookings do not participate in the
-- constraint at all, so the very overlap this migration exists to forbid stays
-- admissible for them; and confirming an existing held reservation finds no claim row
-- to update, which the service treats as an invariant breach rather than a domain
-- answer.
--
-- What counts as live is exactly what §1 calls an active claim: an unexpired hold, or a
-- confirmed reservation whose booking is still active. Elapsed holds, cancelled
-- reservations, and cancelled bookings are not claims and are deliberately skipped.
INSERT INTO user_time_claims
    (reservation_id, user_organisation_id, user_id, slot_organisation_id, slot_id,
     claim_range, expires_at)
SELECT r.reservation_id, r.user_organisation_id, r.user_id,
       r.slot_organisation_id, r.slot_id,
       tstzrange(s.starts_at, s.ends_at, '[)'),
       -- A confirmed claim is permanent; only a hold carries an expiry.
       CASE WHEN r.state = 'held' THEN r.expires_at END
FROM reservations r
JOIN slots s
  ON s.slot_organisation_id = r.slot_organisation_id AND s.slot_id = r.slot_id
WHERE (r.state = 'held' AND r.expires_at > now())
   OR (r.state = 'confirmed' AND EXISTS (
        SELECT 1 FROM bookings b
        WHERE b.reservation_id = r.reservation_id AND b.state = 'active'));

-- The constraint is already live when the rows above are inserted, so the backfill is
-- checked row by row rather than against an empty table. If the database already holds
-- overlapping bookings for one user — which is possible, because admitting them is
-- precisely the bug this migration fixes — the INSERT raises exclusion_violation and the
-- whole migration aborts.
--
-- That refusal is deliberate. Skipping the conflicting rows would leave those users
-- unprotected while the schema claimed otherwise, which is the "correct by accident"
-- state this change exists to remove. To find them before migrating:
--
--   SELECT a.reservation_id, b.reservation_id
--     FROM reservations a JOIN reservations b
--       ON a.user_organisation_id = b.user_organisation_id
--      AND a.user_id = b.user_id
--      AND a.reservation_id < b.reservation_id
--     JOIN slots sa ON sa.slot_organisation_id = a.slot_organisation_id AND sa.slot_id = a.slot_id
--     JOIN slots sb ON sb.slot_organisation_id = b.slot_organisation_id AND sb.slot_id = b.slot_id
--    WHERE a.state IN ('held','confirmed') AND b.state IN ('held','confirmed')
--      AND tstzrange(sa.starts_at, sa.ends_at, '[)') && tstzrange(sb.starts_at, sb.ends_at, '[)');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- btree_gist is deliberately left installed: CREATE EXTENSION IF NOT EXISTS does not
-- establish that this migration created it, and dropping a possibly-shared extension is
-- worse than leaving an unused one.
DROP TABLE user_time_claims;
-- +goose StatementEnd
