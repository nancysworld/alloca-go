# Planning documents

**This directory owns planning:** priority, sequence, work-unit split, active schedule constraints,
descope order, the history of how an order was reached, and — per milestone — the validation
intent that scheduled work must establish. Durable goals, problems, and requirements live in
[`../requirements/`](../requirements/); system shape and contracts in
[`../design/`](../design/).

Milestone planning lives in one subdirectory per milestone, holding `milestone-plan.md` (the
schedule) and `milestone-validation.md` (what must be demonstrated or falsified). General
planning documents that no single milestone owns stay directly here
([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

That pair is the convention for **current and new** milestones. A milestone completed before the
convention existed keeps its historical shape: AG-M1 has a milestone plan and no validation sibling,
because its durable validation meaning had already moved to the documents that own it — correctness
gates and the authority model to
[`../design/transaction-semantics.md`](../design/transaction-semantics.md), measurement vocabulary
and outcome taxonomy to
[`../design/measurement-contract.md`](../design/measurement-contract.md). Writing a retrospective
`milestone-validation.md` for it would manufacture planning history rather than record it.

Code, tests, requirements, and durable design must not depend on a document here for normative
meaning ([`../development/engineering-process.md`](../development/engineering-process.md) §6.3).

- [`alloca-go-roadmap.md`](alloca-go-roadmap.md) — the **exploration roadmap**: areas and
  questions that may be worth exploring. Directional, non-normative, unscheduled, and upstream of
  Goal selection — it says where the project *might* go, never what it has committed to.
- [`open-questions.md`](open-questions.md) — specific engineering questions exposed by design or
  evidence that remain unresolved but are not currently scheduled Problems; a narrower bridge
  between the broad roadmap and active engineering work.
- [`ag-sept/`](ag-sept/) — the current AG-Sept milestone:
  - [`ag-sept/milestone-plan.md`](ag-sept/milestone-plan.md) — current schedule, budget,
    work-unit status, August publication checkpoint, and the pending Iteration C continuation;
  - [`ag-sept/milestone-validation.md`](ag-sept/milestone-validation.md) — milestone validation
    intent and `VAL-*` status;
  - [`ag-sept/milestone-validation-pr4c-aws-probe.md`](ag-sept/milestone-validation-pr4c-aws-probe.md)
    — fixed-per-unit independent probe method; PR4c produced no performance cell and Iteration C
    remains open pending an equivalent independent environment;
  - [`ag-sept/milestone-plan-v0.4.md`](ag-sept/milestone-plan-v0.4.md) — the archived v0.4 plan,
    superseded 5 August 2026 and non-normative. Retained publicly because PR1 and PR2 were planned
    and reported under it, and their records cite its section numbers.
- [`ag-m1/`](ag-m1/) — the completed AG-M1 milestone:
  - [`ag-m1/milestone-plan.md`](ag-m1/milestone-plan.md) — AG-M1 PR split and correctness gates;
    historical, not normative for current work, and without a validation sibling for the reason
    given above.
- [`release-shaping-experiment.md`](release-shaping-experiment.md) — synthetic AG-M2/AG-M5 experiment plan comparing synchronized, rolling, clustered, and hot-slot release shapes.
- [`tech-debts.md`](tech-debts.md) — the `DEBT-n` register: deliberate gaps, why each was accepted, and the trigger that ends the acceptance.

AG-Sept per-PR records began here as scope notes and now live under
[`../development/implementation/`](../development/implementation/), where they record how the work
was actually built. This directory retains intended work and validation, not implementation history.
