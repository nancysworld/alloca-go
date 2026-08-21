# AG-Sept — goal, problems, and requirements

**Status:** Living — goal/problem/requirement record for the AG-Sept engineering iterations.  
**Scope:** record the durable goal that governs AG-Sept, the problem framing that starts each
iteration, and the system requirements that define what an acceptable resolution must preserve.  
**Does not own:** design, validation scenarios, PR sequence, budget, or measured results.

The cross-cutting requirements themselves are owned by
[`system-requirements.md`](system-requirements.md). This document does not redefine them; it
records which ones a particular AG-Sept problem brings into scope and how evidence changed the
next problem while the governing goal remained stable.

The engineering model is defined by
[`../development/engineering-process.md`](../development/engineering-process.md).

## Goal

> **Establish an evidence-backed horizontal-scaling story for Alloca-Go: identify the meaningful
> scaling boundaries, demonstrate how independent work can use additional service-compute and
> writable PostgreSQL resources without weakening correctness, and make the resulting architecture,
> evidence, and limitations explicit.**

The goal is deliberately broader and more stable than any one AG-Sept problem. It does not
preselect service replicas, database partitioning, Kubernetes, AWS, or another mechanism. Those
are possible answers only when a problem, requirements, design, and evidence justify them.

AG-Sept is sufficiently complete when Analyse & Review can support the goal for the agreed
milestone scope with retained evidence and explicit limitations. It does **not** require every
conceivable scaling problem to be solved; remaining problems may be deferred or become later
engineering goals.

## 1. Iteration A — identify the first scaling frontier

### Problem

AG-M1 established a correct transactional service, but Alloca-Go did not yet know which subsystem
would limit sustainable throughput first under controlled load.

The open problem was therefore:

> **Where is the first single-authority scaling frontier — application compute, PostgreSQL,
> connection/admission pressure, a hot logical authority, telemetry, the load generator, or the
> shared environment — and what next scaling problem follows from that evidence?**

The purpose was diagnosis, not to prove a preselected replica or database-sharding solution.

### Requirements in scope

- **REQ-COR-1** — the measured service must preserve transactional correctness under load;
- **REQ-SCALE-1** — independent work should be able to benefit from additional resources where
  correctness does not require serialization;
- **REQ-SCALE-3** — hot logical-authority serialization must remain explicit rather than being
  mistaken for a system-wide scaling failure;
- **REQ-EVID-1** — the frontier claim must be admissible and reproducible;
- **REQ-EVID-2** — generator, telemetry, connection, database, application, and shared-environment
  limits must remain distinguishable claims.

### Sufficiently resolved when

Iteration A was sufficiently resolved when retained evidence could identify or tightly bound the
first limiting subsystem without violating correctness and without mistaking the measurement
system for the service frontier.

A complete explanation of every secondary mechanism was not required before the next problem
could be framed; unresolved limitations had to remain explicit.

### Analyse & Review outcome

The closure record `../development/engineering-process.md` §1.4.1 requires. Detail stays in the
owning report; this is the durable outcome.

**1. Problem verdict — sufficiently resolved.** The first single-authority scaling frontier is
identified: PostgreSQL, not application compute.

