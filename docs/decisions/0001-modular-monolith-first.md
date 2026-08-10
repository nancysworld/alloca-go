# 0001 — Modular monolith first

**Status:** Accepted
**Date:** 2026-07-21
**Milestone:** AG-M0

## Context

Alloca-Go must answer questions about correct authority, per-unit capacity,
horizontal scale efficiency, and overload behaviour with reproducible evidence
inside a 40-day window. Two structural options were available at the outset:

1. **Service-decomposed from day one** — separate deployables for admission, domain,
   repository, and workers.
2. **Modular monolith** — one deployable with clear internal module boundaries and
   explicit authority ownership, decomposed later only where evidence demands it.

Two founding principles constrain the choice: *correct authority before distribution*,
and *decomposition follows evidence rather than presentation value*. The scarce
resource in this project is trustworthy measurement, not deployable count. Premature
decomposition would add network hops, partial-failure modes, deployment coordination,
and telemetry surface that confound the very capacity and contention measurements the
project exists to produce — before any evidence shows a module needs its own failure
or scaling domain.

## Decision

Build Alloca-Go as a **modular monolith**: a single stateless Go API deployable, with
internally separated modules (request admission, domain services, idempotency and
outcome classification, transactional repository, background workers) and explicit
write-authority boundaries. PostgreSQL is the cross-node serialization authority.
Horizontal scale is achieved by running multiple identical instances of this one
deployable, not by splitting it into multiple services.

Service decomposition, and any additional infrastructure (Kubernetes, Redis, Kafka,
DynamoDB, multi-region writes), is deferred until repository-local evidence
demonstrates a specific need.

## Consequences

**Enables**
- Capacity and contention experiments measure the domain and database, not an
  inter-service network fabric — cleaner attribution of bottlenecks.
- Independent authorities (e.g. `organisation_id`) scale horizontally by adding
  identical instances; the authority-boundary design (see
  [`../design/system-context.md`](../design/system-context.md)) keeps this safe.
- Faster iteration and a smaller operational surface within the delivery window.

**Costs / accepts**
- Module boundaries are enforced by convention and package structure, not by network
  isolation; discipline is required to keep them clean (the `internal/` layout and
  authority boundaries are the mechanism).
- A single hot authority is not solved by adding instances; that serialization
  ceiling is measured explicitly (Layer C, AG-M4/AG-M6), never hidden.

**Defers**: service decomposition for its own sake, Kubernetes/EKS,
Redis/DynamoDB/Kafka, multi-region active-active writes, and dynamic shard movement.

## Revisit when

Reopen this decision when repository-local evidence shows any of:
- a module needs an independent failure domain or deployment cadence;
- a module's scaling profile diverges sharply from the rest of the API such that
  co-scaling wastes measured capacity;
- the single-deployable model blocks a required AG-M3/AG-M4 measurement.

A future ADR would supersede this one rather than editing it.
