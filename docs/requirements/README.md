# Requirements

This directory records durable **problems and system requirements**: why something needs to be
solved or proved, and what must be true for the problem to be considered sufficiently resolved.

Requirements are intentionally more stable than milestone plans and less prescriptive than
design. A milestone may change order, budget, topology mechanism, or implementation approach
without changing the underlying requirement.

## What belongs here

A problem belongs here when it is durable enough to motivate one or more system requirements.
For example, a measured scaling limit may create a durable requirement for independent work to
use independent writable resources while preserving correctness.

Temporary implementation defects, debugging notes, and PR-local discoveries stay with the
implementation work unless analysis shows that they expose a missing durable requirement.

The ownership boundary is:

- **problem** — why something durable needs to be solved or proved;
- **requirements** — what must be true, independently of replaceable mechanism;
- **design** — the system shape and contracts chosen to satisfy those requirements;
- **validation plans** — how the requirements and design claims will be proved or falsified;
- **planning** — when work happens, its priority, budget, sequencing, and descope order;
- **implementation records** — what was actually built, discovered, changed, or deferred;
- **measurements** — what experiments established.

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

The current cross-cutting requirement set is
[`system-requirements.md`](system-requirements.md).
