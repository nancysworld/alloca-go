// Package postgres is the authoritative implementation of the domain's persistence
// ports (domain.Repository and domain.Tx) against PostgreSQL, which ADR-0002 records
// as Alloca-Go's transactional authority.
//
// Three things live here that cannot be proven above the SQL layer, and they are the
// reason this package exists rather than the in-memory reference being enough:
//
//   - Cross-node serialization. The slot row's SELECT … FOR UPDATE is the per-slot
//     mutex (transaction-semantics §2). Two transactions targeting one slot serialize;
//     transactions on different slots do not contend, which is the dispersed-authority
//     property AG-M2/AG-M4 measure.
//   - Authoritative time. The attempt's decision timestamp comes from PostgreSQL's
//     clock_timestamp(), resolved after the row-lock wait, so API-host clock skew
//     cannot change a release, closure, or expiry decision (§1.5).
//   - Layered deadlines. Each transaction sets lock_timeout and statement_timeout from
//     the validated budget, so the innermost responsible bound fires first and yields
//     a distinct classified outcome instead of a generic outer timeout (§6).
//
// Dependency rule (project-structure §4): this package implements interfaces the
// domain owns and imports the domain; nothing flows the other way, and no pgx type
// appears in a domain or service signature.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
)

// Repo is the PostgreSQL-backed domain.Repository.
type Repo struct {
	pool   *pgxpool.Pool
	budget config.RequestBudget
}

// New constructs a Repo over an existing pool. The budget supplies the per-transaction
// lock_timeout, statement_timeout, and transaction budget; it is assumed to have passed
// config.Load's §8.1 ordering validation, which is what makes "the innermost bound
// fires first" true rather than hoped for.
func New(pool *pgxpool.Pool, budget config.RequestBudget) *Repo {
	return &Repo{pool: pool, budget: budget}
}

// WithinTx runs fn inside one transaction, committing when fn returns nil and rolling
// back otherwise.
//
// The transaction is bounded three ways, from the inside out (§6): lock_timeout bounds
// each lock acquisition, statement_timeout bounds each statement, and the derived
// context bounds the transaction as a whole. SET LOCAL scopes the first two to this
// transaction, so a pooled connection never leaks a timeout into the next borrower.
//
// Deadline classification is the subtle part. When the transaction-scoped deadline
// fires but the caller's context is still live, the database layer was the binding
// bound, so the error is reported as domain.ErrDBTimeout (timeout_db). When the
// caller's own context is done, that error is returned unwrapped so the edge can tell
// a server deadline from a client disconnect. Collapsing both into one error would
// lose exactly the distinction the outcome taxonomy exists to record.
func (r *Repo) WithinTx(ctx context.Context, fn func(ctx context.Context, tx domain.Tx) error) (err error) {
	// Acquisition is bounded separately and sits outside the transaction budget: a
	// request that never got a connection did no database work (§6, §4 note).
	acquireCtx, cancelAcquire := context.WithTimeout(ctx, r.budget.DBAcquireCap)
	conn, err := r.pool.Acquire(acquireCtx)
	cancelAcquire()
	if err != nil {
		return r.classifyAcquire(ctx, err)
	}
	defer conn.Release()

	txCtx, cancel := context.WithTimeout(ctx, r.budget.TxnBudget)
	defer cancel()

	pgxTx, err := conn.BeginTx(txCtx, pgx.TxOptions{})
	if err != nil {
		return r.classify(ctx, txCtx, err)
	}
	// Rollback is idempotent after a successful commit (pgx returns ErrTxClosed), so
	// this is safe on every path and guarantees no transaction is left open by a panic
	// or an early return. It runs on its own cleanup bound (see cleanupContext).
	defer func() {
		cleanupCtx, cancelCleanup := r.cleanupContext(txCtx)
		defer cancelCleanup()
		_ = pgxTx.Rollback(cleanupCtx)
	}()

	if err := r.applyTimeouts(txCtx, pgxTx); err != nil {
		return r.classify(ctx, txCtx, err)
	}

	t := &tx{conn: pgxTx}
	if err := fn(txCtx, t); err != nil {
		return r.classify(ctx, txCtx, err)
	}

	if err := pgxTx.Commit(txCtx); err != nil {
		return r.classifyCommit(ctx, txCtx, err)
	}
	return nil
}

// cleanupContext derives the context used for a transaction's rollback. Cleanup has to
// outlive the reason the transaction is being abandoned — a transaction the caller
// cancelled or that blew its budget still has to be closed, and a rollback issued on
// the expired context would be refused before it reached PostgreSQL, leaving the
// transaction open until the connection is destroyed.
//
// But detaching from cancellation removes the *deadline* along with it, and an
// unbounded rollback is its own failure: if the connection is stalled, the deferred
// Rollback blocks forever, the connection is never released, and the timed-out request
// never returns. Repeat that under load and the pool drains — a per-request database
// stall escalates into a whole-service outage, which is precisely the failure the
// layered budget (§6) exists to prevent.
//
// So cleanup is detached from the caller *and* independently bounded, by
// StatementTimeout: a rollback is a statement, and it is the same bound the session
// applies to every other statement in the transaction. When it expires, pgx marks the
// connection unusable and Release discards it rather than returning a connection with
// an open transaction to the pool.
func (r *Repo) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.budget.StatementTimeout)
}

