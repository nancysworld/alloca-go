# AG-Sept — Measured scale-out and distributed authority

**Status:** Draft v0.4  
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

| Workstream | PR | Days | Weight |
|---|---|---:|---:|
| Measurement harness and load generator | PR1 | 2.0 | 20% |
| Single-instance frontier, with the diagnostic time-series minimum | PR2 | 2.5 | 25% |
| Containerisation and local multi-instance experiment | PR3 | 2.0 | 20% |
| AWS and Kubernetes deployment path, conditional | PR4 | up to 2.0 | up to 20% |
| Architecture conclusions and one justified boundary | PR5 | 1.5 | 15% |
| **Total allocated budget** | | **10.0** | **100%** |

**AWS scale-out experiments are outside this table.** The §11 matrix needs roughly a further 1.5 days, and there is no room for them in the ten once the local work is funded at what it actually costs. They are reachable from underspend or from the reserve below, and from nowhere else. Planning them inside the base allocation would mean planning to overrun by 15% on day one.

**AWS reallocation rule:** The days assigned to AWS are conditional, not additional contingency. If the AWS path threatens completion of the measurement harness, single-instance baseline, local multi-instance experiment, or architecture report, some or all of that budget is reassigned to those local P0 outcomes. The implementation evidence determines whether returned time is spent on stronger reruns, generator validation, local scale-out, observability, or documentation.

**Why observability moved.** The retention path and diagnostic panels were bundled with the harness workstream, and they are scoped into PR2 because that is the first PR whose conclusions depend on reading a time series rather than a total. The half day follows the work; PR1 keeps 2.0 for the harness alone, which is what it consumed.

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

AG-Sept should extend the observation boundary AG-M1 established (§1) with a minimal aggregated recorder suitable for capacity experiments.

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

This section assigns the requirements above to implementation PRs. The earlier sections remain normative for the technical details; these scopes make ownership, evidence, and exit conditions explicit so required work does not fall between PRs.

### PR1 — Measurement substrate and load harness

**Indicative budget:** 2 days.

**Scope:**

- add the bounded aggregated metrics recorder and separate metrics listener described in §6.1;
- measure telemetry overhead and decide whether an asynchronous sink is justified under §6.2;
- implement deterministic reset and seed tooling, including the hot-identity clean-start assertion;
- implement the external generator for dispersed, hot-slot, and hot-identity workloads, including synchronized starts, response validation, client-side latency and outcome capture, machine-readable summaries, and generator utilisation;
- emit the run manifest structure of §6.4, with every generator- and workload-supplied field populated: commit identity, generator Go version and `GOMAXPROCS`, workload and dataset parameters, concurrency, duration and warm-up, generator location and utilisation, target, and timestamp;
- implement the separate persisted-state verifier and all reconciliation checks in §6.5;
- implement the mandatory response-validation negative control;
- document the local operator path from service start through seed, load, metrics scrape, and verification.

**Evidence:** one controlled local smoke run with its client report, server metrics scrape, reconciliation verdict, telemetry-overhead result, and response-validation control.

**Exit:** one controlled local run produces reconcilable machine-readable client, server, and persisted-state totals; the mandatory response-validation control passes; the manifest carries every field the generator can determine for itself; and generator and telemetry behaviour are observable.

**Not in PR1:** capacity claims, parameter sweeps, retained time-series storage, dashboards, containers, or replica comparisons.

**Manifest completion is staged, and the stages are named.** The generator is an HTTP client and cannot discover the service's shape, so the remaining §6.4 fields are supplied by the operator in the PR that first has something to say: service shape in PR2, topology and image identity in PR3, environment in PR4. The rule that survives the staging is §6.4's own — **no run may be quoted as a capacity claim while a field its topology requires is unpopulated.** PR1 is exempt because it quotes nothing.

### PR2 — Single-instance frontier

**Indicative budget:** 2.5 days — 1.0 for the retention path and diagnostic panels, 1.5 for the sweeps, controls, and report.

**Scope:**

