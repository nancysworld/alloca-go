# Engineering process

**Status:** Living

This document records how engineering work moves from an open question to an architectural
decision, an implementation, and evidence. It exists to keep decision ownership clear and to
keep reviews at the right abstraction level.

The process is tool-assisted, but responsibility remains human: the repository maintainer owns
goals, scope, accepted trade-offs, merge decisions, and what the project ultimately claims.

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

An architecture reviewer should challenge implementation only where it affects the architectural
contract or where the contract is not implementable as written. An implementation reviewer
should challenge architecture where feasibility, complexity, or observed behaviour exposes a
flaw in the model.

Neither role should quietly rewrite the other layer to make a local review easier.

### 1.4 Evidence closes the loop

Measured or observed behaviour may invalidate an assumption in a plan or design. When that
happens, update the owning architectural or planning document deliberately, then adapt the
implementation. Do not preserve a stale plan merely because it was agreed earlier.

The project's evidence labels and quotability rules remain owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md).

## 2. Roles and decision ownership

The roles below describe responsibilities, not particular products. Tools may change without
changing the process.

| Role | Owns | Does not own |
|---|---|---|
| **Project owner** | goals, scope, priorities, accepted trade-offs, final decisions, merge and publication | automatic acceptance of any reviewer or agent output |
| **Architecture / design reviewer** | problem framing, invariants, authority and trust boundaries, formal design, ADR review, cross-cutting architectural consistency | replaceable implementation mechanics unless they affect the architectural contract |
| **Implementation agent / reviewer** | concrete implementation, tests, implementation records, operational mechanics, feasibility feedback, local validation | silently changing accepted architecture to simplify implementation |
| **Independent reviewer** | adversarial checks of code, tests, contracts, and evidence; finding assumptions the primary implementation path missed | project ownership or unilateral scope changes |

### 2.1 Current AG-Sept working mapping

For transparency, the current tool mapping is:

| Role | Current tool / owner |
|---|---|
| Project owner | repository maintainer |
| Architecture / design reviewer | ChatGPT |
| Implementation agent / reviewer | Claude |
| Additional independent review | Codex when used |

This mapping is descriptive, not architectural. Changing tools does not require changing any
system design.

## 3. Working boundary between architecture and implementation

### 3.1 Architecture questions

Examples include:

- which component is authoritative for a correctness decision;
- whether two states must commit atomically;
- what a database or service boundary means;
- which invariants a scale-out design must preserve;
- what evidence is required before a claim is admissible;
- where a trust or provenance boundary belongs.

The preferred flow is:

1. frame the problem and constraints;
2. draft or revise the formal design or ADR;
3. have the implementation role challenge feasibility and hidden cost;
4. resolve architectural disagreements explicitly;
5. implement against the accepted contract.

This is the preferred "reverse review" path when the unresolved question is architectural:
the architecture is made explicit first, then implementation reviews it rather than discovering
the contract implicitly in code.

### 3.2 Implementation questions

Examples include:

- data structures and file schemas;
- package and function shape;
- CLI flags and scripts;
- test fixtures and fault-injection mechanics;
- exact preflight sequencing;
- how an accepted invariant is enforced in the current code.

The implementation role should choose these mechanisms and record why where maintenance needs
that history. Architecture review should focus on whether the mechanism satisfies the contract,
not on prescribing an equivalent local implementation unnecessarily.

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
| [`implementation/`](implementation/) | evolving implementation records: how a scoped change was built, discoveries during implementation, review decisions, deferrals, and what actually shipped |
| [`../operations/`](../operations/) | procedures for building, running, deploying, and operating the system |
| [`../measurements/`](../measurements/) | experiment inputs, artifacts, reports, and measured conclusions |

An implementation record may begin life as a scope note while work is still being planned. Once
implementation is underway, it becomes the durable record of the work actually performed and
belongs under `docs/development/implementation/`.

Existing `docs/planning/*-scope.md` files predate this convention. They may be migrated when
next materially touched; pure path churn is not required in an active review merely to satisfy
the directory convention.

The document owner principle is simple:

> A fact should have one normative home. Other documents link to it rather than maintaining a
> second competing copy.

## 6. AI-assisted engineering and public provenance

AI assistance is treated as engineering input, not autonomous authority. Designs, patches,
reviews, and explanations produced with AI tools are subject to the same correctness,
evidence, and review requirements as human-authored work.

Public documentation may name the tools used when that is useful provenance, but durable rules
are written in terms of roles so the process survives tool changes. The human project owner is
responsible for accepting technical decisions and repository claims.

No architectural argument is accepted because of which model proposed it. It is accepted
because the reasoning, implementation, tests, and evidence support it.
