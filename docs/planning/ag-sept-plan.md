# AG-Sept — Measured scale-out and distributed authority

**Status:** Draft v0.1  
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
4. deploy and repeat a meaningful subset of the experiment on AWS;
5. identify the database, connection, compute, or authority boundary that limits each workload;
6. document the distributed-system shape implied by the measurements;
7. extract a service only if doing so demonstrates a real consistency, failure, or scaling boundary.

Kubernetes may be used as the deployment substrate, but Kubernetes is not the subject of the milestone. The milestone succeeds by producing valid evidence and a defensible architectural conclusion, not by touching the largest number of infrastructure technologies.

## 2. Goal and exit statement

AG-Sept proves how Alloca's correctness authorities behave under load and horizontal scale.

At completion, the repository should support the following evidence-backed statement:

> Alloca began as a modular monolith because its central problem is transactional correctness, not service decomposition. After proving the invariants, the project measured one application instance, then scaled stateless instances against a shared PostgreSQL authority. Dispersed and hot-authority workloads were measured separately, so improvements from additional compute are not confused with the serialization limit of one slot or one identity. The same architecture was deployed on AWS, and the results were used to decide which future boundaries justify independent deployment.

The measurements may reject an initial performance or scaling hypothesis. A negative or surprising result is a valid milestone outcome when the experiment is reproducible, the generator is ruled out as the bottleneck, correctness remains intact, and the limiting mechanism is identified.

## 3. Scope principles

### 3.1 Measure mechanisms before realistic composites

AG-Sept uses simplified controlled workloads inspired by real synchronized-release and hot-resource patterns. It does not reproduce a full product workflow.

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

## 4. Time budget and priority

The ten development days are allocated approximately as follows:

| Workstream | Days | Weight |
|---|---:|---:|
| Measurement harness and observability | 2.0 | 20% |
| Single-instance capacity baseline | 2.0 | 20% |
| Containerisation and local multi-instance experiment | 1.5 | 15% |
| AWS and Kubernetes deployment path | 2.0 | 20% |
| AWS scale-out experiments | 1.5 | 15% |
| Architecture conclusions and one justified boundary | 1.0 | 10% |
| **Total** | **10.0** | **100%** |

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

A possible synthetic shape is:

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

A possible minimal shape is:

- one slot with capacity 20;
- 50–100 distinct users;
- synchronized reserve attempts;
- one unique idempotency key per logical request.

The run must distinguish:

- admitted reservations;
- expected `no_capacity` refusals;
- database or client timeouts;
- internal failures;
- replays, if any.

Expected business refusals are valid completed outcomes but are not counted as successful reservation goodput.

### 5.3 Hot-identity control

One identity, or a small number of identities, attempts concurrent reservations across overlapping slots.

Purpose:

- measure the cost and containment of the user-identity serialization authority introduced by AG-M1;
- demonstrate that serialization is scoped to one identity rather than global;
- distinguish identity-lock queueing from slot-capacity contention.

A possible minimal shape is:

- one user identity;
- 20 overlapping slots;
- concurrent reserve attempts;
- exactly one admitted reservation;
- remaining domain answers classified as `schedule_conflict`, unless a bounded infrastructure outcome legitimately occurs.

A stronger mixed version may run many independent identities, each producing a small overlapping burst. That version should demonstrate that different identities continue to make progress even though each identity's own mutations serialize.

The hot-identity control is required locally. Repeating it on AWS is desirable but may be reduced to a smaller matrix if the milestone is constrained.

### 5.4 Simplified synchronized release wave

After the three controls work, AG-Sept may add one small composite workload representing a synchronized booking release.

A possible shape is:

- around 40 slots released together;
- fixed capacity per slot;
- around 1,000 users;
- attempts beginning within a short release window;
- a deliberately skewed distribution so a few slots are hotter than average.

The first version should avoid complex browsing, alternative-choice loops, repeated retries, or fallback activities. Those behaviours can obscure the primary scaling question and turn the generator into a separate product simulation.

This workload is desirable because it shows how independent and hot authorities interact. It is not required if implementing it would displace the controlled baselines or AWS experiment.

### 5.5 Explicit workload exclusions

AG-Sept does not require:

- a full fitness-club booking simulation;
- a full game inventory or shared-chest simulation;
- domain-specific names or confidential scenario details in public documentation;
- user-behaviour modelling beyond what is needed to generate the authority distribution;
- a retry storm model before the baseline timeout and outcome behaviour are understood.

## 6. Measurement substrate

### 6.1 Aggregated service metrics

