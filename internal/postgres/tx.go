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
const slotColumns = `slot_id, organisation_id, resource_id, capacity, release_at, starts_at, ends_at`

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
func (t *tx) LockSlot(ctx context.Context, id domain.SlotID) (domain.Slot, error) {
	batch := &pgx.Batch{}
	batch.Queue(`SELECT `+slotColumns+` FROM slots WHERE slot_id = $1 FOR UPDATE`, string(id))
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

func (t *tx) SlotIDForReservation(ctx context.Context, id domain.ReservationID) (domain.SlotID, error) {
	var slotID string
	err := t.conn.QueryRow(ctx, `SELECT slot_id FROM reservations WHERE reservation_id = $1`, string(id)).Scan(&slotID)
	if err != nil {
		return "", mapError(ctx, err)
	}
	return domain.SlotID(slotID), nil
}

func (t *tx) Reservation(ctx context.Context, id domain.ReservationID) (domain.Reservation, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT reservation_id, slot_id, organisation_id, user_id, state, created_at, expires_at
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
func (t *tx) HeldReservations(ctx context.Context, slotID domain.SlotID) ([]domain.Reservation, error) {
	rows, err := t.conn.Query(ctx, `
		SELECT reservation_id, slot_id, organisation_id, user_id, state, created_at, expires_at
		FROM reservations WHERE slot_id = $1 AND state = 'held'`, string(slotID))
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

func (t *tx) ActiveBookingCount(ctx context.Context, slotID domain.SlotID) (int, error) {
	var count int
	err := t.conn.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE slot_id = $1 AND state = 'active'`,
		string(slotID)).Scan(&count)
	if err != nil {
		return 0, mapError(ctx, err)
	}
	return count, nil
}

func (t *tx) BookingForReservation(ctx context.Context, id domain.ReservationID) (domain.Booking, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT booking_id, reservation_id, slot_id, organisation_id, user_id, state, created_at
		FROM bookings WHERE reservation_id = $1`, string(id))
	var b domain.Booking
	err := row.Scan(&b.ID, &b.ReservationID, &b.SlotID, &b.OrganisationID, &b.UserID, &b.State, &b.CreatedAt)
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
			(reservation_id, slot_id, organisation_id, user_id, state, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (reservation_id) DO UPDATE
			SET state = EXCLUDED.state, expires_at = EXCLUDED.expires_at`,
		string(r.ID), string(r.SlotID), string(r.OrganisationID), string(r.UserID),
		string(r.State), r.CreatedAt, r.ExpiresAt)
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

func (t *tx) PutBooking(ctx context.Context, b domain.Booking) error {
	_, err := t.conn.Exec(ctx, `
		INSERT INTO bookings
			(booking_id, reservation_id, slot_id, organisation_id, user_id, state, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (booking_id) DO UPDATE
			SET state = EXCLUDED.state`,
		string(b.ID), string(b.ReservationID), string(b.SlotID), string(b.OrganisationID),
		string(b.UserID), string(b.State), b.CreatedAt)
	if err != nil {
		return mapError(ctx, err)
	}
	return nil
}

func (t *tx) FindRecord(ctx context.Context, key domain.ScopeKey) (domain.IdempotencyRecord, error) {
	row := t.conn.QueryRow(ctx, `
		SELECT organisation_id, user_id, operation, key, request_hash, outcome, reason,
		       reservation_id, booking_id, created_at
		FROM idempotency_records
		WHERE organisation_id = $1 AND user_id = $2 AND operation = $3 AND key = $4`,
		string(key.OrganisationID), string(key.UserID), string(key.Operation), key.Key)

	var rec domain.IdempotencyRecord
	// result_ref is nullable — a refusal creates no entity — so these scan through
	// pointers and stay empty when NULL.
	var resID, bkID *string
	err := row.Scan(&rec.OrganisationID, &rec.UserID, &rec.Operation, &rec.Key,
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
			(organisation_id, user_id, operation, key, request_hash, outcome, reason,
			 reservation_id, booking_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		string(rec.OrganisationID), string(rec.UserID), string(rec.Operation), rec.Key,
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
	err := row.Scan(&r.ID, &r.SlotID, &r.OrganisationID, &r.UserID, &r.State,
		&r.CreatedAt, &r.ExpiresAt)
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
