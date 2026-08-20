# AG-Sept PR4c — AWS independent-unit probe

**Status:** Not executed — `STOP / DEFER`.  
**Purpose:** retain the bounded independent-unit validation method; PR4c produced no AWS result.  
**Design:** [`../../design/independent-capacity-probe.md`](../../design/independent-capacity-probe.md).  
**Governing validation:** [`ag-sept-validation-plan.md`](ag-sept-validation-plan.md) §4.6,
`VAL-SCALE-5`, `VAL-NEG-7`, `VAL-NEG-8`.  
**Provisioning evidence:** [`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/).

The probe was not executed because the applied EC2 quota could not provision its 5-vCPU minimum
topology. No partial topology substitutes for the missing independent-resource comparison.

## 1. Question

At one common non-canonical worker level, do two equivalent independently provisioned capacity
units behave materially the same when measured separately and when the **same per-unit workload
slices** are measured together?

This is a composition diagnostic, not a capacity multiplier measurement and not `VAL-SCALE-5`.

## 2. Cells and workload identity

```text
P1-CU1        CU1 -> A/B
P1-CU2        CU2 -> C/D
P2-CU1+CU2    CU1 -> A/B, CU2 -> C/D
```

Each unit keeps its own workload, state, worker count, pool policy, and service/database shape
unchanged between individual and composed cells. A/B/C/D are equivalent synthetic populations.

Because the active participant set changes between P1 and P2, a future implementation must use a
distinct probe/unit-slice workload identity rather than weakening `WL-MUT-DISP-4`.

## 3. Last planned starting configuration

```text
workers_per_group (P)     12
measured window            120 s
pool_max_conns             8 per shard group
conditioning target        4,000 fresh mutations / organisation
fixture                     45,000 slots / organisation, capacity 20
service replicas/group      1
PostgreSQL authorities      1 / active group
```

These are diagnostic starting values, not AWS capacity points.

## 4. Preconditions

No interpreted future probe begins until:

- CU1 and CU2 are equivalent serving instances with separate storage/resource envelopes;
- the generator/measurement stack runs on separate compute;
- service, PostgreSQL/schema, pool, and timeout configuration match across units;
- each unit's individual and composed cells start from the same declared logical state;
- placement and active-unit metadata match the requested topology;
- generator worker streams are independent and the shared generator **process and host** have
  demonstrable headroom; and
- response validation, reconciliation, and final logical-mutation accounting succeed.

Retain enough pre-measurement state/table evidence to expose a material working-set mismatch before
interpreting the comparison.

## 5. Outputs and interpretation

Retain:

```text
p1_CU1_AB
p1_CU2_CD
p2_CU1_AB
p2_CU2_CD
p2_total = p2_CU1_AB + p2_CU2_CD
```

Only when repeat evidence permits interpretation, derive:

```text
R_CU1 = p2_CU1_AB / p1_CU1_AB
R_CU2 = p2_CU2_CD / p1_CU2_CD
R2_probe = p2_total / (p1_CU1_AB + p1_CU2_CD)
```

**One observation per cell is insufficient.** It cannot distinguish a composition effect from
ordinary run-to-run/provider variation. A future execution must either repeat or interleave the
individual and composed cells for at least one unit, or report the raw observations without
retention ratios or a composition claim. PR4b's 25.1% spread across identical G1 runs is the local
counterexample to treating a close pair as evidence of stability.

`R2_probe` is neither `E2_aws` nor comparable with `E2_local`: the local comparison holds total
workload fixed, while this probe adds an equivalent workload/resource unit. None of these quantities
establishes saturation or discharges `VAL-SCALE-5`.

## 6. AG-Sept outcome

No cell ran and no probe quantity was measured or derived. `VAL-SCALE-5` remains **unproven**.

**`STOP / DEFER`.** Any future execution starts from this method, revalidates provisioning
prerequisites, and is planned as post-AG-Sept work.
