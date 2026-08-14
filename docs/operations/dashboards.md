# Reading the diagnostic dashboard

**Status:** Living — current through AG-Sept PR4a
**Scope:** how to read the Grafana dashboard: what question each panel answers, what it cannot
answer, and the readings that look sound and are not.

**This document owns interpretation, not content.**
[`deploy/observability/panels.json`](../../deploy/observability/panels.json) is the canonical
source for every query, unit and note; `test/scripts/gen-dashboard.py` generates the dashboard
from it, and `internal/observability/panels_test.go` enforces the rules below. Where this page
and `panels.json` disagree, `panels.json` wins — change the expression there, never in the
dashboard JSON.

What the service *emits* is owned by [`../design/observability.md`](../design/observability.md);
what may be *claimed* from a number is owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md).

## Raising it

```sh
make itc-rehearse ITC_GROUPS=4                 # the topology first — the obs stack attaches to its network
make obs-rehearse ITC_CPUS_GENERATOR=8-11      # Prometheus, Grafana, node_exporter, all pinned
```

Grafana is at `http://localhost:3000/d/alloca-frontier`, Prometheus at `http://localhost:9091`.
Panels are provisioned from the repository, so a change needs `docker restart alloca-grafana`
rather than an edit in the browser — an edit made in Grafana is lost and, worse, is not the query
the exported CSVs used.

**Grafana is for diagnosis between rungs, never during one.** A query engine under variable load
on the generator host is the one part of the arrangement that can move while a rung is being
measured (`ag-sept-pr4.md` §2.2).

## Two jobs, two cadences

| Job | Scraped | What it is |
|---|---|---|
| `alloca-go` | 1 s | the service units — one target per shard-group authority |
| `node` | 5 s | `node_exporter`, the host sensor (`VAL-NEG-7`) |

The cadences differ deliberately: 1 s would alias the pool-pressure signal a frontier is read
from, and it would multiply the retained snapshot for `node_exporter`'s far larger series count.

Every service panel carries **`{{authority}}` first** in its legend, so one unit reads identically
across every graph and can be followed without relying on colour.

## The panels

The dashboard is grouped into four sections, and this page walks them in the same order.

### Demand and outcome

| Panel | Answers | Do not read it as |
|---|---|---|
| **Throughput and goodput** | how much useful work completed. Goodput excludes replays and non-mutations | throughput ≠ goodput; a run can complete requests while committing nothing |
| **Latency** | the p50/p95/p99 SLO gates | aggregate across units, deliberately — the gate is on the deployment, not on one authority |
| **Outcomes** | the mix by outcome | a business refusal is a valid domain answer, never a failure |

### Database pool

The pool is the object of study in Iteration C, not a background indicator.

| Panel | Answers |
|---|---|
| **Pool occupancy (conns)** — acquired against total | is the pool populated, and is it saturated? |
| **Pool lifecycle (conns/s)** — new connections, destroyed | is it churning underneath a steady population? |
| **Pool acquire concurrency (s/s)** — acquire, empty-acquire | how many acquires are in flight at once, and why? |
| **Pool mean acquire duration (s)** | per-acquire cost, which the concurrency figure confounds with volume |

A **saturated** pool reads: acquired meeting total, acquire cost paid waiting for releases. A pool
that is *not* saturated while acquire cost climbs is the open anomaly (`ag-sept-pr4.md` §3.13.1).

**`s/s` is a concurrency, not a duration.** A counter of accumulated seconds, differentiated by
wall-clock seconds, is the average number of operations in flight — the standard reading of a
duration-counter rate. So **1 s/s means one acquire was in progress at all times, on average**, for
that authority; 0.4 means one in progress 40% of the time; 2.0 means two overlapping. It exceeds 1
whenever concurrent workers wait at once, which is why the axis is not a percentage.

**Idle and constructing are exported but not plotted.** `idle` is exactly `total - acquired`, so on
a four-unit rung it added four more oscillating lines and no information; `constructing` asks the
same question as `new connections` and was flat. Both remain in every cell's CSVs — `idle` is the
series §3.13.1 actually reads — under `display: false`.

### Service process (alloca-go units)

**Service CPU (cores)**, **Service goroutines**, **Service GC pause p75** and **Service resident
memory** — four graphs rather than one because they are four units. Cores, a count, a duration and
bytes on one axis meant the count set the scale and the rest lay flat and unreadable.

Every title says *service* because the section below measures the machine those services run on,
and CPU carries its unit because "cores" is the reading most often assumed wrongly: **2.0 means two
cores' worth of work, not 2%**.

### Host (the machine every unit shares)

`node_exporter` measures the **machine**, which on the local rehearsal is one WSL VM shared by all
four shard groups and the generator. That sharing is why local runs are rehearsal evidence and not
capacity evidence.

