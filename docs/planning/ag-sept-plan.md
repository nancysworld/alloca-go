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
- **Iteration C — shard-group capacity, with independent provisioning as the strongest evidence
  layer.** PR4a qualified the method; PR4b ran the complete sustained G1/G2/G4 experiment on the
  scheduler-partitioned workstation; PR4c refined and attempted to provision the smallest useful
  independent AWS probe, then closed `STOP / DEFER` before any measured cell because the account's
  applied Standard On-Demand quota was **1 vCPU** at execution time. The minimum bounded topology
  required **5 vCPUs (2 + 2 + 1)** and EC2 refused even one selected two-vCPU `c5.large`.
  **PR4c produced no AWS performance evidence, does not discharge `VAL-SCALE-5`, and derives no
  `E2_aws`/`E4_aws`.** Any AWS capacity experiment is now unscheduled post-AG-Sept work.

  **PR4b executed on 2026-08-19 and did not derive `E2_local`/`E4_local`.** `G4_local` resolved;
  `G1` and `G2` did not, and `G1_local` is the denominator of both efficiencies. The evidence
  localises the limit to the shared write path rather than to `alloca-go` — without proving the
  mechanism, since the killing test was not run — so the local tier could not produce the
  efficiency figures originally intended. `VAL-SCALE-6` is executed but not discharged. That
  empirically justifies the independent-resource requirement while also warning that independent
  provisioning need not eliminate per-unit variance.

The next scheduled work is therefore PR5: combine Iteration C Analyse & Review with the AG-Sept
architecture/milestone conclusion, carrying the AWS result as **explicitly unproven due to external
provisioning**, not as unfinished in-milestone execution.

| Workstream | Work unit | Status |
|---|---|---|
| Measurement substrate and load harness | PR1 | merged `71914a4` |
| Single-instance frontier | PR2 | merged `0d40de4` |
| Placement, booking policy, confirm/cancel ownership | PR3a | merged `aa1e3a5` |
| Multi-authority harness — topology, routing, certification, verifier | PR3b | merged `57f501d` |
| Multi-authority correctness and failure-isolation evidence | PR3c | merged #16 |
| Iteration B Analyse & Review | review step, PR #17 | merged; Iteration B closed and Iteration C Problem selected |
| Iteration C planning | PR #18 | complete in #18; closes Requirements → Design → Validation → Schedule |
| Iteration C experiment preparation and measurement qualification | PR4a | merged #19; no canonical scaling result |
| Iteration C local sustained capacity and scale characterisation | PR4b | merged #20; gate met on the "explicitly unresolved" branch. `G4_local` resolved, `G1`/`G2` unresolved, efficiencies withheld, `VAL-SCALE-6` **not discharged** |
| Iteration C independently provisioned AWS probe | PR4c | **closed `STOP / DEFER` 2026-08-20; no measured AWS cell. Applied quota was 1 vCPU, below the 5-vCPU minimum topology** |
| Iteration C A&R and AG-Sept conclusion | PR5 | not started; follows PR4b evidence plus PR4c's explicit external blocker |

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
| Iteration C planning | PR #18 | 0.5 | 0.5 | 0.0 | complete; hard planning cap met |
| Iteration C method preparation and measurement qualification | PR4a | 4.0 | 4.0 | 0.0 | complete; consumed the whole former PR4a/PR4b envelope (§2.1.3) |
| Iteration C local sustained capacity characterisation | PR4b | 1.0 | 1.0 | 0.0 | complete; funded by the 1.0-day contingency draw (§2.1.3). Gate met on the "explicitly unresolved" branch; no further draw |
| Iteration C independent AWS probe | PR4c | **1.0** | **1.0** | **0.0** | closed `STOP / DEFER`; design/provisioning/closeout completed, no measured AWS cell |
| Iteration C A&R and AG-Sept conclusion | PR5 | 1.5 | 0.0 | 1.5 | not started |
| **Allocated development budget** | | **17.5** | **16.0** | **1.5** | |
| Contingency | | **2.0** | **1.0** | **1.0** | 1.0 drawn by §2.1.1; 1.0 reallocated to PR4b by §2.1.3; 1.0 reallocated to PR4c by §2.1.4 |
| **Total milestone budget** | | **19.5** | **17.0** | **2.5** | |

