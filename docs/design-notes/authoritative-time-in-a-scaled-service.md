# Authoritative time in a horizontally scaled service

**Status:** Design note — revised recommendation for review before AG-M1 PR3  
**Scope:** clarify how Alloca-Go should use wall-clock time once multiple API instances
can execute transactions against the same PostgreSQL authority.

This note was prompted while reviewing the `Clock` port in
`internal/domain/ports.go`. The current AG-M1 service resolves `now` once from an
application-owned clock before entering the transaction and threads that value through
reserve, confirm, cancel, expiry, and idempotency decisions. That shape is deterministic
and easy to test, but a production fleet introduces two correctness concerns:

1. each API instance has its own wall clock, and those clocks can differ or be corrected
   independently;
2. a transaction may wait to acquire the slot lock, so a timestamp resolved before that
   wait can be stale at the point where the mutation is actually serialized.

This is not primarily a timestamp-sorting problem. The correctness-sensitive question is:
which clock, and which instant from that clock, decides release, expiry, and slot-close
boundaries when requests for the same slot can arrive through different API instances and
wait behind the same PostgreSQL row lock?

## 1. Problem

With one API instance, application timestamps are usually increasing, apart from clock
corrections such as a backwards step. With multiple instances, even well-synchronised
hosts can disagree by a small amount:

```text
API A: 10:00:00.120
API B: 10:00:00.085
```

If each instance independently evaluates:

```text
release_at <= now
now < starts_at
expires_at <= now
```

then two requests near the same boundary can receive different decisions solely because
they reached different API nodes.

Resolving `now` at transaction start is also insufficient. A transaction can begin and
then wait on `SELECT ... FOR UPDATE` until `lock_timeout`. During that wait:

- a held reservation may expire;
- a slot may cross `starts_at` and close;
- another serialized transaction may change the slot's state.

The authoritative timestamp must therefore describe the decision point after the relevant
slot lock is acquired, not merely the start of the request or transaction.

Wall-clock timestamps also do not provide a reliable global operation or commit order.
A transaction with an earlier `created_at` can commit after a transaction with a later
one, and a host-clock correction can move wall time backwards. Exact ordering must come
from transactional serialization, persisted state, versions, identifiers, and explicit
causal references rather than timestamp sorting.

## 2. Separate the uses of time

Alloca-Go should use different mechanisms for different jobs:

- **Process-local monotonic time** — request latency, deadline consumption, retry delay,
  and other elapsed-duration measurements. In Go, use `time.Since(start)` or equivalent
  APIs that retain the monotonic component of `time.Time`.
- **Authoritative transactional wall time** — release eligibility, slot closure, hold
  expiry, and transaction-owned timestamps whose value affects booking semantics.
- **Wall-clock timestamps for humans and telemetry** — approximate UTC time for logs,
  dashboards, audit display, and correlation; not a total-order mechanism.
- **Versions and explicit relationships** — mutation order and causality, such as a
  per-slot version, reservation-to-booking reference, idempotency record, request ID, or
  causation ID.

The practical rule is:

> Transactional authority and persisted state determine correctness; versions and
> explicit relationships determine ordering and causality; timestamps describe
> approximately when events happened.

## 3. Recommendation for Alloca-Go

Use this split in production:

```text
Application monotonic clock
    request latency
    local timeout accounting
    retry delays

PostgreSQL decision timestamp
    resolved after the relevant slot lock is acquired
    release eligibility
    slot closure
    hold expiry
    created_at / expires_at values owned by the attempt

Versions / IDs / relationships
    mutation ordering
    causal reconstruction
```

Because PostgreSQL is already the cross-node transactional and serialization authority,
it should also be the authoritative wall-clock source for correctness-sensitive mutation
decisions. This removes API-node clock skew from release, expiry, and close-window
semantics without introducing another distributed authority.

The normative semantic requirement proposed for PR3 is:

