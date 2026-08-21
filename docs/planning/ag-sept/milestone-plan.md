# AG-Sept — milestone plan

**Status:** Living — August publication checkpoint in progress; Iteration C remains open  
**Delivery window:** August 2026 for the public-release checkpoint; Iteration C resumes when its independent-capacity environment is available  
**Predecessor:** AG-M1 — correct transactional core and end-to-end service path  
**Supersedes:** [`../ag-sept-plan-v0.4.md`](../ag-sept-plan-v0.4.md), retained as the archived plan used by PR1 and PR2.

## What this document owns

This document owns **schedule**: priority, sequence, active work-unit scope, current budget constraints,
descope order, and milestone/publication status. It does not own requirements, system design,
validation meaning, implementation mechanics, or measured conclusions.

| Concern | Owner |
|---|---|
| Goal, current Problem, requirements | [`../../requirements/ag-sept.md`](../../requirements/ag-sept.md), [`../../requirements/system-requirements.md`](../../requirements/system-requirements.md) |
| System and workload design | [`../../design/`](../../design/) |
| Validation intent and `VAL-*` status | [`milestone-validation.md`](milestone-validation.md) |
| Evidence admissibility | [`../../design/measurement-contract.md`](../../design/measurement-contract.md) |
| Measured results | [`../../measurements/`](../../measurements/) |
| Implementation history | [`../../development/implementation/`](../../development/implementation/) |

This plan is deliberately non-normative for runtime meaning. Code, tests, requirements, and durable
design must not depend on it for architectural truth
([`../../development/engineering-process.md`](../../development/engineering-process.md) §6.3).

## 1. Current state

AG-Sept has progressed through three engineering iterations:

- **Iteration A — identify the first scaling frontier. Resolved.** PR2 established PostgreSQL as the
  limiting subsystem while the Go service retained substantial compute headroom.
- **Iteration B — compose independent writable database authorities. Resolved.** PR #17 closed the
  iteration after Phase 1 correctness/composition and failure-isolation evidence.
- **Iteration C — characterise shard-group capacity scaling. OPEN.** PR4a qualified the method;
  PR4b completed the scheduler-partitioned G1/G2/G4 experiment; PR4c refined the independent probe
  but stopped before measurement because the required environment could not be provisioned.

The current scheduler-partitioned result is retained in
[`../../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md`](../../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md).
Its evidence boundary is concise:

- `G4_local = 3493.9/s` resolved;
- `G1` and `G2` did not resolve, so `G1_local`, `G2_local`, `E2_local`, and `E4_local` are withheld;
- `VAL-SCALE-6` was executed but is not discharged;
- identical G1 runs showed 25.1% variation, localised by retained evidence to the shared write path
  rather than `alloca-go`, without proving the deeper storage mechanism;
- no independently provisioned performance cell exists, so `VAL-SCALE-5` remains pending.

The PR4c work unit is closed; the Iteration C Problem is not. Iteration C resumes when an equivalent
independently provisioned environment can be created. AWS after quota approval is one option; another
provider/environment is acceptable if it satisfies the same design and validation contracts.

PR5 is now the **evidence checkpoint + publication-readiness** work unit. Public release is a
repository-readiness event, not Analyse & Review and not an `END` decision for Iteration C.

| Workstream | Work unit | Status |
|---|---|---|
| Measurement substrate and load harness | PR1 | merged |
| Single-instance frontier | PR2 | merged |
| Database-authority placement, harness, correctness and failure isolation | PR3a–PR3c | merged |
| Iteration B Analyse & Review | PR #17 | merged; Iteration B closed |
| Iteration C planning | PR #18 | complete |
| Iteration C method qualification | PR4a / #19 | merged |
| Scheduler-partitioned sustained characterisation | PR4b / #20 | merged; local result bounded as above |
| Independently provisioned probe attempt | PR4c | work unit closed at provisioning; `VAL-SCALE-5` pending |
| Evidence checkpoint + publication readiness | PR5 / #22 | in progress |
| Independent-capacity continuation | future Iteration C work unit | waiting for a qualifying environment |

## 2. Time budget

