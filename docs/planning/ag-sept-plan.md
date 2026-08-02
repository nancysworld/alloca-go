# AG-Sept — Measured scale-out and distributed authority

**Status:** Draft v0.4  
**Created:** 31 July 2026  
**Delivery window:** August 2026  
**Development budget:** 10 focused development days, followed by 2–3 days for reruns, review, refinement, documentation, and public-release preparation  
**Predecessor:** AG-M1 — correct transactional core and end-to-end service path

## 1. Executive intent

AG-M1 established the correctness foundation: a runnable Go HTTP service, PostgreSQL-backed transactional authority, explicit outcome semantics, idempotency, reservation expiry, schedule non-overlap, tests, CI, and an observation boundary.

AG-Sept converts that foundation into measured distributed-systems evidence. It asks:

> How does a correctness-first stateful service behave as load and stateless replica count increase, where does horizontal scaling stop helping, and what architecture follows from the evidence?

The progression is:

1. establish a reproducible single-instance baseline;
2. scale stateless application replicas against the same PostgreSQL authority;
3. compare dispersed traffic with one hot slot and one hot identity;
4. attempt a bounded AWS deployment and rerun without displacing local evidence;
5. identify the compute, pool, database, transaction, network, or authority frontier;
6. document the architecture implied by the measurements;
7. extract a service only when doing so demonstrates a real consistency, failure, or scaling boundary.

Kubernetes and AWS are means, not the subject. The milestone succeeds through valid evidence and a defensible architectural conclusion.

## 2. Goal and completion statement

At completion, the repository should support this evidence-backed statement:

> Alloca began as a modular monolith because its central problem is transactional correctness, not service decomposition. After proving the invariants, the project measured one application instance, then scaled stateless instances against a shared PostgreSQL authority. Dispersed and hot-authority workloads were measured separately, so gains from additional compute are not confused with the serialization limit of one slot or one identity. The results were then used to decide which future boundaries justify independent deployment.

An AWS result strengthens this story but is not allowed to weaken or delay the local measurement work. A negative or surprising result is valid when the run is reproducible, the generator is ruled out as the bottleneck, correctness remains intact, and the limiting mechanism is identified or tightly bounded.

## 3. Scope principles

### 3.1 Measure mechanisms before composites

Required controlled workloads isolate one mechanism at a time:

- dispersed requests across many independent slots and identities;
- many distinct users contending for one slot;
- one identity contending across overlapping slots.

A small synchronized-release wave may follow only after the controls are established. It cannot replace them because it changes several variables at once.

### 3.2 Correctness gates remain non-negotiable

No performance result is quotable unless it preserves:

- slot capacity safety;
- one logical mutation per idempotency key;
- schedule non-overlap for one identity;
- total terminal-outcome classification;
- safe treatment of ambiguous outcomes;
- bounded request and database timeout behaviour;
- reconciliation of client totals, server totals, and persisted state.

### 3.3 Application scale is not authority scale

Adding API replicas increases stateless compute. It does not remove the serialization requirement of one hot slot or one hot identity.

Scale efficiency must therefore be reported separately for dispersed authorities, one hot slot, and one hot identity or mixed identities. The normative definitions remain in [measurement-contract.md](../design/measurement-contract.md) §3 and §3.1. For one indivisible hot authority, efficiency approaching `1/N` at `N` replicas is the expected serialization ceiling, not evidence that the whole system fails to scale.

### 3.4 Evidence-led implementation

This plan fixes questions, gates, evidence, scope limits, and PR ownership. It deliberately does not freeze exact sweep values, dashboard layout, resource sizes, Kubernetes packaging, AWS orchestration, or service boundaries before evidence supports them.

Every quantitative statement must follow the evidence labels in [measurement-contract.md](../design/measurement-contract.md) §2: `[MEASURED]`, `[DERIVED]`, or `[HYPOTHESIS]`.

## 4. Time budget

| Workstream | Days |
|---|---:|
| Measurement harness and observability | 2.0 |
| Single-instance frontier | 2.0 |
| Containerisation and local scale-out | 1.5–2.0 |
| AWS deployment path | up to 2.0 |
| AWS measurements | up to 1.5 |
| Architecture conclusions | 1.0 |

