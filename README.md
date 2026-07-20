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

The project starts as a modular monolith with a stateless Go API, PostgreSQL as the transactional source of truth, an external Go load generator, background settlement workers, and production-oriented telemetry. Service decomposition and additional infrastructure will follow measured evidence rather than presentation value.

## Representative workloads

- **Fitness-club synchronized release:** many independent organisations publish bookable sessions at nearly the same time, producing a large release wave with both normally contested and unusually hot sessions.
- **Shared-resource contention:** thousands of users mutate shared inventory quantities and a conserved balance, including extreme contention on one authority key.

Both workloads are synthetic engineering models. They are not descriptions of any organisation's internal architecture, traffic, or product implementation.

## Roadmap

The initial 40-day roadmap is documented in [`docs/planning/alloca-go-roadmap.md`](docs/planning/alloca-go-roadmap.md).

## Core principle

> Optimise for sustainable, SLO-compliant, resilient throughput per unit cost—not peak accepted request rate.

## Status

Foundation planning is in progress. Implementation begins with AG-M0: project structure, measurement contract, provisional SLOs, and CI.
