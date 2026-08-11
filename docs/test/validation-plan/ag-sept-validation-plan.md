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
  — database-authority placement and Phase 1 booking semantics.

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

**Validation for Iteration C is not defined in this A&R PR.** The existing scaling validations
below retain their established meanings, but none is automatically promoted into the new
iteration merely because it already exists. Iteration C must proceed through Requirements,
Design, and Validation plan before Schedule commits an experiment matrix.

One constraint is inherited rather than chosen: any capacity claim must **explain, exclude, or
conservatively bound shared-environment variation**, because PR2's unexplained ~2× excursions
otherwise leave linear and materially sub-linear composition indistinguishable (frontier report §6;
`../../requirements/ag-sept.md` §2). That fixes what Iteration C's validation must achieve, not
which instrument achieves it.

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

### 2.4 Negative controls must be discriminating

A negative control succeeds only when the intended gate demonstrably notices the deliberately
introduced defect or constraint. Reasonable-looking output is not evidence that the gate is
active.

## 3. Controlled workloads

### 3.1 Dispersed workload

**Purpose:** expose the general application/database frontier while contention on any one slot
or user identity is low.

Defining properties:

- requests are spread across many slots and user identities;
- fixture/reset discipline prevents sold-out state from becoming the primary workload;
- no single logical business authority should dominate the run;
- exact counts, duration, concurrency, and rate are run parameters, not durable requirements.

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
| Cross-authority refusal control | 2 authorities × 1 replica | bounded cross-authority reserves, as their own evidence class | VAL-COR-4 |
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
- shared-workstation contention.

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

## 9. Current validation status

| Validation area | Status | Authoritative evidence / next analysis |
|---|---|---|
| single-authority frontier | established | PR2 measurement report; produced Iteration B problem |
| response validation and generator headroom | established for the baseline scope | retained PR1/PR2 evidence |
| telemetry-overhead control | **not discharged** | PR2 found within-mode spread larger than the between-mode delta; no overhead figure is claimed |
| Phase 1 placement and supported policy implementation | established for Iteration B | PR3a/PR3b implementation records plus PR3c controls/evidence |
| multi-authority reconciliation | established for Iteration B | PR3b harness exercised and reconciled by PR3c retained runs |
| Phase 1 correctness and failure isolation | established for Iteration B | PR3c report and retained artifacts; VAL-COR-1..3, VAL-COR-5, VAL-COR-6 and VAL-FAIL-1 |
| cross-authority refusal (VAL-COR-4) | **partially discharged** | the refusal and the absence of partial mutation are established on the deployed topology; §3.5's **same-key replay** clause is proven only in deterministic service/adapter tests and is not exercised by the retained control (PR3c report §7.4) |
| database-authority composition (VAL-SCALE-3) | established as architecture/correctness evidence | PR3c; explicitly **not** a capacity multiplier on the co-resident workstation |
| Iteration C shard-group capacity | **Problem selected; validation not yet defined** | Requirements → Design → Validation plan must precede Schedule |
| stateless replica scaling | unproven and not selected by this A&R | existing VAL-SCALE-1/2 remain candidate validation definitions, not committed Iteration C work |
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
