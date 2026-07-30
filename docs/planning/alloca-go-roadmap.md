# Alloca-Go Roadmap

**Status:** Draft v0.3  
**Created:** 20 July 2026  
**Delivery window:** 20 July–28 August 2026 inclusive  
**Predecessor:** prior RuntimeIQ-Alloca prototype work and archived planning documents
(lineage: [high-level design](../design/high-level-design.md) §1.1)

## 1. Executive intent

Alloca-Go is a production-shaped distributed reservation system in Go.

It is not a line-by-line port of the earlier prototype. The prior work remains an experimental record and evidence base; Alloca-Go starts a new implementation whose purpose is to answer four connected questions:

1. How should scarce or conserved state be owned and mutated correctly under concurrency?
2. What is the sustainable, SLO-compliant throughput of one production capacity unit?
3. How efficiently can those units scale horizontally before a shared dependency or hot authority becomes the bottleneck?
4. How should the system degrade under overload so users receive explicit, bounded outcomes instead of latency growth, timeouts, and generic errors?

The project uses two synthetic representative workloads:

- a fitness-club synchronized booking release across approximately 200 independent organisations, each modelled with around 5,000 potential participants;
- a shared-resource workload where thousands of users contend for inventory quantities or a conserved balance.

These are engineering models, not claims about any organisation's internal architecture, scale, traffic, or implementation.

The 40-day plan prioritises a defensible end-to-end system, measured capacity economics, and a clear public technical record over broad feature coverage.

## 2. Starting evidence and open hypotheses

### 2.1 Prior prototype evidence — not yet reproduced in this repository

The following figures are `[PRIOR-UNREPRODUCED]` inputs from the RuntimeIQ-Alloca prototype ([high-level design](../design/high-level-design.md) §1.1). They must not be treated as Alloca-Go results until reproduced from this repository:

- A single-worker prototype `/health` path sustained approximately 2,050 requests per second when measured with a corrected Go client.
- A minimal Go `net/http` service on the same machine completed at least 61 times as many successful responses. This was a client-limited, stateless, whole-stack comparison—not a language-only or booking-path comparison.
- Prototype booking work was dominated by database transactions and contention rather than the HTTP framework; an approximate reserve rate near 125 operations per second was inferred, not directly measured as a Go booking-path result.
- Booking latency grew approximately linearly with concurrency in the measured range, without a clear throughput knee before timeouts became the visible degradation mode.
- Arrival spread materially changed booking outcomes, showing that concurrency, arrival rate, contention, timeout policy, and load-generator behaviour must be separated experimentally.

### 2.2 Interpretation

Go is justified as the production boundary because it offers substantial request-handling headroom, efficient concurrency, predictable deployment, and a strong operational profile.

The project must not assume that a stateless endpoint improvement will translate directly to a transactional booking-path improvement. The bottleneck may move to PostgreSQL, lock acquisition, connection capacity, authority serialization, telemetry, or load generation.

### 2.3 Hypothesis to test

The prior latency curve suggests that overload may manifest as requests waiting until one of several timeouts expires.

Alloca-Go will reproduce or reject that mechanism under controlled conditions, then compare it with bounded alternatives such as admission control, explicit refusal, retry guidance, or queue-position feedback.

## 3. Core theses

### 3.1 Correct authority before distribution

Every scarce or conserved resource must have an unambiguous write authority, such as:

- a class slot and its capacity;
- a reservation hold and its expiry;
- a shared inventory item quantity;
- a conserved group balance.

Horizontal scaling is safe when independent authorities can be routed and processed independently. Adding API nodes does not remove the serialization limit of one hot authority.

### 3.2 Capacity efficiency and horizontal scaling are complementary

A distributed architecture provides a mechanism to scale out. It does not determine whether each unit of scale is efficient.

Alloca-Go will optimise for:

> sustainable, SLO-compliant, resilient throughput per unit cost

Higher throughput per node can reduce compute cost, database connections, telemetry volume, deployment coordination, restart frequency, autoscaling churn, and the number of operational objects in the system.

The goal is not maximum single-node benchmark throughput at any price. The recommended capacity unit must also preserve failure isolation, deployment safety, scaling granularity, and N+1 headroom.

### 3.3 Goodput matters more than accepted load

Reports will distinguish:

- offered requests;
- admitted requests;
- completed responses;
- successful domain operations;
- expected business refusals;
- timeouts;
- unknown commit outcomes;
- internal failures;
- idempotent replays.