**The numeric columns are the accounting source of truth:** `Allocated = Spent + Left` on every
row. Completed work that returned unused allocation is shown at its current allocation; **Status**
keeps the historical context, not the accounting.

PR4c closes at its allocated **1.0 day**. The day covered the bounded probe design, review-driven
method correction, AWS bootstrap/configuration attempt, quota verification and closeout. It bought
no measured AWS cell because the external prerequisite failed before the environment could exist;
that is a valid stop under the work unit's bounded scope, not an invitation to draw more time.

A further **2–3 days** are reserved beyond the development budget for rerunning decisive
experiments, validating negative controls, reviewing measurements and interpretations, correcting
documentation, polishing diagrams, checking public-disclosure suitability, and preparing the
repository for external readers. **Iteration B's Analyse & Review (#17) spent 0.5 day from this
reserve, leaving 1.5–2.5 days.**

PR5's Iteration C A&R and milestone-conclusion work is funded by PR5's existing **1.5 development
days**, not as another draw on that reserve. The remaining reserve stays available for review depth
and the final pre-publication pass; it is not used to keep waiting for AWS quota.

The time budget is a constraint, not an estimate to be expanded whenever a tool introduces
incidental complexity.

### 2.1 How the contingency is spent

**Completed work returns unused allocation to contingency rather than to scope.** PR3a came in at
1.0 against 3.0 and returned 2.0; PR3c came in at 1.5 against 2.0 (§2.1.2). The figures recorded are
conservative under the half-day rule rather than attempts to account for hours precisely.

The gain is **not** an invitation to widen Iteration C. Contingency is held for reruns, an
investigation that does not resolve on the first attempt, or another hard problem established by
evidence.

**Every PR is funded at what its scope costs.** No PR carries a deliberate shortfall.

PR4b and PR4c were both explicit evidence-driven transfers and are now closed. **1.0 day of
contingency remains** and is not silently reserved for a future AWS campaign.

**Contingency is not scope.** It is drawn on before §5's descope order. After the PR4b and PR4c
transfers, the contingency row is **2.0 total**: **1.0 is drawn** (§2.1.1) and **1.0 remains**.

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

### 2.1.3 PR4a is charged at 4.0 days, and PR4b draws 1.0 from contingency

**Nancy's decision, 2026-08-18:** PR4a is charged at **4.0 days actual**, consuming the whole
former PR4a/PR4b shared envelope. **PR4b is allocated 1.0 day, drawn from contingency.** PR4c was
left unbudgeted at that point.

PR4a absorbed the envelope because the re-baseline moved work into it rather than because it
overran its own scope: independent per-group demand streams, the explicit conditioning contract and
its accounting, the frozen pool policy and the evidence behind it, derived fixture sizing, the
600 s retained-run shape, and the single host-executable entry point — none of which existed when
the 4.0 days were split between preparation and execution.

**The draw is a transfer.** Contingency falls from 4.0 to 3.0 and PR4b's day appears once, as that
row's allocation, so the milestone total stays **19.5 days** exactly as PR3c's return did. It is
recorded differently from §2.1.1, whose day funded work that has no work-unit row and is therefore
visible only as contingency spend.

**What it cost in slack.** 2.0 days of contingency remained after PR4b. PR4b's budget was
deliberately bounded: new methodology work or another diagnostic investigation was a stop/re-plan
signal rather than an automatic second draw. PR4b followed that rule and closed once the local
shared-write-path limit was established strongly enough to withhold the unsupported efficiencies.

### 2.1.4 PR4c receives and closes within 1.0 day

**Nancy's decision, 2026-08-19:** allocate **1.0 day from remaining contingency** to PR4c and start
with the smallest useful independent-node experiment rather than the full retained capacity matrix.
The initial plan assumed the retained 5-vCPU Standard On-Demand allowance could support two
two-vCPU serving units plus separate one-vCPU generator compute.

**Closure, 2026-08-20:** the same quota was observed at **1 vCPU** immediately before execution, and
EC2 enforced it by refusing one `c5.large`. Earlier increase requests to 20 and 12 vCPUs had been
declined, and a later 6-vCPU request was also declined. The 5-vCPU minimum topology therefore could
not be instantiated. No measured cell ran.

PR4c is charged at its allocated **1.0 day** for design, review/method refinement, provisioning
attempt, quota verification and closeout. No further contingency is drawn. The work unit closes
`STOP / DEFER`; any future cloud experiment returns to post-AG-Sept planning rather than inheriting
this allocation.

