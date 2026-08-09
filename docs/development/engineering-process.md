# Engineering process

**Status:** Living

This document owns the engineering process: how work moves from a problem to requirements,
design, validation, implementation, evidence, and a reviewed decision about whether another
iteration is needed. It exists to keep decision ownership clear and reviews at the right
abstraction level without turning those boundaries into rigid permission gates.

The process is tool-assisted, but responsibility remains human: the repository maintainer owns
goals, scope, accepted trade-offs, merge decisions, and what the project ultimately claims,
while participating directly in architecture, implementation reasoning, and review.

## 1. Principles

### 1.1 State the problem before constraining the solution

When a question is genuinely open, describe the problem, invariants, evidence, and constraints
before presenting a closed menu of solutions. A list of plausible implementations can
accidentally foreclose a better architecture that dissolves the trade-off the list assumes.

Options are useful after the solution space is understood; they are not a substitute for
framing the problem.

### 1.2 Keep architecture and implementation at different abstraction levels

Architecture owns durable constraints and system shape. Implementation owns replaceable
mechanics.

A useful test is:

> If an implementation detail can change without reopening the architectural decision, review
> it as implementation, not architecture.

Conversely, an implementation review must escalate when a proposed mechanism would change an
invariant, requirement, authority boundary, trust boundary, evidence contract, or other accepted
architectural property.

### 1.3 Challenge across the boundary; do not silently cross it

An architecture reviewer should challenge implementation where it affects the architectural
contract or where the contract is not implementable as written. An implementation reviewer
should challenge requirements or architecture where feasibility, complexity, or observed
behaviour exposes a flaw in the model.

These are review emphases, not exclusive permissions. A participant may contribute at both
layers. The important rule is that a change crossing a durable boundary is made explicit rather
than hidden inside a local implementation choice.

### 1.4 Work in an engineering iteration loop

Non-trivial work follows an explicit loop:

```text
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ more problem to solve <- Analyse & Review <- Evidence ---+
                                   |
                                   +-> problem sufficiently resolved -> END
```

The stages own different questions:

1. **Problem** — what is unknown, broken, risky, constrained, or worth proving, and why does it
   matter?
2. **Requirements** — what must be true for the problem to be considered sufficiently resolved,
   independent of replaceable mechanism?
3. **Design** — what durable system shape, contract, or decision will satisfy those requirements?
4. **Validation plan** — what tests, experiments, negative controls, faults, or observations can
   prove or falsify the requirements and design claims?
5. **Schedule** — which work happens now, its priority, budget, work-unit/PR split, and descope
   order?
6. **Implement** — build the smallest mechanism that satisfies the accepted contract and makes
   the planned validation possible.
7. **Evidence** — execute the validation and retain results under the repository's evidence
   rules.
8. **Analyse & Review** — interpret the evidence against the problem, requirements, design, and
   validation intent. Decide what was established, what remains uncertain, and whether a new or
   refined problem justifies another iteration.

If the current problem is sufficiently resolved for the agreed scope, the loop ends. Remaining
problems are either outside scope or explicitly deferred. If more work is justified, the next
iteration starts again at **Problem**. Scheduling is therefore not patched directly from raw
evidence: the problem and its durable implications are reconsidered first, then the new schedule
follows from them.

This is an iterative dependency order, not a waterfall and not a documentation quota. A small
change may discharge several stages through existing contracts. A later iteration may reconsider
one layer and leave the others unchanged. For example, evidence may refine the problem while
leaving requirements and design intact but extending the validation plan; stronger evidence may
show that the design itself must change.

The process becomes explicit when work affects an invariant, system requirement, authority or
ownership boundary, failure semantic, deployment boundary, evidence interpretation, or another
durable property.

A durable problem normally lives with the requirements it motivates under `docs/requirements/`.
Temporary implementation defects or investigation notes stay with the implementation work unless
they reveal a missing durable requirement.

The key dependency rule is:

> **Schedule is downstream of durable meaning.** A milestone plan may change repeatedly without
> forcing code, tests, requirements, or architecture to reinterpret what their citations mean.

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

This does **not** make code the authority for requirements or architecture. Requirements, formal
design documents, and ADRs remain authoritative for the durable contracts the implementation
must satisfy; code is expected to realize those contracts.

The project should bias toward a higher code-to-implementation-doc ratio than it has today. That
is a direction, not a numeric metric: remove or avoid prose that merely mirrors code, while
retaining documentation that carries information the code alone cannot preserve clearly.

## 2. Roles and decision ownership

