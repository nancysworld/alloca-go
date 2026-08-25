# Implementation records

This directory holds the durable implementation record for scoped engineering work.

## What a public implementation record is for

> **A public implementation record preserves implementation-specific engineering lessons; it does
> not reconstruct the entire PR.**

Requirements own required behaviour. Design documents own accepted system shape and invariants.
ADRs own consequential architectural decisions. Validation owns what must be demonstrated.
Measurement reports and retained artifacts own empirical results. An implementation record keeps
only what remains useful once those owners exist, and normally answers four questions:

1. **What changed?** — the shipped capability or experiment, in a short outcome summary.
2. **Which implementation decisions mattered?** — mechanisms, local trade-offs and rejected
   alternatives that materially affected the result and are not obvious from the durable design.
3. **What went wrong or changed our method?** — failures, bugs, benchmark defects, review findings
   or surprises that changed implementation, architecture or validation.
4. **What did this work establish, and where is the authoritative evidence?** — the evidence
   boundary, unresolved items, and links to the owning requirement, design, ADR, validation item or
   report.

### Keep

The implemented outcome; a technically significant implementation decision or rejected alternative;
a failure or defect that materially changed the design, implementation, benchmark or validation
method; an evidence limitation or unresolved question needed to interpret the work correctly; an
implementation-specific detail still legitimately cited by code, tests, configuration or evidence;
and concise links to the documents that now own the full semantics.

### Remove rather than polish

Obsolete forward-looking scope; task breakdowns, checklists and day-by-day chronology; requirement,
design, ADR or validation explanations already owned elsewhere; duplicated result tables already
owned by measurement reports and artifacts; repeated ownership and process explanation; review and
handoff conversation; estimates that never became measured results; and personal-name attribution
where the role is sufficient.

Do not preserve a paragraph merely because it is technically correct. **If the same truth has a
better durable owner, link to that owner and delete the duplicate prose.**

### Section anchors are a compatibility surface

Code, tests, configuration and evidence directories cite these records by section number. Preserve
a cited heading as a compact named subsection rather than recreating the chronology around it, and
repoint a citation to a durable owner only as a deliberate change with its references repaired in
the same batch. **Never renumber an existing section.**

## Relationship to plans, designs and ADRs

An implementation record does **not** become the normative home for architecture merely because
implementation exposed the issue. When a finding changes an invariant, authority boundary, trust
boundary, evidence contract or other durable system property, update the owning design or ADR and
link back to it.

> Plans say what we intend to do. Designs say what the system must preserve. ADRs say why a
> consequential architectural choice was made. Implementation records say how a scoped change was
> actually built and what was learned while building it.

## Naming

Use a stable work-unit name rather than a transient branch name, for example `ag-sept-pr3.md`. A
single record may span several PRs when they implement one coherent scoped change.

## Records

| Record | Work | State |
|---|---|---|
| [`ag-sept-pr1.md`](ag-sept-pr1.md) | measurement substrate and load harness | merged |
| [`ag-sept-pr2.md`](ag-sept-pr2.md) | single-instance frontier | merged |
| [`ag-sept-pr3.md`](ag-sept-pr3.md) | horizontal database authority Phase 1, across PR3a/3b/3c | merged |
| [`ag-sept-pr4.md`](ag-sept-pr4.md) | Iteration C shard-group capacity method and local execution, across PR4a/PR4b | PR4a/PR4b merged; `G4_local` resolved, G1/G2-derived local efficiencies withheld; `VAL-SCALE-6` not discharged |

PR4c has no implementation record because no AWS cell ran. Its reusable method is retained in the
[design](../../design/independent-capacity-probe.md) and
[validation plan](../../planning/ag-sept/milestone-validation-pr4c-aws-probe.md); the external blocker is
retained under [`../../measurements/pr4c-quota/`](../../measurements/pr4c-quota/).

The repository-wide process and role boundaries are defined in
[`../engineering-process.md`](../engineering-process.md).