The AG-M1 observation boundary should gain a minimal aggregated recorder suitable for capacity experiments.

Required signals include:

- request count by closed-set terminal outcome;
- request-latency histogram;
- timeout and infrastructure-failure counts;
- business-refusal counts by closed-set reason where useful;
- database pool acquired, idle, and total connection state;
- database acquisition duration;
- expiry-worker iterations, expired records, duration, and failures;
- process CPU and memory;
- Go runtime signals relevant to the experiment;
- replica or instance identity where a bounded distinction is needed.

Metrics must not label by:

- user, slot, reservation, organisation, or idempotency identifier;
- request identifier;
- arbitrary error text;
- other unbounded request content.

The exact metrics backend is an implementation choice. A Prometheus-compatible recorder is the preferred starting point because it supports local and Kubernetes-based experiments without coupling the domain or handlers to an exporter.

### 6.2 Telemetry must not dominate measured latency

AG-M1 deliberately left structured emission synchronous. Before performance claims are made, AG-Sept must show that the observation path is not the primary request bottleneck.

Acceptable approaches include:

- directing structured logs to a bounded local sink while using in-process metrics for the experiment;
- introducing a bounded asynchronous log sink with an explicit drop policy and shutdown-flush behaviour;
- another measured approach that demonstrates low and bounded request-path overhead.

The milestone does not require a general telemetry platform. It requires confidence that the measurement mechanism does not materially create the result being measured.

### 6.3 External load generator

The load generator must be able to:

- execute the required workload shapes;
- control concurrency and/or offered rate;
- synchronize starts where the workload requires a release wave;
- issue semantically valid idempotent requests;
- capture client-side latency and terminal outcomes;
- emit machine-readable run summaries;
- run on compute separate from the service for publishable capacity claims;
- expose enough of its own utilisation to rule out generator saturation.

The first implementation may support closed-loop concurrency sweeps. Open-loop offered-rate control is desirable where it materially improves overload analysis, but it must not jeopardize the core milestone.

### 6.4 Run manifest

Every quotable run must record enough provenance to reproduce or compare it.

Required fields include:

- commit SHA and image tag;
- Go version and observed `GOMAXPROCS`;
- replica count;
- application CPU and memory configuration;
- PostgreSQL version and relevant instance/configuration identity;
- database pool size per replica and aggregate expected pool capacity;
- workload shape and data-set parameters;
- offered rate and/or concurrency;
- run duration and warm-up interval;
- timeout budget;
- reservation TTL;
- generator location, resource configuration, and utilisation;
- deployment topology;
- experiment timestamp and environment name.

Secrets and private endpoints must not be recorded in committed manifests.

## 7. Single-instance baseline

### 7.1 Purpose

The one-replica run is the control for every horizontal-scaling claim. AG-Sept must not jump directly to a multi-replica AWS graph.

### 7.2 Sweep dimensions

The implementation should choose a bounded matrix covering the most informative dimensions, including some combination of:

- request concurrency;
- offered rate;
- PostgreSQL pool size;
- application CPU allocation;
- workload authority distribution.

The matrix should be small enough to complete and repeat. Broad exploratory sweeps may identify the frontier; a smaller decisive set should be rerun for the report.

### 7.3 Required outputs

For each selected configuration, report at least:

- offered requests;
- completed throughput;
- successful reservation goodput;
- expected business refusals;
- p50, p95, and p99 client-visible latency;
- client and database timeout rates;
- unknown and internal-failure rates;
- application CPU and memory utilisation;
- database connection and acquisition behaviour;
- generator utilisation;
- correctness reconciliation.

Where practical, database transaction or lock-wait evidence should be added when it helps attribute the frontier. AG-Sept does not require a tracing platform before such evidence can be collected.

### 7.4 Capacity terms

The report must distinguish:

- **peak observed throughput:** the highest throughput seen in the selected runs;
- **SLO-safe capacity:** the highest sustained load satisfying the provisional latency, timeout, correctness, saturation, and headroom gates;
- **recommended operating capacity:** a conservative level below SLO-safe capacity, preserving operational headroom.

The provisional SLO values may be challenged by the experiment. The capacity definitions must remain stable even if the numeric thresholds change through an explicit documented decision.

## 8. Local multi-instance scale-out

### 8.1 Topology

Run 1, 2, and 4 stateless application replicas against the same PostgreSQL transactional authority. A local load balancer or reverse proxy distributes requests.

Docker Compose is sufficient for the first topology proof. A local Kubernetes cluster may follow when it contributes directly to the AWS deployment path.

