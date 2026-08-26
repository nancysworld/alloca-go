# AG-Sept milestone validation

**Status:** Living — validation intent for the current AG-Sept iterations.  
**Scope:** controlled workloads, topology families, correctness/failure validations, negative
controls, and accept/reject conditions used to answer AG-Sept's scaling problems.  
**Does not own:** PR order, budget, priority, or dates; those belong to
[`milestone-plan.md`](milestone-plan.md).

This document is governed by:

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
- [`../../design/workload-catalog.md`](../../design/workload-catalog.md) — stable named workloads
  reused while topology and implementation change.

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

### Iteration C — shard-group capacity, then independent-provisioning verification

Iteration B's A&R selected the next **Problem**: how aggregate mutation capacity scales as
independently provisioned shard groups are added for independent organisation workloads, what
limits that scaling, and what workload and placement envelope each shard group should own.

The strongest requirement is unchanged: only an independently provisioned topology with equivalent
growing capacity-unit envelopes can discharge `VAL-SCALE-5`. Iteration C now deliberately builds
evidence in two layers rather than leaving useful workstation capacity unused while that external
environment is quota-blocked:

- **local controlled characterisation** — the same G1/G2/G4 workload and qualified measurement
  method run on scheduler-partitioned shard groups inside one workstation. This can establish
  `VAL-SCALE-6`, including `E2_local` and `E4_local`, but the groups still share a host/kernel/
  storage path and the result is never promoted into `VAL-SCALE-5`;
- **independently provisioned verification** — when the complete environment can actually be
  provisioned, the same method runs on equivalent independent capacity-unit hosts. Tier 1 remains
  the target and Tier 2 remains the bounded measurement-limited fallback defined in §4.6.

Requirements and Design constrain both layers deliberately:

- workload: `WL-MUT-DISP-4`, four equivalent independent organisations A/B/C/D;
- shard-group axis: exactly **1, 2, and 4 groups**;
- one service replica and one PostgreSQL authority per shard group, keeping service-replica count
  per group constant;
- **independent closed-loop demand per shard group**: each active group owns its own fixed worker
  pool, so latency or saturation in one group cannot reduce the workers available to another;
- the experiment variable is **`workers_per_group`** — closed-loop load-generator workers assigned
  to one shard group — and is deliberately distinct from PostgreSQL/pool connection counts;
- the pool policy, workload semantics, timeout policy, service image and PostgreSQL configuration
  are fixed before the retained G1/G2/G4 comparisons begin;
- independently provisioned verification uses one equivalent capacity-unit host per group plus
  separate generator compute under `deployment-architecture.md` §13;
- **no efficiency threshold**: obtaining and explaining the numeric result is the validation goal.

Any capacity/scaling claim must also **explain, exclude, or conservatively bound environment and
measurement-system variation** at the evidence level it asserts. Local results carry the shared-host
limitation explicitly; independently provisioned results additionally discharge the equivalent-host
resource-envelope controls.

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

Iteration C never mixes environments in one efficiency calculation. `G1_local`, `G2_local` and
`G4_local` are one scheduler-partitioned workstation family. If independently provisioned evidence
is later obtained, `G1_aws`, `G2_aws` and `G4_aws` are a second family whose capacity-unit
equivalence contract is owned by [`../../design/deployment-architecture.md`](../../design/deployment-architecture.md)
§13.3. A workstation baseline is never combined with an independently provisioned scale-out point.

### 2.4 Negative controls must be discriminating

A negative control succeeds only when the intended gate demonstrably notices the deliberately
introduced defect or constraint. Reasonable-looking output is not evidence that the gate is
active.

## 3. Controlled workloads

Stable workload definitions are now owned by [`../../design/workload-catalog.md`](../../design/workload-catalog.md).
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
`milestone-plan.md`. A row here is a validation's meaning, not a commitment to run it
in a particular milestone.

### 4.6 Iteration C capacity method

Iteration C keeps one topology/workload semantics across two evidence environments:

| Point | Shard groups | Organisation placement |
|---|---:|---|
| `G1` | 1 | `A B C D` |
| `G2` | 2 | `A B` / `C D` |
| `G4` | 4 | `A` / `B` / `C` / `D` |

