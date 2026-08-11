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
`measurement-contract.md` §2 unless identified as a fixed planning budget.

## 1. Where the milestone is

AG-Sept's governing goal, its iteration history, and the currently open problem are owned by
[`../requirements/ag-sept.md`](../requirements/ag-sept.md). In summary:

- **Iteration A — identify the first scaling frontier.** Resolved. PR2 established PostgreSQL as
  the limiting subsystem while the Go service retained substantial compute headroom. The goal was
  *not* thereby achieved; the evidence created the next problem.
- **Iteration B — compose independent writable database authorities.** Resolved by the Analyse &
  Review in PR #17, on the evidence produced by merged PR3a, PR3b, and PR3c (#16). The Phase 1
  authority model is established as a correctness/composition result, not a capacity multiplier.
- **Iteration C — independently provisioned shard-group capacity.** The A&R selects the **Problem
  only**: whether independent shard groups can turn the single-authority PostgreSQL frontier into
  approximately additive aggregate mutation capacity, and what workload/placement envelope each
  group should own. Requirements, design, validation, and schedule are intentionally not committed
  by this A&R.

The decision order is now:

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
| Iteration B Analyse & Review | review step, PR #17 | current; closes Iteration B and selects Iteration C Problem |
| Iteration C reserved scaling envelope | not yet scheduled | 4.5 days reserved; scope waits for Requirements → Design → Validation plan → Schedule |
| Architecture conclusions and one justified boundary | PR5 | not started |

Validation status — what is established, what is pending, and what is explicitly *not*
discharged — is owned by the validation plan's own status table, not duplicated here.

## 2. Time budget

