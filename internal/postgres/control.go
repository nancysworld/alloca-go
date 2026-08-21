package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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
			(slot_id, slot_organisation_id, resource_id, capacity, release_at, starts_at, ends_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (slot_organisation_id, slot_id) DO UPDATE SET
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

// ElapsedHoldSlots returns up to limit slots that currently hold at least one elapsed
// reservation, so the expiry worker knows which slot locks are worth taking.
//
// It takes no timestamp, deliberately: "elapsed" is decided by clock_timestamp() inside
// the query (transaction-semantics §1.5), so the worker's process clock only ever decides
// *when to poll*, never *what has expired*.
//
// It is a candidate list, not an authority — the rows are read without a lock, so a
// concurrent request may settle a slot first. That is harmless: settlement under the slot
// lock re-derives everything and simply expires nothing.
func (r *Repo) ElapsedHoldSlots(ctx context.Context, limit int) ([]domain.SlotRef, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT slot_organisation_id, slot_id
		FROM reservations
		WHERE state = 'held' AND expires_at <= clock_timestamp()
		ORDER BY slot_organisation_id, slot_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: elapsed hold slots: %w", err)
	}
	defer rows.Close()

	var refs []domain.SlotRef
	for rows.Next() {
		var ref domain.SlotRef
		if err := rows.Scan(&ref.OrganisationID, &ref.SlotID); err != nil {
			return nil, fmt.Errorf("postgres: elapsed hold slots: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: elapsed hold slots: %w", err)
	}
	return refs, nil
}

// SlotsByOrganisation returns up to limit of the organisation's slots, for the
// informational listing endpoint.
//
// Ordering is (starts_at, slot_id): chronological, and deterministic because slot_id is
// unique within the organisation the query already fixes — which is what makes the
// caller's truncation predictable rather than an arbitrary subset. The handler does not
// re-sort, so determinism has one owner.
//
// It reads no reservation or booking state, deliberately; see httpapi.slotView.
func (r *Repo) SlotsByOrganisation(ctx context.Context, org domain.OrganisationID, limit int) ([]domain.Slot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+slotColumns+`
		FROM slots
		WHERE slot_organisation_id = $1
		ORDER BY starts_at, slot_id
		LIMIT $2`, string(org), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: slots by organisation: %w", err)
	}
	defer rows.Close()

	var slots []domain.Slot
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: slots by organisation: %w", err)
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: slots by organisation: %w", err)
	}
	return slots, nil
}

// Ready reports whether the database can serve booking traffic, for the readiness probe.
//
// It acquires a pooled connection and round-trips a trivial statement, exercising the two
// things a request needs: that the pool can hand out a connection, and that the server
// answers. Bounding is the caller's job — the probe's timeout is what makes the difference
// between "slow" and "gone" (httpapi.handleReadyz).
func (r *Repo) Ready(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: readiness: %w", err)
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
func (r *Repo) SlotCounts(ctx context.Context, ref domain.SlotRef) (held, activeBookings int, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM reservations
			  WHERE slot_organisation_id = $1 AND slot_id = $2 AND state = 'held'),
			(SELECT count(*) FROM bookings
			  WHERE slot_organisation_id = $1 AND slot_id = $2 AND state = 'active')`,
		string(ref.OrganisationID), string(ref.SlotID)).Scan(&held, &activeBookings)
	if err != nil {
		return 0, 0, fmt.Errorf("postgres: slot counts: %w", err)
	}
	return held, activeBookings, nil
}

// ReservationStates returns a census of reservation states for a slot, so a test can
// assert on the shape of the whole state machine rather than one row at a time.
func (r *Repo) ReservationStates(ctx context.Context, ref domain.SlotRef) (map[domain.ReservationState]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT state, count(*) FROM reservations
		 WHERE slot_organisation_id = $1 AND slot_id = $2 GROUP BY state`,
		string(ref.OrganisationID), string(ref.SlotID))
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

// OverlappingClaims counts the pairs of persisted claims that violate the user schedule
// non-overlap invariant (transaction-semantics §2.2): same user, overlapping
// intervals. It is the direct expression of the decisive assertion —
//
//	at every committed state, for any user and instant, at most one active claim
//	contains that instant
//
// — evaluated against stored rows rather than against what the service reported, so an
// implementation that returned correct answers while writing overlapping state still
// fails. The `<` on reservation_id counts each pair once and excludes the self-join.
func (r *Repo) OverlappingClaims(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM user_time_claims a
		JOIN user_time_claims b
		  ON a.user_organisation_id = b.user_organisation_id
		 AND a.user_id              = b.user_id
		 AND a.reservation_id       < b.reservation_id
		 AND a.claim_range          && b.claim_range`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: overlapping claims: %w", err)
	}
	return count, nil
}

// ClaimCount returns the number of persisted schedule claims, so a test can assert that
// cancellation and expiry actually removed a claim rather than leaving a dead row that
// would block the identity's schedule forever.
func (r *Repo) ClaimCount(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM user_time_claims`).Scan(&count); err != nil {
		return 0, fmt.Errorf("postgres: claim count: %w", err)
	}
	return count, nil
}

