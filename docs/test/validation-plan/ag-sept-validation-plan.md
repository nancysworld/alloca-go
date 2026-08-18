# AG-Sept validation plan

**Status:** Living — validation intent for the current AG-Sept iterations.  
**Scope:** controlled workloads, topology families, correctness/failure validations, negative
controls, and accept/reject conditions used to answer AG-Sept's scaling problems.  
**Does not own:** PR order, budget, priority, or dates; those remain in `docs/planning/`.

This plan is governed by:

- [`../../requirements/system-requirements.md`](../../requirements/system-requirements.md) —
  what the system must preserve;
- [`../../design/measurement-contract.md`](../../design/measurement-contract.md) — what makes an
  experiment or quantitative claim admissible;
- [`../../design/transaction-semantics.md`](../../design/transaction-semantics.md) — the
  transactional invariant set;
- [`../../design/horizontal-scaling.md`](../../design/horizontal-scaling.md) — the complete
  service/database scaling model;
- [`../../design/horizontal-database-authority.md`](../../design/horizontal-database-authority.md)
  — database-authority placement and Phase 1 booking semantics;
- [`../workload-catalog.md`](../workload-catalog.md) — stable named workloads reused while topology
  and implementation change.

Commands, container startup, seeding, and run procedure belong in `docs/operations/`. Executed
results belong in `docs/measurements/`.

## 1. How AG-Sept uses the engineering loop

AG-Sept is intentionally validated in iterations rather than by committing to one large matrix
up front.

### Iteration A — identify the first frontier

**Problem:** which subsystem limits the correct single-authority service first under controlled
load?

Validation uses the dispersed, hot-slot, and hot-identity workloads plus generator,
observability, and reconciliation controls.

**Analysis outcome:** PR2 established that PostgreSQL, rather than available Go service compute,
sets the measured single-authority frontier. The measurement report owns the evidence. This
result ended Iteration A and created the next scaling problem.

### Iteration B — compose writable database authorities

**Problem:** how can independent organisation work use independently writable PostgreSQL
authorities while preserving accepted transactional correctness and failure containment?

Validation covered Phase 1 supported booking, placement enforcement, explicit cross-authority
refusal, per-authority reconciliation, and authority failure isolation.

**Analysis outcome:** sufficiently resolved. PR3c demonstrated the accepted Phase 1 design across
two writable authorities, including supported same-authority operations, explicit cross-authority
refusal, placement enforcement, multi-authority reconciliation, and failure containment. The
retained report and artifacts own the evidence. `VAL-SCALE-3` is discharged in the sense it is
defined here: architecture/correctness and failure-independence evidence on a co-resident
workstation, **not** a capacity multiplier.

The accepted design remains owned by `horizontal-database-authority.md`; the durable A&R closure
and next Problem are owned by `../../requirements/ag-sept.md`.

### Iteration C — independently provisioned shard-group capacity

Iteration B's A&R selected the next **Problem**: how aggregate mutation capacity scales as
independently provisioned shard groups are added for independent organisation workloads, what
limits that scaling, and what workload and placement envelope each shard group should own.

Requirements and Design now constrain the validation deliberately:

- workload: `WL-MUT-DISP-4`, four equivalent independent organisations A/B/C/D;
- shard-group axis: exactly **1, 2, and 4 groups**;
- one service replica and one PostgreSQL authority per shard group, keeping service-replica count
  per group constant;
- one equivalent AWS EC2 capacity-unit host per group and a separate generator host;
- **independent closed-loop demand per shard group**: each active group owns its own fixed worker
  pool, so latency or saturation in one group cannot reduce the workers available to another;
- Tier 1 target: measured saturation-selected `G1`, `G2`, `G4`, derived 2-group and 4-group capacity
  efficiencies, and the limiting-resource interpretation; if that environment exists but cannot be
  driven far enough to establish the target with proven headroom, §4.6 permits a bounded Tier 2
  operating-point result without promoting it to a capacity claim. If the environment cannot be
  provisioned at all, §4.6 gives neither tier and `VAL-SCALE-5` is reported unproven;
- **no efficiency threshold**: obtaining and explaining the numeric result is the validation goal.

Any capacity claim must also **explain, exclude, or conservatively bound shared-environment
variation**. Iteration C does that by removing the workstation's fixed shared resource envelope
from the comparison, retaining host/resource evidence for each capacity unit, and proving generator
headroom separately.

## 2. Validation principles

### 2.1 Isolate mechanisms before combining them

Controlled workloads test one contention or scaling mechanism at a time before a composite
workload is interpreted. A synchronized-release wave may be useful later, but it must not replace
controls that isolate database saturation, slot serialization, identity serialization, replica
scaling, or database-authority composition.

### 2.2 Correctness gates precede performance interpretation