Peak accepted request rate is not a capacity result when requests later time out, violate correctness gates, or amplify retries.

### 3.4 Overload must be explicit and bounded

Under overload, the service should prefer an explicit bounded outcome such as:

- admitted;
- sold out or insufficient balance;
- retry after a bounded delay;
- admission rejected because the queue is full;
- queue position or release token;
- operation outcome unknown, safe to replay with the same idempotency key.

## 4. Initial architecture hypothesis

The initial implementation is a modular monolith. Decomposition follows evidence rather than presentation value.

```text
external Go load generator
            |
            v
   AWS Application Load Balancer
            |
            v
 stateless Go API capacity units
            |
            +-- request admission and per-node ordering
            +-- reservation / shared-resource domain services
            +-- idempotency and outcome classification
            |
            v
 PostgreSQL transactional authority
            |
            +-- authority rows / partitions / shards
            +-- background expiry / settlement workers
            +-- audit and operational records

Observability: OpenTelemetry-compatible metrics and traces -> CloudWatch
Delivery:      GitHub Actions -> container registry -> ECS
Infrastructure: Terraform
```

### 4.1 Starting components

- **Go HTTP API:** stateless request handling and domain orchestration.
- **Domain layer:** slot, reservation, booking, inventory, balance, and idempotency invariants.
- **PostgreSQL repository:** transactional source of truth and cross-node serialization authority.
- **In-process authority router:** a per-node ordering, batching, or admission optimisation only. Until a routing tier guarantees same-key convergence, it cannot serialize an authority across the fleet.
- **Background worker:** reservation expiry and settlement outside the user request path.
- **External load generator:** open-loop and closed-loop modes, synchronized release, response validation, and negative controls.
- **Telemetry:** end-to-end latency, transaction time, lock wait, pool wait, queue depth, timeouts, outcomes, saturation, and cost inputs.

A serialized command lane or actor-like authority becomes a cross-node authority only after routing guarantees that all requests for the same key converge on one owner.

### 4.2 Candidate authority and shard keys

- Fitness-club release: `organisation_id`, then `slot_id` within the organisation.
- Shared-resource workload: `group_id`, then possibly `item_id`; a group-wide conserved balance may remain a group-level authority.

The project will measure whether a coarser authority is operationally simpler and sufficient, or whether finer partitioning materially improves useful throughput without compromising invariants.

### 4.3 Initial AWS platform

The first production slice will use:

- Amazon ECS, initially with Fargate task sizes;
- an Application Load Balancer;
- Amazon RDS for PostgreSQL;
- Terraform;
- GitHub Actions;
- OpenTelemetry-compatible instrumentation and CloudWatch.

Kubernetes, Redis, DynamoDB, Kafka, multi-region writes, and dynamic shard movement are deferred until evidence demonstrates a need.

### 4.4 Go runtime capacity controls

The project will pin the Go toolchain version. For Go 1.25 or later, container-aware `GOMAXPROCS` defaults are expected when no explicit override disables them and the task exposes a CPU limit.

Every capacity run must record:

- Go version;
- configured task vCPU;
- observed `runtime.GOMAXPROCS(0)`;
- whether `GOMAXPROCS` or relevant `GODEBUG` settings were explicitly set;
- CPU throttling and utilisation.

Explicit `GOMAXPROCS` configuration or an external helper is required only when deliberately overriding the runtime default or testing an older Go version.

## 5. Measurement vocabulary

### 5.1 Offered load

Requests generated per second, regardless of whether the service can admit or complete them.

### 5.2 Completed throughput

Responses completed per second, separated by response and domain outcome.

### 5.3 Goodput

Correct, useful domain operations completed per second. Expected sold-out or insufficient-balance responses are reported separately rather than classified as infrastructure failures.

### 5.4 SLO-safe capacity

The highest sustained goodput that satisfies all latency, timeout, correctness, saturation, and headroom gates for the measurement interval.

### 5.5 Recommended operating capacity

A conservative limit below SLO-safe capacity that reserves headroom for variance, rolling deployment, and loss of one capacity unit.

### 5.6 Horizontal scale efficiency

```text
scale efficiency at N units =
    measured goodput at N units
    ----------------------------
    N × single-unit goodput
```

Scale efficiency must be reported separately for each experiment layer:

- stateless API work;
- dispersed independent authorities;
- one indivisible hot authority.

