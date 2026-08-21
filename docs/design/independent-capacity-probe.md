# Independent capacity-unit probe

**Status:** Deferred — refined during PR4c, not executed in AG-Sept.  
**Scope:** bounded diagnostic for composing independently provisioned shard-group capacity units
without changing each unit's own workload/state envelope.  
**Requirements:** `REQ-SCALE-4`, `REQ-EVID-1`, `REQ-EVID-2` in
[`../requirements/system-requirements.md`](../requirements/system-requirements.md).  
**Validation:** [`../planning/ag-sept/milestone-validation-pr4c-aws-probe.md`](../planning/ag-sept/milestone-validation-pr4c-aws-probe.md).

This document owns the probe-specific architecture. It does not redefine the canonical G1/G2/G4
capacity method or `VAL-SCALE-5`.

## 1. Purpose

PR4b showed that the local shared write path could not provide a stable `G1` denominator. A useful
follow-up therefore needs independently growing serving resource envelopes.

The first bounded probe design introduced a second confound: its one-unit cell carried A/B/C/D while
its composed cell split A/B and C/D across two authorities, halving each authority's working set at
the same time resources were added. The refined probe fixes the workload/state envelope **per unit**.

## 2. Capacity-unit topology

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

A capacity unit is one independently provisioned shard group: one service replica, one PostgreSQL
authority, and its own serving resource/storage envelope. Equivalent units use the same intended
instance/resource shape, service/database configuration, pool policy, and timeout policy.

The generator is measurement infrastructure, not serving capacity. Capacity units should be
non-burstable; any burstable generator must retain enough headroom/credit evidence to rule out
measurement-side throttling.

## 3. Fixed per-unit workload slices

```text
slice U1 = A/B
slice U2 = C/D

P1-CU1        CU1 carries U1
P1-CU2        CU2 carries U2
P2-CU1+CU2    CU1 still carries U1; CU2 still carries U2
```

A/B/C/D are equivalent synthetic populations. The comparison asks whether a unit changes when an
equivalent second unit is present while its own workload/state stays fixed.

This is a weak-scaling/composition diagnostic, not the fixed-total-workload G1/G2/G4 comparison.
The existing `WL-MUT-DISP-4` identity therefore remains unchanged; a future implementation needs a
distinct probe/unit-slice workload identity.

## 4. Evidence conditions

For an individual/composed comparison to be interpretable:

- each unit keeps the same organisations, fixture/conditioning state, service/database shape, pool
  policy, worker count, and measured horizon;
- each unit has separate serving storage/resources;
- pre-measurement state evidence is retained strongly enough to expose a material working-set
  mismatch;
- each unit has an independent worker stream; and
- the shared generator process **and host** have demonstrable CPU/scheduling/memory/network headroom.

These are architectural comparability conditions. The validation document owns the concrete cells,
observations, calculations, repeat/interleave rule, and claim gates used to demonstrate them.

## 5. Validation ownership

Probe outputs and their interpretation are validation method, not system design. The retained
observations, retention-ratio calculations, treatment of run-to-run/provider variation, and the
rule for when a composition claim is admissible are therefore defined only in
[`milestone-validation-pr4c-aws-probe.md`](../planning/ag-sept/milestone-validation-pr4c-aws-probe.md).

## 6. AG-Sept outcome

PR4c did not execute the probe because the required environment could not be provisioned. The
external blocker is retained under [`../measurements/pr4c-quota/`](../measurements/pr4c-quota/).
No AWS capacity evidence exists; any future execution is post-AG-Sept work.