A performance result is inadmissible when its run violates the accepted invariant set or fails
reconciliation. The exact admissibility rules are owned by `measurement-contract.md`; this plan
names the AG-Sept scenarios that discharge them.

### 2.3 Compare like with like

Replica or authority scale efficiency is compared only between runs with the same workload
semantics and compatible SLO/evidence status. Deliberate policy refusals, injected failures, and
healthy-capacity runs are separate evidence classes.

For Iteration C, the capacity-unit equivalence contract is owned by
[`../../design/deployment-architecture.md`](../../design/deployment-architecture.md) §13.3. This
plan selects that design rather than restating its fields: `G1`, `G2`, and `G4` use equivalent
shard-group capacity units and the one-group baseline is measured in the same AWS environment as
the multi-group points.

### 2.4 Negative controls must be discriminating

A negative control succeeds only when the intended gate demonstrably notices the deliberately
introduced defect or constraint. Reasonable-looking output is not evidence that the gate is
active.

## 3. Controlled workloads

Stable workload definitions are now owned by [`../workload-catalog.md`](../workload-catalog.md).
The descriptions below retain the validation vocabulary used by earlier AG-Sept evidence; new or
reused workloads should cite a catalog identifier when their semantics need to remain stable across
architectures.

### 3.1 Dispersed workload

**Purpose:** expose the general application/database frontier while contention on any one slot
or user identity is low.

Defining properties:

- requests are spread across many slots and user identities;
- fixture/reset discipline prevents sold-out state from becoming the primary workload;
- no single logical business authority should dominate the run;
- exact counts, duration, concurrency, and rate are run parameters, not durable requirements.

Iteration C uses the catalogued four-organisation form, `WL-MUT-DISP-4`, rather than redefining its
population here.

### 3.2 Hot-slot workload

**Purpose:** expose the serialization frontier of one slot-capacity authority.

Many distinct users contend for one slot or a deliberately small set. Expected capacity
refusals remain valid completed outcomes but do not count as successful booking goodput.
Unrelated slots provide the control that serialization is scoped rather than global.

### 3.3 Hot-identity workload

**Purpose:** expose and contain one user-schedule serialization authority.

One identity, or a deliberately small set, attempts overlapping reservations concurrently. The
fixture starts with no live claims or stale idempotency records for the tested identities.
Unrelated identities provide the control that serialization is identity-scoped.

### 3.4 Multi-organisation dispersed workload

**Purpose:** exercise Phase 1 across several organisations and writable authorities without
mixing unsupported cross-database booking into normal goodput.

Defining properties:

- several organisations are distributed across at least two database authorities;
- at least two organisations are colocated on one authority so supported cross-organisation
  booking remains exercised;
- every generated supported reserve satisfies
  `authority(user_organisation_id) == authority(slot_organisation_id)`;
- the run records placement and observed workload distribution.

### 3.5 Cross-database-authority refusal control

**Purpose:** prove that a reserve whose user and slot ownership axes resolve to different
database authorities is explicitly refused in Phase 1 without partial mutation.

This is a separate evidence class, never mixed into a healthy goodput or latency comparison.
The control must establish the normative refusal, same-key replay, absence of reservation/
booking/claim/slot-side mutation, and absence of a remote slot-authority participant call.

### 3.6 One-hot-organisation workload

**Purpose:** distinguish authority-scoped failure or saturation from global service failure.
Traffic is concentrated on organisations owned by one authority while another authority carries
an unaffected control population.

### 3.7 Simplified synchronized release wave

**Purpose:** combine the isolated mechanisms once each is separately understood, approximating a
release event in which many users converge on a small set of newly available slots.

Defining properties:

- a modest number of slots released together with fixed capacity;
- attempts beginning within a short release window;
- a deliberately skewed distribution so some slots are hotter than average.

The first version deliberately omits browsing, alternative-choice loops, repeated retries, and
fallback activities: a composite workload moves several variables at once and is harder to
interpret than the controls it builds on. Under §2.1 it may extend the controlled workloads and
must not replace them.

### 3.8 Workload exclusions

These are outside AG-Sept's validation intent and are not gaps in it:

- a full fitness-club, game-inventory, or other complete product simulation;
- domain-specific names or confidential scenario detail;
- user-behaviour modelling beyond the authority distribution a validation requires;
- a retry-storm model, until baseline timeout behaviour is understood.

The last is a dependency rather than a preference: a retry model layered on unmeasured timeout
behaviour produces amplification that cannot be attributed.

## 4. Topology families

### 4.1 Single authority, one service replica

Control topology for the established single-authority frontier and controlled workload semantics.

### 4.2 Two authorities, one shard-affine service unit per authority

Minimum topology that can demonstrate independent writable authority, placement enforcement,
colocated cross-organisation booking, explicit cross-authority refusal, and authority failure
containment.

