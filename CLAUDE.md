# CLAUDE.md

Project instructions for Claude Code.

This file records stable repository conventions that are easy to violate silently. It does not
duplicate the code, Makefile, or detailed documentation. When this file conflicts with an owning
document, the owning document wins; update this file rather than working around the discrepancy.

Start with `docs/README.md` for the repository documentation map, then
`docs/design/high-level-design.md` for the system shape and the design ownership table. The
engineering process — the goal-governed iteration loop and the document ownership model — is
defined in `docs/development/engineering-process.md`.

## Roles and design escalation

The repository maintainer owns scope, trade-offs, final decisions, merges, and what the project
claims. Roles and their review emphases are defined in
`docs/development/engineering-process.md` §2.

Work in the implementation role — code, tests, implementation records, operational mechanics, and
local validation — should also review architecture and design from the implementation side,
surface constraints, and challenge assumptions, but must not independently settle decisions owned
by the maintainer or an owning design document.

Pause and escalate when an implementation decision changes or materially affects:

- scope;
- schemas;
- public or internal contracts;
- domain invariants;
- authority boundaries;
- failure semantics;
- the interpretation of evidence.

When escalating a genuine design question, state the problem, constraints, evidence, and known
trade-offs clearly. Do not present the currently visible options as exhaustive unless they truly
are exhaustive.

## Validation discipline

Use the repository's documented build, test, integration, smoke, and CI commands rather than
inventing parallel validation paths.

A green Go test suite does not validate non-Go artifacts such as PromQL, YAML, shell, SQL,
Compose configuration, or dashboards. Validate those with the relevant runtime or validator
before claiming they work.

Prefer discriminating tests: where practical, a correctness gate should have a test or controlled
mutation that fails specifically when that property is removed. Passing a broad suite is weaker
evidence than demonstrating that the intended gate detects its own absence.

Do not claim validation that was not actually performed.

## Evidence discipline

Every quantitative claim must follow the evidence classification defined by
`docs/design/measurement-contract.md`.

Do not promote derived, prior, hypothetical, or unreproduced evidence into measured evidence.
A report must not quote a number unless its provenance and retained artifact satisfy the
measurement contract.

Use the repository's quotability terminology precisely. Name the applicable level rather than
using "quotable" as a vague boolean property.

Audit the proposition, not merely the wording: when changing a claim, search for the underlying
assertion across the repository rather than only for the sentence being edited.

Treat single load-test readings as provisional until reproduced or otherwise justified by the
measurement contract.

## Code conventions

The domain owns its interfaces; adapters implement them. Follow the normative import directions
defined in `docs/design/project-structure.md`. A change that requires a prohibited dependency is
a design issue, not a reason for a quiet exception.

Reuse `internal/domain` types for shared domain semantics. Do not redeclare outcomes, reasons,
identities, or other concepts already owned by the domain merely for local convenience. Wire and
report shapes may remain local where they are genuinely representation-specific.

`cmd/<binary>/main.go` is assembly only; domain logic belongs elsewhere.

Comments should explain what the code cannot: rationale, non-obvious constraints, rejected
alternatives, and operational consequences. Do not restate the code.

Prefer clear names over explanatory comments for ordinary local variables and control flow.

## Documentation

Keep one normative home per fact. Other documents should link to or summarize the owning source
without maintaining a competing version.

Follow the document ownership model defined in `docs/development/engineering-process.md`.

**Schedule is downstream of durable meaning.** Code, tests, requirements, and durable design must
not cite a milestone plan for normative meaning — a plan is expected to change and cannot be the
stable definition of runtime behaviour or evidence validity. Cite `REQ-*`, `INV-*`, `VAL-*`, a
design document, an ADR, or the measurement contract instead. A plan may be cited for a genuinely
scheduling or historical fact, and the citation should say that is what it is.

Name the document as well as the section in a cross-document reference; a bare `§` is safe only
where its owner is unambiguous in context. Line numbers are not durable references.

Prefer executable truth over duplicated implementation prose. Code and tests own implementation
mechanics; implementation documentation should record what code cannot, such as discoveries,
review findings, deferrals, scope movement, rationale, and operational consequences.

A deliberate gap belongs in the tech-debt register only when its validity depends on a stated
condition and it has a concrete trigger for reconsideration.

## PR workflow

Follow the repository's current PR template, branch convention, review process, and milestone
scope documents.

Keep implementation records aligned with what actually shipped. Before a PR is considered ready,
audit the durable documentation against the implementation rather than assuming the original
scope note is still accurate.

Do not mark work complete merely because the implementation compiles or tests pass; completion
also requires the repository's documented validation, evidence, and documentation gates.

## Public disclosure

This repository is intended for eventual public release. The rules are owned by
`docs/public-disclosure-policy.md`, and `docs/pre-public-checklist.md` is the final sweep before
visibility changes. They apply to code, comments, documentation, commit messages, branch names,
and PR text alike; do not maintain a second copy of them here.