// IdempotencyRecordCount returns the number of persisted idempotency records.
//
// It exists for the clean-start assertion of ag-sept/milestone-validation.md §3.3, which cannot be made from
// claims alone. Records outlive the reservations they describe — a hold expires, a claim is
// settled, a booking is cancelled, and the record stays, because its whole purpose is to
// answer a retry that arrives after the entity is gone. So a fixture can hold zero live
// claims and still make the next run a replay of the last one, since PR1's workloads derive
// deterministic keys from the workload name and sequence number.
func (r *Repo) IdempotencyRecordCount(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records`).Scan(&count); err != nil {
		return 0, fmt.Errorf("postgres: idempotency record count: %w", err)
	}
	return count, nil
}

// suiteLockKey is the integration suites' advisory-lock key: "alloca" in ASCII. The value
// is arbitrary; what matters is that every suite uses the *same* one, which is why it
// lives here rather than in one package's test files.
const suiteLockKey int64 = 0x616c6c6f6361

// suiteLockWait bounds how long a second test process queues behind the first. Long
// enough to sit through a full suite run, short enough that a wedged process fails with an
// explanation rather than hanging until someone notices.
const suiteLockWait = 3 * time.Minute

// ClaimDatabase takes a session-scoped advisory lock so only one integration test binary
// works against a given database at a time, and returns the release. Like Truncate, it
// exists for tests and is never called by the service.
//
// It is required because every suite truncates the *whole* database, so two concurrent
// test processes sharing one DATABASE_URL delete each other's world mid-test — and
// `go test ./...` runs package binaries in parallel, so two suites is the normal case, not
// an unusual one. The symptom is maximally misleading: a slot a test has just seeded and
// committed disappears, and the operation under test is refused unknown_target, which
// reads as a correctness bug in the code under test rather than as two processes
// colliding. Verified by running a TRUNCATE in a loop underneath the suite, which
// reproduces exactly that refusal.
//
// The claim is held on a dedicated connection for the life of the process, because a lock
// taken with pg_advisory_lock belongs to the *session* that took it: a pooled connection
// could be handed to another borrower or reset, dropping the claim while the suite runs.
// Ending the session releases the lock, so a crashed or killed process cannot wedge the
// next one.
//
// It guards against another test binary, which is the case that actually happens. It
// cannot protect against an unrelated client writing to the same database — nothing short
// of a private database can.
func ClaimDatabase(dsn string) (release func(), err error) {
	ctx, cancel := context.WithTimeout(context.Background(), suiteLockWait)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("claim database: connect: %w", err)
	}
	// Blocks until the holder exits. The DSN is deliberately not echoed on failure: it
	// carries credentials.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, suiteLockKey); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("claim database: gave up after %s waiting for another integration "+
			"test process to finish with this database; each process truncates the whole "+
			"database, so they cannot share one: %w", suiteLockWait, err)
	}
	return func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelClose()
		// Ending the session releases the lock; an explicit unlock would be redundant.
		_ = conn.Close(closeCtx)
	}, nil
}

// truncateStatementTimeout bounds the truncate itself, deliberately generously.
//
// It is not derived from the request budget: emptying six tables is control-plane work
// whose duration has nothing to do with what a booking request is allowed to take. It is
// still bounded rather than unlimited, so a truncate blocked behind another session's lock
// fails with a clear timeout instead of hanging a test run.
const truncateStatementTimeout = 30 * time.Second

// Truncate empties every domain table. It exists so an integration test starts from a
// known world; it is never called by the service.
//
// It overrides the session's statement_timeout for the duration of the truncate, because
// the pool's session default comes from the caller's RequestBudget (see OpenPool) and some
// tests deliberately choose a tiny one to exercise timeout behaviour. Without this, such a
// test applies its own 200ms bound to its *setup*, and a loaded machine turns that into a
// flake: TRUNCATE ... CASCADE on six tables is occasionally slower than a bound chosen to
// make a lock wait trip.
//
// That is exactly what failed once in CI, in TestTransactionTimeoutsDoNotLeakAcrossTransactions
// of all places — the setup truncate timed out, not the property under test, which made a
// harness problem read as the leak the test exists to disprove.
func (r *Repo) Truncate(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: truncate: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// SET LOCAL scopes the override to this transaction, so the pooled connection is
	// returned with the session default intact — the same discipline the request path uses.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = '%dms'`,
		truncateStatementTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("postgres: truncate: set statement_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`TRUNCATE user_time_claims, user_identities, idempotency_records, bookings, reservations, slots RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("postgres: truncate: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: truncate: commit: %w", err)
	}
	return nil
}