### 4.3 One authority, multiple stateless replicas

Used when a later iteration tests service-compute scaling separately from writable-authority
composition. Replica counts are experiment parameters rather than requirements.

### 4.4 Composed topology

Multiple authorities with multiple service replicas may be used after both axes are understood
separately. Its purpose is compositional confidence, not an independent production capacity
multiplier from a co-resident workstation.

### 4.5 Minimum experiment matrix

The smallest set of runs that discharges the validations below. Replica and authority counts are
`[HYPOTHESIS]` experiment parameters; the workload/topology pairing is the validation intent.

| Experiment | Topology | Workloads | Discharges |
|---|---|---|---|
| Phase 1 supported correctness | 2 authorities × 1 replica | multi-organisation dispersed; colocated cross-organisation booking; one-hot-organisation; wrong-`UserRef` confirm and cancel | VAL-COR-1..3 |
| Cross-authority refusal control | 2 authorities × 1 replica | bounded cross-authority reserves, as their own evidence class, **plus one same-key repost of a recorded refusal** — the cell's distinct keys cannot show replay | VAL-COR-4 |
| Placement enforcement | 2 authorities × 1 replica | deliberate misroute | VAL-COR-5, VAL-NEG-6 |
| Phase 1 failure isolation | 2 authorities × 1 replica | multi-organisation dispersed, one authority taken down, any ambiguity it produces resolved after restoration | VAL-COR-6, VAL-FAIL-1 |
| Replica matrix | 1 authority × several replica counts | dispersed across all counts; hot-slot and hot-identity at the extremes | VAL-SCALE-1 |
| Connection-budget control | 1 authority × ≥2 replica counts | dispersed, under both budget configurations | VAL-SCALE-2, VAL-NEG-4 |
| Composed run | 2 authorities × chosen replica count | multi-organisation dispersed | VAL-SCALE-3, VAL-SCALE-4 |

The replica matrix runs **within one shard group**. Whether replicas scale application compute is
a single-authority question, and partitioning does not change the answer; running the full matrix
across both topologies would multiply measurement time without producing a new conclusion. One
composed run shows the two axes together.

Which of these are scheduled, in what order, and with what budget is owned by
`docs/planning/ag-sept-plan.md`. A row here is a validation's meaning, not a commitment to run it
in a particular milestone.

### 4.6 Iteration C fixed capacity matrix

Iteration C is deliberately more constrained than the generic families above. It runs exactly the
following placement matrix for `WL-MUT-DISP-4`:

| Capacity point | Shard groups | Organisation placement | Capacity-unit resources |
|---|---:|---|---|
| `G1` | 1 | `A B C D` | 1 equivalent shard-group EC2 host |
| `G2` | 2 | `A B` / `C D` | 2 equivalent shard-group EC2 hosts |
| `G4` | 4 | `A` / `B` / `C` / `D` | 4 equivalent shard-group EC2 hosts |

Each group has **one service replica + one PostgreSQL authority**. The generator runs on separate
EC2 compute and routes equal workload share for A/B/C/D according to the active placement map.
`WL-MUT-DISP-4` keeps the request pair itself topology-independent: every user books a slot owned
by the **same organisation** at `G1`, `G2`, and `G4`. The existing `multi-org-dispersed` workload,
which deliberately includes colocated cross-organisation pairs, remains separate Iteration-B-style
correctness coverage and must not be substituted into this capacity matrix.

**The closed-loop demand streams are independent per shard group.** A single physical generator
host may run all of them, but one shared worker pool may not feed several groups: when one group is
slow, workers blocked on that group would otherwise stop issuing requests to healthy groups and a
local saturation event would appear as a topology-wide throughput drop. For a rung whose declared
per-group concurrency is `c`, `G1`, `G2`, and `G4` therefore have total concurrency `c`, `2c`, and
`4c` respectively, with each group retaining its own `c` workers. Within a group, its worker pool
continues to distribute demand equally over the organisations that placement assigns to that group.
Backpressure is allowed to lower the request-completion rate of the group that is slow; it must not
reduce the configured worker population or offered demand opportunity of another group.

The shard-group capacity units remain like-for-like under `deployment-architecture.md` §13.3; the
workload semantics, service image, pool policy, timeout policy, and PostgreSQL configuration stay
fixed across `G1`, `G2`, and `G4`.

#### Tier 1 — capacity-point selection rule

A capacity point must be selected by the **same saturation rule** at all three topologies; it must
not be the final rung merely because the sweep stopped there.

For each topology:

1. for **each concurrency rung**, reset/reseed once and then run that rung as **one sustained
   closed-loop measured run** far enough to establish its operating regime and the saturation
   region while preserving the `measurement-contract.md` §3 capacity definition, §5
   experiment/evidence gates, and §7 provisional SLO/outcome gates. Fine-grained time slices
   inside that sustained rung are analysis windows showing evolution/stationarity; they are not
   independent repeat samples and there is no destructive reseed between those slices;
2. select the highest gated rung whose sustained Goodput is followed by at least one higher rung
   that either **does not produce higher sustained Goodput** or fails one of those gates — this is
   the operational saturation-knee/plateau point for `G1`, `G2`, or `G4`;
3. repeat that selected point once as a **separate full confirmation run**, beginning from a fresh
   reset/reseed and the same declared configuration. The confirmation is an independent execution
   of the selected experiment, not a second slice taken from the first sustained run;
4. retain the complete ladder, each sustained rung's time-series shape, the point-selection
   justification, reconciliation, and resource evidence alongside the selected point and its
   confirmation run.

The repeated short-cell series used in PR4a remains valid **diagnostic machinery**: it exposed and
root-caused a fixture-induced cached-plan regime. It is not the canonical capacity-ladder shape.
Repeated `TRUNCATE -> short load -> gap` cycles deliberately recreate an empty-table planner state
at roughly the same timescale as the short run itself; sustained rungs let one workload evolve
continuously instead of repeatedly manufacturing that transient.

The whole sustained run remains in the measurement and reconciliation populations. No hidden
warm-up traffic may mutate state and then disappear from client totals. If a later analysis wants
to distinguish an initial transient from a sustained interval, that boundary must remain explicit
and auditable under `measurement-contract.md` §12 rather than retroactively rewriting the run.

PR2 report §5.6 and §6.3 are the reason this rule is explicit: with a closed-loop harness,
latency/deadline gates can remain comfortably non-binding while throughput has already saturated.
Without a demonstrated higher rung, a sweep endpoint is not a capacity result and cannot enter
`E2` or `E4`.

**Fixture headroom is part of the selection, not a precondition of it.** Step 2 turns on a higher
rung failing to produce more Goodput, and `WL-MUT-DISP-4` consumes the state it books: every
admitted reserve takes capacity from a seeded slot, so a long enough ladder against a fixed
population runs out of bookable state before it runs out of service. When that happens the higher
rung is short of *fresh mutations*, not short of *service* — it demonstrates an exhausted fixture,
says nothing about where the server's frontier is, and therefore cannot establish that the rung
beneath it was the frontier. The selected point is invalidated along with the rung that was
supposed to exceed it (`measurement-contract.md` §5, useful-demand / fixture-headroom gate).

So the selected point, its confirmation run, **and the higher rung its selection rests on** must
each retain enough clean fixture state to offer fresh mutations throughout their full sustained
measured interval. Fixture sizing must therefore cover both the deepest intended rung **and the
longer sustained duration**, with safety headroom, while preserving the required per-organisation
comparability across `G1`, `G2`, and `G4`. An unexpected population of `business_refusal`
attributable to spent fixture state — rather than to the slot contention `WL-MUT-DISP-4` is
designed to create — invalidates the point rather than describing it.

An all-refusal run is the limiting case and is treated the same way: it may be sound and may
certify at whatever provenance level its manifest earns, but it carries no useful demand and backs
no capacity number. Provenance is not what it lacks.

If the two retained observations materially disagree, do not average the disagreement into a
clean headline number: explain, exclude, or conservatively bound the variation before promoting a
single capacity result.

**Per-authority data volume is an intended co-varying factor of this sharding experiment.** `G1`
places four organisation datasets on one authority while `G4` places one on each. Retain
per-authority row/data-volume and, where practical, index/working-set evidence sufficient to make
that change visible. A smaller per-authority working set may be part of what sharding buys; it must
therefore be named when interpreting sub- or super-linear efficiency rather than silently treated
as invariant.

Tier 1 derives:

```text
E2 = G2 / (2 × G1)
E4 = G4 / (4 × G1)
```

under `measurement-contract.md` §3.1. `E2` and `E4` are **results, not gates**; no percentage is
required for Iteration C to be sufficiently resolved.

#### Tier 2 — operating-point horizontal scale when capacity is measurement-limited

Tier 1 is the stronger Iteration C result and the only tier that supports a claim about aggregate
mutation **capacity** scaling.

**Tier 2 presupposes that the complete `G4` environment exists.** It is the fallback for an
environment that has been provisioned and then proves unable to *drive* the topology family far
enough — generator headroom, a resource limit, or another demonstrated measurement-system frontier.
Only then may Iteration C retain a weaker operating-point comparison rather than turning the
measurement limit into an arbitrary capacity endpoint.

