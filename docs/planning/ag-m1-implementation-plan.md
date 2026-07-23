# AG-M1 implementation plan — Correct transactional core

**Status:** Agreed 22 July 2026
**Milestone dates:** 23–28 July 2026 · **Priority:** P0
**Roadmap:** [`alloca-go-roadmap.md`](alloca-go-roadmap.md) §AG-M1
**Governing contracts:** [`../design/measurement-contract.md`](../design/measurement-contract.md)
(outcome taxonomy §4, timeout budget §8/§8.1),
[`../design/project-structure.md`](../design/project-structure.md) (dependency rules),
[`../design/latency-timeouts-and-retries.md`](../design/latency-timeouts-and-retries.md)
(retry policy).

This document is the **plan of work** for AG-M1: how the milestone is split into
reviewable PRs and what each proves. It is not itself a normative contract — where a
design decision needs to bind later code, it is recorded in the milestone's design
doc (`transaction-semantics.md`, PR2) or a decision record (ADR-0002, PR2), not here.

---

## 1. Objective

Implement the smallest **correct authoritative** booking system before any
distributed-deployment concern (roadmap AG-M1). Correctness — capacity safety,
idempotency, and explicit outcome/timeout semantics — is the deliverable; capacity
and cost are later milestones.

## 2. Scope (from the roadmap)

Slot capacity and lifecycle; reservation hold / confirm / cancel / expiry; booking
create / cancel; idempotency scope and request hashing; PostgreSQL schema and
migrations; a transactional repository; an explicit aggregate lock strategy;
background expiry and settlement; per-request deadline configuration with fail-fast
§8.1 startup validation; health and readiness; structured outcome and timing
telemetry; runtime metadata (Go version, observed `GOMAXPROCS`).

Out of scope for AG-M1 (deferred to the named milestone): the external load system
and any capacity number (AG-M2); admission/queueing and the `retry_after` /
`admission_rejected` / `queue_position` outcomes (AG-M2+); AWS and the
load-balancer timeout (`timeout_lb`) leg of the chain (AG-M3); shared-resource
inventory quantities and conserved balances (AG-M6).

## 3. Design spine

The decisions that shape every PR. The authoritative form of §3.2–§3.4 lands in
`transaction-semantics.md` and ADR-0002 (both PR2); this is the summary the PR
split is built on.

Several of these decisions were sharpened by reusing the design lessons (not code —
it is Python) of the predecessor prototype: the domain model, invariants, schema
shape, expiry-settlement strategy, and fault-vs-refusal error taxonomy. All are
reframed synthetically; see [`../public-disclosure-policy.md`](../public-disclosure-policy.md).

### 3.1 Domain-model decisions (decided)

- **Single-unit holds.** A reservation holds **one** unit of a slot's capacity;
  capacity is an integer count, no per-reservation quantity field. Shared-resource
  **quantities and conserved balances are explicitly AG-M6**.
- **Minimal identity now.** Entities and mutations carry `organisation_id` (coarse
  authority / AG-M5 routing key) and `user_id` (participant / fairness). AG-M1 adds
  **no authentication** — identity exists to scope idempotency correctly (a client key
  must not be global across organisations/users) and to seed AG-M5, avoiding a later
  repaint.
- **Timed booking window.** A slot is a time window; its lifecycle derives from
  authoritative service time, not a mutable flag: reservable ⇔
  `release_at <= now < starts_at`. Holds satisfy `now < expires_at <= starts_at`, and
  capacity-returning cancellation is only valid before `starts_at`. The TTL is
  service-owned. (Organisation-level *synchronized release at scale* remains AG-M5.)

### 3.2 The slot row is the aggregate; lock it explicitly