// applyTimeouts sets the per-transaction database bounds. SET LOCAL cannot be
// parameterised, so the values are formatted in; they are integer milliseconds from a
// validated config, never caller input.
func (r *Repo) applyTimeouts(ctx context.Context, pgxTx pgx.Tx) error {
	stmt := fmt.Sprintf(
		"SET LOCAL lock_timeout = '%dms'; SET LOCAL statement_timeout = '%dms'",
		r.budget.LockTimeout.Milliseconds(),
		r.budget.StatementTimeout.Milliseconds(),
	)
	if _, err := pgxTx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("postgres: set transaction timeouts: %w", err)
	}
	return nil
}

// classifyAcquire maps a pool-acquisition failure. An over-budget acquisition is
// timeout_server for AG-M1 (transaction-semantics §4 note); AG-M2 may reclassify it as
// retry_after once admission exists.
func (r *Repo) classifyAcquire(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("postgres: acquire connection: %w", ctx.Err())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("postgres: acquire connection exceeded db_acquire_cap: %w", context.DeadlineExceeded)
	}
	return fmt.Errorf("postgres: acquire connection: %w", err)
}

// classify maps an error raised inside the transaction to the vocabulary the edge
// classifies. Domain sentinels raised by the tx methods pass through untouched: a
// not-found or a scoped-key conflict is a domain answer, not an infrastructure fault.
func (r *Repo) classify(ctx context.Context, txCtx context.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrConflict),
		errors.Is(err, domain.ErrTimeNotEstablished), errors.Is(err, domain.ErrDBTimeout):
		return err
	case ctx.Err() != nil:
		// The caller's own deadline or cancellation. Returned unwrapped so the edge can
		// distinguish timeout_server from timeout_client.
		return fmt.Errorf("postgres: %w", ctx.Err())
	case txCtx.Err() != nil:
		// The transaction budget alone expired: the database layer was the binding
		// bound even though no single statement tripped its own timeout.
		return fmt.Errorf("postgres: transaction exceeded txn budget: %w", domain.ErrDBTimeout)
	default:
		return err
	}
}

// classifyCommit maps a commit failure, where the distinction between "definitely did
// not commit" and "unknown" is the one that matters.
//
// A *pgconn.PgError means the server processed the COMMIT and rejected it, so the
// transaction definitely did not commit — a definite fault, not an ambiguous one. Any
// other failure (a broken connection, a deadline firing while the acknowledgement was
// in flight) leaves the outcome genuinely unknown: the commit may well have been
// applied and only the answer lost. That case is ErrCommitUnknown, which the client
// resolves by replaying the same idempotency key (§5.4).
//
// Reporting an ambiguous commit as a definite failure would be the more dangerous
// error of the two: a client told "failed" may reasonably reissue with a new key, and
// a new key is exactly what breaks the one-key-one-mutation gate.
//
// Within the definite branch, a timeout SQLSTATE is still a timeout: COMMIT is subject
// to the session's statement_timeout like any other statement, and when it trips the
// database layer was the binding bound. Returning that raw would classify the request
// as internal_failure and lose a timeout_db from the outcome mix exactly when commit
// processing exceeded its database bound (§6).
func (r *Repo) classifyCommit(ctx context.Context, txCtx context.Context, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.LockNotAvailable:
			return fmt.Errorf("postgres: commit exceeded lock_timeout: %w", domain.ErrDBTimeout)
		case pgerrcode.QueryCanceled:
			// The server answered, so the transaction definitely did not commit — this is
			// not the ambiguous case below. As in mapError, 57014 covers both
			// statement_timeout expiry and a cancellation the server honoured, and only
			// the former is timeout_db; a caller who cancelled keeps timeout_client /
			// timeout_server.
			if ctx.Err() != nil {
				return fmt.Errorf("postgres: commit cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("postgres: commit exceeded statement_timeout: %w", domain.ErrDBTimeout)
		}
		return fmt.Errorf("postgres: commit rejected (%s): %w", pgErr.Code, err)
	}
	if ctx.Err() != nil || txCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("postgres: commit acknowledgement lost: %w", domain.ErrCommitUnknown)
	}
	return fmt.Errorf("postgres: commit failed, outcome unknown: %w: %w", domain.ErrCommitUnknown, err)
}

// Static assertion that Repo satisfies the domain port.
var _ domain.Repository = (*Repo)(nil)