> Each transaction attempt resolves exactly one authoritative decision timestamp from
> PostgreSQL after acquiring the relevant slot lock, and uses that value consistently for
> the remainder of the attempt.

The value should be used for:

- settling holds that elapsed before or while the transaction waited for the lock;
- deciding whether the slot is released or closed at the serialization point;
- computing and validating `expires_at`;
- recording transaction-owned creation timestamps;
- constructing the idempotency record for the committed attempt.

`transaction_timestamp()` / `now()` is not suitable for this purpose because it returns
transaction-start time. A transaction may wait on the slot lock after that instant, making
the value stale when the decision is eventually serialized.

The preferred PR3 PostgreSQL shape is a `pgx.Batch` on the same transaction containing,
in order:

```text
1. SELECT ... FROM slots WHERE slot_id = $1 FOR UPDATE
2. SELECT clock_timestamp()
```

The server executes the statements in order, so the second statement is evaluated only
after the lock statement completes, including any row-lock wait. Sending both statements
as one batch avoids an additional client/server round trip while keeping the ordering
contract at the statement level rather than relying on target-list or planner evaluation
order. PR3 must still prove the post-lock property with an integration test; the test is
the acceptance gate, not the implementation assumption.

A single-statement form, such as a deliberately materialised CTE, is an acceptable fallback
only if the adapter cannot use a batch and the same post-lock evaluation property is
verified. The batch is preferred because its sequencing is easier to explain and review.

This choice gives one coherent wall-clock source, not a monotonic clock. PostgreSQL's wall
clock can still step backwards. Mutation order therefore continues to come from the slot
lock and persisted state; if an explicit per-slot order becomes necessary, use a version
or sequence rather than assuming decision timestamps are non-decreasing.

## 4. Port-shape implication

Authoritative time belongs to the transaction port because it is meaningful only after
the transaction has acquired the relevant authority lock:

```go
type Tx interface {
    LockSlot(ctx context.Context, id SlotID) (Slot, error)
    Now(ctx context.Context) (time.Time, error)
    // existing transactional methods...
}
```

A successful `LockSlot` establishes the attempt's authoritative timestamp as part of the
adapter operation: it acquires the slot lock, resolves the post-lock PostgreSQL timestamp,
and memoises that value. `Tx.Now()` returns the memoised value and must not lazily resolve
its own time source. Calling `Now()` before a successful `LockSlot` is an adapter/programming
error rather than an invitation to obtain pre-lock time accidentally.

A repository shape that passes `now` into the `WithinTx` closure is not suitable: it must
resolve the value before the closure can acquire the slot lock and therefore structurally
encodes the wrong decision point.

Failure to resolve or decode authoritative time is a system fault, never a business
refusal. The edge maps an underlying lock, statement, or deadline expiry to the applicable
`timeout_*` outcome; other authoritative-time failures map to `internal_failure`.

The service-level `Clock` dependency should be retired from the production mutation path
when PR3 adopts transaction-owned time. Leaving both `Service.clock.Now()` and `Tx.Now()`
visible in the same operation would create two competing sources of semantic time.
Deterministic tests should instead inject a manual clock into the in-memory transaction
adapter, which implements the same lock-establishes-time contract.

Three paths need explicit adapter treatment:

- `confirm` and `cancel` currently resolve `slot_id` from the reservation before locking
  the slot. The timestamp must be established by `LockSlot`, not by the earlier lookup.
- The `unknown_target` path may complete without locking a slot. It still needs one
  PostgreSQL-sourced attempt timestamp where a persisted outcome requires time; PR3 must
  define an explicit no-slot resolution path rather than pretending a slot lock occurred.
- The PR4 expiry/settlement worker performs the same `held -> expired` semantic transition
  outside an incoming request. Its `now` must come from its own PostgreSQL transaction by
  the same authority rule; it must not retain an API-host `Clock` as a second semantic time
  source.

## 5. Attempt semantics, TTL, and clock corrections

