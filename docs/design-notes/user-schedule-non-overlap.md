# User schedule non-overlap

**Status:** Accepted 28 July 2026. Implementation is AG-M1 PR4; the original
worker/API/telemetry PR becomes PR5.  
**Scope:** define the cross-resource correctness invariant that one identity cannot hold
or confirm overlapping bookings, record the design decisions that implement it, and state
the correctness gates the implementation must pass.  
**Normative owner:** this note records the design and its rationale. Once PR4 lands,
[`../design/transaction-semantics.md`](../design/transaction-semantics.md) is the
normative contract for the invariant and this note is history, not a second specification.

## 1. Decision summary

Alloca-Go must enforce this committed-state invariant:

> For one identity, no two active booking claims may overlap in time.

The invariant is cross-resource. It applies even when the two bookings refer to different
slots, sessions, resources, organisations, or future service partitions.

The time model is half-open:

```text
[starts_at, ends_at)
```

Therefore:

- `10:00–11:00` and `11:00–12:00` are adjacent and do not overlap;
- identical intervals overlap;
- partial overlap in either direction overlaps;
- an interval wholly containing another overlaps.

Two intervals overlap exactly when:

```text
existing.starts_at < requested.ends_at
AND requested.starts_at < existing.ends_at
```

For this invariant, an **active booking claim** is one that currently grants or reserves
exclusive use of the identity's time:

- a confirmed booking is active;
- an unexpired hold is active;
- a cancelled reservation or booking is inactive;
- an elapsed hold is inactive, whether or not a cleanup worker has rewritten its row yet.

The database transaction boundary is authoritative. A service-level pre-check may improve
error reporting, but it is not the correctness mechanism.

## 2. Why the existing slot lock is insufficient

AG-M1 serialises mutations for one slot by locking that slot's aggregate row
(`transaction-semantics.md` §2). That proves capacity safety for concurrent requests
targeting the same slot.

User schedule safety has a different conflict key. Consider two concurrent transactions:

```text
T1: identity (org_a, user_1) reserves Yoga,     10:00–11:00
T2: identity (org_a, user_1) reserves Swimming, 10:30–11:30
```

If Yoga and Swimming are different slots, T1 and T2 lock different slot rows. Each can
observe no conflicting claim and both can commit unless they also contend on a shared
schedule authority or the database rejects the overlapping committed state.

The unsafe check-then-write shape is:

```text
SELECT conflicting claim for identity K;
-- both transactions observe none
INSERT claim;
-- both transactions commit
```

Changing the isolation level or adding an application query without defining the shared
serialization mechanism does not by itself establish the invariant.

## 3. Contract boundaries

### 3.1 What the invariant protects

The rule protects an identity's schedule, not slot capacity:

```text
slot capacity safety
    one slot cannot admit more active claims than capacity
user schedule safety
    one identity cannot own two active claims covering the same instant
```

A successful reserve must satisfy both invariants in the same logical transaction.

### 3.2 What it does not claim

This note does not define:

- travel or preparation buffers between adjacent bookings;
- household, team, membership, or account-wide limits;
- limits such as "at most N bookings per day";
- recurring-series conflict policy;
- priority rules for choosing which of two racing requests should win;
- cross-database enforcement after physical sharding (§13).

One limit is deliberate and worth stating plainly, because it follows directly from §3.3:

> The invariant protects a **user's** schedule, and the user is whoever or whatever the
> booked time belongs to — not whoever arranged it.

A user need not be a person. If a member books grooming appointments for two pets, each
pet is the thing whose time is being reserved, so each is its own user: overlapping
appointments for two different pets are perfectly legitimate, and refusing the second
would be a bug. If instead both were booked under the member's own `user_id`, the second
would be refused as a schedule conflict — correctly, by this invariant's rules, but
wrongly for the product. The same reasoning covers a member booking on behalf of a child,
a team, or a guest.

