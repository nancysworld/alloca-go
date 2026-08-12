# AG-Sept — milestone plan

**Status:** Living — the current AG-Sept schedule
**Delivery window:** August 2026
**Predecessor:** AG-M1 — correct transactional core and end-to-end service path
**Supersedes:** [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) (31 July 2026), retained as an
archived snapshot because PR1 and PR2 were planned and reported under it.

## What this document owns

**Schedule only:** priority, budget, sequence, work-unit/PR split, contingency, descope order,
deliverable status, and the scheduling history that explains how the order was reached.

Everything durable lives elsewhere, and this plan links rather than restates it:

| Question | Owner |
|---|---|
| What worthwhile outcome is AG-Sept pursuing, and what problem is open now? | [`../requirements/ag-sept.md`](../requirements/ag-sept.md) |
| What must be true of any acceptable resolution? | [`../requirements/system-requirements.md`](../requirements/system-requirements.md) (`REQ-*`) |
| What workload semantics stay stable while topology changes? | [`../test/workload-catalog.md`](../test/workload-catalog.md) |
| What is the durable system shape? | [`../design/horizontal-scaling.md`](../design/horizontal-scaling.md), [`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md), [`../design/deployment-architecture.md`](../design/deployment-architecture.md) |
| What transactional properties must hold? | [`../design/transaction-semantics.md`](../design/transaction-semantics.md) (`INV-*`) |
| How will the claims be proved or falsified? | [`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md) (`VAL-*`) |
| What makes a run's numbers admissible? | [`../design/measurement-contract.md`](../design/measurement-contract.md) |
| How is the system run and operated? | [`../operations/`](../operations/) |
| What did the experiments establish? | [`../measurements/`](../measurements/) |
| How was the work actually built? | [`../development/implementation/`](../development/implementation/) |

