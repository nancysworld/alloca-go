package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/config"
)

// OpenPool builds a connection pool from a DSN and the request budget.
//
// The pool is where the budget stops being a document and starts constraining
// behaviour: DBAcquireCap bounds how long a request waits for a connection, and the
// session defaults below mean a connection is never handed out without the database
// bounds already set — a transaction that somehow skipped applyTimeouts still cannot
// wait forever on a lock.
//
// It verifies connectivity before returning, so a bad DSN or an unreachable database
// fails at startup rather than on the first request.
//
// The budget is validated here rather than assumed valid because OpenPool is exported
// and the failure mode of an unvalidated one is silent: LockTimeout and
// StatementTimeout are rendered into session settings, and PostgreSQL reads a 0ms
// timeout as "no timeout", so a zero-valued budget would produce a pool with the
// database bounds disabled — every lock wait unbounded, in the exact configuration the
// deadline chain is meant to rule out. A missing bound cannot be detected later from
// the outcome mix either: it looks like a request that simply took a long time.
func OpenPool(ctx context.Context, dsn string, budget config.RequestBudget) (*pgxpool.Pool, error) {
	if err := budget.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: invalid request budget: %w", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	// Session-level defaults. SET LOCAL inside each transaction still overrides these;
	// they are the floor for anything running outside one.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = fmt.Sprintf("%dms", budget.LockTimeout.Milliseconds())
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%dms", budget.StatementTimeout.Milliseconds())
	// All persisted and reported timestamps are UTC, so the session must not depend on
	// the server's configured zone.
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	return pool, nil
}

// connectTimeout bounds the startup connectivity check. It is separate from the
// per-request budget: this runs once, before the service serves traffic.
const connectTimeout = 10 * time.Second
