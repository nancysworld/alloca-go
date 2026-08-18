# Ten identical cells — the base rate for the degraded regime

Ten `G4` cells driven back to back on 2026-08-16 by
[`test/scripts/itc-repeat.sh`](../../../../test/scripts/itc-repeat.sh), identical in every
respect — same fixture, same concurrency, same window, same service binary, one reseed each —
and differing only in when they ran. `series.txt` records the configuration once.

Until this series, every statement this repository made about the degraded regime rested on one
or two runs. A single reading cannot separate a regime from the tail of a distribution, and
PR4b's ladder selects an operating point by claiming a higher rung produced no more *sustained*
Goodput, which is unanswerable while nominally identical rungs disagree by half. This is the
population that question needs.

## Evidence class: rehearsal/diagnostic only

**No capacity claim rests on these cells.** All four shard groups share one workstation, kernel,
storage path and page cache, so the cpuset partition bounds CPU and nothing else. Nothing here
can discharge `VAL-SCALE-5`, become a Tier-2 result, or be mixed with AWS points to derive `E2`
or `E4`. Every cell certifies at `capacity`, which is a statement about *provenance* — the
declaration, the observed deployment and the per-unit `/meta` all agree — and not a statement
that capacity was measured.

## What the series established

**Three cells in ten did not run flat**, and the seven that did agree to within 2.6%:

| | cells | rate/s | within-cell spread |
|---|---|---:|---|
| flat | 7 | 2,950–3,101 | 1.11–1.21 |
| degraded | 3 | 982–1,780 | 2.33–2.58 |

The healthy cluster also contains 2026-08-14's 3,152/s, taken two days earlier at a different
revision. **The environment is reproducible to a few percent when it is in the healthy regime**;
the 1.5× disagreements recorded before this series were the regime, not the environment. The
gap between 1.21 and 2.33 is wide and empty, which is what makes a within-window spread usable
as a discriminator.

```
cell        rate/s  shape spread  first    min   last acq ms   busiest strvd  p99ms  fix%
cell-01       1780  decay   2.33   2823   1210   1210   1.63  a1 4.0/4     0   24.0  41.7
cell-02       3138   flat   1.21   3454   2849   2927   0.84  a3 3.6/4     0   13.0  73.5
cell-03       3100   flat   1.19   3307   2823   2968   0.91  a1 3.7/4     0   13.1  72.7
cell-04       1308  decay   2.58   2140    829    829   2.52  a4 3.9/4     0   33.4  30.7
cell-05       3129   flat   1.16   3435   2962   3038   0.93  a3 3.4/4     0   13.0  73.4
cell-06       3049   flat   1.19   3425   2886   2958   1.20  a1 3.5/4     0   13.3  71.5
cell-07       1630    dip   2.47   2394    970   1533  41.38  a1 4.0/4     3   61.3  38.2
cell-08       3055   flat   1.18   3351   2839   2875   0.97  a1 3.4/4     0   13.0  71.6
cell-09       3148   flat   1.11   3247   2949   2949   0.83  a2 3.8/4     0   11.7  73.8
cell-10       3104   flat   1.11   3298   2974   2974   0.94  a1 3.9/4     0   12.6  72.8
```

Regenerate it from the cells with
`./test/scripts/itc-classify.py docs/measurements/pr4a-rehearsal/repeats/cell-*/`.

## The three degraded cells are not one fault

Host CPU, and the service's share of it, split them in two — the first evidence of its kind,
because `node_exporter` was not present when the earlier degraded cell was captured:

| cell | host CPU | service CPU | non-service | iowait | authorities affected |
|---|---:|---:|---:|---:|---|
| flat (7 cells) | 7.57–7.70 | 1.99–2.03 | 5.57–5.71 | 0.79–0.88 | — |
| cell-01 | **8.35** | 0.90 | **7.45** | 0.45 | all four, equally |
| cell-04 | **8.40** | 0.70 | **7.70** | 0.24 | all four, equally |
| cell-07 | **5.09** | 0.87 | 4.22 | 0.62 | **authority-1 only** |

