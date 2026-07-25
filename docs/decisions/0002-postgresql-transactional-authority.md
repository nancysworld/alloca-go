# 0002 — PostgreSQL as transactional authority

**Status:** Accepted (AG-M1 PR2 merged 2026-07-25; implemented in PR3)
**Date:** 2026-07-22
**Milestone:** AG-M1

## Context

Alloca-Go needs one authoritative owner for each slot's scarce capacity across multiple
stateless API instances. The authority must preserve the AG-M1 correctness gates under
concurrent reserve, confirm, cancel, and expiry operations:

- consumed capacity never exceeds the slot's capacity;
- reservation and booking state remain mutually consistent;
- one idempotency key cannot produce two logical mutations;
- cancellation, confirmation, and expiry races have one valid winner;
- ambiguous commit outcomes remain replay-safe.

An in-process mutex or router cannot provide cross-instance authority. A separate
lock service would split mutation state from its concurrency control and introduce a
second failure domain. Optimistic concurrency is viable for some workloads, but it
would make contention handling and retry ownership part of the initial correctness
surface before Alloca-Go has measured that trade-off.

The project already requires durable relational state and transaction-local
idempotency. PostgreSQL can therefore own both the mutation and the serialization
mechanism in one transaction.

AG-M1 also needs explicit implementation choices for database access and schema
migration. The runtime path requires PostgreSQL-specific transactions, error codes,
context cancellation, pool acquisition control, and pool telemetry. Migrations should
remain reviewable as plain SQL and deploy independently from serving traffic.

## Decision

Use **PostgreSQL as the transactional authority** for booking capacity and mutation
state.

For every capacity-changing operation, the PostgreSQL adapter will:

1. begin one database transaction;
2. resolve the target slot and acquire its row lock with
   `SELECT ... FOR UPDATE`;
3. evaluate the domain preconditions and capacity invariant while holding that lock;
4. write the reservation/booking mutation and its idempotency record in the same
   transaction;
5. commit before returning the authoritative terminal outcome.

The slot row is the per-slot serialization point even when no slot column changes.
Operations against different slots do not intentionally share this lock.

Use **`github.com/jackc/pgx/v5` with `pgxpool`** for runtime database access:

- native pgx transactions and PostgreSQL error classification;
- context-aware query, lock-wait, transaction, and pool-acquisition cancellation;
- explicit pool configuration and observable pool statistics;
- no ORM or database-portability abstraction in the authoritative adapter.

Use **`github.com/pressly/goose/v3` with embedded plain-SQL migrations** for schema
management:

- migrations live as ordered SQL files embedded with `embed.FS`;
- goose uses the pgx `database/sql` compatibility adapter only in the migration path;
- application repository queries continue to use native pgx/pgxpool APIs;
- migrations run through a dedicated command, deployment step, or test helper—not
  automatically in every API replica's normal startup path.

Readiness must not rely on pool construction alone. The service will establish and
verify database connectivity (for example with `Ping`) before reporting ready.

The detailed entity state machines, row-derived capacity accounting, outcome mapping,
and replay semantics remain governed by
[`../design/transaction-semantics.md`](../design/transaction-semantics.md). This ADR
records the durable authority and technology choices that implement those semantics.

## Consequences

**Enables**

- One atomic boundary for capacity state, mutation outcome, and idempotency record.
- Cross-instance correctness without an external distributed lock service.
- Deterministic one-winner behaviour for operations targeting the same slot.
- PostgreSQL-specific timeout and error classification needed by the measurement
  contract.
- Reviewable, reproducible schema changes packaged with the service source.
- Direct pool and transaction telemetry for AG-M2 capacity experiments.

**Costs / accepts**

- Mutations targeting one slot serialize at its row lock; a hot slot has a deliberate
  single-authority ceiling.
- The authoritative adapter is PostgreSQL-specific and is not portable to another
  database without a new adapter and evidence.
- Long transactions, inconsistent lock ordering, or work performed while holding the
  slot lock can reduce throughput or create deadlocks; transaction scope must remain
  minimal and lock ordering explicit.
- Row-derived capacity checks may cost more than denormalised counters; AG-M2 measures
  that cost before any optimisation is introduced.
- Schema migration is a separate operational step that must complete before code
  requiring the new schema receives traffic.
- The migration path uses `database/sql` compatibility while the runtime path uses
  native pgx; the separation must remain deliberate and tested.

**Rejects for AG-M1**

- in-process locks or routing as cross-node authority;
- Redis or another external lock service for booking correctness;
- ORM-managed schema and mutation semantics;
- automatic migration execution by every serving replica;
- unconstrained retries after transaction or commit ambiguity.

## Revisit when

Reopen this decision when repository-local evidence shows any of:

- row locking or row-derived capacity accounting prevents the required SLO-safe
  operating point and a tested alternative preserves every correctness gate;
- PostgreSQL cannot provide the required availability, scale, or deployment topology;
- a different authority boundary is introduced for shared-resource quantities in
  AG-M6;
- operational evidence shows the dedicated migration workflow is unsafe or
  insufficient;
- a supported non-PostgreSQL adapter becomes a real product requirement rather than a
  hypothetical portability goal.

A future ADR must supersede this one rather than rewriting the accepted historical
decision.
