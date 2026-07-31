# AG-Sept — Measured scale-out and distributed authority

**Status:** Draft v0.3  
**Created:** 31 July 2026  
**Delivery window:** August 2026  
**Development budget:** 10 focused development days, followed by 2–3 days for reruns, review, refinement, documentation, and public-release preparation  
**Predecessor:** AG-M1 — correct transactional core and end-to-end service path

## 1. Executive intent

AG-M1 established the correctness foundation: Alloca-Go now has a runnable HTTP service, PostgreSQL-backed transactional authority, explicit outcome semantics, idempotency, reservation expiry, schedule non-overlap, tests, CI, and an observation boundary.

AG-Sept converts that correctness work into measured distributed-systems evidence.

The milestone answers one connected question:

> How does a correctness-first stateful service behave as load and stateless replica count increase, where does horizontal scaling stop helping, and what architecture follows from the evidence?

The intended progression is:

1. establish a reproducible single-instance capacity baseline;
2. run multiple stateless application replicas against the same PostgreSQL authority;
3. compare dispersed traffic with traffic concentrated on one transactional authority;
4. attempt to deploy and repeat a meaningful subset of the experiment on AWS without displacing the local evidence;
5. identify the database, connection, compute, or authority boundary that limits each workload;
6. document the distributed-system shape implied by the measurements;
7. extract a service only if doing so demonstrates a real consistency, failure, or scaling boundary.

Kubernetes may be used as the deployment substrate, but Kubernetes is not the subject of the milestone. The milestone succeeds by producing valid evidence and a defensible architectural conclusion, not by touching the largest number of infrastructure technologies.

## 2. Goal and exit statement

AG-Sept proves how Alloca's correctness authorities behave under load and horizontal scale.

At completion, the repository should support the following evidence-backed statement:

> Alloca began as a modular monolith because its central problem is transactional correctness, not service decomposition. After proving the invariants, the project measured one application instance, then scaled stateless instances against a shared PostgreSQL authority. Dispersed and hot-authority workloads were measured separately, so improvements from additional compute are not confused with the serialization limit of one slot or one identity. The results were then used to decide which future boundaries justify independent deployment.

An AWS deployment and rerun strengthen this story but must not consume the local measurement work that establishes it.

The measurements may reject an initial performance or scaling hypothesis. A negative or surprising result is a valid milestone outcome when the experiment is reproducible, the generator is ruled out as the bottleneck, correctness remains intact, and the limiting mechanism is identified.

## 3. Scope principles

### 3.1 Measure mechanisms before realistic composites

AG-Sept uses simplified controlled workloads inspired by synchronized-release and hot-resource patterns. It does not reproduce a full product workflow.

The controlled workloads isolate one mechanism at a time:

- low-contention requests across many independent authorities;
- many users contending for one slot authority;
- concurrent schedule mutations contending for one identity authority.

A small synchronized release wave may combine these mechanisms after the controls are established. It is not a replacement for them because a composite workload changes several variables at once and is harder to interpret.

### 3.2 Correctness gates remain non-negotiable

No performance result is publishable if the run violates the AG-M1 invariants or produces unreconciled outcomes.

At minimum, each measured run must preserve:

- slot capacity safety;
- one logical mutation per idempotency key;
- schedule non-overlap for one identity;
- total terminal-outcome classification;
- safe treatment of ambiguous outcomes;
- bounded request and database timeout behaviour.

### 3.3 Horizontal application scale is not authority scale

Adding application replicas can increase available stateless compute and concurrency. It does not remove the serialization requirement of one hot slot or one hot identity.

AG-Sept therefore reports scale efficiency separately for:

- dispersed independent authorities;
- one hot slot authority;
- one hot identity authority or a mixed-identity workload.

The normative capacity and scale-efficiency definitions remain owned by [measurement-contract.md](../design/measurement-contract.md) §3 and §3.1. For one indivisible hot authority, efficiency approaching `1/N` at `N` replicas is the expected serialization ceiling, not evidence that the whole system fails to scale.

No single headline number may be presented as "Alloca scales by X" across these different mechanisms.

### 3.4 Implementation remains evidence-led

This plan defines questions, gates, workload shapes, required evidence, and scope limits. It deliberately does not freeze:

- the load-generator library or exact internal structure;
- exact concurrency, rate, or duration sweep values;
- Prometheus client selection;
- dashboard layout;
- application and database resource sizes;
- Kubernetes packaging choice;
- AWS orchestration choice when EKS threatens the experiment schedule;
- any service boundary before evidence supports it.

Implementation PRs should choose the smallest mechanism that satisfies the relevant gate and leaves the experiment reproducible.