**A provisioning limit is not a Tier-2 trigger.** If AWS quota, or anything else external, prevents
the complete equivalent `G4` environment from being instantiated at all, there is no common per-unit
`L` to select and no comparable topology family to apply it across — the thing Tier 2 measures does
not exist. The outcome is then neither Tier 1 nor Tier 2: Iteration C records the external
limitation and reports `VAL-SCALE-5` and aggregate capacity scaling as **explicitly unproven**.
Neither a partial AWS topology nor a shared-workstation rehearsal substitutes for the missing
environment, and no local number may be promoted to fill the gap.

The distinction is between an environment that cannot be *built* and one that cannot be *driven*.
Only the second produces evidence at all.

Select the **highest useful common per-unit workload intensity** `L` that the complete `G4`
measurement environment can drive without becoming the plausible limiter. Apply that same per-unit
intensity across the topology family. With the closed-loop harness, `L=c` means **exactly `c`
workers in each group's independent demand stream**, hence total concurrency `c`, `2c`, and `4c`
for the 1-, 2-, and 4-group topologies while A/B/C/D retain equal workload share within their
active groups. Retain the resulting goodputs `g1(L)`, `g2(L)`, and `g4(L)`. Derive:

```text
E2(L) = g2(L) / (2 × g1(L))
E4(L) = g4(L) / (4 × g1(L))
```

`L` must not be chosen merely because it is easy to drive. Its selection must be justified as a
substantial operating point within the proven generator/resource envelope, and the same correctness,
reconciliation, SLO/evidence, like-for-like capacity-unit, independent-demand, and `VAL-NEG-7`
controls still apply.

If only Tier 2 is achieved, the conclusion is deliberately bounded: **horizontal scaling is
established at `L`; aggregate capacity scaling remains unresolved.** A Tier-2 point cannot be
promoted into a capacity result, cannot satisfy the Tier-1 saturation-selection rule by implication,
and cannot be used to claim that the 2- or 4-group topology reached maximum useful Goodput.
Accordingly, Tier 2 does **not** discharge `VAL-SCALE-5`; that capacity validation remains explicitly
unproven while the operating-point horizontal-scale result is retained as valid evidence at `L`.

#### Open-loop comparison follows the closed-loop capacity round

Closed-loop remains the primary Iteration C capacity method for this round so the experiment changes
one major variable at a time. After the sustained closed-loop ladder is qualified and yields measured
sustainable rates, a bounded **open-loop comparison round** may use those rates to choose offered-load
points below, around, and above the observed closed-loop frontier. Its purpose is complementary:
show how achieved Goodput, latency, timeout/error behaviour, and queueing respond when offered arrival
rate no longer self-throttles as service latency rises.

An open-loop driver must bound outstanding work: explicit request deadlines, an explicit maximum
in-flight count, and accounting for arrivals that could not be launched because that bound was
reached. The open-loop round is not used retroactively to redefine the closed-loop capacity result;
it is a second lens on overload/latency behaviour.

## 5. Correctness and policy validations

### VAL-COR-1 — Reconciliation for measured runs

**Requirements:** REQ-COR-1, REQ-EVID-1.

Every admissible measured run satisfies the reconciliation contract in
`measurement-contract.md`, including per-authority invariant checks and correctly aggregated
client/server/persisted totals for multi-authority runs.

### VAL-COR-2 — Supported Phase 1 booking

**Requirements:** REQ-COR-1, REQ-ROUTE-1.

Demonstrate same-organisation and colocated cross-organisation reserve paths through one
database authority, including schedule non-overlap across slot-owning organisations for the same
`UserRef`.

### VAL-COR-3 — Confirm/cancel ownership consistency

A wrong `UserRef` receives the normative ownership result regardless of whether user and slot
organisations happen to be colocated. Placement must not accidentally change the domain answer.

### VAL-COR-4 — Cross-database-authority refusal

**Requirements:** REQ-COR-1, REQ-ROUTE-1.

Run the refusal control in §3.5 and prove that Phase 1 creates no partial mutation.

**Two cells discharge it, not one.** The refusal cell drives distinct keys throughout, which is
what makes it a clean evidence class and also what makes it structurally unable to show §3.5's
same-key replay clause — a persisted record is a necessary condition for replay, not a
demonstration of it. That clause is discharged by the `controls` cell's case 3b, which reposts the
refusal's own key and asserts the recorded reason and `replay=true` against `replay=false` on the
first post — the transition, rather than a flag that a service labelling every refusal a replay
would also satisfy. A re-validation that runs only the refusal cell leaves the clause unexercised.

### VAL-COR-5 — Placement enforcement / misrouting

**Requirements:** REQ-ROUTE-1.

Deliberately send a request to a service unit that does not own the routing organisation. The
unit rejects it at the routing/deployment boundary and does not write state.