The roles below describe **responsibility and review emphasis, not permissions**. One participant
may occupy several roles at once, and a question does not have to be routed away merely because
it falls outside someone's primary role. Anyone should answer or contribute when the reasoning
is clear; uncertainty, high-impact trade-offs, or boundary-crossing consequences are what trigger
additional review.

| Role | Primary responsibility | Boundary |
|---|---|---|
| **Project owner** | goals, scope, priorities, accepted trade-offs, final decisions, merge and publication; active participation in architecture and implementation reasoning | does not automatically accept reviewer or agent output; seeks additional review when uncertain or when a decision has material architectural or implementation consequences |
| **Architecture / design reviewer** | problem framing, requirements, invariants, authority and trust boundaries, formal design, ADR review, cross-cutting architectural consistency | avoids prescribing replaceable implementation mechanics unless they affect the architectural contract |
| **Implementation agent / reviewer** | concrete implementation, tests, implementation records, operational mechanics, feasibility feedback, local validation | does not silently change accepted requirements or architecture to simplify implementation; raises the conflict instead |
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

For a genuinely unresolved architectural question, the preferred flow inside the larger
iteration loop is:

1. frame the problem and constraints;
2. identify or refine the requirements;
3. draft or revise the formal design or ADR when the decision needs a durable architectural
   home;
4. have implementation challenge feasibility and hidden cost;
5. resolve material disagreement explicitly;
6. implement against the accepted contract.

This is the preferred "reverse review" path when the unresolved question is architectural: the
architecture is made explicit first, then implementation reviews it rather than discovering the
contract implicitly in code.

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

Escalate from implementation to the durable layers when a finding implies any of:

- an accepted requirement or invariant cannot be preserved;
- an authority or ownership boundary must move;
- local atomicity would become distributed coordination;
- a new privileged or trusted component is required;
- evidence would mean something different from what the design claims;
- a supposedly replaceable mechanism has become a system-wide constraint;
- the implementation can satisfy the design only through disproportionate complexity that
  suggests the abstraction itself is wrong.

At that point, state the problem cleanly and reopen the owning requirement or design decision
rather than patching around it.

### 3.4 Ready for implementation

Before a non-trivial architectural work unit begins implementation, four questions should have
answers:

1. **Which requirement is being satisfied or investigated?** If the work is exploratory, state
   the problem and the condition under which the result would create or revise a requirement.
2. **Which design document owns the system shape or contract?** If design is intentionally open,
   state what decision implementation may explore rather than silently settling it in code.
3. **How will the claim be validated?** Name the applicable validation plan, test, experiment,
   negative control, or acceptance/falsification condition.
4. **Where is the work scheduled?** The plan owns priority, budget, work-unit/PR placement, and
   descope order.

This is a lightweight readiness gate, not a template requirement. Existing stable documents may
answer most of it by reference.

If implementation discovers that an answer is wrong, the scheduled scope is not authority. The
finding returns to **Problem** for analysis in the next iteration, reopening requirements, design,
or validation intent as needed before work is rescheduled.

## 4. Review behaviour

### 4.1 Review the owning layer

Before raising a finding, identify which document owns the rule being challenged.

- Requirement contradiction or missing durable obligation -> requirements document.
- Architectural contradiction -> formal design or ADR.
- Validation-strategy defect -> validation plan when scenario-specific; measurement contract
  when the repository-wide evidence rule itself is wrong.
- Implementation defect or ambiguity -> implementation record and code.
- Operating procedure defect -> operations document.
- Measurement interpretation defect -> measurement contract/report.
- Scope, sequencing, budget, or priority problem -> plan or implementation record, depending on
  whether work has started.

A review comment should not demand implementation precision from an ADR, or use an
implementation preference to rewrite an accepted requirement or architectural invariant.

### 4.2 Separate blocker, contract question, and improvement

A review should distinguish:

- **blocker** — the current implementation or evidence cannot satisfy an accepted contract;
- **contract question** — the owning requirement/design is ambiguous or may itself need revision;
- **improvement** — useful hardening that does not invalidate the current contract.

This prevents optional hardening from appearing equivalent to a correctness failure and makes
scope decisions explicit.

### 4.3 Prefer discriminating tests

Where practical, a correctness or certification gate should have a test or controlled mutation
that fails specifically when that property is removed. Passing a broad suite is weaker evidence
than demonstrating that the intended gate notices its own absence.

## 5. Branch and PR conventions

Work happens on a branch named for the **work unit**, not for the change:

```text
agent/<work-unit>          e.g. agent/ag-sept-pr3b
agent/<work-unit>-<aspect> e.g. agent/ag-sept-pr3b-status
```

The work-unit name is the same stable name the implementation record uses
([`implementation/README.md`](implementation/README.md)), so the branch, its record, and its PR
are traceable to one another a year later. A second branch against the same work unit adds a
short suffix naming what it carries rather than inventing a new unit name. The `agent/` prefix
records that the branch carries agent-implemented work submitted for maintainer review; it is a
statement about how the change was produced, not about how much it is trusted, and it does not
shorten the review the change would otherwise get.

A PR opens as a **draft** and stays one until the local gate and the validation the change calls
for have passed and the template's questions are answered; it is then marked ready, and the
maintainer merges. [`../../.github/pull_request_template.md`](../../.github/pull_request_template.md)
owns those questions and the disclosure checklist — they are not restated here.

Branch names are themselves published artifacts. They fall under
[`../public-disclosure-policy.md`](../public-disclosure-policy.md) exactly as file contents,
commit messages, and PR text do.

## 6. Documentation ownership and lifecycle

The repository separates documents by what they own:

| Area | Owns |
|---|---|
| [`../requirements/`](../requirements/) | durable problem statements when they motivate system requirements, and the requirements that state what must be true independently of replaceable mechanism |
| [`../design/`](../design/) | current normative system design and contracts |
| [`../test/validation-plan/`](../test/validation-plan/) | validation intent: scenarios, workloads, negative controls, fault cases, acceptance/falsification conditions, and requirement/design coverage |
| [`../planning/`](../planning/) | forward-looking milestone schedules, budgets, sequencing, priorities, descope order, and intended work-unit scope |
| [`../decisions/`](../decisions/) | significant architectural choices: why and what, not replaceable implementation mechanics |
| [`implementation/`](implementation/) | selective implementation records: rationale, discoveries, review decisions, deferrals, and other context not adequately carried by code and tests |
| [`../operations/`](../operations/) | procedures for building, running, deploying, and operating the system |
| [`../measurements/`](../measurements/) | experiment inputs, artifacts, reports, and measured conclusions |

The distinctions are deliberate:

> **Problem** says why something durable needs to be solved or proved. **Requirements** say what
> must be true. **Design** says how the system is shaped to make that true. **Validation plans**
> say how we intend to prove or falsify the claims. **Plans** say what we choose to do, when, and
> with what priority or budget.

A durable problem normally sits with its requirements. A temporary implementation problem stays
with the current implementation work unless analysis shows that it exposes a missing system
requirement.

An implementation record may begin life as a scope note while work is still being planned. Once
implementation is underway, it becomes the durable record of the work actually performed and
belongs under `docs/development/implementation/`. It should remain selective: code and tests own
the precise mechanics, while the record preserves context that would otherwise be lost.

The AG-Sept per-PR records were migrated from `docs/planning/*-scope.md` when this directory was
created; `docs/planning/` no longer holds implementation history.

The document-owner principle is simple:

> A fact should have one normative home. Other documents link to it rather than maintaining a
> second competing copy.

### 6.1 Durable reference direction

Durable artifacts depend on durable owners for normative meaning.

- Code and tests may cite requirements, design documents, ADRs, and stable invariant identifiers.
- Operations and implementation records may cite those contracts plus code/configuration they
  explain.
- Validation plans cite requirements/design and the measurement contract.
- Milestone plans cite requirements, design, and validation plans when scheduling their work.
- **Code, tests, requirements, and durable design must not depend on a milestone plan for
  normative meaning.** A plan is expected to change and therefore cannot be the stable
  definition of runtime behaviour or evidence validity.

A plan may still be cited for a genuinely historical or scheduling fact, for example why work
moved from one PR to another, but the citation should make that purpose explicit.

Bare section references are safe only when their owning document is unambiguous in context.
Cross-document references should name the document as well as the section. Line-number citations
into living documents are not durable references.

## 7. AI-assisted engineering and public provenance

AI assistance is treated as engineering input, not autonomous authority. Designs, patches,
reviews, and explanations produced with AI tools are subject to the same correctness, evidence,
and review requirements as human-authored work.

Public documentation may name the tools used when that is useful provenance, but durable rules
are written in terms of roles so the process survives tool changes. The human project owner
participates in technical reasoning across layers and remains responsible for accepting
technical decisions and repository claims.

No architectural argument is accepted because of which participant or model proposed it. It is
accepted because the reasoning, implementation, tests, and evidence support it.
