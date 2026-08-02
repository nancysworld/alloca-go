# Technical debt register

Deliberate gaps: behaviour that is correct today *under a stated condition*, recorded
together with the condition, so the day it stops holding is a lookup rather than a
rediscovery.

**This document owns the register, not the designs.** Every entry links to the document
that owns the affected behaviour; where they disagree, that document wins. An entry here
never restates a design decision — it records what the decision costs and when the bill
arrives.

## 1. What earns an entry

Three things must hold together:

1. **No invariant is broken.** A debt is not a bug. If a property in the invariant register
   ([`../design/transaction-semantics.md`](../design/transaction-semantics.md) Appendix A)
   fails, that is a defect and goes to the issue tracker.
2. **The decision was right when it was made.** Debt is the residue of a sound trade-off,
   not of an oversight. The entry says what was traded and for what.
3. **There is a trigger.** A stated condition under which the trade-off stops being
   acceptable. Without one an entry is a worry, not a debt, and it will sit here forever
   accumulating nothing but agreement.

Work not yet started belongs in [`alloca-go-roadmap.md`](alloca-go-roadmap.md); a plan for
splitting a milestone belongs in that milestone's plan. Debt is the third thing: a design
that is sound inside a boundary, with the boundary written down.

IDs follow the invariant convention — `DEBT-1`, stable, never reused or renumbered, so a
code comment or a PR description can cite one and still be right in a year.

## 2. Index

