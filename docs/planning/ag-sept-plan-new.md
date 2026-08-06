# AG-Sept — Measured scale-out and horizontal database authority

**Status:** Draft v0.5 — normative for all remaining AG-Sept work
**Created:** 5 August 2026, superseding [`ag-sept-plan-old.md`](ag-sept-plan-old.md) (v0.4, 31 July 2026)
**Delivery window:** August 2026
**Development budget:** 19.5 focused development days for the milestone, of which 5.5 are spent (PR1, PR2, PR3a) and 14.0 remain — followed by 2–3 days for reruns, review, refinement, documentation, and public-release preparation
**Predecessor:** AG-M1 — correct transactional core and end-to-end service path

## 0. What changed from v0.4, and why

v0.4 planned one service baseline, then stateless replicas against one PostgreSQL authority,
then a conditional AWS deployment. **PR2's measured result changed the order of the
investigation.** The single-instance frontier is set by PostgreSQL, not by `alloca-go`, which
retained substantial application-compute headroom
([`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md)).
Adding stateless replicas can raise throughput while database concurrency stays below that
frontier; it cannot move the frontier itself.

Four changes follow.

1. **Horizontal database authority becomes the milestone's primary work**, ahead of stateless
   replica scaling. Its design is
   [`../design-notes/horizontal-database-authority.md`](../design-notes/horizontal-database-authority.md),
   which phases the route: Phase 1 composes independent organisation-home authorities and
   supports every booking whose participating organisations resolve to the same authority;
   Phase 2 adds a cross-authority protocol and is deliberately deferred beyond AG-Sept.
   **Only Phase 1 is in scope here.**
2. **The AWS path is dropped** (§10). Its budget returns to local P0 work under v0.4 §4's own
   reallocation rule. AG-Sept closes with local-container evidence and no published capacity
   number, which the v0.4 descope order already accepted as a valid outcome.
3. **The local scale-out experiment survives intact** but moves behind the database work as
   PR4. v0.4 PR3's obligations — the recommended operating capacity number, PR2's falsifiable
   scale-out prediction, the mandatory connection-budget control, and the required PostgreSQL
   and node exporters — are carried forward in full rather than dropped (§14 PR4). Nancy's
   call, 2026-08-05: the milestone should leave the system scalable on both axes, even if only
   on local containers.
4. **The budget rises from 10 development days to 19.5** — 4.5 spent, 15 remaining — allocated
   in §4. Nancy's approval, 2026-08-05.

Sections carried forward from v0.4 keep their numbers, because PR1's and PR2's scope notes,
`load-harness.md`, `environment.md`, and the PR2 report cite them. Where a section's content
changed, the change is stated in the section rather than hidden by renumbering.

## 1. Executive intent

AG-M1 established the correctness foundation: Alloca-Go has a runnable HTTP service,
PostgreSQL-backed transactional authority, explicit outcome semantics, idempotency, reservation
expiry, schedule non-overlap, tests, CI, and an observation boundary.

AG-Sept converts that correctness work into measured distributed-systems evidence.

The milestone answers one connected question:

> How does a correctness-first stateful service behave as load, writable database authority,
> and stateless replica count increase; where does each axis stop helping; and what
> architecture follows from the evidence?

The intended progression is:

1. establish a reproducible single-instance capacity baseline — **discharged by PR2**;
2. compose independent writable database authorities while preserving every accepted
   transaction semantic, and prove correctness and failure isolation across them;
3. run multiple stateless application replicas against one authority, and test PR2's
   prediction about what replicas do to a database-bound frontier;
4. compare dispersed traffic with traffic concentrated on one transactional authority;
5. identify the database, connection, compute, or authority boundary that limits each workload;
6. document the distributed-system shape implied by the measurements;
7. extract a service only if doing so demonstrates a real consistency, failure, or scaling
   boundary.

Containers are the deployment substrate. Kubernetes and AWS are not subjects of this milestone
(§9, §10). The milestone succeeds by producing valid evidence and a defensible architectural
conclusion, not by touching the largest number of infrastructure technologies.

## 2. Goal and exit statement

AG-Sept proves how Alloca's correctness authorities behave under load, under horizontal
application scale, and under horizontal database authority composition.

At completion, the repository should support the following evidence-backed statement:

> Alloca began as a modular monolith because its central problem is transactional correctness,
> not service decomposition. After proving the invariants, the project measured one application
> instance and found the limiting subsystem to be the database rather than the application.
> It then composed independent organisation-home PostgreSQL authorities, preserving every
> accepted transaction semantic on the local path, and proved correctness and failure isolation
> across them. Stateless replicas were measured separately against one authority, so gains from
> additional compute are not confused with the serialization limit of one slot, one identity,
> or one writer. The results were then used to decide which future boundaries justify
> independent deployment.

The measurements may reject an initial performance or scaling hypothesis. A negative or
surprising result is a valid milestone outcome when the experiment is reproducible, the
generator is ruled out as the bottleneck, correctness remains intact, and the limiting
mechanism is identified.

**What AG-Sept will not be able to say.** Every run is co-resident on one workstation: service,
generator, telemetry stack, and both PostgreSQL authorities share one 10-vCPU WSL2 allocation.
No capacity multiplier — from replicas or from authority composition — is publishable from this
environment. §6.3's separate-generator rule stands and is honoured by labelling, since the
compute that would satisfy it is no longer funded (§10).

## 3. Scope principles

### 3.1 Measure mechanisms before realistic composites

AG-Sept uses simplified controlled workloads inspired by synchronized-release and hot-resource
patterns. It does not reproduce a full product workflow.

The controlled workloads isolate one mechanism at a time:

- low-contention requests across many independent authorities;
- many users contending for one slot authority;
- concurrent schedule mutations contending for one identity authority.

A small synchronized release wave may combine these mechanisms after the controls are
established. It is not a replacement for them because a composite workload changes several
variables at once and is harder to interpret.

### 3.2 Correctness gates remain non-negotiable

No performance result is publishable if the run violates the AG-M1 invariants or produces
unreconciled outcomes.

At minimum, each measured run must preserve:

- slot capacity safety;
- one logical mutation per idempotency key;
- schedule non-overlap for one identity;
- total terminal-outcome classification;
- safe treatment of ambiguous outcomes;
- bounded request and database timeout behaviour.

**Extended for multiple authorities.** A run spanning several writable authorities must
reconcile outcomes against persisted state on *every* participating authority, and must not
treat sequential cross-database reads as one atomic snapshot. The quiesced verification rule is
in the design note §6 and is normative for PR3.

### 3.3 Three scaling axes, three separate claims

Adding application replicas increases stateless compute and concurrency. It does not remove the
serialization requirement of one hot slot or one hot identity, and it does not move the
frontier of one writable database authority.

Adding writable authorities composes independent capacity across organisations. It does not
make one organisation, one slot, or one identity parallel.

AG-Sept therefore reports scale efficiency separately for:

- dispersed independent authorities within one database;
- one hot slot authority;
- one hot identity authority or a mixed-identity workload;
- independent organisations distributed across several writable databases.

The normative capacity and scale-efficiency definitions remain owned by
[measurement-contract.md](../design/measurement-contract.md) §3 and §3.1. For one indivisible
hot authority, efficiency approaching `1/N` at `N` replicas is the expected serialization
ceiling, not evidence that the whole system fails to scale.

No single headline number may be presented as "Alloca scales by X" across these different
mechanisms.

### 3.4 Implementation remains evidence-led

This plan defines questions, gates, workload shapes, required evidence, and scope limits. It
deliberately does not freeze:

- the load-generator library or exact internal structure;
- exact concurrency, rate, or duration sweep values;
- Prometheus client selection;
- dashboard layout;
- application and database resource sizes;
- the container orchestration mechanism, provided it is reproducible;
- any service boundary before evidence supports it.

Implementation PRs should choose the smallest mechanism that satisfies the relevant gate and
leaves the experiment reproducible.

### 3.5 Evidence labels for plan parameters

All proposed workload sizes, replica matrices, authority counts, durations, and resource values
in this plan are `[HYPOTHESIS]` until measured, unless they are explicitly identified as a fixed
planning budget or required test shape. Implementation reports must replace or retain that
label according to the evidence convention in
[measurement-contract.md](../design/measurement-contract.md) §2.

## 4. Time budget and priority

The development allocation is a planning constraint. 5.5 days are spent; 14.0 remain. **Half a day
is the unit**, here and in the scope notes: nothing is estimated well enough to distinguish 0.3
from 0.4, and finer granularity is false precision that invites its own overrun (Nancy's call,
2026-08-05).

| Workstream | PR | Allocated | Status |
|---|---|---:|---|
| Measurement harness and load generator | PR1 | 2.0 | spent 2.0 — merged `71914a4` |
| Single-instance frontier, with the diagnostic time-series minimum | PR2 | 2.5 | spent 2.5 — merged `0d40de4` |
| Placement, booking policy, and confirm/cancel ownership | PR3a | 3.0 | **done in 1.0 — merged `aa1e3a5`; 2.0 returned to contingency** |
| Multi-authority harness — topology, generator routing, authority-aware verification | PR3b | 2.5 | in progress, PR #14 |
| Multi-authority correctness and failure-isolation evidence | PR3c | 2.0 | remaining |
| Container and local scale-out — replicas, exporters, controls | PR4 | 4.5 | remaining, provisional envelope |
| Architecture conclusions and one justified boundary | PR5 | 1.5 | remaining |
| **Committed** | | **16.0** | 5.5 spent, 10.5 remaining |
| Unallocated contingency | | 3.5 | remaining |
| **Total milestone budget** | | **19.5** | unchanged |

**PR3a came in at 1.0 against 3.0, and the 2.0 goes to contingency rather than to scope.**
The figure recorded is the conservative one: implementation alone was about half a day, and
1.0 is what it cost including the design decisions and the re-planning around it. Being
generous to ourselves on our own favourable numbers is how estimates stop meaning anything.

The gain is **not** an invitation to widen PR3b or PR4. It is held for where this milestone
is most likely to need it — reruns, an investigation that does not resolve on the first
attempt, a hard problem that turns out to deserve more argument than a day allows. PR2's
unexplained ~2× excursion is the standing example of work that consumed far more than its
share, and nothing about PR3a going well makes that less likely to recur.

**Every PR is funded at what its scope costs.** No PR carries a deliberate shortfall, and no
part of §14 depends on the reserve to be reachable. This is the difference the increased budget
bought, and it is worth naming: v0.4 planned AWS experiments outside its own table, which is how
a plan overruns on day one.

**Why the database work is funded first.** Nancy's call, 2026-08-05: scaling design and
implementation take priority over capacity measurement, and database authority scaling comes
before service replica scaling. This is the direct consequence of PR2's finding that the
database, not the application, sets the frontier — measuring replicas harder against an
unchanged writer would refine a number the milestone has already explained.

**PR3a rose from 2.5 to 3.0 before implementation started, and that is the contingency working
as intended.** The design note's revision of 2026-08-05 settled the three contract questions
raised in its review, and one of them resolved towards *build it*: confirm and cancel gain an
explicit `UserRef` ownership check, which the earlier estimate priced as a decision rather than
a domain-contract correction with its own normative updates and tests
([`ag-sept-pr3-scope.md`](ag-sept-pr3-scope.md) §5.5). The half day came from contingency rather
than from another PR.

**The 3.5 unallocated days are contingency, not scope.** They are drawn on before §16's
descope order, and two things could plausibly claim them, in this order:

1. **PR2's unexplained ~2× excursions turning out to be reproducible and diagnosable** once
   PR4's node exporter can see them. That would be a real finding, and chasing it is worth more
   than another matrix cell.
2. **Group A of the PR2 deferral register** — the overload question, roughly 1.5 days (§14).
   It is the founding unreproduced question in `high-level-design.md` §1.1, and it is the one
   candidate here that is *new scope* rather than insurance. Adding it is Nancy's call, not a
   default — and PR3a's returned 2.0 makes it affordable for the first time, which is a
   reason to decide it deliberately rather than to let it drift in.

**Review depth is the throughput control, and it is Nancy's to set** (2026-08-05). The
implementation side of this milestone is not the constraint; the review step is, and it can
be traded for speed when momentum matters more than scrutiny. The consequence is stated
rather than left implicit: less detailed review moves the burden of catching errors onto
the implementation side's own verification — the mutation tests, the live SQL checks, the
re-measurement of quoted figures. Where that verification is weak, a lighter review does not
find it. PR3a's own record is the argument: three of nine review findings were comments of
mine asserting the opposite of the truth, and a pre-merge audit found six more stale claims.
Those were caught by review. Going faster means catching more of them before review.

Unspent contingency is not a licence to expand a PR. It returns to the reserve.

**The AWS budget is gone, not deferred.** v0.4 allocated up to 2.0 days to AWS plus a further
1.5 outside the table. Both are withdrawn under v0.4 §4's reallocation rule and returned to
local P0 work (§10). They are part — not the whole — of the 9.5 additional days this plan
adds; the rest is new budget Nancy approved on 2026-08-05 for the database authority work.

A further 2–3 days are reserved for:

- rerunning decisive experiments;
- validating negative controls;
- reviewing measurements and interpretations;
- correcting documentation;
- polishing diagrams and the repository entry point;
- checking public-disclosure suitability;
- preparing the project for external readers.

The time budget is a constraint, not an estimate to be expanded whenever a tool introduces
incidental complexity.

## 5. Required workload model

§5.1–§5.5 are carried forward from v0.4 unchanged except for §5.6, which is new.

### 5.1 Dispersed-authority control

Requests are distributed across many slots and many users so contention on any one slot or
identity is low.

Purpose:

- establish the general application and PostgreSQL frontier;
- measure the benefit of additional stateless replicas when transactions can proceed mostly
  independently;
- identify compute, pool, connection, or database saturation before one business authority
  dominates the run.

A possible `[HYPOTHESIS]` synthetic shape is:

- approximately 1,000 slots;
- hundreds or thousands of users;
- one reservation workflow per user;
- slot selection distributed across the full set;
- sufficient capacity or data reset discipline to avoid the run becoming primarily a sold-out
  test.

The exact figures are implementation parameters. The defining property is low contention and a
broad authority distribution.

### 5.2 Hot-slot control

Many distinct users attempt to reserve one slot, or a deliberately small set of slots, at
approximately the same time.

Purpose:

- expose the serialization frontier of the slot row that owns capacity;
- separate useful throughput from waits, business refusals, and timeouts;
- show why adding stateless API replicas cannot remove one shared transactional authority.

A possible `[HYPOTHESIS]` minimal shape is:

- one slot with capacity 20;
- 50–100 distinct users;
- synchronized reserve attempts;
- one unique idempotency key per logical request.

The run must distinguish admitted reservations, expected `no_capacity` refusals, timeouts, and
internal failures using the closed outcome model in
[measurement-contract.md](../design/measurement-contract.md) §4. `replay` is an orthogonal flag
and must be reported without modelling it as a peer terminal outcome.

Expected business refusals are valid completed outcomes but are not counted as successful
reservation goodput. Goodput follows [observability.md](../design/observability.md) §3.1 and
covers the mutation surface rather than informational reads such as `list_slots`.

### 5.3 Hot-identity control

One identity, or a small number of identities, attempts concurrent reservations across
overlapping slots.

Purpose:

- measure the cost and containment of the user-identity serialization authority introduced by
  AG-M1;
- demonstrate that serialization is scoped to one identity rather than global;
- distinguish identity-lock queueing from slot-capacity contention.

A possible `[HYPOTHESIS]` minimal shape is:

- one user identity;
- 20 overlapping slots;
- concurrent reserve attempts;
- exactly one admitted reservation;
- remaining domain answers classified as `schedule_conflict`, unless a bounded infrastructure
  outcome legitimately occurs.

Every hot-identity run must begin from a deterministic reset or fresh seed and assert that the
tested identities have zero live claims before load starts. Confirmed claims are not
expiry-reaped, so reusing contaminated fixtures can produce a plausible but meaningless
all-conflict rerun.

A stronger mixed version may run many independent identities, each producing a small
overlapping burst.

### 5.4 Simplified synchronized release wave

After the controls work, AG-Sept may add one small composite workload with `[HYPOTHESIS]`
parameters such as:

- around 40 slots released together;
- fixed capacity per slot;
- around 1,000 users;
- attempts beginning within a short release window;
- a deliberately skewed distribution so a few slots are hotter than average.

The first version should avoid complex browsing, alternative-choice loops, repeated retries, or
fallback activities. The workload is desirable but must not displace the controlled baselines,
the multi-authority correctness evidence, or the local scale-out experiment.

### 5.5 Explicit workload exclusions

AG-Sept does not require:

- a full fitness-club booking simulation;
- a full game inventory or shared-resource simulation;
- domain-specific names or confidential scenario details;
- user-behaviour modelling beyond the required authority distribution;
- a retry-storm model before baseline timeout behaviour is understood.

### 5.6 Multi-organisation dispersed control — new in v0.5

The dispersed control of §5.1 uses one organisation for both the slot and user sides, so every
PR1 and PR2 request was same-organisation and same-authority by construction. Phase 1 needs a
dispersed workload whose requests spread across **several organisations placed on different
writable authorities**.

Required properties:

- several organisations, assigned across authorities by the versioned placement map (§8.2),
  **including at least two organisations colocated on one authority**;
- `authority(slot_organisation_id) == authority(user_organisation_id)` on every generated
  booking request, which is the condition Phase 1 supports. It is deliberately weaker than
  organisation-identifier equality: a user registered with one organisation booking a slot owned
  by another is supported whenever the two are colocated, which is what keeps INV-13 exercised
  end to end;
- a reported organisation-to-authority distribution for every run, since equal organisation
  counts do not imply equal load.

**Cross-authority requests are a separate control, not a share of this workload.** Deliberate
policy refusals are cheap compared with a real booking, so mixing them into the supported
workload would flatter both goodput and latency. The refusal control is a bounded run of its own
and is reported as its own evidence class (design note §6.2).

A one-hot-organisation variant is required for the failure-isolation experiment: it shows that
one saturated or unavailable authority bounds its own organisations and no others.

## 6. Measurement substrate

§6.1, §6.2, and §6.5 are carried forward from v0.4 unchanged. §6.3 and §6.4 are amended.

### 6.1 Aggregated service metrics

AG-Sept should extend the observation boundary AG-M1 established with a minimal aggregated
recorder suitable for capacity experiments.

Required signals include:

- request count by closed-set terminal outcome;
- request-latency histogram;
- timeout, infrastructure-failure, and business-refusal counts;
- database pool state and acquisition duration;
- expiry-worker iteration outcomes and duration;
- process CPU and memory;
- relevant Go runtime signals;
- bounded replica identity where needed.

Metrics must not label by user, slot, reservation, organisation, idempotency key, request
identifier, arbitrary error text, or other unbounded request content.

**Authority identity is a bounded label and is permitted; organisation is not.** A shard-affine
service unit serves a fixed, small set of authorities — one — so an authority identifier is
bounded by topology in the way replica identity is. Organisation remains forbidden as a label
because it grows with tenants. Per-authority totals are therefore read from per-service scrapes,
which is sound precisely because §8.2 makes each service unit shard-affine.

### 6.2 Telemetry must not dominate measured latency

Before performance claims are made, AG-Sept must show that observation is not the primary
request bottleneck.

Acceptable approaches include a bounded local sink, a bounded asynchronous log sink with
explicit drop and shutdown behaviour, or another measured approach demonstrating low and
bounded request-path overhead.

### 6.3 External load generator

The load generator must:

- execute the required workload shapes;
- control concurrency and/or offered rate;
- synchronize starts where needed;
- issue valid idempotent requests;
- capture client-side latency and terminal outcomes;
- emit machine-readable summaries;
- run separately from the service for publishable claims;
- expose enough utilisation to rule out generator saturation;
- **route by the same versioned placement map the services use** (new in v0.5, §8.2):
  **mutations by `user_organisation_id`**, since user-home owns the schedule claim and the
  client idempotency scope and is therefore the stable home for a mutation and every replay of
  it; **reads by `slot_organisation_id`**, which is the authority that holds the rows they
  return.

Closed-loop sweeps are sufficient initially. Open-loop rate control is desirable where it
materially improves overload analysis.

**The separate-generator rule stands and will not be satisfied in AG-Sept.** v0.4 expected the
separate compute to arrive with the AWS deployment. §10 withdraws that. The rule is therefore
honoured by labelling: every AG-Sept run is a bounded local result, `quotability.level` stays
`local` by construction, and no run is presented as a published capacity claim.

### 6.4 Run manifest

Every quotable run must record:

- commit SHA and image tag;
- Go version and observed `GOMAXPROCS`;
- replica count and application resources;
- PostgreSQL version and configuration identity;
- pool size per replica and aggregate expected pool capacity;
- workload and dataset parameters;
- offered rate and/or concurrency;
- duration and warm-up;
- timeout budget and reservation TTL;
- generator location, resources, and utilisation;
- deployment topology and timestamp;
- **authority count, the routing/placement version, and the organisation-to-authority
  assignment the run used** (new in v0.5).

Secrets and private endpoints must not be committed.

**Manifest completion is staged, and the stages are renamed for v0.5.** The generator is an HTTP
client and cannot discover the service's shape, so the remaining fields are supplied in the PR
that first has something to say: service shape in PR2 (discharged — PR2 took the
operator-supplied count to zero by reading `/meta`), placement and authority identity in PR3b,
topology and image identity in PR3b, replica count and aggregate pool capacity in PR4. The
environment stage v0.4 assigned to the AWS PR does not arrive; §10 records why. The rule that
survives the staging is this section's own — **no run may be quoted as a capacity claim while a
field its topology requires is unpopulated.**

**Multi-service runs need one further rule.** When several service units serve one run, the
manifest records every unit's `/meta`, and the run is uncertifiable if the units disagree on
commit revision or report incompatible schema versions. One topology, one binary, one schema.

### 6.5 Correctness reconciliation

PR1 established a self-check used by every later measured run. At minimum it must reconcile:

- consumed slot capacity against admitted reservation mutations;
- distinct logical idempotency keys against committed mutations and replays;
- live claims against admitted reservations per identity and interval;
- every completed request against the closed terminal-outcome set, with `replay` folded in as
  an orthogonal flag rather than double-counted.

A run with unreconciled client totals, server totals, or persisted state is not quotable.

**Extended for multiple authorities in PR3b.** The contract, not the mechanism:

1. the run is quiesced, and any `unknown_replayable` mutation is resolved by replaying its own
   idempotency key before verification begins;
2. **local safety invariants are checked independently on each authority** — capacity, schedule
   non-overlap, idempotency, and lifecycle are all local properties of the rows one authority
   owns;
3. **persisted and server totals are aggregated across authorities and compared once** with the
   run's global client totals — once, not per authority, because the client's totals are a
   property of the run rather than of any one authority;
4. **each service unit's scrape pair is differenced independently before the sum is taken.**
   Differencing the sums instead would let one unit restarting mid-run vanish into another
   unit's counters, which is the one arithmetic error this contract exists to prevent;
5. the verdict aggregates without treating sequential cross-database reads as one atomic
   snapshot.

The architectural requirement is that a multi-organisation run must never compare one
organisation's persisted rows against the run's unpartitioned global summary. In particular,
looping the existing organisation-scoped entry point against the unchanged global report is
**not** a discharge of this contract — it would compare one organisation's rows with every
organisation's totals. The verifier's data structures, query factoring, and scrape aggregation
are otherwise PR3b's to choose (design note §6.3).

## 7. Single-instance baseline — discharged

The one-replica run is the control for every scale-out claim. **PR2 discharged this section.**
Its results, limitations, and the one deferred term are in
[`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md)
and [`ag-sept-pr2-scope.md`](ag-sept-pr2-scope.md) §5.6.

Two obligations survive into later PRs:

- the **recommended operating capacity** term, deferred by PR2 because two of its three
  components need more than one replica to be meaningful. It is PR4's (§14);
- the **±2× caveat** on every figure from this machine. PR4's node exporter is required rather
  than desirable, but installing it does not discharge the caveat: the caveat stands until
  instrumented reruns *explain* the excursions, *exclude* them, or *bound* them conservatively.
  An instrument that can see a thing is not yet an answer about it.

Peak observed throughput, SLO-safe capacity, recommended operating capacity, and scale
efficiency use the normative definitions in
[measurement-contract.md](../design/measurement-contract.md) §3 and §3.1 rather than local
restatements.

## 8. Horizontal scaling experiments

### 8.1 Connection-budget control

Carried forward from v0.4 unchanged, and still mandatory (§12.3).

Compare:

1. a roughly constant aggregate database connection budget as replicas increase; and
2. the original full pool size multiplied per replica.

This separates application-compute scaling from changed pressure on the shared database
admission boundary.

**Phase 1 changes the arithmetic, not the control.** Shard-affine service units open one pool
each (§8.2), so the aggregate against any one authority is `replicas_in_that_shard_group ×
pool_size`, not `replicas × authorities × pool_size`. The control is run per shard group.

### 8.2 Horizontal database authority — Phase 1

The design is owned by
[`../design-notes/horizontal-database-authority.md`](../design-notes/horizontal-database-authority.md).
This plan states only what AG-Sept must build and prove, and does not restate the design.

Phase 1 assigns each organisation one writable PostgreSQL home authority holding its complete
transactional state, supports booking wherever the user and slot organisations *resolve to* the
same authority — placement equality, not organisation-identifier equality — and keeps every
supported operation inside the existing single-database transaction. Cross-organisation booking
therefore survives Phase 1 unchanged whenever the two organisations are colocated, including in
the current one-authority deployment.

The required properties for AG-Sept:

- a **versioned, immutable placement map** for the duration of a run, used identically by every
  service unit and by the generator, and recorded in every artifact;
- **shard-affine service units** — one authority, one pool, readiness bound to that authority,
  and no fallback to another authority on failure;
- **server-side placement enforcement** — a unit rejects a request for an organisation it does
  not own, rather than trusting the caller or the generator to route correctly;
- **cross-authority booking represented as an explicit outcome**, not made structurally
  impossible, so Phase 2 remains reachable. Note which case this is: colocated
  cross-*organisation* booking is supported and must keep working (INV-13); only a booking whose
  two organisations resolve to *different authorities* is refused;
- **authority-aware verification** (§6.5).

### 8.3 Local multi-instance scale-out

Run `[HYPOTHESIS]` replica counts of 1, 2, and 4 against one PostgreSQL authority through a
local load-balancing path.

For dispersed traffic, determine whether throughput and goodput increase, how scale efficiency
changes, and where PostgreSQL or pool acquisition becomes dominant. **PR2 made a falsifiable
prediction here and it is a first-class result, not a by-product** (§14 PR4).

For hot-slot traffic, determine whether replicas improve useful throughput or merely add
waiters, refusals, and timeout pressure, while confirming unrelated authorities still progress.
For one indivisible hot slot, scale efficiency approaching `1/N` is the expected correct result.

For hot-identity traffic, confirm contention is scoped to the identity and unrelated identities
continue to progress.

**The replica matrix runs within one shard group.** Whether replicas scale application compute
is a single-authority question, and sharding does not change the answer. Running the full matrix
across both topologies would multiply measurement time without producing a new conclusion. One
composed run at the chosen replica count across both authorities shows the two axes together
(§11).

## 9. Container path

### 9.1 Container gate

The service must have a reproducible, production-shaped image with immutable experiment tagging,
environment-driven configuration, health probes, graceful termination, and no development-only
runtime tooling.

**The gate moves earlier in v0.5.** Phase 1 needs two service units and two databases from PR3b
onward. Running that topology as containers from the start is cheaper than converting it
mid-milestone, and it lets the topology and image-identity manifest fields land once (§6.4).

### 9.2 Kubernetes

Out of scope for AG-Sept. v0.4 §9.2 made a local Kubernetes gate strongly desirable as
preparation for EKS; with the AWS path withdrawn (§10), it prepares for nothing this milestone
delivers. Container orchestration is whatever is smallest and reproducible — Compose is
sufficient.

### 9.3 Scope ceiling

AG-Sept does not require a service mesh, custom operators, multi-cluster management, GitOps
platform, custom-metric autoscaling, elaborate Helm framework, production secret platform,
PostgreSQL in Kubernetes, or a full tracing stack.

## 10. Deployment beyond the workstation — withdrawn from AG-Sept

v0.4 §10 and §11 planned an AWS deployment — ECR, EKS, RDS, an AWS load-balancing path, and
**separate EC2 generator compute** — with decision gates, a smoke gate, and an experiment
matrix. **All of it is withdrawn from AG-Sept** under v0.4 §4's reallocation rule, and its
budget funds the database authority work (§4).

Two consequences, both recorded rather than absorbed:

1. **The separate generator compute never arrives.** It is the one thing §6.3 requires for a
   publishable capacity claim. AG-Sept therefore closes with a bounded local frontier and no
   published capacity number. The v0.4 descope order already accepted this as valid, and it is
   more honest than promoting a co-resident measurement.
2. **No cloud, managed-database, or network boundary is measured.** Any statement about how
   Alloca behaves on managed infrastructure remains unevidenced and must not appear in the
   architecture conclusion.

The AWS topology, gates, and matrix remain readable in
[`ag-sept-plan-old.md`](ag-sept-plan-old.md) §10 and §11 for whichever milestone picks them up.
This is a deferral of the work, not a decision that it lacks value.

## 11. Local experiment matrix

Replacing v0.4's AWS matrix. A recommended `[HYPOTHESIS]` minimum:

| Experiment | Topology | Workloads |
|---|---|---|
| Phase 1 supported correctness | 2 authorities × 1 replica each | multi-organisation dispersed, colocated cross-organisation booking, one-hot-organisation, wrong-`UserRef` confirm and cancel |
| Cross-authority refusal control | 2 authorities × 1 replica each | bounded cross-authority reserves — its own evidence class, never mixed into a goodput or latency comparison |
| Phase 1 failure isolation | 2 authorities × 1 replica each | multi-organisation dispersed, with one authority taken down, ambiguous mutations replayed, and the authority restored |
| Replica matrix | 1 authority × 1, 2, 4 replicas | dispersed at 1/2/4; hot slot at 1/4; hot identity at 1/4 |
| Connection-budget control | 1 authority × 2, 4 replicas | dispersed, both budget configurations |
| Composed run | 2 authorities × chosen replica count | multi-organisation dispersed |

Scale efficiency must compare like-for-like SLO-safe goodput, or another clearly named
like-for-like point when SLO-safe capacity cannot be established precisely.

Each result must state the believed limiting mechanism and supporting evidence. Possible
boundaries include application CPU, database pool acquisition, PostgreSQL connection capacity,
transaction or lock wait, one slot authority, one identity authority, one writable authority,
telemetry overhead, generator saturation, and shared-workstation contention.

## 12. Negative controls

Carried forward from v0.4 unchanged, with one addition.

### 12.1 Response-validation control — mandatory

Deliberately supply an invalid HTTP-status and domain-outcome combination and prove that the
load harness rejects the response and invalidates the run.

This control is required by [measurement-contract.md](../design/measurement-contract.md) §5 and
is not descopable. It proves that response validation is active before goodput or capacity can
be quoted.

### 12.2 Generator bottleneck control — mandatory

Deliberately constrain the generator and show how the apparent frontier changes. Publishable
runs must then demonstrate adequate generator headroom.

### 12.3 Connection-pool multiplication control — mandatory

Compare a controlled aggregate connection budget with a configuration where each added replica
receives the original full pool size.

### 12.4 Resource-limit control — desirable

Where time permits, constrain application CPU and confirm that the observed frontier changes
predictably.

### 12.5 Misrouting control — mandatory, new in v0.5

Deliberately send a request for an organisation to the service unit that does not own it, and
prove the unit refuses it rather than writing the row. Without this control, "no supported
request reached the wrong authority" is a property of the generator's routing table rather than
of the system, and the placement invariant is untested.

The control asserts a specific answer: **`invalid_request`, refused at the transport edge, plus
the misroute counter** — never a `business_refusal`. A misroute is a routing or deployment fault,
not a domain answer, and it must not be recorded as the user's durable domain outcome on an
authority that does not own them. `api-surface.md` §2.6 states the client-visible half.

## 13. Architecture conclusion

The final architecture document should show external clients and generator, load balancing,
stateless API replicas, the placement boundary, independent PostgreSQL authorities, expiry
workers, metrics, and deployment boundaries.

It must connect measured behaviour to AG-M1's authority model, extended by Phase 1's placement
model:

```text
slot row               owns capacity
user identity row      serializes schedule mutation
claim relation         proves schedule validity
organisation home      selects the one writable authority that owns all of the above
```

It should explain what remains safe across replicas, why a hot slot is not parallelized by more
API instances, why one identity serializes while unrelated identities remain independent, why
independent organisations compose across writers while one organisation does not, and which
boundaries are only process-local optimizations.

Potential future components may include asynchronous event publication, reporting/read models,
notifications, telemetry ingestion, authority-aware admission, and maintenance for elapsed
claims. For each candidate, record its consistency requirements, failure semantics, evidence for
separation, and operational cost.

### 13.1 Optional service split

Do not split the reservation transaction merely to claim microservices.

The most defensible optional extraction is an asynchronous event or reporting consumer fed
through a transactional outbox, demonstrating at-least-once delivery, idempotent consumption,
and independent failure and scaling. Implementation is a stretch goal; a complete design is
sufficient when measurements consume the budget.

The expiry worker is not an automatic microservice candidate because it shares the same domain
and transaction rules and duplicate workers are already correctness-safe.

## 14. PR sequence

This section assigns the requirements above to implementation PRs. The earlier sections remain
normative for the technical details; these scopes make ownership, evidence, and exit conditions
explicit so required work does not fall between PRs.

PR1 and PR2 are complete and their scopes are unchanged from
[`ag-sept-plan-old.md`](ag-sept-plan-old.md) §14. They are summarised here only for sequence.

### PR1 — Measurement substrate and load harness — merged

2.0 days. Metrics recorder, telemetry-overhead measurement, reset and seed tooling, external
generator, run manifest, persisted-state verifier, response-validation control, operator
documentation. Scope note: [`ag-sept-pr1-scope.md`](ag-sept-pr1-scope.md).

### PR2 — Single-instance frontier — merged

2.5 days. Prometheus retention path, diagnostic panels, one-instance sweeps for all three
controlled workloads, telemetry comparison, generator-bottleneck control, frontier report. Scope
note: [`ag-sept-pr2-scope.md`](ag-sept-pr2-scope.md). Result: the frontier is set by PostgreSQL,
not by `alloca-go`; this is what reordered the milestone (§0).

### PR3a — Placement, booking policy, and confirm/cancel ownership

**Indicative budget:** 3.0 days. The three contract questions raised in review of the design
note were settled in its revision of 2026-08-05
([PR #12](https://github.com/nancysworld/alloca-go/pull/12)), and one of them resolved towards
building rather than documenting; §4 records where the extra half day came from.

**Scope:**

- implement the versioned placement map of §8.2: loading, validation, immutability for a run,
  and a startup gate that fails on an unassigned organisation, a doubly-assigned organisation,
  or an incompatible schema version;
- bind each service unit to exactly one authority and its assigned organisation set;
- **enforce placement server-side** — reject a request for an organisation the unit does not
  own, and prove it with the §12.5 control;
- implement the Phase 1 booking policy on **resolved authorities, not organisation identifiers**
  (§8.2), so colocated cross-organisation booking keeps working and INV-13 stays exercised;
- add `cross_authority_unsupported` as a normative domain reason, through every closed set it
  touches: the domain reason set and its validation, the HTTP status mapping,
  [`api-surface.md`](../design/api-surface.md), the invariant register in
  [`transaction-semantics.md`](../design/transaction-semantics.md), the
  [`measurement-contract.md`](../design/measurement-contract.md) §4 taxonomy, the metric label
  allowlist, and reconciliation;
- **add the explicit `UserRef` ownership check to confirm and cancel**, returning the existing
  `unknown_target` on mismatch. The service does not validate ownership today, so this is a
  deliberate domain-contract correction with its own normative updates and tests — and it is
  what stops the result of a wrong identity depending on whether two organisations happen to be
  colocated;
- extend `/meta` with authority identifier, routing version, and schema version;
- add the placement invariants to the invariant register with discriminating tests, each proved
  to fail without the property it asserts.

**Evidence:** the closed-set and contract changes with their tests; the misrouting control; the
ownership check proved to change behaviour in both placements identically.

**Exit:** a service unit cannot be started against an inconsistent placement map or an
incompatible schema; a misrouted request is refused rather than written; the booking policy is
one named outcome across every closed set that must know about it; a wrong `UserRef` gets the
same answer whether or not the organisations are colocated; and no accepted AG-M1 invariant has
been weakened to make any of it fit.

**Not in PR3a:** multi-authority runs, generator changes, verifier changes, any measurement.

### PR3b — Multi-authority harness

**Indicative budget:** 2.5 days.

**Scope:**

- containerise the service to the §9.1 gate and stand up the two-authority topology
  reproducibly, with per-authority migration as a separate step (ADR-0002 — serving replicas
  never migrate);
- teach the generator to route by the placement map — **mutations by `user_organisation_id`,
  reads by `slot_organisation_id`** (§6.3) — and add the multi-organisation dispersed and
  one-hot-organisation workloads of §5.6, plus the bounded cross-authority refusal control as a
  separate workload rather than a share of the dispersed one;
- retain the idempotency keys of any `unknown_replayable` response so PR3c can replay them after
  an authority is restored (§6.5). Keys are already derived deterministically per workload, so
  this is a small register and a resolution pass, **not** the retry-on-timeout load control of
  the PR2 deferral register's group A, which stays out of scope;
- record placement, authority count, and assignment in the manifest, and require every
  participating unit's `/meta` to agree on revision and schema before a run is certifiable
  (§6.4);
- extend the verifier to accept the placement map, run each existing check against the authority
  that owns the organisation, sum server totals across per-service scrapes, and emit one
  aggregated verdict that records which authorities it read;
- populate the §6.4 topology and image-identity fields.

**Evidence:** one reconciled two-authority run with an aggregated verdict, an agreeing
multi-service manifest, and the placement recorded in the artifact.

**Exit:** a multi-authority run is reproducible from a version-controlled topology, produces an
aggregated correctness verdict naming every authority it read, and is refused certification when
the units disagree.

**Not in PR3b:** replica scaling, exporters, capacity claims, per-authority client-side
attribution — per-authority work is read from per-service scrapes instead (§6.1).

### PR3c — Multi-authority correctness and failure isolation

**Indicative budget:** 2.0 days.

**Scope:**

- seed several organisations across both authorities, at least two of them colocated, and run
  the §11 correctness experiments;
- prove the supported-workload gates: same-organisation and **colocated cross-organisation**
  booking both succeed through the local transaction and preserve global schedule non-overlap;
  confirm and cancel with a wrong `UserRef` return `404 unknown_target` regardless of
  colocation; no supported request reaches the wrong authority; and capacity, schedule,
  idempotency, lifecycle, and outcome reconciliation pass independently on every authority;
- run the **cross-authority refusal control** as its own bounded evidence class: user-home
  records `cross_authority_unsupported` before any slot-authority work or booking-state
  mutation, replay returns the recorded refusal, no reservation, claim, booking, or slot-side
  row is created, and the slot authority receives no request. Reported separately, so deliberate
  refusals never enter a goodput or latency comparison;
- run the failure-isolation experiment — take one authority down, show only its organisations
  are affected and that the other continues to serve, then restore it and show recovery needs no
  writes on the other authority;
- **report the failure experiment as its own evidence class.** A down authority may produce
  `internal_failure` (unreachable before the transaction begins), `timeout_db` (an acquisition,
  lock, or statement bound fires), or **`unknown_replayable`** (the connection is lost while the
  commit acknowledgement is in flight). After restoration, ambiguous mutations are resolved by
  replaying their own idempotency keys *before* the final correctness verdict. Affected and
  unaffected populations are reported separately, and the run is not judged against the
  aggregate SLO gates that govern a healthy capacity run;
- **if a deliberately timed mid-commit connection loss can be produced, discharge the rest of
  INV-21** — the register's longest-standing "not directly proven" entry. PR3b closed the
  narrower half: a `COMMIT` attempted on a session already terminated is proven to classify
  as `unknown_replayable` and to leave no row. What remains is the fault the entry was
  actually named for — the connection lost *while* the commit or its acknowledgement is in
  flight — which needs something interposed between client and server rather than a
  terminated backend. A generic authority shutdown does not claim that proof; only the
  targeted fault does, and the mechanism is implementation's to choose;
- verify under the quiesced consistency rule (§3.2, §6.5);
- report, with the organisation-to-authority distribution each run measured.

**Evidence:** a multi-authority correctness report with per-authority verdicts, the
failure-isolation result, and the routing and schema metadata for every run.

**Exit:** Phase 1 correctness and failure isolation are demonstrated on independent writable
authorities; every accepted transaction semantic on the supported path is unchanged; and no
result claims a throughput multiplier from this workstation.

**Not in PR3c:** cross-authority booking, rebalancing, replica scaling, any capacity-composition
claim.

### PR4 — Container and local scale-out

**Indicative budget:** 4.5 days — 0.5 for replica orchestration, 1.0 for the exporters and
dashboard extension, 1.0 for the §11 replica matrix, 0.5 for the connection-budget control, 0.5
for the prediction test and the operating-capacity number, 0.5 for the composed run, and 0.5 for
the report.

**This is a provisional envelope, not a committed scope (Nancy's call, 2026-08-05).** The
decision order is: freeze Phase 1 → implement and measure PR3 → review its findings → re-scope
PR4 from that evidence. The budget is reserved now so the milestone can be planned; what it
buys is decided after PR3 reports, because PR2 already demonstrated once that evidence can
change which experiment is worth running. The four obligations carried from v0.4 PR3 — the
operating-capacity number, PR2's falsifiable prediction, the connection-budget control, and the
exporters — survive that re-scoping as obligations; their order, depth, and matrix do not.

**Scope:**

- provide the smallest reproducible local load-balancing or orchestration path for one, two,
  and four replicas within one shard group;
- **add a PostgreSQL exporter and a node exporter, and treat both as required rather than
  desirable.** PR2 could name its bottleneck only from hand-driven `pg_stat_activity` sampling,
  and scale efficiency cannot be honestly computed while the shared authority is the one
  component with no instrumentation. The node exporter is the only instrument that can see
  PR2's unexplained ~2× excursions, which slow service, database, and generator together;
- **test PR2's falsifiable scale-out prediction as a first-class result.** PR2's pool ladder is
  a scale-out experiment in disguise — from the database's side, two replicas at pool 10
  resemble one replica at pool 20 — and it predicts **2 replicas × pool 10 ≈ 3,060 req/s, not
  2 × 2,074**. Measure it, state whether it held, and if it did not, say what the pool ladder
  got wrong. See the PR2 report §5.4;
- **report the recommended operating capacity PR2 deferred**, which needs a second replica
  before two of its three components mean anything. See
  [`ag-sept-pr2-scope.md`](ag-sept-pr2-scope.md) §5.6 and §5.6.1;
- populate the replica-count and aggregate-pool-capacity manifest fields;
- reuse PR2's Prometheus and dashboard path, adding bounded replica and authority identity and
  only the views scale-out diagnosis needs;
- run the §11 replica matrix and the mandatory connection-budget control of §8.1 and §12.3;
- run one composed two-authority run at the chosen replica count;
- calculate like-for-like scale efficiency, and distinguish application-compute gains from
  pressure on the database admission boundary;
- fold in the cheap evidence hygiene from the PR2 deferral register, group C: one TSDB snapshot
  per sweep rather than per cell, and re-run the PR2 cells under the fixed exporter;
- run the desirable resource-limit control of §12.4 if room remains.

**Evidence:** a local scale-out report containing the replica matrix, the connection-budget
control, the prediction result, per-workload scale efficiency, correctness verdicts, and the
composed run.

**Exit:** dispersed and hot-authority traffic are compared across replica counts without
changing the correctness model; pool multiplication is explained; PR2's prediction is confirmed
or refuted with evidence; the operating-capacity number is reported; and all quoted runs remain
reproducible and reconciled.

**Not in PR4:** AWS, Kubernetes, autoscaling, a service mesh, or an attempt to eliminate the
hot-authority serialization frontier.

### PR5 — Architecture conclusion and boundary decision

**Indicative budget:** 1.5 focused development days, followed by the reserved 2–3 days for
reruns, review, refinement, documentation, and public-release preparation. The split is
deliberate: the analysis, the tables, and the boundary decision are development work; polishing
prose, diagrams, and the repository entry point is what the reserve exists for, and moving it
into the base allocation is how the reserve quietly becomes contingency.

**Scope:**

- produce the final single-instance, multi-authority, and scale-out comparison tables, and only
  the charts needed to explain decisive findings;
- update the architecture diagrams and connect each frontier to the slot-row, identity-row,
  claim-relation, and organisation-home authority model;
- record the service-boundary decision, including why the reservation transaction remains
  together and whether an asynchronous event or reporting boundary is justified;
- state what Phase 2 would cost and why it was deferred, so the phasing is a recorded decision
  rather than an omission;
- update the repository entry point, reproduction instructions, limitations, evidence labels,
  negative controls, and public-disclosure checks.

**Evidence:** a public-ready set in which raw artifacts support every reported result and
architecture claim, with explicit limitations and no scale claim crossing incomparable workload
shapes.

**Exit:** a reviewer can reproduce the topology and decisive runs, distinguish measured facts
from calculations and interpretation, understand why replicas help or do not help for each
authority distribution and why authority composition is a different axis, and follow the
evidence to the architecture decision.

**Not in PR5:** feature expansion for presentation value, speculative decomposition, rich
dashboard work unrelated to a finding, or reopening settled measurement definitions.

### Deferred from PR2 — partially assigned in v0.5

v0.4 recorded a register of work PR2 deferred, unassigned pending a re-planning session. That
session is this document. The register is resolved as follows; the full reasoning stays in
[`ag-sept-plan-old.md`](ag-sept-plan-old.md) §14 and is not restated.

- **Group A — the overload question** (open-loop arrival mode, retry-on-timeout control,
  `slots_for()` over-provisioning, the timeout-budget negative control, and the unenforced
  `admission_cap` manifest field). **Remains unassigned and does not fit AG-Sept.** It is
  load-harness work of PR1's family, roughly 1.5 days, and `high-level-design.md` §1.1 makes the
  unreproduced overload question a founding one — which is why it should be scoped deliberately
  in its own milestone rather than absorbed here. **The `admission_cap` field is the exception
  and should be fixed opportunistically:** a manifest field that is published, range-validated,
  and enforced nowhere is worse than an absent one, and either enforcing it or removing it is
  minutes of work in any PR that touches the manifest.
- **Group B — instrumentation** (PostgreSQL exporter, node exporter). **Assigned to PR4 and
  promoted to required.** With the database established as the limiting subsystem, these stop
  being supporting work for a replica matrix and become the primary instrument. The ±2× caveat
  on every figure from this machine stands until the node exporter exists.
- **Group C — evidence hygiene** (per-sweep TSDB snapshots, re-running PR2 cells under the fixed
  exporter, optional histogram buckets). **Assigned to PR4**, which re-runs those cells anyway.

## 15. Priority tiers

### P0 — required

- reproducible external load harness;
- aggregated metrics;
- minimal reproducible time-series retention and diagnostic dashboard;
- response-validation negative control;
- correctness reconciliation, extended to every participating authority;
- one-instance baseline;
- dispersed and hot-slot controls;
- local hot-identity control with clean-start assertion;
- Phase 1 placement, server-side enforcement, and the misrouting control;
- multi-authority correctness and failure-isolation evidence;
- multi-instance shared-PostgreSQL comparison;
- connection-budget control;
- PostgreSQL and node exporters;
- the separate-generator rule: a co-resident run is reported as a bounded local frontier and
  never as a published capacity claim. The compute that would satisfy it does not arrive in
  AG-Sept, so the rule is honoured by labelling;
- evidence-led architecture conclusion.

### P1 — strongly desirable

- PR2's falsifiable prediction tested as a first-class result;
- recommended operating capacity;
- composed two-authority run;
- resource-limit control;
- simplified synchronized release wave;
- polished architecture and result diagrams.

### P2 — only after decisive evidence

- transactional outbox implementation;
- independently deployed consumer;
- autoscaling;
- fault injection beyond the authority-unavailability experiment;
- distributed tracing;
- richer dashboards.

### Explicitly out of scope

- cross-authority booking, and any distributed commit or saga protocol (design note §10);
- splitting one organisation across writable authorities;
- online rebalancing or dual-write migration between authorities;
- a shared global workflow database;
- decomposing the reservation transaction;
- multi-region writes;
- AWS, EKS, RDS, and any cloud measurement (§10);
- Kubernetes;
- a broker solely to claim event-driven architecture;
- service mesh;
- complete production authentication and authorization;
- eliminating the hot-authority serialization frontier;
- reproducing a full commercial workload.

## 16. Descope order

If time slips, remove work in this order:

1. the simplified synchronized release wave;
2. the resource-limit control (§12.4);
3. dashboard polish beyond the diagnostic minimum;
4. the composed two-authority run — the two axes are then reported separately;
5. the hot-identity and hot-slot rows of the replica matrix at 4 replicas, keeping 1 and 2;
6. optional histogram-bucket work from the PR2 deferral register, group C;
7. PR5's chart production beyond what a decisive finding requires.

Do not descope: the response-validation control, the misrouting control, the connection-budget
control, Phase 1 placement enforcement, multi-authority correctness reconciliation, the
failure-isolation experiment, the PostgreSQL and node exporters, the one-instance control,
minimal diagnostic time-series visibility, measurement validity, or the architecture report.

**The correctness work is not a source of budget.** If PR3a–PR3c overrun, the difference comes
first from §4's unallocated contingency and then from PR4's measurement scope through this list —
never from the gates that make a run admissible. That is the direct expression of §3.2, and it
is the order in which the two reserves are spent: contingency, then descope, and the 2–3 day
reserve last and only for what it is for.

## 17. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator with placement-aware routing;
3. aggregated service, runtime, database, and host metrics;
4. a minimal reproducible time-series retention path and diagnostic dashboard;
5. a single-instance report;
6. a multi-authority correctness and failure-isolation report;
7. a local scale-out report;
8. current and intended architecture diagrams;
9. authority and bottleneck analysis across all three scaling axes;
10. a service-boundary decision record, and a recorded decision on Phase 2;
11. a concise public repository summary;
12. explicit limitations, evidence labels, and negative-control results.

Together these should answer what is correct, what is fast under which workload, where
replicas help, where authority composition helps, where shared authority remains the frontier,
which dependency becomes limiting, and what should be separated next.

## 18. Completion gate

AG-Sept is complete when:

- required workloads run reproducibly from clean fixtures;
- single-instance, multi-authority, and local scale-out results are available;
- response validation, misrouting, generator headroom, and connection-budget controls have been
  demonstrated;
- outcomes and persisted state reconcile on every participating authority;
- database connection and authority boundaries are visible in retained measurements and
  diagnostic time series;
- one authority's failure is shown to bound its own organisations and no others;
- measured facts, calculations, and interpretation are separated;
- architecture reflects evidence rather than desired presentation;
- containers and authority composition remain means rather than goals;
- the repository is suitable for public review under the disclosure policy.
