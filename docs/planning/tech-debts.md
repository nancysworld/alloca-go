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
| [**DEBT-3**](#5-debt-3--service-identity-is-sampled-only-before-a-run) | The load harness records service provenance before load starts but does not prove the same service identity remained behind the target for the whole run | measurement provenance | 2026-08-03, AG-Sept PR1 | open |
| [**DEBT-4**](#6-debt-4--lockbyreservation-returns-a-six-value-maybe-answered-protocol) | The shared confirm/cancel prologue returns six values, three of which encode "I may already have answered"; a caller that mishandles them proceeds on zero values | service orchestration | 2026-08-05, AG-Sept PR3a | open |
| [**DEBT-5**](#7-debt-5--no-automated-line-length-guardrail) | Nothing in CI bounds line length, so declarations grow until a human notices; ~104 code lines exceed 110 columns, the longest at 205 | tooling, readability | 2026-08-05, AG-Sept PR3a | open |
| [**DEBT-6**](#8-debt-6--res-denotes-three-unrelated-types) | `res` names a Reservation, a Result and a Response in different files, and the obvious mechanical rename is wrong in three separate ways | naming, readability | 2026-08-05, AG-Sept PR3a | open |
| [**DEBT-7**](#9-debt-7--implementation-records-that-still-read-as-scope-notes) | The three per-PR records were moved and re-headed, not rewritten; they still present planned scope in a form indistinguishable from a record of what shipped | documentation taxonomy | 2026-08-07, AG-Sept PR3b | open |

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

## 5. DEBT-3 — service identity is sampled only before a run

### What it is

`alloca-load` reads `/meta` once, immediately before it starts the workload, and records that
service revision and runtime shape in the run manifest. It does not read `/meta` again after
the workload completes, nor does each request carry a replica/build identity in its response.

The recorded identity therefore means **the service behind the target when the run began**.
It does not prove that every request in a long run reached that same binary. A restart,
rolling replacement, load-balancer target change, or mixed-version replica set could make
part of the run execute on different code while the manifest still names only the initial
service.

### Why it is this way

PR1 measures a short controlled local run against one explicitly started service process.
Within that boundary, a single pre-run read is the smallest mechanism that fixes the real
provenance defect: recording the generator's revision as the code under test.

Adding post-run checks, per-response build identity, or replica-aware provenance would expand
PR1 into deployment orchestration before the project has replicas or long retained sweeps.
That cost is not justified by the current topology.

### Why it is acceptable today

The PR1 procedure starts one local service with `make dev-measured`, performs a short run,
and saves one metrics scrape from the same process. There is no orchestrator replacing the
service and no load balancer routing across versions.

A restart during this run would usually also reset `alloca_requests_total`, causing the
client/server reconciliation check to fail rather than silently certifying the result. That
is useful protection, but it is incidental rather than a complete identity proof: a new
process could restore or expose compatible totals, and a mixed-replica topology would not
necessarily reset the aggregate.

### Trigger — when it stops being acceptable

Any one of:

- **PR2** introduces long-running sweeps where an unattended service restart becomes
  plausible during one measured run;
- **PR3** introduces multiple replicas or a load balancer, especially if replicas can run
  different revisions;
- deployment automation performs rolling replacement while experiments may be active;
- a retained or published result must prove that one code identity served the whole sample,
  rather than merely recording the identity observed at its start.

PR3 is the hard trigger: once more than one replica can answer, one pre-run `/meta` response
cannot describe the set of binaries that served the run.

### What a fix must preserve

- The generator remains an external HTTP-only client with no database or deployment-control
  credentials.
- A failed identity check must leave a machine-readable report explaining the failure rather
  than discard the run artifact.
- Provenance checks must not add per-request high-cardinality metric labels.
- A check must distinguish an intentional homogeneous restart from a mixed-version run; merely
  observing that *some* revision changed is not enough to describe which requests saw which
  code.

### Options, none decided

1. Fetch `/meta` before and after each run and require the service revision and relevant
   runtime fields to match. Smallest extension and likely sufficient for PR2's single-instance
   sweeps.
2. Expose a bounded build-information metric per replica and retain the scrape alongside the
   run. Suitable once PR3 introduces replica identity, provided the label set remains bounded
   by deployment size rather than request traffic.
3. Return a build/revision identifier in a response header and have the generator record the
   set seen during the run. This directly proves which identities answered requests, but adds
   bytes and parsing to every measured response.
4. Have the experiment orchestrator pin and record an immutable image digest for every target
   replica, then verify the target set did not change during the run. Strongest deployment-
   level answer, but belongs with PR3/PR4 orchestration rather than the HTTP client alone.

Options 1 and 2 compose: pre/post checking detects single-instance replacement, while the
replica metric describes a multi-instance target set.

### Evidence

Raised during review of AG-Sept PR1 commit `62050a0`, which correctly changed the manifest to
read `service_commit_sha` from the service's `/meta`. The implementation fetches that value
before `Runner.Run`; there is no corresponding post-run fetch. This is deliberate and sound
for PR1's one-process local procedure, but the guarantee narrows as soon as run duration or
replica count grows.

## 6. DEBT-4 — `lockByReservation` returns a six-value "maybe answered" protocol

### What it is

`internal/service/service.go` — the prologue confirm and cancel share takes six parameters
and returns six values:

```go
func (s *Service) lockByReservation(
	ctx context.Context, tx domain.Tx, id domain.ReservationID, caller domain.UserRef,
	scope domain.ScopeKey, hash string,
) (
	slot domain.Slot, res domain.Reservation, now time.Time,
	done bool, result domain.Result, err error,
)
```

Three of the returns are the work — `slot`, `res`, `now` — and three are a control protocol:
`done` means "I have already produced the caller's answer, in `result`". Both callers destructure
it identically:

```go
slot, res, now, done, r, err := s.lockByReservation(...)
if done || err != nil {
	return r, err
}
```

The hazard is not the length. It is that **`done` and the work returns are silently coupled**:
when `done` is true, `slot`, `res` and `now` are zero values, and nothing in the type system says
so. A caller that checked only `err` would compile, pass a smoke test against a live reservation,
and mis-handle exactly the paths this function exists to short-circuit — a replay, an unknown
target, and now a non-owner. Those are the paths least likely to be covered by a hand test and
most likely to matter.

### Why it is this way

It accreted, and each step was right on its own. The function began as "resolve the slot for a
reservation and lock it". Then the idempotency lookup moved inside it, because the lookup must
happen after the slot lock and before settlement. Then settlement joined it, then the
authoritative instant, and in AG-Sept PR3a the ownership comparison
([INV-23](../design/transaction-semantics.md#appendix-a--invariant-register)). Each addition had
a reason to be in the shared prologue rather than duplicated across two callers, and duplicating
it would have been the worse error: confirm and cancel drifting apart on lock order or on when
time is resolved is precisely the class of bug the invariant register exists to prevent.

What was never revisited is the *shape* the accretion produced.

### Why it is acceptable today

- There are exactly **two** callers, both in the same file, both destructuring identically, and
  both covered by tests that exercise the replay, unknown-target and non-owner paths.
- No invariant is at risk. The lock order (INV-10), the authoritative instant (INV-9) and the
  ownership rule (INV-23) are each pinned by their own tests, which fail if this function
  reorders or drops a step regardless of how its results are shaped.
- The alternative shapes all cost something real, and none is obviously right yet (below).

### Trigger — when it stops being acceptable

Any one of:

1. **A third caller.** Two callers agreeing by inspection is a convention; three is a rule
   nobody wrote down.
2. **A seventh return value**, or a second boolean. The protocol is already at the limit of
   what a reader can hold; a second flag makes the valid combinations something to reason about
   rather than read.
3. **Phase 2's cross-authority coordinator.** The horizontal-database-authority note's
   compatibility obligation 3 requires a future coordinator to dispatch to this same-authority
   path rather than rewrite it
   ([formal design](../design/horizontal-database-authority.md) §4.3). A coordinator calling
   it from a different package cannot rely on two callers in one file agreeing by eye, and it is
   the first caller that will not have been written by someone who just read the function.

### What a fix must preserve

- **One prologue, not two.** Whatever shape replaces it, confirm and cancel must keep sharing
  the same lock order, the same settlement, and the same authoritative instant.
- **The short-circuit must stay explicit at the call site.** The current code's one virtue is
  that `if done || err != nil { return r, err }` is visible in both callers; a fix that hides
  the early answer inside a helper trades a shape problem for a control-flow one.
- **No new database round trips.** The ownership check was deliberately placed before the slot
  lock so a wrong identity cannot contend with a slot it has no claim on; a refactor must not
  reintroduce that contention.

### Options, none decided

1. **A result struct with a constructor per outcome** — `prologue{Answered(result)}` versus
   `prologue{Proceed(slot, res, now)}` — so "answered" and "the work" cannot both be read from
   one value. Most direct expression of the actual protocol; costs a type.
2. **Split into two functions**: one that resolves and short-circuits, one that locks and
   settles. Reads better but risks the two callers sequencing them differently, which is the
   duplication this function exists to prevent.
3. **Leave it and pin the coupling with a test** that asserts the work returns are zero when
   `done` is true. Cheapest, and it converts an invisible convention into a checked one without
   touching the confirm/cancel path.

### Evidence

Raised by Nancy during review of AG-Sept PR3a, 2026-08-05, after the ownership parameter pushed
the signature to 258 columns and it was wrapped in `0bf2f8d`. Wrapping made it legible; it did
not make it good, and the length was the symptom rather than the debt. PR3a made the shape
marginally worse by one parameter and one path, which is what moved it from a shape someone
might tidy to one worth recording.

## 7. DEBT-5 — no automated line-length guardrail

### What it is

Nothing in `make ci` bounds line length. **gofmt does not do this and never will** — it
normalises indentation, alignment and spacing, and has no opinion on where a line wraps, by
deliberate Go design. `.golangci.yml` enables the standard bundle plus the gofmt formatter, and
no length linter (`lll`, `golines`) is configured.

As measured at AG-Sept PR3a:

| Metric | Value |
|---|---|
| Code lines over 110 columns | 104 |
| Median of those | 122 |
| 90th percentile | 140 |
| Longest | 205 — `internal/service/service.go`, the `commit` signature |

The counts are a snapshot and will drift with every change; re-measure rather than trusting
them:

```sh
cat $(git ls-tree -r HEAD --name-only | grep '\.go$') \
  | awk 'length > 110 && $0 !~ /^[[:space:]]*\/\// {c++} END {print c}'
```

Six of them are function signatures; the rest are mostly long error strings, which wrap badly
and are the less interesting half. Signatures are where it bites: `commit` takes seven
parameters on one 205-column line, and a reader has to count commas to find the one they want.

**This is not a correctness debt.** Its cost is reviewer attention — spent noticing formatting
that a linter would have caught, and not spent on the logic. It has already been paid twice in
one review: `lockByReservation` reached 258 columns before a human spotted it, and `commit` was
found by eye immediately after.

### Why it is this way

Recorded in `.golangci.yml` itself, and it was the right call:

> The linter set is deliberately conservative for a young repository … Stricter style linters
> (e.g. revive) can be layered in once a green baseline is observed in CI. Keep additions
> incremental so a new check never silently blocks unrelated work.

Turning on `lll` today would fail CI on ~104 pre-existing lines across most packages. That is not
an incremental addition; it is a repo-wide reformat wearing a linter's clothes, and it would land
in whichever PR happened to enable it — exactly the "silently blocks unrelated work" the config
warns against.

### Why it is acceptable today

- Nothing about it can produce a wrong answer. Every invariant is pinned by tests that do not
  care how the source is wrapped.
- The repository has one regular author and a review step that has, empirically, caught the two
  worst instances.
- The distribution is not pathological: the median over-limit line is 122 columns, which is long
  but readable. It is the tail that is the problem, not the shape of the whole.

### Trigger — when it stops being acceptable

Any one of:

1. **Before the repository goes public.** The code becomes the artifact being read by people
   with no context and no review conversation to fall back on, and a 205-column signature is a
   worse first impression than the code deserves. This is the strongest trigger and it has a
   date attached to it — see [`../pre-public-checklist.md`](../pre-public-checklist.md).
2. **A second regular contributor.** The convention currently lives in review comments and
   nowhere else; transmitting it by correction does not scale past one person.
3. **The count grows rather than shrinks** across two consecutive milestones. Today's 103 is a
   stock to work down; a stock that grows means review is no longer catching it.

### What a fix must preserve

- **CI must not turn red on unrelated work.** Whatever limit is chosen, the pre-existing lines
  are dealt with in the same change that introduces the check, or excluded explicitly with the
  exclusion recorded rather than left to accumulate.
- **Error strings must stay greppable.** Splitting a message across concatenated literals is the
  usual remedy and it breaks `grep` for the whole sentence. A limit that forces every long
  message to be split trades one readability problem for another; the check should be able to
  ignore string-heavy lines, or the limit set high enough not to force it.
- **No automatic rewriter in CI.** `golines` can reformat mechanically, but a tool that rewraps
  signatures unattended will produce diffs nobody reviewed in files nobody touched.

### Options, none decided

1. **`lll` at 120**, with the existing ~104 lines fixed in the enabling PR. Clean end state; the
   enabling PR is large and touches almost every package.
2. **`lll` at 120 with `nolint` on the pre-existing lines**, worked down opportunistically.
   Small enabling PR, but the exclusions are a second register to maintain and tend to become
   permanent.
3. **Limit new and changed lines only**, via a review-time check rather than a linter — the
   check this session performed by hand: diff added lines against the branch point and flag
   anything beyond the repo's existing maximum. Catches the regression without a reformat, and
   is the cheapest thing that would have caught both instances above.
4. **Leave it to review and record the convention** in the contributing notes, so it is at least
   written down once rather than transmitted by correction.

### Evidence

Raised by Nancy during review of AG-Sept PR3a, 2026-08-05, on finding the `commit` signature at
205 columns immediately after `lockByReservation` had been wrapped from 258 in `0bf2f8d`. The
second find is the one that made it a debt rather than an incident: the first could be called
carelessness in a single PR, but two in one review is the absence of a guardrail. `commit` itself
is pre-existing and untouched by PR3a — it is cited as the current worst case, not as something
that PR introduced.

## 8. DEBT-6 — `res` denotes three unrelated types

### What it is

The identifier `res` appears 40 times in non-test code and 116 times in tests, and it does
not mean one thing:

| Meaning | Where | Rename would be |
|---|---|---|
| `domain.Reservation` | `service/service.go`, `postgres/tx.go`, `inmem/inmem_test.go` | `reservation` |
| `reconcile.Result` | `reconcile/reconcile.go`, `reconcile_test.go`, `servertotals_test.go` | `result` |
| a reserve `Response` | `loadgen/workload.go` | `reserveResponse` |
| `domain.Result` from a service call | `reconcile_integration_test.go` | `result` |

A reader moving between `service.go` and `reconcile.go` carries the wrong expansion into
the second file, and nothing on the line corrects them — both read `res.Checks` or
`res.State` as if the name told you what that was.

### Why the obvious fix is not obvious

This is the part worth recording, because the remedy looks like a one-line regex and is
wrong in **three** independent ways:

1. **The expansion differs per file.** A single `\bres\b` → `reservation` pass is wrong in
   `reconcile` and `loadgen`, and it would still compile — `res.Checks` renamed to
   `reservation.Checks` is valid Go against a `Result`, just a lie.
2. **14 of the matches are string literals, not identifiers** — `"res-1"`, `"res-doomed"`,
   `fmt.Sprintf("res-%d", …)`. These are reservation-ID *fixtures* and an ID prefix
   convention, nothing to do with variable naming. A word-boundary rename rewrites them
   silently, and it stays green as long as each file is internally consistent — until one
   file's fixture is referenced from another that expanded it differently.
3. **At least one is a struct field, not a local**: `seqIDGen{ res, bok int }` in
   `postgres/main_test.go`, where `res` counts minted reservation IDs and `bok` counts
   booking IDs. Renaming the field is a different change from renaming a local, and `bok`
   shows the same habit one field along.

So the safe-looking mechanical fix is the dangerous one, and the manual fix is 156
occurrences across 15 files.

### Why it is this way

Each site is locally defensible: in a five-line scope holding one reservation, `res` reads
fine, and the same is true of the `Result` in `reconcile`. **The collision is emergent** —
no single change created it, and no single reviewer saw two of them side by side. It is the
cross-file property that is the debt, not any one of the names.

### Why it is acceptable today

- No correctness risk: the compiler resolves the types, and every invariant is pinned by
  tests indifferent to local naming.
- The cost is a reader's attention, and it is bounded — each occurrence sits in a short
  scope where the declaring line is visible.
- The fix is disproportionate right now: 14 files, two of them heavily touched by open
  branches (`agent/ag-sept-pr3a-placement`, `agent/ag-sept-pr3bc-ambiguity`), so it would
  conflict with work under review for a benefit no reader has yet asked for twice.

### Trigger — when it stops being acceptable

Any one of:

1. **Someone reads the wrong expansion into a review comment or a bug report.** One instance
   of a human being actually misled converts this from a tidiness item to a defect risk.
2. **A fourth meaning appears.** Three is a coincidence to be worked down; four is a
   convention forming, and conventions are harder to reverse than habits.
3. **When a package is already being substantially rewritten.** `reconcile` is due
   authority-aware changes in PR3b (`ag-sept-plan-new.md` §6.5). Renaming inside a package
   that is being reworked anyway costs almost nothing and conflicts with nothing.

That third clause is the intended route: **this is three renames, not one**, and each should
travel with work already happening in its package rather than as a sweep.

### What a fix must preserve

- **String-literal fixtures must not move** unless every reference moves with them.
  `"res-1"` is an identifier value; it is not the variable-naming problem and should be
  left alone or changed deliberately as its own decision.
- **Each file gets the expansion its type deserves**, verified by reading the declaration —
  not by one pattern applied everywhere.
- **No mixed state within a package.** A package half-renamed is worse than either end,
  because the reader can no longer assume the name means anything at all.

### Options, none decided

1. **Opportunistic, per package, as each is next substantially edited** — the trigger's
   third clause. Slowest, cheapest, no conflicts. Preferred unless something forces the
   issue.
2. **One rename PR per meaning** — three small PRs, each internally consistent and
   reviewable. Clean, but three rounds of conflict against open branches.
3. **Leave it and rely on review** to stop new instances, accepting the existing ones. Does
   nothing for the reader who hits it tomorrow.

### Evidence

Surveyed during AG-Sept PR3a review, 2026-08-05, after Nancy's feedback that short local
names are a readability problem for humans. The rename was started and **stopped by Nancy on
seeing the survey** — the right call: what looked like renaming one variable in one file was
156 occurrences across 15 files with three different correct answers. Recorded so the next
person to reach for the regex finds the survey rather than repeating it.

## 9. DEBT-7 — implementation records that still read as scope notes

### What it is

The three documents under [`../development/implementation/`](../development/implementation/)
were moved out of `docs/planning/` and re-headed on 2026-08-07. They were not rewritten. They
still carry the shape of the scope notes they were: §1 quotes the plan's exit gates forward as
intent, the "What PR-n delivers" tables list what was *planned*, and budgets appear as
estimates. Only §6d of [`ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md) was
written as a record of what was built.

Residual wording elsewhere still calls them scope notes:
[`ag-sept-plan-new.md`](ag-sept-plan-new.md) §0 and §4,
[`horizontal-database-authority.md`](../design/horizontal-database-authority.md) §9, and the
PR2 frontier report.

### Why it is this way

The directory convention arrived after the work it classifies. Moving the files cost one commit
and fixed the taxonomy immediately; rewriting three documents covering two merged PRs and one in
flight is a different job, and doing it inside an open review would have churned the branch the
review was reading.

### Why it is acceptable today

PR1 and PR2 are merged, and their conclusions live where a reader looks for conclusions — the
measurement reports and the plan's own per-PR entries — not in these records. The
forward-looking sections are visibly forward-looking: they quote the plan verbatim and say so.
No document currently cites a "delivers" row as evidence that something shipped.

### Trigger — when it stops being acceptable

- When one is next materially updated. This is the convention's own rule, already stated in
  [`../development/implementation/README.md`](../development/implementation/README.md).
- When PR3c closes and `ag-sept-pr3.md` has to state outcomes for three PRs rather than a
  delivery plan for two.
- **Immediately, if anyone cites a "What PR-n delivers" row as evidence that something
  shipped.** That is the actual failure mode: in a directory named `implementation/`, a table
  of planned deliverables is indistinguishable in form from a record of built ones.

### What a fix must preserve

- **Section numbers.** [`ag-sept-plan-old.md`](ag-sept-plan-old.md) §5.6 and §5.6.1,
  [`ag-sept-plan-new.md`](ag-sept-plan-new.md) §5.5 and §5.6,
  [`container-topology.md`](../operations/container-topology.md) §6a and §6b,
  [ADR-0003](../decisions/0003-deployed-artifact-identity.md) §6d, and
  `test/scripts/sweep.sh` §5.4 all cite into these documents. Renumbering silently invalidates
  a shell comment that nothing tests.
- **The historical statements about what these documents were.** `ag-sept-pr1.md` §1 records
  that an earlier revision misquoted its own exit gate, and `ag-sept-pr2.md` §5.8 cites "PR1's
  own scope note" for quoting a range that matched neither its table nor its artifact. Both are
  accurate about the review round of 2026-08-03. Rewriting them to say "record" would falsify
  the history these documents exist to keep.
- **Estimates stay labelled as estimates.** Turning "2.5 days budgeted" into "2.5 days spent"
  without evidence would manufacture a measurement, which is the one thing this repository
  must not do to itself.

### Options, none decided

1. **Opportunistically, per record, as each is next materially updated** — the convention's
   stated rule. Cheapest, no churn, but leaves mixed state for as long as it takes.
2. **One pass over all three when PR3c closes**, when PR3's outcomes are finally knowable and
   PR1/PR2 have been stable long enough that rewriting them is bounded.
3. **Leave them, and let the `Type:` header carry the distinction.** Does nothing for a reader
   who opens §2 and reads a plan as a result.

### Related

[`../design-notes/horizontal-database-authority.md`](../design-notes/horizontal-database-authority.md)
is now a pointer with no inbound references left outside its own directory. It can be deleted
whenever link stability for external bookmarks stops mattering — the same class of transitional
artifact, kept deliberately rather than forgotten.

### Evidence

Raised 2026-08-07 during AG-Sept PR3b review, in the change that moved the records out of
`docs/planning/`. The move was mechanical and verified by resolving every relative link in
`docs/`; the conversion was deliberately not attempted in the same change, and this entry
records that as a decision rather than leaving it to be rediscovered as an inconsistency.
