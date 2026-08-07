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

## Existing scope records

Some implementation records currently live under `docs/planning/` with names such as
`*-scope.md`. They predate this directory convention. Migrate them when they are next materially
updated; do not create path churn in an active review solely for classification.

The repository-wide process and role boundaries are defined in
[`../engineering-process.md`](../engineering-process.md).