Every scarce resource has exactly one write authority (roadmap thesis 3.1). For
booking that authority is the **slot**. Every capacity-changing operation —
`reserve`, `confirm`, `cancel`, `expire` — first takes a row lock on the slot
(`SELECT … FOR UPDATE`) inside its transaction, giving **single-writer
serialization per slot** within PostgreSQL. The capacity invariant
`held + confirmed ≤ capacity` is checked under that lock. This is the "explicit
aggregate lock strategy" the roadmap requires, and the basis of ADR-0002
(PostgreSQL as the transactional authority).

Consequence for race gates: because operations on one slot serialize, a
cancel/confirm race has **one winner** — the first transaction commits, the second
observes the updated state under the lock and returns an explicit domain answer
(§3.3), never a silent double-mutation.

### 3.3 Outcome taxonomy: terminal outcome **+** orthogonal `replay` flag

Outcomes are encoded exactly as measurement-contract §4 requires — a terminal
outcome **and** an orthogonal `replay` boolean, **never** a flat enum with
`idempotent_replay` as a peer. A replay returns the *originally recorded* terminal
outcome with `replay=true`; it never re-runs the mutation or resolves to a different
outcome.

Indicative AG-M1 mapping (authoritative version in `transaction-semantics.md`):

| Domain trigger | Terminal outcome | Notes |
|---|---|---|
| Reserve with capacity available | `admitted_success` | one held unit added under the slot lock |
| Reserve when `held + confirmed == capacity` | `business_refusal` | sold out — a valid domain answer, not a failure |
| Confirm a valid held reservation | `admitted_success` | held → confirmed |
| Confirm an expired / cancelled / already-confirmed reservation | `business_refusal` | lost the race or acted on a dead hold |
| Cancel a held / confirmed reservation (before start) | `admitted_success` | releases the unit |
| Cancel an already-terminal reservation (different key) | `business_refusal` | nothing to release |
| Well-formed request for an unknown slot/reservation/booking | `business_refusal` (`unknown_target`) | a valid domain "no", not a fault |
| Same idempotency key, different request hash | `business_refusal` (`idempotency_conflict`) | key not repurposed |
| Malformed body / missing field / missing idempotency key | `invalid_request` | rejected before the domain path (measurement-contract §4, added in AG-M1) |
| Internal invariant/accounting violation | `internal_failure` | a fault, never a refusal |
| Lock wait exceeds `lock_timeout` | `timeout_db` | innermost bound fires first (§8) |
| Statement exceeds `statement_timeout` | `timeout_db` | |
| Server request context deadline exceeded | `timeout_server` | |
| Client disconnects / cancels the request | `timeout_client` | |
| Commit acknowledgement lost / ambiguous | `unknown_replayable` | must be replay-safe with the same key |
| Unexpected fault | `internal_failure` | |
| Any prior key replayed | *recorded outcome* + `replay=true` | returns the original, never re-mutates |

`retry_after`, `admission_rejected`, `queue_position` (admission) and `timeout_lb`
(AWS) are defined in the contract but produced only from AG-M2/AG-M3 onward.

### 3.4 Idempotency is domain-local

An idempotency record — key scope, request hash, and the recorded terminal outcome —
is written **in the same transaction as the mutation**. A replayed key returns the
recorded outcome (`replay=true`); a replay after a lost response returns the original
outcome; an `unknown_replayable` result is resolved by replaying the *same* key,
never by issuing a new mutation. There is **no shared idempotency service**;
idempotency stays with the mutation owner (project-structure §8, AG-M0 review
obligation).

### 3.5 Dependency direction (project-structure §4)

The domain owns its interfaces (`Repository`, `Clock`, `IDGen`), expressed only in
domain types; `postgres` implements them; `service` and `httpapi` depend on the
domain interfaces, never on `postgres`. No transport (HTTP) or persistence (SQL,
`pgx`) type leaks into `domain` or `service`. A change that would need a prohibited
import is an ADR trigger, not a quiet exception.

## 4. PR split

Four PRs, one open at a time (per the agreed workflow). Each answers the four PR
questions and is independently reviewable.

