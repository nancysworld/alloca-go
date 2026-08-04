# Alloca-Go

A production-shaped distributed reservation system in Go, exploring correctness, contention, scalability, overload behaviour, and capacity economics.

## Purpose

Alloca-Go investigates how a stateful backend should:

- preserve scarce-resource and conserved-balance invariants under concurrency;
- expose explicit outcomes for refusal, timeout, retry, and unknown commit state;
- determine sustainable SLO-compliant throughput per production capacity unit;
- scale independent authorities horizontally while making hot-key limits visible;
- degrade through bounded admission or queueing rather than uncontrolled timeout growth;
- measure cost per useful operation, including resilience headroom.

The project starts as a modular monolith with a stateless Go API, PostgreSQL as the transactional authority, an external Go load generator, background settlement workers, and production-oriented telemetry. Service decomposition and additional infrastructure will follow measured evidence rather than presentation value.

## Representative workloads

- **Fitness-club synchronized release:** many independent organisations publish bookable sessions at nearly the same time, producing a large release wave with both normally contested and unusually hot sessions.
- **Shared-resource contention:** thousands of users mutate shared inventory quantities and a conserved balance, including extreme contention on one authority key.

Both workloads are synthetic engineering models. They are not descriptions of any organisation's internal architecture, traffic, or product implementation.

## Predecessor

Alloca-Go succeeds **RuntimeIQ-Alloca**, the booking prototype of the author's earlier
RuntimeIQ project. It is a new implementation rather than a port: domain knowledge,
open questions, and prior evidence carry over; code does not, and no prior result
becomes an Alloca-Go result without being reproduced here. Every prototype figure is
labelled `[PRIOR-UNREPRODUCED]` until an experiment in this repository reproduces it. See
[`docs/design/high-level-design.md`](docs/design/high-level-design.md) §1.1.

## Design

The design record starts at [`docs/design/high-level-design.md`](docs/design/high-level-design.md) —
the entry point that frames the problem, states the design principles, shows the
architecture at a glance, and maps which document owns each detailed decision.

## Building and testing

`make ci` is the full local gate and mirrors CI exactly: formatting, `go vet`,
`golangci-lint` at a pinned version, build, tests, and tests under the race detector.

Tests come in two tiers. The default gate is hermetic and needs no services. The
**integration** tier proves the properties that only exist against a real PostgreSQL —
capacity safety under genuinely concurrent transactions, the post-lock decision
timestamp, the idempotency-key race, and the assembled HTTP-to-database path — so it is
behind the `integration` build tag and requires a database:

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

The steps are also available individually, and `make run` deliberately does **not**
migrate. Migrations are applied by a separate binary, never by a serving replica, so
replicas never race the same DDL on startup ([ADR-0002](docs/decisions/0002-postgresql-transactional-authority.md));
a `run` that quietly migrated would make local development the one place that rule does not
hold:

```sh
make db-up     # local PostgreSQL on port 15432
make migrate   # once, before a new version serves traffic
make run       # serves on :8080
make db-down
```

Every target defaults `DATABASE_URL` to the local container and accepts an override, so
the same commands work against another database:

```sh
make run DATABASE_URL='postgres://user:pass@host:5432/alloca?sslmode=require'
```

### Smoke-testing a running service

```sh
make dev      # in one terminal
make smoke    # in another
```

`make smoke` drives the running binary over a real socket: the operational endpoints, the
read route, reserve, an idempotent replay with the body fields reordered, each refusal
shape, then confirm and cancel. It is the one check that exercises the configured
`http.Server`, its timeouts and the wiring in `cmd/alloca-go` — the integration tests reach
the handler through `httptest` and never bind a socket.

It seeds its own slot with SQL, because AG-M1 has no slot-creation endpoint, and removes
its rows afterwards. `BASE` overrides the target service.

`MAX_TIME` is how long each request waits — curl's own patience, which no server-side
deadline affects. Raise it when the service is paused in a debugger, or every request fails
with an empty body while you are still reading the stack:

```sh
make smoke MAX_TIME=600
```

### Running a measurement run

`make smoke` checks that the service works. The AG-Sept load harness measures it: a seeded
fixture, an external generator holding no database credentials, and a reconciliation of
client, server and persisted-state totals. The procedure is in
[`docs/operations/load-harness.md`](docs/operations/load-harness.md).

**What it has found so far** lives in [`docs/measurements/`](docs/measurements/) — the reports
and the retained artifacts every figure in them is re-derived from. The load-bearing result is
the [single-instance frontier](docs/measurements/reports/ag-sept-pr2-single-instance-frontier.md):
on a developer workstation the service reaches ~4,300 booking req/s, and **the limit is
PostgreSQL rather than the Go service** — `alloca-go` still has substantial compute headroom at
that rate, using about 1.2 CPU cores of a 10-vCPU allocation. So adding service replicas raises
throughput only while aggregate database concurrency is below that limit, and cannot lift the
saturated ceiling beyond it. Read that report's §5.5 before quoting the
number — it is a workstation measurement against an untuned container, not a capacity claim, and
[`docs/measurements/environment.md`](docs/measurements/environment.md) is the machine it was
taken on.

The HTTP contract — routes, request and response shapes, status mapping, and the
`/healthz`, `/readyz`, `/meta` operational endpoints — is documented in
[`docs/design/api-surface.md`](docs/design/api-surface.md). What the service emits about
itself is in [`docs/design/observability.md`](docs/design/observability.md).

## Roadmap

The initial 40-day roadmap is documented in [`docs/planning/alloca-go-roadmap.md`](docs/planning/alloca-go-roadmap.md).

## Public-disclosure policy

This repository is intended to be safe for eventual public release. Rules for
what must never be recorded, and the checks required before changing visibility,
live in [`docs/public-disclosure-policy.md`](docs/public-disclosure-policy.md)
and [`docs/pre-public-checklist.md`](docs/pre-public-checklist.md).

## Core principle

> Optimise for sustainable, SLO-compliant, resilient throughput per unit cost — not peak accepted request rate.

## Status

AG-M0 (foundation and measurement contract) is complete; AG-M1 (the correct
transactional core) is in progress. See the [roadmap](docs/planning/alloca-go-roadmap.md)
for the milestone plan and current status.