AG-Sept is timeboxed, but detailed allocation and contingency bookkeeping is **coordination history,
not durable technical meaning**. It was useful while the work was being scheduled; it does not need
to remain as a permanent accounting ledger in the milestone plan. Git/PR history and implementation
records preserve that history where it is useful to reconstruct.

The active constraint is simple:

- PR5 has a **1.5 development-day** budget for the evidence checkpoint and publication-readiness work;
- the remaining review/publication reserve may be used for the bounded pre-publication documentation
  pass;
- Iteration C continuation and its eventual Analyse & Review are **not** funded or scheduled by PR5;
  they receive a new schedule when the required independent environment exists.

Budget constrains scope; it does not weaken correctness or evidence gates.

## 3. Work-unit sequence

The sequence below records purpose and status only. Detailed implementation decisions belong in
[`../../development/implementation/`](../../development/implementation/); detailed results belong in
[`../../measurements/`](../../measurements/).

### PR1–PR2 — measurement substrate and first frontier — complete

PR1 established the load/measurement substrate. PR2 used it to identify PostgreSQL, rather than
`alloca-go`, as the first measured mutation frontier.

### PR3a–PR3c + Iteration B A&R — database-authority composition — complete

These work units established the Phase 1 authority model, multi-authority execution/reconciliation,
correctness and failure isolation. The result is a correctness/composition result, not a capacity
multiplier.

### PR #18 — Iteration C planning — complete

Selected `WL-MUT-DISP-4`, the shard-group capacity-unit model, numeric efficiency as an output rather
than a pass threshold, and the independent-resource requirement.

### PR4a — method qualification — complete

Qualified the sustained experiment: independent per-group demand, explicit conditioning,
state-preserving pool recycle, fixed pool policy, fixture headroom, retained 600 s runs,
resource/provenance evidence, and the controls required before interpreting a topology comparison.

### PR4b — scheduler-partitioned evidence — complete

Executed the complete local G1/G2/G4 sustained family. The accepted result and its limitations are
owned by the PR4 checkpoint report linked in §1.

### PR4c — independent probe attempt — stopped at provisioning