**So the modelling rule is: whoever's time is consumed gets a `user_id`.** The account
that arranges or pays for a booking is a separate concern, and AG-M1 does not model it.

Two consequences follow, and neither is a gap this milestone should close:

- Alloca cannot tell that two `user_id`s belong to the same household, owner, or human.
  Linking them would require an account or identity service that AG-M1 deliberately does
  not have (`transaction-semantics.md` §1.1: AG-M1 introduces no authentication).
- Likewise `(org_a, user_1)` and `(org_b, user_1)` are distinct users with independent
  schedules, even if the same real-world individual is behind both. Collapsing them would
  require `user_id` to be globally unique — exactly the property the composite key exists
  to avoid depending on.

### 3.3 Identity scope key

The schedule-claim scope key is:

```text
(user_organisation_id, user_id)
```

`user_organisation_id` is **the organisation under which the user's identity is issued
and scoped**. It is *not* derived from the target slot. For the representative club workload
this reads naturally as the member's home club, but the technical definition is identity
issuance, not venue.

Consequently a cross-organisation booking is well defined: identity `(org_a, user_1)` may
book a slot owned by `org_b`, and the resulting claim is keyed `(org_a, user_1)`. The
identity's schedule is protected across every organisation it books into — which is
precisely the case §2 shows the slot lock cannot cover.

Two properties follow, and both are reasons to prefer this key over a bare global
`user_id`:

- `(user_organisation_id, user_id)` is already a globally unique identity, so the invariant
  needs no new identifier and no "`user_id` must be globally unique" precondition. It
  reuses the tuple that already scopes idempotency (`transaction-semantics.md` §5.1), so
  identity means one thing throughout the system.
- All of one user's claims carry a single `user_organisation_id`, so they shard together
  under the organisation-based routing AG-M5 plans. A globally-keyed alternative would
  scatter them.

The current code already behaves this way, but by construction rather than by contract:
`internal/service/service.go` writes the reservation's `OrganisationID` from the command,
not from the locked slot. PR4 makes that explicit in `transaction-semantics.md` §1.1 and
pins it with a cross-organisation test (§10.1 gate 8), so a later change cannot "tidy"
the field to the slot's organisation and silently disable the invariant.

PR4 also corrected the other half of the picture. A slot's identity is the pair
`(slot_organisation_id, slot_id)` — the slot's *owner* — and it was previously keyed by
`slot_id` alone (`transaction-semantics.md` §1.2). The two changes are the same
correction seen from opposite ends: identity is a pair scoped to an organisation, and
which organisation depends on whether the thing is a *user* or a *slot*. A claim
therefore carries both, in separate columns, because a cross-organisation booking has
different values in each.

## 4. Lifecycle semantics

The invariant applies to active claims, not to all historical lifecycle rows. Two facts
about the existing schema shape the design.

### 4.1 Lifecycle rows are not one-to-one with claims

Confirming does not move a row from one table to another. The reservation stays at
`state = 'confirmed'` **and** a booking row is inserted with `state = 'active'`
(`transaction-semantics.md` §3.1–§3.2). One logical claim is therefore represented by two
rows after confirmation.

Any model that derives active claims from a union of `reservations` and `bookings`
self-conflicts the instant a hold is confirmed: the identity appears to hold two
overlapping claims for the same booking. This is the strongest argument for a separate
relation whose rows correspond exactly to live claims.

### 4.2 An elapsed hold must stop blocking without worker help

A row with `state = 'held'` is not an active claim forever. Once its persisted
`expires_at` has elapsed according to authoritative PostgreSQL time, it must not continue
to block the identity's schedule merely because the cleanup worker has not rewritten it.

A partial constraint such as:

```sql
WHERE state IN ('held', 'confirmed')
```

is insufficient if elapsed holds remain stored as `held`. Conversely:

```sql
WHERE state = 'confirmed'
   OR (state = 'held' AND expires_at > now())
```

