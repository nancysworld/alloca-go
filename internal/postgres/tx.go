package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// tx is the transactional view over one pgx transaction. It is valid only for the
// duration of one WithinTx call.
//
// now/established hold the attempt's authoritative timestamp. They are plain fields
// rather than anything synchronised because a pgx transaction is single-goroutine by
// construction: the service drives it sequentially inside the WithinTx closure.
type tx struct {
	conn        pgx.Tx
	now         time.Time
	established bool
}

// slotColumns is the projection shared by every slot read, kept in one place so the
// lock query and any later reader cannot drift apart in column order.
const slotColumns = `slot_id, slot_organisation_id, resource_id, capacity, release_at, starts_at, ends_at`

// LockSlot takes the slot's row lock and establishes the attempt's authoritative
// timestamp from the instant *after* that lock is granted.
//
// The two statements go out as one pgx.Batch. PostgreSQL executes a batch's
// statements in order, so clock_timestamp() is evaluated only once the FOR UPDATE
// statement has completed — including any time spent waiting behind another
// transaction's lock. Batching them keeps that ordering guarantee at the statement
// level, where it is a documented property, rather than relying on target-list or
// planner evaluation order within a single statement, where it is not. Sending both
// in one round trip means the guarantee costs no extra latency.
//
// transaction_timestamp()/now() would be wrong here whatever the shape: they return
// transaction-start time, which is stale by exactly the lock wait.
func (t *tx) LockSlot(ctx context.Context, ref domain.SlotRef) (domain.Slot, error) {
	batch := &pgx.Batch{}
	// Both halves of the slot's identity (§1.2): slot_id alone is unique only within an
	// organisation, so locking by it could resolve to another organisation's slot.
	batch.Queue(`SELECT `+slotColumns+` FROM slots WHERE slot_organisation_id = $1 AND slot_id = $2 FOR UPDATE`,
		string(ref.OrganisationID), string(ref.SlotID))
	batch.Queue(`SELECT clock_timestamp()`)

	results := t.conn.SendBatch(ctx, batch)
	// Every queued result must be consumed before Close, on the error paths too, or
	// the connection is left with unread results and cannot be reused.
	slot, slotErr := scanSlot(results.QueryRow())
	var now time.Time
	timeErr := results.QueryRow().Scan(&now)
	closeErr := results.Close()

	switch {
	case slotErr != nil:
		return domain.Slot{}, mapError(ctx, slotErr)
	case timeErr != nil:
		return domain.Slot{}, fmt.Errorf("postgres: resolve authoritative time: %w", mapError(ctx, timeErr))
	case closeErr != nil:
		return domain.Slot{}, fmt.Errorf("postgres: lock slot batch: %w", mapError(ctx, closeErr))
	}

	// Established only on the success path: a missing slot leaves the attempt with no
	// timestamp, forcing the caller onto the explicit no-slot path.
	t.setNow(now)
	return slot, nil
}