Each group has **one service replica + one PostgreSQL authority**. `WL-MUT-DISP-4` keeps the request
pair topology-independent: every user books a slot owned by the **same organisation** at `G1`,
`G2`, and `G4`. The existing `multi-org-dispersed` workload deliberately exercises colocated
cross-organisation pairs and remains separate Iteration-B correctness coverage.

#### 4.6.1 Worker semantics and demand independence

The experiment variable is **`workers_per_group`**, meaning closed-loop load-generator workers
assigned to one shard group. This is not a PostgreSQL connection count and is not constrained to a
multiple of `pool_max_conns`.

For `workers_per_group = w`:

```text
G1 total_workers = w
G2 total_workers = 2w
G4 total_workers = 4w
```

Every active group owns a separate fixed worker pool. A single physical generator may host all
pools, but a worker blocked on group A may not reduce the workers available to B/C/D. Within a
group, its workers distribute demand equally across the organisations assigned to that group.
Per-group and aggregate request/outcome accounting are both retained.

Historical PR4a labels predate this convention. In particular, old G4 `c16` means **16 total
workers, approximately 4/group**, and old G4 `c32` means **32 total, approximately 8/group**. They
must not be compared as though `c16` meant `workers_per_group=16`.

One topology consequence is deliberate and must remain visible when interpreting the result. At a
common `w`, per-organisation worker share is approximately `w/4` at G1, `w/2` at G2, and `w` at G4.
The experiment holds **per-shard-group intensity** fixed because shard-group capacity is the object
being scaled; it cannot simultaneously hold per-organisation intensity fixed. Per-organisation
request distribution is therefore retained alongside the per-authority data/working-set trajectory.
If organisation-local contention becomes material, that is workload-envelope evidence rather than a
quantity to hide.

#### 4.6.2 Conditioning is explicit state preparation, not discarded warm-up

The PR4a diagnosis demonstrated that `TRUNCATE -> immediate peak load` repeatedly manufactures a
cold/empty mutation-table planner state whose cached execution plan can outlive the fresh planner's
own correction. The canonical sustained experiment must therefore not begin from that uncontrolled
transient.

Each retained capacity run uses this sequence:

1. reset/reseed the declared fixture once;
2. drive an explicit **conditioning phase** through the real mutation path until a fixed,
   predeclared **state target** is reached. Prefer a per-organisation mutation target over a time
   duration so faster and slower topologies do not begin measurement at different logical states;
3. retain conditioning requests/outcomes separately and capture the persisted state/counters at the
   conditioning boundary;
4. deterministically recycle the service DB pool or restart the service, without reseeding or
   removing the conditioned database state, so measured connections begin against the representative
   table state rather than carrying plans made against empty mutation tables;
5. pass readiness/provenance/observability checks, then open the measured interval.

Conditioning uses the same physical tables and mutation path but must not create logical conflicts
with the measured population; the implementation may use a disjoint conditioning identity/key/slot
namespace inside the same fixture. Its mutations remain real state and fixture sizing includes both
conditioning supply and measured supply.

This is **not** the old arbitrary `-warm-up` behaviour whose requests disappear from accounting.
`measurement-contract.md` §5 and §12 own the population boundary: conditioning is explicit and
auditable, measured Goodput/latency begin only at the declared measured boundary, and final
reconciliation accounts for the conditioning baseline plus the measured/resolution deltas. No
traffic that mutates persisted state may be silently discarded.

Cold-empty-database behaviour remains a legitimate separate workload question. It is simply not the
capacity question Iteration C has selected.

#### 4.6.3 Pool policy is qualified once, then fixed

Before retained G1/G2/G4 capacity comparisons begin, a bounded G1 sensitivity check establishes
that the chosen `pool_max_conns` is not an obviously removable connection-admission ceiling for the
current shard-group resource shape. This is not a new pool-tuning matrix: the purpose is to choose a
reasonable fixed policy, not optimise connection count.

Once selected, pool policy stays identical across G1/G2/G4 within an evidence environment. If a
later result shows the pool itself became the frontier, that is a measured limiting mechanism; the
configuration is not silently changed mid-comparison.

#### 4.6.4 Adaptive reconnaissance brackets saturation; it is not capacity evidence

Iteration C does **not** require a low-to-high sustained ladder merely to discover where saturation
might lie. Short, non-canonical reconnaissance probes may search adaptively in
`workers_per_group`, beginning near the range suggested by prior/local evidence and moving upward or
downward as necessary.