Authoritative `now` becomes **per transaction attempt**, not per incoming request. If the
service retries the whole operation after the idempotency-record insert-race backstop, the
new transaction obtains a new decision timestamp:

```text
request
  attempt 1 -> timestamp T1 -> rolls back
  attempt 2 -> timestamp T2 -> commits
```

Only the committed attempt's timestamp becomes durable. This is a semantic change from
the current implementation, where one service-clock value is captured outside the retry
loop and shared by both attempts.

For reserve, `expires_at = now + reservation_ttl` now measures the full TTL from the
post-lock decision point, not from request arrival. Time spent waiting for the slot lock
does not erode a successful hold's duration. Near `starts_at`, however, the later decision
time can make the full TTL no longer fit inside the slot window; that request must be
refused as `outside_window` rather than granted a shortened hold. Under contention,
lock-wait duration can therefore change the outcome mix as well as latency, which AG-M2
must account for when reading release-wave results.

Centralising wall time in PostgreSQL provides coherent decisions across API nodes; it does
not make the clock infallible. A PostgreSQL host clock correction can still move future
decision timestamps backwards. The state machine must therefore remain irreversible:
for example, an `expired` reservation must never become `held` again merely because wall
time moved backwards.

If stronger ordering is ever required, add an explicit version or sequence. Do not turn a
wall-clock assumption into a correctness invariant.

## 6. Operational assumptions and validation

Host clock synchronisation is still required for logs, metrics, tracing, TLS, and normal
operations, but correctness must not depend on API clocks being perfectly aligned. All
persisted and externally reported timestamps should use UTC.

PR3 and later tests should verify:

1. the PostgreSQL adapter resolves one timestamp per transaction attempt after acquiring
   the relevant slot lock;
2. `LockSlot` establishes and memoises that timestamp, and `Now()` cannot resolve time
   independently or succeed before the lock contract is satisfied;
3. a transaction that waits while a hold expires settles that hold using the post-wait
   timestamp;
4. a transaction that waits while the slot crosses `starts_at` cannot reserve after the
   booking window has closed;
5. a request whose TTL fit at arrival but no longer fits after lock wait is refused as
   `outside_window` and creates no reservation;
6. the same attempt timestamp is used for settlement, boundary decisions, expiry
   calculation, transaction-owned timestamps, and its idempotency record;
7. authoritative-time resolution errors map to `timeout_*` or `internal_failure`, never
   `business_refusal`;
8. the in-memory adapter can inject the equivalent timestamp deterministically;
9. concurrent requests routed through API instances with deliberately skewed host clocks
   cannot disagree because of API-host time;
10. the expiry/settlement worker uses PostgreSQL-owned time rather than an API-host clock;
11. reports do not claim commit or mutation order from timestamp ordering;
12. elapsed-time telemetry continues to use monotonic process-local duration measurement;
13. the `pgx.Batch` lock-then-time sequence demonstrably evaluates the decision timestamp
    after the row-lock wait rather than before it.

Useful negative controls include:

- run two API instances with deliberately skewed host clocks while PostgreSQL remains the
  semantic time source;
- force one transaction to wait on a slot lock while a hold expires or the slot closes;
- step the database wall clock backwards and verify that terminal state does not reverse,
  consumed-capacity reconciliation remains valid, and no expired reservation becomes
  live again.

## 7. Relationship to current AG-M1 documents

This note refines the current rule in `docs/design/transaction-semantics.md` §1.5. PR2's
reference implementation resolves `now` through the service-owned `Clock` port before
entering the transaction; that port shape is intentionally provisional for the production
PostgreSQL adapter.

If this recommendation is accepted, PR3 should update `transaction-semantics.md` §1.5,
`internal/domain/ports.go`, the service, and both repository adapters together, then prove
the choice with PostgreSQL integration tests. PR4 should apply the same authority rule to
the expiry/settlement worker.

At that point, `transaction-semantics.md` §1.5 becomes the normative owner. This note is
retained as historical rationale and is considered superseded rather than maintained as a
parallel normative specification.