**Cells 01 and 04 — the machine did more work while the services did less.** Non-service CPU
rises by about two cores over the healthy figure while throughput halves, uniformly across all
four authorities, with iowait at its lowest. Something on this host consumed roughly two cores
that alloca-go did not, and no instrument in this deployment can currently name it: PostgreSQL
is the largest unmeasured resident, and autovacuum and checkpointing are the two mechanisms with
this profile, but neither is established by these cells.

**Cell 07 — one authority stalled and the rest went idle.** Per authority, inside the measured
window:

| authority | acquired /4 | mean acquire | peak acquire | empty-acquire peak |
|---|---:|---:|---:|---:|
| **authority-1** | **3.7** | **26.4 ms** | **41.4 ms** | **10.0 s/s** |
| authority-2 | 1.0 | 0.03 ms | 0.23 ms | 0.09 s/s |
| authority-3 | 0.9 | 0.02 ms | 0.12 ms | 0.04 s/s |
| authority-4 | 1.2 | 0.04 ms | 0.33 ms | 0.13 s/s |

`WL-MUT-DISP-4` round-robins four organisations through a **closed-loop** generator, so when one
authority slows, its workers block and the other three units stop being asked. The starvation is
mechanical, not a second fault — but its consequence is not cosmetic: **one sick unit collapses
the whole cell's throughput, and in aggregate all four units look sick.** For a `G1/G2/G4`
comparison that is a direct threat to the result, because a point can be destroyed by one slow
unit and the summed panels will not say which.

Pool lifecycle is flat at zero throughout every cell — no connections were constructed or
destroyed after the first — so connection churn is excluded as a mechanism for either mode.

## This contradicts `ag-sept-pr4.md` §3.13.1, which needs correcting

That section concluded, of the retained 2026-08-13 degraded cell, that *"the degraded pool was
not saturated… holds 6–9 connections idle while acquired sits at 7–10 of 16. Whatever is
limiting it, it is not pool capacity."* Read **per authority**, the same retained cell
([`../windows/60s/`](../windows/60s/)) says:

| authority | acquired /4 | acquire concurrency |
|---|---:|---:|
| **authority-3** | **3.4** | **5.55 s/s** |
| authority-4 | 3.2 | 0.45 s/s |
| authority-1 | 1.0 | 0.00 s/s |
| authority-2 | 0.9 | 0.00 s/s |

It was a single-authority stall as well — the same shape as cell-07, on a different unit. Each
capacity unit has its own pool of four connections, so summing them across a stalled deployment
reports "7 of 16 acquired, 9 idle", which reads as a pool with spare capacity. The sick unit's
pool was saturated and queueing; the idle connections were on units nobody was asking.

**The corrected reading is sharper than the original.** On the sick unit the connections are
*busy, not scarce* — acquires queue because holders are slow — which places the delay inside the
query, at the database boundary, rather than in connection availability inside the service.

The implementation record has not been edited: the wording of a superseded conclusion is the
maintainer's to change, and this file states the evidence rather than rewriting the narrative.

## What is retained, and what is not

Each cell carries its `run.json`, the reseed transcript, the fixture and CPU-partition records,
the observed cpusets, the bracketing per-unit scrapes, and its own `panels/` export with
`index.json`. Every figure above is re-derivable from those.

**One TSDB snapshot, in `cell-10`, deliberately.** A Prometheus snapshot is cumulative rather
than cell-local, and `cell-10`'s spans `2026-08-16 19:04 .. 21:30` — the whole series and more.
The other nine snapshots held subsets of the same blocks, so retaining them would have cost
about 280 MB to say the same thing. Bound any query of it by the window in the relevant cell's
`panels/index.json`, exactly as when a snapshot is read for its own cell.

That snapshot is what keeps the next question answerable after Prometheus's retention drops it:
splitting `node_cpu_seconds_total` by `cpu` into the capacity units' set (0–7), the
generator/monitor set (8–11) and the idle headroom (12–15) would say whether cells 01 and 04's
two extra cores landed inside the capacity units — where PostgreSQL is — or on the measuring
side. No panel exports that series.
