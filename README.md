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
make db-up     # local PostgreSQL on port 55432
make migrate   # once, before a new version serves traffic
make run       # serves on :8080
make db-down
```

Every target defaults `DATABASE_URL` to the local container and accepts an override, so
the same commands work against another database:

```sh
make run DATABASE_URL='postgres://user:pass@host:5432/alloca?sslmode=require'
```

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