Reconnaissance answers only: **which two worker levels should receive the expensive retained
measurement?** It does not enter `E2`/`E4`, does not itself establish capacity, and is not promoted
because a probe happened to look stable.

Let:

- `S` = the candidate selected worker level;
- `H` = a higher worker level whose retained result is used to decide whether `S` is at the useful
  sustained frontier.

If `H` still produces materially higher gated sustained Goodput, the bracket is not found: move
higher and repeat the reconnaissance/retained-point selection as necessary. There is **no fixed
maximum such as 16 workers/group**; the upper bound is empirical and generator/resource gates still
apply.

**Probes are 120 s, and the trade that buys is stated rather than hidden** (maintainer decision,
2026-08-19). A probe reads the early, high part of a trajectory that PR4a measured declining to
roughly 0.75× by 600 s (`ag-sept-pr4.md` §3.21), so a bracket is selected on a *different quantity*
from the full-600 s horizon average that decides the retained comparison in §4.6.5. Longer probes
would narrow that gap and consume the budget the retained runs need. The mitigation is the selection
rule below, not a claim that the two quantities agree.

**`S` is the lowest worker level on the discovered plateau** (maintainer decision, 2026-08-19).
Its immediately lower reconnaissance level must be materially worse, and `H` is a higher level that
is not materially better. Three consequences follow, and each is a mistake this rule exists to
prevent:

- **A candidate must beat the level below it, not merely fail to be beaten from above.** §4.6.5's
  rule tests only that `H` produces no materially higher sustained Goodput, which establishes that
  `S` is not *below* the frontier and says nothing about `S` sitting past it. A too-high `S` would
  understate capacity at every topology while passing that rule unchallenged, and every efficiency
  derived from it would inherit the error silently.
- **Where throughput is flat across several levels, the lowest of them is the frontier.** Each
  delivers the same Goodput and the lowest does it with the least queueing, so selecting a higher
  one attributes capacity to workers that bought nothing. This is not hypothetical: local G2 and G4
  reconnaissance were both flat from 12 to 16 workers per group — 2.2% and 2.0% apart, inside the
  margin — while G1 was not, 16 exceeding 12 by 7.0%.
- **Reconnaissance that cannot satisfy the rule proposes no bracket.** The honest outcomes are to
  probe lower, or to extend the range; they are not to retain four 600 s runs at a level the
  reconnaissance declined to stand behind.

Deciding this at reconnaissance costs one short probe. Discovering it after the fact costs four
retained 600 s runs, which is why the rule belongs here rather than in §4.6.5.

**A common bracket across the topologies is a permitted, recorded exception to per-topology
selection** (maintainer decision, 2026-08-19). Reconnaissance selects per topology, and those
selections can differ for reasons that are not properties of the topology: on 2026-08-19 the local
run returned S=12, S=8, S=12, where `G2`'s 8 read within **0.8%** of its own 12 — inside the margin,
and far inside the single-probe variation §4.6.4 records above. `E2` and `E4` compare topologies, so
arms measured at different demand-per-group carry that difference into the efficiency itself, which
§2.3 forbids.

The maintainer may therefore fix one `S`/`H` bracket for every arm, subject to three conditions:

1. **the level must sit on each topology's own measured plateau** — probed by that topology's
   reconnaissance, and not materially below the best rate that reconnaissance observed there. A
   level a topology never probed, or one it measured materially below its own best, is refused;
2. **`H` must still satisfy §4.6.4's definition at every topology** — higher than `S`, and not
   materially better;
3. **the comparison must report that it ran at a common bracket**, naming both the common levels and
   what reconnaissance selected per topology, so a reader can see the substitution rather than
   infer it.

This is an exception to *which* level each arm runs at, and to nothing else. It does not relax the
lowest-plateau rule, which still decides each topology's own selection and remains what conditions
1 and 2 are checked against; and a comparison driven this way has not run the per-topology selection
end to end, so it must not be described as though it had.

The conditions are enforced rather than trusted: `itc-local-experiment.sh` validates a hand-set
bracket against each topology's retained probe table before driving anything, because this is the
one control that can move the retained operating point by hand.

#### 4.6.5 Retained closed-loop capacity points are 600 s, with both sides confirmed