The AWS allocation is conditional. If AWS threatens the measurement substrate, single-instance baseline, local scale-out, or architecture report, its remaining budget returns to those local P0 outcomes.

A further 2–3 days are reserved for decisive reruns, review, documentation, diagrams, disclosure checks, and public-release preparation.

## 5. Required workloads

### 5.1 Dispersed-authority control

Spread requests across many slots and identities with sufficient capacity or reset discipline to avoid measuring sold-out behaviour. Use it to locate the general application, pool, PostgreSQL, or compute frontier.

### 5.2 Hot-slot control

Send synchronized reserve attempts from distinct users to one slot or a deliberately small slot set. Distinguish admitted reservations, expected `no_capacity` refusals, timeouts, unknown outcomes, and internal failures. Expected business refusals are completed outcomes but not successful mutation goodput.

### 5.3 Hot-identity control

Send one identity, or a controlled set of identities, across overlapping slots. The minimal shape expects exactly one admission and the remainder as `schedule_conflict`, except for legitimate bounded infrastructure outcomes.

Every run must begin from a deterministic reset or fresh seed and assert zero live claims for the tested identities. Confirmed claims are not expiry-reaped, so contaminated fixtures can produce plausible but meaningless all-conflict results.

### 5.4 Optional synchronized-release wave

After the controls work, a small skewed release wave may combine them. It must not displace the controlled baselines or local scale-out experiment.

### 5.5 Workload exclusions

AG-Sept does not require a full commercial booking simulation, game inventory simulation, detailed user-behaviour model, confidential scenario detail, or retry-storm model before baseline timeout behaviour is understood.

## 6. Measurement contract for the milestone

### 6.1 Service telemetry

Required bounded-cardinality signals include:

- request count by operation, terminal outcome, refusal reason, and replay disposition;
- request-latency histogram;
- timeout, infrastructure-failure, and business-refusal counts;
- database-pool state, acquire count, and acquisition duration;
- expiry-worker outcomes and duration;
- process CPU and memory;
- relevant Go runtime signals;
- bounded replica identity when multiple replicas are measured.

Never label by user, slot, reservation, organisation, idempotency key, request ID, arbitrary error text, or other unbounded request content.

### 6.2 Telemetry overhead

Before capacity claims, observation must be shown not to dominate request latency. A bounded local sink is acceptable when measured overhead is small; an asynchronous sink is added only when evidence shows it is needed.

### 6.3 External generator

The generator must execute the controlled workloads, synchronize starts where needed, issue valid idempotent requests, capture client latency and terminal outcomes, emit machine-readable summaries, run separately from the service for publishable claims, and expose enough utilisation to rule out generator saturation.

### 6.4 Run manifest

Every quotable run records code and image identity, Go version, replica count, application resources, PostgreSQL version/configuration identity, pool budget, workload and dataset parameters, concurrency or offered rate, duration and warm-up, timeout budget, reservation TTL, generator location/resources/utilisation, topology, and timestamp. Secrets and private endpoints must not be committed.

### 6.5 Correctness reconciliation

Every retained run reconciles:

- consumed slot capacity against admitted reserve mutations;
- distinct scoped idempotency keys against committed mutations and replays;
- live claims against admitted reservations per identity and interval;
- every completed request against the closed terminal-outcome set, with replay orthogonal.

An invalid, incomplete, interrupted, or unreconciled run is not quotable.

## 7. Single-instance evidence

The one-replica result is the control for every scale-out claim. Use bounded sweeps of the smallest useful set of concurrency, pool, and application-resource inputs.

Required outputs are completed throughput, successful mutation goodput, expected refusals, p50/p95/p99 latency, timeout/unknown/internal-failure rates, application and database utilisation, generator utilisation, and reconciliation.

Report peak observed throughput, SLO-safe capacity, recommended operating capacity, and the believed limiting mechanism using the normative measurement contract.

## 8. Local scale-out evidence

Compare one, two, and four stateless replicas against the same PostgreSQL authority through a reproducible local load-balancing path.

For dispersed traffic, determine whether goodput increases and where pool or PostgreSQL pressure becomes dominant. For hot-slot and hot-identity traffic, show whether replicas add useful work or merely add waiters and pressure, while confirming unrelated authorities continue to progress.