- establish a minimal reproducible Prometheus retention path and compact diagnostic dashboard covering the required outputs of §7 — which include **p50** as well as p95 and p99 latency, since p50 is one of the three latency gates SLO-safe capacity is measured against (`measurement-contract.md` §7) — plus database-pool pressure, process CPU and memory, and relevant Go runtime signals;
- keep the queries and small panel set version-controlled and reproducible without building a general observability platform;
- populate the §6.4 service-shape fields, which are the ones needed to interpret this PR's own numbers: PostgreSQL version, pool size per replica, server `GOMAXPROCS`, timeout budget, and reservation TTL;
- run the bounded one-instance sweeps of §7 for dispersed, hot-slot, and hot-identity workloads, producing every output §7 requires;
- discharge §6.2 end to end, which PR1 deliberately did not: compare throughput and p99 with telemetry enabled against the same workload with it disabled, at the same dataset, concurrency and environment, and report the delta as measured evidence. PR1 established only the per-call cost against a real sink, which bounds this comparison without standing in for it;
- perform the mandatory generator-bottleneck control and demonstrate generator headroom for quotable runs;
- vary concurrency or offered rate, pool size, and application resources only as needed to identify or tightly bound the frontier;
- report peak observed throughput, SLO-safe capacity, and recommended operating capacity according to the measurement contract, or document why a value remains unresolved. **"Unresolved" must not swallow the capacity question itself**: where a contract term is genuinely not computable at one replica, the PR still states the machine's measured ceiling, what mechanism sets it, and the variance around it, so a reader learns the answer rather than the reason there is none;
- retain run reports, reconciliation verdicts, environment details, and the time-series exports or snapshots needed to support the interpretation.

**Evidence:** repeatable reconciled one-instance results for all three controlled workloads, diagnostic time series, the generator control, and a report identifying or bounding each limiting mechanism.

**Exit:** each controlled workload has a repeatable one-instance result; the generator is ruled out; the time series expose or tightly bound the limiting mechanism; and the recommended operating point is reported or explicitly deferred with evidence.

**Not in PR2:** replica scaling, Kubernetes, AWS, rich dashboards, alerting, or the optional synchronized release wave.

**The generator stays on the service host, and that bounds what PR2 may claim.** §6.3 requires the generator to run separately from the service for a publishable claim, and no second machine is assigned before PR4. So PR2's frontier is a *bounded local* result: the §12.2 headroom control is what limits how much the co-resident generator can be distorting it, and every figure carries that limitation. Publishable capacity requires the separate generator compute of §10, which arrives with PR4 and only if its gate passes. If that gate fails, AG-Sept closes with a bounded local frontier and no published capacity number — an outcome the descope order already accepts, and a more honest one than promoting a co-resident measurement.

### PR3 — Container and local scale-out

**Indicative budget:** 2 days.

**Scope:**