**2. Evidence.**
[`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md)
and its retained artifacts. PostgreSQL sets the measured frontier while the Go service retains
substantial compute headroom. The report owns the numbers, conditions, and limitations — including
the ±2× environmental caveat and the **still-undischarged telemetry-overhead control** (VAL-NEG-3,
where within-mode spread exceeded the between-mode delta, so no overhead figure is claimed).

**3. Durable learning.** Service-compute scaling and writable-database-authority composition are
distinct axes whose evidence must not be merged — now REQ-SCALE-1 and REQ-EVID-2, with the
system shape in [`../design/horizontal-scaling.md`](../design/horizontal-scaling.md). The
undischarged telemetry control is carried in the validation plan's status table rather than being
quietly dropped.

**4. Goal progress.** One of the goal's three parts — identifying the meaningful scaling
boundaries — is established for the single-authority case. What remains: demonstrating that
independent work can use additional writable resources without weakening correctness, and making
the resulting architecture and limitations explicit.

**5. Loop decision — continue.** The goal is not sufficiently achieved, so a new iteration starts
at **Problem**. Repeating service-replica measurements against the unchanged saturated writer
could refine behaviour below the frontier, but could not answer how end-to-end writable capacity
should scale. The next problem is therefore Iteration B, below.

## 2. Iteration B — compose independent writable database authority

**Iteration B spans PR3a, PR3b and PR3c.** PR3a and PR3b built the placement model, booking policy,
multi-authority topology and authority-aware verification; PR3c produced the retained correctness
and failure-isolation evidence. The iteration closes here at Analyse & Review rather than inside
its evidence-producing PR.

### Problem

The evidence-backed problem was:

> **What placement and coordination model lets independent organisation work use independently
> writable PostgreSQL authorities while preserving Alloca-Go’s accepted transactional invariants,
> routing ownership, replay safety, and failure containment?**

The difficult boundary is not merely “how to run two databases”. Reserve joins two independent
ownership axes: slot capacity and the user schedule. If those axes resolve to different writable
transaction domains, one local PostgreSQL transaction can no longer coordinate the complete
operation.

Iteration B therefore needed to establish a horizontally composable local path without pretending
that cross-database atomic booking had already been solved.

### Requirements in scope

- **REQ-COR-1** — every supported Phase 1 operation preserves the accepted transactional model;
- **REQ-COR-2** — ambiguous mutations remain conservatively replay-safe through authority
  failure/recovery;
- **REQ-SCALE-1** — independent organisations can use independent writable resources;
- **REQ-SCALE-3** — one hot slot, identity, or database authority remains an explicit local
  serialization/resource boundary;
- **REQ-ROUTE-1** — one version-compatible placement model is enforced rather than trusted only
  to the caller/generator;
- **REQ-FAIL-1** — loss or saturation of one authority does not redirect authoritative writes to
  another and should not prevent unrelated healthy authorities from progressing;
- **REQ-DEPLOY-1** — the multi-authority deployment is identifiable and operable as a coherent
  topology;
- **REQ-EVID-1** and **REQ-EVID-2** — correctness/failure conclusions remain reconciled and
  distinct from unsupported capacity-composition claims on shared workstation resources.

### Design consequence accepted for Phase 1

The accepted Phase 1 answer is intentionally narrower than cross-database booking:
organisations have explicit writable homes; supported reserve keeps user and slot ownership axes
inside the same database authority; cross-database-authority reserve is explicitly refused rather
than implemented as two unrelated local commits.

The design is owned by
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md), within
the complete two-axis model in
[`../design/horizontal-scaling.md`](../design/horizontal-scaling.md).

### Sufficiently resolved when

Iteration B could end for AG-Sept scope when evidence demonstrated, at minimum:

- supported same-authority booking preserves the accepted invariant set across multiple writable
  authorities;
- colocated cross-organisation booking remains supported;
- unsupported cross-database booking is an explicit replayable policy result with no partial
  mutation;
- misrouting is rejected by the system rather than prevented only by a correct generator;
- reconciliation succeeds across participating authorities under the measurement contract;
- one authority’s failure is contained to its own dependency boundary and recovery does not need
  compensating writes on an unrelated authority;
- limitations of the shared-workstation environment remain explicit rather than promoted into a
  capacity multiplier.

The concrete validation is owned by
[`../planning/ag-sept/milestone-validation.md`](../planning/ag-sept/milestone-validation.md).

### Analyse & Review outcome

**1. Problem verdict — sufficiently resolved.** The Phase 1 placement and coordination model is a
correct horizontally composable authority boundary for the supported path. Independent writable
PostgreSQL authorities can serve their own organisation work without weakening the accepted
transaction semantics, and failure of one authority remains inside its shard-group dependency
boundary. Cross-authority reserve remains deliberately unsupported rather than being implemented
with an unproven distributed transaction protocol.

**2. Evidence.**
[`../measurements/reports/ag-sept-pr3c-phase1-correctness.md`](../measurements/reports/ag-sept-pr3c-phase1-correctness.md)
and its retained artifacts, produced by PR #16 after the PR3a/PR3b design and harness work.
Two hardened retained passes demonstrate supported correctness, colocated cross-organisation
booking, explicit cross-authority refusal, placement enforcement, multi-authority reconciliation,
and failure containment. A separately retained live `unknown_replayable` demonstrates the
measurement/reconciliation population contract on a real fault — as **one observation and not a
rate**, at an earlier commit whose harness had not yet gained the containment assertions, so its
accounting claim rests only on that run's retained client totals and verdict row counts and not on
its unasserted containment properties (report §5.2). The report explicitly makes **no capacity or
throughput multiplier claim** from the co-resident 10-vCPU workstation topology.

**3. Durable learning.** Authority composition is now a correctness-proven architecture, not yet a
capacity-proven scaling result. The shard group was already the accepted scaling unit
([`../design/horizontal-scaling.md`](../design/horizontal-scaling.md) owns that model); what
Iteration B adds is that the accepted shard-group boundary **survived correctness, composition and
failure-isolation validation on the supported path, and is therefore a candidate capacity unit
whose capacity behaviour remains unproven.** Changing the number of authorities and changing
replica count within one authority remain distinct experiments. PR2's mutation-heavy evidence still
puts the first limit in PostgreSQL, so service replicas or microservice decomposition are not
promoted without evidence that service compute or an independent service boundary is actually
limiting.

**An evidence obligation follows for any later capacity claim, independently of mechanism.** PR2's
unexplained ~2× environmental excursions slow service, database and generator together, and the
frontier report §6 records that no amount of database instrumentation removes that caveat. A future
iteration therefore cannot credibly distinguish linear from materially sub-linear scaling unless
shared-environment variation is **explained, excluded, or conservatively bounded**. That obligation
is durable; which instrument discharges it is a Validation/Design choice, not settled here.

**Report §7.2's two observations are carried forward together, as one diagnostic family**, because
they concern the same failure-path behaviour and neither is promoted into the next Problem:

- the ~500 ms warm/failure-path latency against the ~5 s stopped-authority observation recorded in
  [`../development/implementation/ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md)
  §6b; and
