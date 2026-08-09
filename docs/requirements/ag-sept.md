# AG-Sept — problems and requirements

**Status:** Living — problem/requirement record for the AG-Sept engineering iterations.  
**Scope:** record the durable problem framing that starts each AG-Sept iteration and identify the
system requirements that define what “sufficiently resolved” means.  
**Does not own:** design, validation scenarios, PR sequence, budget, or measured results.

The cross-cutting requirements themselves are owned by
[`system-requirements.md`](system-requirements.md). This document does not redefine them; it
records which ones a particular AG-Sept problem brings into scope and how evidence changed the
next problem.

The engineering loop is defined by
[`../development/engineering-process.md`](../development/engineering-process.md).

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

PR2 established PostgreSQL as the limiting subsystem at the measured single-authority frontier
while the Go service retained substantial compute headroom. The exact numbers, conditions,
limitations, and the still-undischarged telemetry-overhead control remain owned by
[`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md)
and its retained artifacts.

That evidence ended Iteration A and changed the next problem. Repeating service-replica
measurements against the unchanged saturated writer could refine behaviour below the frontier,
but could not answer how end-to-end writable capacity should scale.

## 2. Iteration B — compose independent writable database authority

### Problem

The evidence-backed problem is now:

> **What placement and coordination model lets independent organisation work use independently
> writable PostgreSQL authorities while preserving Alloca-Go’s accepted transactional invariants,
> routing ownership, replay safety, and failure containment?**

The difficult boundary is not merely “how to run two databases”. Reserve joins two independent
ownership axes: slot capacity and the user schedule. If those axes resolve to different writable
transaction domains, one local PostgreSQL transaction can no longer coordinate the complete
operation.

Iteration B therefore needs to establish a horizontally composable local path without pretending
that cross-database atomic booking has already been solved.

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

### Design consequence already accepted

The accepted Phase 1 answer is intentionally narrower than cross-database booking:
organisations have explicit writable homes; supported reserve keeps user and slot ownership axes
inside the same database authority; cross-database-authority reserve is explicitly refused rather
than implemented as two unrelated local commits.

The design is owned by
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md), within
the complete two-axis model in
[`../design/horizontal-scaling.md`](../design/horizontal-scaling.md).

### Sufficiently resolved when

Iteration B can end for AG-Sept scope when evidence demonstrates, at minimum:

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

## 3. Candidate next iteration — stateless service replicas

Service-replica scaling is a plausible next problem, not yet an automatically scheduled
continuation of Iteration B.

After Iteration B evidence is analysed and reviewed, the project asks whether the remaining
high-value problem is:

> **What useful capacity or availability do additional stateless replicas add within one
> database-authority shard group, and how does per-authority connection pressure change that
> result?**

If that is still the right problem, it begins a new iteration at **Problem**. Requirements and
design may remain largely unchanged; the validation plan already records the candidate replica
and connection-budget validations, and only then should the milestone schedule commit their
scope and budget.

If the Iteration B evidence exposes a more important problem, the loop starts from that problem
instead. The existence of candidate validation work is not itself a scheduling commitment.