### 2.2 Review depth is the throughput control

**Review is not free. Nancy's decision, 2026-08-12:** Iteration B's Analyse & Review in PR #17 is
charged at **0.5 day** against the separate 2–3 day review/rerun/interpretation reserve. That leaves
**1.5–2.5 days** in that reserve.

For the final Iteration C, A&R is deliberately combined with PR5 rather than scheduled as a separate
review work unit. This is a milestone-closeout exception, not a change to the normal engineering
loop: PR5 still produces the Analyse & Review outcome required by
[`../development/engineering-process.md`](../development/engineering-process.md) §1.4.1, but also
uses that outcome to make the AG-Sept goal and architecture conclusion in the same work unit.

Review depth is Nancy's to set. Less detailed review moves more responsibility onto implementation
verification; it does not make the correctness/evidence gates optional.

### 2.3 Iteration C planning is intentionally bounded, not a general precedent

**Nancy's decision, 2026-08-12:** PR #18 has a **0.5-day hard cap** and closes within that
active-work budget.

That cap is justified by this iteration's state, not by a belief that Requirements/Design/
Validation should generally be rushed. PR2 already exposed the mutation frontier, Iteration B
already established the authority architecture, and PR #17 already selected a narrow Problem. The
remaining planning work is primarily to place known decisions in their correct semantic owners and
derive the minimum environment that makes the comparison legitimate.

A future Problem with genuine unresolved uncertainty should receive the investigation its
uncertainty requires. **Process depth follows uncertainty; this bounded decision-recording exercise
is specific to Iteration C.**

### 2.4 PR4a and PR4b shared the PR4 envelope after local qualification expanded

**Nancy's decision, 2026-08-14:** retire the original fixed 1.5-day / 2.5-day PR4a/PR4b split while
keeping the combined **4.0-day PR4 allocation unchanged**.

**Superseded by §2.1.3 (2026-08-18)**, which charged the whole 4.0 days to PR4a and funded PR4b
separately from contingency. The reasoning below is why the split was retired and remains accurate
as history; the shared envelope it created no longer exists.

The evidence changed the expected value of the two work units. The 16-vCPU WSL/Docker setup proved
to be a useful, repeatable and unmetered place to exercise the real G1/G2/G4 machinery, while AWS
quota approval was slower than the schedule assumed. More importantly, the PR4 implementation
record exposed materially different local performance regimes under nominally similar cells.
Carrying that unresolved ambiguity into AWS would spend metered time before the measurement
procedure itself was qualified.

That decision expanded PR4a from machinery rehearsal to measurement qualification. It remains part
of the scheduling history; §2.5 records the later evidence-driven re-cut after the root cause was
demonstrated and the workstation's usefulness became clearer.

### 2.5 PR4 is re-cut as preparation → local evidence → bounded independent verification

**Nancy's decisions, 2026-08-18 through 2026-08-20:** the PR boundary follows purpose rather than
environment, and independent verification is never allowed to weaken its resource-separation
requirement merely to fit an account quota.

The workstation exposed enough CPU to exercise the complete G1/G2/G4 topology and was unmetered, so
limiting it to a rehearsal would have thrown away useful controlled evidence. PR4b then established
that the local G1 denominator varied materially because the shard groups still shared the host write
path, both justifying independent provisioning and warning against assuming a cloud capacity unit
would be variance-free.

PR4c was therefore opened probe-first. Review refined that probe again before execution: the future
paired comparison keeps **A/B fixed on CU1 and C/D fixed on CU2** both alone and composed, rather
than halving each authority's working set only at G2; and the shared generator host's physical
headroom becomes an explicit admissibility condition in addition to independent worker streams.

The intended boundary was:

```text
PR4a — prepare and qualify the experiment
PR4b — execute and analyse the full sustained experiment locally
PR4c — probe two independent AWS units separately and together
```

The third step was then externally blocked. Earlier retained CLI evidence showed a 5-vCPU applied
Standard On-Demand quota; execution-time evidence showed 1 vCPU, below even one selected two-vCPU
capacity unit. The refined PR4c method is retained, but no workload implementation or measured AWS
result is claimed.

This does **not** weaken the Iteration C Problem or `REQ-SCALE-4`. Local scheduler partitioning and
independent provisioning remain different evidence classes. The complete independently provisioned
capacity question remains unresolved and moves out of AG-Sept rather than keeping the milestone
open indefinitely.

