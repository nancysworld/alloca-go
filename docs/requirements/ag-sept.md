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
multi-authority topology and authority-aware verification; PR3c is the evidence-producing work
unit. **The iteration does not close inside PR3c** — it closes at Analyse & Review, which is an
explicit step of its own afterwards (Nancy's call, 2026-08-10).

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

### Analyse & Review outcome

**Pending — Iteration B's evidence is not yet produced.** PR3c owes it.

**This review is an explicit step of its own, after PR3c completes**, not something PR3c performs
on itself and not a pre-created PR4. Its time comes from the milestone's existing 2–3 day
review/rerun/interpretation reserve; no new development allocation is created for it (Nancy's
call, 2026-08-10).

When PR3c reports, this section records the same five-part closure record §1.4.1 requires, written
to the worked example under Iteration A: problem verdict, evidence, durable learning, **goal
progress**, and the loop decision. The last two are the ones this iteration must not skip —
whether the problem is resolved and whether the AG-Sept goal is sufficiently achieved are
different questions.

**The review must explicitly revisit the goal's service-compute clause.** The goal asks for
evidence that independent work can use additional *service-compute* as well as writable-database
resources, and Iteration B addresses only the second. So the review asks, on the evidence:

- does the multi-authority result and its resource balance make service compute the next
  meaningful frontier?
- or must the resource allocation within a shard group change first, before a service frontier
  could even be exposed — in which case *that* is the next problem?
- or does another problem carry higher value, or is the goal sufficiently achieved for scope?

**The goal itself stands unchanged until that review.** If the evidence justifies revising it, that
is an explicit goal/scope decision, taken then and recorded here — never an implicit consequence of
what the next iteration happens to schedule.

Only that review may select the next iteration and release the reserved post-Iteration-B envelope
([`../planning/ag-sept-plan.md`](../planning/ag-sept-plan.md) §3). Until it happens, the envelope
buys nothing.

## 3. Candidate next iteration — stateless service replicas

Service-replica scaling is a plausible next problem, not yet an automatically scheduled
continuation of Iteration B.

After Iteration B evidence is analysed and reviewed against the governing goal, the project asks
whether the remaining high-value problem is:

> **What useful capacity or availability do additional stateless replicas add within one
> database-authority shard group, and how does per-authority connection pressure change that
> result?**

If that is still the right problem for achieving the goal, it begins a new iteration at
**Problem**. Requirements and design may remain largely unchanged; the validation plan already
records the candidate replica and connection-budget validations, and only then should the
milestone schedule commit their scope and budget.

It is not the only candidate. If Iteration B's evidence shows that service compute cannot become a
meaningful frontier without first changing the resource balance within a shard group, then
**rebalancing is the next problem** and replica scaling waits behind it. A third possibility is
that another problem carries more value, or that the goal is sufficiently achieved for scope and
the loop ends. The review chooses; this section only records what is currently most likely.

If the Iteration B evidence shows that the AG-Sept goal is already sufficiently achieved for
scope, the loop ends. If it exposes a more important problem, the next iteration starts from that
problem instead. The existence of candidate validation work is not itself a scheduling
commitment.