### 3.5 Evidence labels for plan parameters

All proposed workload sizes, replica matrices, durations, and resource values in this plan are `[HYPOTHESIS]` until measured, unless they are explicitly identified as a fixed planning budget or required test shape. Implementation reports must replace or retain that label according to the evidence convention in [measurement-contract.md](../design/measurement-contract.md) §2.

## 4. Time budget and priority

The ten-day development allocation is a planning constraint:

| Workstream | Days | Weight |
|---|---:|---:|
| Measurement harness and observability | 2.0 | 20% |
| Single-instance capacity baseline | 2.0 | 20% |
| Containerisation and local multi-instance experiment | 1.5 | 15% |
| AWS and Kubernetes deployment path | up to 2.0 | up to 20% |
| AWS scale-out experiments | up to 1.5 | up to 15% |
| Architecture conclusions and one justified boundary | 1.0 | 10% |
| Contingency returned from AWS when needed | up to 3.5 | up to 35% |

A further 2–3 days are reserved for:

- rerunning decisive experiments;
- validating negative controls;
- reviewing measurements and interpretations;
- correcting documentation;
- polishing diagrams and the repository entry point;
- checking public-disclosure suitability;
- preparing the project for external readers.

The time budget is a constraint, not an estimate to be expanded whenever a tool introduces incidental complexity.

## 5. Required workload model

### 5.1 Dispersed-authority control

Requests are distributed across many slots and many users so contention on any one slot or identity is low.

Purpose:

- establish the general application and PostgreSQL frontier;
- measure the benefit of additional stateless replicas when transactions can proceed mostly independently;
- identify compute, pool, connection, or database saturation before one business authority dominates the run.

A possible `[HYPOTHESIS]` synthetic shape is:

- approximately 1,000 slots;
- hundreds or thousands of users;
- one reservation workflow per user;
- slot selection distributed across the full set;
- sufficient capacity or data reset discipline to avoid the run becoming primarily a sold-out test.

The exact figures are implementation parameters. The defining property is low contention and a broad authority distribution.

### 5.2 Hot-slot control

Many distinct users attempt to reserve one slot, or a deliberately small set of slots, at approximately the same time.

Purpose:

- expose the serialization frontier of the slot row that owns capacity;
- separate useful throughput from waits, business refusals, and timeouts;
- show why adding stateless API replicas cannot remove one shared transactional authority.

A possible `[HYPOTHESIS]` minimal shape is:

- one slot with capacity 20;
- 50–100 distinct users;
- synchronized reserve attempts;
- one unique idempotency key per logical request.

The run must distinguish admitted reservations, expected `no_capacity` refusals, timeouts, and internal failures using the closed outcome model in [measurement-contract.md](../design/measurement-contract.md) §4. `replay` is an orthogonal flag and must be reported without modelling it as a peer terminal outcome.

Expected business refusals are valid completed outcomes but are not counted as successful reservation goodput. Goodput follows [observability.md](../design/observability.md) §3.1 and covers the mutation surface rather than informational reads such as `list_slots`.

### 5.3 Hot-identity control

One identity, or a small number of identities, attempts concurrent reservations across overlapping slots.

Purpose:

- measure the cost and containment of the user-identity serialization authority introduced by AG-M1;
- demonstrate that serialization is scoped to one identity rather than global;
- distinguish identity-lock queueing from slot-capacity contention.

A possible `[HYPOTHESIS]` minimal shape is:

- one user identity;
- 20 overlapping slots;
- concurrent reserve attempts;
- exactly one admitted reservation;
- remaining domain answers classified as `schedule_conflict`, unless a bounded infrastructure outcome legitimately occurs.

Every hot-identity run must begin from a deterministic reset or fresh seed and assert that the tested identities have zero live claims before load starts. Confirmed claims are not expiry-reaped, so reusing contaminated fixtures can produce a plausible but meaningless all-conflict rerun.

A stronger mixed version may run many independent identities, each producing a small overlapping burst. The local hot-identity control is required; the AWS rerun is desirable but optional if the milestone is constrained.

### 5.4 Simplified synchronized release wave

After the controls work, AG-Sept may add one small composite workload with `[HYPOTHESIS]` parameters such as:

- around 40 slots released together;
- fixed capacity per slot;
- around 1,000 users;
- attempts beginning within a short release window;
- a deliberately skewed distribution so a few slots are hotter than average.

The first version should avoid complex browsing, alternative-choice loops, repeated retries, or fallback activities. The workload is desirable but must not displace the controlled baselines or the local scale-out experiment.

### 5.5 Explicit workload exclusions