For each topology, once reconnaissance identifies a candidate bracket:

1. establish the conditioned start state and run `S` for **600 s**;
2. from a fresh reset/reseed/conditioning sequence, run `H` for **600 s**;
3. from another fresh sequence, repeat `S` as a separate **600 s confirmation**;
4. from another fresh sequence, repeat `H` as a separate **600 s confirmation**.

The 600 s duration is a fixed Iteration C experiment parameter chosen to expose sustained behaviour
well beyond the short cached-plan transient PR4a diagnosed. It is not a claim that ten minutes is a
universal stationarity threshold; evidence may force a later explicit methodology revision.

Each 600 s run is analysed as ten contiguous **60 s time slices** for stationarity/evolution. Those
slices are observations of one trajectory, **not ten independent samples**. No destructive reset
occurs between slices.

**The comparison quantity is the full-600 s horizon average** (maintainer decision, 2026-08-18).
Every Iteration C number that enters a comparison — `S` against `H`, a point against its
confirmation, one topology against another, and any efficiency derived from them — is the
**fresh-mutation Goodput over the whole 600 s measured interval, from the same fixed conditioned
starting state**. Nothing is read from a sub-interval.

This is a definition, not a workaround, and it exists because the trajectory is not stationary
within the window. PR4a measured both topologies declining and then plateauing across the 600 s
(`ag-sept-pr4.md` §3.21), which leaves two ways to state a run's Goodput and only one of them
comparable:

- **the horizon average**, which asks what the topology delivers over a fixed horizon from a fixed
  starting state — a question every arm answers the same way; or
- **a later plateau**, which asks what it settles to — and cannot be compared across arms, because
  each arm reaches its plateau at a different elapsed time and a different accumulated dataset
  size, so the selection itself would carry the difference the comparison is trying to measure.

**The slices therefore describe and disqualify; they never select.** They show evolution, expose a
run whose shape says it belongs to an invalid regime, and let a reader see what the average
averages. Reading a headline number from slices 6–10 because the trajectory looks flatter there is
exactly the cherry-picking this rule forbids, and it would make the reported figure a function of
where the reader chose to start.

**Dataset state at both ends of the interval is part of the evidence**, not an incidental
observation. A horizon average is only interpretable beside the state it began from and the state
it reached: the conditioned starting state is fixed and declared, and the per-authority row and
data volume at the start and end of the measured interval are retained under §4.6.6. Two arms whose
starting states differ are not comparable however carefully their averages are computed.

Whether an indefinitely growing dataset is the right thing for this benchmark to represent at all
is a separate and open question, carried to Analyse & Review rather than settled here.

`S` is selected only when the `S/H` relationship is reproducible: the deciding `H` run and its
confirmation both fail to produce higher sustained Goodput — the horizon average defined above —
than the corresponding selected-point runs, or reproducibly fail a legitimate load-induced SLO/system gate. A fixture, generator,
measurement-system, unrelated environment, or qualification failure at `H` cannot establish the
knee. If the two `H` observations disagree materially, or either belongs to an unrelated invalid
regime, the knee remains unresolved; investigate or move the bracket rather than averaging the
disagreement into a result.

**"Materially" is 5% of the compared quantity, and it is a preselected engineering materiality
margin — not a noise floor** (maintainer decision, 2026-08-19; rationale corrected 2026-08-19 after
review). It states the smallest difference in sustained Goodput this experiment is willing to treat
as a real difference in capacity. It is chosen in advance, before any comparison, so that a knee is
not decided by a threshold picked to fit the numbers.

**Materiality and reproducibility are two separate gates, and conflating them is what the original
wording did.** That version called 5% "derived" from the ten identical `G4` cells that agreed to
within 2.6% ([`pr4a-rehearsal/repeats/`](../../measurements/pr4a-rehearsal/repeats/)) and said a
difference must exceed "what this machine can distinguish from noise". PR4b then measured four
identical `G1` runs spanning **25.1%**, so 5% plainly does not bound this environment's noise, and a
rationale resting on that reading would have been falsified by the milestone's own evidence. The
2.6% population informed the *choice* of margin; it never licensed treating the margin as a
measurement of noise, and no single environment-wide noise figure exists to license it — the same
machine reproduces to 1.4% at `G4`.

Separated, each gate does one job:

- **materiality** asks whether a difference is large enough to matter — is `H` meaningfully above
  `S`, is a point meaningfully apart from its confirmation;
- **reproducibility** asks whether this environment can measure the point at all. A point whose two
  observations differ by more than the margin has not been measured to the resolution the comparison
  requires, and is inadmissible regardless of what its mean would have been.

**So `G1`'s failure strengthens the method rather than undermining its threshold.** The margin did
not fail; the environment failed the admissibility gate, and the rule refused to report a number
rather than reporting one it could not support. A margin widened until `G1` passed would have
described nothing but the choice of margin.

§4.6.4 brackets with the same threshold, so a bracket is judged by the standard it was chosen by.
Any analysis applying this rule must state the threshold it used: a knee decided by an unstated
margin cannot be checked.

The threshold operationalises the rule; it does not replace it. A difference below 5% is not
evidence of equality, and none of the other conditions above — an invalid regime, a fixture or
generator failure, an unsound run — becomes admissible by falling inside it.

This replaces the earlier rule that confirmed only the selected point. It also replaces the earlier
canonical “every ladder rung is a sustained run” shape: short reconnaissance discovers the bracket;
only the two load-bearing points and their independent confirmations receive the full retained
600 s treatment.

#### 4.6.6 Fixture and state trajectory are part of the evidence

The selected point, deciding higher point, and both confirmations must retain enough clean fixture
state to offer fresh mutations throughout conditioning **and** the full 600 s measured interval.
Fixture sizing therefore uses the deepest intended bracket, an expected maximum useful rate, the
600 s duration, the conditioning population, and explicit safety headroom. The same per-organisation
fixture size is then reused unchanged across G1/G2/G4 within the comparison.

An unexpected population of `business_refusal` attributable to spent fixture state invalidates the
point rather than describing capacity. An all-refusal run remains sound/provenance-bearing if its
accounting is correct, but it contains no mutation-capacity result.

Retain the data trajectory as well as the start state. Per-authority row/data/index/working-set
volume intentionally changes as four organisation datasets move from one authority at G1 to one per
authority at G4, and table growth during 600 s may itself affect work per request. These are
co-varying properties of the sharding experiment and must be named when interpreting scale
efficiency.

#### 4.6.7 Local sustained capacity characterisation

The scheduler-partitioned workstation runs the complete method above and derives:

```text
E2_local = G2_local / (2 × G1_local)
E4_local = G4_local / (4 × G1_local)
```

**Each `G_local` is the mean of that topology's two selected-point observations** (maintainer
decision, 2026-08-19). The selected point is measured twice, as §4.6.5 requires, and both runs are
full 600 s horizon averages from the same fixed conditioned start — neither is more canonical than
the other, so quoting one would discard a measurement of equal standing on the basis of running
order alone. Both observations and the spread between them are reported beside the mean: a mean
whose inputs are not shown cannot be checked, and the spread is the reader's evidence that the two
runs reproduced at all. The deciding `H` observations are never averaged into anything — they decide
whether the knee is resolved, and then leave the derivation.

**An unresolved knee withholds the figure rather than lowering it.** Where §4.6.5's conditions do
not hold, that topology has no `G_local`, and any efficiency dividing it is withheld with the reason
stated. This matters most for `G1_local`, which is the denominator of both efficiencies: an
unresolved `G1` withholds `E2_local` and `E4_local` together. An efficiency computed from a level
nothing selected reads exactly like a scaling result while being a statement about an arbitrary
operating point, and it is the number a report is most likely to quote.

This discharges `VAL-SCALE-6` when the method/evidence gates hold. It is a quantitative capacity and
scale characterisation of the **explicitly recorded local environment**, not rehearsal-only data.
Its shared workstation/WSL kernel/storage/cache resources remain a first-class limitation, so
`E2_local`/`E4_local` do not discharge `VAL-SCALE-5`, are not Tier 1 or Tier 2, and are never mixed
with independently provisioned points.

#### 4.6.8 Tier 1 — independently provisioned capacity verification

Tier 1 remains the strongest Iteration C result and the only path here that can establish aggregate
mutation-capacity scaling across independently provisioned shard groups.

It presupposes a complete G1/G2/G4 family of equivalent independently provisioned capacity-unit
hosts plus generator compute separate from the serving units. Apply the same qualified method,
workload semantics and fixed configuration used to make the local result interpretable. AWS EC2 is
the currently selected bounded mechanism under `deployment-architecture.md` §13, not an
architectural requirement in itself.