is not a sound schema mechanism at all: PostgreSQL index predicates and exclusion
constraints require immutable expressions, so they cannot depend on a moving wall-clock
condition whose truth changes without a row update.

The resolution mirrors the settlement rule the slot authority already uses
(`transaction-semantics.md` §2.1): **correctness must not depend on the background worker
having run**, so the operation settles stale state itself, inside the transaction, before
evaluating preconditions. §6 applies that rule to claims.

## 5. Concurrency authority — options considered

A correct implementation needs one database-visible authority shared by every transaction
that may conflict for the same identity.

### 5.1 PostgreSQL exclusion constraint on active claims — chosen

```sql
EXCLUDE USING gist (
    user_organisation_id WITH =,
    user_id              WITH =,
    claim_range          WITH &&
)
```

`user_organisation_id` and `user_id` are `text`, so the `btree_gist` extension is
**required** for the scalar equality operators — not optional. It is a *trusted*
extension (verified on PostgreSQL 16), so installing it needs the `CREATE` privilege on
the database rather than superuser; it must, however, be available on the server, which
on RDS means `rds.allowed_extensions`. The rollback deliberately leaves it installed:
`CREATE EXTENSION IF NOT EXISTS` cannot establish that this migration created it, and
dropping a possibly-shared extension is worse than leaving an unused one.

The claim relation is part of the `00001_init.sql` baseline rather than a transitional
migration. AG-M1 has no deployed database and no persisted data, so there was nothing to
migrate; the first public schema states the model the project believes rather than
preserving the record of correcting it during review. Migration compatibility begins at
that baseline, and every change after it is forward-only and numbered.

Strengths:

- the invariant is declared at the authoritative storage boundary;
- transactions targeting different slots still conflict correctly;
- every writer, including future tools and workers, gets the same protection;
- the database rejects a race that passes an earlier service pre-check.

Constraints, all discharged by §6:

- the relation must contain only active claims, or use a predicate whose truth changes
  only through row updates;
- the adapter must map the exclusion violation to a stable domain refusal;
- contention and deadlock behaviour with the slot lock must be measured and tested;
- migrations must account for the extension and any conflicting existing data.

### 5.2 Per-identity schedule row lock — rejected as the sole mechanism

A transaction locks one stable row per identity, settles elapsed claims, checks the
interval, then mutates. Easy to explain, and it supports richer identity-level policy
inside one critical section. But every path that creates, removes, confirms, expires, or
moves a claim must take the same lock, new identities need a race-safe way to establish
the row, and a single missed writer path silently breaks the invariant with no backstop.

### 5.3 Serializable isolation with a predicate read — rejected

Theoretically viable, but it broadens a narrow invariant into a transaction-wide
mechanism, introduces retry semantics across all mutations, and is easier to misuse than
an invariant-specific authority.

### 5.4 Advisory lock — rejected

Advisory-lock key construction, collision analysis, and universal writer discipline are
application conventions rather than schema-enforced facts. Useful at most as an
optimisation alongside an authoritative constraint.

## 6. Decided design

Active claims live in their own relation, so no moving-clock predicate is ever needed and
one logical claim is exactly one row:

```text
user_time_claims
    reservation_id    primary key, references reservations
    user_organisation_id, user_id
                      the user (§3.3) — never the slot's organisation
    slot_organisation_id, slot_id
                      the slot's identity (§1.2), its *owner's* organisation — not the
                      identity's above; telemetry and settlement, never part of the key
    claim_range       tstzrange over [slot.starts_at, slot.ends_at)
    expires_at        the backing hold's expiry; NULL once confirmed
    EXCLUDE USING gist (user_organisation_id =, user_id =, claim_range &&)
```

Keying the row on `reservation_id` makes the confirm case safe by construction: confirming
**updates** the existing claim rather than inserting a second one, so a booking can never
self-conflict with the hold it was created from (§10.2 gate 13).