AG-Sept does not require:

- a full fitness-club booking simulation;
- a full game inventory or shared-resource simulation;
- domain-specific names or confidential scenario details;
- user-behaviour modelling beyond the required authority distribution;
- a retry-storm model before baseline timeout behaviour is understood.

## 6. Measurement substrate

### 6.1 Aggregated service metrics

The AG-M1 observation boundary should gain a minimal aggregated recorder suitable for capacity experiments.

Required signals include:

- request count by closed-set terminal outcome;
- request-latency histogram;
- timeout, infrastructure-failure, and business-refusal counts;
- database pool state and acquisition duration;
- expiry-worker iteration outcomes and duration;
- process CPU and memory;
- relevant Go runtime signals;
- bounded replica identity where needed.

Metrics must not label by user, slot, reservation, organisation, idempotency key, request identifier, arbitrary error text, or other unbounded request content.

A Prometheus-compatible recorder is the preferred starting point, but the concrete backend remains an implementation choice.

### 6.2 Telemetry must not dominate measured latency

Before performance claims are made, AG-Sept must show that observation is not the primary request bottleneck.

Acceptable approaches include a bounded local sink, a bounded asynchronous log sink with explicit drop and shutdown behaviour, or another measured approach demonstrating low and bounded request-path overhead.

### 6.3 External load generator

The load generator must:

- execute the required workload shapes;
- control concurrency and/or offered rate;
- synchronize starts where needed;
- issue valid idempotent requests;
- capture client-side latency and terminal outcomes;
- emit machine-readable summaries;
- run separately from the service for publishable claims;
- expose enough utilisation to rule out generator saturation.

Closed-loop sweeps are sufficient initially. Open-loop rate control is desirable where it materially improves overload analysis.

### 6.4 Run manifest

Every quotable run must record:

- commit SHA and image tag;
- Go version and observed `GOMAXPROCS`;
- replica count and application resources;
- PostgreSQL version and configuration identity;
- pool size per replica and aggregate expected pool capacity;
- workload and dataset parameters;
- offered rate and/or concurrency;
- duration and warm-up;
- timeout budget and reservation TTL;
- generator location, resources, and utilisation;
- deployment topology and timestamp.

Secrets and private endpoints must not be committed.

### 6.5 Correctness reconciliation

PR1 must establish a self-check used by every later measured run. At minimum it must reconcile:

- consumed slot capacity against admitted reservation mutations;
- distinct logical idempotency keys against committed mutations and replays;
- live claims against admitted reservations per identity and interval;
- every completed request against the closed terminal-outcome set, with `replay` folded in as an orthogonal flag rather than double-counted.

A run with unreconciled client totals, server totals, or persisted state is not quotable.

## 7. Single-instance baseline

The one-replica run is the control for every scale-out claim.

A bounded sweep should cover the most informative combinations of request concurrency or offered rate, PostgreSQL pool size, application CPU, and authority distribution.

Required outputs include:

- offered requests;
- completed throughput;
- successful goodput as defined by [observability.md](../design/observability.md) §3.1;
- expected business refusals;
- p50, p95, and p99 latency;
- timeout, unknown, and internal-failure rates;
- application and database utilisation;
- generator utilisation;
- correctness reconciliation.

Peak observed throughput, SLO-safe capacity, recommended operating capacity, and scale efficiency use the normative definitions in [measurement-contract.md](../design/measurement-contract.md) §3 and §3.1 rather than local restatements.

The provisional SLO values may be challenged, but the capacity definitions must remain stable.

## 8. Local multi-instance scale-out

Run `[HYPOTHESIS]` replica counts of 1, 2, and 4 against the same PostgreSQL authority through a local load-balancing path.

For dispersed traffic, determine whether throughput and goodput increase, how scale efficiency changes, and where PostgreSQL or pool acquisition becomes dominant.

For hot-slot traffic, determine whether replicas improve useful throughput or merely add waiters, refusals, and timeout pressure, while confirming unrelated authorities still progress. For one indivisible hot slot, scale efficiency approaching `1/N` is the expected correct result.

For hot-identity traffic, confirm contention is scoped to the identity and unrelated identities continue to progress.

### 8.1 Connection-budget control

Compare:

1. a roughly constant aggregate database connection budget as replicas increase; and
2. the original full pool size multiplied per replica.

This separates application-compute scaling from changed pressure on the shared database admission boundary.

## 9. Container and Kubernetes path

### 9.1 Container gate

The service must have a reproducible, production-shaped image with immutable experiment tagging, environment-driven configuration, health probes, graceful termination, and no development-only runtime tooling.

