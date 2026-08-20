# AG-Sept PR4c — AWS independent-unit probe

**Status:** Not executed — `STOP / DEFER`, 2026-08-20.  
**Purpose:** preserve the bounded independent-unit validation design and record why no AWS result
exists.  
**Design:** [`../../design/independent-capacity-probe.md`](../../design/independent-capacity-probe.md).  
**Governing validation plan:** [`ag-sept-validation-plan.md`](ag-sept-validation-plan.md) §4.6,
`VAL-SCALE-5`, `VAL-NEG-7`, `VAL-NEG-8`.  
**Measurement contract:** [`../../design/measurement-contract.md`](../../design/measurement-contract.md).

This probe never became a measured experiment. At execution time the account's applied
`Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances` quota in `eu-west-2` was 1 vCPU,
and EC2 refused the launch of one selected two-vCPU `c5.large`. The minimum planned topology needed
5 vCPUs. No partial topology substitutes for the missing independent-resource comparison.

The design below is retained as a **post-AG-Sept candidate only**. It is not implemented by this PR
and it does not create a follow-on schedule or budget.

## 1. Question

At one common non-canonical worker level, do two equivalent independently provisioned capacity units
behave materially the same when driven separately and when the **same per-unit workload slices** are
driven together?

The paired comparison is intended to distinguish capacity-unit composition from shared-resource
coupling. It does not estimate a precise capacity multiplier and does not discharge `VAL-SCALE-5`.
Probe-specific `P1/P2` names are used deliberately so these observations cannot be confused with the
canonical `G1/G2/G4` capacity family.

## 2. Fixed-per-unit workload shape

The future probe should use the four established equivalent synthetic organisations as two fixed
capacity-unit slices:

```text
P1-CU1        CU1 -> A/B
P1-CU2        CU2 -> C/D
P2-CU1+CU2    CU1 -> A/B, CU2 -> C/D
```

The unit's workload, fixture/state population, worker count, pool policy, service/database shape and
organisation assignment therefore remain unchanged between its individual and composed cells.
Total active workload grows from two organisations to four when the second capacity unit is added.
This is a weak-scaling/composition diagnostic: **add one equivalent workload/resource unit and ask
whether the first unit changes merely because the second is present**.

A/B/C/D are intentionally equivalent synthetic populations. Their identifiers distinguish routing
and evidence only; no behavioural difference is intended.

### 2.1 Do not reuse `WL-MUT-DISP-4` unchanged

`WL-MUT-DISP-4` deliberately fixes one global A/B/C/D population across its G1/G2/G4 comparison.
The probe above has a different identity: each measured capacity unit owns one fixed two-organisation
slice, and the active slice count changes with topology.

A future implementation must therefore introduce a distinct probe/unit-slice workload identity or
family and retain the slice participants in the manifest. It must **not** relax the existing
`WL-MUT-DISP-4` validation merely to make this probe fit.

## 3. Common probe configuration

The last planned first pass was:

```text
workers_per_group (P)     12
measured window            120 s
pool_max_conns             8 per shard group
conditioning target        4,000 fresh mutations / organisation
fixture                     45,000 slots / organisation, capacity 20
service replicas/group      1
PostgreSQL authorities      1 / active group
```

Those values are diagnostic starting points inherited from PR4a/PR4b, not AWS capacity points. A
future execution must re-establish that the selected resource envelope can support the measurement
before interpreting any result.

## 4. Independent generator requirement

One separate generator/measurement host drives the probe. Generator independence has two distinct
parts:

1. **stream independence** — each active capacity unit has its own fixed worker pool, request
   sequence and response accounting, so a slow CU2 request cannot consume CU1's workers;
2. **resource headroom** — those streams still share one generator process/host, so the composed
   `P2-CU1+CU2` cell is uninterpretable if generator CPU, scheduling, memory, network, GC/runtime or
   measurement overhead plausibly limits the offered demand.

A future interpreted cell therefore retains:

- generator process CPU in **cores consumed** (`cpu_seconds / measured_duration`), not only a
  normalised percentage;
- whole generator-host CPU busy, run queue/load, steal/iowait, memory and network evidence;
- per-unit workers, completions, Goodput/outcomes and latency;
- enough runtime evidence to investigate a generator-side anomaly if one appears.

If the generator is a burstable T-family instance, use **Unlimited** credit mode for the measured
probe and retain the relevant CPU-credit/surplus state so run order cannot silently become a credit-
depletion variable. Capacity units themselves should be non-burstable for a capacity comparison.

`VAL-NEG-8` remains established structurally by PR4a. A future cloud execution may add a short
deployed slow/stopped-unit control if useful, but the ordinary composed cell still has to
demonstrate physical generator headroom independently of that structural property.

## 5. Preconditions and evidence

No interpreted future probe begins until:

- CU1 and CU2 are separate equivalent serving instances with separate storage allocations;
- the generator/measurement stack is on separate compute;
- service image, PostgreSQL/schema/configuration, pool and timeout policy match across units;
- each unit's individual-cell starting state matches its own composed-cell starting state logically,
  including fixture and conditioning population;
- placement and active-unit metadata match the requested topology;
- host/service/PostgreSQL/generator evidence is populated and time-aligned;
- response validation, reconciliation and final logical-mutation accounting succeed.

Because the original design review exposed a working-set confound, a future execution should also
retain pre-measurement per-unit state evidence sufficient to show that the intended fixed-per-unit
logical population was actually achieved. Physical table/index byte sizes need not satisfy an
arbitrary percentage gate, but any material unexplained mismatch should be investigated before the
paired ratio is interpreted.

## 6. Diagnostic outputs

Let the full-window per-unit fresh-mutation Goodput observations be:

```text
p1_CU1_AB
p1_CU2_CD
p2_CU1_AB
p2_CU2_CD
p2_total = p2_CU1_AB + p2_CU2_CD
```

Report the raw values first, then derive:

```text
R_CU1 = p2_CU1_AB / p1_CU1_AB
R_CU2 = p2_CU2_CD / p1_CU2_CD
R2_probe = p2_total / (p1_CU1_AB + p1_CU2_CD)
```

The per-unit retention ratios are the primary diagnostic: if only one unit changes under
composition, the aggregate ratio must not hide it.

The difference between the two individual baselines may also be reported, but it should be named a
**baseline difference**, not a measured noise range. One observation per unit cannot estimate
run-to-run variance or a confidence interval.

No numerical pass threshold is predeclared. The intended hypothesis is simply that composition
introduces **no material observed per-unit change beyond the unit/environment variation visible in
the bounded probe**.

### 6.1 One observation per cell cannot support that hypothesis

With a single individual and a single composed reading per unit, `R_CU1` and `R_CU2` confound a
composition effect with ordinary run-to-run and provider variation. The hypothesis above is stated
against "variation visible in the bounded probe" — a quantity that one reading per cell does not
produce at all.

PR4b is the standing warning, from this repository's own evidence: at `G1` the selected-point pair
agreed to **1.3%**, while four *identical* runs of that same cell spanned **25.1%**
([`pr4b-capacity/README.md`](../../measurements/pr4b-capacity/README.md),
[`pr4b-drift-g1/README.md`](../../measurements/pr4b-drift-g1/README.md)). A close pair is not
evidence of stability; it is what an unstable cell looks like most of the time.

A future execution therefore does one of two things:

- **repeats or interleaves the individual and composed cells** for at least one unit, so a
  composition difference can be read against that unit's own repeat spread; or
- **reports the raw observations uninterpreted** — deriving no retention ratio and testing no
  composition hypothesis.

At probe scale the extra cell costs minutes. Choosing neither option produces a number that reads as
composition evidence while being indistinguishable from noise, which is the specific failure
`VAL-SCALE-6` already demonstrated locally.

`R2_probe` is neither `E2_aws` nor comparable with `E2_local`. The local quantity belongs to the
fixed-total A/B/C/D strong-scaling-style comparison; this probe holds each unit's workload/state
envelope fixed and adds an equivalent workload slice with the second unit. None of these diagnostic
quantities establishes saturation or discharges `VAL-SCALE-5`.

## 7. Execution outcome in AG-Sept

No probe cell ran. Therefore:

```text
p1_CU1_AB     NOT MEASURED
p1_CU2_CD     NOT MEASURED
p2_CU1_AB     NOT MEASURED
p2_CU2_CD     NOT MEASURED
R_CU1          NOT DERIVED
R_CU2          NOT DERIVED
R2_probe       NOT DERIVED
VAL-SCALE-5    UNPROVEN
```

A retained AWS CLI `GetServiceQuota` capture from planning reported an applied value of **5 vCPU**
for the relevant Standard On-Demand quota. The earlier table capture preserves the quota name and
applied value together with its command line and Region, but not its exact date; see
[`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/). At execution time on 2026-08-20,
a second retained capture explicitly querying `eu-west-2` quota `L-1216C47A` reported **1 vCPU**,
and EC2 enforced that value by refusing one selected two-vCPU `c5.large`. The retained captures
therefore show the reported applied value fell from **5.0 to 1.0** between observations. Why AWS
reduced it is unknown and was deliberately not pursued further in AG-Sept. Quota-increase requests
were declined. This is not a Tier-2 trigger because the independently provisioned environment never
existed.

## 8. Decision

**`STOP / DEFER`.** AG-Sept proceeds to PR5 with PR4b as its strongest capacity evidence and with
independent AWS capacity explicitly unproven. If the cloud experiment is revisited later, start from
this refined fixed-per-unit method, revalidate external prerequisites first, and plan it as new
post-milestone work.