// LockUserIdentity takes the identity's schedule-serialization lock (§2.2): reserve
// calls it after LockSlot and before any claim work, so one identity's claim-creating
// transactions queue here instead of meeting inside the GiST exclusion index — where
// concurrent overlapping inserts deadlock, because each places its index tuple and
// then waits on the others'.
//
// Three statements, one batch, executed in order for the same reason LockSlot's are:
//
//  1. INSERT … ON CONFLICT DO NOTHING creates the row on the identity's first
//     reserve. It takes no lock on a pre-existing row — which is why it cannot
//     replace the FOR UPDATE — but when the row is genuinely new, concurrent
//     first-timers serialize on the speculative insert here instead.
//  2. SELECT … FOR UPDATE is the lock. Identity rows are never deleted, so this
//     always finds the row step 1 guaranteed exists.
//  3. clock_timestamp() resolves the instant after the lock wait (§1.5): the wait can
//     last until lock_timeout, and every decision made before it is stale by exactly
//     its length.
func (t *tx) LockUserIdentity(ctx context.Context, user domain.UserRef) (time.Time, error) {
	if !t.established {
		// An attempt with no timestamp has not locked its slot, and proceeding would
		// take the two authorities in the order the design forbids (§2.2 lock order).
		return time.Time{}, domain.ErrTimeNotEstablished
	}

	batch := &pgx.Batch{}
	batch.Queue(`INSERT INTO user_identities (user_organisation_id, user_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		string(user.OrganisationID), string(user.UserID))
	batch.Queue(`SELECT 1 FROM user_identities
		WHERE user_organisation_id = $1 AND user_id = $2 FOR UPDATE`,
		string(user.OrganisationID), string(user.UserID))
	batch.Queue(`SELECT clock_timestamp()`)

	results := t.conn.SendBatch(ctx, batch)
	// Every queued result must be consumed before Close, on the error paths too.
	_, insertErr := results.Exec()
	var one int
	lockErr := results.QueryRow().Scan(&one)
	var now time.Time
	timeErr := results.QueryRow().Scan(&now)
	closeErr := results.Close()

	switch {
	case insertErr != nil:
		return time.Time{}, fmt.Errorf("postgres: establish user identity: %w", mapError(ctx, insertErr))
	case lockErr != nil:
		return time.Time{}, fmt.Errorf("postgres: lock user identity: %w", mapError(ctx, lockErr))
	case timeErr != nil:
		return time.Time{}, fmt.Errorf("postgres: resolve authoritative time: %w", mapError(ctx, timeErr))
	case closeErr != nil:
		return time.Time{}, fmt.Errorf("postgres: lock user identity batch: %w", mapError(ctx, closeErr))
	}
	return now.UTC(), nil
}

// Now returns the attempt's authoritative timestamp, or ErrTimeNotEstablished if no
// lock (or explicit no-slot resolution) has established one. It never resolves a time
// source of its own — that is the whole point of memoising.
func (t *tx) Now(_ context.Context) (time.Time, error) {
	if !t.established {
		return time.Time{}, domain.ErrTimeNotEstablished
	}
	return t.now, nil
}

// ResolveTimeWithoutSlot establishes the timestamp for the one path that has no slot
// to lock: recording an unknown_target refusal (transaction-semantics §5.5). It costs
// one extra round trip, which is acceptable on a refusal path and is the honest
// alternative to pretending a slot lock occurred.
//
// If a timestamp is already established it is returned unchanged, so an attempt still
// has exactly one decision instant.
func (t *tx) ResolveTimeWithoutSlot(ctx context.Context) (time.Time, error) {
	if t.established {
		return t.now, nil
	}
	var now time.Time
	if err := t.conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("postgres: resolve authoritative time: %w", mapError(ctx, err))
	}
	t.setNow(now)
	return t.now, nil
}

// setNow records the attempt timestamp in UTC. Persisted and reported timestamps are
// UTC throughout, so the session TimeZone cannot change how a value reads later.
func (t *tx) setNow(now time.Time) {
	t.now = now.UTC()
	t.established = true
}

func (t *tx) SlotRefForReservation(ctx context.Context, id domain.ReservationID) (domain.SlotRef, error) {
	var ref domain.SlotRef
	err := t.conn.QueryRow(ctx,
		`SELECT slot_organisation_id, slot_id FROM reservations WHERE reservation_id = $1`,
		string(id)).Scan(&ref.OrganisationID, &ref.SlotID)
	if err != nil {
		return domain.SlotRef{}, mapError(ctx, err)
	}
	return ref, nil
}

func (t *tx) Reservation(ctx context.Context, id domain.ReservationID) (domain.Reservation, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT reservation_id, slot_organisation_id, slot_id, user_organisation_id,
		       user_id, state, created_at, expires_at
		FROM reservations WHERE reservation_id = $1`, string(id))
	res, err := scanReservation(row)
	if err != nil {
		return domain.Reservation{}, mapError(ctx, err)
	}
	return res, nil
}

// HeldReservations returns the slot's held rows for settlement and consumed-capacity
// derivation. It is called with the slot lock held, so the result cannot be changed
// by a concurrent transaction before it is used.
func (t *tx) HeldReservations(ctx context.Context, ref domain.SlotRef) ([]domain.Reservation, error) {
	rows, err := t.conn.Query(ctx, `
		SELECT reservation_id, slot_organisation_id, slot_id, user_organisation_id,
		       user_id, state, created_at, expires_at
		FROM reservations
		WHERE slot_organisation_id = $1 AND slot_id = $2 AND state = 'held'`,
		string(ref.OrganisationID), string(ref.SlotID))
	if err != nil {
		return nil, mapError(ctx, err)
	}
	defer rows.Close()

	var held []domain.Reservation
	for rows.Next() {
		res, err := scanReservation(rows)
		if err != nil {
			return nil, mapError(ctx, err)
		}
		held = append(held, res)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(ctx, err)
	}
	return held, nil
}

