# The window experiment, and the cell that reproduced PR2's open anomaly

Three `G4` rehearsal cells driven on 2026-08-13, identical but for window length, each reseeded
from the same state. The intended question was whether reported Goodput depends on how long a
rung runs. **It does not answer that question**, and the reason it cannot is what the directory
is kept for.

The reading is in
[`../../development/implementation/ag-sept-pr4.md`](../../development/implementation/ag-sept-pr4.md)
§3.12.

## Evidence class: rehearsal/diagnostic only

**These are not capacity results, and no ladder or frontier rests on them.** All four shard
groups share one workstation, kernel, storage path and page cache, so the cpuset partition
bounds CPU and nothing else. Nothing here can discharge `VAL-SCALE-5`, become a Tier-2 result,
or be mixed with AWS points to derive `E2` or `E4`.

`run.json`'s `quotability.level` reads **`capacity`** for all three, and that is a statement
about *provenance* — the declaration, the observed deployment and the per-unit `/meta`
reconciliation all line up, which is one of the things PR4a exists to rehearse. It is not a
statement that these are capacity measurements. Each manifest's own `environment` field says so:
*"scheduler-partitioned rehearsal; shared host/kernel/storage, not independently provisioned
capacity evidence."*

Every cell is a **single reading**, unreproduced. This workstation's throughput moved by roughly
2× between PR2 runs, and any local figure inherits that until reproduced.

## The three cells

| | [`window-30s/`](window-30s/) | [`window-60s/`](window-60s/) | [`window-120s-exhausted/`](window-120s-exhausted/) |
|---|---|---|---|
| Window | 30.007 s | 60.005 s | 120.003 s |
| Admitted | 92,125 | 83,794 | 256,000 |
| Average rate | 3,070/s | 1,396/s | *not a service rate — see below* |
| p50 / p99 | 4.4 / 16.0 ms | 5.1 / 52.8 ms | 4.3 / 13.2 ms |
| Generator, per core | 0.1146 | 0.0588 | 0.1252 |
| Regime | accelerating | degrading | fixture-bound |

All three: `c=16`, `SLOTS=3200`, `CAPACITY=20`, workload `WL-MUT-DISP-4`, service and generator
both at `7e46f0f` with unmodified trees, image `alloca-go:7e46f0f`. Units on CPUs 0–7 (two per
group), generator on 8–11, verified for each run by `itc-cpuset-check.sh` — see
`cpu-partition.txt` and `observed-cpusets.txt` in each cell.

### The 120 s cell is fixture-bound and its rate must not be quoted

It admitted **exactly 256,000 of a 256,000-mutation supply** and then took a further **128,239
`business_refusal` / `no_capacity`** outcomes — both readable in its `run.json` `totals`. Its
2,133/s is precisely `supply ÷ duration` and describes the fixture, not the service.

It is retained rather than deleted because it is the first live catch by the exhaustion reporter
added in §3.10, and because the refusal count bounds how much demand the cell still had left.

**Note that `measurement_sound` is `true` for this cell.** Soundness is an accounting property —
the client's totals reconcile against the server's — and it is orthogonal to whether the window
measured the service. A run can be perfectly sound and still describe its own fixture.

### The 30 s and 60 s cells were not the same experiment

One accelerated throughout; the other degraded from its first exported sample and admitted 9%
fewer mutations in twice the time. Window length is confounded with which regime a run landed
in, so the two are not comparable and the original question stays open.

The 60 s cell reproduces PR2's open throughput anomaly on three of its four signature elements
and contradicts the fourth (the pool divergence). Its significance is that the generator halved
in sympathy — 0.1146 → 0.0588 per core — while confined to CPUs **disjoint** from every unit,
which excludes shared-CPU contention by construction in a way PR2's unpartitioned host could
not. The per-cell detail is in §3.12; the series it is read from are in each cell's `panels/`.

## Reading the series

The averages above are `admitted ÷ window`. The **shape** — which is the actual finding — is in
`panels/*.csv`, and the two differ by a lot inside a single cell: the 60 s cell's instantaneous
Goodput falls 2,194 → 927/s across its own window.

Always bound a `tsdb-snapshot/` query by `panels/index.json`'s `window`. Prometheus blocks are
shared history, so a snapshot carries neighbouring cells' windows too — and these three cells
ran within four minutes of each other, which makes the hazard unusually live here.

## Two departures from what the run wrote

Both are naming-only; no content was altered.

- **`seed.log` is retained as `seed.txt`.** `.gitignore` excludes `*.log`, so the original name
  would leave this README citing a file the repository does not carry. `itc-run.sh` now writes
  `seed.txt` itself.
- **`generator-output.txt` is not retained.** It was 0 bytes in all three runs, and that is
  correct rather than a lost transcript: `alloca-load` writes to `-out` and prints to the
  terminal only when it fails or refuses. Retaining an empty file under that name would suggest
  a transcript went missing. The generator's own reporting is in `run.json`.

Each cell also carries the four units' bracketing scrapes (`s1`–`s4`, `-baseline` and `-after`),
which are the server-side accounting the client totals are checked against.
