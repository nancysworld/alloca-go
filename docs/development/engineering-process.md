# Engineering process

**Status:** Living

This document records how engineering work moves from an open question to an architectural
decision, an implementation, and evidence. It exists to keep decision ownership clear and to
keep reviews at the right abstraction level without turning those boundaries into rigid
permission gates.

The process is tool-assisted, but responsibility remains human: the repository maintainer owns
goals, scope, accepted trade-offs, merge decisions, and what the project ultimately claims,
while participating directly in architecture, implementation reasoning, and review.

## 1. Principles

### 1.1 State the problem before constraining the solution

When a design question is genuinely open, describe the problem, invariants, evidence, and
constraints before presenting a closed menu of solutions. A list of plausible implementations
can accidentally foreclose a better architecture that dissolves the trade-off the list assumes.

Options are useful after the solution space is understood; they are not a substitute for
framing the problem.

### 1.2 Keep architecture and implementation at different abstraction levels

Architecture owns durable constraints and system shape. Implementation owns replaceable
mechanics.

A useful test is:

> If an implementation detail can change without reopening the architectural decision, review
> it as implementation, not architecture.

Conversely, an implementation review must escalate when a proposed mechanism would change an
invariant, authority boundary, trust boundary, evidence contract, or other accepted architectural
property.

### 1.3 Challenge across the boundary; do not silently cross it

An architecture reviewer should challenge implementation where it affects the architectural
contract or where the contract is not implementable as written. An implementation reviewer
should challenge architecture where feasibility, complexity, or observed behaviour exposes a
flaw in the model.

These are review emphases, not exclusive permissions. A participant may contribute at both
layers. The important rule is that a change crossing the architecture/implementation boundary
is made explicit rather than hidden inside a local implementation choice.

### 1.4 Evidence closes the loop

Measured or observed behaviour may invalidate an assumption in a plan or design. When that
happens, update the owning architectural or planning document deliberately, then adapt the
implementation. Do not preserve a stale plan merely because it was agreed earlier.

The project's evidence labels and quotability rules remain owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md).

### 1.5 Prefer executable implementation truth over duplicated prose

For current **implementation mechanics**, code and tests are the authoritative source. Do not
maintain a second prose version of behaviour that can already be read precisely from the code.
Detailed parallel documentation is expensive to keep synchronized and becomes misleading when
implementation changes faster than prose.

Implementation documentation should therefore explain what the code cannot explain well on its
own: rationale, non-obvious constraints, rejected approaches that matter for maintenance,
important discoveries, cross-component interactions, operational consequences, and deliberate
deferrals. It should help a reader understand the code, not restate it function by function.

This does **not** make code the authority for architecture. Formal design documents and ADRs
remain authoritative for the durable contracts the implementation must satisfy; code is
expected to realize those contracts.

The project should bias toward a higher code-to-implementation-doc ratio than it has today.
That is a direction, not a numeric metric: remove or avoid prose that merely mirrors code, while
retaining documentation that carries information the code alone cannot preserve clearly.

## 2. Roles and decision ownership

The roles below describe **responsibility and review emphasis, not permissions**. One participant
may occupy several roles at once, and a question does not have to be routed away merely because
it falls outside someone's primary role. Anyone should answer or contribute when the reasoning
is clear; uncertainty, high-impact trade-offs, or boundary-crossing consequences are what
trigger additional review.

| Role | Primary responsibility | Boundary |
|---|---|---|
| **Project owner** | goals, scope, priorities, accepted trade-offs, final decisions, merge and publication; active participation in architecture and implementation reasoning | does not automatically accept reviewer or agent output; seeks additional review when uncertain or when a decision has material architectural or implementation consequences |
| **Architecture / design reviewer** | problem framing, invariants, authority and trust boundaries, formal design, ADR review, cross-cutting architectural consistency | avoids prescribing replaceable implementation mechanics unless they affect the architectural contract |
| **Implementation agent / reviewer** | concrete implementation, tests, implementation records, operational mechanics, feasibility feedback, local validation | does not silently change accepted architecture to simplify implementation; raises the conflict instead |
| **Independent reviewer** | adversarial checks of code, tests, contracts, and evidence; finding assumptions the primary path missed | does not own project scope or make unilateral architectural changes |

Roles overlap deliberately. In particular, the project owner may act as a **co-architecture /
design reviewer** and a **co-implementation reviewer**: answering design or implementation
questions directly when confident, challenging proposals, and helping choose mechanisms and
trade-offs. The purpose of specialist reviewers is to add depth and independent challenge, not
to prevent that participation.

### 2.1 Current AG-Sept working mapping

For transparency, the current working arrangement is:

| Role | Current participant / tool |
|---|---|
| Project owner + co-architecture/design reviewer + co-implementation reviewer | repository maintainer |
| Architecture / design reviewer | ChatGPT |
| Implementation agent / reviewer | Claude |
| Additional independent review | Codex when used |

This mapping is descriptive, not architectural. It records how the work is currently shared;
changing tools or redistributing responsibilities does not require changing system design.

## 3. Working boundary between architecture and implementation