Claim lifecycle, all transitions inside the operation's existing transaction:

| Operation | Effect on `user_time_claims` |
|---|---|
| reserve (success) | insert claim; `expires_at` = the hold's expiry |
| confirm (success) | update the claim: `expires_at → NULL` (permanent) |
| cancel reservation | delete the claim |
| cancel booking | delete the claim |
| expiry settlement | delete the claim alongside the `held → expired` transition |

**Claim settlement (the answer to §4.2).** Immediately after acquiring the slot lock and
before evaluating preconditions, `reserve` deletes this identity's own elapsed claims:

```sql
DELETE FROM user_time_claims
 WHERE user_organisation_id = $1 AND user_id = $2
   AND expires_at IS NOT NULL AND expires_at <= $3   -- authoritative tx time
```

This is the identity-scoped analogue of the slot-scoped settlement in
`transaction-semantics.md` §2.1, and it carries the same guarantee: an elapsed hold stops
blocking the schedule whether or not the worker has run. The delete is bounded by one
identity's own live claims, and it is indexed by the exclusion constraint's leading
columns.

Confirmed claims have `expires_at IS NULL` and are never settled — only cancellation
removes them.

A transactional pre-check query may still be issued before the insert to produce a clearer
refusal, but the exclusion constraint remains the race-proof authority: any pre-check that
passes can still lose to a concurrent commit, and the constraint is what catches it.

## 7. Lock order (normative for PR4)

Every path that touches both authorities uses one order:

```text
slot authority  →  user schedule authority
```

Reserve, confirm, cancel, and expiry all already begin by locking the slot row
(`transaction-semantics.md` §2), so this order preserves existing flows unchanged and adds
the claim mutation after the precondition evaluation. No path may touch
`user_time_claims` before its slot lock, including the PR5 worker and any future
administrative operation.

Two concurrent reserves for one identity on different slots take *different* slot locks
and then contend on the claim relation, where one blocks until the other commits. That is
a wait, not a cycle: the claim authority is the only shared resource, so it cannot deadlock
against the slot locks as long as the order above holds everywhere.

An exclusion constraint can block while a conflicting transaction is uncommitted. That
wait stays inside the existing caller, transaction, `lock_timeout`, and `statement_timeout`
budgets, and database timeout classification stays distinct from a business refusal.

## 8. Domain outcome and idempotency

A schedule conflict is a **business refusal**, not a system fault, and it does not enlarge
the outcome set. In the two-axis taxonomy of `internal/domain/outcome.go`, `Outcome` is a
closed set and `Reason` is the stable code a `business_refusal` carries. So:

```text
Outcome = OutcomeBusinessRefusal
Reason  = ReasonScheduleConflict     ("schedule_conflict")
```

`no_capacity` is a sibling `Reason`, not an outcome, and the two must stay distinct: a
schedule conflict can occur while the slot still has capacity.

Idempotency rules are unchanged by this invariant:

- replaying a successful reserve with the same key returns the original result rather than
  conflicting with its own claim;
- replaying a refused command returns the recorded refusal;
- reusing one key for a different command remains key misuse;
- two different keys racing for overlapping intervals for one identity produce at most one
  successful active claim;
- an ambiguous commit remains `unknown_replayable`; the client replays with the same key
  to learn whether the claim exists.

Because the idempotency record is written in the same transaction and before the mutation
(`transaction-semantics.md` §5.2), an exclusion violation is interpreted only after replay
resolution has established that the request is a genuinely new mutation.

## 9. Authoritative time

The authoritative-time contract (`transaction-semantics.md` §1.5) remains in force.

The claim interval comes from persisted slot data, never from an API-host clock.
PostgreSQL-owned decision time determines whether a hold has elapsed and may be settled
before the conflict decision. API-node clocks never decide whether a claim is active.

Half-open semantics are independent of the decision timestamp:

```text
claim range: [slot.starts_at, slot.ends_at)
hold active: decision_now < expires_at
```

A hold may stop blocking before its slot interval begins, if it expires or is cancelled.
Confirmation keeps the same interval active without creating a second claim.

## 10. Required correctness gates

PR4 is incomplete until PostgreSQL integration tests prove the following against persisted
state.

### 10.1 Interval and identity semantics

1. non-overlapping claims for one identity both succeed;
2. adjacent intervals succeed;
3. identical intervals conflict;
4. partial overlap from either side conflicts;
5. a requested interval contained by an existing interval conflicts;
6. a requested interval containing an existing interval conflicts;
7. different identities may hold overlapping intervals;
8. a command issued for `(org_a, user_1)` against a slot owned by `org_b` creates a claim
   keyed `(org_a, user_1)`, and conflicts with that identity's other claims — not with
   `(org_b, user_1)`'s.

### 10.2 Lifecycle semantics

9. an active hold conflicts with another hold;
10. an active hold conflicts with a confirmed booking;
11. a confirmed booking conflicts with another active claim;
12. a cancelled reservation or booking does not conflict;
13. an elapsed hold does not conflict even when the expiry worker has not run;
14. confirmation neither briefly removes protection nor creates a duplicate self-conflict;
15. cancellation and expiry remove the claim atomically with their lifecycle transition.

### 10.3 Concurrency semantics

16. two concurrent overlapping reserves for one identity on different slots produce
    exactly one success;
17. concurrent non-overlapping reserves for one identity can both succeed;
18. concurrent overlapping reserves for different identities can both succeed when slot
    capacity permits;
19. a reserve racing cancellation or expiry observes one serializable committed outcome;
20. a reserve racing confirmation cannot bypass the invariant during the state change;
21. the gates fail when the exclusion constraint is removed (§11).

The decisive persisted-state assertion is:

> At every committed database state, for any identity and any instant, at most one active
> claim contains that instant.

### 10.4 Fault and budget semantics

22. a genuine committed conflict maps to `business_refusal` / `schedule_conflict`;
23. waiting past `lock_timeout` or `statement_timeout` maps to `timeout_db`, not to the
    business refusal;
24. caller cancellation maps to the applicable client/server timeout outcome;
25. an ambiguous commit remains replayable with the same idempotency key;
26. rollback and cleanup remain independently bounded.

## 11. Negative controls

Passing concurrency tests are credible only if they fail when the protection is removed.
Three controls were executed against the PR4 implementation; each is recorded with what
was changed and which gate failed.

**1. Drop the exclusion constraint.** Automated, and permanently part of the suite:
`TestNegativeControlDroppingTheConstraintAdmitsOverlap` drops
`user_time_claims_no_overlap`, runs the different-slot race, and *asserts that
overlapping claims are persisted*. It is the inverse of the acceptance gate, so if the
race ever stops happening the control fails and says so. Result: overlapping claim pairs
appear, confirming the acceptance gate is discriminating rather than passing vacuously.

**2. Remove identity-scoped claim settlement.** Deleting the `SettleClaims` call from
`Service.Reserve` makes `TestElapsedHoldStopsBlockingWithoutTheWorker` fail with
`business_refusal`/`schedule_conflict` — an abandoned hold on another slot goes on
blocking the identity, exactly the worker-dependence §4.2 forbids. Verified at both
layers, service and PostgreSQL.

**3. Key the booking's claim separately on confirm.** Making confirm insert a
booking-keyed claim instead of updating the hold's claim in place makes
`TestConfirmKeepsOneClaimAndKeepsBlocking` fail with `ErrScheduleConflict`: the identity
conflicts with itself the instant it confirms. This is what keying the relation on
`reservation_id` (§6) makes unrepresentable.

Note what control 3 replaced. An earlier draft proposed "delete the claim on confirm
instead of updating it" — that was executed and **passed**, because the delete precedes
the insert within the same transaction, so there is nothing to conflict with. It was not
a discriminating control, and the version above is what actually models the risk.