### 8.1 Connection-budget control

Compare:

1. approximately constant aggregate database connection capacity as replicas increase; and
2. the original full pool multiplied per replica.

This separates application-compute scaling from changed pressure on the shared database admission boundary.

## 9. Container and orchestration boundary

The service image must be reproducible and production-shaped: immutable experiment tagging, environment-driven configuration, probes, graceful termination, and no development-only runtime tooling.

Local Kubernetes is useful when it helps prove replica changes, probes, resource limits, rolling replacement, graceful termination, and metrics scraping. It is not required when a simpler local substrate provides the experiment cleanly.

AG-Sept does not require a service mesh, operators, multi-cluster management, GitOps, custom-metric autoscaling, elaborate Helm, PostgreSQL in Kubernetes, or a full tracing stack.

## 10. AWS path and stop gates

Preferred topology: ECR image, Alloca replicas on EKS, RDS PostgreSQL, an AWS load balancer, separate EC2 generator compute, and metrics sufficient to explain application and database behaviour.

By the end of the first focused AWS day, continue with EKS only when cluster, application, database connectivity, load balancing, and basic metrics work; otherwise switch to ECS/Fargate or EC2-hosted containers.

After at most two focused deployment days, continue to AWS measurements only when the path is stable and observable. Otherwise stop and return the remaining budget to decisive local evidence and documentation.

Before AWS capacity runs, migrations, readiness, one successful workflow, one expected refusal, metrics, separate generator reachability, and disclosure safety must pass.

## 11. AWS experiment minimum

When AWS passes its gates:

| Workload | Replica counts |
|---|---|
| Dispersed authorities | 1, 2, 4 |
| Hot slot | 1, 4 |
| Hot identity or mixed identities | 1, 4 when time permits |

Each result must state the believed limiting mechanism and supporting evidence. AWS uses the same definitions, manifests, validation, reconciliation, and evidence labels as local runs.

## 12. Negative controls

Mandatory:

- **Response validation:** prove an invalid status/outcome combination invalidates the run.
- **Generator bottleneck:** deliberately constrain the generator, show the false frontier, then demonstrate headroom in publishable runs.
- **Pool multiplication:** compare controlled aggregate connection capacity with full pool size multiplied per replica.

Desirable: constrain application CPU and confirm that the frontier moves predictably.

## 13. Architecture conclusion

The final architecture must show clients and generator, load balancing, stateless API replicas, PostgreSQL authority, expiry workers, metrics, and deployment boundaries.

It must connect evidence to the authority model:

```text
slot row            owns capacity
user identity row   serializes schedule mutation
claim relation      proves schedule validity
```

It should explain why one hot slot is not parallelized by more API replicas, why one identity serializes while unrelated identities remain independent, which optimizations are process-local, and which future components justify independent deployment.

Do not split the reservation transaction merely to claim microservices. The most defensible optional extraction is an asynchronous event or reporting consumer through a transactional outbox; implementation is a stretch goal, while a complete evidence-led decision is required.

## 14. PR sequence and owned scope

This section is the execution contract for AG-Sept. Earlier sections define technical requirements; this section assigns them to PRs so nothing required remains ownerless or is deferred accidentally. Later PRs reuse the fixtures, harness, reconciliation, metrics, and evidence conventions established earlier.

### PR1 — Measurement substrate and load harness

**Budget:** 2 days.  
**Purpose:** establish a trustworthy measurement path before producing a capacity claim.

**Scope:**

- bounded Prometheus recorder on the AG-M1 observation boundary;
- separate metrics listener for request, pool, process, runtime, and expiry-worker signals;
- measured telemetry overhead and an evidence-based synchronous/asynchronous sink decision;
- deterministic reset/seed tooling with the hot-identity clean-start assertion;
- external generator with dispersed, hot-slot, and hot-identity workloads, synchronized starts, response validation, client latency/outcome totals, generator utilisation, and machine-readable reports;
- complete run manifest;
- separate verifier with all four reconciliation checks;
- response-validation negative control;
- local operator guide covering start, seed, load, scrape, and verify.