The development allocation is a planning constraint. **Half a day is the unit**, here and in the
implementation records: nothing is estimated well enough to distinguish 0.3 from 0.4, and finer
granularity is false precision that invites its own overrun (Nancy's call, 2026-08-05).

| Workstream | Work unit | Allocated | Status |
|---|---|---:|---|
| Measurement harness and load generator | PR1 | 2.0 | spent 2.0 |
| Single-instance frontier, with the diagnostic time-series minimum | PR2 | 2.5 | spent 2.5 |
| Placement, booking policy, and confirm/cancel ownership | PR3a | 3.0 | **done in 1.0; 2.0 returned to contingency** |
| Multi-authority harness | PR3b | 2.5 | spent 2.5 |
| Multi-authority correctness and failure-isolation evidence | PR3c | 2.0 | **done in 1.5; 0.5 returned to contingency** |
| Iteration C reserved scaling envelope | not yet scheduled | 4.5 | **reserved budget, uncommitted scope** |
| Architecture conclusions and one justified boundary | PR5 | 1.5 | remaining |
| **Allocated development budget** | | **15.5** | 9.5 spent, 6.0 remaining |
| Contingency | | 4.0 | **1.0 drawn (§2.1.1), 3.0 remaining** |
| **Total milestone budget** | | **19.5** | **10.5 spent, 9.0 remaining** |

**Allocated is not committed.** The 15.5 row is the development budget this milestone has
apportioned after completed work returns unused allocation to contingency, not a statement that all
of it is committed technical scope. The 4.5-day envelope is now reserved for Iteration C, but
**what work it buys is not scheduled by the A&R**: Requirements, Design, and Validation plan must
first determine the necessary shape (§3).

A further **2–3 days** are reserved beyond the development budget for rerunning decisive
experiments, validating negative controls, reviewing measurements and interpretations, correcting
documentation, polishing diagrams, checking public-disclosure suitability, and preparing the
repository for external readers.

The time budget is a constraint, not an estimate to be expanded whenever a tool introduces
incidental complexity.

### 2.1 How the contingency is spent

**Completed work returns unused allocation to contingency rather than to scope.** PR3a came in at
1.0 against 3.0 and returned 2.0; PR3c came in at 1.5 against 2.0 and returns 0.5 (§2.1.2). The
figures recorded are conservative under the half-day rule rather than attempts to account for
hours precisely.

The gain is **not** an invitation to widen the Iteration C envelope. It is held for where
this milestone is most
likely to need it — reruns, an investigation that does not resolve on the first attempt, a hard
problem that deserves more argument than a day allows. PR2's unexplained ~2× excursion is the
standing example of work that consumed far more than its share, and nothing about a later work
unit going well makes that less likely to recur.

**Every PR is funded at what its scope costs.** No PR carries a deliberate shortfall, and nothing
in §3 depends on the reserve to be reachable. That is the difference the increased budget bought,
and it is worth naming: v0.4 planned AWS experiments outside its own table, which is how a plan
overruns on day one.

**Contingency is not scope.** It is drawn on before §5's descope order. After PR3c's 0.5-day return,
contingency is **4.0 total**: **1.0 is drawn** (§2.1.1) and **3.0 remains**. Two things could
plausibly claim the rest, in this order:

1. **PR2's unexplained ~2× excursions turning out to be reproducible and diagnosable** once a
   node exporter can see them. That would be a real finding, and chasing it is worth more than
   another matrix cell.
2. **The overload question deferred from PR2** — roughly 1.5 days. It is the founding
   unreproduced question in [`../design/high-level-design.md`](../design/high-level-design.md)
   §1.1, and it is the one candidate here that is *new scope* rather than insurance. Adding it is
   Nancy's call, not a default — and the returned budget makes it affordable, which is a reason to
   decide it deliberately rather than to let it drift in.

Unspent contingency is not a licence to expand a PR. It returns to the reserve.

### 2.1.1 The documentation and methodology expansion draws on contingency

**Nancy's decision, 2026-08-10:** the document-ownership and engineering-process expansion carried
by PR #15 — the goal/problem/requirements split, the validation-plan and exploration-roadmap
ownership model, the iteration-loop definition, and the citation migration that followed — is
charged to **AG-Sept contingency**.

**It does not consume PR3c's allocation.** PR3c is funded for the multi-authority correctness and
failure-isolation evidence and nothing else; methodology work that happened to land before it must
not arrive as a silent shortfall in the evidence PR.

**Drawn: 1.0 day** (Nancy's figure at merge, rounded under the half-day rule of §2). This remains
the only contingency draw so far; §2.1.2 records PR3c's 0.5-day return separately.

### 2.1.2 PR3c returns its unused half day

**Nancy's decision, 2026-08-11:** PR3c is charged at **1.5 days actual** against its 2.0-day
allocation. The 0.5-day difference returns to contingency rather than being carried into Analyse &
Review or silently charged to the next work unit.

The charge includes the final review and docs-only cleanup on PR #16. Recording it before merge
was deliberate: PR3c owns the work that produced and closed its evidence, while the Analyse &
Review that follows is funded from the separate 2–3 day review/rerun/interpretation reserve.

The accounting therefore moves contingency from 3.5 to **4.0 total**, with **1.0 drawn and 3.0
remaining**. The milestone total stays **19.5 days**; **10.5 are spent and 9.0 remain**.

### 2.2 Review depth is the throughput control

**It is Nancy's to set** (2026-08-05). The implementation side of this milestone is not the
constraint; the review step is, and it can be traded for speed when momentum matters more than
scrutiny.

The consequence is stated rather than left implicit: less detailed review moves the burden of
catching errors onto the implementation side's own verification — the mutation tests, the live SQL
checks, the re-measurement of quoted figures. Where that verification is weak, a lighter review
does not find it. PR3a's own record is the argument: three of nine review findings were comments
asserting the opposite of the truth, and a pre-merge audit found six more stale claims. Those were
caught by review. Going faster means catching more of them before review.

## 3. PR sequence

Each entry states what the work unit delivers and the gate that ends it. The **technical meaning**
of every term below is owned by the linked requirement, design document, or validation; this
section schedules the work and does not redefine it.

PR1 and PR2 were planned under v0.4 and their scopes are unchanged.

### PR1 — Measurement substrate and load harness — merged

2.0 days. Metrics recorder, telemetry-overhead measurement, reset and seed tooling, external
generator, run manifest, persisted-state verifier, response-validation control, operator
documentation.

Record: [`ag-sept-pr1.md`](../development/implementation/ag-sept-pr1.md).

### PR2 — Single-instance frontier — merged

2.5 days. Prometheus retention path, diagnostic panels, one-instance sweeps for all three
controlled workloads, telemetry comparison, generator-bottleneck control, frontier report.

Record: [`ag-sept-pr2.md`](../development/implementation/ag-sept-pr2.md). Report:
[`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md).
Result: the frontier is set by PostgreSQL, not by `alloca-go` — which is what reordered this
milestone (§6).

Two obligations survive into later PRs:

- the **recommended operating capacity** term, deferred because two of its three components need
  more than one replica to be meaningful, so it is owed by whatever scaling work the next
  iteration selects;
- the **±2× caveat** on every figure from this machine. A node exporter is required rather
  than desirable, but installing it does not discharge the caveat: the caveat stands until
  instrumented reruns *explain* the excursions, *exclude* them, or *bound* them conservatively. An
  instrument that can see a thing is not yet an answer about it.

### PR3a — Placement, booking policy, and confirm/cancel ownership — merged

**Budget:** 3.0 days; **actual 1.0**. It rose from 2.5 to 3.0 before implementation started, and
that is the contingency working as intended: the design revision of 2026-08-05 settled three
contract questions, one of which resolved towards *build it* — confirm and cancel gained an
explicit `UserRef` ownership check, priced by the earlier estimate as a decision rather than a
domain-contract correction with its own normative updates and tests. The half day came from
contingency rather than from another PR.

**Delivers:** the versioned placement map and its startup gate; shard-affine service units;
server-side placement enforcement; the Phase 1 booking policy on resolved authorities;
`cross_authority_unsupported` through every closed set it touches; the `UserRef` ownership check
on confirm and cancel; `/meta` extended with authority, routing, and schema identity; placement
invariants in the register with discriminating tests.

**Owners:** REQ-ROUTE-1; `horizontal-database-authority.md`; `deployment-architecture.md` §3–§4;
`api-surface.md` §2.6; VAL-COR-3, VAL-COR-5.

**Gate:** a service unit cannot be started against an inconsistent placement map or an
incompatible schema; a misrouted request is refused rather than written; the booking policy is one
named outcome across every closed set that must know about it; a wrong `UserRef` gets the same
answer whether or not the organisations are colocated; and no accepted AG-M1 invariant has been
weakened to make any of it fit.

### PR3b — Multi-authority harness — merged

**Budget:** 2.5 days; spent 2.5.

**Delivers:** the containerised two-authority topology with per-authority migration;
placement-aware generator routing; the multi-organisation, one-hot-organisation, and
cross-authority-refusal workloads; the ambiguous-request register that retains
`unknown_replayable` keys for PR3c; placement and topology fields in the manifest with
multi-service certification; the authority-aware verifier and its aggregated verdict.

**Owners:** `deployment-architecture.md`; `measurement-contract.md` §11–§12; validation plan §3.4,
§3.5, §3.6, §4.2.

**Gate:** a multi-authority run is reproducible from a version-controlled topology, produces an
aggregated correctness verdict naming every authority it read, and is refused certification when
the units disagree.

### PR3c — Multi-authority correctness and failure isolation — merged #16

**Budget:** 2.0 days; **actual 1.5, with 0.5 returned to contingency** (§2.1.2).

**Delivers:** the Phase 1 correctness experiments and per-authority verdicts; the cross-authority
refusal control as its own bounded evidence class; the failure-isolation experiment — one
authority down, then restored, with any ambiguous mutations the fault actually produced resolved
under their original idempotency keys before final verification; and the report, including the
organisation-to-authority distribution each run measured.

**Owners:** REQ-COR-1, REQ-COR-2, REQ-FAIL-1, REQ-ROUTE-1; VAL-COR-1..6, VAL-FAIL-1, VAL-SCALE-3;
validation plan §4.5; `measurement-contract.md` §12–§13.

**PR3c was not a service-replica experiment (Nancy's call, 2026-08-10).** Its scope stayed inside
the allocation and did not add replicas or exporters, force the service to become the bottleneck,
or draw on the Iteration C envelope.

It retained the service and database resource evidence its runs already produced — CPU, pool
acquisition and saturation alongside the correctness verdicts — as input to the Analyse & Review.
The A&R uses that evidence without promoting the correctness topology into a capacity claim.

**Opportunistic debt retained:** INV-21's acknowledgement-lost case still needs a targeted fault
that lets `COMMIT` land while its acknowledgement is lost. The generic authority shutdown did not
claim that proof, and this does not block the scaling Problem selected by the A&R.

**Gate:** discharged. Phase 1 correctness and failure isolation are demonstrated on independent
writable authorities; every accepted transaction semantic on the supported path is unchanged; and
no result claims a throughput multiplier from the shared workstation.

### Iteration B Analyse & Review — PR #17

**PR3a + PR3b + PR3c together are Iteration B, and PR #17 closes it** (Nancy's call,
2026-08-10). The review reads PR3c's retained evidence against the Problem, requirements, and
governing Goal, then records the five-part closure in
[`../requirements/ag-sept.md`](../requirements/ag-sept.md) (`engineering-process.md` §1.4.1).

**Its time comes from the existing 2–3 day review/rerun/interpretation reserve** described in §2,
not from a new development allocation and not from PR3c's allocation. Nothing in the budget table
changes for it.

The A&R verdict is that Iteration B is sufficiently resolved and the Goal is not yet sufficiently
achieved. It therefore selects Iteration C's Problem: independently provisioned shard-group
capacity. **It does not schedule Iteration C.**

### Iteration C scaling envelope — 4.5 days reserved, not yet scheduled

The 4.5-day envelope remains a budget reservation, not committed technical scope. A&R has selected
the next Problem, but the next iteration still starts at **Requirements**, then Design and
Validation plan, before this plan can assign a work-unit/PR split or spend the envelope.

There is therefore **no PR4 schedule in this PR**. In particular, the old candidate of multiplying
stateless replicas against one shared PostgreSQL writer is not the selected Iteration C Problem.
Service replicas, exporters, independently provisioned database resources, cloud infrastructure,
or another mechanism may become part of the design/validation only if the next iteration justifies
them.

The previously sketched replica-scaling work remains useful as historical scheduling input and as
unproven validation possibilities, but it is **superseded as the current candidate schedule**.
`ag-sept-validation-plan.md` keeps the meanings of VAL-SCALE-1/2 without promoting them into
Iteration C automatically.

### PR5 — Architecture conclusion and boundary decision

**Budget:** 1.5 development days, followed by the reserved 2–3 days. The split is deliberate: the
analysis, the tables, and the boundary decision are development work; polishing prose, diagrams,
and the repository entry point is what the reserve exists for, and moving it into the base
allocation is how the reserve quietly becomes contingency.

**Delivers:** the final single-instance, multi-authority, and scale-out comparison tables, and only
the charts a decisive finding needs; updated architecture diagrams connecting each frontier to the
slot-row, identity-row, claim-relation, and organisation-home authority model
(`horizontal-scaling.md` §6); the service-boundary decision, including why the reservation
transaction remains together and whether an asynchronous event or reporting boundary is justified;
what Phase 2 would cost and why it was deferred, so the phasing is a recorded decision rather than
an omission; and the updated repository entry point, reproduction instructions, limitations,
evidence labels, and public-disclosure checks.

The boundary decision starts from ADR-0001: do not split the reservation transaction merely to
claim microservices. The most defensible optional extraction is an asynchronous event or reporting
consumer fed through a transactional outbox; a complete design is sufficient when measurements
consume the budget. The expiry worker is not an automatic candidate — it shares the same domain and
transaction rules, and duplicate workers are already correctness-safe.

**Gate:** a reviewer can reproduce the topology and decisive runs, distinguish measured facts from
calculations and interpretation, understand why replicas help or do not help for each authority
distribution and why authority composition is a different axis, and follow the evidence to the
architecture decision.

**Not in PR5:** feature expansion for presentation value, speculative decomposition, dashboard work
unrelated to a finding, or reopening settled measurement definitions.

## 4. Manifest and reconciliation staging

The **rules** are `measurement-contract.md` §11 (run manifest), §12 (reconciliation), and §13
(quotability levels). What is scheduled — and so belongs here — is *when each obligation becomes
dischargeable*.

The generator is an HTTP client and cannot discover the service's shape, so manifest fields arrive
in the PR that first has something to say: service shape in PR2 (discharged — PR2 took the
operator-supplied count to zero by reading `/meta`), placement and authority identity in PR3b,
topology and image identity in PR3b; replica count, aggregate pool capacity and environment with
whatever scaling work the next iteration selects.

Reconciliation: PR1 established the single-authority self-check; PR3b extended it to multiple
authorities; PR3c exercised the multi-authority contract against deliberate failure rather than
only a healthy topology.

**Quotability:** AG-Sept does not fund separate generator compute (§6.3), so **no AG-Sept run can
reach `publishable`**. That is the only level co-residency blocks. A later run **may reach
`capacity`** if it carries the topology and environment provenance that level requires
(`measurement-contract.md` §13.2). Historical runs keep the level their own artifacts reached —
PR1's and PR2's are `local` because the operator-supplied deployment fields were not yet
populated, and no later harness capability retroactively promotes them.

The staging schedules the *work*, never the rule.

## 5. Priority and descope

### 5.1 P0 — required

**Unconditional.** These do not depend on what the Iteration C envelope ultimately funds:

reproducible external load harness; aggregated metrics; minimal reproducible time-series retention
and diagnostic dashboard; response-validation control (VAL-NEG-1); correctness reconciliation
extended to every participating authority (VAL-COR-1); the one-instance baseline; dispersed,
hot-slot, and hot-identity controls; Phase 1 placement, server-side enforcement, and the misrouting
control (VAL-COR-5); multi-authority correctness and failure-isolation evidence (VAL-FAIL-1); the
separate-generator rule honoured by labelling (`measurement-contract.md` §13.1); and the
evidence-led architecture conclusion.

**Iteration C has not yet reached Schedule.** The previous replica-specific conditional P0 items —
VAL-SCALE-1, VAL-SCALE-2, PostgreSQL exporter, and node exporter — remain unproven planning inputs,
not automatically committed Iteration C scope. The next Requirements/Design/Validation pass must
decide which controls are necessary for the selected shard-group-capacity Problem before this
priority table is updated again.

### 5.2 P1 — strongly desirable

The entries below predate Iteration B's A&R and are **not automatically Iteration C scope**:
PR2's falsifiable prediction tested as a first-class result; recommended operating capacity; the
composed two-authority run (VAL-SCALE-4); the resource-limit control (VAL-NEG-5); the simplified
synchronized release wave (validation plan §3.7); polished architecture and result diagrams.
They are reclassified when Iteration C reaches Schedule.

### 5.3 P2 — only after decisive evidence

Transactional outbox implementation; an independently deployed consumer; autoscaling; fault
injection beyond the authority-unavailability experiment; distributed tracing; richer dashboards.

### 5.4 Out of scope for AG-Sept

Cross-authority booking and any distributed commit or saga protocol
(`horizontal-database-authority.md` §4.2); splitting one organisation across writable authorities;
online rebalancing or dual-write migration; a shared global workflow database; decomposing the
reservation transaction; multi-region writes; AWS, EKS, RDS, and any cloud measurement (§6);
Kubernetes; a broker solely to claim event-driven architecture; a service mesh; complete production
authentication and authorization; eliminating the hot-authority serialization frontier; and
reproducing a full commercial workload.

Container orchestration is whatever is smallest and reproducible — Compose is sufficient
(`deployment-architecture.md` §11).

### 5.5 Descope order

**This order is the pre-Iteration-C schedule and will be re-derived when Iteration C reaches
Schedule.** Until then it is retained as the current reserve/descope history, not as a commitment
that every listed item belongs to the new Problem.

If time slips, remove work in this order:

1. the simplified synchronized release wave;
2. the resource-limit control (VAL-NEG-5);
3. dashboard polish beyond the diagnostic minimum;
4. the composed two-authority run — the two axes are then reported separately;
5. the hot-identity and hot-slot rows of the replica matrix at the highest replica count;
6. optional histogram-bucket work deferred from PR2;
7. PR5's chart production beyond what a decisive finding requires.

**Do not descope:** the response-validation control, the misrouting control, Phase 1 placement
enforcement, multi-authority correctness reconciliation, the
failure-isolation experiment, the one-instance control, minimal
diagnostic time-series visibility, measurement validity, or the architecture report.

**The correctness work is not a source of budget.** If later work overruns, the difference comes
first from §2's unallocated contingency and then from the Iteration C envelope through the schedule
that has actually been accepted — never from the gates that make a run admissible. The 2–3 day
reserve stays last and only for what it is for.

## 6. Scheduling history

### 6.1 Why the database work is funded first

Nancy's call, 2026-08-05: scaling design and implementation take priority over capacity
measurement, and database-authority scaling comes before service-replica scaling.

This is the direct consequence of PR2's finding that the database, not the application, sets the
frontier — measuring replicas harder against an unchanged writer would refine a number the
milestone has already explained. The reasoning is recorded durably as Iteration A's Analyse &
Review outcome in [`../requirements/ag-sept.md`](../requirements/ag-sept.md); this entry records
only that it changed the schedule.

### 6.2 What changed from v0.4

v0.4 planned one service baseline, then stateless replicas against one PostgreSQL authority, then
a conditional AWS deployment. PR2's measured result changed the order:

1. **Horizontal database authority became the milestone's primary work**, ahead of stateless
   replica scaling. Only Phase 1 is in scope.
2. **The AWS path was dropped** (§6.3).
3. **The local scale-out experiment survived intact** but moved behind the database work,
   carrying v0.4 PR3's obligations forward in full rather than dropping them. It was held as
   the post-Iteration-B envelope rather than a committed PR (§3). Nancy's call,
   2026-08-05: the milestone should leave the system scalable on both axes, even if only on local
   containers. **Iteration B's later A&R superseded stateless-replica scaling as the current
   candidate Problem; the 4.5-day budget remains reserved for Iteration C instead.**
4. **The budget rose from 10 development days to 19.5.** Nancy's approval, 2026-08-05.

### 6.3 The AWS path is withdrawn, not deferred in place

v0.4 planned an AWS deployment — ECR, EKS, RDS, an AWS load-balancing path, and **separate EC2
generator compute** — with decision gates, a smoke gate, and an experiment matrix. All of it is
withdrawn from AG-Sept under v0.4's own reallocation rule, and its budget funds the database
authority work (§2).

Two consequences, both recorded rather than absorbed:

1. **The separate generator compute never arrives.** It is the one thing a published capacity claim
   requires (`measurement-contract.md` §13.1). AG-Sept therefore closes with a bounded local
   frontier and **no published capacity number**. The v0.4 descope order already accepted this as
   valid, and it is more honest than promoting a co-resident measurement.
2. **No cloud, managed-database, or network boundary is measured.** Any statement about how
   Alloca-Go behaves on managed infrastructure remains unevidenced and must not appear in the
   architecture conclusion.

The AWS topology, gates, and matrix remain readable in
[`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) §10 and §11 for whichever milestone picks them up.
This is a deferral of the work, not a decision that it lacks value.

### 6.4 The PR2 deferral register

PR2 deferred a register of work, unassigned pending a re-planning session. It is resolved as
follows; the full reasoning stays in [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) §14.

- **Group A — the overload question** (open-loop arrival mode, retry-on-timeout control,
  `slots_for()` over-provisioning, the timeout-budget negative control, and the unenforced
  `admission_cap` manifest field). **Remains unassigned and does not fit AG-Sept.** It is
  load-harness work of PR1's family, roughly 1.5 days, and `high-level-design.md` §1.1 makes the
  unreproduced overload question a founding one — which is why it should be scoped deliberately in
  its own milestone rather than absorbed here. **The `admission_cap` field is the exception and
  should be fixed opportunistically:** a manifest field that is published, range-validated, and
  enforced nowhere is worse than an absent one, and either enforcing it or removing it is minutes
  of work in any PR that touches the manifest.
- **Group B — instrumentation** (PostgreSQL exporter, node exporter). **Previously promoted to
  required for the old replica-scaling envelope; Iteration C must reconsider that requirement
  against its own validation design before Schedule.**
- **Group C — evidence hygiene** (per-sweep TSDB snapshots, re-running PR2 cells under the fixed
  exporter, optional histogram buckets). **Previously assigned to the old replica-scaling
  envelope; retained as candidate hygiene work, not automatically Iteration C scope.**

## 7. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator with placement-aware routing;
3. aggregated service and runtime metrics;
4. a minimal reproducible time-series retention path and diagnostic dashboard;
5. a single-instance report;
6. a multi-authority correctness and failure-isolation report;
7. current and intended architecture diagrams;
8. authority and bottleneck analysis across the scaling axes AG-Sept actually measured, with the
   axes it did not measure named as such;
9. a service-boundary decision record, and a recorded decision on Phase 2;
10. a concise public repository summary;
11. explicit limitations, evidence labels, and negative-control results.

Iteration C may add a capacity/scale-efficiency report and further database/host instrumentation,
but those are **not committed by the A&R**. Its Requirements, Design, Validation plan, and
Schedule decide which additional deliverables are necessary. Item 8 names unmeasured axes rather
than letting a reader assume every possible scaling mechanism was exercised.

Together these should answer what is correct, what is fast under which workload, where authority
composition helps, where shared authority remains the frontier, which dependency becomes limiting,
and what should be separated next — and, where a scaling axis was not measured, that it was not.

## 8. Completion gate

AG-Sept's schedule is complete when the deliverables of §7 exist and every P0 item of §5.1 is
either validated or explicitly recorded as unproven.

**That is not the same as the goal being achieved.** Whether AG-Sept's governing goal is
sufficiently achieved is an Analyse & Review decision against
[`../requirements/ag-sept.md`](../requirements/ag-sept.md), taken on the evidence — and it may
conclude that another iteration is worth scheduling, or that a remaining problem is deferred to a
later goal. A finished schedule is an input to that decision, not a substitute for it.

Concretely, before that review can be held:

- required workloads run reproducibly from clean fixtures;
- single-instance and multi-authority results are available, plus the results of whatever the
  Iteration C schedule selects, or a record that it selected nothing;
- the response-validation, misrouting and generator-headroom controls have been demonstrated, and
  any additional controls required by Iteration C's validation design have been demonstrated;
- outcomes and persisted state reconcile on every participating authority;
- authority boundaries are visible in retained measurements, and database-connection boundaries
  too where the selected validation requires them;
- one authority's failure is shown to bound its own organisations and no others;
- measured facts, calculations, and interpretation are separated;
- the architecture reflects evidence rather than desired presentation;
- containers and authority composition remain means rather than goals;
- the repository is suitable for public review under the disclosure policy.