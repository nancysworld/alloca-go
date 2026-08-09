# Planning documents

**This directory owns scheduling facts only:** priority, budget, sequence, work-unit split,
contingency, descope order, and the history of how an order was reached. Durable goals, problems,
and requirements live in [`../requirements/`](../requirements/); system shape and contracts in
[`../design/`](../design/); validation intent in
[`../test/validation-plan/`](../test/validation-plan/).

Code, tests, requirements, and durable design must not depend on a document here for normative
meaning ([`../development/engineering-process.md`](../development/engineering-process.md) §6.1).

- [`alloca-go-roadmap.md`](alloca-go-roadmap.md) — project milestones, priorities, and evidence plan.
- [`ag-sept-plan.md`](ag-sept-plan.md) — **the current AG-Sept plan**: 19.5 development days, horizontal database authority before stateless replica scaling, no cloud path.
- [`ag-sept-plan-v0.4.md`](ag-sept-plan-v0.4.md) — archived v0.4 snapshot, superseded 5 August 2026. Retained because PR1 and PR2 were planned and reported under it and cite its section numbers.
- [`ag-m1-implementation-plan.md`](ag-m1-implementation-plan.md) — AG-M1 PR split and correctness gates.
- [`release-shaping-experiment.md`](release-shaping-experiment.md) — synthetic AG-M2/AG-M5 experiment plan comparing synchronized, rolling, clustered, and hot-slot release shapes.
- [`tech-debts.md`](tech-debts.md) — the `DEBT-n` register: deliberate gaps, why each was accepted, and the trigger that ends the acceptance.

The AG-Sept per-PR records began here as scope notes and are now implementation records under
[`../development/implementation/`](../development/implementation/): `ag-sept-pr1.md`,
`ag-sept-pr2.md`, `ag-sept-pr3.md`. This directory holds what we intend to do; those record how
the work was actually built.