The control asserts a **specific** answer, owned by
[`../../design/api-surface.md`](../../design/api-surface.md) §2.6: `invalid_request` refused at
the transport edge, plus the misroute counter — never a `business_refusal`. A misroute is a
routing or deployment fault, not a domain answer, and it must not be recorded as a user's durable
domain outcome on an authority that does not own them. This also keeps it distinct from a valid
domain-level cross-authority refusal (VAL-COR-4).

Without this control, "no supported request reached the wrong authority" is a property of the
generator's routing table rather than of the system, and REQ-ROUTE-1 is untested.

### VAL-COR-6 — Ambiguous mutation resolution

**Requirements:** REQ-COR-2.

Where a fault produces `unknown_replayable`, retain the idempotency key, restore the dependency,
replay that same key, and resolve the ambiguity before the final correctness verdict. Resolution
must satisfy the measurement/reconciliation population contract in `measurement-contract.md` §12.

The accounting mechanism is validated discriminatingly in deterministic end-to-end tests, not by
requiring three corresponding live fault injections. In every case the original
`unknown_replayable` remains part of the measured population exactly as observed: one measured
request, zero measured Goodput, and no retroactive rewrite of measured latency, outcome totals,
replay counts, or duration-based rates. Resolution attempts are retained separately for
reconciliation/recovery accounting.

The three states are:

1. **original committed** — same-key resolution returns `replay=true`; measured performance remains
   unchanged, while reconciliation establishes exactly one final logical mutation for the key and
   records the resolution HTTP attempt as a replay. With one resolution attempt, recovery may be
   described as one eventual logical mutation across two HTTP attempts, but not as measured
   `1/2` Goodput;
2. **original did not commit** — same-key resolution returns `replay=false`; measured performance
   again remains unchanged, while the resolution request performs exactly one final logical
   mutation after the measured interval and is retained in the reconciliation population;
3. **still ambiguous** — no final logical-mutation credit is established and the run is rejected
   from reconciliation/certification rather than producing a verdict from incomplete state.

The test must make the population boundary discriminating: an implementation that folds resolution
requests into measured `Completed`, `Goodput`, measured outcome/replay totals, latency, or the
measurement interval fails even if its final persisted-row count is correct. Conversely, an
implementation that omits resolution traffic from reconciliation/server-scrape accounting also
fails.

These accounting cases do not depend on the failure-isolation experiment naturally producing an
ambiguous commit. A deliberately timed loss while `COMMIT` or its acknowledgement is in flight is
the strongest remaining control for INV-21's acknowledgement-lost case, but it is a separate,
opportunistic fault injection. A generic stopped authority must not be reported as proof of that
specific fault.

## 6. Failure-isolation validation

### VAL-FAIL-1 — One database authority unavailable

**Requirements:** REQ-FAIL-1, REQ-ROUTE-1.

Run separate affected and unaffected populations, then make one authority unavailable.
Demonstrate that:

- organisations owned by the unavailable authority receive applicable bounded infrastructure
  outcomes;
- organisations owned by another healthy authority continue independently;
- no request fails over to the surviving writer;
- restoration requires no compensating writes on the unaffected authority;
- **any** ambiguous mutations the experiment actually produces are resolved before the final
  persisted-state verdict; the experiment is not required to manufacture `unknown_replayable`;
- the failure experiment is reported separately from healthy capacity/SLO runs.

## 7. Scaling validations

**Every scaling result names its believed limiting mechanism and the evidence for it.** Naming a
frontier without naming what set it is an observation, not a conclusion, and it is the step at
which incomparable mechanisms get collapsed into one number (REQ-EVID-2).

The candidate boundaries are:

- application CPU;
- database connection-pool acquisition;
- PostgreSQL connection/admission capacity;
- transaction or lock wait;
- one slot-capacity authority;
- one user-identity authority;
- one writable database authority;
- telemetry overhead;
- generator saturation;
- shared-host or external-environment contention.

The list is the differential diagnosis a result argues against, not a menu to pick from: a claim
that one of these is limiting carries the evidence that distinguishes it from the others.

### VAL-SCALE-1 — Replica comparison within one shard group

**Requirements:** REQ-SCALE-1, REQ-SCALE-2, REQ-SCALE-3, REQ-EVID-2.

Compare like-for-like dispersed traffic across at least two replica counts against one database
authority. Selected hot-slot and hot-identity comparisons confirm that adding stateless replicas
does not remove the underlying logical-authority serialization frontier.

Each result names the believed limiting mechanism and supporting evidence.

### VAL-SCALE-2 — Connection-budget control

**Requirements:** REQ-SCALE-1, REQ-EVID-2.

For multiple replicas within one shard group compare:

1. an approximately constant aggregate database connection budget; and
2. the original per-replica pool size multiplied by replica count.