### 9.2 Local Kubernetes gate

When Kubernetes is used, first prove the execution model locally. Useful scope includes Deployment, Service or ingress, probes, resource requests and limits, replica changes, rolling replacement, graceful termination, and basic metrics scraping.

### 9.3 Scope ceiling

AG-Sept does not require a service mesh, custom operators, multi-cluster management, GitOps platform, custom-metric autoscaling, elaborate Helm framework, production secret platform, PostgreSQL in Kubernetes, or a full tracing stack.

Amazon RDS remains the preferred AWS database target.

## 10. AWS deployment

### 10.1 Preferred topology

The preferred target is:

- image in Amazon ECR;
- Alloca replicas on Amazon EKS;
- Amazon RDS for PostgreSQL;
- an AWS load-balancing path;
- separate EC2 generator compute;
- metrics sufficient to explain application and database behaviour.

The exact region, instance classes, and sizes are `[HYPOTHESIS]` experiment inputs.

### 10.2 AWS decision gates

AWS must not consume the experiment.

By the end of the first focused AWS deployment day:

- continue with EKS if the cluster, application, database connectivity, load-balancing path, and basic metrics are operational;
- otherwise switch to a simpler AWS runtime such as ECS/Fargate or EC2-hosted containers.

After at most two focused AWS development days:

- continue to the AWS measurement matrix if the end-to-end path is stable and observable;
- otherwise stop AWS work and return the remaining budget to decisive local reruns, architecture analysis, and public documentation.

A complete, reconciled single-instance and local multi-instance evidence set is a valid AG-Sept completion. AWS is a preferred extension, not permission to weaken or delay the core evidence.

A local Kubernetes deployment may still demonstrate the execution model when AWS uses a simpler orchestration path or is stopped at the decision gate.

The evidence value comes from measuring horizontal scaling against shared authority and explaining the result, not from spending several days resolving cluster plumbing.

### 10.3 AWS smoke gate

Before capacity runs:

- migrations complete;
- readiness withholds traffic until dependencies are available;
- one end-to-end reservation workflow succeeds;
- one expected refusal is observed correctly;
- metrics and outcome totals are visible;
- the generator reaches the service from separate compute;
- no private connection data enters committed logs or reports.

## 11. AWS experiment matrix

When AWS passes the decision gates, a recommended `[HYPOTHESIS]` minimum is:

| Workload | Replica counts |
|---|---|
| Dispersed authorities | 1, 2, 4 |
| Hot slot | 1, 4 |
| Hot identity or mixed identities | 1, 4 where time permits |

The simplified synchronized release wave may replace the AWS hot-identity rerun after the local identity result is established.

Scale efficiency must compare like-for-like SLO-safe goodput, or another clearly named like-for-like point when SLO-safe capacity cannot be established precisely.

Each result must state the believed limiting mechanism and supporting evidence. Possible boundaries include application CPU, database pool acquisition, PostgreSQL connection capacity, transaction or lock wait, one slot authority, one identity authority, telemetry overhead, generator saturation, and network or load-balancer behaviour.

## 12. Negative controls

### 12.1 Response-validation control — mandatory

Deliberately supply an invalid HTTP-status and domain-outcome combination and prove that the load harness rejects the response and invalidates the run.

This control is required by [measurement-contract.md](../design/measurement-contract.md) §5 and is not descopable. It proves that response validation is active before goodput or capacity can be quoted.

### 12.2 Generator bottleneck control — mandatory

Deliberately constrain the generator and show how the apparent frontier changes. Publishable runs must then demonstrate adequate generator headroom.

### 12.3 Connection-pool multiplication control — mandatory

Compare a controlled aggregate connection budget with a configuration where each added replica receives the original full pool size.

### 12.4 Resource-limit control — desirable

Where time permits, constrain application CPU and confirm that the observed frontier changes predictably.

## 13. Architecture conclusion

The final architecture document should show external clients and generator, load balancing, stateless API replicas, PostgreSQL authority, expiry workers, metrics, and deployment boundaries.

It must connect measured behaviour to AG-M1's authority model:

```text
slot row            owns capacity
user identity row   serializes schedule mutation
claim relation      proves schedule validity
```

It should explain what remains safe across replicas, why a hot slot is not parallelized by more API instances, why one identity serializes while unrelated identities remain independent, and which boundaries are only process-local optimizations.

Potential future components may include asynchronous event publication, reporting/read models, notifications, telemetry ingestion, authority-aware admission, and maintenance for elapsed claims. For each candidate, record its consistency requirements, failure semantics, evidence for separation, and operational cost.

### 13.1 Optional service split