## 3. PR sequence

Each entry states what the work unit delivers and the gate that ends it. Technical meaning stays in
the linked requirements, design, workload catalogue, measurement contract, and validation plan.

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

**Actual 0.5 day. Docs-only.** Requirements → Design → Validation plan → Schedule for the Problem
selected by #17.

It records `WL-MUT-DISP-4`, the shard-group capacity-unit model, numeric efficiency as an output
rather than a threshold, the fixed 1/2/4 local placement family, and the stronger independent-resource
envelope requirement. Later PR4 evidence refined **how** the experiment is scheduled without
weakening those owners.

### PR4a — Iteration C experiment preparation and measurement qualification — merged #19

PR4a answered one question: **can the experiment be trusted?** It produced no canonical G1/G2/G4
capacity result.

It delivered the reusable method and run primitives needed before the evidence-producing comparison:

- independent **`workers_per_group`** streams with per-group sequence/accounting and the
  discriminating `VAL-NEG-8` control;
- explicit state-based conditioning, retained conditioning population, measured-start baseline and
  state-preserving service/pool recycle;
- the bounded pool sensitivity and frozen `pool_max_conns=8` policy;
- fixture-sizing/headroom machinery;
- the canonical 600 s run primitive, retained 60 s slices, observability and reconciliation;
- the host evidence needed to diagnose CPU/memory/storage behaviour;
- removal of the benchmark-induced cached-plan regime from the measured start state.

AWS-specific execution was deliberately removed from its exit gate once the local environment
proved useful enough to qualify the method without spending quota.

### PR4b — Local sustained capacity and scale characterisation — merged #20

PR4b answered: **what does the qualified experiment show on the scheduler-partitioned workstation?**
It was the mandatory evidence-producing PR4 work unit, with a **1.0-day bounded experiment-execution
budget** from §2.1.3.

It drove adaptive reconnaissance, fixed the common 12/16 S/H bracket, derived one final fixture and
retained the full S/H + confirm-S/confirm-H 600 s comparison across G1/G2/G4.

**Gate met 2026-08-19 on the "explicitly unresolved" branch, at the allocated 1.0 day and with no
further contingency draw.** All twelve retained runs were driven and reconciled. `G4_local` resolved
at 3493.9/s; `G1` and `G2` did not, so `G1_local`, `G2_local`, `E2_local` and `E4_local` are
withheld. Four identical G1 runs then measured 25.1% spread and refuted run position as the
explanation. Goodput covaries with delivered write bandwidth at broadly stable mutations/MiB, which
localises the limiting variation to the shared write path without proving the deeper VHDX mechanism.
Nothing was promoted into `VAL-SCALE-5`, and `VAL-LOAD-1` was not attempted because the closed-loop
result it depends on was not secured.

### PR4c — Bounded independently provisioned AWS probe — closed `STOP / DEFER`

PR4c was intended to answer: **what does the first genuinely independent two-unit environment
show?** It closes at **1.0 day** from §2.1.4 with **no measured AWS cell**.

The planned minimum serving topology was:

```text
CU1                 2-vCPU equivalent non-burstable serving unit
CU2                 2-vCPU equivalent non-burstable serving unit
generator/monitor   separate 1-vCPU measurement compute
```

Earlier retained AWS CLI evidence showed the relevant applied quota at **5 vCPU**, which was enough
for that 2 + 2 + 1 shape. On 2026-08-20 the same applied quota was **1 vCPU**. EC2 then refused one
`c5.large` because that single instance requires two vCPUs. With the independent environment unable
to exist, the probe stopped before bootstrap/load execution rather than collapsing resources onto
one host or changing the serving-unit question.

Review work before the stop improved the future method: keep A/B fixed on CU1 and C/D fixed on CU2
both separately and together, use a distinct probe/unit-slice workload identity rather than
weakening `WL-MUT-DISP-4`, compare per-unit retained Goodput as well as aggregate composition, and
make generator **process + host** headroom an explicit evidence gate.

Design: [`../design/independent-capacity-probe.md`](../design/independent-capacity-probe.md). Validation
record: [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).

**Gate:** `STOP / DEFER`. Record the quota change and failed provisioning attempt, retain no AWS
performance number, leave `VAL-SCALE-5` unproven, and proceed to PR5. Future AWS work requires a
new post-AG-Sept decision and prerequisite check.