**Evidence:** one controlled local smoke run with client report, server scrape, reconciliation verdict, telemetry-overhead artifact, and negative-control result.

**Exit gate:** client, server, and persisted-state totals reconcile; disabling validation makes the run fail; the manifest is complete; and generator and telemetry behaviour are observable.

**Not in PR1:** capacity claims, sweeps, Prometheus retention, dashboards, containers, or replica comparisons.

### PR2 — Single-instance frontier and diagnostic time series

**Budget:** 2 days.  
**Purpose:** establish the one-replica control and identify why its frontier occurs.

**Scope:**

- minimal reproducible Prometheus retention and a compact diagnostic dashboard covering throughput, mutation goodput, p95/p99 latency, outcomes, pool pressure, process CPU/memory, Go runtime state, and generator utilisation;
- version-controlled queries and a small panel set, not a general observability platform;
- bounded single-instance sweeps for dispersed, hot-slot, and hot-identity workloads;
- generator-bottleneck negative control and demonstrated generator headroom;
- the smallest useful variation of concurrency, pool size, and application resources needed to locate or bound the frontier;
- peak observed throughput, SLO-safe capacity, and recommended operating capacity;
- reconciliation and rejection of every incomplete, interrupted, invalid, or unreconciled run;
- retained reports, time-series exports or snapshots, environment details, and written interpretation separated into measured, derived, and hypothesised statements.

**Evidence:** repeatable one-instance results for all three workloads, diagnostic time series, negative controls, and a report identifying or bounding each frontier.

**Exit gate:** each workload has a repeatable reconciled result; the generator is ruled out; the time series expose or tightly bound the limiting mechanism; and the recommended operating point is reported or explicitly deferred with evidence.

**Not in PR2:** replica scaling, Kubernetes, AWS, rich dashboards, alerting, or a composite release wave.

### PR3 — Container and local scale-out

**Budget:** 1.5–2 days.  
**Purpose:** measure stateless application scaling against the unchanged shared PostgreSQL authority.

**Scope:**

- production-shaped immutable service image;
- smallest reproducible local load-balancing/orchestration path for one, two, and four replicas;
- reuse PR2's Prometheus/dashboard path, adding bounded replica identity and only the per-replica and aggregate panels needed for diagnosis;
- dispersed, hot-slot, and local hot-identity comparisons across replica counts;
- mandatory connection-budget control;
- proof that unrelated authorities progress while one authority serializes;
- like-for-like scale efficiency and separation of application-compute gains from changed database admission pressure;
- complete manifests, reconciliation, topology, and time-series evidence for retained runs.

**Evidence:** local multi-instance report with replica matrix, connection-budget control, per-workload scale efficiency, correctness verdicts, and supporting frontier evidence.

**Exit gate:** replica comparisons preserve the correctness model; pool multiplication is explained; one hot authority is shown as a scoped serialization limit rather than a global scaling result; and all quoted runs remain reproducible and reconciled.

**Not in PR3:** AWS, autoscaling, production Kubernetes platform engineering, service mesh, or eliminating the hot-authority frontier.

### PR4 — AWS deployment and measurement, conditional

**Budget:** no more than 2 deployment days before the stop gate, plus up to 1.5 measurement days only when the gate passes.  
**Purpose:** repeat a meaningful subset on production-shaped separate compute without allowing cloud plumbing to consume the milestone.

**Scope before the gate:**

- immutable image in ECR;
- application runtime, load balancer, RDS, migrations, readiness, graceful shutdown, metrics collection, and separate EC2 generator;
- EKS preferred, with ECS/Fargate or EC2 fallback at the first-day gate;
- AWS smoke gate;
- topology, resources, region, versions, pool budget, and dated basic cost snapshot without secrets or private endpoints.

**Scope after the gate:**

- dispersed at one, two, and four replicas;
- hot slot at one and four;
- hot identity or mixed identities only when time remains;
- reuse of the local measurement, reconciliation, and visualization contracts;
- comparison of local conclusions against network, load-balancer, cloud compute, and managed-database boundaries.

**Evidence:** either reproducible AWS deployment and reconciled measurements with a basic cost record, or a dated stop-decision report describing the failed gate and budget reassignment.

