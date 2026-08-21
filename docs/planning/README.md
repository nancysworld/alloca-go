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
meaning ([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

- [`alloca-go-roadmap.md`](alloca-go-roadmap.md) — the **exploration roadmap**: areas and
  questions that may be worth exploring. Directional, non-normative, unscheduled, and upstream of
  Goal selection — it says where the project *might* go, never what it has committed to.
- [`ag-sept/`](ag-sept/) — the current AG-Sept milestone:
  - [`ag-sept/milestone-plan.md`](ag-sept/milestone-plan.md) — **the current AG-Sept schedule**:
    19.5 development days; Iteration B closed; Iteration C fixed as a bounded 0.5-day planning PR
    followed by a 1.5-day AWS EC2 capacity-environment PR and a 2.5-day 1/2/4-shard-group
    capacity-evidence PR. The AWS path is deliberately minimal EC2 measurement infrastructure,
    not the withdrawn EKS/RDS plan.
  - [`ag-sept/milestone-validation.md`](ag-sept/milestone-validation.md) — AG-Sept validation
    intent: the single-authority frontier, Phase 1 multi-authority correctness/failure isolation,
    and the Iteration C shard-group capacity method including `VAL-SCALE-5`/`VAL-SCALE-6`.
  - [`ag-sept/milestone-validation-pr4c-aws-probe.md`](ag-sept/milestone-validation-pr4c-aws-probe.md)
    — the deferred fixed-per-unit independent probe method. PR4c did not execute it and
    `VAL-SCALE-5` remains unproven.
- [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) — archived v0.4 snapshot, superseded 5 August 2026. Retained because PR1 and PR2 were planned and reported under it and cite its section numbers. Deliberately **not** moved under `ag-sept/`: PR1's and PR2's records cite it by the filename it had when they were written.
- [`ag-m1-implementation-plan.md`](ag-m1-implementation-plan.md) — AG-M1 PR split and correctness gates.
- [`release-shaping-experiment.md`](release-shaping-experiment.md) — synthetic AG-M2/AG-M5 experiment plan comparing synchronized, rolling, clustered, and hot-slot release shapes.
- [`tech-debts.md`](tech-debts.md) — the `DEBT-n` register: deliberate gaps, why each was accepted, and the trigger that ends the acceptance.

The AG-Sept per-PR records began here as scope notes and are now implementation records under
[`../development/implementation/`](../development/implementation/): `ag-sept-pr1.md`,
`ag-sept-pr2.md`, `ag-sept-pr3.md`. This directory holds what we intend to do; those record how
the work was actually built.