### PR5 — Iteration C Analyse & Review + AG-Sept conclusion

**Budget:** 1.5 development days, following PR4b's retained local evidence and PR4c's explicit
external provisioning blocker.

PR5 consumes the strongest retained Iteration C evidence and closes both the final iteration and
the AG-Sept milestone. It records the Analyse & Review outcome required by
`engineering-process.md` §1.4.1 in the governing Goal/Problem record, then assembles the
evidence-backed architecture conclusion.

It delivers:

- the **Iteration C Problem verdict** — sufficiently resolved, unresolved, or refined — with the
  retained evidence that supports it;
- the **durable learning** propagated into requirements, design, validation, ADRs, or explicitly
  recorded as requiring no change;
- the **AG-Sept Goal progress/verdict**, stating what the milestone established and what remains
  unproven or deferred;
- an evidence-backed architecture conclusion, comparison tables/diagrams, service-boundary
  decision, limitations, and reproduction entry points;
- explicit deferred-work and roadmap candidates where useful, without silently promoting them into
  requirements or scheduled work;
- the loop decision **`END` for AG-Sept**, either because the Goal is sufficiently achieved for the
  agreed scope or because the maintainer makes an explicit milestone goal/scope closure decision
  under `engineering-process.md` §1.4.

Because this work closes AG-Sept, **PR5 does not choose or schedule the next Alloca Problem.** If
Alloca continues after the milestone, a separate kickoff starts from current evidence, the roadmap,
and current priorities to choose the next Goal and Problem before beginning a new engineering
iteration.

**Gate:** the Iteration C A&R outcome is durable, the AG-Sept goal/scope verdict is explicit, every
architecture claim is traceable to retained evidence, limitations and unproven areas are named, no
new implementation stage is invented, and the AG-Sept engineering loop is closed before
publication work begins.

The Iteration C closeout path is now:

```mermaid
flowchart TD
    A[PR4a: prepare + qualify method] --> B[PR4b: full local sustained G1/G2/G4 evidence]
    B --> C[PR4c: refine independent probe + attempt AWS provisioning]
    C --> D[Applied quota 1 vCPU: probe STOP / DEFER, no AWS result]
    D --> E[PR5: Iteration C A&R + AG-Sept conclusion]
    E --> F[Pre-publication documentation pass]
    F --> G[Public release]
    G --> H[AG-Sept closed]
```

A future AWS experiment may reuse the retained design, but it does not alter this closeout path.

## 4. Manifest and reconciliation staging

The rules are `measurement-contract.md` §11–§13. PR3b established multi-authority topology/image
identity. PR4a extended the harness so conditioning, `workers_per_group`, per-group demand identity,
measured-start baselines, fixed pool policy and the 600 s retained run shape are explicit and
auditable. PR4b applied that machinery to every quoted local G1/G2/G4 point.

PR4c produced **no run artifact** because provisioning failed before the independent environment
existed. Its refined future method deliberately requires a distinct probe/unit-slice workload
identity and generator-host headroom evidence; those are design requirements, not capabilities
claimed as implemented by AG-Sept.

Reconciliation: PR1 established the single-authority self-check; PR3b extended it to multiple
authorities; PR3c exercised it against deliberate failure. PR4a added the conditioning/measurement/
resolution population boundary; PR4b exercised it on the full local sustained result. No AWS
reconciliation exists because no AWS measured cell ran.

**Quotability target:** provenance level and validation meaning remain separate. No external
project-level independent-capacity claim follows until the governing validation/evidence gates for
that claim are satisfied.

## 5. Priority and descope

### 5.1 P0 — required

For Iteration C the mandatory path remains independent of successful AWS capacity measurement:

- stable `WL-MUT-DISP-4` request semantics and fixed per-organisation fixture populations for the
  local Iteration C matrix;
- PR4a's qualified method and reusable primitives: independent `workers_per_group`, `VAL-NEG-8`,
  explicit conditioning and measured-start accounting, fixed pool policy, fixture-sizing/headroom
  mechanism, qualified 600 s run shape, and generator/resource/reconciliation controls;
- PR4b's **full local G1/G2/G4 sustained capacity/scale characterisation**, delivered 2026-08-19 on
  the explicit unresolved branch: `G4_local` resolved, `G1`/`G2` did not, and unsupported
  efficiencies were withheld;