**This plan is not a normative reference for code, tests, requirements, or design.** It is
expected to change; a citation into it should be for a scheduling or historical fact and should
say so ([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

Sizes, matrices, durations, and replica counts proposed here are `[HYPOTHESIS]` under
`measurement-contract.md` §2 unless identified as a fixed planning budget or are fixed by the
owning validation/design document.

## 1. Where the milestone is

AG-Sept's governing goal, its iteration history, and the currently open problem are owned by
[`../requirements/ag-sept.md`](../requirements/ag-sept.md). In summary:

- **Iteration A — identify the first scaling frontier.** Resolved. PR2 established PostgreSQL as
  the limiting subsystem while the Go service retained substantial compute headroom.
- **Iteration B — compose independent writable database authorities.** Resolved by the Analyse &
  Review in PR #17. The Phase 1 authority model is established as a correctness/composition result,
  not a capacity multiplier.
- **Iteration C — independently provisioned shard-group capacity.** Requirements, Design, and
  Validation now fix the bounded experiment: reusable `WL-MUT-DISP-4`, organisations A/B/C/D,
  1/2/4 shard groups, one service + one PostgreSQL authority per group, equivalent AWS EC2
  capacity-unit hosts, and a separate generator host. The result is numeric `G1/G2/G4` and derived
  `E2/E4`; no efficiency threshold is a pass criterion.

The decision order has now reached Schedule:

```text
Iteration B evidence -> Analyse & Review -> Iteration C Problem
                                           |
                                           v
                         Requirements -> Design -> Validation plan -> Schedule
```

| Workstream | Work unit | Status |
|---|---|---|
| Measurement substrate and load harness | PR1 | merged `71914a4` |
| Single-instance frontier | PR2 | merged `0d40de4` |
| Placement, booking policy, confirm/cancel ownership | PR3a | merged `aa1e3a5` |
| Multi-authority harness — topology, routing, certification, verifier | PR3b | merged `57f501d` |
| Multi-authority correctness and failure-isolation evidence | PR3c | merged #16 |
| Iteration B Analyse & Review | review step, PR #17 | merged; Iteration B closed and Iteration C Problem selected |
| Iteration C planning | PR #18 | in progress; docs-only Requirements → Design → Validation → Schedule |
| Iteration C AWS capacity environment | PR4a | scheduled after #18 |
| Iteration C 1/2/4 capacity evidence | PR4b | scheduled after PR4a |
| Architecture conclusions and one justified boundary | PR5 | not started; remains downstream of Iteration C evidence/A&R |

Validation status is owned by the validation plan's own status table, not duplicated here.

## 2. Time budget

The development allocation is a planning constraint. **Half a day is the unit**, here and in the
implementation records: nothing is estimated well enough to distinguish 0.3 from 0.4, and finer
granularity is false precision that invites its own overrun (Nancy's call, 2026-08-05).

| Workstream | Work unit | Allocated | Spent | Left | Status |
|---|---|---:|---:|---:|---|
| Measurement harness and load generator | PR1 | 2.0 | 2.0 | 0.0 | merged |
| Single-instance frontier, with the diagnostic time-series minimum | PR2 | 2.5 | 2.5 | 0.0 | merged |
| Placement, booking policy, and confirm/cancel ownership | PR3a | 1.0 | 1.0 | 0.0 | merged; originally 3.0, with 2.0 returned to contingency |
| Multi-authority harness | PR3b | 2.5 | 2.5 | 0.0 | merged |
| Multi-authority correctness and failure-isolation evidence | PR3c | 1.5 | 1.5 | 0.0 | merged; originally 2.0, with 0.5 returned to contingency |
| Iteration C planning | PR #18 | 0.5 | 0.0 | 0.5 | in progress; hard planning cap |
| Iteration C AWS capacity environment | PR4a | 1.5 | 0.0 | 1.5 | scheduled |
| Iteration C 1/2/4 capacity evidence | PR4b | 2.5 | 0.0 | 2.5 | scheduled |
| Architecture conclusions and one justified boundary | PR5 | 1.5 | 0.0 | 1.5 | not started |
| **Allocated development budget** | | **15.5** | **9.5** | **6.0** | |
| Contingency | | **4.0** | **1.0** | **3.0** | 1.0 drawn by §2.1.1 |
| **Total milestone budget** | | **19.5** | **10.5** | **9.0** | |

**The numeric columns are the accounting source of truth:** `Allocated = Spent + Left` on every
row. Completed work that returned unused allocation is shown at its current allocation; **Status**
keeps the historical context, not the accounting.

The former 4.5-day uncommitted Iteration C envelope is now scheduled as **0.5 planning + 1.5
capacity environment + 2.5 evidence/report**. It has not grown because AWS returned to the plan;
the infrastructure choice must fit the envelope the Problem already had.

A further **2–3 days** are reserved beyond the development budget for rerunning decisive
experiments, validating negative controls, reviewing measurements and interpretations, correcting
documentation, polishing diagrams, checking public-disclosure suitability, and preparing the
repository for external readers. **Iteration B's Analyse & Review (#17) spent 0.5 day from this
reserve, leaving 1.5–2.5 days.**

The time budget is a constraint, not an estimate to be expanded whenever a tool introduces
incidental complexity.

### 2.1 How the contingency is spent

**Completed work returns unused allocation to contingency rather than to scope.** PR3a came in at
1.0 against 3.0 and returned 2.0; PR3c came in at 1.5 against 2.0 and returned 0.5 (§2.1.2). The
figures recorded are conservative under the half-day rule rather than attempts to account for
hours precisely.

The gain is **not** an invitation to widen Iteration C. Contingency is held for reruns, an
investigation that does not resolve on the first attempt, or another hard problem established by
evidence.

**Every PR is funded at what its scope costs.** No PR carries a deliberate shortfall, and no
Iteration C work unit depends on contingency to be reachable.

**Contingency is not scope.** It is drawn on before §5's descope order. After PR3c's 0.5-day return,
contingency is **4.0 total**: **1.0 is drawn** (§2.1.1) and **3.0 remains**.

Unspent contingency is not a licence to expand a PR. It returns to the reserve.

### 2.1.1 The documentation and methodology expansion draws on contingency

**Nancy's decision, 2026-08-10:** the document-ownership and engineering-process expansion carried
by PR #15 is charged to **AG-Sept contingency**.

**Drawn: 1.0 day** under the half-day accounting rule.

### 2.1.2 PR3c returns its unused half day

**Nancy's decision, 2026-08-11:** PR3c is charged at **1.5 days actual** against its former 2.0-day
allocation. The 0.5-day difference returned to contingency rather than being carried into Analyse
& Review or silently charged to the next work unit.

The milestone total stays **19.5 days**; the return changes allocation, not the total budget.

### 2.2 Review depth is the throughput control

**Review is not free. Nancy's decision, 2026-08-12:** Iteration B's Analyse & Review in PR #17 is
charged at **0.5 day** against the separate 2–3 day review/rerun/interpretation reserve. That leaves
**1.5–2.5 days** in that reserve.

Review depth is Nancy's to set. Less detailed review moves more responsibility onto implementation
verification; it does not make the correctness/evidence gates optional.

### 2.3 Iteration C planning is intentionally bounded, not a general precedent

**Nancy's decision, 2026-08-12:** PR #18 has a **0.5-day hard cap** and should merge within that
active-work budget.

That cap is justified by this iteration's state, not by a belief that Requirements/Design/
Validation should generally be rushed. PR2 already exposed the mutation frontier, Iteration B
already established the authority architecture, and PR #17 already selected a narrow Problem. The
remaining planning work is primarily to place known decisions in their correct semantic owners and
derive the minimum environment that makes the comparison legitimate.

A future Problem with genuine unresolved uncertainty should receive the investigation its
uncertainty requires. **Process depth follows uncertainty; this bounded decision-recording exercise
is specific to Iteration C.**

## 3. PR sequence

Each entry states what the work unit delivers and the gate that ends it. Technical meaning stays in
the linked requirements, design, workload catalogue, and validation plan.

### PR1 — Measurement substrate and load harness — merged

2.0 days. Metrics recorder, telemetry-overhead measurement, reset and seed tooling, external
generator, run manifest, persisted-state verifier, response-validation control, operator
documentation.

Record: [`ag-sept-pr1.md`](../development/implementation/ag-sept-pr1.md).

### PR2 — Single-instance frontier — merged

2.5 days. Prometheus retention path, diagnostic panels, one-instance sweeps for controlled
workloads, telemetry comparison, generator-bottleneck control, frontier report.

Record: [`ag-sept-pr2.md`](../development/implementation/ag-sept-pr2.md). Report:
[`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md).
Result: PostgreSQL, not `alloca-go`, sets the measured mutation frontier.

### PR3a — Placement, booking policy, and confirm/cancel ownership — merged

**Actual 1.0 day.** Delivers the versioned placement map, shard-affine service units, placement
enforcement, Phase 1 booking policy, confirm/cancel ownership, and supporting contracts/tests.

### PR3b — Multi-authority harness — merged

**Actual 2.5 days.** Delivers the two-authority topology, per-authority migration, placement-aware
generator, topology/provenance certification, and authority-aware verifier.

### PR3c — Multi-authority correctness and failure isolation — merged #16

**Actual 1.5 days.** Delivers retained Phase 1 correctness, refusal, routing, reconciliation and
failure-isolation evidence. It deliberately makes no capacity multiplier claim from the shared
workstation.

### Iteration B Analyse & Review — PR #17 — merged

**Actual 0.5 day from the separate review reserve.** Closes Iteration B, repairs the VAL-COR-4
same-key replay evidence gap found during review, and selects the Iteration C Problem.

### Iteration C planning — PR #18 — 0.5 day

**Docs-only.** Requirements → Design → Validation plan → Schedule for the Problem selected by #17.

It records:

- `WL-MUT-DISP-4` as the reusable four-organisation A/B/C/D mutation workload;
- numeric scale efficiency as an output rather than a threshold;
- the shard-group capacity-unit model and like-for-like resource-envelope requirement;
- the derived environment decision: minimal AWS EC2, not EKS/RDS/Kubernetes;
- the fixed 1/2/4 placement matrix;
- the concrete PR4a/PR4b schedule below;
- the public README update reflecting the broader project North Star and current proven state.

**Gate:** all planning owners agree without introducing implementation, deployment config, scripts,
measurements, or capacity results. Review fixes only material contradictions/gaps; settled design
choices are not reopened merely because more alternatives exist.

### PR4a — AWS capacity environment — 1.5 days

**Delivers:** the smallest reproducible AWS implementation of `deployment-architecture.md` §13:

- one equivalent EC2 capacity-unit host per shard group, each running one Alloca-Go service and one
  PostgreSQL authority;
- a separate EC2 generator host;
- versioned bootstrap/deployment configuration sufficient to instantiate 1-, 2-, and 4-group
  topologies with the fixed placement maps;
- per-authority migration and the existing provenance/certification contract;
- bounded host/service/database resource evidence required by VAL-NEG-7;
- smoke/correctness verification that all three topologies are runnable before capacity sweeps.

**Not in PR4a:** EKS, RDS, Kubernetes, autoscaling, service-replica multiplication, capacity
headline numbers, or a cloud-production architecture claim.

**Gate:** clean infrastructure can instantiate the 1/2/4 topology family with equivalent
capacity-unit hosts and separate generator, all units pass provenance/readiness/placement checks,
and the measurement system can retain the resource evidence required by VAL-SCALE-5/VAL-NEG-7.

### PR4b — 1/2/4 shard-group capacity evidence — 2.5 days

**Delivers:** the fixed `WL-MUT-DISP-4` matrix from validation-plan §4.6:

- `G1`: A/B/C/D on one shard group;
- `G2`: A/B and C/D on two shard groups;
- `G4`: A, B, C, D on four shard groups;
- one capacity sweep plus one retained confirmation of the selected point per topology;
- correctness/reconciliation and generator/resource controls at every quoted point;
- measured `G1/G2/G4`, derived `E2/E4`, limiting-resource analysis, workload/resource envelope,
  limitations, and retained report/artifacts.

There is **no efficiency pass threshold**. A sub-linear result is acceptable evidence if it is
admissible and explained or conservatively bounded.

**Gate:** VAL-SCALE-5 and VAL-NEG-7 are either discharged with retained evidence or explicitly
reported as unproven; no number is promoted beyond its measurement-contract evidence level.

Iteration C Analyse & Review follows PR4b using the separate review/rerun/interpretation reserve.
It decides whether the Problem is sufficiently resolved and whether the AG-Sept Goal is achieved or
another Problem is worth pursuing. PR5 does not bypass that gate.

### PR5 — Architecture conclusion and boundary decision

**Budget:** 1.5 development days, downstream of Iteration C A&R.

It assembles the evidence-backed architecture conclusion, comparison tables/diagrams, service-
boundary decision, limitations, reproduction entry points, and public-release polish. It must not
invent a new implementation stage if A&R concludes the milestone should end.

## 4. Manifest and reconciliation staging

The rules are `measurement-contract.md` §11–§13. PR3b established multi-authority topology/image
identity; PR4a extends environment/topology capture to the independent EC2 capacity-unit design;
PR4b records the exact 1/2/4 placement and resource envelope for each capacity run.

Reconciliation: PR1 established the single-authority self-check; PR3b extended it to multiple
authorities; PR3c exercised it against deliberate failure; PR4b applies it to every quoted capacity
point.

**Quotability:** the earlier AG-Sept plan could not satisfy the measurement contract's separate-
generator prerequisite for a published capacity claim. Iteration C now schedules separate EC2
generator compute, so that specific blocker is removed. A PR4b result reaches only the evidence
level for which **all** `measurement-contract.md` §13 requirements are actually satisfied; older
PR1/PR2/PR3 evidence is not retroactively promoted.

## 5. Priority and descope

### 5.1 P0 — required

For Iteration C the non-descopable capacity path is now explicit:

- `WL-MUT-DISP-4` workload semantics;
- the `G1/G2/G4` 1/2/4-shard-group matrix;
- one equivalent EC2 capacity-unit host per shard group;
- separate generator compute and generator-headroom proof (VAL-NEG-2);
- correctness/reconciliation for every quoted point (VAL-COR-1);
- resource-envelope evidence sufficient for VAL-NEG-7;
- measured `G1/G2/G4` and derived `E2/E4` with no preselected threshold;
- limiting-resource interpretation and explicit workload/resource envelope;
- retained evidence and report.

The older service-replica VAL-SCALE-1/2 path is not Iteration C scope.

### 5.2 P1 — strongly desirable

Only after the P0 Iteration C result is secure: a deliberately constrained resource control
(VAL-NEG-5) if it materially strengthens the limiting-resource diagnosis; polished charts beyond
the minimum needed to communicate the result; extra diagnostic reruns requested by A&R.

### 5.3 P2 — only after decisive evidence

Transactional outbox implementation; independently deployed consumer; autoscaling; additional
fault injection; distributed tracing; richer dashboards; service-replica scaling without evidence
that service compute has become the relevant frontier.

### 5.4 Out of scope for AG-Sept

Cross-authority booking and any distributed commit/saga protocol; splitting one organisation
across writable authorities; online rebalancing or dual-write migration; multi-region writes;
EKS; RDS; Kubernetes; a service mesh; autoscaling; a broker solely to claim event-driven
architecture; complete production authentication/authorization; eliminating hot-authority
serialization; and reproducing a full commercial workload.

**Narrow AWS exception:** EC2 is in scope only as the bounded Iteration C measurement mechanism in
`deployment-architecture.md` §13 — equivalent capacity-unit hosts plus separate generator compute.
This is not a reopening of the former EKS/RDS production-shaped AWS plan.

### 5.5 Descope order

If time slips, remove work in this order:

1. optional resource-limit control beyond the evidence already needed for VAL-NEG-7;
2. chart/dashboard polish beyond the diagnostic minimum;
3. extra confirmation/rerun work beyond the one required retained confirmation per topology,
   unless A&R needs it to resolve material variation;
4. PR5 chart production beyond what the decisive findings require.

**Do not descope:** the 1/2/4 matrix, separate generator, equivalent growing resource envelopes,
response validation, reconciliation, resource-envelope evidence, retained provenance, or the
limiting-resource interpretation. Those properties make the capacity result mean what it says.

Contingency is drawn before weakening any mandatory evidence gate.

## 6. Scheduling history

### 6.1 Why database-authority scaling came first

Nancy's call, 2026-08-05: scaling design and implementation take priority over capacity
measurement, and database-authority scaling comes before service-replica scaling because PR2 found
PostgreSQL to be the first mutation frontier.

### 6.2 What changed from v0.4

v0.4 planned one service baseline, stateless replicas against one PostgreSQL authority, then a
conditional AWS deployment. PR2's measured result changed the order:

1. horizontal database authority became primary work ahead of replicas;
2. the original AWS path was withdrawn and its budget moved to the authority work;
3. Iteration B established the authority architecture and PR #17 selected capacity composition as
   the next Problem;
4. Iteration C Requirements/Design then derived that the current fixed workstation cannot prove
   the selected capacity claim, which justified a **new, much smaller AWS EC2 path**;
5. the 4.5-day Iteration C envelope did not increase when that mechanism was selected.

### 6.3 The original AWS path remains withdrawn; Iteration C adds only minimal EC2

The v0.4 AWS plan — ECR/EKS/RDS/load-balancing path plus separate EC2 generator and its larger
matrix — remains withdrawn. Iteration C does **not** restore it.

The new derivation is narrower:

```text
REQ-SCALE-4 independent growing envelopes
        -> fixed workstation cannot supply them
        -> external independent compute is required
        -> AWS EC2 is the available bounded mechanism
        -> one host per shard group + one separate generator host
```

No EKS, RDS, managed-database, service-mesh, or cloud-production conclusion follows from this
choice. The capacity baseline and scale-out points are all re-measured in the same EC2 environment;
workstation `G1` is never mixed with AWS `G2/G4`.

### 6.4 The PR2 deferral register

- **Overload question:** remains outside AG-Sept; it deserves its own future Problem rather than
  being absorbed into Iteration C.
- **Instrumentation:** the old PostgreSQL/node-exporter choices are no longer schedule owners. The
  durable obligation is VAL-NEG-7: retain enough host/service/database evidence to explain,
  exclude, or conservatively bound material environment variation. PR4a selects the smallest
  mechanism that satisfies it.
- **Evidence hygiene:** per-run retained evidence required by PR4b is in scope; unrelated PR2
  re-runs/histogram work remains deferred.

## 7. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator with placement-aware routing;
3. a stable reusable workload catalogue;
4. aggregated service/runtime/resource evidence sufficient for the claims made;
5. the single-instance frontier report;
6. the multi-authority correctness/failure-isolation report;
7. the Iteration C 1/2/4 shard-group capacity/scale-efficiency report;
8. current and intended architecture diagrams;
9. authority and bottleneck analysis across the scaling axes actually measured, with unmeasured
   axes named explicitly;
10. a service-boundary decision and recorded Phase-2/deferred-work conclusion;
11. a concise public repository summary;
12. explicit limitations, evidence labels, and negative-control results.

## 8. Completion gate

AG-Sept's schedule is complete when the deliverables of §7 exist and every P0 item is validated or
explicitly recorded as unproven.

Before Iteration C A&R can judge the Goal:

- `WL-MUT-DISP-4` runs reproducibly from clean fixtures;
- the AWS 1/2/4 topologies are reproducible with equivalent capacity-unit hosts and separate
  generator compute;
- `G1/G2/G4` and `E2/E4` exist only at the evidence level their retained runs support;
- response validation, generator headroom, and resource-envelope controls are demonstrated;
- outcomes and persisted state reconcile on every participating authority;
- limiting resources and material environment variation are explained or conservatively bounded;
- measured facts, calculations, interpretation, and limitations are separated;
- architecture reflects evidence rather than desired presentation;
- the repository remains suitable for public review under the disclosure policy.

A finished schedule is an input to Analyse & Review, not a substitute for it.
