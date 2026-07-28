# User schedule non-overlap

**Status:** Proposed. Implementation timing is intentionally deferred until after AG-M1
PR3 is merged and the design has been reviewed.  
**Scope:** define the cross-resource correctness invariant that one user cannot hold or
confirm overlapping bookings, and identify the implementation and validation properties
that a later milestone must satisfy.

## 1. Decision summary

Alloca-Go must enforce this committed-state invariant:

> For one user, no two active booking claims may overlap in time.

The invariant is cross-resource. It applies even when the two bookings refer to different
slots, sessions, resources, clubs, or future service partitions.

The time model is half-open:

```text
[start_at, end_at)
```

Therefore:

- `10:00–11:00` and `11:00–12:00` are adjacent and do not overlap;
- identical intervals overlap;
- partial overlap in either direction overlaps;
- an interval wholly containing another overlaps.

Two intervals overlap exactly when:

```text
existing.start_at < requested.end_at
AND requested.start_at < existing.end_at
```

For this invariant, an **active booking claim** is one that currently grants or reserves
exclusive use of the user's time:

- a confirmed booking is active;
- an unexpired hold is active;
- a cancelled booking is inactive;
- an expired hold is inactive, whether or not historical reservation rows have already
  been compacted or archived.

The database transaction boundary must be authoritative. A service-level pre-check may
improve error reporting, but it cannot be the correctness mechanism.

## 2. Why the existing slot lock is insufficient

AG-M1 serialises mutations for one slot by locking that slot's aggregate row. That proves
capacity safety for concurrent requests targeting the same slot.

User schedule safety has a different conflict key. Consider two concurrent transactions:

```text
T1: user U reserves Yoga,     10:00–11:00
T2: user U reserves Swimming, 10:30–11:30
```

If Yoga and Swimming are different slots, T1 and T2 lock different slot rows. Each can
observe no conflicting user booking and both can commit unless they also contend on a
shared user-schedule authority or the database rejects the overlapping committed state.

The unsafe check-then-write shape is:

```text
SELECT conflicting booking for user U;
-- both transactions observe none
INSERT booking claim;
-- both transactions commit
```

Changing the isolation level or adding an application query without defining the shared
serialization mechanism does not by itself establish the invariant.

## 3. Contract boundaries

### 3.1 What the invariant protects

The rule protects a user's schedule, not slot capacity:

```text
slot capacity safety
    one slot cannot admit more active claims than capacity
user schedule safety
    one user cannot own two active claims covering the same instant
```

A successful reserve must satisfy both invariants in the same logical transaction.

### 3.2 What it does not currently claim

This note does not yet define:

- travel or preparation buffers between adjacent bookings;
- household, team, membership, or account-wide limits;
- limits such as “at most N bookings per day”;
- recurring-series conflict policy;
- priority rules for choosing which of two racing requests should win;
- cross-database enforcement after physical sharding.

Those may become future policies, but they are not part of the present correctness gate.

### 3.3 Scope key

The initial scope key is `user_id`.

If the product later distinguishes identities by tenant or club, the schema must state
whether the real key is `(tenant_id, user_id)` or a globally unique `user_id`. The
implementation must not silently infer that choice from current identifier formatting.

## 4. Lifecycle semantics

The invariant applies to active claims rather than to all historical reservation rows.
This distinction matters because hold expiry is time-dependent.

A row with lifecycle state `held` is not necessarily an active claim forever. Once its
persisted `expires_at` has elapsed according to authoritative PostgreSQL time, it must not
continue to block the user's schedule merely because an asynchronous cleanup worker has
not rewritten the row to `expired` yet.

Consequently, a partial database constraint such as:

```sql
WHERE status IN ('held', 'confirmed')
```

is insufficient if elapsed holds can remain stored as `held`. Conversely, a partial
predicate such as:

```sql
WHERE status = 'confirmed'
   OR (status = 'held' AND expires_at > now())
```

is not a sound schema mechanism: PostgreSQL index predicates and exclusion constraints
cannot depend on a moving wall-clock condition whose truth changes without a row update.

A later implementation must therefore choose one of these explicit lifecycle models:

1. **Eager settlement before conflict enforcement.** The transaction settles elapsed
   holds for the user before testing or inserting a new claim, while also using a shared
   user-schedule serialization mechanism.
2. **Separate live-claim relation.** Historical booking/reservation state remains in its
   lifecycle table, while a separate relation contains only currently active user-time
   claims. Reserve inserts a claim, confirm retains or transforms it, and cancel/expiry
   removes it.
3. **Materialised active state maintained transactionally.** The lifecycle row contains a
   stable, indexable active marker that is changed transactionally when the claim stops
   being active; correctness then depends on expiry settlement occurring before any
   conflicting decision.

The design review must reject any model in which correctness depends on the cleanup worker
running promptly.

## 5. Concurrency authority

A correct implementation needs one database-visible authority shared by all transactions
that may conflict for the same user.

Candidate mechanisms are:

### 5.1 PostgreSQL exclusion constraint on active claims

Conceptually:

```sql
EXCLUDE USING gist (
    user_id WITH =,
    tstzrange(start_at, end_at, '[)') WITH &&
)
```

This directly rejects overlapping ranges for the same user. UUID or scalar equality may
require PostgreSQL's `btree_gist` extension.

Strengths:

- the invariant is declared at the authoritative storage boundary;
- transactions targeting different slots still conflict correctly;
- every writer, including future tools or workers, receives the same protection;
- the database can reject a race that passes an earlier service pre-check.

Constraints:

- the relation participating in the constraint must contain only active claims, or use a
  stable predicate whose truth changes only through row updates;
- the adapter must map the exclusion violation to a stable domain outcome;
- contention and deadlock behaviour with the slot lock must be measured and tested;
- migrations must account for the required extension and existing conflicting data.

### 5.2 Per-user schedule row lock

A transaction locks one stable row for the user, settles elapsed claims, checks the
requested interval, then mutates the slot and user schedule.

Strengths:

- easy to explain as explicit serialization;
- supports richer future user-level policies inside the same critical section;
- can make the losing request's domain reason explicit before insert.

Constraints:

- every path that creates, removes, confirms, expires, or moves a claim must acquire the
  same user lock;
- new users need a race-safe way to establish the authority row;
- lock ordering between user and slot authorities must be globally fixed to avoid
  deadlocks;
- a missed writer path silently breaks the invariant unless a database constraint also
  provides a backstop.

### 5.3 Serializable isolation with a predicate read

A serializable transaction reads the user's overlapping range and inserts only if none is
found. PostgreSQL may abort one transaction with a serialization failure.

This is theoretically viable but is not the preferred default for Alloca-Go at this
stage. It broadens the transaction-level mechanism, introduces retry semantics across all
mutations, and is easier to misuse than a narrow invariant-specific authority.

### 5.4 Advisory lock

A transaction-scoped advisory lock derived from `user_id` can serialize one user's
schedule mutations.

This is not preferred as the sole correctness boundary. Advisory-lock key construction,
collision analysis, and universal writer discipline are application conventions rather
than schema-enforced facts. It may be useful as an optimisation or transitional mechanism
only when paired with an authoritative constraint.

## 6. Preliminary recommendation

The implementation design should start from this combination:

1. represent active user-time ownership in a form whose rows correspond exactly to live
   claims;
2. enforce non-overlap with a PostgreSQL exclusion constraint;
3. optionally perform an earlier transactional conflict query to return a clearer domain
   refusal;
4. retain the exclusion constraint as the race-proof backstop;
5. define one global lock order if reserve/confirm/cancel/expiry must touch both slot and
   user-schedule authorities.

A separate `user_time_claims` relation is the cleanest current candidate because it keeps
the moving expiry condition out of an index predicate:

```text
user_time_claims
    user_id
    reservation_id or booking_id
    slot_id
    time_range
    claim_kind or lifecycle reference
```

The exact schema is deliberately not decided by this note. Before implementation, the
design must compare this relation with settling and constraining the existing reservation
model, using the acceptance criteria in this document.

## 7. Transaction and lock-order requirements

Reserve, confirm, cancel, expiry, and any future reschedule operation must preserve both
slot capacity and user schedule safety atomically.

The implementation design must publish one lock order and follow it on every path. For
example:

```text
user schedule authority -> slot authority
```

or:

```text
slot authority -> user schedule authority
```

The choice requires inspection of the existing transaction flows; this note does not pick
one prematurely. The required property is that all multi-authority paths use the same
order, including workers and administrative operations.

An exclusion constraint can still produce blocking while another conflicting transaction
is uncommitted. Its wait must remain inside the existing caller, transaction, lock, and
statement budgets. Database timeout classification must remain distinct from a business
conflict refusal.

## 8. Domain outcomes and idempotency

A schedule conflict is a business refusal, not a system fault. The exact public outcome
name should be chosen alongside the API contract; candidates include:

```text
schedule_conflict
user_time_conflict
booking_overlap
```

The name must distinguish this refusal from `no_capacity` because the slot may still have
capacity.

Idempotency rules continue to apply before and after this invariant is added:

- replaying the same successful reserve command and idempotency key returns the original
  successful result rather than conflicting with its own claim;
- replaying the same refused command returns the recorded refusal;
- reusing one idempotency key for a different command remains key misuse;
- two different keys racing for overlapping intervals for one user produce at most one
  successful active claim;
- an ambiguous commit remains `unknown_replayable`; the client must replay with the same
  key to learn whether the claim exists.

A database exclusion violation must therefore be interpreted only after the idempotency
path has established whether the request is a replay or a genuinely new mutation.

## 9. Authoritative time

The authoritative-time contract from `transaction-semantics.md` remains in force.

The requested booking interval comes from persisted slot data, not from an API-host clock.
PostgreSQL-owned decision time determines whether an existing hold has elapsed and can be
settled before the conflict decision. API-node clocks must not decide whether a user-time
claim remains active.

