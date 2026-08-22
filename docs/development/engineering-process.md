# Engineering process

**Status:** Living

This document owns the engineering process: how a durable goal governs repeated iterations from
problem framing through requirements, design, validation, implementation, evidence, and reviewed
learning. It keeps decision ownership clear and reviews at the right abstraction level without
turning those boundaries into rigid permission gates.

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

Architecture review should challenge implementation where it affects the architectural contract
or exposes that the contract is not implementable as written. Implementation review should
challenge architecture where feasibility, complexity, or observed behaviour exposes a flaw in
the model.

These are review emphases, not exclusive permissions. A participant may contribute at both
levels. The important rule is that a change crossing an architectural boundary is made explicit
rather than hidden inside a local implementation choice.

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

The **Goal is outside the loop** because it normally survives several iterations. A surprising
result may completely change the next problem without changing the goal.

The loop stages own different questions:

1. **Problem** — what current gap, uncertainty, failure, constraint, or risk prevents sufficient
   progress toward the goal, and why does it matter now?
2. **Requirements** — what must be true for an acceptable resolution, independently of
   replaceable mechanism?
3. **Design** — what system shape, contract, or architectural decision will satisfy those
   requirements?
4. **Validation plan** — what tests, experiments, controls, faults, or observations can prove or
   falsify the requirements and design claims?
5. **Schedule** — which work happens now, its priority, budget, work-unit/PR split, and descope
   order?
6. **Implement** — build the smallest mechanism that satisfies the accepted contract and makes
   the planned validation possible.
7. **Evidence** — execute the validation and retain results under the repository's evidence
   rules.
8. **Analyse & Review** — interpret the evidence against the current problem, requirements,
   design, validation intent, and governing goal; decide what was established, what remains
   uncertain, and whether another problem is worth solving.

A resolved problem does not automatically end the work. If the goal is not sufficiently
achieved, Analyse & Review identifies the next problem and a new iteration starts at **Problem**.
If the goal is sufficiently achieved for the agreed scope, the loop ends; remaining problems are
outside scope or explicitly deferred.

This is an iterative dependency order, not a waterfall or documentation quota. A later iteration
may reconsider one stage and leave the others unchanged. Evidence may also justify revising the
goal, but that is an explicit goal/scope decision rather than an ordinary consequence of solving
one problem.

A roadmap sits upstream of this loop. It may collect candidate areas and questions for future
exploration, but it is directional, non-normative, and unscheduled. Selecting a roadmap item does
not bypass the engineering iteration; Analyse & Review may also return an interesting but
unscheduled question to the roadmap. The roadmap is
[`../planning/alloca-go-roadmap.md`](../planning/alloca-go-roadmap.md).

### 1.4.1 Analyse & Review closes an iteration with a durable outcome

An explicit engineering iteration closes with a short **Analyse & Review outcome** in the
governing Goal/Problem record:

1. **Problem verdict** — sufficiently resolved, unresolved, or refined;
2. **Evidence** — retained evidence supporting that verdict;
3. **Durable learning** — requirements/design/validation/ADR implications, including explicitly
   "none" where nothing changes;
4. **Goal progress** — what the result established toward the goal and what remains;
5. **Loop decision** — `END`, or the next/refined problem.

Detailed evidence and analysis remain in their owning reports. Accepted learning is propagated
into its owning requirements, design, validation, or decision record **before** the next
iteration is scheduled.

The key dependency rule is:

> **Schedule is downstream of durable meaning.** A milestone plan may change repeatedly without
> forcing code, tests, requirements, or architecture to reinterpret what their citations mean.

The project's evidence labels and quotability rules remain owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md).

### 1.5 Prefer executable implementation truth over duplicated prose

For current implementation mechanics, code and tests are authoritative. Do not maintain a second
prose version of behaviour that can already be read precisely from the code.

Implementation documentation should explain what code cannot explain well on its own: rationale,
non-obvious constraints, rejected approaches that matter for maintenance, consequential
discoveries, cross-component interactions, operational consequences, and deliberate deferrals.
It should help a reader understand the code, not restate it function by function. Requirements,
design documents, and ADRs remain authoritative for the architectural contracts the code must
satisfy.

Source comments follow the same boundary. Aim for a healthy code-to-comment balance, not a
numeric ratio. Keep comments local to non-obvious intent, invariants, safety reasons, constraints,
or why an apparently simpler mechanism is wrong. When an explanation needs architectural
history, alternatives, experiment evidence, or several paragraphs of context, move it to the
owning document and leave a concise comment and durable reference. Code files should not become
parallel documentation files.

### 1.6 Compress documentation to durable signal

Useful working detail is not automatically durable project information. Before a work unit is
complete, accumulated prose should be compressed to the minimum record needed to preserve
requirements, decisions, evidence boundaries, reproducibility, and future engineering
understanding.

Compression follows the architecture/implementation boundary in §1.2. Architecture-facing
documents should exclude replaceable implementation detail especially aggressively.
Implementation-facing documents may retain technical detail when it contributes to implementation
understanding, reproducibility, consequential decisions, or avoiding an important failed path,
but chronological completeness is not a goal.