For one hot authority, efficiency approaching `1/N` is the expected serialization ceiling, not evidence that the entire system fails to scale. Dispersed-authority measurements are the primary test of horizontal composition.

### 5.7 Capacity-unit economics

Each tested configuration will report:

- successful operations per second;
- successful operations per second per vCPU;
- cost per hour;
- cost per million successful operations;
- memory headroom;
- database connections required;
- p50, p95, and p99 latency;
- timeout and unknown-outcome rates;
- failure-domain and deployment implications.

Cost figures are reproducible calculations tied to a dated input block, not timeless prices. The input block must state:

- pricing snapshot date and AWS region;
- on-demand, Savings Plan, or Spot assumptions;
- ECS/Fargate task shape, count, and run duration;
- RDS engine, instance class, deployment mode, storage, and I/O assumptions;
- ALB, data-transfer, CloudWatch logs, metrics, and trace costs;
- currency and exchange-rate assumptions where conversion is used.

## 6. SLO lifecycle

SLOs are established progressively rather than invented after benchmarking.

### AG-M0: provisional contract

Define service-level indicators, measurement boundaries, an initial timeout budget, and provisional target values. These are hypotheses used to drive experiments, not production claims.

### AG-M1: outcome and timeout semantics

Make business refusal, conflict, client cancellation, server timeout, database timeout, unknown commit outcome, and internal failure distinguishable. Define safe replay behaviour for every ambiguous outcome.

### AG-M2: local challenge

Measure the first latency-throughput frontier and determine whether the provisional targets are realistic on one node.

### AG-M3: AWS timeout-chain validation

Align client deadlines, load-balancer behaviour, server deadlines, database statement and lock timeouts, health checks, and autoscaling delay.

### AG-M4: operational ratification

Use measured capacity and cost curves to ratify operational SLOs, recommended concurrency and arrival-rate limits, autoscaling thresholds, and error-budget policy.

### AG-M5–AG-M7: scenario gates

Treat ratified SLOs and correctness rules as hard gates for synchronized release, hot-authority contention, and fault-recovery experiments.

### 6.1 Required service-level indicators

- end-to-end p50, p95, and p99 latency;
- successful domain goodput;
- expected refusal or conflict rate;
- client, server, load-balancer, and database timeout rates;
- unknown commit outcomes;
- idempotent replay count and result;
- queue, lock, and connection-pool wait time;
- database transaction time;
- API CPU, memory, goroutine count, garbage-collection pressure, and observed `GOMAXPROCS`;
- database CPU, connections, I/O, lock activity, and transaction saturation;
- admission queue depth and age where applicable.

### 6.2 Overload objective

> Under overload, requests receive explicit bounded outcomes before they accumulate into infrastructure or client timeouts.

## 7. Forty-day milestone plan

| Priority | Milestone | Dates | Primary result |
|---|---|---|---|
| P0 | AG-M0 — Foundation and measurement contract | 20–22 Jul | Architecture, vocabulary, provisional SLOs, repo and CI foundation |
| P0 | AG-M1 — Correct transactional core | 23–28 Jul | Correct booking API, idempotency, timeout semantics, background expiry |
| P0 | AG-M2 — External load system and local frontier | 29 Jul–2 Aug | Trustworthy single-node latency-throughput evidence |
| P0 | AG-M3 — AWS production slice | 3–8 Aug | Reproducible ECS/RDS deployment, telemetry, timeout-chain validation |
| P0 | AG-M4 — Capacity-unit economics and scaling frontier | 9–14 Aug | Machine choice, per-node capacity, scale efficiency, cost frontier, ratified SLOs |
| P1 | AG-M5 — Synchronized booking release | 15–20 Aug | Multi-organisation release wave, admission, fairness, noisy-neighbour evidence |
| P1 | AG-M6 — Shared-resource hot authority | 21–25 Aug | Conserved inventory/balance model and hot-key strategy comparison |
| P1 | AG-M7 — Faults, recovery, and evidence package | 26–28 Aug | Failure experiments, runbooks, final reports, public technical narrative |

### 7.1 P0 scope-protection order

If the P0 schedule slips, reduce breadth in this order while preserving correctness and measurement validity:

1. use single-AZ development RDS rather than adding production-grade database redundancy;
2. deploy and validate one API task shape before broadening the machine sweep;
3. defer autoscaling implementation until after manual one-, two-, and four-task measurements;
4. reduce the number of machine shapes and repeat counts, but retain negative controls and variance reporting;
5. defer AG-M5–AG-M7 rather than weakening AG-M0–AG-M4 conclusions.

