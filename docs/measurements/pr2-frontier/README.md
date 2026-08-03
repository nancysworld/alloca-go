# AG-Sept PR2 — single-instance frontier report

**Status:** the frontier's *mechanism* is identified and its *magnitude* is bounded rather
than resolved. SLO-safe capacity and recommended operating capacity are **deferred with
evidence**, which is what the plan's exit gate allows and what §5 below justifies.

All figures are `[MEASURED]` from the artifacts in this directory unless labelled otherwise.
Every number below is derived from a retained `run.json`, `verdict.json` or panel CSV; none is
quoted from a terminal.

**What this cannot be.** The generator shared a host with the service throughout, so every run
here is `quotability.level: local` by construction. Publishable capacity needs the separate
compute of §10, which arrives with PR4. Nothing in this report is a capacity claim.

---

## 1. The generator is not the bottleneck `[MEASURED]`

Artifacts: [`../pr2-generator-control/`](../pr2-generator-control/). ag-sept-plan §12.2,
mandatory. Dispersed, concurrency 16, 20s windows.

| Generator `GOMAXPROCS` | throughput req/s | generator CPU/core | % of unconstrained |
|---:|---:|---:|---:|
| 1 | 2294.6 | 0.0127 | 106.7% |
| 2 | 2122.1 | 0.0163 | 98.7% |
| 5 | 2053.7 | 0.0177 | 95.5% |
| 10 (all) | 2151.0 | 0.0185 | 100.0% |

Throughput is flat across a **tenfold** change in the generator's compute. The harness
therefore had at least an order of magnitude more capacity than it needed at this operating
point, and the numbers below describe the service rather than the generator.

This control runs first because nothing after it means anything without it: a closed-loop
harness cannot distinguish its own ceiling from the service's, since both appear as throughput
that stops rising with concurrency.

---

## 2. The frontier and its mechanism `[MEASURED]`

Artifacts: [`dispersed/`](dispersed/). 30s windows, 10s warm-up, pool at its default of 10.

| concurrency | throughput req/s | p99 ms | generator CPU/core |
|---:|---:|---:|---:|
| 8 | 1821.6 | 8.2 | 0.017 |
| 16 | 2038.2 | 13.2 | 0.018 |
| 32 | 2053.6 | 24.0 | 0.018 |
| 64 | 2091.6 | 47.0 | 0.018 |
| 128 | 2049.1 | 92.8 | 0.018 |

Throughput plateaus at **~2050 req/s from concurrency 16**, while p99 rises linearly with
concurrency — 13, 24, 47, 93 ms. That combination is the saturation signature: added
concurrency buys queue depth, not work. Latency is proportional to concurrency because the
queue is, which is Little's Law rather than a discovery.

**The mechanism is the database connection pool.** From the c=64 panels:

| signal | value |
|---|---|
| `alloca_db_pool_acquired_connections` | 10 |
| `alloca_db_pool_max_connections` | 10 |
| `rate(alloca_db_pool_acquire_wait_seconds_total)` | **53.6 s/s** |
| `rate(process_cpu_seconds_total)` | **1.1 of 10 cores** |

The pool is pinned at its ceiling, requests accumulate 53.6 seconds of acquire-wait per
wall-clock second, and the service uses 11% of the machine's CPU. It is not compute-bound; it
is waiting for connections.

### 2.1 The pool hypothesis, tested directly `[MEASURED]`

Artifacts: [`pool/`](pool/) and [`pool-repeat/`](pool-repeat/). Concurrency 64, two passes.

| pool size | run 1 | run 2 | spread | req/s per connection (run 2) |
|---:|---:|---:|---:|---:|
| 5 | 1406.0 | 1398.1 | 0.6% | 280 |
| 10 | 2194.9 | 2074.2 | 5.5% | 207 |
| 20 | *1008.9* | 3063.8 | — | 153 |
| 40 | 3768.0 | 3456.9 | 8.4% | 86 |

Throughput scales with pool size, which confirms the pool is the binding constraint. The
`pool=20` first run is an outlier and is retained rather than dropped — §4 explains it.

**Returns diminish sharply.** Per-connection throughput falls from 280 to 86 req/s as the pool
grows from 5 to 40, and doubling from 20 to 40 buys only 13%. So the pool stops being the whole
story somewhere between 20 and 40 connections, and a second constraint — most likely PostgreSQL
itself, which this deployment cannot observe (§6) — takes over.

---

## 3. Contended workloads `[MEASURED]`

Artifacts: [`contended-1/`](contended-1/), [`contended-2/`](contended-2/). Two passes each.

| workload | concurrency | throughput req/s | goodput req/s |
|---|---:|---:|---:|
| `hot_slot` | 16 | 623.4, 633.7 | 1.7 |
| `hot_slot` | 64 | 436.4, 637.7 | 1.7 |
| `hot_identity` | 16 | 525.6, 530.6 | ~0.03 |
| `hot_identity` | 64 | 532.1, 487.0 | ~0.03 |