Do not split the reservation transaction merely to claim microservices.

The most defensible optional extraction is an asynchronous event or reporting consumer fed through a transactional outbox, demonstrating at-least-once delivery, idempotent consumption, and independent failure and scaling. Implementation is a stretch goal; a complete design is sufficient when measurements consume the budget.

The expiry worker is not an automatic microservice candidate because it shares the same domain and transaction rules and duplicate workers are already correctness-safe.

## 14. Proposed PR sequence

### PR1 — Measurement substrate and load harness

Indicative budget: 2 days. Exit when one controlled local run produces reconcilable machine-readable client, server, and persisted-state totals; the mandatory response-validation control passes; and generator and telemetry behaviour are observable.

### PR2 — Single-instance frontier

Indicative budget: 2 days. Exit when each controlled workload has a repeatable one-instance result and an identified or bounded limiting mechanism.

### PR3 — Container and local scale-out

Indicative budget: 1.5–2 days. Exit when dispersed and hot-authority traffic are compared across replica counts without changing the correctness model, including the connection-budget control.

### PR4 — AWS deployment, conditional

Indicative budget: no more than 2 days before the AWS stop gate. Exit when the service runs on AWS and accepts a controlled workload from separate generator compute with visible outcomes and metrics, or when the stop decision is documented and the remaining budget returns to local evidence.

### PR5 — Final experiment and architecture decision

Indicative budget: up to 2 days. Use AWS results when available; otherwise use decisive local reruns. Exit when a reviewer can reproduce the topology, distinguish measured facts from interpretation, and understand why replicas help or do not help for each authority distribution.

## 15. Priority tiers

### P0 — required

- reproducible external load harness;
- aggregated metrics;
- response-validation negative control;
- correctness reconciliation;
- one-instance baseline;
- dispersed and hot-slot controls;
- local hot-identity control with clean-start assertion;
- multi-instance shared-PostgreSQL comparison;
- connection-budget control;
- separate generator for publishable runs;
- evidence-led architecture conclusion;
- explicit AWS attempt/stop decision.

### P1 — strongly desirable

- meaningful AWS deployment and measurement;
- local Kubernetes before AWS;
- EKS runtime;
- mixed-identity run;
- recommended operating capacity;
- simplified synchronized release wave;
- basic dated AWS cost record;
- polished architecture and result diagrams.

### P2 — only after decisive evidence

- transactional outbox implementation;
- independently deployed consumer;
- autoscaling;
- fault injection;
- distributed tracing;
- richer dashboards;
- extensive cost optimization.

### Explicitly out of scope

- decomposing the reservation transaction;
- sharding or multiple transactional databases;
- multi-region writes;
- a broker solely to claim event-driven architecture;
- service mesh;
- complete production authentication and authorization;
- sophisticated Kubernetes platform engineering;
- eliminating the hot-authority serialization frontier;
- reproducing a full commercial workload.

## 16. Descope order

If time slips, remove work in this order:

1. outbox implementation;
2. autoscaling;
3. dashboard polish;
4. detailed cost comparison;
5. AWS hot-identity rerun;
6. simplified synchronized release wave;
7. local Kubernetes when a working AWS runtime already provides the required orchestration experiment;
8. remaining AWS implementation or reruns after the AWS stop gate.

Do not descope the response-validation control, one-instance control, dispersed-versus-hot-slot comparison, local hot-identity clean-start control, multi-instance shared-authority experiment, connection-budget control, separate generator, measurement validity, correctness reconciliation, or architecture report.

## 17. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator;
3. aggregated service and runtime metrics;
4. a single-instance report;
5. a local multi-instance report;
6. an AWS deployment guide, measured AWS result, or documented stop decision;
7. current and intended architecture diagrams;
8. authority and bottleneck analysis;
9. a service-boundary decision record;
10. a concise public repository summary;
11. explicit limitations, evidence labels, and negative-control results.

Together these should answer what is correct, what is fast under which workload, where scale-out helps, where shared authority remains the frontier, which dependency becomes limiting, and what should be separated next.

## 18. Completion gate

AG-Sept is complete when:

- required workloads run reproducibly from clean fixtures;
- single- and local multi-instance results are available;
- response validation, generator headroom, and connection-budget controls have been demonstrated;
- outcomes and persisted state reconcile;
- database connection and authority boundaries are visible;
- measured facts, calculations, and interpretation are separated;
- architecture reflects evidence rather than desired presentation;
- an AWS measurement is available or the bounded AWS stop decision is documented;
- Kubernetes and decomposition remain means rather than goals;
- the repository is suitable for public review under the disclosure policy.