This separates additional service compute from increased pressure on the shared database
admission boundary. Aggregate pool capacity is calculated per database authority.

### VAL-SCALE-3 — Database-authority composition

**Requirements:** REQ-SCALE-1, REQ-COR-1, REQ-EVID-2.

Use the multi-organisation dispersed workload across at least two writable database authorities
and establish correctness and failure independence. On a co-resident workstation, interpret this
as architecture/correctness evidence rather than a production capacity multiplier.

### VAL-SCALE-4 — Composed topology

Optional after service-replica and database-authority behaviour are understood separately. Run a
representative replica count across multiple authorities to show that the two axes compose
without changing routing or correctness semantics.

### VAL-SCALE-5 — Independently provisioned shard-group capacity

**Requirements:** REQ-COR-1, REQ-SCALE-1, REQ-SCALE-4, REQ-DEPLOY-1, REQ-EVID-1, REQ-EVID-2.

Run the fixed matrix and Tier-1 saturation-selection rule in §4.6 and obtain admissible `G1`, `G2`,
and `G4` for `WL-MUT-DISP-4`. The request-pair semantics remain same-organisation at every topology.
Each group is driven by its own closed-loop demand stream at the declared per-group concurrency;
one group's latency/backpressure must not reduce the worker population offered to another. Derive
`E2` and `E4` under the measurement contract and identify or conservatively bound the limiting
mechanism at each relevant frontier, explicitly accounting for the intended change in per-authority
data/working-set volume as organisations are distributed across more authorities.

The validation passes when the numbers are reproducible/admissible, correctness reconciles, the
resource envelopes are comparable, generator/shared-environment effects cannot plausibly explain
the result, per-group demand independence is proven, the saturation point is established rather
than assumed from sweep depth, the selected point and the rung above it retained fixture headroom
to offer fresh mutations throughout (§4.6), and the limitations are stated. It does **not**
require an efficiency percentage.

If the complete environment exists but a proven measurement-system limit forces §4.6's Tier-2 path
instead, retain that operating-point horizontal-scale result, but report `VAL-SCALE-5` as
**unproven**. Tier 2 cannot be promoted into a capacity result merely because it is the strongest
result the available environment could drive.

If the complete equivalent environment cannot be provisioned at all, neither tier applies. Record
the external limitation and report `VAL-SCALE-5` as **unproven** with no substitute result: this
validation is defined over independently provisioned capacity units, and a topology that was never
instantiated produces no evidence about them at any tier.

## 8. Measurement-system negative controls

The repository-wide experiment template remains owned by `measurement-contract.md`. AG-Sept
uses these concrete controls.

### VAL-NEG-1 — Response-validation-active proof

Deliberately supply an invalid HTTP-status/domain-outcome combination and prove the load harness
rejects the response and invalidates the run.

### VAL-NEG-2 — Generator bottleneck control

Deliberately constrain the load generator and demonstrate how the apparent frontier changes.
Any stronger capacity interpretation then requires evidence that the selected generator has
headroom.

For Iteration C, PR4a must first preflight a generator configuration above the intended `G4` sweep
range under `deployment-architecture.md` §13.2. PR4b still proves headroom at every quoted server
point. The generator may be resized and requalified between topology points because it is
measurement infrastructure rather than part of the shard-group capacity unit; its actual shape and
headroom evidence remain part of each run's provenance/evidence.

### VAL-NEG-3 — Telemetry-overhead control

Compare relevant observation modes and demonstrate that telemetry is not the primary
request-path bottleneck for the measured result.

### VAL-NEG-4 — Connection-pool multiplication control

The discriminating control is VAL-SCALE-2 and is mandatory when interpreting replica scale-out.

### VAL-NEG-5 — Resource-limit control

Where diagnostically useful, constrain an identified resource and confirm that the frontier
changes in the predicted direction. This is not a universal completion gate.

### VAL-NEG-6 — Misrouting control

The discriminating control is VAL-COR-5 and is mandatory for a multi-authority topology claim.

### VAL-NEG-7 — Capacity-unit resource-envelope control

**Requirements:** REQ-SCALE-4, REQ-EVID-2.

Before interpreting `VAL-SCALE-5`, retain enough per-host CPU, memory, storage/I/O, network and
service/database resource evidence to establish that each shard-group host had the intended
equivalent envelope and that material host/environment variation is explained, excluded, or
conservatively bounded.

A run in which one capacity-unit host is materially constrained relative to its peers is evidence
about that constraint, not a clean shard-group scale-efficiency point.

### VAL-NEG-8 — Per-shard-group demand independence

Before interpreting `VAL-SCALE-5`, prove that the multi-group closed-loop driver maintains a
separate fixed worker pool per active shard group. The discriminating control deliberately delays,
constrains, or otherwise slows one target group while a healthy control group remains available.
The slowed group may complete fewer requests because its own workers are blocked; the healthy
group must retain its configured worker population and continue issuing independently rather than
losing demand because workers are shared globally.

