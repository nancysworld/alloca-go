# Alloca-Go — Project Journey

**Status:** Living — narrative orientation for the project.  
**Scope:** explain how Alloca-Go arrived at its current architecture and evidence boundary, and how the style of investigation is evolving.  
**Does not own:** requirements, accepted design, validation obligations, schedules, or measured results. Those remain in their authoritative documents and reports.

Alloca-Go is not the implementation of a predetermined architecture. It is an engineering project built around a recurring pattern:

> **Ask a systems question, gather enough evidence to narrow it, then choose the next question deliberately.**

The booking domain provides a stable stateful system in which correctness, performance, reliability, and scaling questions can be investigated without changing the subject every time the architecture changes.

This document tells that story. For current system shape, start with [`design/high-level-design.md`](design/high-level-design.md). For possible future directions, see the [`exploration roadmap`](planning/alloca-go-roadmap.md). For the engineering loop itself, see [`development/engineering-process.md`](development/engineering-process.md).

## 1. A booking prototype left an unanswered systems question

Alloca-Go has a predecessor: **RuntimeIQ-Alloca**, the booking prototype from the earlier RuntimeIQ project. Alloca-Go is a new implementation in Go rather than a port. Domain knowledge, questions, and design lessons carried forward; code and quantitative results did not.

The predecessor had already exposed the interesting shape of the problem. Under concurrency, response time grew until timeouts became visible, but the prototype had not isolated why. That made the next useful step less about adding product features and more about building a system in which the behaviour could be investigated cleanly.

The booking domain is useful for that purpose because it is not merely CRUD. It is an **allocation problem under contention**: multiple requests compete for scarce units while holds expire, users retry, outcomes may be ambiguous, and correctness must survive concurrent mutation. A performance result is not useful if the experiment allows oversell, duplicate logical mutations, or invalid schedule overlap.

The project therefore began with two commitments that still shape it:

- treat the workload as a synthetic engineering model rather than a claim about any real organisation's traffic or architecture; and
- keep prior observations as hypotheses or context until this repository reproduces them under its own evidence rules.

That boundary is recorded in [`design/high-level-design.md`](design/high-level-design.md) §1.1 and [`design/measurement-contract.md`](design/measurement-contract.md).

## 2. Correctness had to come before scale

AG-M1 deliberately built the smallest correct authoritative booking system before trying to distribute it or claim capacity.

That meant making the hard state questions explicit first: which row or relation owns each correctness decision, how slot capacity is serialized, how one user's schedule is protected from overlapping bookings, how idempotency turns retries into one logical mutation, how authoritative time is chosen, and how ambiguous outcomes remain safe to replay.

PostgreSQL became the transactional authority for those guarantees. The service remained a **modular monolith** with enforced internal boundaries rather than being split into services in advance. The architectural rule was simple: decomposition would follow evidence of an independent ownership, scaling, or failure boundary rather than presentation value.

This order matters to the rest of the journey. Later scaling work does not ask how to make a weaker booking system faster. It asks which work can proceed independently **without weakening the transaction semantics already established**.

The normative model is in [`design/transaction-semantics.md`](design/transaction-semantics.md); the monolith-first decision is [`decisions/0001-modular-monolith-first.md`](decisions/0001-modular-monolith-first.md).

## 3. Measure before deciding what to distribute

Once the correctness substrate existed, the next question was deliberately open:

> **What actually limits a correct single-authority system first?**

The candidate answers included application compute, PostgreSQL, the database connection pool, a hot logical authority, telemetry, the load generator, or the shared development environment. The point of the experiment was diagnosis rather than validation of a preselected scaling technique.

Iteration A produced the first load-bearing performance result. On the retained developer-workstation experiment, throughput plateaued at roughly **4,300 booking requests per second** while the Go service still had substantial compute headroom. Increasing concurrency added latency and queueing rather than useful work. The limiting subsystem was **PostgreSQL, not the Go service**.

The exact PostgreSQL sub-mechanism was less certain than the subsystem conclusion, and the report keeps that distinction explicit. The number is also a bounded workstation result, not a production capacity claim.

The more important outcome was what the evidence ruled out as the immediate next move. Adding more stateless service replicas against the same saturated writer might change behaviour below the frontier, but it could not answer how end-to-end writable capacity should scale.

That conclusion is retained in [`measurements/reports/ag-sept-pr2-single-instance-frontier.md`](measurements/reports/ag-sept-pr2-single-instance-frontier.md), with the resulting Analyse & Review in [`requirements/ag-sept.md`](requirements/ag-sept.md) §1.

## 4. Scale the authority the evidence identified

Iteration A changed the next problem from "how do we scale the service?" to a more specific question:

> **How can independent organisation work use independently writable PostgreSQL authorities while preserving the accepted correctness model?**

That is harder than simply starting a second database. Reserve operations involve two ownership axes: slot capacity and the user's schedule. If those axes live in different writable transaction domains, one local PostgreSQL transaction can no longer coordinate the whole operation.

Iteration B therefore introduced explicit organisation placement and independently writable database authorities, but kept the first distributed design intentionally narrow. Supported Phase 1 mutations stay inside one writable authority. A reserve whose user and slot resolve to different authorities is **explicitly refused** rather than implemented as two local commits and described as atomic.