- correctness/reconciliation and provenance/resource evidence required for every quoted result;
- `VAL-SCALE-5` explicitly unproven because the complete independent Tier-1 family did not run.

PR4c is now closed at provisioning. Successful AWS capacity measurement remains outside the P0
completion gate. The older service-replica VAL-SCALE-1/2 path is not Iteration C scope.

### 5.2 P1 — strongly desirable

- `VAL-LOAD-1` bounded open-loop characterisation only after a secure closed-loop result exists in
  the environment being characterised;
- a deliberately constrained resource control (`VAL-NEG-5`) if it materially strengthens a
  limiting-resource diagnosis;
- polished charts beyond the minimum needed to communicate the result;
- extra diagnostic reruns requested by A&R.

The former PR4c AWS opportunity is no longer an AG-Sept P1 item; it is a post-milestone candidate.
Capacity/resource economics is likewise unscheduled unless a future kickoff selects it as part of a
new Goal or Problem.

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

Independent AWS capacity measurement is now also **deferred beyond AG-Sept**. The retained PR4c
design is a future experiment candidate only; no cloud-production conclusion follows from the
failed provisioning attempt.

### 5.5 Descope order

PR4c has already taken the strongest descope action available: **stop before weakening the experiment
when the required independent environment cannot be provisioned**.

If time slips in the remaining closeout, remove work in this order:

1. optional open-loop depth or the whole `VAL-LOAD-1` round when no secure closed-loop result exists;
2. optional resource-limit control beyond the evidence already needed for diagnosis;
3. chart/dashboard polish beyond the diagnostic minimum;
4. extra confirmation/rerun work beyond what the active validation method requires, unless A&R
   needs it to resolve material variation;
5. PR5 presentation/chart polish beyond what the milestone conclusion requires.

**Do not descope:** PR4a measurement qualification; independent per-group demand; explicit
conditioning/accounting; the fixed pool policy; PR4b's full local evidence; response validation;
reconciliation; provenance/resource capture; or honest evidence labelling. Do not reinterpret the
AWS provisioning failure as measurement evidence.

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
4. Iteration C Requirements/Design derived that the fixed workstation cannot prove the selected
   independently provisioned capacity claim, which justified a much smaller AWS EC2 path;
5. PR4a added a bounded local resource-partitioned rehearsal so the topology and measurement
   machinery could be debugged before metered work;
6. that rehearsal proved unusually valuable, exposed and ultimately root-caused a benchmark-induced
   cached-plan regime, while the workstation itself proved large enough to exercise the full
   topology family;
7. AWS quota-increase requests were declined on the new account, making AWS a weaker immediate
   execution environment than the unmetered workstation even though it remained the stronger
   **evidence class** for independent resource envelopes;
8. on 2026-08-18 the work was therefore re-cut by purpose: PR4a qualifies, PR4b measures locally,
   and PR4c independently verifies only if useful;
9. PR4b then measured the local shared-write-path limitation directly enough to withhold the
   efficiencies, while its identical G1 control showed that independent provisioning should not be
   assumed variance-free. PR4c was therefore opened probe-first;
10. review refined that probe to fixed per-unit A/B and C/D workload slices and explicit generator-
    host headroom, but on 2026-08-20 the previously retained **5-vCPU applied quota was observed at
    1 vCPU**. EC2 refused one two-vCPU `c5.large`, so PR4c stopped before measurement and AG-Sept
    moved to closeout rather than redesigning around the account limit.

### 6.3 The original AWS path remains withdrawn; independent capacity is post-AG-Sept

The v0.4 AWS plan — ECR/EKS/RDS/load-balancing path plus separate EC2 generator and its larger
matrix — remains withdrawn. PR4c did **not** restore it and produced no AWS performance result.

The completed evidence path is:

```text
qualified method
    -> full local scheduler-partitioned characterisation
    -> refined AWS probe design + failed provisioning prerequisite
    -> PR5 Analyse & Review
```

A fuller independent G1/G2/G4 verification, or the bounded paired probe itself, is possible future
work only if later selected and if external prerequisites are verified first. No EKS, RDS,
managed-database, service-mesh, or cloud-production conclusion follows.

### 6.4 The PR2 deferral register

- **Overload question:** broad overload mechanism work remains outside AG-Sept; `VAL-LOAD-1` is a
  bounded characterisation attached to an already-secured capacity result, not a new
  admission-control design problem.
