# Engineering process

**Status:** Living

This document owns the engineering process: how a durable goal governs repeated iterations from
problem framing through requirements, design, validation, implementation, evidence, and reviewed
learning. It exists to keep decision ownership clear and reviews at the right abstraction level
without turning those boundaries into rigid permission gates.

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

### 1.4 Put a goal above the engineering iteration loop

Non-trivial exploratory work is governed by a **Goal** and proceeds through repeated engineering
iterations beneath it.

```text
                               GOAL
             What worthwhile outcome are we trying to achieve,
                    and what would make us stop?
                                |
                                v
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ next problem <- Analyse & Review <- Evidence ------------+
                            |             |
                            |             +-- current problem resolved,
                            |                 goal not yet achieved
                            |
                            +-- goal sufficiently achieved -> END
```

The **Goal is outside the loop** because it normally survives several iterations. It states the
worthwhile outcome being pursued, why it matters, and the condition under which the work is
sufficiently complete for the agreed scope. A surprising result may completely change the next
problem without changing the goal.

The loop stages own different questions:

1. **Problem** — what current gap, uncertainty, failure, constraint, or risk prevents sufficient
   progress toward the goal, and why does it matter now?
2. **Requirements** — what must be true for an acceptable resolution of that problem,
   independently of replaceable mechanism?
3. **Design** — what durable system shape, contract, or decision will satisfy those requirements?
4. **Validation plan** — what tests, experiments, negative controls, faults, or observations can
   prove or falsify the requirements and design claims?
5. **Schedule** — which work happens now, its priority, budget, work-unit/PR split, and descope
   order?
6. **Implement** — build the smallest mechanism that satisfies the accepted contract and makes
   the planned validation possible.
7. **Evidence** — execute the validation and retain results under the repository's evidence
   rules.
8. **Analyse & Review** — interpret the evidence against the current problem, requirements,
   design, validation intent, **and the governing goal**. Decide what was established, what
   remains uncertain, how much progress was made toward the goal, and whether another problem is
   worth solving.

A resolved problem does not automatically end the work. If the goal is not yet sufficiently
achieved, Analyse & Review identifies the next most important problem and a new iteration starts
at **Problem**. If the goal is sufficiently achieved for the agreed scope, the loop ends;
remaining problems are outside scope or explicitly deferred.

Replanning is therefore a consequence of a new iteration, not a separate process stage.
Scheduling is not patched directly from raw evidence: evidence is analysed against the goal and
current problem first, durable implications are reconsidered, and only then is new work
scheduled.

This is an iterative dependency order, not a waterfall and not a documentation quota. A small
change may discharge several stages through existing contracts. A later iteration may reconsider
one layer and leave the others unchanged. For example, evidence may refine the problem while
leaving requirements and design intact but extending the validation plan; stronger evidence may
show that the design itself must change.

A goal can itself be revised when evidence or strategy shows that it is no longer worthwhile,
feasible, or correctly scoped, but that is an explicit goal/scope decision rather than an
ordinary consequence of solving one problem.

**Upstream of the loop, a roadmap may collect candidate areas and questions worth future
exploration. It is directional, non-normative, and unscheduled.** Selecting a roadmap item does
not bypass the engineering iteration: worthwhile work is first framed as a Goal or Problem, then
proceeds through Requirements, Design, Validation plan, and Schedule. The relationship also runs
backwards — Analyse & Review may surface a question that is interesting but not worth pursuing
now, and the roadmap is where it belongs, rather than being prematurely promoted into a
requirement or a scheduled work unit. The roadmap is
[`../planning/alloca-go-roadmap.md`](../planning/alloca-go-roadmap.md).

The process becomes explicit when work affects an invariant, system requirement, authority or
ownership boundary, failure semantic, deployment boundary, evidence interpretation, or another
durable property.

A durable engineering goal and the durable problems it generates normally live with the
requirements they govern under `docs/requirements/`. Project-wide intent may already have a
higher-level owner such as the high-level design; link to that owner rather than copying it. The
exploration roadmap is not such an owner — it holds candidate directions, not accepted goals.
Temporary implementation defects or investigation notes stay with the implementation work unless
they reveal a missing durable requirement.

### 1.4.1 Analyse & Review closes an iteration with a durable outcome

**Analyse & Review closes an explicit engineering iteration with a durable outcome.** Its minimum
output is a short **Analyse & Review outcome** in the governing Goal/Problem record, stating:

1. **Problem verdict** — sufficiently resolved, unresolved, or refined;
2. **Evidence** — the retained evidence supporting that verdict;
3. **Durable learning** — requirements/design/validation/ADR implications, including explicitly
   "none" where nothing changes;
4. **Goal progress** — what the result established toward the governing goal, and what remains;
5. **Loop decision** — `END`, or the next/refined problem that starts the next iteration.

Detailed evidence and analysis remain in their owning reports; this is a closure record, not a
second copy of them. Accepted learning is propagated into its durable owners **before** the next
iteration is scheduled — that ordering is the point, because scheduling from unpropagated
evidence is how a durable contract silently falls behind what the project knows.

This is a minimum closure record for work where the iteration loop is explicit. It is not a
documentation quota for trivial changes.

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

Source comments follow the same boundary. Aim for a healthy code-to-comment balance, not a
numeric ratio. Comments should stay local and explain what a reader needs at the code site: a
non-obvious invariant, safety reason, constraint, intent, or why an apparently simpler mechanism
is wrong. When an explanation needs several paragraphs, architectural history, alternatives,
experiment evidence, or broader context, move that material to the owning document and leave a
concise comment and durable reference. Code files should not become parallel documentation files.

### 1.6 Compress documentation to durable signal

Useful working detail is not automatically durable project information. Before a work unit is
considered complete, accumulated or generated prose should be compressed to the minimum record
needed to preserve requirements, decisions, evidence boundaries, reproducibility, and future
engineering understanding.

Compression is deliberately asymmetric across the two documentation layers defined in §6.
**Layer 1** is held to the stricter abstraction boundary: requirements, design, validation intent,
planning, and architectural decisions should preserve durable intent and conclusions without
accumulating replaceable implementation mechanics, command lines, configuration detail,
experiment history, transient debugging discoveries, or other Layer-2 material. A technical fact
does not move into Layer 1 merely because it helped reach a decision; Layer 1 records the durable
conclusion at the appropriate abstraction level and links to the owning evidence or implementation
record where necessary.

**Layer 2** may retain technical detail because implementation understanding, operation, and
evidence sometimes require it, but chronological completeness is not a goal. Keep detail when it
helps a future engineer understand the current implementation, reproduce an operation or result,
understand why a consequential decision was made, or avoid repeating an important failed path.
Ordinary workflow narration, superseded intermediate reasoning, and discoveries that no longer
affect interpretation should be removed rather than preserved as an engineering diary.

Prefer deletion and links to one semantic owner over repeated explanation. Review chronology,
exact timestamps, ordinary workflow narration, duplicated rationale, and intermediate discoveries
are normally transient unless they materially change interpretation or a future decision. If
changing one fact routinely requires edits in many documents, ownership has failed: keep one
normative or empirical owner and have other documents link to it.

AI assistance makes overproduction cheap, so **AI-generated documentation carries a compression
obligation**. The goal is not to preserve everything that was useful during the work; it is to
leave the durable signal. Documentation review should therefore be deletion-biased and reduce
reader/review surface without weakening evidence or traceability.

Compression is part of normal maintenance, not only a publication cleanup. Review the affected
records during PR review and perform a broader compression pass at the end of each development
cycle, when obsolete investigation paths, duplicated rationale, and information that now has a
clearer semantic owner are easiest to identify.

Recording these rules does not expand the work unit that records them. In particular, AG-Sept
PR5 establishes the policy but does **not** require a repository-wide retrofit of existing
documents or comments. The first systematic application to the existing Alloca corpus is the
planned pre-publication documentation pass after PR5 / AG-Sept close. That pass should also
collect real examples from the repository to sharpen this guidance rather than inventing examples
in advance.

## 2. Roles and decision ownership

The roles below describe **responsibility and review emphasis, not permissions**. One participant
may occupy several roles at once, and a question does not have to be routed away merely because
it falls outside someone's primary role. Anyone should answer or contribute when the reasoning
is clear; uncertainty, high-impact trade-offs, or boundary-crossing consequences are what trigger
additional review.

| Role | Primary responsibility | Boundary |
|---|---|---|
| **Project owner** | goals, scope, priorities, accepted trade-offs, final decisions, merge and publication; active participation in architecture and implementation reasoning | does not automatically accept reviewer or agent output; seeks additional review when uncertain or when a decision has material architectural or implementation consequences |
| **Architecture / design reviewer** | goal/problem framing, requirements, invariants, authority and trust boundaries, formal design, ADR review, cross-cutting architectural consistency | avoids prescribing replaceable implementation mechanics unless they affect the architectural contract |
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