- build the reproducible production-shaped image required by §9.1;
- **report the recommended operating capacity PR2 deferred.** §3 of `measurement-contract.md` defines it as reserving headroom for variance, rolling deployment, and loss of one unit; at one replica only the first is meaningful, so PR2 deferred the number with that as its evidence and reported measured variance as a named component. PR3 is the first PR where a second replica makes the other two computable. See [`ag-sept-pr2-scope.md`](ag-sept-pr2-scope.md) §5.6 and §5.6.1 for what PR2 hands over;
- **test PR2's falsifiable scale-out prediction as a first-class result, not as a by-product of the replica matrix.** PR2 measured this machine's ceiling at ~4,300 req/s and identified PostgreSQL's write-ahead log as what sets it, with the service at 12% CPU and the connection pool no longer binding. Its pool ladder is a scale-out experiment in disguise — from the database's side, two replicas at pool 10 resemble one replica at pool 20 — and it therefore predicts **2 replicas × pool 10 ≈ 3,060 req/s, not 2 × 2,074**. Measure it, state whether the prediction held, and if it did not, say what the pool ladder got wrong. See [`../measurements/reports/ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md) §5.4;
- **add a PostgreSQL exporter, and treat it as required rather than desirable.** PR2 could name the bottleneck only from hand-driven `pg_stat_activity` sampling retained as diagnostic evidence, and the scale-efficiency line below cannot be honestly computed while the shared authority is the one component with no instrumentation. A node exporter is worth the same day's work: PR2's unexplained ~2× excursions slow the service, the database *and* the generator together, which no per-process exporter can see;
- populate the §6.4 topology and image-identity fields that first become meaningful here: replica count, deployment topology, image tag, and a build-stamped commit SHA;
- provide the smallest reproducible local load-balancing or orchestration path for one, two, and four replicas against one PostgreSQL authority;
- reuse PR2's Prometheus and dashboard path, adding bounded replica identity and only the per-replica and aggregate views needed for scale-out diagnosis;
- compare dispersed, hot-slot, and local hot-identity workloads across replica counts;
- perform the mandatory connection-budget control in §8.1 and §12.3;
- confirm unrelated authorities continue to progress while one slot or identity serializes;
- calculate like-for-like scale efficiency and distinguish application-compute gains from increased pressure on the shared database admission boundary;
- preserve complete manifests, reconciliation verdicts, topology, and diagnostic time-series evidence for quoted runs;
- run the desirable resource-limit control of §12.4 here if the replica work leaves room, since container CPU and memory limits are the first place it can be applied cheaply. It is the one control that may be dropped without a stop decision.

**Evidence:** a local multi-instance report containing the replica matrix, connection-budget control, per-workload scale efficiency, correctness verdicts, and supporting frontier evidence.

**Exit:** dispersed and hot-authority traffic are compared across replica counts without changing the correctness model; pool multiplication is explained; the hot-authority result is correctly scoped; and all quoted runs remain reproducible and reconciled.

**Not in PR3:** AWS, autoscaling, production Kubernetes platform engineering, a service mesh, or an attempt to eliminate the hot-authority serialization frontier.

### PR4 — AWS deployment, conditional

**Indicative budget:** no more than 2 focused deployment days before the AWS stop gate. The further 1.5 measurement days §4 assigns to AWS experiments are **not** inside the base ten once PR2 and PR3 are funded honestly — they are available only from underspend in PR2, PR3 or PR5, or from the 2–3 day reserve. If neither materialises, the post-gate matrix shrinks to its §11 minimum, or the stop decision is taken. This is the reallocation rule of §4 running in the direction it was always going to run.

**Scope before the gate:**

- publish the immutable service image to ECR;
- populate the remaining §6.4 field, environment, and the aggregate pool capacity the deployed topology implies;
- establish the application runtime, load-balancing path, RDS database, migrations, readiness, graceful shutdown, metrics collection, and separate EC2 generator described in §10;
- prefer EKS, switching to ECS/Fargate or EC2-hosted containers at the first-day gate when necessary;
- pass the AWS smoke gate in §10.3;
- record topology, resources, region, versions, pool budget, and a dated basic cost snapshot without secrets or private endpoints.

**Scope after the gate:**

- run the minimum AWS matrix in §11: dispersed traffic at one, two, and four replicas and hot-slot traffic at one and four;
- run hot identity or mixed identities only where time permits;
- reuse the local measurement, reconciliation, evidence, and visualization contracts;
- compare local conclusions with any network, load-balancer, cloud-compute, or managed-database boundary visible in AWS.

**Evidence:** either a reproducible AWS deployment with reconciled measurements and a basic cost record, or a dated stop-decision report describing the failed gate and how the remaining budget is reassigned.

**Exit:** the service accepts a controlled workload from separate generator compute with visible outcomes and metrics and the minimum useful subset is measured, or the bounded stop decision is documented early enough for PR5 to strengthen local evidence.

**Not in PR4:** multi-region operation, production security completeness, managed-observability expansion, custom autoscaling, or prolonged EKS troubleshooting.

### PR5 — Final experiment and architecture decision

**Indicative budget:** 1.5 focused development days, followed by the reserved 2–3 days for reruns, review, refinement, documentation, and public-release preparation. The split is deliberate: the analysis, the tables, and the boundary decision are development work; the polishing of prose, diagrams, and the repository entry point is what the reserve exists for, and moving it into the base allocation is how the reserve quietly becomes contingency.

**Scope:**

- use AWS results when available; otherwise spend returned AWS budget on decisive local reruns, stronger controls, or narrower uncertainty bounds;
- add the simplified synchronized release wave only when the controlled story is complete and the composite adds explanatory value;
- resolve contradictory or unstable measurements through targeted reruns;
- produce the final one-instance, local scale-out, and optional AWS comparison tables and only the charts needed to explain decisive findings;
- update the architecture diagrams and connect each frontier to the slot-row, identity-row, and claim-relation authority model;
- record the service-boundary decision, including why the reservation transaction remains together and whether an asynchronous event or reporting boundary is justified;
- update the repository entry point, reproduction instructions, limitations, evidence labels, negative controls, cost notes, and public-disclosure checks.

**Evidence:** a public-ready set in which raw artifacts support every reported result and architecture claim, with explicit limitations and no scale claim crossing incomparable workload shapes.

**Exit:** a reviewer can reproduce the topology and decisive runs, distinguish measured facts from calculations and interpretation, understand why replicas help or do not help for each authority distribution, and follow the evidence to the architecture decision.

**Not in PR5:** feature expansion for presentation value, speculative decomposition, rich dashboard work unrelated to a finding, or reopening settled measurement definitions.

## 15. Priority tiers

### P0 — required

- reproducible external load harness;
- aggregated metrics;
- minimal reproducible time-series retention and diagnostic dashboard;
- response-validation negative control;
- correctness reconciliation;
- one-instance baseline;
- dispersed and hot-slot controls;
- local hot-identity control with clean-start assertion;
- multi-instance shared-PostgreSQL comparison;
- connection-budget control;
- the separate-generator rule: a co-resident run is reported as a bounded local frontier and never as a published capacity claim. The *rule* is required; the separate compute that would satisfy it arrives with the conditional PR4, so the rule is honoured by labelling when the machine does not exist;
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
3. dashboard polish beyond the diagnostic minimum;
4. detailed cost comparison;
5. AWS hot-identity rerun;
6. simplified synchronized release wave;
7. local Kubernetes when a working AWS runtime already provides the required orchestration experiment;
8. remaining AWS implementation or reruns after the AWS stop gate.

Do not descope the response-validation control, one-instance control, dispersed-versus-hot-slot comparison, local hot-identity clean-start control, multi-instance shared-authority experiment, connection-budget control, the separate-generator rule for published capacity, minimal diagnostic time-series visibility, measurement validity, correctness reconciliation, or architecture report.

## 17. Final deliverables

The public-ready milestone should leave:

1. a runnable containerized service;
2. a reproducible external load generator;
3. aggregated service and runtime metrics;
4. a minimal reproducible time-series retention path and diagnostic dashboard;
5. a single-instance report;
6. a local multi-instance report;
7. an AWS deployment guide, measured AWS result, or documented stop decision;
8. current and intended architecture diagrams;
9. authority and bottleneck analysis;
10. a service-boundary decision record;
11. a concise public repository summary;
12. explicit limitations, evidence labels, and negative-control results.

Together these should answer what is correct, what is fast under which workload, where scale-out helps, where shared authority remains the frontier, which dependency becomes limiting, and what should be separated next.

## 18. Completion gate

AG-Sept is complete when:

- required workloads run reproducibly from clean fixtures;
- single- and local multi-instance results are available;
- response validation, generator headroom, and connection-budget controls have been demonstrated;
- outcomes and persisted state reconcile;
- database connection and authority boundaries are visible in retained measurements and diagnostic time series;
- measured facts, calculations, and interpretation are separated;
- architecture reflects evidence rather than desired presentation;
- an AWS measurement is available or the bounded AWS stop decision is documented;
- Kubernetes and decomposition remain means rather than goals;
- the repository is suitable for public review under the disclosure policy.
