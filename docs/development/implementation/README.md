# Implementation records

This directory holds the durable implementation record for scoped engineering work.

An implementation record begins from intended scope, then evolves with the work. Unlike a
forward-looking plan, it may record:

- the concrete mechanism selected to satisfy an accepted design;
- implementation-specific trade-offs and rejected local alternatives;
- discoveries made while coding or testing;
- review findings and how they were resolved or deliberately deferred;
- discriminating tests and validation performed;
- scope movement between PRs;
- what actually shipped and what remains open.

It does **not** become the normative home for architecture merely because implementation exposed
the issue. When a finding changes an invariant, authority boundary, trust boundary, evidence
contract, or other durable system property, update the owning design or ADR and link back to it.

The boundary is:

> Plans say what we intend to do. Designs say what the system must preserve. ADRs say why a
> consequential architectural choice was made. Implementation records say how a scoped change
> was actually built and what was learned while building it.

## Naming

Use a stable work-unit name rather than a transient branch name, for example:

```text
ag-sept-pr3.md
ag-sept-pr4.md
```

A single implementation record may span several PRs when they implement one coherent scoped
change and the document benefits from preserving their shared history.

## Records

| Record | Work | State |
|---|---|---|
| [`ag-sept-pr1.md`](ag-sept-pr1.md) | measurement substrate and load harness | merged |
| [`ag-sept-pr2.md`](ag-sept-pr2.md) | single-instance frontier | merged |
| [`ag-sept-pr3.md`](ag-sept-pr3.md) | horizontal database authority Phase 1, across PR3a/3b/3c | merged |
| [`ag-sept-pr4.md`](ag-sept-pr4.md) | Iteration C shard-group capacity method and local execution, across PR4a/PR4b | PR4a/PR4b merged; `G4_local` resolved, G1/G2-derived local efficiencies withheld; `VAL-SCALE-6` not discharged |

PR4c has no implementation record because no AWS cell ran. Its reusable method is retained in the
[design](../../design/independent-capacity-probe.md) and
[validation plan](../../test/validation-plan/ag-sept-pr4c-aws-probe.md); the external blocker is
retained under [`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/).

The first three began as `docs/planning/ag-sept-pr*-scope.md` and moved here once the directory
existed. They are named for the work unit, not the branch, and PR3's and PR4's records each span
several PRs because they implement one coherent scoped change.

Those three still read as scope notes in places, because that is what they were written as. Sections
that state intent rather than what was built are the parts to revise as each is next materially
updated — not a reason to rewrite their history.

The repository-wide process and role boundaries are defined in
[`../engineering-process.md`](../engineering-process.md).