The resulting unit is a **shard group**: one database authority plus compatible stateless service replicas bound to it. Service-compute scaling inside a shard group and adding another writable authority are treated as two different axes.

PR3c then tested whether that boundary composed correctly. Two writable authorities served their own organisation work, cross-authority requests were refused without partial mutation, misrouting was rejected, reconciliation held, and failure of one authority remained contained while the healthy peer continued. Restoration did not require compensating writes on an unrelated authority.

Just as important, the experiment made **no throughput multiplier claim**. Both authorities still shared one workstation resource envelope. The result established a correctness- and failure-isolation boundary, not independent capacity.

The accepted architecture is owned by [`design/horizontal-scaling.md`](design/horizontal-scaling.md) and [`design/horizontal-database-authority.md`](design/horizontal-database-authority.md). The retained correctness evidence is [`measurements/reports/ag-sept-pr3c-phase1-correctness.md`](measurements/reports/ag-sept-pr3c-phase1-correctness.md).

## 5. Correctness proof is not a capacity proof

Iteration B made the shard group a **candidate capacity unit**. It did not prove that adding shard groups adds useful capacity.

That distinction became the next Problem:

> **How does aggregate mutation capacity behave as independently provisioned shard groups are added for independent organisation workloads, and what limits that scaling?**

Iteration C fixes the workload and compares 1, 2, and 4 shard groups. The intended result is numeric capacity and scale efficiency under controlled, independently growing resource envelopes. There is deliberately no preselected efficiency threshold to optimise toward; a sub-linear result is useful if it is trustworthy and its limiting mechanism is identified or conservatively bounded.

The first implementation of that experiment was local. PR4a qualified the sustained measurement method, including workload invariance, conditioning, generator controls, provenance, reconciliation, and a fixed measurement horizon. PR4b then executed the full G1/G2/G4 comparison with scheduler partitioning on one workstation.

Only `G4_local` resolved. `G1` and `G2` did not reproduce tightly enough to become accepted capacity quantities, so the derived efficiencies were withheld rather than estimated.

That refusal to manufacture a denominator is part of the result. The experiment had enough instrumentation to show that material variation tracked the workstation's shared write path while mutation work per written MiB remained comparatively stable. It therefore localised the problem away from the Go service, but did **not** prove the deeper storage mechanism.

The retained analysis is [`measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md`](measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md).

## 6. The experiment found the limit of its own environment

Scheduler partitioning removed one known confound: generator and serving groups no longer competed for the same assigned logical CPUs. It did not turn one workstation into independently growing capacity units.

The shard groups still shared the host kernel, memory hierarchy, Docker Desktop environment, storage path, and higher-level I/O behaviour. The observed G1/G2 variation was large enough that the local denominator could not support the independent-capacity conclusion Iteration C was designed to make.

PR4c therefore refined an independently provisioned probe rather than treating the local topology as good enough. That probe was not executed. The available EC2 quota could not provision the complete environment, so there is **no AWS performance cell**, no independent `G1/G2/G4` result, and `VAL-SCALE-5` remains unproven.

Stopping there is part of the engineering record. The work unit ended; the Problem did not.

Iteration C remains open until an equivalent independently provisioned environment is available. AWS is one possible implementation of that environment, not an architectural requirement. Another provider or environment is acceptable if it preserves the experiment's resource-independence and provenance requirements.

This checkpoint is intentionally not an Analyse & Review closure. The current evidence boundary and continuation condition are recorded in the PR4 checkpoint report and [`requirements/ag-sept.md`](requirements/ag-sept.md) §3.

## 7. A new phase: methodology-informed investigation

Alloca-Go’s work so far has been guided by engineering experience, measurement, iterative hypotheses, and the results of each experiment. That approach established the correctness substrate, identified PostgreSQL as the first measured frontier, led to the writable-authority design, and exposed the limits of the local experimental environment.

The next phase adds another input: **established systems-performance methodology**.

Brendan Gregg’s *Systems Performance* is a useful starting point. The plan is to study its methods, compare them with the approaches already used in Alloca, identify useful gaps or refinements, and apply selected techniques where they improve a real investigation. The aim is not to adopt a fixed recipe, but to combine established methodology with the project’s existing engineering process.

This also changes the pace of the project. Future progress does not need to mean immediately building another mechanism or launching another benchmark. Time can instead go into studying methodology, revisiting retained evidence, improving workload characterisation or observation, and designing smaller, more discriminating experiments.

The working principle becomes:

> **Methodology informs the investigation. Evidence decides the conclusion. Engineering judgement chooses the next useful question.**

Alloca-Go therefore remains open-ended and evidence-led, but future work can draw more deliberately on established systems-performance practice. The [`exploration roadmap`](planning/alloca-go-roadmap.md) continues to record possible directions; the [`engineering process`](development/engineering-process.md) selects one worthwhile problem at a time.

The longer-term direction is:

> **Use a real stateful backend as a systems-engineering laboratory: learn established methods, apply them selectively, and let evidence determine what the system needs next.**
