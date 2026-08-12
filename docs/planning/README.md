# Planning documents

**This directory owns scheduling facts only:** priority, budget, sequence, work-unit split,
contingency, descope order, and the history of how an order was reached. Durable goals, problems,
and requirements live in [`../requirements/`](../requirements/); system shape and contracts in
[`../design/`](../design/); validation intent in
[`../test/validation-plan/`](../test/validation-plan/).

Code, tests, requirements, and durable design must not depend on a document here for normative
meaning ([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

- [`alloca-go-roadmap.md`](alloca-go-roadmap.md) — the **exploration roadmap**: areas and
  questions that may be worth exploring. Directional, non-normative, unscheduled, and upstream of
  Goal selection — it says where the project *might* go, never what it has committed to.
- [`ag-sept-plan.md`](ag-sept-plan.md) — **the current AG-Sept plan**: 19.5 development days;
  Iteration B closed; Iteration C fixed as a bounded 0.5-day planning PR followed by a 1.5-day AWS
  EC2 capacity-environment PR and a 2.5-day 1/2/4-shard-group capacity-evidence PR. The AWS path is
  deliberately minimal EC2 measurement infrastructure, not the withdrawn EKS/RDS plan.
- [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) — archived v0.4 snapshot, superseded 5 August 2026. Retained because PR1 and PR2 were planned and reported under it and cite its section numbers.
- [`ag-m1-implementation-plan.md`](ag-m1-implementation-plan.md) — AG-M1 PR split and correctness gates.
- [`release-shaping-experiment.md`](release-shaping-experiment.md) — synthetic AG-M2/AG-M5 experiment plan comparing synchronized, rolling, clustered, and hot-slot release shapes.
- [`tech-debts.md`](tech-debts.md) — the `DEBT-n` register: deliberate gaps, why each was accepted, and the trigger that ends the acceptance.

The AG-Sept per-PR records began here as scope notes and are now implementation records under
[`../development/implementation/`](../development/implementation/): `ag-sept-pr1.md`,
`ag-sept-pr2.md`, `ag-sept-pr3.md`. This directory holds what we intend to do; those record how
the work was actually built.
