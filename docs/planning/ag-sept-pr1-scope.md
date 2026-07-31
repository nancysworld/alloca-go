# AG-Sept PR1 — Measurement substrate and load harness (scope)

**Status:** Draft — open decisions listed in §3, nothing implemented yet
**Budget:** 2 development days ([AG-Sept plan](ag-sept-plan.md) §14)
**Owner doc:** [ag-sept-plan.md](ag-sept-plan.md) §6 is normative for what this PR builds; this
note records only how PR1 discharges it and which choices are still open.

## 1. Exit gate

From the plan, unchanged:

> One controlled local run produces reconcilable machine-readable client, server, and
> persisted-state totals; the mandatory response-validation control passes; and generator
> and telemetry behaviour are observable.

Three things have to be true together, and the third is the one that constrains the design:
the run must reconcile **client totals, server totals, and persisted state**, so something
in the harness needs to read the database after a run.

## 2. What PR1 delivers

| # | Deliverable | Plan reference |
|---|---|---|
| 1 | Aggregated metrics recorder on the existing observation boundary, bounded label sets only | §6.1 |
| 2 | Evidence that synchronous telemetry is not the request-path bottleneck | §6.2 |
| 3 | External load generator: workload shapes, closed-loop concurrency, synchronized start, valid idempotent requests, client-side outcome capture, machine-readable summary, own utilisation | §6.3 |
| 4 | Run manifest emitted with every run | §6.4 |
| 5 | Correctness reconciliation used by every later run | §6.5 |
| 6 | Response-validation-active negative control | §12.1, `measurement-contract.md` §5.5 |
| 7 | One controlled local smoke run exercising all of the above | §14 PR1 |

Deliverable 6 is mandatory and not descopable: `measurement-contract.md` §5.5 requires a
control that **fails when response validation is silently disabled**, so a reported success
cannot be an unchecked `200`.

## 3. Open decisions

Recorded here rather than settled unilaterally, because each one adds a dependency, a
binary, or an endpoint.

### 3.1 Metrics backend

`prometheus/client_golang` versus a small in-repo recorder exposing the Prometheus text
format. The plan calls a Prometheus-compatible recorder "the preferred starting point" while
leaving the backend an implementation choice (§6.1). The trade is a well-understood
dependency and free histogram/registry machinery against no new runtime dependency and a
recorder we own.

### 3.2 Where reconciliation reads persisted state

The generator must run on separate compute (§6.3), so giving it database credentials
conflicts with that. Options: a separate verifier binary with database access; a read-only
verification endpoint on the service; or the generator writing only client totals and a
local step joining them to a direct database query.

### 3.3 Telemetry treatment

§6.2 accepts a bounded local sink, a bounded asynchronous sink with explicit drop and
shutdown behaviour, or another measured approach. The asynchronous sink is real design work
(drop policy, shutdown ordering) that `observability.md` §5.1 deliberately deferred; the
bounded local sink is nearly free and may be sufficient to show the request path is not
observation-bound.

## 4. Non-goals

Sweeps, capacity claims, replica counts and any published number belong to PR2 and later.
PR1 produces the substrate and one smoke run, not a result.

## 5. Evidence labels

Every quantitative value this PR emits is `[MEASURED]` from a named artifact or
`[HYPOTHESIS]`, per `measurement-contract.md` §2. PR1 quotes no capacity number.