### 8.2 Questions for dispersed traffic

- Does completed throughput and goodput increase with replica count?
- How does scale efficiency change from 1 to 2 to 4 replicas?
- Does application CPU cease to be the limiting resource?
- At what point does PostgreSQL, pool acquisition, or another shared dependency dominate?
- Are per-replica pools multiplying the aggregate database connection demand?

### 8.3 Questions for a hot slot

- Does adding replicas improve successful reservation goodput?
- Does it only increase concurrent waiters, refusals, or timeout pressure?
- Is the slot-row authority visible as the limiting boundary?
- Are unrelated slots still able to progress when the hot slot is saturated?

### 8.4 Questions for a hot identity

- Is contention contained to the identity being mutated?
- Do unrelated identities continue to scale?
- Does the identity lock create bounded queueing without causing global collapse?
- Are schedule-conflict outcomes reconciled with persisted claims?

### 8.5 Connection-budget control

At least one comparison must test the difference between:

1. holding the aggregate database connection budget roughly constant while increasing replica count; and
2. naively multiplying the same per-replica pool size across all replicas.

This control should reveal whether apparent scaling or degradation is caused by application compute or by changing pressure on the shared database admission boundary.

## 9. Container and Kubernetes path

### 9.1 Container gate

The service must have a production-shaped container image with:

- a reproducible multi-stage build or equivalent small runtime image;
- immutable tagging by commit SHA for experiments;
- non-root execution where practical;
- environment-driven configuration;
- liveness and readiness support;
- graceful termination behaviour;
- no development-only tooling in the runtime image.

### 9.2 Local Kubernetes gate

If Kubernetes is used, first prove the execution model locally with a small cluster such as `kind` or `k3d`.

The useful scope is:

- Deployment;
- Service or ingress/load-balancing path;
- liveness and readiness probes;
- resource requests and limits;
- replica-count changes;
- rolling replacement and graceful termination;
- basic metrics scraping where straightforward;
- configuration and secret boundaries appropriate to an experiment.

### 9.3 Kubernetes scope ceiling

AG-Sept does not require:

- a service mesh;
- custom operators;
- multi-cluster management;
- a GitOps platform;
- custom-metric autoscaling;
- an elaborate Helm framework;
- a production secret-management system;
- PostgreSQL running inside Kubernetes;
- a full tracing stack.

Amazon RDS remains the preferred PostgreSQL deployment because the experiment concerns application scale against a shared transactional authority, not stateful Kubernetes operations.

## 10. AWS deployment

### 10.1 Preferred topology

The preferred target is:

- container image in Amazon ECR;
- Alloca application replicas on Amazon EKS;
- Amazon RDS for PostgreSQL;
- an AWS load-balancing path;
- the load generator on separate EC2 compute;
- metrics collection sufficient to explain application and database behaviour.

The exact region, instance classes, and resource sizes are experiment inputs, not plan commitments.

### 10.2 EKS decision gate

EKS must not consume the experiment.

By the end of the first focused AWS deployment day:

- if the cluster, application, database connectivity, load-balancing path, and basic metrics are operational, continue with EKS;
- if incidental EKS setup is preventing the measurement work, switch to a simpler AWS runtime such as ECS/Fargate or EC2-hosted containers and complete the scale-out experiment.

A local Kubernetes deployment may still demonstrate Kubernetes understanding when AWS uses a simpler orchestration path.

The portfolio value comes primarily from measuring horizontal scaling against shared authority and explaining the result, not from spending several days resolving cluster plumbing.

### 10.3 AWS smoke gate

Before capacity runs:

- migrations complete successfully;
- readiness withholds traffic until dependencies are available;
- one end-to-end reservation workflow succeeds;
- one expected business refusal is observed correctly;
- metrics and client outcome totals are visible;
- the generator reaches the service from separate compute;
- no private connection data enters committed logs or reports.

## 11. AWS experiment matrix

The final AWS matrix should be deliberately small and decisive.

A recommended minimum is:

| Workload | Replica counts |
|---|---|
| Dispersed authorities | 1, 2, 4 |
| Hot slot | 1, 4 |
| Hot identity or mixed identities | 1, 4 where time permits |

The simplified synchronized release wave may replace the AWS hot-identity rerun if it provides greater portfolio value after the local identity result is established.

### 11.1 Horizontal scale efficiency

For replica count `N`:

```text
scale efficiency at N replicas =
    measured SLO-safe goodput at N replicas
    ----------------------------------------
    N × measured SLO-safe goodput at 1 replica
```

