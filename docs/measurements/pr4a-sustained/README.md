# PR4a sustained qualification and pool-policy sensitivity

Five retained 600 s runs, driven 2026-08-18 through
[`test/scripts/itc-local-experiment.sh`](../../../test/scripts/itc-local-experiment.sh) on the
scheduler-partitioned workstation.

## What these are, and what they are not

**Qualification evidence for the corrected Iteration C method, and the evidence behind one
configuration decision.** They establish that the method runs cleanly for the canonical duration
at the hardest local topology, and they fix `pool_max_conns` for the shard-group capacity unit.

They are **not** a capacity result. No `E2_local` or `E4_local` follows from them, 16 workers per
group is not a selected `S`, and nothing here discharges `VAL-SCALE-5` or `VAL-SCALE-6`. PR4b
owns saturation reconnaissance, `S`/`H` selection and the retained capacity comparison
(`ag-sept-validation-plan.md` §4.6.4–§4.6.5).

Every run is a *single* observation. `measurement-contract.md` treats a single reading as
provisional, and none of these was repeated.

## Configuration common to all five

`WL-MUT-DISP-4`, conditioned to 4,000 fresh mutations per organisation followed by a
state-preserving service/pool recycle, 600 s measured interval, 16 workers per shard group with
one independent demand stream each, 50,000 slots per organisation at capacity 20, ordinary
observability only — no plan probe, no `auto_explain`, no `pg_stat_statements`. Generator confined
to CPUs 8–11, each shard group to two CPUs. All five certified `capacity`.

Fixture sizing is derived in [`fixture-sizing.txt`](fixture-sizing.txt) from the measured G4
aggregate rate rather than chosen. Consumption reached 47.1% of the measured supply at worst, so
no run was near exhaustion.

## The runs

| directory | topology | `pool_max_conns` | 600 s Goodput | sustained rate |
|---|---|---:|---:|---:|
| [`g1-pool4`](g1-pool4/) | G1 | 4 | 566,651 | 944.4/s |
| [`g1-pool8`](g1-pool8/) | G1 | 8 | 749,181 | **1,248.6/s** |
| [`g1-pool16`](g1-pool16/) | G1 | 16 | 627,960 | 1,046.6/s |
| [`g4-pool4`](g4-pool4/) | G4 | 4 | 1,714,342 | 2,857.1/s |
| [`g4-pool8`](g4-pool8/) | G4 | 8 | 2,061,575 | **3,436.0/s** |
| [`g4-pool16`](g4-pool16/) | G4 | 16 | 1,877,629 | 3,129.3/s |

The policy was decided on G1, which is where the variable is isolated. G4 was then driven at all
three values, and reproduces the same ordering: 8 is the peak on both topologies.

## Two findings the artifacts carry

**The pool policy is frozen at 8** (`ag-sept-pr4.md` §3.22). 4 was an admission ceiling; 16
removed the admission queue entirely and converted it into database and host contention, for 16%
*less* Goodput than 8.

**Every run declines and then plateaus** — G1 0.74×/0.80×/0.69×, G4 0.76×/0.81×/0.73× from first
60 s slice to last, in each case flattest at pool=8. This is not the cached-plan regime of §3.20 and appears to follow accumulated
dataset growth; it is recorded for Analyse & Review in §3.21 and is unresolved.

## Reading a run

Each directory holds `run.json` (manifest, per-group totals, certification), `conditioning.json`
(the conditioning phase's own artifact), `phases.txt` (the boundaries every server-side
observation is attributed against), `slices.txt` (ten contiguous 60 s slices), `fixture.txt`,
`seed.txt`, the CPU partition as declared and as observed, and `panels/` (the retained
time series). The TSDB snapshots are not promoted here — they remain beside the runs under
`test/results/` and are large.

    ./test/scripts/itc-slices.py docs/measurements/pr4a-sustained/g1-pool8
    ./test/scripts/itc-classify.py docs/measurements/pr4a-sustained/g1-pool8