**4. Weaken `slots` to a single-column key.** Keying by `slot_id` alone makes the
slot-identity gates fail at *setup* — two organisations can no longer own a slot with the
same identifier — and on a database that already holds such a pair, PostgreSQL refuses to
create the key at all. The old schema cannot represent the state the gates require.

Control 1 remains in the suite; 2, 3 and 4 were executed and reverted.

A fifth control was written and then removed with the thing it guarded. Review raised
(Codex, P1) that a claim table added to a database holding live reservations would exempt
every one of them, and the migration gained a backfill plus two tests. Consolidating the
schema into a single baseline (below) dissolved the problem rather than fixing it: the
claim table and the reservations table are now created by the same migration, so a
reservation without a claim is not a reachable state.

## 12. Performance and scaling

This invariant adds an identity-keyed contention domain. That is desirable for one
identity's conflicting requests, but its cost must not be confused with slot contention.

The AG-M2 measurement plan should distinguish:

- same identity, same slot;
- same identity, different overlapping slots;
- same identity, different non-overlapping slots;
- different identities, same slot;
- different identities, different slots.

and record added statements per mutation, exclusion-index wait time, deadlock and retry
counts, effect on p95/p99 and the timeout outcome mix, index growth, and whether one
unusually active identity can affect unrelated identities through pool pressure.

Because this changes the write path, **AG-M2's frontier baseline must be measured after
PR4 lands**, or it has to be re-run. No performance claim is made by this note.

## 13. Scale-out boundary (AG-M5)

While one PostgreSQL authority owns all claims, the database enforces the invariant across
every API replica. AG-M1 through AG-M4 are in that regime, so this is not a correctness
gap today.

AG-M5's organisation-based sharding makes it an explicit constraint. A cross-organisation
booking touches two authorities: slot capacity in the *slot's* organisation, and the
schedule claim in the *identity's* organisation. Those can land on different shards.

Claims for one user always share one `user_organisation_id` (§3.3), so a user's own
claims stay co-located and the exclusion constraint keeps working locally. What AG-M5 must
decide is how a single booking spans two organisation authorities. This note records the
constraint and does not attempt the distributed-transaction design.

## 14. Decisions

| # | Question | Decision |
|---|---|---|
| 1 | Scope key | `(user_organisation_id, user_id)`, scoped by identity issuance, never derived from the slot (§3.3) |
| 2 | Which relation represents active claims | a separate `user_time_claims` relation (§6) |
| 3 | How an elapsed hold stops participating | identity-scoped claim settlement inside the transaction, before preconditions (§6) |
| 4 | Constraint primary or backstop | the exclusion constraint is the authority; any pre-check is for message quality only (§6) |
| 5 | Lock order | slot authority → user schedule authority, on every path (§7) |
| 6 | Which operations touch a claim | reserve inserts, confirm updates, cancel and expiry delete (§6) |
| 7 | Refusal name | `OutcomeBusinessRefusal` + new `ReasonScheduleConflict` (§8) |
| 8 | Distinguishing a violation from faults | exclusion violation → refusal; lock/statement timeout → `timeout_db` (§8, §10.4) |
| 9 | Concurrency acceptance gate | §10.3 gate 16 |
| 10 | Discriminating negative control | §11 |
| 11 | Migration and extension | folded into the `00001_init.sql` baseline; `btree_gist` required (§5.1) |
| 12 | Owning milestone | AG-M1 PR4; original worker/API/telemetry PR becomes PR5 |

Remaining open, deliberately deferred:

- buffers, per-day limits, and other identity-level policies (§3.2);
- the AG-M5 cross-authority design for bookings spanning two organisations (§13);
- bounded/indexed claim settlement if the AG-M2 measurements show the per-identity delete
  on the hot path is material (§12).