| ID | Debt | Area | Raised | Status |
|---|---|---|---|---|
| [**DEBT-1**](#3-debt-1--no-reaper-for-abandoned-schedule-claims) | Elapsed `user_time_claims` rows are reclaimed only by their owner's next reserve, so an identity that never returns leaves its row indefinitely | schedule claims | 2026-08-02, AG-Sept PR1 | open |
| [**DEBT-2**](#4-debt-2--no-retention-policy-for-durable-rows) | No durable row is ever deleted and no retention policy exists; `idempotency_records` is the instance with no product reason to keep it | data lifecycle | 2026-08-02, AG-Sept PR1 | open |

## 3. DEBT-1 — no reaper for abandoned schedule claims

### What it is

A held reservation whose TTL lapses is expired by the expiry worker: the reservation row
moves to `expired` and its capacity returns to the slot. The `user_time_claims` row backing
it is **not** removed at that moment, and nothing removes it on a timer. Exactly two
statements delete a claim:

| Statement | Trigger | Scope |
|---|---|---|
| `SettleClaims` (`internal/postgres/tx.go:401`) | the **owning user's** next reserve (`internal/service/service.go:137`) | that user's elapsed claims |
| `DeleteClaim` (`internal/postgres/tx.go:386`) | cancellation, and reserve withdrawing a claim it provisionally inserted | one reservation's claim |

Neither is time-driven, and the claim → reservation foreign key has no `ON DELETE CASCADE`
(expiry is an `UPDATE` in any case, so there would be nothing to cascade). An identity that
reserves once, abandons the hold, and never comes back therefore leaves a permanent row.
Growth is proportional to abandoned holds by non-returning identities, and unbounded in
time.

### Why it is this way

Not an oversight — it falls out of the lock discipline, and the schema comments say so.

No transaction may write claim rows for a user it is not acting for
(`internal/domain/ports.go:188`), because the lock order is slot → user identity → claims
(**INV-10**) and the identity lock is what keeps one user's concurrent claim inserts off the
GiST index, where they would otherwise each wait on the other's uncommitted tuple and
deadlock. Slot-scoped expiry never touching claims is not incidental either: it is
**INV-11**, with a negative control
(`postgres/schedule_test.go` — `TestNegativeControlElapsedClaimSurvivesUntilSettled`).

So the worker cannot reap claims *as currently shaped*: it locks slots, and a claim belongs
to an identity. The lazy reclaim is the price of never taking an identity lock on a user's
behalf without acting for them.

### Why it is acceptable today

A stale claim can only block the identity that owns it — the exclusion constraint is keyed
by user — and that identity's next reserve settles it before preconditions are evaluated.
No other user is affected, no capacity is withheld (that is the reservation's job, and the
worker does reclaim it), and no correctness gate depends on the worker having run.

AG-M1 has no deployed database, so the accumulated cost is currently zero.

### Trigger — when it stops being acceptable

Any one of:

- a deployment with a long tail of identities that reserve once and never return, where
  rows accumulate faster than returning users reclaim them;
- **AG-M2** measuring index growth on `user_time_claims`, which
  [`../design-notes/user-schedule-non-overlap.md`](../design-notes/user-schedule-non-overlap.md)
  §12 already puts on its list — that measurement is the natural place for this to surface
  as a number rather than an argument;
- any retention or data-minimisation requirement, which would need a deletion path that
  does not depend on the subject coming back.

### What a fix must preserve

**INV-11 constrains the scope, not the caller.** It says only *user-scoped* claim
settlement removes an elapsed claim — so a background reaper that takes the identity lock
and calls the same `SettleClaims`, one user at a time, extends the set of callers without
weakening the invariant. That is the same relationship `SettleSlot` already has to the
request path: the worker routes through the service so the worker and a concurrent request
apply identical rules and cannot drift.

A fix must therefore keep the slot → identity → claims order (INV-10), keep the exclusion
constraint unpredicated (a time predicate is not immutable and cannot be indexed), and
leave confirmed claims — `expires_at IS NULL` — untouched.

### Options, none decided

1. An identity-scoped reaper worker: find users with elapsed claims via the existing
   partial index `user_time_claims_settlement_idx`, then per user take the identity lock and
   call `SettleClaims`. Closest to the existing design; costs one worker and a source query.
2. Fold the reclaim into the existing expiry worker as a second, identity-keyed phase,
   keeping the two settlements separately locked rather than merging them into one
   transaction.
3. Accept the growth and bound it externally (retention policy, partitioning by claim
   range). Cheapest now, and the option that ages worst if claim volume is real.

[`../design/transaction-semantics.md`](../design/transaction-semantics.md) §10 already
defers scalable *slot-scoped* settlement — indexed bounded settlement, expiry queues — to
AG-M2+. That is a different problem (throughput of reclaiming capacity, not reclaiming
abandoned claim rows), but both touch the settlement path, so whichever lands first should
be shaped with the other in view.

### Evidence

Observed locally on 2026-08-02 while documenting the load harness, at a 10s TTL: after the
worker expired all five `hot-slot` reservations, `reservations` held five `expired` rows and
`user_time_claims` still held five rows, all elapsed. One of the five identities then
reserved again and its row was replaced; the other four remained. The permanence half is
also the standing assertion of `TestNegativeControlElapsedClaimSurvivesUntilSettled`.

## 4. DEBT-2 — no retention policy for durable rows

### What it is

No durable row is ever deleted. `user_time_claims` is the only table any `DELETE` statement
in the codebase touches (DEBT-1 covers when that runs); everything else accumulates for the
life of the database. Reservations reach terminal states by `UPDATE`, not removal, and no
`ON DELETE` clause exists anywhere in the schema. Nothing in the design documents states
when a row of any table may be removed.

| Table | Grows with | Why the rows are kept | Whose decision the horizon is |
|---|---|---|---|
| `reservations` | every reserve | terminal states *are* the record — they explain a past refusal and are referenced by bookings and idempotency records | product / legal retention |
| `bookings` | every confirm | the durable commitment itself | product / legal retention |
| `idempotency_records` | every mutation, admitted or refused | replay correctness for as long as a client may still retry the key | engineering — no product reason to keep them beyond that |
| `user_identities` | first reserve per identity | **exempt — must never be deleted**, see below | not a retention subject |

`idempotency_records` is the pressing one, and the reason to open this entry now rather than
at deployment: it grows linearly with total mutations, refusals included — a 60-request
harness run writes 60 rows — and unlike the other two it has no product reason to exist
after its key stops being retryable. The others are business history that some retention
rule would legitimately keep for years.

### Why it is this way

AG-M1 has no deployed database, no data owner, and no product retention requirement, so any
horizon written today would be a guess dressed as a policy. Nothing in the transactional
core needs deletion either: both partial indexes on `reservations` are `WHERE state =
'held'`, so terminal rows fall out of them and cost heap space rather than write-path work.
That is a designed property and it is what makes deferring this safe.

### Why it is acceptable today

Correctness does not depend on it, and neither does hot-path cost. Terminal reservations and
bookings are outside the indexes the reserve path consults; idempotency lookups are
primary-key hits on a B-tree, so the table growing linearly costs logarithmically. The debt
is storage, operational surface, and — the part that is not a performance question at all —
having no answer to "delete this user's data".

### Trigger — when it stops being acceptable

- the first deployment with sustained real traffic, where growth is measured rather than
  hypothetical;
- any data-minimisation or subject-erasure requirement: every table above carries `user_id`
  and organisation identifiers;
- backup, restore, or migration time dominated by history rather than live state;
- AG-M2 or later measuring anything where table size is a variable it did not control.

### What any policy must preserve

1. **`user_identities` is exempt.** Rows are created on a user's first reserve and never
   deleted, and a `FOR UPDATE` on a never-deleted row is what makes the insert-then-lock
   acquisition race-free (`00001_init.sql:160`, §2.2). A policy that swept "inactive"
   identities would reintroduce exactly the acquisition race the identity lock exists to
   close. It is not a retention subject, and the exemption needs to survive whoever writes
   the policy without reading this line.
2. **An idempotency record may only be purged once no client can still retry its key.** The
   record's entire job is that a retry after an unobserved outcome replays rather than
   commits twice (INV-5), and the retry policy in
   [`../design/latency-timeouts-and-retries.md`](../design/latency-timeouts-and-retries.md)
   §4 rule 4 tells clients to resolve an unknown commit outcome by replaying the *same* key,
   with no stated deadline. So the purge horizon is a contract with clients before it is a
   storage decision, and it has to be derived there rather than chosen here.
3. **Referential order.** `bookings.reservation_id` and `user_time_claims.reservation_id`
   both reference `reservations`, with no `ON DELETE` behaviour. A deletion path therefore
   errors rather than cascades — safe, but it means any sweep has to order its work.

### Options, none decided

1. A bounded time-based purge of `idempotency_records` on a horizon derived from the retry
   policy. Smallest scope, clearly engineering-owned, and it addresses the only table with
   no product stake.
2. Archival of terminal reservations and bookings to a cold table or a partition — range
   partitioning on `created_at` makes dropping old history O(1) rather than a mass delete.
3. A full retention policy owned by the product, including erasure semantics for `user_id`.
   The only option that answers a subject-erasure request, and the one that needs a decision
   maker this project does not yet have.

These compose: (1) can land alone and early without prejudging (3).

### Related

DEBT-1 is a different problem despite the shared symptom. A stale claim is not history — it
sits inside the unpredicated exclusion index every reserve consults, and removing it
restores the state the schema describes. Deleting a terminal reservation would destroy the
record instead. Different cost, different trigger, different owner.

### Evidence

Confirmed on 2026-08-02 by exhaustive search: the only `DELETE` statements outside tests
target `user_time_claims` (`internal/postgres/tx.go:386`, `:401`); the only other row remover
is the reset `TRUNCATE` (`internal/postgres/control.go:303`), which serves the integration
suites and `alloca-seed -reset`; and `grep "ON DELETE"` over the migrations returns nothing.