If the available matrix cannot establish SLO-safe capacity precisely for every workload, the report may use another clearly named like-for-like throughput point. It must not silently mix peak throughput, goodput, or different SLO conditions.

### 11.2 Required interpretation

Each result must state which mechanism is believed to limit the run and what evidence supports that interpretation.

Possible boundaries include:

- application CPU;
- application memory or throttling;
- database pool acquisition;
- PostgreSQL connection capacity;
- transaction or lock wait;
- one slot authority;
- one identity authority;
- telemetry overhead;
- load-generator saturation;
- network or load-balancer behaviour.

Interpretation must be separated from measured facts and derived calculations according to the measurement contract.

## 12. Negative controls

### 12.1 Generator bottleneck control

Deliberately constrain or under-provision the generator and show how the apparent service frontier changes. The publishable run must then demonstrate adequate generator headroom.

This control prevents a client-limited plateau from being reported as server capacity.

### 12.2 Connection-pool multiplication control

Compare a controlled aggregate database connection budget with a configuration where each added replica receives the original full pool size.

This control demonstrates whether the replica-count result is partly a database-admission experiment.

### 12.3 Resource-limit control

Where time permits, constrain application CPU and show that the observed frontier or latency changes predictably.

This is useful evidence that the harness can detect a known bottleneck. It is desirable, not mandatory when the first two controls and decisive runs already consume the available budget.

## 13. Architecture conclusion

### 13.1 Current distributed shape

The final architecture document should show:

- external clients and load generator;
- load-balancing boundary;
- stateless Alloca API replicas;
- PostgreSQL as shared transactional authority;
- expiry workers;
- metrics and reporting path;
- deployment and configuration boundaries.

### 13.2 Authority model

The document must connect measured behaviour to AG-M1's three-authority model:

```text
slot row            owns capacity
user identity row   serializes schedule mutation
claim relation      proves schedule validity
```

It should explain:

- which state and invariants remain safe across multiple application replicas;
- why one hot slot cannot be parallelized merely by adding API instances;
- why one identity's mutations serialize while unrelated identities remain independent;
- why the exclusion constraint remains the final validity authority even when the identity row serializes cooperating writers;
- which boundaries are process-local optimizations rather than cross-node authorities.

### 13.3 Future component candidates

Potential future components may include:

- asynchronous event publication;
- reporting or read models;
- notifications;
- load generation;
- telemetry ingestion;
- authority-aware admission or routing;
- maintenance for elapsed claims that never receive another identity-scoped operation.

For each candidate discussed, state:

- the reason it could be deployed independently;
- whether it participates in the correctness transaction;
- expected delivery and failure semantics;
- whether AG-Sept evidence supports separation now;
- what new operational cost the split introduces.

### 13.4 Optional service split

AG-Sept must not split the reservation write transaction merely to claim a microservice architecture.

The most defensible optional extraction is an asynchronous event or reporting consumer fed by a transactional outbox. Such a boundary would demonstrate:

- separation of correctness-critical mutation from non-critical downstream processing;
- at-least-once delivery;
- idempotent consumption;
- independent failure and scaling behaviour;
- a service boundary with a real consistency contract.

Implementation is a stretch goal. A complete design may satisfy this part of the milestone when the core measurements consume the development budget.

The expiry worker is not an automatic microservice candidate. It uses the same domain and transactional rules, and duplicate workers are already correctness-safe. Deployment separation without an authority, scaling, or failure reason would add operational surface without strengthening the architecture.

## 14. Proposed PR sequence

The PR boundaries may be adjusted as implementation knowledge improves, but the milestone should avoid one large ten-day PR.

### PR1 — Measurement substrate and load harness

Indicative budget: 2 days.

Possible contents:

- aggregated metrics recorder;
- runtime and pool signals;
- measured treatment of synchronous telemetry overhead;
- load-generator foundation;
- run-manifest format;
- one short reconciliation smoke run.

Exit gate:

> One controlled local run produces machine-readable client and server totals that reconcile, while generator and telemetry behaviour are observable.

### PR2 — Single-instance frontier

Indicative budget: 2 days.

Possible contents:

- dispersed, hot-slot, and hot-identity fixtures;
- sweep automation;
- generator negative control;
- first single-instance measurement report;
- recommended operating point or explicit explanation of why the provisional SLO prevents one.

Exit gate:

> Each controlled workload has a repeatable one-instance result and an identified or bounded limiting mechanism.

### PR3 — Container and local scale-out

Indicative budget: 1.5–2 days.

Possible contents:

- runtime container;
- local load-balancing topology;
- optional local Kubernetes manifests;
- 1/2/4 replica comparison;
- connection-budget control;
- local scale-efficiency report.

Exit gate:

> Dispersed and hot-authority traffic are compared across replica counts without changing the correctness model.

### PR4 — AWS deployment

Indicative budget: 2 days.

Possible contents:

- ECR image publication;
- EKS deployment or documented fallback runtime;
- RDS connectivity and migration path;
- probes, resource configuration, and graceful shutdown;
- external-generator smoke test;
- reproducible deployment instructions or infrastructure definition.

Exit gate:

> The end-to-end service runs on AWS and accepts a controlled workload from separate generator compute with visible outcomes and metrics.

### PR5 — AWS experiment and architecture decision

Indicative budget: 2 days.

Possible contents:

- final AWS matrix;
- result tables and graphs;
- limitations and negative controls;
- current and intended architecture diagrams;
- service-boundary decision;
- concise cost inputs where useful and reproducible;
- portfolio-facing README summary.

Exit gate:

> A reviewer can reproduce the topology, distinguish measured facts from interpretation, and understand why replicas help or do not help for each authority distribution.

## 15. Priority tiers

### 15.1 P0 — required for AG-Sept completion

- reproducible external load harness;
- aggregated metrics adequate to explain the experiment;
- one-instance baseline;
- dispersed-authority and hot-slot controls;
- local hot-identity control;
- multi-instance comparison against shared PostgreSQL;
- generator separation for publishable results;
- meaningful AWS deployment and measured run;
- architecture conclusion derived from evidence;
- clear limitations and negative controls;
- correctness reconciliation.

### 15.2 P1 — strongly desirable

- local Kubernetes deployment before AWS;
- EKS as the AWS runtime;
- explicit aggregate-connection-budget experiment;
- mixed-identity scaling run;
- recommended operating capacity;
- simplified synchronized release wave;
- basic dated AWS cost record;
- polished architecture and results diagrams.

### 15.3 P2 — only after the decisive evidence is complete

- transactional outbox implementation;
- independently deployed consumer;
- autoscaling experiment;
- fault injection;
- distributed tracing;
- richer Grafana dashboards;
- more extensive cost optimization.

### 15.4 Explicitly out of scope

- decomposing the reservation transaction;
- sharding or multiple transactional databases;
- multi-region writes;
- Kafka or another broker solely to claim event-driven architecture;
- service mesh;
- complete production authentication and authorization;
- sophisticated Kubernetes platform engineering;
- eliminating the hot-authority serialization frontier;
- reproducing a full commercial workload.

The hot-authority frontier is something to demonstrate, measure, and explain. AG-Sept does not need to remove a serialization requirement that exists to preserve correctness.

## 16. Descope order

If time slips, remove work in this order:

1. transactional-outbox implementation;
2. autoscaling;
3. dashboard polish;
4. detailed cost comparison;
5. AWS hot-identity rerun;
6. simplified synchronized release wave;
7. local Kubernetes, when a working AWS runtime already provides the required orchestration experiment.

Do not descope:

- the one-instance control;
- dispersed versus hot-slot comparison;
- multi-instance shared-authority experiment;
- generator separation;
- measurement validity;
- correctness reconciliation;
- the architecture report.

## 17. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator;
3. aggregated service and runtime metrics;
4. a single-instance measurement report;
5. a local multi-instance scale-out report;
6. an AWS deployment guide or infrastructure definition;
7. a measured AWS scale-out report;
8. current and intended architecture diagrams;
9. an authority and bottleneck analysis;
10. a service-boundary decision record;
11. a concise portfolio-facing repository summary;
12. explicit limitations, evidence labels, and negative-control results.

Together these deliverables should answer:

- What is correct?
- What is fast, under which workload and resource configuration?
- Where does horizontal scaling help?
- Where does one shared authority remain the frontier?
- Which shared dependency becomes limiting as replicas are added?
- What should be separated next, and what should deliberately remain together?

## 18. Completion gate

AG-Sept is complete when all of the following are true:

- the required controlled workloads run reproducibly;
- single-instance and multi-instance results are available;
- at least one meaningful AWS capacity comparison has been completed;
- the load generator has been ruled out as the bottleneck for quoted results;
- request outcomes and persisted state reconcile;
- the database connection and authority boundaries are visible in the analysis;
- measured facts, derived calculations, and interpretations are separated;
- the architecture document reflects the evidence rather than the desired presentation;
- Kubernetes and service decomposition have remained means rather than milestone goals;
- the repository is suitable for public review under the disclosure policy.
