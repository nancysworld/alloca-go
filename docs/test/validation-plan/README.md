# Validation plans

This directory records **how Alloca-Go intends to prove or falsify requirements and design
claims** before implementation or measurement is treated as complete.

Validation planning is one stage inside the engineering iteration loop defined by
[`../../development/engineering-process.md`](../../development/engineering-process.md). The loop
is governed by a Goal that normally spans several iterations:

```text
                               GOAL
                                |
                                v
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ next problem <- Analyse & Review <- Evidence ------------+
                            |
                            +-- goal sufficiently achieved -> END
```

## What a validation plan owns

A validation plan may define:

- controlled workload semantics;
- required topology families;
- deterministic, integration, concurrency, or fault scenarios;
- negative controls;
- hypotheses and accept/reject conditions;
- the requirement or design property each validation exercises;
- the minimum evidence needed before a property may be claimed as demonstrated.

It does **not** own:

- the governing goal, problem, or system requirements — `docs/requirements/` does for durable
  engineering work;
- repository-wide evidence validity rules —
  [`../../design/measurement-contract.md`](../../design/measurement-contract.md) does;
- durable architecture — `docs/design/` does;
- commands and operator procedure — `docs/operations/` does;
- executable implementation mechanics — code and automated tests do;
- measured results — `docs/measurements/` does;
- timing, priority, budget, or PR sequence — `docs/planning/` does.

A validation plan can therefore survive substantial replanning. Scheduling may change *when* a
validation is run without changing what property the validation means, and a resolved problem may
lead to a different next validation while the governing goal remains stable.

## Current plans

- [`ag-sept-validation-plan.md`](ag-sept-validation-plan.md) — AG-Sept validation of the
  single-authority frontier, Phase 1 multi-authority correctness/failure isolation, and service
  replica scaling when Analyse & Review selects it as the next problem toward the AG-Sept goal.