Do not cut correctness gates, timeout/outcome semantics, separate-host capacity validation, or result reproducibility.

## 8. Milestone details

## AG-M0 — Foundation and measurement contract

**Dates:** 20–22 July  
**Priority:** P0

### Objective

Create a minimal, reviewable project foundation and define how future claims will be measured.

### Deliverables

- repository README and Go `.gitignore`;
- Go module and minimal service skeleton;
- formatting, lint, unit-test, and race-test CI foundation;
- `docs/design/system-context.md`;
- `docs/design/measurement-contract.md`;
- initial architecture and authority-boundary diagrams;
- provisional SLO table and timeout budget;
- decision record for modular monolith first;
- explicit separation of repository-local measurements, prior evidence, inference, and open hypotheses.

### Exit criteria

- every future experiment has defined inputs, outputs, validation, and negative controls;
- the provisional SLO and timeout chain are explicit;
- measured facts cannot be confused with estimates or architectural hypotheses;
- CI can build and test the minimal service reproducibly.

## AG-M1 — Correct transactional core

**Dates:** 23–28 July  
**Priority:** P0

### Objective

Implement the smallest correct authoritative booking system before distributed deployment concerns.

### Scope

- slot capacity and lifecycle;
- reservation hold, confirmation, cancellation, and expiry;
- booking creation and cancellation;
- idempotency scope and request hashing;
- PostgreSQL schema and migrations;
- transactional repository;
- explicit aggregate lock strategy;
- background expiry and settlement;
- per-request deadline configuration with fail-fast startup validation of the timeout-budget ordering, including the per-phase transport bounds and the write-phase relationship (measurement contract §8.1);
- health and readiness endpoints;
- structured outcome and timing telemetry;
- runtime metadata including Go version and observed `GOMAXPROCS`.

### Required semantics

The API must distinguish successful mutation, expected business refusal, transactional conflict, client cancellation, server deadline, database timeout, lost response after commit, unknown commit outcome, permanent failure, and idempotent replay.

### Correctness gates

- capacity is never exceeded;
- reserved and confirmed counts remain consistent with reservation and booking rows;
- one idempotency key cannot produce two logical mutations;
- replay after a lost response returns the original outcome;
- expiry cannot release confirmed capacity;
- cancellation and confirmation races have one valid winner;
- timed-out and unknown-outcome transactions are accounted for explicitly.

## AG-M2 — External load system and local frontier

**Dates:** 29 July–2 August  
**Priority:** P0

### Objective

Build a trustworthy load system and establish the first single-node latency-throughput frontier.

### Load-generator requirements

- separate Go process;
- closed-loop concurrency mode;
- open-loop arrival-rate mode;
- synchronized release barrier;
- persistent connection reuse;
- full status and domain-outcome validation;
- negative control proving response validation is active;
- configurable deadlines, retries, connection counts, and think time;
- machine-readable raw results and reproducible report generation.

Local correctness and development runs may be co-located. Any published capacity claim must use a load generator on separate compute from the service under test.

The capacity gate requires:

- generator CPU, memory, connection, and network telemetry;
- a generator-capacity sweep;
- a deliberately under-provisioned generator negative control;
- evidence that the selected generator configuration has headroom at the reported server operating point.

### Experiment layers

- **Layer A:** stateless API capacity.
- **Layer B:** dispersed transactional capacity across independent authority keys.
- **Layer C:** initial hot-slot capacity against one authority.

### Exit criteria

- load-generator limits are distinguishable from server limits;
- the first SLO-safe capacity and recommended local operating cap are measured;
- the linear-latency and timeout-degradation hypothesis is reproduced or rejected;
- provisional SLO values are retained, revised, or rejected with evidence.

## AG-M3 — AWS production slice

**Dates:** 3–8 August  
**Priority:** P0

### Objective

Deploy the smallest production-shaped AWS system and prove that it can be reproduced, observed, and operated.

### Deliverables

- Terraform for network, security groups, ECS, load balancer, registry, service, and RDS PostgreSQL;
- container build and deployment through GitHub Actions;
- secrets and configuration handling;
- database migration procedure;
- application metrics, traces, logs, dashboards, and alerts;
- health, readiness, and deployment checks;
- controlled external load execution from separate compute;
- infrastructure and operating-cost inventory.