- `timeout_server` holding at exactly 304 across all three retained failure runs while
  `internal_failure` moved freely and total volume varied by 4.2% — a quantity that is bit-identical
  across runs whose other outcome counts are not, which the report calls structural rather than
  incidental and does not explain.

The trigger is the concrete one the report itself states: **neither reading may be used to change a
timeout budget**, and the family is investigated if a decision comes to depend on the failure
path's timing behaviour.

One item of **named validation debt** is carried, and it blocks neither the verdict nor the scaling
work: INV-21's live “commit landed, acknowledgement lost” injection, tracked by the invariant
register.

**A second item was found by this review and closed rather than carried.** `VAL-COR-4`'s exit
criterion asks for “an explicit replayable policy result with no partial mutation”. The refusal and
the absence of partial mutation were established on the two-authority stack, but *replayability*
was established a layer below it, by `TestCrossAuthorityRefusalIsReplayable` at the service layer
and `TestRefusalIsRecordedAndReplayed` on the PostgreSQL adapter, because the retained control
drives 2,000 distinct keys and reposts none of them (PR3c report §7.4). The criterion was therefore
met by composition rather than by one observation on the deployed stack.

The fix was one repost rather than an experiment, so it was made here instead of deferred: the
`controls` cell gained case 3b, which reposts the cross-authority refusal's own key and asserts the
recorded reason and `replay=true` against `replay=false` on the decision itself, so what the control
shows is the transition rather than a flag. VAL-COR-4 is **established for Iteration B** on the
evidence retained in
[`../measurements/pr3c-phase1/controls-replay/`](../measurements/pr3c-phase1/controls-replay/).

Two things this deliberately did **not** do. The retained PR3c cells were not re-run or
reinterpreted — their measured numbers are unchanged, and 2,000 persisted records still do not
evidence replay. Nor were the PR3c report's findings revised: its §7.4 describes the runs it
describes and stays accurate about them, so the section gained a narrower heading and a forward
pointer rather than a correction.

No accepted Phase 1 requirement or architecture needs revision.

**4. Goal progress.** Iteration B establishes that independent work can be partitioned across
independently writable PostgreSQL authorities **without weakening correctness**. It does not yet
establish that adding independently provisioned writable resources increases aggregate mutation
capacity, because PR3c's authorities shared one workstation resource envelope.

**The service-compute clause, answered on the retained resource evidence rather than by
assumption.** PR3c §6 was retained specifically for this review. It shows very low service pressure
on the healthy path — roughly 0.32 service cores per unit over the failure window against a shared
10-vCPU allocation, and pool acquire-wait of 0.00–0.02 s on every healthy cell in both passes. But
those cells were bounded correctness runs at concurrency 8, deliberately sized so every request
could succeed, and the report says plainly that they say nothing about the far higher-concurrency
regime where PR2 found acquire-wait to be the dominant term. The retained resource evidence
therefore **does not overturn PR2's database-bound conclusion**, and it does not expose a service
frontier. Service compute is consequently not the next meaningful frontier on current evidence, and
adding replicas to an already database-bound shard group is not justified merely to satisfy the
wording of the goal. The clause remains open rather than discharged.

**5. Loop decision — continue.** The goal is not yet sufficiently achieved. The next Problem is to
test whether the shard-group boundary that Iteration B established as a **candidate** capacity unit
behaves as one when shard groups are provisioned independently. Service-replica scaling remains a
later candidate when a workload or resource balance makes service compute a meaningful frontier.

## 3. Iteration C — characterise shard-group capacity scaling

### Problem

> **How does aggregate mutation capacity scale as independently provisioned shard groups are added
> for independent organisation workloads, what limits that scaling, and what workload and placement
> envelope should each shard group own?**

