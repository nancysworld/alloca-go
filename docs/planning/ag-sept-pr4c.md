# AG-Sept PR4c — bounded AWS independent verification

**Status:** Closed — `STOP / DEFER`, 2026-08-20; AWS measurement did not start.  
**Milestone:** AG-Sept, Iteration C.  
**Predecessor:** PR4b (#20), merged 2026-08-19.  
**Budget:** **1.0 development day**, transferred from AG-Sept contingency on 2026-08-19; closed within that allocation.  
**Design:** [`../design/independent-capacity-probe.md`](../design/independent-capacity-probe.md).  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).  
**Milestone schedule/budget owner:** [`ag-sept-plan.md`](ag-sept-plan.md).

PR4c was the bounded attempt to obtain genuinely independent shard-group evidence before AG-Sept
closeout. It produced **no AWS performance result**. The experiment was stopped at provisioning
because the account could no longer instantiate even one selected two-vCPU capacity unit.

## 1. What happened

Earlier retained AWS CLI evidence for `eu-west-2` showed the applied
`Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances` quota at **5 vCPU**. PR4c was
therefore deliberately reduced to a minimum **2 + 2 + 1 vCPU** topology: two equivalent two-vCPU
serving units plus separate one-vCPU generator/measurement compute.

At execution time on 2026-08-20 the same applied quota was **1 vCPU**. EC2 independently enforced
that value by refusing the launch of a single `c5.large` because that instance alone requires two
vCPUs. Earlier requests to increase the quota to 20 and then 12 vCPUs had been declined; a later
request for 6 vCPUs was also declined.

This is an **external provisioning/account constraint**, not evidence about Alloca-Go capacity or
architecture. No throughput, scale-efficiency, or independent-composition conclusion is inferred
from the failed launch.

## 2. Why AG-Sept stops here

The experiment exists to compare **independently growing serving resource envelopes**. Shrinking the
serving units, collapsing them onto one host, switching to a materially different topology, or
using a partial single-unit run merely to fit a 1-vCPU quota would answer a different question.

Successful AWS capacity measurement was already not an Iteration C exit gate. PR4b delivered the
strongest local evidence available and correctly withheld unsupported `G1_local`, `G2_local`,
`E2_local`, and `E4_local`; `VAL-SCALE-5` remains explicitly unproven. The milestone therefore moves
to PR5 Analyse & Review / architecture conclusion rather than waiting indefinitely on an external
quota decision.

Any future AWS execution is a **post-AG-Sept candidate** requiring a new planning decision and
sufficient quota. It does not inherit AG-Sept schedule or budget automatically.

## 3. Probe design retained for future use

Before provisioning stopped, review improved the probe method. That design is worth retaining even
though it was not implemented or measured.

The future paired comparison should keep a fixed workload/state envelope **per capacity unit**:

```text
G1-CU1        CU1 drives organisations A/B
G1-CU2        CU2 drives organisations C/D
G2-CU1+CU2    CU1 still drives A/B; CU2 still drives C/D
```

A/B/C/D are equivalent synthetic populations; their names carry no intended behavioural
difference. This shape compares each unit alone with the **same workload slice it carries when
composed**, avoiding the earlier design's factor-of-two per-authority working-set reduction between
G1 and G2.

The existing `WL-MUT-DISP-4` identity must **not** be weakened to express this probe: that workload
intentionally means the exact global A/B/C/D population at every topology. A future implementation
must introduce an explicit probe/unit-slice workload identity or family and retain the slice
assignment in its manifest.

The generator remains outside the capacity units and must provide one independent worker stream per
active unit. A future G2 result is interpretable only when generator **process and host headroom** are
observed directly; separate worker pools prove scheduling independence but do not prove that the
shared generator host is not a resource bottleneck.

## 4. Result boundary

PR4c reports only this outcome:

```text
AWS probe: NOT EXECUTED
reason: applied Standard On-Demand quota = 1 vCPU at execution time
minimum planned probe: 5 vCPU (2 + 2 + 1)
VAL-SCALE-5: UNPROVEN
```

No `g1_CU1`, `g1_CU2`, `g2_CU1_CU2`, `R2_probe`, `G1_aws`, `G2_aws`, `E2_aws`, or `E4_aws` exists.

## 5. Exit decision

**`STOP / DEFER`.** Record the external quota change and failed provisioning attempt, preserve the
refined experiment design for possible post-AG-Sept work, and proceed to PR5. Do not weaken the
independent-resource requirement or reinterpret local evidence to manufacture the missing AWS
result.