Derive only within that environment:

```text
E2_aws = G2_aws / (2 × G1_aws)
E4_aws = G4_aws / (4 × G1_aws)
```

`E2_aws` and `E4_aws` are results, not gates. Where both local and AWS evidence exist, comparing
`E*_local` with `E*_aws` is useful evidence about how well scheduler partitioning approximated
independent resource envelopes; it does not retroactively promote the local result.

If quota or another external provisioning limit prevents the complete equivalent G4 environment
from existing, there is no Tier 1 result. Record the blocker and leave `VAL-SCALE-5` explicitly
unproven; neither the complete local experiment nor a partial AWS topology substitutes for it.

#### 4.6.9 Tier 2 — operating-point horizontal scale when a complete environment cannot be driven

Tier 2 presupposes that the complete independently provisioned `G4` environment **exists** but a
demonstrated measurement-system limit prevents Tier 1 from establishing saturation.

Select the highest useful common per-group worker level `L` the complete environment can drive
without becoming the plausible limiter, and retain `g1(L)`, `g2(L)` and `g4(L)` under the same
correctness/reconciliation/resource controls. Derive:

```text
E2(L) = g2(L) / (2 × g1(L))
E4(L) = g4(L) / (4 × g1(L))
```

The conclusion is deliberately bounded: **horizontal scaling is established at `L`; aggregate
capacity scaling remains unresolved**, so Tier 2 does not discharge `VAL-SCALE-5`.

A generator limit is not a soft Tier-2 exit. If PR4a qualified generator headroom above the intended
range and a later run contradicts that result, first record the mismatch, resize/requalify where
reasonably possible, and determine the real measurement frontier. Tier 2 is available only after a
remaining demonstrated measurement-system limit prevents Tier 1; “the generator could not drive it”
without that diagnosis is insufficient.

#### 4.6.10 Bounded open-loop comparison is a second lens

Closed-loop remains the capacity-selection method. After the sustained closed-loop result is
secure, `VAL-LOAD-1` may run offered-load points below, around and above the observed sustainable
rate to expose latency, queueing, timeout/error and Goodput behaviour when arrival rate no longer
self-throttles as service latency rises.

An open-loop driver must bound outstanding work with explicit request deadlines and a maximum
in-flight count, and account for arrivals that could not be launched because that bound was
reached. The open-loop round does not redefine closed-loop capacity and does not enter `E2`/`E4`.

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

Run the refusal control in §3.5 and prove that Phase 1 creates no partial booking state.

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

## 7. Scaling and load validations

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

Run the §4.6 method on the complete independently provisioned G1/G2/G4 family for
`WL-MUT-DISP-4`. Each group is driven by its own closed-loop worker pool at the declared
`workers_per_group`; one group's latency/backpressure must not reduce another group's configured
workers. Tier 1 derives `E2_aws` and `E4_aws` and identifies or conservatively bounds the limiting
mechanism at each relevant frontier.

The validation passes when the S/H relationship and **both** points reproduce, correctness and
conditioning-aware reconciliation hold, fixture headroom remains throughout, resource envelopes
are comparable, generator/shared-environment effects cannot plausibly explain the result,
per-group demand independence is proven, and limitations/co-varying data and per-organisation
intensity are stated. It does **not** require an efficiency percentage.

If the complete environment exists but a proven measurement-system limit forces §4.6's Tier-2
path, retain that operating-point horizontal-scale result but report `VAL-SCALE-5` as **unproven**.
If the complete equivalent environment cannot be provisioned at all, neither tier applies: record
the external limitation and report `VAL-SCALE-5` as **unproven** with no local or partial-AWS
substitute.

### VAL-SCALE-6 — Local scheduler-partitioned shard-group capacity characterisation

**Requirements:** REQ-COR-1, REQ-SCALE-1, REQ-EVID-1, REQ-EVID-2.

Run the same §4.6 qualified method across local G1/G2/G4 with the declared non-overlapping
scheduler CPU partition and shared-host resource evidence. Derive `E2_local` and `E4_local`, retain
the S/H points and both confirmations for each topology, and identify or conservatively bound the
local limiting mechanisms and shared-host effects.