- **Instrumentation:** the old PostgreSQL/node-exporter choices are no longer schedule owners. The
  durable obligations are the evidence needed to identify/bound each quoted result's frontier and
  `VAL-NEG-7` for any independently provisioned claim.
- **Evidence hygiene:** per-run retained evidence required by the strongest executed PR4 path is in
  scope; unrelated PR2 re-runs/histogram work remains deferred.

## 7. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator with placement-aware routing and independent per-group
   demand streams;
3. a stable reusable workload catalogue;
4. aggregated service/runtime/resource evidence sufficient for the claims made;
5. the single-instance frontier report;
6. the multi-authority correctness/failure-isolation report;
7. the PR4a measurement-qualification record, PR4b local sustained result — delivered with
   `G4_local` established and `E2_local`/`E4_local` withheld — and the PR4c **deferred independent-
   unit design plus explicit AWS provisioning blocker**, with no AWS diagnostic number and
   `VAL-SCALE-5` left unproven;
8. current and intended architecture diagrams;
9. authority and bottleneck analysis across the scaling axes actually measured, with unmeasured
   axes named explicitly;
10. the PR5 **Iteration C A&R + AG-Sept conclusion**, including the Problem verdict, Goal verdict,
    evidence-backed architecture conclusion, service-boundary decision, limitations, and deferred
    work;
11. a concise public repository summary;
12. explicit limitations, evidence labels, and negative-control results;
13. a bounded **pre-publication documentation pass** across `docs/`, reviewed from an external
    reader's perspective and applying the diagram convention in [`../README.md`](../README.md):
    improve first-read comprehension where diagrams materially help; reduce unnecessary density or
    duplication without weakening precision; verify navigation, semantic ownership, terminology,
    and document status; and remove stale planning or implementation language where it obscures the
    public story.

## 8. Completion gate

AG-Sept's schedule is complete when the deliverables of §7 exist and every P0 item is validated or
explicitly recorded as unproven.

Before PR5 can close Iteration C and judge the AG-Sept Goal:

- PR4a has qualified the reusable method and run primitives defined by the validation plan;
- PR4b has executed the full local G1/G2/G4 S/H + confirmation method and retained the explicit
  unresolved `VAL-SCALE-6` verdict without manufacturing `E2_local`/`E4_local`;
- PR4c has **explicitly refused execution at provisioning** because the independent environment
  could not be instantiated under the 1-vCPU applied quota, and has retained no synthetic or
  partial AWS result as a substitute;
- `VAL-SCALE-5` remains explicitly unproven because a complete independent validation did not run;
- response validation, conditioning-aware reconciliation, provenance and applicable resource
  controls are demonstrated for every result actually quoted;
- outcomes and persisted state reconcile on every participating authority for interpreted runs;
- measured facts, calculations, interpretation, and limitations are separated;
- architecture reflects evidence rather than desired presentation;
- the repository remains suitable for public review under the disclosure policy.

**Successful AWS capacity measurement is not an Iteration C exit gate.** PR4b's complete local
characterisation remains the mandatory capacity-characterisation path. PR4c demonstrates the other
valid branch: when the stronger independent environment cannot be provisioned, record the blocker
and leave the stronger claim unproven rather than weakening the experiment or keeping the milestone
open indefinitely.

PR5 then records the required Analyse & Review outcome and closes the **AG-Sept** engineering loop.
It does not start the next Alloca iteration. Any continuation after AG-Sept begins with a separate
kickoff that chooses the next Goal and Problem from the evidence, roadmap, and priorities current at
that time.

**After PR5 and before the repository is made public**, complete §7's pre-publication documentation
pass. This is a public-readiness gate, not a new technical iteration: it improves how the settled
AG-Sept work is understood without changing requirements, evidence, or semantic ownership. Some
improvements may land naturally during PR5, especially architecture conclusions and diagrams, but
the repo-wide external-reader pass happens after PR5 so it polishes the final technical story once.
The pass is complete when the durable docs are navigable and internally consistent for an external
reader, diagrams have been added where they materially improve first-read comprehension under
`docs/README.md`, and stale or unnecessarily dense prose has been cleaned without rewriting the
historical or evidential record.

A finished schedule is an input to Analyse & Review, not a substitute for it; in this final
iteration, PR5 is the work unit that performs that Analyse & Review and closes the milestone.
