# AG-Sept PR2 — single-instance frontier report

> ## The conclusion
>
> **This machine tops out at ~4,300 booking requests per second, and the limit is
> PostgreSQL — not alloca-go.**
>
> At that rate the service uses **1.2 of 10 cores (12%)**, the connection pool has stopped
> being the constraint (acquire-wait falls from 53.6 s/s to **0.003** at pool 80), and the
> largest single wait is **`LWLock:WALWrite`** — backends serialising on the write-ahead
> log. Throughput is flat from concurrency 64 to 128 while p99 doubles: added load buys
> queue depth, not work.
>
> **The consequence for PR3: running more alloca-go replicas against this one database
> will not raise throughput.** The pool ladder already tested that in disguise, and §5.4
> turns it into a prediction PR3 can falsify.
>
> Evidence: [`plateau/`](../pr2-frontier/plateau/) · [`plateau-repeat/`](../pr2-frontier/plateau-repeat/) ·
> [`postgres-waits/`](../pr2-frontier/postgres-waits/). Full reasoning in [§5](#5-capacity-on-this-machine-the-conclusion).
> Scope limit in [§5.5](#55-what-this-number-is-not).

**Status:** the frontier's mechanism is identified, the ceiling is measured, and the
bottleneck is named. What remains deferred is narrower than it was: **SLO-safe capacity** and
the contract's **recommended operating capacity** term, for the reasons in §5.5 and §5.6.

All figures are `[MEASURED]` from the artifacts in this directory unless labelled otherwise.
Every number below is derived from a retained `run.json`, `verdict.json` or panel CSV; none is
quoted from a terminal. The one exception is labelled wherever it appears: the PostgreSQL wait
percentages in §4.1 and §5.2 come from [`postgres-waits/`](../pr2-frontier/postgres-waits/), which is
hand-driven sampling retained as **diagnostic evidence, not a measured deliverable** — read
that directory's caveats before quoting them.

**What this cannot be.** The generator shared a host with the service throughout, so every run
here is `quotability.level: local` by construction, and every `run.json` refuses the `capacity`
level by name. Publishable capacity needs the separate compute of §10, which arrives with PR4.

So §5's ceiling is a **measured property of this workstation**, and it is stated plainly
because that is the question PR2 exists to answer. It is *not* a capacity claim in the
contract's sense, and §5.5 is the section that keeps those two apart. Do not quote the number
without it.

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

### 1.1 The same control, re-run at the plateau `[MEASURED]`

Artifacts: [`../pr2-generator-control-plateau/`](../pr2-generator-control-plateau/), 2026-08-04.
Dispersed, **concurrency 128, pool 80**, 20s windows.

The control above ran at ~2,150 req/s. §5's conclusion is drawn at ~4,300 — twice the rate — so
the control was re-run at the operating point the conclusion actually rests on rather than
being argued across from a different one.

| Generator `GOMAXPROCS` | throughput req/s | generator CPU/core | % of unconstrained |
|---:|---:|---:|---:|
| 1 | 4682.7 | 0.0174 | 113.4% |
| 2 | 4236.2 | 0.0209 | 102.6% |
| 4 | 4135.4 | 0.0227 | 100.1% |
| 10 (all) | 4130.1 | 0.0245 | 100.0% |

**Headroom holds at the plateau**: a generator confined to a *single* core delivers 4682.7
req/s, matching and slightly exceeding the unconstrained 4130.1. Constraining the harness
tenfold does not reduce the answer, so the harness is not what produces it.

This also **corroborates the plateau from a third independent path**: the control shares no
code with the sweep, runs no warm-up phase, and seeds a fixture seven times smaller (8,000
slots against 61,540) — and still lands at 4130.1–4682.7 req/s, against the sweep's
3883.7–4345.0. Fixture and index size do not set the ceiling; if anything the smaller table is
marginally faster, which is the direction cache locality predicts and the opposite of a
fixture-bound result.

> **The first attempt at this control did not conclude, and that is why the script changed.**
> The 2026-08-04 first pass read 101.5%, 100.6%, **32.8%**, 100.0% — one rung had met the §4
> excursion (its generator CPU *fell* to 0.0105, so the generator was starved alongside
> everything else rather than saturated). The script printed a **pass** anyway: its verdict
> took the lowest `GOMAXPROCS` within 5% of unconstrained and reported headroom "down to" it,
> so it could not distinguish "flat across the ladder" from "flat with a hole in it" — and
> would have passed identically had the hole been a genuine generator limit.
>
> `control-generator.sh` now requires **every** rung within the band and reports `INCONCLUSIVE`
> otherwise, alongside the admissibility gates of ChatGPT's P1 (every requested rung present,
> sound, response-validated, free of timeouts and unknown outcomes, at or above `-require`, and
> sharing one service identity). The table above is the re-run under the hardened script, which
> exits zero. The refused first pass is not retained: it was superseded rather than explanatory,
> and §4 already documents the excursion with better evidence.

---

## 2. The frontier and its mechanism `[MEASURED]`

Artifacts: [`dispersed/`](../pr2-frontier/dispersed/). 30s windows, 10s warm-up, pool at its default of 10.

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

Artifacts: [`pool/`](../pr2-frontier/pool/) and [`pool-repeat/`](../pr2-frontier/pool-repeat/). Concurrency 64, two passes.

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
story somewhere between 20 and 40 connections, and a second constraint takes over. §2.2 finds
it and names it.

### 2.2 Past the pool: the plateau and what actually binds `[MEASURED]`

Artifacts: [`plateau/`](../pr2-frontier/plateau/), [`plateau-repeat/`](../pr2-frontier/plateau-repeat/), 2026-08-04. Two
passes, 30s windows, 10s warm-up.

**The §2 and §2.1 grid was a cross, not a rectangle.** The concurrency arm ran at pool 10 and
the pool arm at c=64, so the highest number either produced — 3768 req/s — sat at the corner
where they met, with no cell combining high concurrency and a large pool. A corner is not a
plateau, so it could not be called a ceiling.

| concurrency | pool | pass 1 | pass 2 | p99 ms |
|---:|---:|---:|---:|---:|
| 64 | 80 | 4326.8 | 4113.3 | 34.0 / 37.5 |
| 128 | 80 | 4345.0 | *1165.0* | 61.6 |
| 128 | 40 | *1953.4* | 3883.7 | 51.0 |
| 64 | 40 | *1008.9*† | *1233.9*, *1099.9* | 112.7 / 128.7 |

*Italics are cells carrying the §4 signature; they are retained, not dropped. †from `pool/`.*

Two things follow, and the first is the ceiling:

**Throughput plateaus at ~4,300 req/s.** Between c=64 and c=128 at pool 80 it moves 4326.8 →
4345.0 (pass 1) — **0.4%** — while p99 rises 34.0 → 61.6 ms. That is the saturation signature
again, one level up from §2: doubling the offered concurrency changes only the queue.

**At pool 80 the connection pool is no longer the constraint at all.** `pool_acquire_wait`
collapses to **0.003 s/s** through the c=64/pool=80 window — against 22.8 s/s at pool 40 and
53.6 at pool 10, so four orders of magnitude down and effectively nil — and only 58–64 of the
80 connections are ever in use, because the generator's 64 workers cannot ask for more. Whatever limits throughput at
4,300 req/s, it is not the pool — which is what makes §5's conclusion a statement about the
database rather than about a tunable.

---

## 3. Contended workloads `[MEASURED]`

Artifacts: [`contended-1/`](../pr2-frontier/contended-1/), [`contended-2/`](../pr2-frontier/contended-2/). Two passes each.

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

### 4.1 What the 2026-08-04 passes added, including two suspects now ruled out

Ten more cells reproduced the excursion four times and narrowed it considerably. Artifacts:
[`plateau/`](../pr2-frontier/plateau/), [`plateau-repeat/`](../pr2-frontier/plateau-repeat/),
[`postgres-waits/`](../pr2-frontier/postgres-waits/).

**It is not the configuration.** The affected cell moves between passes: `c=128/pool=40` read
1953.4 then 3883.7, while `c=128/pool=80` read 4345.0 then 1165.0. Nothing about a pool size or
a concurrency predicts it.

**The warm-up is never the slow phase.** Across all eight plateau cells the 10-second warm-up
ran at 3629.5–4832.5 req/s — every time, including in every cell whose measured window then
collapsed:

| cell | warm-up | measured window | ratio |
|---|---:|---:|---:|
| c=64 pool=80 (both passes) | 4648.9 / 4470.4 | 4326.8 / 4113.3 | 0.93 / 0.92 |
| c=128 pool=40 pass 2 | 3716.0 | 3883.7 | 1.05 |
| c=64 pool=40 (both passes) | 4252.7 / 4309.8 | **1233.9 / 1099.9** | **0.29 / 0.26** |
| c=128 pool=80 pass 2 | 4743.6 | **1165.0** | **0.25** |

That asymmetry is the strongest clue in the report: the machine demonstrably reaches ~4.3k
req/s at the start of *every* cell, and then sometimes cannot hold it 20 seconds later.

**Autovacuum and checkpoints were the two leading suspects. Both are now ruled out**
(`postgres-waits/`, and see that directory's caveats):

- autovacuum ran **inside** a clean 3883.2 req/s window, while the collapsed window that
  followed had its autovacuum complete *before* the window opened;
- the one checkpoint captured mid-sweep was a *requested* checkpoint that wrote 154 buffers
  and coincided with the **fastest** cell of its pass.

**What the collapse looks like from inside PostgreSQL:** backends dominated by `RUNNING`
(77.1% and 59.9% of active samples, against 18.0% and 12.9% in clean cells) — executing rather
than blocked on any lock, while delivering a third of the throughput. The service's CPU halves,
and the *generator's* CPU halves with it (0.010 against 0.024). Everything on the host slows at
once.

**So §4's original wording was one step ahead of its evidence.** It said "for reasons outside
the harness's control", which asserts an external cause; what is actually established is that
the cause is not the configuration, not autovacuum, not a checkpoint, and not confined to any
one process. Naming it needs the host-level instrumentation neither PR2 nor PR3 currently
plans — see §6.

---

## 5. Capacity on this machine: the conclusion

### 5.1 The number `[MEASURED]`

**Peak throughput: 4345.0 req/s** — dispersed, concurrency 128, pool 80
([`plateau/dispersed-c128-pool80/`](../pr2-frontier/plateau/dispersed-c128-pool80/)).

**Sustained plateau: ~4,300 req/s**, from the four clean cells across two passes:

| cell | throughput | p99 | artifact |
|---|---:|---:|---|
| c=128 pool=80 | 4345.0 | 61.6 ms | [`plateau/dispersed-c128-pool80/`](../pr2-frontier/plateau/dispersed-c128-pool80/) |
| c=64 pool=80 | 4326.8 | 34.0 ms | [`plateau/dispersed-c64-pool80/`](../pr2-frontier/plateau/dispersed-c64-pool80/) |
| c=64 pool=80 | 4113.3 | 37.5 ms | [`plateau-repeat/dispersed-c64-pool80/`](../pr2-frontier/plateau-repeat/dispersed-c64-pool80/) |
| c=128 pool=40 | 3883.7 | 51.0 ms | [`plateau-repeat/dispersed-c128-pool40/`](../pr2-frontier/plateau-repeat/dispersed-c128-pool40/) |

**Variance at the ceiling: 4.9%**, from c=64/pool=80 (4326.8 and 4113.3). That is the §5.6
variance component measured at the ceiling rather than at the default pool, and it is a
**single pair** — the honest sample size, because the other three configurations each lost one
of their two cells to a §4 excursion, so no second clean pair exists. Repeating the plateau
until every configuration has one is the cheapest way to strengthen this report.

Two independent corroborations sit in the artifacts, and together they are worth more than any
single cell:

- **All eight warm-up phases**, at four configurations across two passes, ran at
  **3629.5–4832.5 req/s, mean 4325.4** — ten seconds each, before any §4 excursion could
  develop. The plateau does not depend on which cells are treated as clean.
- **The §1.1 generator control**, which shares no code with the sweep and uses a fixture seven
  times smaller, independently lands at **4130.1–4682.7 req/s**.

Three measurement paths, one answer.

At the default pool of 10 the same machine does 2194.9 / 2074.2 (§2.1), so **pool sizing alone
is worth roughly 2× on this hardware** — the single most valuable configuration finding in PR2.

### 5.2 What decides it: PostgreSQL's write-ahead log `[MEASURED]`

Three candidates are eliminated at the plateau cell itself:

| candidate | reading at the plateau | verdict |
|---|---|---|
| alloca-go compute | 1.2 of 10 cores (**12%**) | not the constraint |
| the load generator | §1.1's control **re-run at this operating point**: 4682.7 req/s on one core, against 4130.1 unconstrained | not the constraint |
| the connection pool | acquire-wait **0.003 s/s** (53.6 at pool 10), 58–64 of 80 connections in use | **not the constraint at pool 80** |
| fixture / index size | §1.1 reaches the same rate on 8,000 slots as the sweep does on 61,540 | not the constraint |

The pool ceasing to bind is what makes this a database result rather than a tuning result. And
inside PostgreSQL the largest single wait, in both clean cells sampled, is
**`LWLock:WALWrite`** — 41.0% and 36.8% of active-backend samples, with `LWLock:BufferContent`
second at 22.5% and 32.7% ([`postgres-waits/`](../pr2-frontier/postgres-waits/)).

That is a database serialising on its write-ahead log. Every `reserve` is a durable
transaction, WAL writes are serialised, and one PostgreSQL instance has one WAL.

**Read `postgres-waits/README.md` before quoting those percentages** — they are 2-second
samples of `pg_stat_activity`, so they are shares of *observed active backends*, not shares of
time.

### 5.3 Where the 4,300 goes

At the plateau, per completed request: **1.2 of 10 host cores** across the whole service, and
p99 of 34–62 ms against a 500 ms objective. The machine is ~88% idle at its own ceiling. This
is not a system that runs out of CPU; it is one that runs out of *serialised durable writes*.

### 5.4 The prediction PR3 must test

**Adding alloca-go replicas against this database is predicted not to raise throughput.**

The pool ladder already ran the experiment in disguise: from PostgreSQL's side, two replicas
holding pool 10 each look much like one replica holding pool 20. So §2.1's ladder gives a
falsifiable number rather than an opinion —

> **2 replicas × pool 10 should land near 3,060 req/s (the measured pool-20 point), not near
> 4,148 (2 × the measured pool-10 point).**

If PR3 measures ~3,060, the constraint is confirmed shared and downstream, and horizontal
scale-out of the service is the wrong lever on this hardware. If it measures ~4,148, the
pool-ladder reading is wrong and this section is what caught it.

Two caveats to keep attached. Replicas are not purely equivalent to connections — each carries
its own Go runtime and GC — but at 12% CPU that is not what binds here. And this does **not**
make PR3 redundant: it still has to prove the non-overlap invariant holds across replicas under
distributed authority, which is a correctness result independent of throughput, and the two
deferred headroom components only become computable once there is more than one unit.

### 5.5 What this number is not

**It is a workstation figure, not a capacity claim.** Every cell is `quotability.level: local`
by construction — the generator shared a host with the service throughout — and each
`run.json` refuses the `capacity` level by naming the manifest fields PR3 and PR4 still owe.
The separate compute of §10 is what makes a publishable number possible.

It is also **not** a claim about PostgreSQL in general. It describes a stock
`postgres:16-alpine` container with default `shared_buffers` (128 MB), `checkpoint_timeout`
(5 min) and `max_wal_size` (1 GB), on a WSL2 workstation whose storage path is Docker Desktop's.
A tuned database on real storage is a different measurement, and §6 is why this deployment
cannot say by how much.

### 5.6 SLO-safe and recommended operating capacity: still deferred, for narrower reasons

**SLO-safe capacity: not resolved, and the reason is not latency.** Against
`measurement-contract.md` §7's provisional gates every cell passes comfortably — the worst p99
anywhere in the plateau set is 61.6 ms against a 500 ms objective, and no timeouts or unknown
outcomes occurred at any point. The latency gates are **not binding**, so SLO-safe capacity is
not bounded by them. What now bounds the exploration is throughput saturation rather than
missing cells: past c=128 at pool 80 the queue grows and the rate does not.

**Recommended operating capacity: deferred to PR3**, per the scope note §5.6 and the plan's §14
PR3 line — but only the *term* is deferred, not the work. `measurement-contract.md` §3 defines
it as a cap reserving headroom for **variance, rolling deployment, and loss of one unit**. At
one replica the last two cannot be reserved against at all, so the defined quantity is not
computable here. The one component that *is* meaningful is measured and reported above:
**4.9% run-to-run at the ceiling from a single clean pair** (0.6–8.4% across three pairs at the
default pool, §2.1), with §4's excursions as the outer bound. PR3 should not treat one pair as
a variance estimate without repeating it.

---

## 6. What this deployment cannot see, and what PR3 should fix

`ag-sept-plan.md` §7 requires "application and database utilisation". Application utilisation
is measured (`process_cpu_seconds_total`). **Database utilisation is still not part of the
measured deliverable** — the service exposes its own pool state but nothing about the
PostgreSQL process, and §14 PR2's wording says "database-pool pressure", which is a narrower
thing.

[`postgres-waits/`](../pr2-frontier/postgres-waits/) closed enough of that gap to name the bottleneck (§5.2)
and to eliminate two suspects in §4.1, but it is hand-driven sampling retained as diagnostic
evidence, not instrumentation: 2-second polls, not time-weighted, and it perturbs the window it
observes. **PR3 should replace it with a PostgreSQL exporter** on the container path, at which
point §5.2's WAL finding becomes a series rather than a sample, and §5.4's prediction can be
tested with both sides instrumented.

**One gap this report could not close, and PR3 inherits it too.** §4.1's excursions slow the
service, the database and the load generator *simultaneously* — so identifying them needs
host-level metrics (CPU steal, page cache, disk latency on the Docker Desktop storage path),
which neither a service exporter nor a PostgreSQL exporter provides. A node exporter is the
cheapest instrument that would see it. Until something does, single readings from this
workstation carry §4's ~2× caveat no matter how good the database instrumentation gets.

### 6.1 The dashboard's latency trace is bucket-limited; `run.json` is not `[MEASURED]`

Raised in review: the Grafana p95/p99 traces appear to stop at ~0.25 s. They are not clipped,
and the distribution is not really clustered there — it is **histogram resolution**, and the
report's own latency figures are unaffected because none of them come from the histogram.

`requestBuckets` (`internal/metrics/metrics.go:123`) steps `… 0.05, 0.1, 0.25, 0.5 …`. Once p99
falls inside the 2.5×-wide `(0.1, 0.25]` bucket, `histogram_quantile` interpolates within it and
can return anything up to the upper bound — typically landing high, hence the pin at ~0.2485.
Comparing the panel against the client's exact percentile, computed from raw samples, shows the
panel never exceeding the bound of the bucket the true value sits in:

| cell | panel p99 | client p99 | bucket |
|---|---:|---:|---|
| `pr2-telemetry/full-1` | 0.2460 | **0.1225** | (0.1, 0.25] |
| `pr2-telemetry/metrics_only-1` | 0.2462 | **0.1245** | (0.1, 0.25] |
| `contended-2/hot-slot-c64` | 0.2471 | 0.1589 | (0.1, 0.25] |
| `plateau-repeat/dispersed-c128-pool80` | 0.4393 | 0.2667 | (0.25, 0.5] |

The last row is the proof that it is not a ceiling: once the true value crosses into the next
bucket, the panel follows. Within the wide bucket the over-estimate reaches **2×**.

**Every latency figure in this report comes from `run.json`**, whose percentiles are computed
from raw per-request samples rather than from buckets — so §2, §2.2 and §5 are unaffected. It
is the *dashboard* that misleads, and only between 0.1 s and 0.25 s.

Buckets are deliberately **not** changed here. Adding boundaries costs cardinality on a
labelled histogram, and PR2's latency conclusions do not depend on the panel. If PR3 needs the
dashboard to be quantitatively trustworthy in that band, insert `0.15` and `0.2` and re-measure
the telemetry overhead, since bucket count is part of what §6.2 was trying to price.

### 6.2 TSDB snapshots are cumulative, and the redesign is deferred to PR3

Raised in review, confirmed, and **deliberately not fixed in PR2** (Nancy's call, 2026-08-04).

`export-panels.sh` copies a full Prometheus snapshot per cell, and Prometheus blocks are
shared history rather than cell-local: across the 33 cells here there are **140 block copies of
only 39 distinct blocks — 72% redundant**, and 560 of the 1,288 tracked files under
`docs/measurements/` are TSDB.

**What a reader needs to know to use them:** a cell's snapshot is *not* scoped to that cell. It
carries whatever history the Prometheus instance held at export time, including other cells'
windows. **`panels/index.json`'s `window` is what isolates a cell's evidence** — a query against
a snapshot without those bounds will silently span neighbouring cells.

Deferred rather than fixed because the redesign changes how every cell is produced, and PR3
re-runs everything on the container path anyway; doing it here means re-exporting 33 cells for
no new evidence. The shape PR3 should take: one snapshot per *sweep* with cell-specific CSVs and
windows, or a fresh Prometheus data directory per sweep. It matters more there than here,
because PR3 multiplies cells by replica count.

---

## 7. Reproducing this

```sh
make db-up && make obs-up
make dev-measured                         # terminal 1; builds the service so /meta reports a revision
go build -o bin/alloca-load ./cmd/alloca-load

./test/scripts/control-generator.sh       # §12.2, first (§1)

# §1.1 — the same control at the plateau operating point
WORKLOAD=dispersed CONCURRENCY=128 WINDOW=20s PROCS="1 2 4 0" SLOTS=8000 \
  DATABASE_URL="$DATABASE_URL&pool_max_conns=80" \
  OUT=docs/measurements/pr2-generator-control-plateau ./test/scripts/control-generator.sh

# §2 — the concurrency arm, at the default pool
WORKLOADS=dispersed CONCURRENCIES="8 16 32 64 128" WINDOW=30s WARMUP=10s \
  OUT=docs/measurements/pr2-frontier/dispersed ./test/scripts/sweep.sh

# §2.1 — the pool arm, at c=64
WORKLOADS=dispersed CONCURRENCIES=64 POOLS="5 10 20 40" WINDOW=30s WARMUP=10s \
  OUT=docs/measurements/pr2-frontier/pool ./test/scripts/sweep.sh

# §2.2 and §5 — the plateau, where the two arms are combined
WORKLOADS=dispersed CONCURRENCIES="64 128" POOLS="40 80" WINDOW=30s WARMUP=10s \
  OUT=docs/measurements/pr2-frontier/plateau ./test/scripts/sweep.sh
```

`sweep.sh` requires a clean working tree, so commit one pass before running the next — Go
stamps an untracked file as a modified tree and every cell would be refused at level `none`.

Each cell directory holds its own `run.json`, `verdict.json`, both metrics scrapes, 14 panel
CSVs with an `index.json` recording the resolved queries and window, and a Prometheus TSDB
snapshot. Every figure above can be re-derived from those without re-running anything.

**A harness limit found while producing §2.2, and not yet fixed.** `sweep.sh`'s `slots_for()`
sizes the fixture from an assumption of "up to ~400 admitted units per concurrent worker per
second". Measured admission is 17–68, so it over-provisions by roughly 48×; at c=256 it asks
`alloca-seed` for a 122,980-slot fixture and the seed times out
(`plateau/dispersed-c256-pool40/seed.log`, retained). **Concurrency above 128 is therefore
currently unreachable**, which is why §2.2's plateau is demonstrated between c=64 and c=128
rather than across a wider span. The over-provisioning is deliberate — §5.5 of the scope note
explains why a too-small fixture fails silently — but the constant needs to come down or the
sizing needs to use a measured rate.