**Exit gate:** a controlled workload runs from separate compute with visible metrics and reconciled outcomes and the minimum subset is measured; or the bounded stop decision is documented early enough for PR5 to strengthen local evidence.

**Not in PR4:** multi-region operation, production security completeness, managed observability expansion, custom autoscaling, or prolonged EKS troubleshooting.

### PR5 — Final experiments, architecture decision, and public evidence

**Budget:** up to 2 focused development days plus the reserved 2–3 refinement days.  
**Purpose:** turn the accumulated evidence into the clearest defensible account of Alloca's scaling behaviour and next architecture boundary.

**Scope:**

- use AWS results when available; otherwise spend returned budget on decisive local reruns, stronger controls, or narrower uncertainty bounds;
- add the synchronized-release wave only when the controlled story is complete and it adds explanatory value;
- resolve contradictions and rerun unstable measurements;
- produce final one-instance, local scale-out, and optional AWS comparison tables and only the charts needed for decisive findings;
- update architecture diagrams and connect each frontier to the slot-row, identity-row, and claim-relation model;
- record the service-boundary decision, including why the reservation transaction remains together and whether an asynchronous reporting/event boundary is justified;
- update repository entry point, limitations, reproduction instructions, evidence labels, negative controls, cost notes, and public-disclosure checks.

**Evidence:** a public-ready set in which raw artifacts support every report and architecture claim, with explicit limitations and no scale claim crossing incomparable workload shapes.

**Exit gate:** a reviewer can reproduce the topology and decisive runs, distinguish measurements from calculations and interpretation, see where replicas help and where one authority serializes, and follow the evidence to the architecture decision.

**Not in PR5:** feature expansion for presentation value, speculative decomposition, rich dashboard work unrelated to a finding, or reopening settled measurement definitions.

## 15. Priority tiers

### P0 — required

- reproducible external load harness;
- aggregated bounded-cardinality metrics;
- minimal reproducible time-series retention and diagnostic dashboard;
- response-validation and generator-bottleneck controls;
- correctness reconciliation;
- one-instance baseline for all three controlled workloads;
- local one/two/four-replica shared-PostgreSQL comparison;
- connection-budget control;
- separate generator for publishable runs;
- evidence-led architecture conclusion;
- explicit AWS attempt/stop decision.

### P1 — strongly desirable

- meaningful AWS deployment and measurement;
- local Kubernetes before AWS where useful;
- EKS runtime;
- mixed-identity run;
- recommended operating capacity;
- synchronized-release wave;
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

Remove work in this order when time slips:

1. outbox implementation;
2. autoscaling;
3. dashboard polish beyond the diagnostic minimum;
4. detailed cost comparison;
5. AWS hot-identity rerun;
6. synchronized-release wave;
7. local Kubernetes when another working runtime provides the experiment;
8. remaining AWS implementation or reruns after the stop gate.

Do not descope response validation, generator headroom, one-instance controls, dispersed-versus-hot-slot comparison, local hot-identity clean start, local multi-instance shared-authority comparison, connection-budget control, diagnostic time-series visibility, correctness reconciliation, or the architecture report.

## 17. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator and deterministic fixture tools;
3. aggregated service and runtime metrics;
4. a minimal reproducible time-series retention path and diagnostic dashboard;
5. a single-instance report;
6. a local multi-instance report;
7. an AWS deployment guide, measured AWS result, or documented stop decision;
8. current and intended architecture diagrams;
9. authority and bottleneck analysis;
10. a service-boundary decision record;
11. a concise public repository summary;
12. explicit limitations, evidence labels, manifests, and negative-control results.

## 18. Completion gate

AG-Sept is complete when:

- required workloads run reproducibly from clean fixtures;
- single- and local multi-instance results are available;
- response validation, generator headroom, and connection-budget controls are demonstrated;
- client outcomes, server totals, and persisted state reconcile;
- database connection and authority boundaries are visible in retained totals and diagnostic time series;
- measured facts, calculations, and interpretation are separated;
- architecture follows evidence rather than presentation goals;
- an AWS measurement is available or the bounded AWS stop decision is documented;
- Kubernetes and decomposition remain means rather than goals;
- the repository is suitable for public review under the disclosure policy.