The proof may be a deterministic generator-level integration test rather than a capacity run, but
it must fail against the old shared-pool/round-robin design. Equal aggregate request counts in a
healthy run are not sufficient evidence: the defect appears specifically when one group's response
time diverges.

## 9. Current validation status

| Validation area | Status | Authoritative evidence / next analysis |
|---|---|---|
| single-authority frontier | established | PR2 measurement report; produced Iteration B problem |
| response validation and generator headroom | established for the baseline scope | retained PR1/PR2 evidence |
| telemetry-overhead control | **not discharged** | PR2 found within-mode spread larger than the between-mode delta; no overhead figure is claimed |
| Phase 1 placement and supported policy implementation | established for Iteration B | PR3a/PR3b implementation records plus PR3c controls/evidence |
| multi-authority reconciliation | established for Iteration B | PR3b harness exercised and reconciled by PR3c retained runs |
| Phase 1 correctness and failure isolation | established for Iteration B | PR3c report and retained artifacts; VAL-COR-1..3, VAL-COR-5, VAL-COR-6 and VAL-FAIL-1 |
| cross-authority refusal (VAL-COR-4) | established for Iteration B | all four §3.5 clauses now hold on the deployed topology: the refusal and the absence of partial mutation by the PR3c passes, and **same-key replay** by control 3b, retained in [`../../measurements/pr3c-phase1/controls-replay/`](../../measurements/pr3c-phase1/controls-replay/). The replay clause was the gap the Iteration B A&R found (PR3c report §7.4), and it was closed by adding the repost to the control rather than by re-running or reinterpreting the retained cells |
| database-authority composition (VAL-SCALE-3) | established as architecture/correctness evidence | PR3c; explicitly **not** a capacity multiplier on the co-resident workstation |
| Iteration C shard-group capacity (VAL-SCALE-5) | **defined; not yet executed** | Tier 1 uses sustained closed-loop rungs over the fixed `WL-MUT-DISP-4` A/B/C/D 1/2/4 matrix, with independent per-group worker pools, saturation-selected `G1/G2/G4`, one separate confirmation run per selected point, and derived `E2/E4`. Where the complete environment exists but a proven measurement-system limit prevents Tier 1, §4.6 Tier 2 may establish horizontal scaling at a common per-unit `L`, with capacity explicitly unproven. Where that environment cannot be provisioned at all, neither tier applies and `VAL-SCALE-5` is unproven with no substitute result |
| Iteration C per-group demand independence (VAL-NEG-8) | **defined; not yet executed** | generator must prove one slow group cannot throttle the configured worker population of healthy groups; control must fail against shared global closed-loop workers |
| Iteration C resource-envelope control (VAL-NEG-7) | **defined; not yet executed** | retain per-host resource evidence and explain/exclude/bound material environment variation |
| stateless replica scaling | unproven and not selected by Iteration C | existing VAL-SCALE-1/2 remain separate future validation definitions |
| composed multi-authority + multi-replica topology | optional later validation | only after both axes are understood separately |

The milestone schedule may change order, budget, or optional depth. A mandatory property does not
disappear merely because a PR moves: either it is validated, or Analyse & Review records that the
property remains unproven or outside the agreed scope.

## 10. Evidence, analysis, and the next iteration

For each executed validation:

1. the run/report satisfies `measurement-contract.md`;
2. raw machine-readable artifacts are retained under `docs/measurements/` according to repository
   convention;
3. measured facts, derived calculations, interpretation, and limitations remain distinct;
4. the report identifies the applicable `VAL-*` validation and `REQ-*` requirements;
5. **Analyse & Review** compares the evidence with the current problem, requirements, design,
   and validation intent.

Analyse & Review then records the closure outcome
[`../../development/engineering-process.md`](../../development/engineering-process.md) §1.4.1
requires, in [`../../requirements/ag-sept.md`](../../requirements/ag-sept.md): problem verdict,
evidence, durable learning, **goal progress**, and the loop decision.

**Two questions, not one.** "Is this problem resolved?" and "is the governing goal sufficiently
achieved?" are separate, and answering only the first is how a milestone ends while its goal is
still open:

- problem resolved **and** goal sufficiently achieved for the agreed scope → the loop ends;
- problem resolved, goal **not** yet achieved → Analyse & Review names the next problem, and a new
  iteration starts at **Problem**;
- problem unresolved or refined by the evidence → the refined problem starts the next iteration.

In every case, accepted learning reaches requirements, design, and this plan before the next
iteration is scheduled.

A surprising result is therefore not a plan failure. It is evidence that may start the next
engineering iteration.