Prefer deletion and links to one semantic owner over repeated explanation. Ordinary workflow
narration, exact timestamps, duplicated rationale, superseded intermediate reasoning, and
transient discoveries are normally removed unless they materially change interpretation or a
future decision. Review affected records during PR review and perform a broader compression pass
at the end of each development cycle.

## 2. Roles and decision ownership

The roles below describe responsibility and review emphasis, not permissions. One participant may
occupy several roles; uncertainty, high-impact trade-offs, or boundary-crossing consequences are
what trigger additional review.

| Role | Primary responsibility | Boundary |
|---|---|---|
| **Project owner** | goals, scope, priorities, accepted trade-offs, final decisions, merge and publication; active participation in architecture and implementation reasoning | does not automatically accept reviewer or agent output; seeks additional review for uncertainty or material architectural/implementation consequences |
| **Architecture / design reviewer** | goal/problem framing, requirements, invariants, authority and trust boundaries, formal design, ADR review, cross-cutting architectural consistency | avoids prescribing replaceable implementation mechanics unless they affect the architectural contract |
| **Implementation agent / reviewer** | concrete implementation, tests, implementation records, operational mechanics, feasibility feedback, local validation | does not silently change accepted requirements or architecture to simplify implementation; raises the conflict instead |
| **Independent reviewer** | adversarial checks of code, tests, contracts, and evidence; finding assumptions the primary path missed | does not own project scope or make unilateral architectural changes |

The project owner may act as both co-architecture/design reviewer and co-implementation reviewer.
Specialist reviewers add depth and independent challenge; they do not prevent direct
participation across the boundary.

Which participant or tool currently occupies each role is a working arrangement, not an
architectural property: changing tools or redistributing responsibilities does not require
changing system design, and the rules above are therefore written in terms of roles rather than
participants.

## 3. Boundary crossing and implementation readiness

### 3.1 When an implementation issue becomes architectural

Escalate from implementation when a finding implies any of:

- an accepted requirement or invariant cannot be preserved;
- an authority or ownership boundary must move;
- local atomicity would become distributed coordination;
- a new privileged or trusted component is required;
- evidence would mean something different from what the design claims;
- a supposedly replaceable mechanism has become a system-wide constraint;
- satisfying the design requires disproportionate complexity that suggests the abstraction is
  wrong.

State the problem cleanly and reopen the owning requirement or design decision rather than
patching around it.

### 3.2 Ready for implementation

Before a non-trivial architectural work unit begins implementation, five questions should have
answers:

1. **Which goal and current problem does this work advance?**
2. **Which requirement is being satisfied or investigated?**
3. **Which design document owns the system shape or contract?** If design is intentionally open,
   state what implementation may explore rather than silently settling it in code.
4. **How will the claim be validated?** Name the applicable validation plan, test, experiment,
   control, or acceptance/falsification condition.
5. **Where is the work scheduled?** The plan owns priority, budget, work-unit/PR placement, and
   descope order.

This is a lightweight readiness gate, not a template requirement. Existing stable documents may
answer most of it by reference.

If implementation shows that an answer is wrong, scheduled scope is not authority. The finding
feeds Evidence and Analyse & Review; further work starts a new or refined problem and reopens the
owning requirement, design, or validation intent as necessary before rescheduling.

## 4. Review behaviour

### 4.1 Review the semantic owner

Before raising a finding, identify its owner using §6 and review that owner at its own abstraction
level. Do not demand implementation precision from an ADR, or use an implementation preference
to rewrite an accepted requirement or architectural invariant.

### 4.2 Separate blocker, contract question, and improvement

A review should distinguish:

- **blocker** — the implementation or evidence cannot satisfy an accepted contract;
- **contract question** — the owning requirement/design is ambiguous or may itself need revision;
- **improvement** — useful hardening that does not invalidate the current contract.

This prevents optional hardening from appearing equivalent to a correctness failure and keeps
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

The work-unit name matches the implementation record
([`implementation/README.md`](implementation/README.md)), so branch, record, and PR remain
traceable. A second branch against the same work unit adds a short suffix rather than inventing a
new work-unit name. The `agent/` prefix records how the change was produced, not how much it is
trusted, and does not shorten review.

A PR opens as a **draft** and stays one until its local gate and applicable validation pass and the
template questions are answered; the maintainer then marks it ready and merges.
[`../../.github/pull_request_template.md`](../../.github/pull_request_template.md) owns those
questions and the disclosure checklist.

Branch names are published artifacts and fall under
[`../public-disclosure-policy.md`](../public-disclosure-policy.md) exactly as file contents,
commit messages, and PR text do.

## 6. Documentation ownership and lifecycle

The documentation structure makes the architecture/implementation boundary from §1.2 visible
rather than requiring readers to infer it from individual files:

```text
ARCHITECTURE
  docs/requirements/                     goals, problems, requirements
  docs/design/                           normative system design and contracts
  docs/decisions/                        significant architectural decisions
  docs/planning/                         project-wide/general planning
  docs/planning/<milestone>/
      milestone-plan.md                  milestone schedule and scope
      milestone-validation.md            milestone validation intent

IMPLEMENTATION
  docs/development/implementation/       selective implementation records
  test/                                  executable tests and test support
  docs/operations/                       build/run/deploy/operate procedures
  docs/measurements/                     experiment execution, evidence, results
```

The split is about abstraction and ownership, not importance. Architectural documents define or
govern what implementation must satisfy; implementation documents and executable artifacts record
how the accepted architecture is realised, operated, exercised, and measured.

Planning is shown on the architectural side because it governs work before and above the concrete
mechanism, but a plan is **not** normative architecture. Schedule, budget, priority, and validation
intent may change without changing runtime meaning.

The document-owner principle is:

> A fact should have one normative or empirical home. Other documents link to it rather than
> maintaining a second competing copy.

### 6.1 Architectural documents

Architectural documents stay at the abstraction required to define the problem, accepted system
shape, significant decisions, and the work and validation needed to establish them. Replaceable
implementation mechanics do not move into these documents merely because they informed a
decision.

| Area | Owns |
|---|---|
| [`../requirements/`](../requirements/) | durable engineering goals, durable problems that block them, and requirements stating what must be true independently of replaceable mechanism |
| [`../design/`](../design/) | current normative system design and contracts |
| [`../decisions/`](../decisions/) | significant architectural choices: why and what, not replaceable implementation mechanics |
| [`../planning/`](../planning/) | project-wide/general planning documents; milestone-specific planning lives in one subdirectory per milestone |
| `../planning/<milestone>/milestone-plan.md` | schedule: budget, sequencing, priorities, descope order, intended work-unit/PR scope, and status |
| `../planning/<milestone>/milestone-validation.md` | validation intent: scenarios, workloads, controls, fault cases, acceptance/falsification conditions, and requirement/design coverage |

General planning documents that are not owned by one milestone stay directly under
`docs/planning/`. Keeping `milestone-plan.md` and `milestone-validation.md` together makes the
planning set for a milestone easy to find while preserving their distinct ownership.

A durable engineering goal and problem normally sit with the requirements they govern.
Milestone validation states **what must be demonstrated or falsified**, not how a particular test
or experiment happens to implement that intent.

### 6.2 Implementation documents

Implementation documentation may contain technical detail when that detail is needed to
understand the implementation, operate the system, reproduce evidence, preserve consequential
reasoning, or avoid repeating an important failed path. It remains selective under §1.5 and §1.6:
code and tests own precise mechanics, and chronological completeness is not a goal.

| Area | Owns |
|---|---|
| [`implementation/`](implementation/) | selective implementation rationale, discoveries, review decisions, deferrals, and context not adequately carried by code and tests |
| `../../test/` | executable tests and test-support implementation: fixtures, harnesses, fault injection, and other validation mechanics |
| [`../operations/`](../operations/) | procedures for building, running, deploying, and operating the system |
| [`../measurements/`](../measurements/) | experiment inputs, execution artifacts, reports, measured results, and empirical conclusions |

An implementation record may begin as a scope note while work is being planned. Once
implementation starts, it becomes the selective record of work actually performed under
`docs/development/implementation/`. A temporary implementation problem stays there unless
analysis shows that it exposes a missing architectural requirement or decision, in which case it
crosses the boundary explicitly under §3.1.

The validation boundary follows the same split: **milestone validation owns validation intent;
`test/` owns executable validation mechanics; `measurements/` owns empirical execution and
results.** Commands, concrete run configuration, dashboards, retained artifacts, observations,
and measured results therefore stay on the implementation side rather than being copied into the
milestone validation document.

### 6.3 Durable reference direction

Durable artifacts depend on durable owners for normative meaning.

- Code and tests may cite requirements, design documents, ADRs, and stable invariant identifiers.
- Operations and implementation records may cite those contracts plus code/configuration they
  explain.
- Milestone validation documents cite requirements/design and the measurement contract.
- Milestone plans cite goals, requirements, design, and their milestone validation document when
  scheduling work.
- **Code, tests, requirements, and durable design must not depend on a milestone plan for
  normative meaning.**

A plan may still be cited for a genuinely historical or scheduling fact, but that purpose should
be explicit. Cross-document references should name the document as well as the section; line
numbers into living documents are not durable references.

## 7. AI-assisted engineering and public provenance

AI assistance is engineering input, not autonomous authority. Designs, patches, reviews,
explanations, and documentation produced with AI tools are subject to the same correctness,
evidence, compression, and review requirements as human-authored work.

Public documentation may name the tools used when useful provenance, but durable rules are written
in terms of roles so the process survives tool changes. The human project owner remains
responsible for accepted technical decisions, repository claims, merge, and publication.

No architectural argument is accepted because of which participant or model proposed it. It is
accepted because the reasoning, implementation, tests, and evidence support it.
