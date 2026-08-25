# Implementation records

This directory holds the durable implementation record for scoped engineering work.

A public implementation record preserves implementation-specific engineering lessons; it does not reconstruct the entire PR.

Requirements own required behaviour. Design documents own accepted system shape and invariants. ADRs own consequential architectural decisions. Validation owns what must be demonstrated. Measurement reports and retained artifacts own empirical results.

An implementation record captures only what remains useful after those owners exist:

- the outcome of a scoped change;
- implementation-specific decisions that are not already documented elsewhere;
- failures or discoveries that changed implementation, architecture or validation;
- evidence boundaries and links to the authoritative documents.

When implementation reveals a durable system property, update the owning requirement, design, ADR or validation document rather than making the implementation record the normative source.

> Plans say what we intend to do. Designs say what the system must preserve. ADRs say why a consequential architectural choice was made. Implementation records say how a scoped change was built and what was learned while building it.

## Naming

Use a stable work-unit name rather than a transient branch name, for example:

```text
ag-sept-pr3.md
ag-sept-pr4.md
```

A single record may span several PRs when they implement one coherent scoped change.

## Records

| Record | Work | State |
|---|---|---|
| [`ag-sept-pr1.md`](ag-sept-pr1.md) | measurement substrate and load harness | merged |
| [`ag-sept-pr2.md`](ag-sept-pr2.md) | single-instance frontier | merged |
| [`ag-sept-pr3.md`](ag-sept-pr3.md) | horizontal database authority Phase 1, across PR3a/3b/3c | merged |
| [`ag-sept-pr4.md`](ag-sept-pr4.md) | Iteration C shard-group capacity method and local execution, across PR4a/PR4b | PR4a/PR4b merged; `G4_local` resolved, G1/G2-derived local efficiencies withheld; `VAL-SCALE-6` not discharged |

PR4c has no implementation record because no AWS cell ran. Its reusable method is retained in the [design](../../design/independent-capacity-probe.md) and [validation plan](../../planning/ag-sept/milestone-validation-pr4c-aws-probe.md); the external blocker is retained under [`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/).

The repository-wide process and role boundaries are defined in [`../engineering-process.md`](../engineering-process.md).