The validation passes when the quantitative local result is admissible and reproducible **for that
explicit workstation environment**. Passing it does not establish independent resource-envelope
composition, does not satisfy REQ-SCALE-4's strongest claim, and does not discharge `VAL-SCALE-5`.
The point of the validation is to retain useful controlled evidence without laundering shared-host
partitioning into independent provisioning.

**Executed 2026-08-19 and NOT discharged.** Every stage of the method ran and its gates held; the
environment did not support the result. One qualification: the retained runs used a common
cross-topology bracket under §4.6.4's recorded exception rather than each topology's own selection,
so `G2` was measured at 12/16 where its reconnaissance had selected 8/12. `G4_local` resolved at 3493.9/s, but `G1`'s run-to-run
spread is 25.1% under identical conditions — five times this plan's own 5% margin — so `G1_local`
and `G2_local` are withheld, and with them both efficiencies. Goodput tracks delivered write
bandwidth at a near-constant 65–72 mutations per MiB — across the four identical `G1` runs, Goodput
spans 25.1% while that ratio spans 1.0% — which places the limit on the shared storage path rather
than on `alloca-go`. Whether reproducibility improves *because* more authorities issue I/O
concurrently is a reading of the retained evidence rather than a result established by it: the
identical-run control was driven at `G1` only. Status, evidence and the maintainer's decision to
stop execution are recorded in §9 and in `ag-sept-pr4.md` §3.28–§3.31.

**The reproducibility requirement is the part that failed, and it is not negotiable.** A single
admissible `G1` reading exists; what does not exist is evidence that it describes the topology
rather than one draw from a 25%-wide distribution. Widening the margin to admit it would make the
validation pass by redefining the standard it is meant to enforce.

### VAL-LOAD-1 — Bounded open-loop load-response characterisation

After the closed-loop capacity result is established for the environment being characterised,
drive a bounded set of offered arrival rates below, around and above that observed sustainable rate.
Retain offered arrivals, launched requests, completions, Goodput, latency, outcome/timeout rates,
maximum in-flight occupancy, and arrivals not launched because the in-flight bound was reached.

The validation is admissible when those populations reconcile, request deadlines and the
max-in-flight bound are explicit, and generator headroom is established. Its output describes the
latency/reliability response to fixed offered demand. It does **not** select closed-loop capacity,
does not enter `E2`/`E4`, and is not required to discharge `VAL-SCALE-5` or `VAL-SCALE-6`.

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

For Iteration C, PR4a first qualifies a generator configuration above the intended local G4
reconnaissance/retained range. PR4b still proves headroom at every quoted local server point. If
PR4c later executes independently provisioned verification, that environment re-establishes
headroom for every quoted point rather than inheriting the workstation result.

A later observation that contradicts PR4a's headroom qualification is recorded as a discovered
measurement-system mismatch first. Resize/requalify where reasonably possible before treating a
remaining demonstrated generator limit as a reason to use Tier 2.

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
service/database resource evidence to establish that each independently provisioned shard-group
host had the intended equivalent envelope and that material host/environment variation is
explained, excluded, or conservatively bounded.

A run in which one capacity-unit host is materially constrained relative to its peers is evidence
about that constraint, not a clean independently provisioned scale-efficiency point.

The local `VAL-SCALE-6` result retains analogous resource evidence but cannot satisfy this control's
independent-host premise merely by assigning disjoint CPU sets.

### VAL-NEG-8 — Per-shard-group demand independence

Before interpreting either `VAL-SCALE-5` or `VAL-SCALE-6`, prove that the multi-group closed-loop
driver maintains a separate fixed worker pool per active shard group. The discriminating control
deliberately delays, constrains, or otherwise slows one target group while a healthy control group
remains available. The slowed group may complete fewer requests because its own workers are blocked;
the healthy group must retain its configured `workers_per_group` and continue issuing independently
rather than losing demand because workers are shared globally.

The proof may be a deterministic generator-level integration test rather than a capacity run, but
it must fail against the old shared-pool/round-robin design. Equal aggregate request counts in a
healthy run are not sufficient evidence: the defect appears specifically when one group's response
time diverges. Independent streams also require disjoint idempotency-key sequence spaces; an
implementation that starts each group at sequence zero must include stable group identity in the
key rather than minting the same logical key in several streams.