Refined the fixed-per-unit independent probe and attempted to provision it. No performance cell ran.
The reusable method is owned by
[`../../design/independent-capacity-probe.md`](../../design/independent-capacity-probe.md) and
[`milestone-validation-pr4c-aws-probe.md`](milestone-validation-pr4c-aws-probe.md); provisioning
state is retained under [`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/).

`STOP / DEFER` applies to this work unit, not to the Iteration C Problem.

### PR5 — evidence checkpoint + publication readiness — in progress

PR5 does not perform Analyse & Review. It:

- records the PR4 evidence boundary while the experiment context is fresh;
- keeps Iteration C explicitly open;
- applies the documentation-ownership/process changes needed before publication;
- performs the bounded external-reader/disclosure pass required before the repository changes
  visibility.

### Iteration C continuation — pending environment

Resume the independent-capacity validation when the environment contract can be satisfied. Formal
Analyse & Review happens only when that continuation reaches a genuine decision point.

## 4. Measurement and reconciliation staging

Measurement mechanics are not schedule-owned. The applicable rules remain in
[`../../design/measurement-contract.md`](../../design/measurement-contract.md) and
[`milestone-validation.md`](milestone-validation.md).

For scheduling purposes only:

- PR4a qualified the method and controls;
- PR4b exercised them on the complete scheduler-partitioned family;
- PR4c produced no measured run because provisioning failed first;
- no independent-capacity claim exists until the corresponding validation/evidence gates are
  satisfied.

## 5. Priority and descope

### 5.1 P0 — publication checkpoint

Before public release:

- retain the PR4 scheduler-partitioned evidence and its limitations without weakening evidence
  labels;
- keep `VAL-SCALE-5` pending and Iteration C open;
- make architecture, documentation ownership, navigation, and reproduction entry points clear to an
  external reader;
- complete disclosure/history/metadata review.

Successful independent-capacity measurement is **not** a publication prerequisite. It remains an
Iteration C technical requirement.

### 5.2 P1 — useful publication polish

Add diagrams, charts, examples, or further compression only where they materially improve first-read
understanding or remove duplication.

### 5.3 P2 — later technical work

Do not add new architecture merely to make the public repository appear more complete. Service
replicas, service decomposition, autoscaling, messaging, distributed tracing, richer deployment
machinery, or additional fault work require their own evidence-backed Problem.

### 5.4 Out of scope for the publication checkpoint

Cross-authority booking/distributed transaction protocols, online rebalancing, multi-region writes,
EKS/RDS/Kubernetes/service-mesh adoption, autoscaling, full production auth, and reproduction of a
commercial workload are not publication work.

The independent-capacity experiment is different: it is **unfinished Iteration C work**, simply not
scheduled into the publication checkpoint.

### 5.5 Descope order

If publication work slips, remove optional polish before weakening navigation, evidence boundaries,
disclosure checks, or the explicit statement that Iteration C remains open.

## 6. Scheduling history

Only history that explains the current schedule is retained here. Detailed intermediate allocation
changes and implementation chronology belong to Git/PR history and implementation records.

### 6.1 Why database-authority scaling came first

PR2 found PostgreSQL to be the first mutation frontier. Database-authority composition therefore had
higher value than multiplying application replicas against an unchanged saturated writer.

### 6.2 What changed from v0.4

The original plan expected service scale-out followed by a larger AWS path. Evidence changed that
shape:

1. database-authority composition moved ahead of service replicas;
2. Iteration B established the authority boundary and selected capacity composition as the next
   Problem;
3. Iteration C derived that the fixed workstation cannot prove independently provisioned capacity;
4. local rehearsal became method qualification, then a full scheduler-partitioned characterisation;
5. the local result exposed shared-write-path variation and withheld unsupported efficiencies;
6. the independent probe was refined but provisioning blocked execution;
7. publication was then separated from engineering-loop closure so Iteration C can continue later
   without forcing a premature A&R verdict.

### 6.3 The original AWS path remains withdrawn; independent capacity remains Iteration C

The earlier EKS/RDS-style deployment plan remains withdrawn. The current independent-capacity test is
a much smaller evidence-driven experiment whose provider is an implementation choice.

Current publication path:

```text
qualified method
    -> scheduler-partitioned characterisation
    -> independent probe defined, provisioning blocked
    -> PR4 evidence checkpoint
    -> public-readiness pass
    -> public release
```

Technical continuation:

```text
qualifying independent environment
    -> resume Iteration C validation
    -> retain and interpret evidence
    -> Analyse & Review at a genuine decision point
```

No cloud-production conclusion follows from the blocked provisioning attempt.

### 6.4 PR2 deferrals that still matter

- overload/open-loop work is meaningful only after the applicable closed-loop capacity result is
  secure;
- instrumentation is selected by the evidence needed for a claim, not because a particular exporter
  appeared in an earlier schedule;
- unrelated PR2 reruns and measurement refinements remain deferred unless a later Problem needs them.

## 7. Public-release deliverables

The checkpoint should leave:

1. a runnable service and reproducible external load generator;
2. clear current architecture and workload contracts;
3. the PR2 single-instance frontier and PR3c correctness/failure-isolation reports;
4. the PR4 scheduler-partitioned checkpoint report with unresolved/withheld quantities explicit;
5. the independent-probe design and provisioning evidence with `VAL-SCALE-5` still pending;
6. clear links from claims to evidence and reproduction entry points;
7. concise, navigable public documentation with limitations and disclosure boundaries explicit;
8. an unmistakable statement that Iteration C remains open after publication.

## 8. Publication checkpoint gate

The August publication schedule is complete when:

- PR4 evidence is retained and correctly labelled;
- no independent-capacity result is implied where none exists;
- the documentation is navigable, internally consistent, and compressed to durable signal;
- repository content/history/PR metadata pass disclosure review;
- an external reader can tell what is established, what is unproven, and what work remains open.

Successful independent-capacity measurement is not a public-release gate; it remains an Iteration C
technical gate. Publication therefore does not alter the Iteration C acceptance criteria in
[`../../requirements/ag-sept.md`](../../requirements/ag-sept.md) or the validation meaning in
[`milestone-validation.md`](milestone-validation.md).

After public release, Iteration C remains open until its independent-capacity work reaches a genuine
engineering decision point.