| PR | Title | Delivers | DB? | Status |
|---|---|---|---|---|
| 1 | Timeout budget + §8.1 startup validation | `config.RequestBudget`, `config.Validate()`, `/meta` exposure, `.gitignore __debug_bin*` | no | **Merged #3** |
| 2 | Domain core + services + idempotency | `internal/domain` (entities, invariants, ports, outcome types), `internal/idempotency`, `internal/service`, in-memory repository, `transaction-semantics.md`, ADR-0002, measurement-contract §4 `invalid_request` amendment; full unit + race tests | no | **Draft #4** |
| 3 | PostgreSQL adapter | `internal/postgres` (transactional repo, `SELECT … FOR UPDATE`, per-txn `lock_timeout`/`statement_timeout`, error→outcome mapping), schema + migrations, CI Postgres integration tests | yes | planned |
| 4 | Expiry worker + HTTP API + telemetry | `internal/worker` (expiry that cannot release confirmed capacity), `internal/httpapi` booking endpoints (idempotency-key handling, outcome→HTTP mapping), structured outcome/timing telemetry, `cmd` wiring, readiness gated on DB, e2e tests | yes | planned |

**Why this order.** PR1 is DB-free and discharges the §8.1 obligation, giving later
PRs a validated budget. PR2 proves every correctness gate expressible above the SQL
layer against an in-memory repository (which remains a permanent test double). PR3 is
where SQL-level serialization is actually proven — "capacity never exceeded" under
real concurrent transactions needs a real PostgreSQL. PR4 closes the loop with the
worker, transport, and telemetry once the authoritative repository exists.

## 5. Correctness gates → where proven

The roadmap's AG-M1 gates and the layer that establishes each:

| Correctness gate | Primarily proven in |
|---|---|
| Capacity is never exceeded | PR3 (real concurrent `FOR UPDATE` transactions); PR2 for the invariant logic |
| Reserved/confirmed counts consistent with reservation/booking rows | PR3 (reconciliation under lock); PR2 for the state model |
| One idempotency key cannot produce two logical mutations | PR2 (logic) + PR3 (same-transaction record under concurrency) |
| Replay after a lost response returns the original outcome | PR2 (replay resolution) + PR3 (durable record) |
| Expiry cannot release confirmed capacity | PR2 (expiry policy) + PR4 (worker) |
| Cancellation/confirmation races have one valid winner | PR3 (serialized under the slot lock) |
| Timed-out and unknown-outcome transactions accounted for explicitly | PR3 (DB error→outcome mapping) + PR4 (telemetry) |

The **required semantics** (roadmap: distinguish successful mutation, business
refusal, conflict, client cancellation, server deadline, DB timeout, lost response
after commit, unknown commit outcome, permanent failure, and idempotent replay) are
realised by the §3.3 taxonomy and validated across PR2–PR4.

## 6. Tooling decisions (recorded — ADR-0002)

Recorded (status **Proposed**) in
[`../decisions/0002-postgresql-transactional-authority.md`](../decisions/0002-postgresql-transactional-authority.md)
(landed as a doc in PR2; Accepted when PR2 merges):

- **Migrations:** `pressly/goose` with embedded plain-SQL migrations, run through a
  dedicated command/step — not automatically by every serving replica.
- **Driver / pool:** `pgx v5` + `pgxpool` (native transactions, error classification,
  context-aware cancellation, observable pool stats; no ORM in the authoritative
  adapter).

Both are exercised at PR3 (the adapter and migrations); the decision itself is
recorded now alongside the transaction semantics it implements.

## 7. Evidence and disclosure discipline

No AG-M1 PR introduces a `[MEASURED]` capacity, latency, or cost number — those are
AG-M2+. Timeout and SLO values remain `[HYPOTHESIS]` (measurement-contract §7–§8).
All examples are synthetic; the plan records no confidential, private, recruitment,
or design-prompt-origin content (see
[`../public-disclosure-policy.md`](../public-disclosure-policy.md)).
