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

## 3. Decisions (settled 2026-07-31, Nancy's call)

Each of these adds a dependency, a binary or an endpoint, so they were decided before
implementation rather than during it.

### 3.1 Metrics backend — `prometheus/client_golang`

The plan calls a Prometheus-compatible recorder "the preferred starting point" (§6.1), and
this is the first observability runtime dependency. Chosen over a hand-rolled recorder
because histogram bucketing and registry machinery are not where PR1's two days should go,
and because it scrapes cleanly under `kind`/EKS later without a second format.

Label sets stay bounded, per §6.1: `operation`, `outcome`, `replay` and similar closed sets
only — never user, slot, reservation, organisation, idempotency key, request identifier or
error text.

### 3.2 Reconciliation reads persisted state from a separate verifier binary

`cmd/alloca-verify` reads the generator's machine-readable client summary, queries the
database directly, and emits the reconciliation report of §6.5. Chosen so the load generator
holds **no database credentials** and stays genuinely external, which §6.3 requires for
publishable runs.

Known wrinkle, carried rather than solved: on AWS the verifier needs network reach to RDS,
so it runs from wherever that is available rather than alongside the generator. Revisit at
PR4 if that proves awkward — a read-only service endpoint remains the fallback.

### 3.3 Telemetry — bounded local sink now, asynchronous sink only if measured to matter

§6.2 requires showing observation is not the request-path bottleneck, not that a particular
mechanism exists. PR1 points structured logs at a bounded local sink and measures request
latency with logging on versus off, reporting the delta as `[MEASURED]`.

If the delta is material, the bounded asynchronous sink (drop policy plus shutdown flush)
follows in PR2. If it is not, `observability.md` §5.1 stays deferred — but deferred on
evidence rather than on assumption, which is the change that matters.

#### Result — the asynchronous sink stays deferred

`[MEASURED]` — raw results:
[`docs/measurements/pr1-telemetry-overhead/raw.txt`](../measurements/pr1-telemetry-overhead/raw.txt),
environment: [`environment.txt`](../measurements/pr1-telemetry-overhead/environment.txt).
Source `internal/telemetry/overhead_test.go`; reproduce with:

```console
$ go test ./internal/telemetry/ -bench Recorder -benchmem -run '^$' -count 5
```

Five runs, so the figures below are the **median with the observed range**, not one sample.
Quoted from the artifact rather than from a terminal, per `measurement-contract` §5.3.

| Recorder | ns/op (median) | range | B/op | allocs/op |
|---|---:|---:|---:|---:|
| `Nop` (call floor) | 0.20 | 0.196–0.213 | 0 | 0 |
| `SlogRecorder` → bounded sink | 792 | 787–803 | 88 | 2 |
| Prometheus recorder | 138 | 135–142 | 0 | 0 |
| **`Tee` (what the service runs)** | **994** | 980–1004 | 88 | 2 |
| `Tee`, parallel | 228 | 221–229 | 88 | 2 |

`[DERIVED]` — median `Tee` cost 994 ns against a request budget in milliseconds is
**≈0.1% of a 1 ms request** (994 ns ÷ 1 ms), and less of a slower one. Observation is not
the request-path bottleneck at any load this milestone will reach, so the bounded
asynchronous sink is **not built in PR1**, and `observability.md` §5.1 stays deferred on
evidence rather than on the assumption it was deferred on originally.

The spread is worth noting for what it says about method rather than about telemetry: the
five `Tee` samples span 980–1004 ns, and an earlier single run of this benchmark produced
1033 ns — outside that range. One sample would have been quoted as fact. It would not have
changed this conclusion, but the habit it represents is the one that eventually does.

**What this does not show, and it is the part that matters.** These numbers measure a sink
that never blocks (`io.Discard`). The risk §5.1 actually names is *backpressure* — a stalled
reader holding the request after its transaction has committed — and no benchmark of a
healthy sink can bound that. What is now established is narrower than "synchronous emission
is safe": it is that emission is not *inherently* expensive, so if a later run shows latency
the observation path explains, the cause is a stalled sink and the fix is the asynchronous
one, not a cheaper encoder. Revisit at the first remote sink, which is when a stall stops
being hypothetical.

## 3.4 Exit gate — discharged

`[MEASURED]` — artifacts in [`docs/measurements/pr1-smoke-run/`](../measurements/pr1-smoke-run/):
`run.json` (generator report), `verdict.json` (reconciliation), `metrics.txt` (server scrape).
Local PostgreSQL 16, one replica, dispersed workload, 20 slots, capacity 5, concurrency 8,
60 iterations.

The gate has three clauses, and each is discharged by a separate artifact rather than by one
run that passed:

**1. Client, server and persisted-state totals reconcile.** All three independently say 60:

| Source | Value |
|---|---|
| Client (`run.json`) | `completed_requests: 60`, `successful_mutation_goodput: 60` |
| Server (`metrics.txt`) | `alloca_requests_total{operation="reserve",outcome="admitted_success",replay="false"} 60` |
| Persisted (`verdict.json`) | 60 live reservations, 60 idempotency records, 60 live claims |

All four §6.5 checks pass, each naming its invariant: INV-1, INV-5, INV-4, INV-7.

**2. The response-validation control passes.** End-to-end, not only in unit tests:
`-validate=false` against the live service produced a run marked not quotable, `alloca-load`
exited 1, and `alloca-verify` also exited 1 — the generator's refusal is carried forward
rather than overridden by a clean reconciliation.

**3. Generator and telemetry behaviour are observable.** The generator reports its own CPU
utilisation (0.02 per core here — nowhere near saturation, so this run is not
client-limited), and the server exposes request, pool and expiry-worker series on a separate
listener.

**What this run is not.** It is a smoke run: 60 requests at concurrency 8 on one host, with
the generator and service sharing a machine. It proves the substrate works end to end; it
establishes no capacity, and no number in it may be quoted as one. That is PR2's work, with
the generator on separate compute.

## 4. Non-goals

Sweeps, capacity claims, replica counts and any published number belong to PR2 and later.
PR1 produces the substrate and one smoke run, not a result.

## 5. Evidence labels

Every quantitative value this PR emits is `[MEASURED]` from a named artifact or
`[HYPOTHESIS]`, per `measurement-contract.md` §2. PR1 quotes no capacity number.