## 9. Current validation status

| Validation area | Status | Authoritative evidence / next analysis |
|---|---|---|
| single-authority frontier | established | PR2 measurement report; produced Iteration B problem |
| response validation and generator headroom | established for the baseline scope | retained PR1/PR2 evidence |
| telemetry-overhead control | **not discharged** | PR2 found within-mode spread larger than the between-mode delta; no overhead figure is claimed |
| Phase 1 placement and supported policy implementation | established for Iteration B | PR3a/PR3b implementation records plus PR3c controls/evidence |
| multi-authority reconciliation | established for Iteration B | PR3b harness exercised and reconciled by PR3c retained runs |
| Phase 1 correctness and failure isolation | established for Iteration B | PR3c report and retained artifacts; VAL-COR-1..3, VAL-COR-5, VAL-COR-6 and VAL-FAIL-1 |
| cross-authority refusal (VAL-COR-4) | established for Iteration B | all four §3.5 clauses now hold on the deployed topology: the refusal and absence of partial booking state by the PR3c passes, and **same-key replay** by control 3b, retained in [`../../measurements/pr3c-phase1/controls-replay/`](../../measurements/pr3c-phase1/controls-replay/) |
| database-authority composition (VAL-SCALE-3) | established as architecture/correctness evidence | PR3c; explicitly **not** a capacity multiplier on the co-resident workstation |
| Iteration C measurement method | **defined and executed end to end** | §4.6: explicit conditioning, fixed pool policy, independent `workers_per_group`, adaptive reconnaissance, retained 600 s S/H + confirmation of both, 60 s analysis slices. PR4b drove all twelve retained runs; the method held and its gates refused what they should |
| Iteration C local capacity (VAL-SCALE-6) | **executed; NOT discharged** | PR4b drove the full G1/G2/G4 method locally. `G4_local` = 3493.9/s resolved (S reproduces to 0.1%, H to 1.0%, H does not beat S); **`G1` and `G2` are explicitly unresolved** and `G1_local`, `G2_local`, `E2_local` and `E4_local` are all withheld. `G1`'s run-to-run spread is **25.1%** under identical conditions — five times the margin — so its knee is not resolvable on this environment by any run order. The evidence localises the limit to the shared write path rather than to `alloca-go`, without proving the mechanism: Goodput tracks delivered write bandwidth at a near-constant 65–72 mutations per MiB, and across the four identical `G1` runs Goodput spans 25.1% while that ratio spans 1.0%. The killing test — `G1` with its data directory off the VHDX — was not run. Whether reproducibility improves *because* more authorities issue I/O concurrently is a reading of the evidence, not a result — the identical-run control was driven at `G1` only. Evidence: [`../../measurements/pr4b-capacity/`](../../measurements/pr4b-capacity/), [`../../measurements/pr4b-drift-g1/`](../../measurements/pr4b-drift-g1/), disk series in [`pr4b-capacity/disk-io-backfill/`](../../measurements/pr4b-capacity/disk-io-backfill/); analysis in `ag-sept-pr4.md` §3.28–§3.31 |
| Iteration C independently provisioned capacity (VAL-SCALE-5) | **defined; externally blocked at present** | optional PR4c only when the complete equivalent environment can actually be provisioned; Tier 1 derives `E2_aws`/`E4_aws`; otherwise remains explicitly unproven |
| Iteration C per-group demand independence (VAL-NEG-8) | **established** | PR4a: one fixed worker pool, sequence and collector per shard group, with group identity in the idempotency key (`internal/loadgen/streams.go`). The control is discriminating and mutation-proved — [`streams_test.go`](../../../internal/loadgen/streams_test.go) drives the same fixture against both designs and **fails against the old shared-pool implementation**, which is what §2.4 requires of a negative control. Recorded in `ag-sept-pr4.md` §3.23. It is a prerequisite for interpreting `VAL-SCALE-6`, so it is stated here rather than left implicit in the PR4a record |
| Iteration C resource-envelope control (VAL-NEG-7) | **defined; not yet executed** | applies to independently provisioned `VAL-SCALE-5`; local resource evidence does not satisfy the independent-host premise |
| bounded open-loop response (VAL-LOAD-1) | **defined; optional after closed-loop result** | second lens only; does not enter closed-loop capacity or E2/E4 |
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