### Timeout-chain validation

Record and align client deadline, load-balancer behaviour, Go request context deadline, admission timeout, connection acquisition timeout, lock timeout, statement timeout, transaction boundary, commit acknowledgement, retry, and idempotent replay policy.

## AG-M4 — Capacity-unit economics and scaling frontier

**Dates:** 9–14 August  
**Priority:** P0

### Objective

Determine the most cost-efficient production capacity unit, then establish how efficiently those units compose horizontally before a shared dependency becomes the bottleneck.

### Questions

1. How much sustainable work can one API capacity unit perform while meeting correctness and SLO gates?
2. Which machine shape provides the best useful throughput per unit cost?
3. How well does the selected shape scale from one to multiple tasks?
4. What is the minimum sensible fleet after high availability, rolling deployment, and N+1 capacity are considered?
5. At what point should engineering effort move from node optimisation to partitioning or database scaling?

### Representative machine sweep

Subject to current platform support and cost, test shapes such as:

- 0.5 vCPU / 1 GiB;
- 1 vCPU / 2 GiB;
- 2 vCPU / 4 GiB;
- 4 vCPU / 8 GiB.

Record Go version, configured vCPU, observed `GOMAXPROCS`, database pool sizing, request concurrency limits, admission settings, telemetry overhead, connection reuse, CPU throttling, CPU headroom, and memory headroom.

### Experiment layers

- **Layer A:** API capacity without PostgreSQL.
- **Layer B:** dispersed transactional capacity across many authority keys.
- **Layer C:** one hot authority to expose the serialization ceiling.
- **Layer D:** fleet economics including minimum HA fleet, availability-zone placement, task loss, deployment headroom, traffic variance, autoscaling delay, database, load balancer, telemetry, and network cost.

Scale-efficiency curves and conclusions must be reported per layer; the Layer C hot-key curve must never be presented as a system-wide horizontal-scaling result.

### Required recommendation

The AG-M4 report must state:

- platform and task shape;
- Go version and observed `GOMAXPROCS`;
- database pool size per task;
- SLO-safe capacity per task;
- recommended operating cap per task;
- dated cost-input block and cost per million successful operations or flows;
- minimum steady fleet;
- target utilisation range;
- autoscaling signal and threshold;
- horizontal scale efficiency by experiment layer;
- next scaling boundary;
- reasons smaller and larger task shapes were rejected.

## AG-M5 — Synchronized booking release

**Dates:** 15–20 August  
**Priority:** P1

### Objective

Model a large synchronized timetable release and determine how organisation-level partitioning, admission, and fairness affect user-visible outcomes.

### Synthetic scenario

- approximately 200 independent organisations;
- approximately 5,000 potential participants per organisation;
- closely aligned release times;
- configurable session and capacity distribution;
- a mixture of normally contested and unusually hot sessions;
- timetable viewing, selection, reserve, confirm, retry, and fallback behaviour.

### Design direction

- route by `organisation_id` so independent organisations can scale independently;
- preserve slot-level transactional authority within an organisation;
- test bounded release gates or queues;
- protect quieter organisations from noisy-neighbour effects;
- keep business refusal distinct from infrastructure overload;
- measure fairness across participants, organisations, and arrival cohorts.

### Exit criteria

- the target wave completes without correctness loss;
- overload produces bounded explicit outcomes rather than uncontrolled timeout growth;
- one hot organisation cannot silently consume all shared capacity;
- the value and limit of organisation-level partitioning are measured;
- scale claims distinguish potential participants from simultaneous active operations.

## AG-M6 — Shared-resource hot authority

**Dates:** 21–25 August  
**Priority:** P1

### Objective

Model conserved shared inventory and balance under extreme contention, then compare authority granularities and serialization strategies.

### Synthetic scenario

- one group with thousands of members;
- shared inventory items with quantities;
- a group-wide conserved balance;
- cold items, hot items, deposits, withdrawals, transfers, and conflicting operations;
- auditable outcome records.

### Correctness invariants

- item quantity never becomes negative;
- balance creation or loss cannot occur outside an explicit operation;
- duplicate requests cannot duplicate withdrawals or deposits;
- the audit trail reconciles with current state;
- concurrent transfer or withdrawal races have one deterministic valid outcome;
- a timed-out client can replay safely and discover the original result.

### Strategies to compare

