# Development documentation

This directory records **how Alloca-Go is developed**: the engineering process that governs
iterations and the implementation records that explain how scoped work was actually built.

It is deliberately separate from requirements, architecture, validation planning, milestone
scheduling, and measurement evidence.

## What lives here

- [`engineering-process.md`](engineering-process.md) — the engineering iteration loop, roles and
  decision ownership, architecture/implementation boundaries, readiness and review rules, branch
  and PR conventions, documentation ownership, and the AI-assisted collaboration model.
- [`implementation/`](implementation/) — durable implementation records: scope as realised,
  concrete mechanisms, discoveries, review findings, deferrals, validation performed, and what
  finally shipped.

## Boundary with the rest of `docs/`

Use the document whose purpose matches the question:

| Question | Home |
|---|---|
| What durable problem are we solving, and what must be true? | [`../requirements/`](../requirements/) |
| What system shape or contract satisfies it? | [`../design/`](../design/) |
| How will we prove or falsify the requirement/design claims? | [`../test/validation-plan/`](../test/validation-plan/) |
| What do we intend to do now, and in what order/budget? | [`../planning/`](../planning/) |
| Why was a consequential architectural choice made? | [`../decisions/`](../decisions/) |
| How was a scoped change actually implemented, reviewed, and refined? | [`implementation/`](implementation/) |
| How should the system be run or operated? | [`../operations/`](../operations/) |
| What did an experiment establish? | [`../measurements/`](../measurements/) |

A document may begin life as planning and later become an implementation record as work
progresses. When that happens, move it here rather than allowing `planning/` to become the
permanent home for implementation history.

The key boundaries are:

> Requirements define what must be true. Architecture defines durable contracts.
> Implementation records explain how the current code realises them and what was learned while
> doing so.

Implementation details should not leak into requirements, ADRs, or formal design unless changing
that detail would change the durable contract itself.
