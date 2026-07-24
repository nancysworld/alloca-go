# Authoritative time in a horizontally scaled service

**Status:** Design note — recommendation for review before AG-M1 PR3
**Scope:** clarify how Alloca-Go should use wall-clock time once multiple API instances
can execute transactions against the same PostgreSQL authority.

This note was prompted while reviewing the `Clock` port in
`internal/domain/ports.go`. The current AG-M1 service resolves `now` once from an
application-owned clock and threads that value through reserve, confirm, cancel, expiry,
and idempotency decisions. That shape is deterministic and easy to test, but a production
fleet introduces a second concern: each API instance has its own wall clock, and those
clocks can differ or be corrected independently.

This is not primarily a timestamp-sorting problem. The correctness-sensitive question is
which clock decides release, expiry, and slot-close boundaries when requests for the same
slot can arrive through different API instances.

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

Wall-clock timestamps also do not provide a reliable global operation or commit order.
A transaction with an earlier `created_at` can commit after a transaction with a later
one. Exact ordering should therefore come from transactional serialization, versions,
identifiers, and explicit causal references rather than timestamp sorting.

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

> Versions and transactional authority determine correctness and ordering; timestamps
> describe approximately when events happened.

## 3. Recommendation for Alloca-Go

Use this split in production:

```text
Application monotonic clock
    request latency
    local timeout accounting
    retry delays

PostgreSQL transaction timestamp
    release eligibility
    slot closure
    hold expiry
    created_at / expires_at values owned by the transaction

Versions / IDs / relationships
    mutation ordering
    causal reconstruction
```

Because PostgreSQL is already the cross-node transactional authority, it should also be
the authoritative wall-clock source for correctness-sensitive mutation decisions. This
removes API-node clock skew from release, expiry, and close-window semantics.

The production PostgreSQL adapter should resolve one timestamp inside the transaction,
using PostgreSQL's transaction-stable time (`transaction_timestamp()`, equivalent to
`now()`), and supply that value to the domain operation. The same value should be used
throughout that transaction for:

- settling elapsed holds;
- deciding whether the slot is released or closed;
- computing and validating `expires_at`;
- recording transaction-owned creation timestamps;
- constructing the idempotency record.

`transaction_timestamp()` is preferred over repeatedly reading `clock_timestamp()`
because Alloca-Go's semantic contract already says that `now` is resolved once and
threaded through the decision path. A stable transaction timestamp matches that model.

This does **not** imply that transaction timestamps define commit order. A transaction
can begin earlier and commit later. Where an exact order is required, use the slot lock
and an explicit version or sequence associated with the relevant authority.

## 4. Port-shape implication

The current domain-owned `Clock` port remains useful for deterministic tests, but the
production source of authoritative mutation time should not be the API process wall
clock.

Before PR3 fixes the PostgreSQL adapter contract, consider moving authoritative time into
the transaction boundary. Two possible shapes are:

```go
type Tx interface {
    Now(ctx context.Context) (time.Time, error)
    // existing transactional methods...
}
```

or:

```go
type Repository interface {
    WithinTx(
        ctx context.Context,
        fn func(ctx context.Context, tx Tx, now time.Time) error,
    ) error
}
```

The PostgreSQL adapter would obtain `now` from `transaction_timestamp()`. The in-memory
reference adapter would obtain it from an injected manual clock, preserving deterministic
unit and race tests.

The exact interface is still a design choice for PR3. The normative requirement is:

> One transaction uses one authoritative timestamp, and all horizontally scaled API
> instances obtain that timestamp from the same transactional authority for
> correctness-sensitive decisions.

## 5. Operational assumptions and validation

Host clock synchronisation is still required for logs, metrics, tracing, TLS, and normal
operations, but correctness must not depend on clocks being perfectly aligned. All
persisted and externally reported timestamps should use UTC.

PR3 and later tests should verify:

1. the PostgreSQL adapter resolves one stable timestamp per transaction;
2. release, expiry, and close-window decisions use that timestamp rather than an API-node
   wall clock;
3. the in-memory adapter can inject the equivalent timestamp deterministically;
4. concurrent requests routed through different API instances cannot disagree because
   of host-clock skew;
5. reports do not claim commit order from `created_at` ordering;
6. elapsed-time telemetry continues to use monotonic process-local duration measurement.

A useful fault/negative-control test is to run two API instances with deliberately skewed
host clocks while both use PostgreSQL transaction time. Domain outcomes near release and
expiry boundaries should remain consistent.

## 6. Relationship to current AG-M1 documents

This note does not change the existing semantic rule in
`docs/design/transaction-semantics.md` that `now` is resolved once at a trusted service
boundary and is never supplied by the client. It sharpens what that trusted boundary
should be in a horizontally scaled production deployment: the PostgreSQL transaction,
not an individual API host.

If accepted, PR3 should update the normative transaction semantics and the domain port
shape together, then prove the choice with PostgreSQL integration tests. Until that
change is accepted, this remains a design recommendation rather than an implemented
claim.