1. frame the problem and constraints in the context of the governing goal;
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

Before a non-trivial architectural work unit begins implementation, five questions should have
answers:

1. **Which goal and current problem does this work advance?** The goal should be stable across
   the iteration; the problem states the current gap the work is trying to close.
2. **Which requirement is being satisfied or investigated?** If the work is exploratory, state
   the condition under which the result would create or revise a requirement.
3. **Which design document owns the system shape or contract?** If design is intentionally open,
   state what decision implementation may explore rather than silently settling it in code.
4. **How will the claim be validated?** Name the applicable validation plan, test, experiment,
   negative control, or acceptance/falsification condition.
5. **Where is the work scheduled?** The plan owns priority, budget, work-unit/PR placement, and
   descope order.

This is a lightweight readiness gate, not a template requirement. Existing stable documents may
answer most of it by reference.

If implementation discovers that an answer is wrong, the scheduled scope is not authority. The
finding feeds Evidence and Analyse & Review; if more work is justified, the next iteration starts
at **Problem**, reopening requirements, design, or validation intent as needed before work is
rescheduled.

## 4. Review behaviour

### 4.1 Review the owning layer

Before raising a finding, identify which document owns the rule being challenged.

- Goal/problem contradiction or missing durable obligation -> requirements document or the
  higher-level owner the requirements document links to.
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
| [`../requirements/`](../requirements/) | durable engineering goals, the durable problems that block them, and requirements stating what must be true independently of replaceable mechanism |
| [`../design/`](../design/) | current normative system design and contracts |
| [`../test/validation-plan/`](../test/validation-plan/) | validation intent: scenarios, workloads, negative controls, fault cases, acceptance/falsification conditions, and requirement/design coverage |
| [`../planning/`](../planning/) | forward-looking milestone schedules, budgets, sequencing, priorities, descope order, and intended work-unit scope |
| [`../decisions/`](../decisions/) | significant architectural choices: why and what, not replaceable implementation mechanics |
| [`implementation/`](implementation/) | selective implementation records: rationale, discoveries, review decisions, deferrals, and other context not adequately carried by code and tests |
| [`../operations/`](../operations/) | procedures for building, running, deploying, and operating the system |
| [`../measurements/`](../measurements/) | experiment inputs, artifacts, reports, and measured conclusions |

These areas form two documentation layers with different abstraction responsibilities:

- **Layer 1 — durable intent and truth:** `requirements/`, `design/`, `test/validation-plan/`,
  `planning/`, and `decisions/`. This layer owns what must be true, the durable system shape and
  contracts, why significant architectural choices were made, what validation is intended to
  establish or falsify, and how work is scheduled. Replaceable technical mechanics stay out of
  this layer unless they themselves become a durable contract.
- **Layer 2 — execution and evidence:** `development/implementation/`, `operations/`, and
  `measurements/`. This layer may carry the technical detail needed to explain implementation and
  operation, reproduce experiments, preserve evidence, and retain the reasoning that materially
  contributed to engineering decisions.

The validation boundary is especially deliberate: **validation intent belongs in Layer 1;
validation execution and results belong in Layer 2.** A validation plan may specify scenarios,
workloads, controls, fault cases, and acceptance/falsification conditions; commands, concrete
experiment configuration, dashboards, retained artifacts, observations, and measured results
belong with their Layer-2 owners.

Layering controls where information belongs, not whether it is held to normal engineering
standards. The rules in §7 apply equally to both layers, and the compression discipline in §1.6
still applies to Layer 2 even though more technical detail is legitimate there.

The distinctions are deliberate:

> **Goal** says what worthwhile outcome we are trying to achieve and what would make us stop.
> **Problem** says what current gap prevents sufficient progress toward that goal.
> **Requirements** say what must be true of an acceptable resolution. **Design** says how the
> system is shaped to make that true. **Validation plans** say how we intend to prove or falsify
> the claims. **Plans** say what we choose to do, when, and with what priority or budget.

A durable engineering goal and problem normally sit with the requirements they govern. A
temporary implementation problem stays with the current implementation work unless analysis
shows that it exposes a missing system requirement.

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
- Milestone plans cite goals, requirements, design, and validation plans when scheduling their
  work.
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
