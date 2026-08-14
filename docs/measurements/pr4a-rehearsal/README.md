# The PR4a local rehearsal

Five `G4` cells driven on 2026-08-13 on the scheduler-partitioned local rehearsal of the
Iteration C 1/2/4 topology, before any metered AWS time was spent.

- [`points/`](points/) — the two operating points, `c=16` and `c=32`.
- [`windows/`](windows/) — three cells differing only in window length, one of which reproduced
  PR2's open throughput anomaly.

The reading is in
[`../../development/implementation/ag-sept-pr4.md`](../../development/implementation/ag-sept-pr4.md)
§3.11, §3.11.1 and §3.12.

## Evidence class: rehearsal/diagnostic only

**No capacity claim rests on any of these cells, and no ladder or frontier is derived from
them.** All four shard groups share one workstation, kernel, storage path and page cache, so the
cpuset partition bounds CPU and nothing else. Nothing here can discharge `VAL-SCALE-5`, become a
Tier-2 result, or be mixed with AWS points to derive `E2` or `E4`.

Every `run.json` reads `quotability.level: capacity`, and that is a statement about *provenance* —
the declaration, the observed deployment and the per-unit `/meta` reconciliation all line up,
which is one of the things PR4a exists to rehearse. It is not a statement that these are capacity
measurements. Each manifest's own `environment` field says so: *"scheduler-partitioned rehearsal;
shared host/kernel/storage, not independently provisioned capacity evidence."*

Every cell is a **single reading**, unreproduced. This workstation's throughput moved by roughly
2× between PR2 runs, and any local figure inherits that until reproduced.

Common to all five: `SLOTS=3200`, `CAPACITY=20`, workload `WL-MUT-DISP-4`, service and generator
both at `7e46f0f` with unmodified trees, image `alloca-go:7e46f0f`. Units on CPUs 0–7 (two per
group), generator on 8–11, verified per run by `itc-cpuset-check.sh` — see each cell's
`cpu-partition.txt` and `observed-cpusets.txt`.

## `points/` — the two operating points

| | [`c16/`](points/c16/) | [`c32/`](points/c32/) |
|---|---|---|
| Window | 60.036 s | 60.038 s |
| Admitted | 110,172 | 129,290 |
| Average rate | 1,835/s | 2,153/s |
| p50 / p95 / p99 | 6.8 / 20.3 / 38.7 ms | 5.8 / 48.1 / 60.3 ms |
| Generator, per core | 0.0769 | 0.0897 |
| Outcome mix | 100% `admitted_success` | 100% `admitted_success` |

**The agreement is the strongest thing in these cells, not the rate.** Each unit's bracketing
scrape pair gives a server-side delta, and the four sum exactly to the client-reported total:

- `c16` — 27,543 × 4 = **110,172**, identical across all four units.
- `c32` — 32,323 / 32,323 / 32,322 / 32,322 = **129,290**.

Two independent accountings agree, and the units are balanced to the request — which is what
`WL-MUT-DISP-4`'s round-robin over four organisations, one homed per authority, must produce if
placement and routing are correct. Both are re-derivable from the `s1`–`s4` `.prom` pairs here.

**Neither is a capacity point and the pair supports no saturation argument.** Both are averages
over non-stationary windows, and a saturation argument selects an operating point precisely by
claiming a higher rung produced no more *sustained* Goodput — the property these windows have not
been shown to have. `c=32` bought 17.4% more Goodput while the tail grew two to three times and
p50 *fell*: the signature of queueing rather than of added capacity. Concurrency 16 also equals
`aggregate_pool_size` 16, so `c=32` is the first rung past it.

> **These cells hold endpoints only — no `panels/`, no TSDB snapshot.** They predate per-cell
> series retention (`a678153`). §3.11's within-window statements — the decay from ~3,200/s to
> ~1,300/s, the latency climb, the memory growth, the pool observation — were read live from
> Grafana, are now past its retention window, and **are not re-derivable from anything retained
> here**. They are recorded as the observations that motivated the window experiment, not as
> evidence. The totals, percentiles and per-unit deltas above are unaffected: those come from
> `run.json` and the scrape pairs.

## `windows/` — the experiment that answered a different question

Three cells identical but for window length, each reseeded from the same state. The intended
question was whether reported Goodput depends on how long a rung runs. **It does not answer
that**, and the reason is what the directory is kept for.

| | [`30s/`](windows/30s/) | [`60s/`](windows/60s/) | [`120s-exhausted/`](windows/120s-exhausted/) |
|---|---|---|---|
| Window | 30.007 s | 60.005 s | 120.003 s |
| Admitted | 92,125 | 83,794 | 256,000 |
| Average rate | 3,070/s | 1,396/s | *not a service rate* |
| p50 / p99 | 4.4 / 16.0 ms | 5.1 / 52.8 ms | 4.3 / 13.2 ms |
| Generator, per core | 0.1146 | 0.0588 | 0.1252 |
| Regime | accelerating | degrading | fixture-bound |

These three **do** carry `panels/` and `tsdb-snapshot/`, and are the first cells in this
directory to retain a cell's shape rather than its endpoints alone.

### The 120 s cell is fixture-bound and its rate must not be quoted

It admitted **exactly 256,000 of a 256,000-mutation supply** and then took a further **128,239
`business_refusal` / `no_capacity`** outcomes — both readable in its `run.json` `totals`. Its
2,133/s is precisely `supply ÷ duration` and describes the fixture, not the service.

