# Alloca-Go — exploration roadmap

**Status:** Living — directional, non-normative, unscheduled.

## Purpose

**Alloca-Go is an experimental project for exploring distributed and data-intensive systems.**

It uses a booking service with explicit correctness invariants, measurement contracts, and
observability as a stable experimental system. Those foundations allow the architecture to change
while correctness and evidence remain comparable throughout the exploration.

The project is not intended to converge quickly on one final architecture. It uses implementation,
experiments, measurement, and analysis to investigate how a stateful system behaves and evolves
under concurrency, load, failure, and growth.

This roadmap answers one question:

> **Where might this project explore, and what is worth learning or proving?**

Its role is deliberately broad. It describes areas of distributed-systems exploration rather than
a sequence of features or technologies.

## What this document is not

It does **not** own current implementation state, established facts or evidence, normative
requirements or invariants, accepted architecture, validation obligations, milestone or PR scope,
dates, budget, or implementation status. Every one of those has a better owner, listed in
[`../README.md`](../README.md).

Inclusion here means **“potentially valuable direction”**, never “committed scope”. An item may sit
here for a long time, be reframed by evidence, or be dropped without ceremony.

The project is application-led rather than technology-led. Microservices, sharding, caching, read
replicas, queues, stream processing, Kubernetes, autoscaling, managed databases, multi-region
deployment, and similar mechanisms are **possible techniques, not destinations**. They become
worth building only when an observed problem and accepted requirements justify them.

That matters because different applications expose different limiting boundaries. A technique that
is central to one distributed system may add cost and failure modes without solving the problem in
another.

## Where it sits in the engineering process

The roadmap is **outside the engineering iteration loop and upstream of Goal selection**:

```text
             EXPLORATION ROADMAP
       possible areas / questions / directions
                         |
              select a worthwhile outcome
                         v
                        GOAL
                         |
                         v
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ next Problem <- Analyse & Review <- Evidence -------------+
```

It is **not another mandatory process stage**. A roadmap item becomes committed work only when the
maintainer deliberately selects it into a Goal or Problem, and it then proceeds through
Requirements, Design, Validation plan, and Schedule like any other work
([`../development/engineering-process.md`](../development/engineering-process.md) §1.4).

The arrow runs the other way too. Analyse & Review may surface an interesting question that is not
worth pursuing now; adding or reframing it here is the correct home for it, rather than
prematurely turning it into a requirement or a scheduled PR.

## The exploration areas

The areas below are related rather than independent checkboxes. An experiment may advance several
at once, and the next worthwhile problem may come from any of them. They do not prescribe an order
and none is ever permanently “finished”.

### 1. Correctness and consistency

**Core question:** how can scarce, shared, or conserved state be owned and mutated correctly under
concurrency, distribution, and partial failure?

Questions worth exploring include:

- Which state genuinely needs one write authority, and which only appears to?
- What must be serialized because of a domain invariant, regardless of available compute?
- What can safely proceed independently?
- Which guarantees can be delegated to the database, and which require an application protocol?
- How should idempotency and replay preserve one logical mutation when outcomes are ambiguous?
- What consistency can safely be weakened, and what cannot?
- What changes when one operation spans more than one writable transaction domain?

Alloca-Go begins here deliberately: a scalability experiment that admits double-booking or loses a
mutation under ambiguity is not useful evidence about a scalable system.

**Related durable owners:**
[`../design/transaction-semantics.md`](../design/transaction-semantics.md) (`INV-*`),
[`../decisions/0002-postgresql-transactional-authority.md`](../decisions/0002-postgresql-transactional-authority.md),
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md).

### 2. Load and performance

**Core question:** how does the system behave as a defined workload approaches and exceeds the
capacity of its limiting resources?

A useful performance discussion starts by defining **load**, not merely quoting organisation,
member, or data counts. Depending on the workload, load may include:

- request or mutation arrival rate;
- concurrency;
- workload mix;
- contention and skew;
- synchronized release waves, bursts, or other peaks;
- active data set and working-set size.

Performance under that load can then be described through:

- throughput and useful goodput;
- response-time distributions rather than only averages;
- queueing and admission delay;
- connection and lock wait;
- service and database execution time;
- resource utilisation and saturation;
- SLO-safe and recommended operating capacity.

Throughput and response time are coupled around saturation: as a limiting resource approaches its
capacity, additional load can accumulate as queueing and cause response time to grow much faster
than useful work. An experiment should therefore ask both **how much work completed** and **where
the time went**.

For Alloca-Go, response time may contain network, service, connection-pool acquisition, database
execution, lock/transaction wait, queueing, and response-path components. Which one matters is an
experimental result, not something to assume from the aggregate latency number.

**Related durable owners:**
[`../design/measurement-contract.md`](../design/measurement-contract.md),
[`../design/latency-timeouts-and-retries.md`](../design/latency-timeouts-and-retries.md),
[`ag-sept/milestone-validation.md`](ag-sept/milestone-validation.md).

### 3. Reliability and failure behaviour

**Core question:** what does the system do when dependencies fail, outcomes become uncertain,
resources are overloaded, or recovery is incomplete?

Questions worth exploring include:

- How far does one dependency failure propagate?
- Can unaffected authority domains continue independently?
- What happens to in-flight work when a database, network path, or service process disappears?
- Which faults create genuinely ambiguous mutations?
- How is an ambiguous mutation resolved without duplicating logical work?
- What state survives restart, failover, or recovery?
- Which recovery needs compensating work, and which can remain local?
- How should overload degrade into explicit bounded outcomes rather than uncontrolled latency,
  timeout cascades, or generic failures?
- What fairness and backpressure properties matter under contention?

Availability is therefore only one part of reliability. Correct failure classification,
containment, replay safety, bounded degradation, and recoverability are equally important.

**Related durable owners:**
[`../design/transaction-semantics.md`](../design/transaction-semantics.md),
[`../design/latency-timeouts-and-retries.md`](../design/latency-timeouts-and-retries.md),
REQ-COR-2, REQ-FAIL-1.

### 4. Scalability

**Core question:** which parts of the system can operate largely independently, and how effectively
can additional resources increase useful capacity without weakening correctness?

The general principle is to find a boundary across which work can proceed independently. The
interesting engineering problem is **where that boundary belongs for this application**.

Alloca-Go already exposes several distinct scaling questions:

- service compute within one database-authority shard group;
- independently writable database authorities;
- read serving and read-heavy workloads;
- one hot slot, identity, organisation, or authority that cannot be averaged away by aggregate
  throughput;
- placement and workload skew across otherwise independent authorities;
- **capacity/resource economics** — how much useful capacity an added resource or cost unit buys,
  and how that relationship changes as an architecture scales;
- eventually, geographical or other deployment boundaries if a problem justifies them.

These axes should not be conflated. Adding stateless replicas to one saturated database writer is a
different experiment from partitioning writable state onto independent writers. Likewise, a
read-heavy workload can expose a service or query frontier that a mutation-heavy benchmark never
sees.

A useful scaling claim therefore names its **capacity unit**, workload, resource envelope, and
scale efficiency rather than saying only that “more instances were faster”. Capacity economics is
a related but distinct question: cloud pricing can make it concrete, but provider-specific prices
are evidence inputs rather than an architectural objective.

**Related durable owners:**
[`../design/horizontal-scaling.md`](../design/horizontal-scaling.md),
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md),
REQ-SCALE-1..3.

### 5. Elasticity and resource adaptation

**Core question:** once a system can use additional resources effectively, how safely and cheaply
can its provisioned capacity and placement change as demand changes?

Elasticity is broader than autoscaling. Autoscaling is one possible mechanism; the distributed
systems question is which resources can be added or removed without violating ownership,
correctness, or availability.

For a shard-affine topology with `M` writable authorities and replica allocation
`[x1, ..., xM]`, at least two different forms of elasticity exist:

- **service-compute elasticity** — change an `x_i` by adding or removing stateless replicas within
  an existing shard group. This can leave state placement unchanged and is comparatively cheap;
