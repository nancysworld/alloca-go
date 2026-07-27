# Architecture decision records

This directory holds architecture decision records (ADRs): short, immutable notes
capturing a significant decision, why it was made, and what it commits us to.

## Format

Each ADR is a numbered file `NNNN-short-title.md` with:

- **Status** — Proposed / Accepted / Superseded (by `NNNN`).
- **Context** — the forces and constraints in play when the decision was made.
- **Decision** — what was decided, stated plainly.
- **Consequences** — what follows, including what is now harder or deferred.
- **Revisit when** — the concrete evidence or event that should reopen the decision.

ADRs are append-only: to change a decision, add a new ADR that supersedes the old
one rather than rewriting history. Numbers are assigned in order.

## Index

| ADR | Title | Status | Milestone |
|---|---|---|---|
| [0001](0001-modular-monolith-first.md) | Modular monolith first | Accepted | AG-M0 |
| [0002](0002-postgresql-transactional-authority.md) | PostgreSQL as transactional authority | Accepted | AG-M1 |

Planned (not yet written): `0003` capacity-unit selection (AG-M4) — see the
roadmap's planned evidence structure.

## Disclosure

ADRs follow the [public-disclosure policy](../public-disclosure-policy.md): synthetic
examples only, prior-work figures labelled per the
[measurement contract](../design/measurement-contract.md) evidence convention.