Both behave as §5.2 and §5.3 predict, and both are **correct results rather than poor ones**:

- `hot_slot` admits its slot's capacity and refuses everything after it, so goodput is
  capacity ÷ window regardless of offered load. Throughput is refusal throughput.
- `hot_identity` admits exactly one claim per identity and refuses every overlapping request
  as `schedule_conflict` — the non-overlap invariant working, not a fault.

Neither is a horizontal-scaling result and neither may be presented as one
(`measurement-contract.md` §3.1, Layer C).

---

## 4. What the environment does to these numbers `[MEASURED]`

This is the finding that decides §5, so it is reported before the conclusions rather than as a
caveat after them.

Three cells showed the same signature: throughput roughly halved, **process CPU roughly
halved with it**, and `pool_acquire_wait` unchanged at ~53.7 s/s.

| cell | throughput req/s | process CPU | acquire wait |
|---|---:|---:|---:|
| `pool=20` run 1 | 1008.9 | 0.40 | ~53.7 |
| telemetry `full` pass 1 | 957.5 | 0.50 | 53.8 |
| telemetry `metrics_only` pass 1 | 918.4 | 0.43 | 53.9 |
| *(typical)* | 2074–3457 | 1.07–1.23 | 53.6–53.7 |

Lower throughput **with** lower CPU is not a service limit — a saturated service works harder,
not less. The pool is fully subscribed in every one of these cells, so each connection is
simply completing fewer operations per second. The service is waiting on the database.

Throughput here is `pool_size ÷ database_service_time`, and the second term moved by ~2×
between runs for reasons outside the harness's control. `pool=20` reproduced at 3063.8 on the
repeat with CPU back to 1.23, confirming the first reading was the environment rather than the
configuration.

**Consequence:** no single-sample figure from this workstation is trustworthy to better than
about 2× without controlling database-side background work. The repeated pairs in §2.1 (0.6%,
5.5%, 8.4% spread) show that *when* the environment is quiet the measurement is tight — the
problem is that quietness is not currently a controlled variable.

---

## 5. Capacity figures: what is reported and what is deferred

**Peak observed throughput `[MEASURED]`: 3768 req/s** (dispersed, concurrency 64, pool 40),
with 3456.9 on repeat. At the default pool of 10 it is 2194.9 / 2074.2.

**SLO-safe capacity: not resolved, and the reason is not latency.** Against
`measurement-contract.md` §7's provisional gates, every cell measured passes comfortably —
p99 reaches 92.8 ms at concurrency 128 against a 500 ms objective, and no timeouts or
unknown outcomes occurred at any point. The latency gates are therefore **not binding** anywhere
in the range explored, which means SLO-safe capacity is not bounded by them and would have to
be found by pushing concurrency far higher than 128. That was not done, and §4 is why it would
not have been trustworthy if it had been.

**Recommended operating capacity: deferred to PR3**, per the scope note §5.6 and the plan's
§14 PR3 line. At one replica only one of the three components
`measurement-contract.md` §3 names — variance — is meaningful; rolling deployment and loss of
one unit cannot be reserved against when there is one unit. The variance PR3 needs is measured
and reported here: **0.6% to 8.4% run-to-run at a fixed configuration in a quiet environment**,
with the ~2× environmental excursions of §4 as the outer bound.

---

## 6. What this deployment cannot see, and what PR3 should fix

`ag-sept-plan.md` §7 requires "application and database utilisation". Application utilisation
is measured (`process_cpu_seconds_total`). **Database utilisation is not measurable here at
all** — the service exposes its own pool state but nothing about the PostgreSQL process, and
§14 PR2's wording says "database-pool pressure", which is a narrower thing.

That gap is exactly what §4 ran into: the limiting term became `database_service_time` and
there is no series for it. PR3, which introduces the container path, is the natural place to
add a PostgreSQL exporter — without it, the second constraint identified in §2.1 cannot be
named, only inferred.

---

## 7. Reproducing this

```sh
make db-up && make obs-up
make dev-measured                         # terminal 1; builds the service so /meta reports a revision
go build -o bin/alloca-load ./cmd/alloca-load

./test/scripts/control-generator.sh       # §12.2, first
WORKLOADS=dispersed CONCURRENCIES="8 16 32 64 128" WINDOW=30s WARMUP=10s \
  OUT=docs/measurements/pr2-frontier/dispersed ./test/scripts/sweep.sh
```

Each cell directory holds its own `run.json`, `verdict.json`, both metrics scrapes, 14 panel
CSVs with an `index.json` recording the resolved queries and window, and a Prometheus TSDB
snapshot. Every figure above can be re-derived from those without re-running anything.
