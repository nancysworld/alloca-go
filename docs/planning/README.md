# Planning documents

**This directory owns planning:** priority, budget, sequence, work-unit split, contingency,
descope order, the history of how an order was reached, and — per milestone — the validation
intent that scheduled work must establish. Durable goals, problems, and requirements live in
[`../requirements/`](../requirements/); system shape and contracts in
[`../design/`](../design/).

Milestone planning lives in one subdirectory per milestone, holding `milestone-plan.md` (the
schedule) and `milestone-validation.md` (what must be demonstrated or falsified). General
planning documents that no single milestone owns stay directly here
([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

Code, tests, requirements, and durable design must not depend on a document here for normative
meaning ([`../development/engineering-process.md`](../development/engineering-process.md) §6.3).

- [`alloca-go-roadmap.md`](alloca-go-roadmap.md) — the **exploration roadmap**: areas and
  questions that may be worth exploring. Directional, non-normative, unscheduled, and upstream of
  Goal selection — it says where the project *might* go, never what it has committed to.
- [`ag-sept/`](ag-sept/) — the current AG-Sept milestone:
  - [`ag-sept/milestone-plan.md`](ag-sept/milestone-plan.md) — current schedule, budget,
    work-unit status, and closeout sequence;
  - [`ag-sept/milestone-validation.md`](ag-sept/milestone-validation.md) — milestone validation
    intent and `VAL-*` status;
  - [`ag-sept/milestone-validation-pr4c-aws-probe.md`](ag-sept/milestone-validation-pr4c-aws-probe.md)
    — deferred fixed-per-unit independent probe method; PR4c produced no AWS capacity evidence.
- [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) — archived v0.4 snapshot, superseded 5 August 2026. Retained at its historical filename because PR1 and PR2 were planned and reported under it and cite its section numbers.
- [`ag-m1-implementation-plan.md`](ag-m1-implementation-plan.md) — AG-M1 PR split and correctness gates.
- [`release-shaping-experiment.md`](release-shaping-experiment.md) — synthetic AG-M2/AG-M5 experiment plan comparing synchronized, rolling, clustered, and hot-slot release shapes.
- [`tech-debts.md`](tech-debts.md) — the `DEBT-n` register: deliberate gaps, why each was accepted, and the trigger that ends the acceptance.

AG-Sept per-PR records began here as scope notes and now live under
[`../development/implementation/`](../development/implementation/), where they record how the work
was actually built. This directory retains intended work and validation, not implementation history.
