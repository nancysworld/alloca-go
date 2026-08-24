# Open engineering questions

**Status:** Living — concrete unresolved engineering questions, not scheduled work.  
**Scope:** record specific questions exposed by design work or evidence that may deserve a future engineering iteration.  
**Does not own:** roadmap direction, current Problems or requirements, accepted design, validation obligations, schedules, or measured results.

This document sits between the broad [`alloca-go-roadmap.md`](alloca-go-roadmap.md) and active engineering work. The roadmap records areas that may be worth exploring; this file records narrower questions the project has actually encountered. A question becomes committed work only when it is selected into a Goal or Problem and then receives requirements, design, validation, and schedule under the [`engineering process`](../development/engineering-process.md).

An unanswered question is not technical debt merely because it is unresolved. Technical debt belongs in [`tech-debts.md`](tech-debts.md) when the project has accepted a known deficiency with a trigger for repayment.

## 1. Cross-authority booking

**Question.** What coordination model, if any, should support a booking whose user and slot resolve to different writable database authorities while preserving the accepted transaction invariants, idempotency/replay semantics, and bounded failure behaviour?

**Why it remains open.** The accepted Phase 1 design deliberately keeps supported mutations inside one writable transaction domain. Cross-database-authority reserve is explicitly refused rather than implemented as two unrelated local commits. Iteration B established that this narrower boundary composes correctly and contains authority failure; it did not attempt a distributed coordination protocol.

**Evidence / current owners.** [`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md) owns the same-/cross-authority design boundary. [`../measurements/reports/ag-sept-pr3c-phase1-correctness.md`](../measurements/reports/ag-sept-pr3c-phase1-correctness.md) retains the Phase 1 correctness and failure-isolation evidence.

**Promote into active work when.** A concrete workload or product requirement makes cross-authority booking worth supporting, so the project can frame the required guarantees and compare coordination approaches against an explicit need rather than adding distributed coordination speculatively.

## 2. Single-authority capacity frontier

**Questions.** What exact PostgreSQL resource or coordination mechanism sets the single-authority mutation frontier in a controlled independently provisioned environment? What causes the large run-to-run/environmental variance seen in retained local experiments, and are those two questions related?

**Why they remain open.** Iteration A established the durable subsystem-level conclusion: PostgreSQL, not available Go service compute, set the measured workstation frontier. The report narrowed possible PostgreSQL mechanisms but did not make the deeper sub-mechanism equally strong evidence. Later Iteration C local controls sharpened the variance question: identical G1 runs varied materially while mutation work per written MiB stayed comparatively stable, localising the variation to the shared write path without proving the storage mechanism beneath it.

**Evidence / current owners.** [`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md) owns the first frontier result and its limitations. [`../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md`](../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md) owns the later shared-write-path and variance evidence.

**Investigation order.** If this question is promoted, characterise one shard group first on a cloud instance dedicated to the experiment, with the generator on separate compute and no other project application sharing the capacity unit. Establish a reproducible sustained frontier, identify or conservatively bound its limiting mechanism and material variance, then freeze the capacity-unit resource/configuration envelope. Only after that single-unit baseline is trustworthy should a multi-unit scalability experiment compare equivalent independently provisioned G2/G4 topologies. The single-authority result is therefore a prerequisite denominator for a credible scale-efficiency comparison, not merely the first cell of that comparison.

**Promote into active work when.** A future performance or scaling decision depends on a trustworthy single-unit capacity baseline, or methodology review identifies a sufficiently discriminating investigation to justify the work. Useful progress may begin with study, re-analysis and better observation before a new experiment is run.