Iteration B proved that independent writable authorities compose correctly **on the supported
path**; it deliberately did not prove that they scale capacity, and cross-authority reserve remains
refused rather than solved. Iteration C asks the next evidence-backed question: when work that can
proceed independently is given independently provisioned service and PostgreSQL resources, how does
useful capacity compose, what limits it, and where does one shard group's safe operating boundary
lie?

The question is deliberately phrased as *how does it scale* rather than *is it additive*. Iteration
C therefore requires a **numeric scale-efficiency result**, not a preselected efficiency threshold.
The result may be high, low, or materially sub-linear; the requirement is to measure it credibly and
explain the limiting mechanism rather than optimise the milestone toward a pass number.

### Requirements in scope

- **REQ-COR-1** — every measured capacity point preserves the accepted transaction semantics;
- **REQ-SCALE-1** — independent organisation work must be able to use additional writable resources;
- **REQ-SCALE-3** — any hot logical-authority ceiling remains explicit rather than being folded into
  the dispersed-workload result;
- **REQ-SCALE-4** — adding a shard group for the capacity comparison adds a controlled
  service-and-database resource envelope rather than subdividing one fixed host allocation;
- **REQ-DEPLOY-1** — every compared unit and topology remains identifiable and reproducible;
- **REQ-EVID-1** — G1, G2, G4 and their derived efficiencies are quotable only at the evidence level
  their retained provenance supports;
- **REQ-EVID-2** — generator, database, service, connection, workload-skew and shared-environment
  limits remain distinguishable explanations.

### Workload requirement

Iteration C selects the reusable
[`WL-MUT-DISP-4`](../design/workload-catalog.md#wl-mut-disp-4--four-organisation-dispersed-mutation-capacity)
workload: four equivalent independent synthetic organisations, `org-a` through `org-d`, each with
the same mutation semantics and equal demand share. The workload definition is stable; topology
placement is not part of it.

This separation is deliberate. Later experiments may apply the same workload to another database
technology, placement model, service topology, or elasticity mechanism and compare evidence without
also changing the demand. Different questions may select different named workloads from the same
catalog.

Read-heavy and realistic mixed read/write workloads remain important open questions — they can move
the limiting resource toward service compute, query/cache pressure, or connection concurrency — but
including them now would change the Problem instead of testing the mutation frontier exposed by PR2
and Iteration B. They remain roadmap work rather than Iteration C acceptance criteria.

### Capacity-composition requirement

The comparison must obtain capacity for the selected workload at **1, 2, and 4 shard groups** using
like-for-like shard-group resource envelopes. The exact validation matrix and organisation placement
are owned by the validation plan, but the meaning is fixed here:

- `G1` is the measured capacity of the one-group topology;
- `G2` is the aggregate measured capacity of the two-group topology;
- `G4` is the aggregate measured capacity of the four-group topology;
- scale efficiency at 2 and 4 groups is derived from those measured capacities under the
  measurement contract.

No efficiency percentage is a pass/fail requirement for Iteration C. A materially sub-linear result
is still a valid result if it is admissible and the limiting mechanism is identified or tightly
bounded.

### Environment requirement

The environment must let the 1/2/4 comparison **grow resources with shard-group count**. The current
co-resident workstation cannot satisfy that requirement for the intended capacity claim: its
services, PostgreSQL authorities, generator, storage path and host scheduler share one 10-vCPU
allocation, so adding groups redistributes a fixed host rather than adding equivalent capacity
units.

This requirement does not itself name a provider. Design must select the smallest reproducible
environment that supplies equivalent independently controlled shard-group envelopes and keeps the
load generator from becoming the shared resource that determines the result. The one-group baseline
and the multi-group points must be measured in that same environment; a workstation G1 must not be
compared with an externally provisioned G2 or G4.

### Sufficiently resolved when

Iteration C is sufficiently resolved for AG-Sept when retained evidence can state, for
`WL-MUT-DISP-4`:

- measured `G1`, `G2`, and `G4` under comparable independently provisioned resource envelopes;
- derived scale efficiency at 2 and 4 groups, with **no preselected efficiency threshold**;
- the limiting subsystem or resource at each relevant frontier, or a conservative bound where the
  evidence cannot identify one uniquely;
- a workload/resource envelope for one shard group that says what population/demand the measured
  capacity result applies to rather than promoting it into an all-workloads claim;
- correctness and reconciliation across every participating authority at every quoted capacity
  point;
- generator and shared-environment effects explained, excluded, or conservatively bounded strongly
  enough that they cannot masquerade as shard-group scale efficiency;
- explicit limitations, including that service-replica scaling, mixed/read-heavy workloads,
  cross-authority booking, elasticity/rebalancing, and production representativeness remain
  separate questions unless separately evidenced.

The concrete topology, repetitions, controls, and run admissibility are owned by
[`../planning/ag-sept/milestone-validation.md`](../planning/ag-sept/milestone-validation.md).