1. one aggregate group transaction authority;
2. item-level authority keys with a separate balance authority;
3. serialized command lane or actor-like processing after same-key routing convergence exists;
4. bounded admission in front of the authority;
5. batching only where semantics preserve ordering and conservation.

### Exit criteria

- conservation invariants survive contention tests;
- the single-authority ceiling is measured and explained;
- the selected design balances throughput, correctness, auditability, and operational simplicity;
- the report demonstrates why horizontal API scaling alone cannot solve one indivisible hot key.

## AG-M7 — Faults, recovery, and evidence package

**Dates:** 26–28 August  
**Priority:** P1

### Objective

Demonstrate that the measured architecture remains understandable and correct when components fail, then package the evidence for public technical review and future development.

### Fault experiments

- terminate an API task with requests in flight;
- perform a rolling deployment during load;
- exhaust or constrain the database connection pool;
- inject lock and statement timeouts;
- interrupt a response after commit;
- temporarily make PostgreSQL unavailable;
- delay telemetry export;
- overload one authority partition while others remain healthy;
- retry after client timeout and unknown outcome.

### Deliverables

- recovery and replay report;
- failure-mode table and runbook;
- final architecture diagram;
- capacity-unit recommendation;
- synchronized-release report;
- shared-resource contention report;
- dated cost model and assumptions;
- concise public project narrative covering problem, evidence, decisions, trade-offs, and open work;
- updated README linking the most important demonstrations and reports.

## 9. Priority boundaries

### P0: must complete by 14 August

- AG-M0 through AG-M4;
- correct booking core;
- trustworthy separate-host load generation;
- AWS deployment and telemetry;
- SLO-safe capacity per node;
- horizontal scale and cost frontier;
- defensible machine-choice recommendation.

### P1: complete by 28 August when P0 is healthy

- synchronized-release scenario;
- shared-resource hot-authority scenario;
- fault and recovery package.

### P2: explicitly deferred

- multi-region active-active writes;
- Kubernetes or EKS migration;
- dynamic shard movement and automatic rebalancing;
- Kafka or another durable event backbone;
- Redis as a distributed lock or authoritative store;
- DynamoDB or another NoSQL rewrite without measured justification;
- complete production security and abuse platform;
- complex identity, social, payment, or timetable product features;
- service decomposition performed only to create more deployable units;
- optimisation below the point where expected infrastructure savings justify engineering cost and risk.

## 10. Planned evidence structure

```text
docs/
  public-disclosure-policy.md
  pre-public-checklist.md
  planning/
    alloca-go-roadmap.md
  design/
    high-level-design.md
    system-context.md
    measurement-contract.md
    transaction-semantics.md
    api-surface.md
    observability.md
    authority-and-sharding.md
    aws-production-slice.md
  decisions/
    0001-modular-monolith-first.md
    0002-postgresql-authority.md
    0003-capacity-unit-selection.md
  reports/
    local-capacity-frontier.md
    aws-capacity-economics.md
    synchronized-release.md
    shared-resource-contention.md
    fault-and-recovery.md
```

Every report should separate:

- repository-local measured results;
- prior evidence not reproduced here;
- derived calculations;
- interpretation;
- limitations;
- decisions;
- next experiments.

Repository governance for public release is kept separately from this roadmap in [`docs/public-disclosure-policy.md`](../public-disclosure-policy.md), so it applies to the whole repository rather than to any single milestone.

## 11. Success definition for 28 August 2026

Alloca-Go is successful when it can answer, with reproducible evidence:

1. What is the correct authoritative transaction model for booking and conserved shared state?
2. What causes latency to grow, and when do timeouts become the visible degradation mode?
3. What bounded overload behaviour replaces uncontrolled timeout failure?
4. Which AWS capacity unit should be used, why, and at what recommended operating cap?
5. How close to linear is horizontal scaling for dispersed authorities, and which shared dependency limits it next?
6. How should organisation-level release waves be partitioned and admitted?
7. How should one extremely hot shared resource be serialized, partitioned, or queued?
8. What does the system cost per useful unit of work for a dated pricing snapshot, including resilience headroom?
9. How does the service recover when a response, task, database connection, or authority path fails?

The intended conclusion is not that Go or distribution makes capacity unlimited. It is that a correct authoritative service can use efficient capacity units, scale independent authorities horizontally, expose the remaining serialization boundaries, and make cost, latency, failure, and user-visible outcomes explicit.
