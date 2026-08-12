# Alloca-Go

Alloca-Go is an experimental Go project for exploring **distributed and data-intensive systems**
through a production-shaped reservation service with explicit correctness invariants, measurement
contracts, and retained evidence.

The booking domain is deliberately stable enough that architecture can change while correctness
and experimental results remain comparable. The project is not intended to converge quickly on one
final architecture; implementation, measurement, failure experiments, and Analyse & Review are used
to decide which problem is worth pursuing next.

## What the project explores

The long-running exploration has five related areas:

- **correctness and consistency** — mutable-state ownership, serialization, transaction boundaries,
  replay/ambiguity, and what must remain atomic as the system becomes distributed;
- **load and performance** — workload shape, contention/skew, throughput, response time, queueing,
  utilisation, saturation, and the resource that actually sets a frontier;
- **reliability and failure** — failure containment, bounded outcomes, recovery, retries,
  backpressure, overload, and ambiguity resolution;
- **scalability** — finding boundaries across which independent work can use additional service or
  writable-state resources without weakening correctness;
- **elasticity** — eventually asking not only whether resources can add useful capacity, but whether
  provisioned compute and state placement can change safely as demand changes.

**Observability and evidence are the experimental foundation across all five.** Quantitative claims
must be reproducible from retained artifacts, and techniques such as sharding, microservices,
caching, queues, Kubernetes, or cloud infrastructure are mechanisms to investigate only when an
evidence-backed problem justifies them.

The exploration roadmap is
[`docs/planning/alloca-go-roadmap.md`](docs/planning/alloca-go-roadmap.md). It is directional rather
than a milestone list: an experiment may expose a problem in any area.

## Current system shape

The service began as a modular monolith with a stateless Go API and PostgreSQL transactional
authority. AG-M1 established the transaction semantics and correctness substrate. AG-Sept then
measured the first mutation frontier, found PostgreSQL limiting before available Go service compute,
and introduced shard-affine independently writable PostgreSQL authorities where organisation work
can proceed independently.

The accepted Phase 1 architecture keeps each supported mutation local to one writable authority.
Cross-authority reserve is explicitly refused rather than disguised as two unrelated local commits.
Service replicas remain stateless for correctness and belong to one shard group/database authority.

The current design entry points are:

- [`docs/design/horizontal-scaling.md`](docs/design/horizontal-scaling.md) — service/database scaling
  axes, shard groups, capacity composition, and their evidence boundaries;
- [`docs/design/horizontal-database-authority.md`](docs/design/horizontal-database-authority.md) —
  placement and writable-authority semantics;
- [`docs/design/transaction-semantics.md`](docs/design/transaction-semantics.md) — transactional
  invariant and outcome model;
- [`docs/design/deployment-architecture.md`](docs/design/deployment-architecture.md) — deployment
  units, provenance, failure boundaries, and the current capacity-environment design;
- [`docs/design/measurement-contract.md`](docs/design/measurement-contract.md) — evidence labels,
  workload/result vocabulary, admissibility, reconciliation, and claim levels.

## Workloads as reusable test cases

Architecture changes should not force the workload to change with it. Stable synthetic workloads
are therefore catalogued in [`docs/test/workload-catalog.md`](docs/test/workload-catalog.md), while
validation plans decide how a selected workload is placed onto a topology.

This lets later experiments apply the same demand to a different database technique, placement
model, service topology, or elasticity mechanism and compare the resulting evidence. New workloads
are added when a genuinely different question needs one rather than expanding one benchmark until
it tests everything at once.

All workloads are synthetic engineering models. They are not descriptions of any organisation's
internal architecture, traffic, or product implementation.

## Predecessor

Alloca-Go succeeds **RuntimeIQ-Alloca**, the booking prototype of the author's earlier RuntimeIQ
project. It is a new implementation rather than a port: domain knowledge, open questions, and prior
evidence carry over; code does not, and no prior result becomes an Alloca-Go result without being
reproduced here. Every prototype figure is labelled `[PRIOR-UNREPRODUCED]` until an experiment in
this repository reproduces it. See
[`docs/design/high-level-design.md`](docs/design/high-level-design.md) §1.1.

## Building and testing

`make ci` is the full local gate and mirrors CI exactly: formatting, `go vet`, `golangci-lint` at a
pinned version, build, tests, and tests under the race detector.

Tests come in two tiers. The default gate is hermetic and needs no services. The **integration**
tier proves properties that only exist against a real PostgreSQL — capacity safety under genuinely
concurrent transactions, the post-lock decision timestamp, the idempotency-key race, and the
assembled HTTP-to-database path — so it is behind the `integration` build tag and requires a
database:

```sh
make db-up             # start a local PostgreSQL in Docker
make test-integration  # run the integration tier under -race
make db-down
```

## Running the service

From cold — starts a local PostgreSQL, migrates it, then serves:

```sh
make dev
```

The steps are also available individually, and `make run` deliberately does **not** migrate.
Migrations are applied by a separate binary, never by a serving replica, so replicas never race the
same DDL on startup
([ADR-0002](docs/decisions/0002-postgresql-transactional-authority.md)):

```sh
make db-up     # local PostgreSQL on port 15432
make migrate   # once, before a new version serves traffic
make run       # serves on :8080
make db-down
```

Every target defaults `DATABASE_URL` to the local container and accepts an override, so the same
commands work against another database:

```sh
make run DATABASE_URL='postgres://user:pass@host:5432/alloca?sslmode=require'
```

### Smoke-testing a running service

```sh
make dev      # in one terminal
make smoke    # in another
```

`make smoke` drives the running binary over a real socket: operational endpoints, the read route,
reserve, an idempotent replay, each refusal shape, then confirm and cancel. It seeds its own slot
with SQL because AG-M1 has no slot-creation endpoint and removes its rows afterwards. `BASE`
overrides the target service; `MAX_TIME` controls curl's own deadline and can be raised when the
service is paused in a debugger.

### Running a measurement run

`make smoke` checks that the service works. The AG-Sept load harness measures it: a seeded fixture,
an external generator holding no database credentials, and reconciliation of client, server, and
persisted-state totals. The procedure is in
[`docs/operations/load-harness.md`](docs/operations/load-harness.md).

Measured conclusions live in [`docs/measurements/`](docs/measurements/), next to the retained
artifacts from which each figure is re-derived. The first load-bearing AG-Sept result is the
[single-instance frontier](docs/measurements/reports/ag-sept-pr2-single-instance-frontier.md): on
the retained developer-workstation experiment the service reached roughly 4,300 booking req/s and
**PostgreSQL, not the Go service, set the frontier**. The report's limitations matter: that is a
workstation result against an untuned container, not a production capacity claim.

Iteration B subsequently established that the shard-group/database-authority boundary composes
correctly and contains authority failure, but deliberately made no throughput multiplier claim from
co-resident authorities.

## Current iteration

AG-Sept Iteration C now asks how aggregate mutation capacity behaves when the correctness-proven
shard-group boundary receives **independently growing resource envelopes**.

The fixed validation uses reusable workload `WL-MUT-DISP-4` — synthetic organisations A/B/C/D — at
1, 2, and 4 shard groups. The capacity baseline and scale-out points use equivalent AWS EC2
capacity-unit hosts plus separate generator compute; AWS is measurement infrastructure for this
question, not a production-architecture commitment. The experiment will obtain numeric `G1`, `G2`,
`G4` and derived scale efficiencies rather than optimise toward a preselected efficiency threshold.

Requirements and current Problem:
[`docs/requirements/ag-sept.md`](docs/requirements/ag-sept.md).  
Validation:
[`docs/test/validation-plan/ag-sept-validation-plan.md`](docs/test/validation-plan/ag-sept-validation-plan.md).  
Schedule:
[`docs/planning/ag-sept-plan.md`](docs/planning/ag-sept-plan.md).

## Engineering process

Work proceeds as a durable Goal above repeated:

```text
Problem -> Requirements -> Design -> Validation -> Schedule
       -> Implement -> Evidence -> Analyse & Review
```

The process is defined in
[`docs/development/engineering-process.md`](docs/development/engineering-process.md). Analyse &
Review is a real engineering stage: it may close an iteration, send it back to discharge a missed
validation gate, or select the next evidence-backed Problem.

## Public-disclosure policy

This repository is intended to be safe for eventual public release. Rules for what must never be
recorded, and the checks required before changing visibility, live in
[`docs/public-disclosure-policy.md`](docs/public-disclosure-policy.md) and
[`docs/pre-public-checklist.md`](docs/pre-public-checklist.md).

## Core principle

> Evidence -> problem -> requirements -> design -> technique.

Scale the boundary that evidence identifies; do not accumulate distributed-systems machinery for
presentation value.

## Status

AG-M0 (foundation and measurement contract) and AG-M1 (correct transactional core) are complete.
AG-Sept is in progress: Iteration A identified the first mutation frontier, Iteration B established
the horizontally composable writable-authority boundary, and Iteration C is preparing the first
independently provisioned shard-group capacity comparison.