func (t *tx) ActiveBookingCount(ctx context.Context, ref domain.SlotRef) (int, error) {
	var count int
	err := t.conn.QueryRow(ctx,
		`SELECT count(*) FROM bookings
		 WHERE slot_organisation_id = $1 AND slot_id = $2 AND state = 'active'`,
		string(ref.OrganisationID), string(ref.SlotID)).Scan(&count)
	if err != nil {
		return 0, mapError(ctx, err)
	}
	return count, nil
}

func (t *tx) BookingForReservation(ctx context.Context, id domain.ReservationID) (domain.Booking, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT booking_id, reservation_id, slot_organisation_id, slot_id,
		       user_organisation_id, user_id, state, created_at
		FROM bookings WHERE reservation_id = $1`, string(id))
	var b domain.Booking
	err := row.Scan(&b.ID, &b.ReservationID, &b.SlotRef.OrganisationID, &b.SlotRef.SlotID,
		&b.UserRef.OrganisationID, &b.UserRef.UserID, &b.State, &b.CreatedAt)
	if err != nil {
		return domain.Booking{}, mapError(ctx, err)
	}
	b.CreatedAt = b.CreatedAt.UTC()
	return b, nil
}

// PutReservation inserts or updates a reservation. The upsert form keeps the port's
// "insert or update" contract in one statement; only the mutable columns are updated,
// so a state transition can never rewrite the row's identity or its slot.
func (t *tx) PutReservation(ctx context.Context, r domain.Reservation) error {
	_, err := t.conn.Exec(ctx, `
		INSERT INTO reservations
			(reservation_id, slot_organisation_id, slot_id, user_organisation_id, user_id,
			 state, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (reservation_id) DO UPDATE
			SET state = EXCLUDED.state, expires_at = EXCLUDED.expires_at`,
		string(r.ID), string(r.SlotRef.OrganisationID), string(r.SlotRef.SlotID),
		string(r.UserRef.OrganisationID), string(r.UserRef.UserID), string(r.State),
		r.CreatedAt, r.ExpiresAt)
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

func (t *tx) PutBooking(ctx context.Context, b domain.Booking) error {
	_, err := t.conn.Exec(ctx, `
		INSERT INTO bookings
			(booking_id, reservation_id, slot_organisation_id, slot_id,
			 user_organisation_id, user_id, state, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (booking_id) DO UPDATE
			SET state = EXCLUDED.state`,
		string(b.ID), string(b.ReservationID), string(b.SlotRef.OrganisationID),
		string(b.SlotRef.SlotID), string(b.UserRef.OrganisationID), string(b.UserRef.UserID),
		string(b.State), b.CreatedAt)
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

// InsertClaim inserts the schedule claim, returning the authoritative instant at which
// the claim authority was actually acquired, or domain.ErrScheduleConflict when the
// exclusion constraint rejects it as overlapping an existing claim of the same user.
//
// The insert runs inside a savepoint (pgx models a nested Begin as one), and what the
// rollback recovers is not data — the rejected row was never written — but the
// transaction's *state*. In PostgreSQL an error aborts the whole transaction block, not
// just the failing statement: every subsequent command then fails with 25P02 and the
// COMMIT is turned into a ROLLBACK. This transaction is not over, because the caller still
// has to record the refusal and commit it. Rolling back to the savepoint clears that
// aborted state and leaves the surrounding transaction usable, which is what turns an
// insert the exclusion constraint rejected into a clean business refusal rather than a
// fault — and keeps the idempotency record, without which a retry would re-run instead of
// replaying.
//
// The rejection usually means the user already holds a claim committed long ago; it can
// equally be one that committed while this insert waited (see below). Neither is a race in
// any sense that needs recovering from — both are the constraint doing its job.
//
// The timestamp comes from RETURNING clock_timestamp() on the insert itself, so it is
// evaluated once the row is in — including any time spent blocked behind a conflicting
// uncommitted claim, which can last until lock_timeout. That is the point: the caller's
// earlier decisions were made before this wait and may be stale by its length
// (transaction-semantics §1.5). Taking the instant in the same statement rather than a
// following query means it cannot drift between acquiring the authority and reading the
// clock, and costs no extra round trip.
//
// The range is built in SQL rather than passed as a range value so the '[)' bound is
// stated where the constraint that reads it lives: the half-open boundary is the
// difference between "adjacent" and "overlapping".
func (t *tx) InsertClaim(ctx context.Context, c domain.ScheduleClaim) (time.Time, error) {
	sp, err := t.conn.Begin(ctx)
	if err != nil {
		return time.Time{}, mapError(ctx, err)
	}
	var acquiredAt time.Time
	err = sp.QueryRow(ctx, `
		INSERT INTO user_time_claims
			(reservation_id, user_organisation_id, user_id, slot_organisation_id, slot_id,
			 claim_range, expires_at)
		VALUES ($1, $2, $3, $4, $5, tstzrange($6, $7, '[)'), $8)
		RETURNING clock_timestamp()`,
		string(c.ReservationID), string(c.UserRef.OrganisationID), string(c.UserRef.UserID),
		string(c.SlotRef.OrganisationID), string(c.SlotRef.SlotID),
		c.StartsAt, c.EndsAt, c.ExpiresAt).Scan(&acquiredAt)
	if err != nil {
		// Rollback restores the savepoint; its own error is deliberately not returned
		// over the classified insert error, which is the one that explains the outcome.
		_ = sp.Rollback(ctx)
		if isExclusionViolation(err) {
			return time.Time{}, domain.ErrScheduleConflict
		}
		return time.Time{}, mapError(ctx, err)
	}
	if err := sp.Commit(ctx); err != nil {
		return time.Time{}, mapError(ctx, err)
	}
	return acquiredAt.UTC(), nil
}

// SetClaimExpiry writes the claim's final expiry, computed from the post-acquisition
// instant. The interval and the user are untouched, so the exclusion constraint cannot
// reject it: the row keeps the range it already occupies.
func (t *tx) SetClaimExpiry(ctx context.Context, id domain.ReservationID, expiresAt time.Time) error {
	tag, err := t.conn.Exec(ctx,
		`UPDATE user_time_claims SET expires_at = $2 WHERE reservation_id = $1`,
		string(id), expiresAt)
	if err != nil {
		return mapError(ctx, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: set expiry on claim for reservation %q: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ConfirmClaim clears the claim's expiry, making it permanent. The interval and the
// identity are untouched, so the constraint cannot reject this update: it is the same
// row occupying the same range it already occupied.
func (t *tx) ConfirmClaim(ctx context.Context, id domain.ReservationID) error {
	tag, err := t.conn.Exec(ctx,
		`UPDATE user_time_claims SET expires_at = NULL WHERE reservation_id = $1`,
		string(id))
	if err != nil {
		return mapError(ctx, err)
	}
	if tag.RowsAffected() == 0 {
		// Confirming a reservation whose claim is missing means hold and claim have
		// diverged. That is an invariant breach, not a domain answer, so it surfaces as a
		// fault rather than silently confirming an unprotected booking.
		return fmt.Errorf("postgres: confirm claim for reservation %q: %w", id, domain.ErrNotFound)
	}
	return nil
}

// DeleteClaim removes a claim. Its callers are cancellation and reserve withdrawing a
// claim it provisionally inserted; expiry settlement deliberately never calls it, since
// user-scoped SettleClaims is the sole reaper of elapsed claims (§2.2).
//
// Deleting an absent claim succeeds: both callers express "this claim must not survive
// the transaction", and a claim user-scoped settlement already removed satisfies that.
// Converging on the intended state is not an error.
func (t *tx) DeleteClaim(ctx context.Context, id domain.ReservationID) error {
	_, err := t.conn.Exec(ctx, `DELETE FROM user_time_claims WHERE reservation_id = $1`, string(id))
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

// SettleClaims deletes the user's elapsed claims. The boundary is expires_at <= now,
// matching Reservation.Elapsed, and now is the attempt's authoritative PostgreSQL
// timestamp — never an API host clock, which must not decide whether a claim is live.
//
// Confirmed claims have expires_at IS NULL and are excluded by the predicate rather than
// by a state check, so a permanent claim cannot be settled away by a boundary mistake.
func (t *tx) SettleClaims(ctx context.Context, user domain.UserRef, now time.Time) error {
	_, err := t.conn.Exec(ctx, `
		DELETE FROM user_time_claims
		WHERE user_organisation_id = $1 AND user_id = $2
		  AND expires_at IS NOT NULL AND expires_at <= $3`,
		string(user.OrganisationID), string(user.UserID), now)
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

// isExclusionViolation reports whether err is a PostgreSQL exclusion-constraint
// violation. Scoped like isUniqueViolation: user_time_claims carries the only exclusion
// constraint in the schema, so 23P01 means the schedule invariant and nothing else.
func isExclusionViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ExclusionViolation
}

func (t *tx) FindRecord(ctx context.Context, key domain.ScopeKey) (domain.IdempotencyRecord, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT user_organisation_id, user_id, operation, key, request_hash, outcome, reason,
		       reservation_id, booking_id, created_at
		FROM idempotency_records
		WHERE user_organisation_id = $1 AND user_id = $2 AND operation = $3 AND key = $4`,
		string(key.UserRef.OrganisationID), string(key.UserRef.UserID),
		string(key.Operation), key.Key)

	var rec domain.IdempotencyRecord
	// result_ref is nullable — a refusal creates no entity — so these scan through
	// pointers and stay empty when NULL.
	var resID, bkID *string
	err := row.Scan(&rec.UserRef.OrganisationID, &rec.UserRef.UserID, &rec.Operation, &rec.Key,
		&rec.RequestHash, &rec.Outcome, &rec.Reason, &resID, &bkID, &rec.CreatedAt)
	if err != nil {
		return domain.IdempotencyRecord{}, mapError(ctx, err)
	}
	if resID != nil {
		rec.ReservationID = domain.ReservationID(*resID)
	}
	if bkID != nil {
		rec.BookingID = domain.BookingID(*bkID)
	}
	rec.CreatedAt = rec.CreatedAt.UTC()
	return rec, nil
}

// InsertRecord inserts the idempotency record, returning domain.ErrConflict when the
// scoped key is already taken. There is deliberately no upsert: the unique constraint
// is the concurrency backstop of §5.3, and "one scoped key records exactly one
// terminal outcome" only holds if a second insert fails rather than overwrites.
func (t *tx) InsertRecord(ctx context.Context, rec domain.IdempotencyRecord) error {
	_, err := t.conn.Exec(ctx, `
		INSERT INTO idempotency_records
			(user_organisation_id, user_id, operation, key, request_hash, outcome, reason,
			 reservation_id, booking_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		string(rec.UserRef.OrganisationID), string(rec.UserRef.UserID), string(rec.Operation), rec.Key,
		rec.RequestHash, string(rec.Outcome), string(rec.Reason),
		nullableID(string(rec.ReservationID)), nullableID(string(rec.BookingID)), rec.CreatedAt)
	if err != nil {
		// The scoped-key unique violation is mapped here rather than in mapError so it
		// is scoped to the one table where it means "another transaction recorded this
		// key first". A unique violation anywhere else — two bookings for one
		// reservation, say — is an invariant breach, and must surface as a fault rather
		// than be retried as an idempotency race.
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return mapError(ctx, err)
	}
	return nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}

// nullableID renders an unset identifier as SQL NULL rather than an empty string, so
// "no entity was created" is representable as absence instead of a sentinel value.
func nullableID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows, so a single scan helper serves
// the single-row and multi-row readers.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSlot(row rowScanner) (domain.Slot, error) {
	var s domain.Slot
	err := row.Scan(&s.ID, &s.OrganisationID, &s.ResourceID, &s.Capacity,
		&s.ReleaseAt, &s.StartsAt, &s.EndsAt)
	if err != nil {
		return domain.Slot{}, err
	}
	s.ReleaseAt, s.StartsAt, s.EndsAt = s.ReleaseAt.UTC(), s.StartsAt.UTC(), s.EndsAt.UTC()
	return s, nil
}

func scanReservation(row rowScanner) (domain.Reservation, error) {
	var r domain.Reservation
	err := row.Scan(&r.ID, &r.SlotRef.OrganisationID, &r.SlotRef.SlotID,
		&r.UserRef.OrganisationID, &r.UserRef.UserID, &r.State, &r.CreatedAt, &r.ExpiresAt)
	if err != nil {
		return domain.Reservation{}, err
	}
	r.CreatedAt, r.ExpiresAt = r.CreatedAt.UTC(), r.ExpiresAt.UTC()
	return r, nil
}

// mapError translates a pgx/PostgreSQL error into the domain vocabulary, which is what
// keeps every failure inside the §4 taxonomy instead of collapsing to a generic fault.
//
// The context check on 57014 matters: query_canceled is raised both when
// statement_timeout expires and when the client cancels the query. Only the former is
// timeout_db; the latter is the caller's own cancellation and must stay distinguishable
// as timeout_client / timeout_server.
func mapError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.LockNotAvailable:
			// lock_timeout expired waiting for a row lock: the innermost bound of the chain.
			return fmt.Errorf("postgres: lock wait exceeded lock_timeout: %w", domain.ErrDBTimeout)
		case pgerrcode.QueryCanceled:
			if ctx.Err() != nil {
				return fmt.Errorf("postgres: query cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("postgres: statement exceeded statement_timeout: %w", domain.ErrDBTimeout)
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("postgres: %w", ctx.Err())
	}
	return err
}

// Static assertion that tx satisfies the domain port.
var _ domain.Tx = (*tx)(nil)
