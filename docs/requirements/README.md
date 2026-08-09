# Requirements

This directory records durable **engineering goals, problems, and system requirements**: what
worthwhile outcome is being pursued, why something currently needs to be solved or proved, and
what must be true for an acceptable resolution.

These are intentionally more stable than milestone plans and less prescriptive than design. A
milestone may change order, budget, topology mechanism, or implementation approach without
changing the governing goal or underlying requirement.

## What belongs here

A **goal** belongs here when it is a durable engineering outcome that should survive replanning
and govern several problem-solving iterations. It states what outcome is worth achieving, why it
matters, and what would make the work sufficiently complete for the agreed scope.

Project-wide intent may already have a higher-level owner such as
[`../design/high-level-design.md`](../design/high-level-design.md) or the roadmap. In that case,
link to the existing owner rather than duplicating it here.

A **problem** belongs here when it is durable enough to motivate one or more system requirements
or a new iteration toward the governing goal. For example, a measured scaling limit may create a
new problem around independent writable resources while the goal remains unchanged.

Temporary implementation defects, debugging notes, and PR-local discoveries stay with the
implementation work unless analysis shows that they expose a missing durable requirement.

The ownership boundary is:

- **goal** — what worthwhile outcome are we trying to achieve, and what would make us stop;
- **problem** — what current gap, uncertainty, failure, constraint, or risk blocks sufficient
  progress toward that goal;
- **requirements** — what must be true, independently of replaceable mechanism;
- **design** — the system shape and contracts chosen to satisfy those requirements;
- **validation plans** — how the requirements and design claims will be proved or falsified;
- **planning** — when work happens, its priority, budget, sequencing, and descope order;
- **implementation records** — what was actually built, discovered, changed, or deferred;
- **measurements** — what experiments established.

A goal normally spans several iterations. Resolving one problem does not end the work if Analyse
& Review concludes that the goal is not yet sufficiently achieved; it identifies the next
problem instead.

## Requirements and invariants

A system requirement and an invariant are related but not interchangeable.

A **requirement** is a durable capability, constraint, or quality the system must satisfy. An
**invariant** is a stronger statement: a property that must remain true across every valid state
or execution covered by its scope.

One system requirement may therefore be satisfied by several invariants together. This
directory does **not** mirror the existing `INV-*` register into a second `REQ-*` list.

Detailed transactional invariants remain owned by
[`../design/transaction-semantics.md`](../design/transaction-semantics.md). Requirements refer
to those stable identifiers where appropriate.

## Stable identifiers

System-level requirements use `REQ-*` identifiers so designs, validation plans, implementation
records, and reviews can refer to them without depending on mutable milestone-plan section
numbers.

Goals and problems do not need identifiers by default; name them clearly in the document whose
scope they govern. Add an identifier only if repeated cross-document citation makes one useful.

The current cross-cutting requirement set is
[`system-requirements.md`](system-requirements.md). AG-Sept's governing goal and iteration
problems are recorded in [`ag-sept.md`](ag-sept.md).