### 3.1 Architecture questions

Examples include:

- which component is authoritative for a correctness decision;
- whether two states must commit atomically;
- what a database or service boundary means;
- which invariants a scale-out design must preserve;
- what evidence is required before a claim is admissible;
- where a trust or provenance boundary belongs.

For a genuinely unresolved architectural question, the preferred flow is:

1. frame the problem and constraints;
2. let participants contribute directly where they have a clear answer;
3. draft or revise the formal design or ADR when the decision needs a durable architectural home;
4. have implementation challenge feasibility and hidden cost;
5. resolve material disagreement explicitly;
6. implement against the accepted contract.

This is the preferred "reverse review" path when the unresolved question is architectural:
the architecture is made explicit first, then implementation reviews it rather than discovering
the contract implicitly in code.

The flow is not a requirement to escalate every architecture-related question. Routine questions
can be answered directly. Escalation is useful when the answer is uncertain, changes a durable
contract, or would materially constrain later implementation.

### 3.2 Implementation questions

Examples include:

- data structures and file schemas;
- package and function shape;
- CLI flags and scripts;
- test fixtures and fault-injection mechanics;
- exact preflight sequencing;
- how an accepted invariant is enforced in the current code.

Implementation decisions may be made collaboratively. The implementation agent normally turns
the chosen mechanism into code, tests, and only the implementation documentation that adds
information beyond the code itself, while the project owner and reviewers may propose, answer,
challenge, or refine those choices directly. Architecture review should focus on whether the
mechanism satisfies the contract, not on prescribing an equivalent local implementation
unnecessarily.

### 3.3 When an implementation issue becomes architectural

Escalate from implementation to architecture when a finding implies any of:

- an accepted invariant cannot be preserved;
- an authority or ownership boundary must move;
- local atomicity would become distributed coordination;
- a new privileged or trusted component is required;
- evidence would mean something different from what the design claims;
- a supposedly replaceable mechanism has become a system-wide constraint;
- the implementation can satisfy the design only through disproportionate complexity that
  suggests the abstraction itself is wrong.

At that point, state the problem cleanly and reopen the owning design decision rather than
patching around it.

## 4. Review behaviour

### 4.1 Review the owning layer

Before raising a finding, identify which document owns the rule being challenged.

- Architectural contradiction -> formal design or ADR.
- Implementation defect or ambiguity -> implementation record and code.
- Operating procedure defect -> operations document.
- Measurement interpretation defect -> measurement contract/report.
- Scope or sequencing problem -> plan or implementation record, depending on whether work has
  started.

A review comment should not demand implementation precision from an ADR, or use an
implementation preference to rewrite an accepted architectural invariant.

### 4.2 Separate blocker, contract question, and improvement

A review should distinguish:

- **blocker** — the current implementation or evidence cannot satisfy an accepted contract;
- **contract question** — the owning design is ambiguous or may itself need revision;
- **improvement** — useful hardening that does not invalidate the current contract.

This prevents optional hardening from appearing equivalent to a correctness failure and makes
scope decisions explicit.

### 4.3 Prefer discriminating tests

Where practical, a correctness or certification gate should have a test or controlled mutation
that fails specifically when that property is removed. Passing a broad suite is weaker evidence
than demonstrating that the intended gate notices its own absence.

## 5. Documentation ownership and lifecycle

The repository separates documents by what they own:

| Area | Owns |
|---|---|
| [`../planning/`](../planning/) | forward-looking milestone plans, budgets, sequencing, and intended scope |
| [`../design/`](../design/) | current normative system design and contracts |
| [`../decisions/`](../decisions/) | significant architectural choices: why and what, not replaceable implementation mechanics |
| [`implementation/`](implementation/) | selective implementation records: rationale, discoveries, review decisions, deferrals, and other context that is not adequately carried by code and tests |
| [`../operations/`](../operations/) | procedures for building, running, deploying, and operating the system |
| [`../measurements/`](../measurements/) | experiment inputs, artifacts, reports, and measured conclusions |

An implementation record may begin life as a scope note while work is still being planned. Once
implementation is underway, it becomes the durable record of the work actually performed and
belongs under `docs/development/implementation/`. It should remain selective: the code and tests
own the precise mechanics, while the record preserves context that would otherwise be lost.

The AG-Sept per-PR records were migrated from `docs/planning/*-scope.md` when this directory was
created; `docs/planning/` no longer holds implementation history.

The document owner principle is simple:

> A fact should have one normative home. Other documents link to it rather than maintaining a
> second competing copy.

## 6. AI-assisted engineering and public provenance

AI assistance is treated as engineering input, not autonomous authority. Designs, patches,
reviews, and explanations produced with AI tools are subject to the same correctness,
evidence, and review requirements as human-authored work.

Public documentation may name the tools used when that is useful provenance, but durable rules
are written in terms of roles so the process survives tool changes. The human project owner
participates in technical reasoning across layers and remains responsible for accepting
technical decisions and repository claims.

No architectural argument is accepted because of which participant or model proposed it. It is
accepted because the reasoning, implementation, tests, and evidence support it.