Half-open interval semantics are independent of the decision timestamp:

```text
claim range: [slot.starts_at, slot.ends_at)
hold active: decision_now < expires_at
```

A hold may stop blocking before the booked slot interval begins if it expires or is
cancelled. Confirmation keeps the same slot interval active without creating a second
claim for the same booking.

## 10. Required correctness gates

The future implementation is incomplete until PostgreSQL integration tests prove the
following against persisted state.

### 10.1 Interval semantics

1. non-overlapping bookings for one user both succeed;
2. adjacent intervals succeed;
3. identical intervals conflict;
4. partial overlap from either side conflicts;
5. a requested interval contained by an existing interval conflicts;
6. a requested interval containing an existing interval conflicts;
7. different users may hold overlapping intervals.

### 10.2 Lifecycle semantics

8. an active hold conflicts with another hold;
9. an active hold conflicts with a confirmed booking;
10. a confirmed booking conflicts with another active claim;
11. a cancelled booking does not conflict;
12. an expired hold does not conflict even when historical cleanup has not yet run;
13. confirmation does not briefly remove protection or create a duplicate self-conflict;
14. cancellation and expiry remove the active claim atomically with their lifecycle
    transition.

### 10.3 Concurrency semantics

15. two concurrent overlapping reserves for the same user and different slots produce
    exactly one success;
16. concurrent non-overlapping reserves for the same user can both succeed;
17. concurrent overlapping reserves for different users can both succeed when slot
    capacity permits;
18. a reserve racing cancellation or expiry observes one serializable committed outcome;
19. a reserve racing confirmation cannot bypass the invariant during the state change;
20. the test fails when the shared user authority or exclusion backstop is deliberately
    removed.

The decisive persisted-state assertion is:

> At every committed database state, for any user and instant, at most one active claim
> contains that instant.

### 10.4 Fault and budget semantics

21. a genuine committed conflict maps to the chosen business refusal;
22. waiting past `lock_timeout` or `statement_timeout` maps to `timeout_db`, not to the
    business refusal;
23. caller cancellation maps to the applicable client/server timeout outcome;
24. an ambiguous commit remains replayable with the same idempotency key;
25. rollback and cleanup remain independently bounded.

## 11. Negative controls

Passing concurrency tests are credible only if they fail when the protection is removed.
The implementation PR must include at least one documented negative control, such as:

- remove the exclusion constraint while leaving the service pre-check in place and show
  that the different-slot race admits both requests;
- skip the per-user authority lock and show that both transactions pass the pre-check;
- delay expiry cleanup and show that the chosen active-claim model still permits a new
  booking after authoritative expiry;
- invert one operation's lock order and demonstrate that the deadlock-focused test detects
  the inconsistency.

The negative control does not need to remain executable in production code, but the PR
must record what was changed and which gate failed.

## 12. Performance and scaling questions

This invariant adds a user-keyed contention domain. That is desirable for conflicting
requests from one user, but its cost must not be confused with slot contention.

A later measurement plan should distinguish:

- same user, same slot;
- same user, different overlapping slots;
- same user, different non-overlapping slots;
- different users, same slot;
- different users, different slots.

The design should record:

- added statements and round trips per mutation;
- exclusion-index or user-lock wait time;
- deadlock and serialization-retry counts;
- effect on p95/p99 latency and timeout outcome mix;
- index growth and cleanup behaviour;
- whether one unusually active user can affect unrelated users through pool pressure.

No performance claim is made by this note.

## 13. Scale-out boundary

While one PostgreSQL authority owns all relevant user claims, the database can enforce the
invariant across every API replica.

Physical sharding changes the problem. If one user's claims can reside in different
databases, a local exclusion constraint or local user lock cannot prove global non-overlap.
A future shard design must therefore keep all schedule claims for one scope key on one
authority, or introduce a dedicated global schedule authority. This is a placement
constraint, not merely a query-routing optimisation.

## 14. Implementation entry criteria

Before scheduling implementation, the design review should explicitly answer:

1. Is the scope key global `user_id` or `(tenant_id, user_id)`?
2. Which persisted relation represents active claims?
3. How does an elapsed hold stop participating without depending on worker promptness?
4. Is the exclusion constraint the primary authority or the backstop to an explicit user
   lock?
5. What is the global lock order across user and slot authorities?
6. Which operations create, retain, replace, or remove a claim?
7. What domain outcome names a schedule conflict?
8. How is an exclusion violation distinguished from timeout, cancellation, and internal
   failure?
9. Which integration test is the concurrency acceptance gate?
10. Which negative control proves that gate is discriminating?
11. What migration and extension requirements apply?
12. Which milestone owns implementation and measurement?

The implementation PR should update the normative transaction and API documents once
these decisions are accepted. This design note should then be marked superseded or
accepted with a pointer to the normative owner, rather than maintained as a second
competing specification.