- **state-placement elasticity** — change `M` or move an organisation between writable
  authorities. This changes authoritative ownership and therefore requires safe state migration,
  routing transition, draining, rollback/failure handling, and proof that writes are neither lost
  nor duplicated.

A third form may be **vertical elasticity**: resize the compute, memory, I/O, or connection
resources of one logical database authority while leaving its ownership boundary unchanged.

Questions worth exploring include:

- What is the minimum service footprint imposed by shard affinity?
- When does vertical resizing beat repartitioning?
- How can a new authority gain useful work rather than merely exist empty?
- How can an authority be drained and removed when demand falls?
- Can placement change while writes continue?
- How should rebalancing react to skew and hot organisations?
- What signal should trigger resource change, and how much headroom should be retained during the
  transition?

Stateful elasticity is intentionally a later problem than basic scalability: first establish that
adding a resource unit helps, then ask how safely to add and remove those units as load changes.

**Related durable owners:**
[`../design/horizontal-scaling.md`](../design/horizontal-scaling.md),
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md).

## Evidence and observability are the experimental foundation

Correctness, performance, reliability, scalability, and elasticity are only useful exploration
areas if the project can distinguish observation from explanation.

Observability and evidence therefore sit **under all five areas** rather than forming one more
feature track. Useful mechanisms include:

- explicit measurement contracts and evidence labels;
- deterministic correctness checks and reconciliation;
- structured logs and bounded metrics;
- response-time and resource attribution;
- negative controls that prove a gate can fail;
- deployment and binary provenance;
- reproducible workload and environment descriptions;
- retained artifacts from decisive experiments;
- Analyse & Review that turns evidence into a durable problem/goal decision without rewriting the
  evidence itself.

The aim is not maximum telemetry. It is enough discriminating evidence to answer **why** the
system behaved as it did and to know what remains uncertain.

**Related durable owners:**
[`../design/measurement-contract.md`](../design/measurement-contract.md),
[`../design/observability.md`](../design/observability.md),
[`../design/deployment-architecture.md`](../design/deployment-architecture.md),
[`../development/engineering-process.md`](../development/engineering-process.md).

## Candidate directions inside those areas

The broad areas above can generate many concrete questions. Current examples include:

- capacity efficiency and safe operating envelope of independently provisioned writable shard
  groups;
- read-heavy and mixed-workload scaling once the mutation path is understood;
- overload, admission control, fairness, backpressure, and open-loop arrival behaviour;
- deliberate acknowledgement-loss fault injection and other targeted ambiguity/recovery faults;
- cross-authority booking and the cost of durable distributed coordination;
- [dynamic placement and reclamation of booking state](../ideas/dynamic-placement-and-reclamation.md):
  placing new slots and user homes below organisation granularity, reusing capacity as work
  retires, and separating active schedule claims from retained history;
- online placement change, state migration, rebalancing, and scale-in/scale-out of writable
  authorities;
- managed/cloud infrastructure when network, failure, or independent-resource boundaries require
  an environment a workstation cannot provide;
- additional conserved-resource scenarios such as divisible inventory or balances when they expose
  a meaningfully different serialization problem;
- asynchronous event/reporting boundaries or service decomposition **only** when workload,
  ownership, deployment, or failure evidence shows a real independent boundary.

None is scheduled by being listed here. Some may never be worth doing in Alloca-Go.

## Synthetic workload note

Workload scenarios used throughout the project — including synchronized booking releases across
many independent organisations and contention for a shared inventory quantity or conserved balance
— are **synthetic engineering models**. They are not descriptions of any organisation's
architecture, scale, traffic, or implementation.

## Selecting from here

Nothing above is scheduled. When one of these directions becomes worth doing, it is framed as a
Goal or Problem under [`../requirements/`](../requirements/), and only then proceeds through
Requirements, Design, Validation plan, and Schedule.

The current milestone's goal and engineering iterations are in
[`../requirements/ag-sept.md`](../requirements/ag-sept.md); what is actually scheduled is in
[`ag-sept/milestone-plan.md`](ag-sept/milestone-plan.md).
