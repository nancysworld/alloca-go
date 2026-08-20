# Independent capacity-unit probe

**Status:** Deferred — refined during PR4c, not executed in AG-Sept.  
**Scope:** smallest independently provisioned experiment that can distinguish capacity-unit
composition from shared-resource coupling without changing each unit's own workload/state envelope.  
**Requirements:** `REQ-SCALE-4`, `REQ-EVID-1`, `REQ-EVID-2` in
[`../requirements/system-requirements.md`](../requirements/system-requirements.md).  
**Umbrella design:** [`horizontal-scaling.md`](horizontal-scaling.md) §12 and
[`deployment-architecture.md`](deployment-architecture.md) §13.  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).

This document owns the **probe-specific architecture** only. It does not redefine the complete
Tier-1 G1/G2/G4 capacity method or the meaning of `VAL-SCALE-5`.

## 1. Why the probe exists

PR4b ran the complete local G1/G2/G4 method on a scheduler-partitioned workstation. It established
`G4_local` but withheld `G1_local`, `G2_local`, `E2_local` and `E4_local` because the shared local
write path made the denominator insufficiently reproducible. That is evidence for the need to add
**independent serving resource envelopes**, not evidence for any particular cloud efficiency.

A first AWS experiment was therefore deliberately bounded to two equivalent capacity units measured
separately and together. Review then exposed a methodological problem in its original placement:
the canonical-style one-group cell placed A/B/C/D behind one authority while the two-group cell
placed only A/B or C/D behind each authority, so the per-authority working set halved at the same
time the second resource envelope was added. A write-path/working-set-sensitive result could
therefore look super-linear for reasons unrelated to capacity-unit composition.

The refined probe removes that confound by fixing the workload/state envelope **per unit** and uses
probe-specific `P1/P2` names so its quantities cannot be confused with canonical `G1/G2/G4`.

## 2. Probe topology

The serving topology is two equivalent capacity-unit hosts plus separate generator/measurement
compute:

```text
                 generator / measurement host
                         /             \
                        v               v
                capacity CU1       capacity CU2
             +-------------+      +-------------+
             | alloca-go   |      | alloca-go   |
             | PostgreSQL  |      | PostgreSQL  |
             | own storage |      | own storage |
             +-------------+      +-------------+
```

One capacity unit is one independently provisioned shard group: one Alloca-Go service replica, one
PostgreSQL authority and its own serving storage/resource envelope. The generator is measurement
infrastructure and is never counted as serving capacity.

Equivalent serving units must use the same intended instance/resource shape, storage class and
configuration, service image, PostgreSQL/schema configuration, pool policy and timeout policy.
They need not be the same physical host.

Capacity units should be non-burstable for the measurement. A burstable generator is acceptable
only when its headroom and credit state are observed directly and it is configured so credit
exhaustion cannot silently throttle later cells.

## 3. Fixed per-unit workload slices

The refined comparison keeps four total synthetic organisations but divides them into two equivalent
capacity-unit slices:

```text
slice U1 = A/B
slice U2 = C/D

P1-CU1        CU1 carries U1 (A/B)
P1-CU2        CU2 carries U2 (C/D)
P2-CU1+CU2    CU1 still carries U1; CU2 still carries U2
```

A/B/C/D are deliberately equivalent populations. Their names distinguish routing and evidence only.
The comparison therefore asks whether the **same unit workload** changes when another equivalent
unit is present.

This is a weak-scaling/composition diagnostic: adding CU2 adds both one serving resource envelope
and one equivalent workload slice. It is not the fixed-total-dataset strong-scaling-style comparison
used by the local `WL-MUT-DISP-4` G1/G2/G4 matrix.

That distinction is intentional. The two experiments answer different questions and their derived
ratios must not be treated as interchangeable.

## 4. Workload identity

The existing `WL-MUT-DISP-4` workload intentionally fixes the exact global A/B/C/D population across
its topology comparison. The unit-slice probe has different semantics because only U1 is active in
`P1-CU1`, only U2 in `P1-CU2`, and both in `P2-CU1+CU2`.

A future implementation must therefore use a **distinct probe/unit-slice workload identity or
family** and retain each slice's participants in the run manifest. Do not weaken or overload
`WL-MUT-DISP-4` to accept different participant sets under the same name.

The request mechanics may be factored internally with the existing workload, but the externally
retained workload identity must preserve the semantic distinction.

## 5. Generator independence

Independent per-group demand has two layers:

1. **logical/scheduling independence** — one fixed worker pool, sequence and response collector per
   capacity unit, so a slow CU2 request cannot consume CU1's worker budget;
2. **physical measurement headroom** — those streams share one generator process/host, so a
   `P2-CU1+CU2` cell is invalid if that shared measurement resource becomes a plausible limiter.

A future probe therefore retains generator process CPU as actual **cores consumed**, whole-host CPU
busy/run queue/steal or iowait/memory/network evidence, and per-stream Goodput/outcome/latency
accounting. The generator's resource envelope must have demonstrable headroom in the composed cell.

If a T-family generator is used, measured cells should use Unlimited credit mode and retain the
credit/surplus state needed to show that run order did not introduce throttling. This requirement is
for the measurement apparatus; capacity units themselves remain non-burstable for the comparison.

## 6. Starting-state equivalence

For each capacity unit, the individual and composed cells must begin from the same declared logical
state:

- same organisations in the slice;
- same slots per organisation and slot capacity;
- same conditioning target per organisation;
- same pool/service/PostgreSQL configuration;
- same worker count and measured horizon;
- fresh reset/reseed/conditioning and state-preserving service/pool recycle before measurement.

Because the original design failed specifically on a working-set confound, a future execution also
retains pre-measurement per-unit row/state and table/index-volume evidence sufficient to verify that
the intended fixed-per-unit start state was achieved. Physical byte counts are evidence, not an
arbitrary pass threshold; a material unexplained mismatch is investigated before interpretation.

State growth during the measured interval is an outcome/mechanism of the run, not a reason to force
its end state to match a slower unit.

## 7. Diagnostic outputs

At one common probe level `P`, retain:

```text
p1_CU1_AB
p1_CU2_CD
p2_CU1_AB
p2_CU2_CD
p2_total
```

and derive:

```text
R_CU1 = p2_CU1_AB / p1_CU1_AB
R_CU2 = p2_CU2_CD / p1_CU2_CD
R2_probe = p2_total / (p1_CU1_AB + p1_CU2_CD)
```

The per-unit retention ratios are primary because they expose asymmetric composition effects that
an aggregate ratio can hide.

The difference between `p1_CU1_AB` and `p1_CU2_CD` is also retained as a baseline/unit observation,
but one reading of each is not a statistically established noise range or confidence interval.

The intended hypothesis is qualitative: composition should introduce **no material observed per-unit
change beyond the unit/environment variation visible in the bounded probe**. There is no preselected
pass percentage.

**That hypothesis is not testable from one observation per cell**, because a single reading produces
no visible variation to judge "material" against, and the retention ratios then confound composition
with ordinary run-to-run variation. A future execution must either repeat or interleave the
individual and composed cells, or report the observations without deriving a ratio. The requirement
and the local evidence behind it are owned by
[`ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md) §6.1.

`R2_probe` is diagnostic only. It is neither `E2_aws` nor comparable with `E2_local`: the canonical
local experiment holds the total A/B/C/D workload fixed while changing placement, whereas this
probe holds each capacity unit's workload/state envelope fixed and adds an equivalent workload slice
with the second unit. `R2_probe` does not establish saturation and cannot discharge `VAL-SCALE-5`.

## 8. AG-Sept execution outcome

A retained AWS CLI `GetServiceQuota` capture from planning reported an applied value of **5 vCPU**
for the relevant Standard On-Demand quota. The earlier table capture preserves the quota name and
applied value together with its command line and Region, but not its exact date; see
[`../measurements/pr4c-quota/`](../measurements/pr4c-quota/). At execution time on 2026-08-20, a
second retained capture explicitly querying `eu-west-2` quota `L-1216C47A` reported **1 vCPU**, and
EC2 refused one selected two-vCPU `c5.large`. The minimum bounded topology required 2 + 2 + 1 =
5 vCPUs. Quota-increase requests were declined.

The retained captures establish that the reported applied value fell from **5.0 to 1.0** between the
observations. The reason for the reduction is unknown and was deliberately not pursued further in
AG-Sept.

The environment therefore never existed and **no probe cell ran**. This is an external account/
provisioning limitation, not an architecture result and not a Tier-2 condition.

AG-Sept consequently closes without independent AWS capacity evidence. `VAL-SCALE-5` remains
unproven. If this experiment is selected after the milestone, external quota/instance prerequisites
must be verified first and the work begins from the refined design above rather than from the
superseded fixed-total-dataset probe shape.
