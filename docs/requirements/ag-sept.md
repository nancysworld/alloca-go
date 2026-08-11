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
[`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md).

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
measurement/reconciliation population contract on a real fault. The report explicitly makes **no
capacity or throughput multiplier claim** from the co-resident 10-vCPU workstation topology.

**3. Durable learning.**

- **Authority composition is now a correctness-proven architecture, not yet a capacity-proven
  scaling result.** `VAL-SCALE-3` is discharged in the validation plan's intended sense:
  architecture/correctness and failure-independence evidence on multiple writable authorities.
  Whether equivalent independently provisioned shard groups produce approximately additive useful
  capacity is a separate question and requires like-for-like independent resource envelopes.
- **The database authority plus its serving replica group is the meaningful writable scaling
  unit.** The general topology remains `M` writable authorities with replica allocation
  `[x1, ..., xM]`, `x_i >= 1`; changing `M` and changing an `x_i` are different scaling operations
  and must be measured separately.
- **Service replicas are not promoted merely because they are easy to add.** PR2's mutation-heavy
  evidence still says PostgreSQL is the first limiting subsystem and the Go service has headroom.
  Adding replicas within the same saturated writer may improve availability or a different
  workload, but no current evidence makes it the next capacity problem.
- **Application characteristics choose the mechanism.** Alloca-Go is currently database-bound on
  the measured mutation path, so speculative microservice decomposition, orchestration, caching,
  or other distributed machinery is not a substitute for evidence about the actual limiting
  boundary.
- **Two observations are carried forward without becoming Iteration C requirements.** The
  repeatable ~500 ms failure-path latency versus the earlier ~5 s stopped-authority observation is
  a diagnostic lead to investigate if timeout/pool behaviour becomes decision-relevant. INV-21's
  acknowledgement-lost fault injection remains explicit correctness-validation debt: the semantics
  and deterministic accounting branches are tested, but a live “commit landed, reply lost” fault
  has not yet been injected.

No accepted Phase 1 requirement or architecture needs revision as a result of PR3c.

**4. Goal progress.** Iteration B establishes that independent work can be partitioned across
independently writable PostgreSQL authorities **without weakening correctness**, which closes the
architectural/correctness half of the writable-resource clause in the governing goal. It does not
yet establish that adding independently provisioned writable resources increases aggregate
mutation capacity, because PR3c's authorities shared one workstation resource envelope. The
service-compute clause also remains open: the current mutation-heavy evidence does not justify
adding replicas to an already database-bound shard group merely to satisfy the wording of the
goal.

**5. Loop decision — continue.** The goal is not yet sufficiently achieved. The next problem is
not “add service replicas”; it is to test whether the correctness-proven shard-group boundary is
also an effective **capacity** scaling unit under independent provisioning. That starts Iteration C
below. Service-replica scaling remains a later candidate when a workload or resource balance makes
service compute a meaningful frontier.

## 3. Iteration C — establish shard-group capacity scaling

### Problem

> **Can independently provisioned shard groups turn the single-authority PostgreSQL frontier into
> approximately additive aggregate mutation capacity for independent organisation workloads, and
> what workload and placement envelope should each shard group own?**

Iteration B proved that independent writable authorities compose correctly; it deliberately did
not prove that they scale capacity. Iteration C asks the next evidence-backed question: when work
that can proceed independently is given independently provisioned service and PostgreSQL
resources, how efficiently does useful capacity compose, what prevents linearity, and where does
one shard group's safe operating boundary lie?

This is a capacity question about the architecture already accepted. It does **not** preselect AWS,
Kubernetes, a managed database, service-replica multiplication, or another deployment mechanism.
The validation environment must provide resource independence strong enough to support the claim;
the smallest mechanism that does so should be chosen in Design and Schedule.

### Requirements in scope

- **REQ-COR-1** — scaling evidence remains conditional on preserved transactional correctness;
- **REQ-SCALE-1** — independent work should benefit from independently provisioned resources;
- **REQ-SCALE-3** — hot slot, identity, organisation, or authority boundaries remain explicit and
  are not averaged away by aggregate throughput;
- **REQ-FAIL-1** — the capacity unit must retain the failure-isolation property established in
  Iteration B;
- **REQ-EVID-1** — baseline and scaled runs must be like-for-like, reproducible, and admissible at
  the level required for the claim;
- **REQ-EVID-2** — service, PostgreSQL, connection, generator, workload-skew, and shared-resource
  limits must remain distinguishable explanations for scale efficiency.

The existing requirements remain sufficient at Problem framing. Design and validation may expose
a missing durable requirement; if so it is added explicitly rather than hidden in the experiment
mechanism.

### Questions the validation must be able to distinguish

These are experiment dimensions, not conclusions or scheduled matrix cells:

- **Horizontal efficiency:** with equivalent independent work and equivalent independent resource
  envelopes, does aggregate goodput approach the sum of the shard groups' single-unit goodput?
- **Fixed-demand relief:** when a known independent organisation workload is spread over more
  writable authorities, does the database bottleneck move as predicted rather than merely moving
  contention somewhere else?
- **Workload shape:** how do mutation-heavy, read-heavy, or realistic mixed workloads change the
  limiting resource? The current evidence is mutation-dominated and must not be generalized to a
  read-heavy deployment without measurement.
- **Placement/skew:** shard sizing follows active request rate, concurrency, contention, workload
  mix and resource demand more directly than raw organisation or user count. Counts are useful
  placement proxies only if evidence shows they correlate with load.
- **Replica allocation:** one service replica per authority (`x_i = 1`) is a plausible mutation-path
  baseline, not an architectural law. Additional replicas within one shard group are tested only
  when service compute, availability, or a read-heavy workload gives a reason to test them.

### Sufficiently resolved when

Iteration C is sufficiently resolved when retained evidence can say, for at least one controlled
independent-organisation workload and resource envelope:

- what one shard group's sustainable mutation-capacity baseline is under the chosen environment;
- how aggregate goodput changes when equivalent independently provisioned shard groups are added;
- the resulting horizontal scale efficiency under like-for-like conditions;
- which subsystem limits each measured topology, with generator/shared-environment alternatives
  ruled out strongly enough for the claim;
- that correctness and the Iteration B authority/failure boundaries remain intact;
- what workload/placement conditions bound the conclusion, including hot-authority or skew cases
  where aggregate independence does not apply.

The Validation plan and Schedule are downstream of this A&R decision and are not committed by this
Problem record.

### Analyse & Review outcome

**Pending.** Iteration C starts at Problem after this Iteration B A&R is accepted. Requirements,
design, validation, and schedule follow in dependency order.
