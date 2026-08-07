# Development documentation

This directory records **how Alloca-Go is developed**: the engineering process that governs
changes and the implementation records that explain how scoped work was actually built.

It is deliberately separate from architecture and planning.

## What lives here

- [`engineering-process.md`](engineering-process.md) — development and review roles,
  architecture/implementation boundaries, problem-first review, escalation rules, and the
  current AI-assisted collaboration model.
- [`implementation/`](implementation/) — durable implementation records: scope as realised,
  concrete mechanisms, discoveries, review findings, deferrals, validation, and what finally
  shipped.

## Boundary with the rest of `docs/`

Use the document whose purpose matches the question:

| Question | Home |
|---|---|
| What do we intend to do? | [`../planning/`](../planning/) |
| What must the system preserve? | [`../design/`](../design/) |
| Why was a consequential architectural choice made? | [`../decisions/`](../decisions/) |
| How was a scoped change actually implemented, reviewed, and refined? | [`implementation/`](implementation/) |
| How should the system be run or operated? | [`../operations/`](../operations/) |
| What did an experiment establish? | [`../measurements/`](../measurements/) |

A document may begin life as planning and later become an implementation record as work
progresses. When that happens, move it here rather than allowing `planning/` to become the
permanent home for implementation history.

The key boundary is:

> Architecture defines durable contracts. Implementation records explain how the current code
> realises them.

Implementation details should not leak into ADRs or formal design unless changing that detail
would change the architectural contract itself.
