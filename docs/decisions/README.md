# Architecture decision records

This directory holds architecture decision records (ADRs): short notes that preserve a
significant architectural choice, the forces behind it, the trade-offs it accepts, and the
conditions that should cause it to be reconsidered.

An ADR records **why and what**, not the detailed **how** of one implementation.

A useful test is:

> If a detail can change without reopening the architectural decision, it does not belong
> in the ADR.

Implementation mechanics belong in the
[`../development/implementation/`](../development/implementation/) record, operating procedures,
or code documentation. An ADR may link to those documents, but should not duplicate their
file formats, flags, scripts, internal APIs, test fixtures, or current PR staging unless one
of those facts is itself the architectural decision.

## Format

Each ADR is a numbered file `NNNN-short-title.md` with:

- **Status** — Proposed / Accepted / Superseded (by `NNNN`).
- **Context** — the durable forces and constraints that make a decision necessary.
- **Decision** — the architectural choice, stated plainly and independently of incidental
  implementation details.
- **Consequences** — what the choice enables, costs, constrains, or deliberately leaves open.
- **Rejected alternatives** — optional; include only alternatives whose rejection explains
  the decision materially.
- **Revisit when** — concrete evidence or changed conditions that should reopen the decision.

A good ADR should still make sense after implementation details, package names, deployment
scripts, or milestone boundaries have changed.

## Scope boundary

ADRs should normally contain:

- authority, ownership, consistency, deployment, trust, or decomposition choices that shape
  more than one implementation component;
- technology choices when choosing that technology is itself architecturally consequential;
- important trade-offs and rejected architectural alternatives;
- invariants or constraints that future implementations must preserve;
- explicit triggers for reconsidering the decision.

ADRs should normally not contain:

- command-line flags, JSON/file schemas, shell commands, package/function names, or script
  paths except as links to the current implementation record;
- exact sequencing of one PR's workflow unless the ordering is itself an architectural
  invariant;
- test cases, fixtures, current topology counts, or temporary milestone mechanics;
- debugging history or implementation archaeology that explains how a defect was found;
- details that can be replaced locally while the architectural decision remains unchanged.

The owning implementation document should explain how the current code realizes the ADR and
carry the detailed review/discovery history needed to maintain it. The repository-wide boundary
between architecture, implementation, evidence, and review roles is defined in
[`../development/engineering-process.md`](../development/engineering-process.md).

## Lifecycle

A **Proposed** ADR is a design under review. It may be revised freely while the architectural
choice is being clarified and while implementation is testing whether the decision is viable.

An **Accepted** ADR is a historical record of a decision the project has adopted. Accepted
ADRs are append-only in substance: if the architecture later changes, add a new ADR that
supersedes the old one rather than rewriting the old decision as though history had always
been different.

Minor corrections that do not change an accepted decision's meaning — spelling, broken links,
or equivalent editorial fixes — are fine.

Numbers are assigned in order when an ADR is created; they are not reserved in advance.

## Index

| ADR | Title | Status | Milestone |
|---|---|---|---|
| [0001](0001-modular-monolith-first.md) | Modular monolith first | Accepted | AG-M0 |
| [0002](0002-postgresql-transactional-authority.md) | PostgreSQL as transactional authority | Accepted | AG-M1 |
| [0003](0003-deployed-artifact-identity.md) | A run identifies its deployed artifact by observation, not by self-report | Proposed | AG-Sept |

Planned (not yet written): `0004` capacity-unit selection (AG-M4) — see the
roadmap's planned evidence structure. It was pencilled in as `0003` before that
number was taken; numbers are assigned when an ADR is written, not reserved.

## Disclosure

ADRs follow the [public-disclosure policy](../public-disclosure-policy.md): synthetic
examples only, prior-work figures labelled per the
[measurement contract](../design/measurement-contract.md) evidence convention.
