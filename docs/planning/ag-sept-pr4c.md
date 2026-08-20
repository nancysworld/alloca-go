# AG-Sept PR4c — bounded AWS independent verification

**Status:** Closed — `STOP / DEFER`; no AWS measurement.  
**Milestone:** AG-Sept, Iteration C.  
**Budget:** **1.0 development day** transferred from contingency, **0.5 charged and 0.5 returned** ([`ag-sept-plan.md`](ag-sept-plan.md) §2.1.4).  
**Design:** [`../design/independent-capacity-probe.md`](../design/independent-capacity-probe.md).  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).  
**Provisioning evidence:** [`../measurements/pr4c-quota/`](../measurements/pr4c-quota/).

PR4c attempted the smallest useful independently provisioned two-unit probe. Provisioning stopped
because the applied EC2 Standard On-Demand quota was **1 vCPU**, below the probe's **5-vCPU** minimum
(2 + 2 serving + 1 generator). No AWS performance cell ran, so `VAL-SCALE-5` remains unproven.

This is an external provisioning constraint, not capacity evidence. PR4b remains the strongest
executed Iteration C capacity evidence.

## Retained signal

Before the stop, review corrected the probe so each capacity unit keeps the same workload/state
slice when measured alone and when composed. The reusable `P1/P2` method, its distinct workload
identity, generator-host headroom requirement, repeat/interleave rule, and interpretation boundary
are owned by the linked design and validation documents. PR4c implemented none of that deferred
workload because the serving environment never existed.

## Exit

**`STOP / DEFER`.** Proceed to PR5. Any independently provisioned AWS capacity experiment is
post-AG-Sept work requiring a new planning decision and verified provisioning prerequisites.