Retained rather than deleted because it is the first live catch by the exhaustion reporter, and
because the refusal count bounds how much demand the cell still had left.

**Note `measurement_sound` is `true` for this cell.** Soundness is an accounting property — the
client's totals reconcile against the server's — and is orthogonal to whether the window measured
the service. A run can be perfectly sound and still describe its own fixture.

### The 30 s and 60 s cells were not the same experiment

One accelerated throughout; the other degraded from its first exported sample and admitted 9%
fewer mutations in twice the time. Window length is confounded with which regime a run landed in,
so the two are not comparable and the original question stays open.

The 60 s cell reproduces PR2's open throughput anomaly on two signature elements — throughput
falling to 0.45 of the healthy rate, and service process CPU roughly halving — and contradicts a
third, since acquire-wait nearly doubled where PR2's was unchanged.

The pool series is the sharpest part, and it must be read carefully: **acquired** connections fall
to **7 while acquire-wait rises**, against a configured maximum of 16.

> **`MaxConns=16` is a ceiling, not a population.** An earlier version of this README read
> "7 of 16" as nine connections existing and being withheld. That does not follow: the pool may
> simply hold seven. Distinguishing "connections exist but are unavailable" from "the population
> has fallen, or is constructing/reconnecting" needs the pool's total, idle and constructing
> state, not its maximum.
>
> What the cell does establish is narrower and still sharp: **workers are waiting to acquire while
> fewer connections are acquired**, which makes connection acquisition and availability a concrete
> part of the degraded-regime diagnosis rather than a generic "service saturation" story. It does
> not localise the stall to the pool/database boundary on its own.
>
> `alloca_db_pool_total_connections` and `alloca_db_pool_idle_connections` are scraped by the
> service and **are present in this cell's `tsdb-snapshot/`** — they were simply never exported to
> `panels/`. The distinction above is therefore answerable from this retained cell, without new
> instrumentation and without re-running it. See `ag-sept-pr4.md` §3.13.

> **Do not read the generator's CPU drop as evidence.** The table above shows generator CPU
> falling 0.1146 → 0.0588 per core, and an earlier version of this record counted that as a third
> matching signature element. It is not one. `alloca-load` is closed-loop — fixed workers send,
> block on the response, repeat — so when the service path slows, generator CPU falls with
> throughput *mechanically*, in a completely healthy generator. It carries no information about
> the cause.
>
> The disjoint cpusets (generator on 8–11, units on 0–7, verified per run) establish one thing
> narrowly: the generator and the units were not competing for the same WSL-visible logical CPUs.
> They do **not** exclude a shared kernel, shared storage, shared physical cores or SMT siblings,
> or Windows-side contention — so no host-level cause is either confirmed or ruled out here.

## Every sample here is labelled with the wrong topology

**Read `topology=single-instance-local` in these artifacts as `itc-g4`.** All 1,188 sample rows
across every `panels/*.csv` here carry it, and the TSDB blocks carry it too.

`prometheus.yml` set `topology` as a job-wide relabel fixed at `single-instance-local`, the PR2
value. A static rule cannot know which topology the discovered targets belong to, so every
Iteration C sample inherited PR2's identity. Nothing reported it because the label was *present
and well-formed*: queries succeeded, CSVs carried rows, and the populated-series gate passed. It
was found by reading a retained value, not by any gate.

**The data is unaffected — only this one label is wrong.** Each row still carries its correct
`authority`, `unit` and `instance`, so per-unit work is sound, and `run.json`'s manifest is the
authoritative environment identity in any case. The harm is exactly the one the label existed to
prevent: a snapshot separated from this directory would misdescribe itself.

**It is annotated rather than corrected, deliberately.** The label is baked into the retained
TSDB blocks at ingest, so no re-export can rewrite it — only re-running these cells could, and
these cells cannot be re-run. The 60 s cell is a *degraded-regime* observation that appeared
once; §3.12's whole finding is that the regime is not reproducible on demand. Re-running would
destroy the evidence to fix its metadata.

Fixed for everything future: topology ownership moved to the target documents
(`itc-obs-targets.sh` stamps `itc-g1`/`itc-g2`/`itc-g4`, `obs-target.sh` keeps
`single-instance-local`), and `itc-obs-targets-test.sh` fails if the job-wide relabel returns. Any
cell dated after that change carries its real rung.

## Reading the series

Average rates above are `admitted ÷ window`. The **shape** — the actual finding — is in
`panels/*.csv`, and the two differ by a lot inside one cell: the 60 s cell's instantaneous
Goodput falls 2,194 → 927/s across its own window.

Always bound a `tsdb-snapshot/` query by `panels/index.json`'s `window`. Prometheus blocks are
shared history, so a snapshot carries neighbouring cells' windows too — and the three window
cells ran within four minutes of each other, which makes the hazard unusually live here.

## Two departures from what the runs wrote

Both are naming-only; no content was altered.

- **`seed.log` is retained as `seed.txt`.** `.gitignore` excludes `*.log`, so the original name
  would leave this README citing a file the repository does not carry. `itc-run.sh` now writes
  `seed.txt` itself.
- **`generator-output.txt` is not retained.** It was 0 bytes in every run, and that is correct
  rather than a lost transcript: `alloca-load` writes to `-out` and prints to the terminal only
  when it fails or refuses. Retaining an empty file under that name would suggest a transcript
  went missing. The generator's own reporting is in `run.json`.

Every cell carries the four units' bracketing scrapes (`s1`–`s4`, `-baseline` and `-after`),
which are the server-side accounting the client totals are checked against.
