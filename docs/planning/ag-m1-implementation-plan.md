# AG-M1 implementation plan — Correct transactional core

**Status:** Historical — AG-M1 completed 30 July 2026. Retained as the record of how the milestone
was planned and split; not normative for any current work.
**Milestone dates:** 23–28 July 2026 · **Priority:** P0

**Reading the roadmap references below.** This plan was written against the pre-2026-08-10
roadmap, which was a milestone master plan carrying theses, scope, gates and an AG-M0–M7 schedule.
That document has since been rewritten as an **exploration roadmap**
([`alloca-go-roadmap.md`](alloca-go-roadmap.md)) and no longer contains the sections cited here.
The durable content moved to its owners: correctness gates and the authority model to
[`../design/transaction-semantics.md`](../design/transaction-semantics.md), measurement vocabulary
and outcome taxonomy to [`../design/measurement-contract.md`](../design/measurement-contract.md),
and the monolith-first decision to
[`../decisions/0001-modular-monolith-first.md`](../decisions/0001-modular-monolith-first.md). The
references are left as written because this is a dated record of what the work was planned
against.
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
it is Python) of the RuntimeIQ-Alloca predecessor prototype
([high-level design](../design/high-level-design.md) §1.1): the domain model,
invariants, schema shape, expiry-settlement strategy, and fault-vs-refusal error
taxonomy. All are reframed synthetically; see
[`../public-disclosure-policy.md`](../public-disclosure-policy.md).

### 3.1 Domain-model decisions (decided)

- **Single-unit holds.** A reservation holds **one** unit of a slot's capacity;
  capacity is an integer count, no per-reservation quantity field. Shared-resource
  **quantities and conserved balances are explicitly AG-M6**.
- **Minimal identity now.** Entities and mutations carry the organisation dimension
  (coarse authority / AG-M5 routing key) and `user_id` (participant / fairness). Both
  identities are pairs and each names its organisation's role —
  `(user_organisation_id, user_id)` and `(slot_organisation_id, slot_id)` — because the
  two differ whenever a user books into another organisation. AG-M1 adds
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

Five PRs, one open at a time (per the agreed workflow). Each answers the four PR
questions and is independently reviewable. The split began as four; PR4 was inserted
during the milestone for the reason recorded below.

| PR | Title | Delivers | DB? | Status |
|---|---|---|---|---|
| 1 | Timeout budget + §8.1 startup validation | `config.RequestBudget`, `config.Validate()`, `/meta` exposure, `.gitignore __debug_bin*` | no | **Merged #3** |
| 2 | Domain core + services + idempotency | `internal/domain` (entities, invariants, ports, outcome types), `internal/idempotency`, `internal/service`, in-memory repository, `transaction-semantics.md`, ADR-0002, measurement-contract §4 `invalid_request` amendment; full unit + race tests | no | **Merged #4** |
| 3 | PostgreSQL adapter | `internal/postgres` (transactional repo, `SELECT … FOR UPDATE`, per-txn `lock_timeout`/`statement_timeout`, error→outcome mapping), schema + migrations, transaction-owned authoritative time (`Tx.Now`, retiring the service `Clock`), `cmd/alloca-migrate`, CI Postgres integration tests | yes | **Merged #5** |
| 4 | User schedule non-overlap invariant + composite slot identity | `user_time_claims` relation (`btree_gist`, exclusion constraint), identity-scoped claim settlement, `ReasonScheduleConflict`, claim lifecycle across reserve/confirm/cancel/expiry; slot identity becomes `(slot_organisation_id, slot_id)` with `domain.SlotRef`/`domain.UserRef` and `contract_version` v2; schema consolidated into the `00001_init.sql` baseline; transaction-semantics §1.1/§1.2/§2.2/§4; PostgreSQL gates + negative controls | yes | **Merged #6** |
| 5 | Expiry worker + HTTP API + telemetry | `internal/worker` (expiry that cannot release confirmed capacity), `internal/httpapi` booking endpoints + informational `GET /v1/slots` (idempotency-key handling, total outcome→HTTP mapping), `internal/telemetry` observation boundary, `internal/ids`, `Service.SettleSlot`, `cmd` wiring, readiness gated on the DB and bounded by `ReadinessTimeout`; [`api-surface.md`](../design/api-surface.md) and [`observability.md`](../design/observability.md); vertical PostgreSQL-backed HTTP tests | yes | **In review #7** |

**Why this order.** PR1 is DB-free and discharges the §8.1 obligation, giving later
PRs a validated budget. PR2 proves every correctness gate expressible above the SQL
layer against an in-memory repository (which remains a permanent test double). PR3 is
where SQL-level serialization is actually proven — "capacity never exceeded" under
real concurrent transactions needs a real PostgreSQL. PR5 closes the loop with the
worker, transport, and telemetry once the authoritative repository exists.