| Panel | Answers | Reading |
|---|---|---|
| **Host CPU busy (cores)** | how much of the machine is busy | non-idle CPU-seconds per second, so it reads in *cores*. 7.6 of 16 logical CPUs is ~48% |
| **Host CPU stolen and blocked (cores)** | was the host prevented from running? | `steal` is the hypervisor taking the CPU — the WSL2 candidate; `iowait` is blocking on storage — the Docker Desktop storage-path candidate. Both are small next to total busy, which is why they have their own graph |
| **Host run queue (load average)** | is work waiting rather than being done? | `node_load1` against the CPU count. **A one-minute average cannot resolve a stall inside a 60 s cell** — it is still filling for most of one |
| **Host memory available** | did the host have room? | *available*, not free: free excludes reclaimable page cache and reads as exhaustion on a healthy machine |

**PSI is missing, and it is the instrument this view actually wants.** `/proc/pressure` does not
exist on the WSL2 kernel, so `node_scrape_collector_success{collector="pressure"}` reads 0 and the
run queue is its coarser stand-in. Pressure reports *contention* rather than consumption — a host
can be far from saturated and still be stalling. The collector stays enabled for hosts whose
kernels serve it (`ag-sept-pr4.md` §3.14).

## Readings that look sound and are not

Each of these shipped, survived review, and was found by someone looking at a screen.

**A ceiling is not a population.** `alloca_db_pool_max_connections` is a configured constant. In-use
falling *against the maximum* does not mean connections exist and are being withheld — the pool may
simply hold fewer. Only `total` says what exists, which is why max is no longer plotted beside
occupancy. Worked example: §3.13.1.

**`acquire_wait` does not measure waiting.** pgxpool's `AcquireDuration` times the whole `Acquire()`
call, including constructing a connection. An acquire that never waited still contributes. Its name
says otherwise and cannot be fixed without breaking queries against retained cells.

**`empty_acquire_wait` does not measure waiting either.** It covers acquires that found no idle
connection — *both* waiting for a release *and* constructing a new one, construction time included
in full. Only `new connections` separates them:

| Observation | Reading |
|---|---|
| empty-wait ↑ **and** new-connections ↑ | construction or churn is implicated |
| empty-wait ↑ **and** new-connections flat | acquires waited for an existing connection |
| acquire-duration ↑ **and** empty-wait flat | the delay is elsewhere in the acquire path |

**The generator's CPU falling is not evidence of anything.** `alloca-load` is closed-loop: workers
send, block on the response, repeat. When the service path slows, generator CPU falls with
throughput mechanically, in a completely healthy generator.

**A panel with no data may have healthy data.** `rate()` needs two samples in its window, and
Grafana's `$__rate_interval` is computed from one datasource-wide setting. A panel on the 5 s host
job rendered empty for a window sized to the 1 s service job — while the CSV export, which
substitutes a literal window, had the data all along.

**An axis carrying two units is labelled for neither**, and the larger series sets the scale.

**A graph with sixteen lines answers nothing.** Four metrics across four authorities is sixteen
series, and occupancy plotted all of them — including one series that was arithmetically implied by
two others. Per-authority detail is what `{{authority}}` legends exist for; it is not a reason to
plot every metric per authority on one graph.

## Adding a panel

Edit [`panels.json`](../../deploy/observability/panels.json), then:

```sh
python3 test/scripts/gen-dashboard.py
make ci
```

The gates will refuse, in `gen-dashboard.py` or in `panels_test.go`:

- a panel defined but placed on no graph, and a graph naming a panel that does not exist — a new
  panel goes into one of the four `SECTIONS` in `gen-dashboard.py`, which is what gives it a
  heading on screen. Set `"display": false` to export a series without plotting it; the exporter
  ignores the flag on purpose, and a test keeps it ignoring it;
- **two units on one axis**, and a unit with no Grafana mapping. The graph title states the unit
  and the axis stays plain numbers — except bytes and seconds, where Grafana's scaling earns its
  place (`22.9 MiB`, `100 µs`);
- a query that preserves per-authority cardinality without `{{authority}}` **first** in its legend,
  and the `per_authority` flag contradicting a query that aggregates authorities away;
- a `rate()` panel on a job scraped more slowly than the datasource's interval without a declared
  `range` of at least two scrapes;
- a dashboard expression that is not canonical, or a canonical one that reaches no panel;
- `process_*` / `go_*` without a `job=` constraint — those families are exported by every
  instrumented Go process, `node_exporter` and Prometheus included;
- more than 15 graphs. That bound is a scope decision, not a style rule: it exists so adding one
  is deliberate and recorded, and each increase carries its reason at the assertion.

**Collect broadly, panel narrowly.** `diskstats`, `netdev` and `filesystem` are scraped and not
plotted. The TSDB snapshot retains everything scraped, so a series nobody thought to plot is still
recoverable — which is how the pool population question was answered from an already-retained cell
without re-running it.