**Why PR4 was inserted.** The user schedule non-overlap invariant
([design note](../design-notes/user-schedule-non-overlap.md)) was identified after this
plan was written. It is a *correctness* invariant of the transactional core: one
identity can currently hold two overlapping bookings on different slots, because those
transactions lock different slot rows and never contend. Shipping AG-M1 — the milestone
whose name is "correct transactional core" — with that gap would overstate what the
milestone proved, so it lands before the worker/API/telemetry PR rather than after.

PR4 also corrects the **slot's** identity to `(slot_organisation_id, slot_id)`. The two
belong together: both are the same correction — an identity is a pair scoped to an
organisation — and which organisation applies depends on whether the thing is a user or
a slot. A cross-organisation booking has different values in each, so the schedule
claim carries both. Keying slots by `slot_id` alone assumed identifiers are unique
across organisations, which nothing establishes; since the slot row *is* the aggregate
lock, that assumption was a correctness one.

Doing it here rather than at AG-M5 is deliberate: it changes the aggregate lock's
resolution path, which is exactly what AG-M2 measures the frontier of and AG-M4 builds
the capacity-unit economics on. Changing it later would invalidate those measurements.

Because both corrections change the schema and AG-M1 has no deployed database, PR4 also
**consolidates the schema into the `00001_init.sql` baseline** rather than layering
transitional migrations on top of it. Migration compatibility begins at that merged
baseline; every change after it is forward-only and numbered. The first public schema
therefore states the model the project believes rather than preserving the record of
correcting it during review — and the transitional machinery it would otherwise carry
(a composite-key rewrite, a column backfill) disappears with it.

Three consequences are accepted deliberately: AG-M1 extends beyond its original 28 July
date; `contract_version` moves to `v2`, invalidating any stored idempotency record
(there is no production data); and **AG-M2's frontier baseline must be measured after
PR4**, since PR4 adds a user-keyed contention domain to the write path.

## 5. Correctness gates → where proven

The roadmap's AG-M1 gates and the layer that establishes each:

| Correctness gate | Primarily proven in |
|---|---|
| Capacity is never exceeded | PR3 (real concurrent `FOR UPDATE` transactions); PR2 for the invariant logic |
| Reserved/confirmed counts consistent with reservation/booking rows | PR3 (reconciliation under lock); PR2 for the state model |
| One idempotency key cannot produce two logical mutations | PR2 (logic) + PR3 (same-transaction record under concurrency) |
| Replay after a lost response returns the original outcome | PR2 (replay resolution) + PR3 (durable record) |
| Expiry cannot release confirmed capacity | PR2 (expiry policy) + PR5 (worker) |
| Cancellation/confirmation races have one valid winner | PR3 (serialized under the slot lock) |
| One identity cannot hold two overlapping active claims | PR4 (exclusion constraint under real concurrent transactions, with negative controls) |
| Two organisations may own same-named slots without sharing capacity or a lock | PR4 (composite slot key; the gates cannot even set up under the old single-column key) |
| Timed-out and unknown-outcome transactions accounted for explicitly | PR3 (DB error→outcome mapping) + PR5 (telemetry) |

The **required semantics** (roadmap: distinguish successful mutation, business
refusal, conflict, client cancellation, server deadline, DB timeout, lost response
after commit, unknown commit outcome, permanent failure, and idempotent replay) are
realised by the §3.3 taxonomy and validated across PR2–PR5.

## 6. Tooling decisions (recorded — ADR-0002)

Recorded (status **Accepted** on PR2's merge, 2026-07-25) in
[`../decisions/0002-postgresql-transactional-authority.md`](../decisions/0002-postgresql-transactional-authority.md):

- **Migrations:** `pressly/goose` with embedded plain-SQL migrations, run through a
  dedicated command/step — not automatically by every serving replica.
- **Driver / pool:** `pgx v5` + `pgxpool` (native transactions, error classification,
  context-aware cancellation, observable pool stats; no ORM in the authoritative
  adapter).

Both are exercised at PR3 (the adapter and migrations). Migrations run through
`cmd/alloca-migrate`, a separate binary: serving replicas never migrate on startup, so
a rollout cannot have several replicas racing the same DDL.

## 7. Evidence and disclosure discipline

No AG-M1 PR introduces a `[MEASURED]` capacity, latency, or cost number — those are
AG-M2+. Timeout and SLO values remain `[HYPOTHESIS]` (measurement-contract §7–§8).
All examples are synthetic; the plan records no confidential, private, recruitment,
or design-prompt-origin content (see
[`../public-disclosure-policy.md`](../public-disclosure-policy.md)).
