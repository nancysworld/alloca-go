# PR4b Iteration C local capacity comparison

Twelve retained 600 s runs, driven 2026-08-19 through
[`test/scripts/itc-local-experiment.sh`](../../../test/scripts/itc-local-experiment.sh) on the
scheduler-partitioned workstation: four per topology at G1, G2 and G4 — the selected point, the
deciding higher point, and each again as an independent confirmation
(`ag-sept-validation-plan.md` §4.6.5).

## What this is, and what it is not

**`VAL-SCALE-6` is not discharged.** One topology's knee resolved and two did not, so `G1_local` and
`G2_local` are withheld, and with them `E2_local` and `E4_local`. §4.6.5 is explicit that an
unresolved knee is the *absence* of a capacity result rather than a low one, and nothing here may be
quoted as a scale efficiency.

All twelve runs certify `capacity` on the provenance ladder (`measurement-contract.md` §13.2). That
is a statement about what each run can say about itself — topology, environment, image identity —
and **not** a statement that shard-group capacity was measured. The two are independent, and this
directory is the case where the first holds and the second does not.

Nothing here is independently provisioned capacity evidence. The shared workstation, WSL kernel and
storage path remain first-class limitations, so no figure here bears on `VAL-SCALE-5`, is Tier 1 or
Tier 2, or may be mixed with an independently provisioned point.

## Configuration common to all twelve

`WL-MUT-DISP-4`, conditioned to 4,000 fresh mutations per organisation followed by a
state-preserving service/pool recycle, 600 s measured interval, `pool_max_conns=8` per shard group,
45,000 slots per organisation at capacity 20, ordinary observability only — no plan probe, no
`auto_explain`, no `pg_stat_statements`. Generator confined to CPUs 8–11, each shard group to two
CPUs. Environment: the 16-vCPU allocation recorded in [`environment.md`](../environment.md).

**S = 12 and H = 16 workers per group at every topology.** Reconnaissance
([`../pr4b-recon/`](../pr4b-recon/)) selected S=12, S=8, S=12; 12 is the measured throughput maximum
at all three and G2's dissent was 0.8%, inside single-probe noise. `E2` and `E4` compare topologies,
so the arms are held at one demand-per-group (`ag-sept-pr4.md` §3.27).

Fixture sizing is derived in [`fixture-sizing.txt`](fixture-sizing.txt) from the fastest retained
probe rather than chosen: 45,000 slots per organisation, worst-case consumption 64.1% of measured
supply.

## The runs

Rates are the full-600 s horizon average of fresh-mutation Goodput from a fixed conditioned start,
which is §4.6.5's comparison quantity. No number is read from a sub-interval.

| topology | S (12) | H (16) | S confirm | H confirm | S reproduces | H reproduces | H beats S |
|---|---:|---:|---:|---:|---|---|---|
| G1 | 1080.1 | 1032.6 | 1094.0 | 1193.8 | yes, 1.3% | **no, 15.6%** | **yes, 9.1%** |
| G2 | 2259.0 | 2323.1 | 2450.8 | 2477.7 | **no, 8.5%** | **no, 6.7%** | no |
| G4 | 3494.8 | 3509.5 | 3492.9 | 3543.5 | yes, 0.1% | yes, 1.0% | no |

**`G4_local` = 3493.9/s** — the mean of two selected-point observations agreeing to 0.1%, with a
deciding higher point that reproduces to 1.0% and does not beat it. This is the one resolved knee.

**G1 and G2 are explicitly unresolved.** At G1 the two `H` observations disagree by 15.6% and the
higher exceeds the best `S` by 9.1%; at G2 neither point reproduces within the 5% margin.

[`capacity-result.txt`](capacity-result.txt) is re-derivable from the runs' own manifests and states
the threshold it applied:

    ./test/scripts/itc-capacity-result.py docs/measurements/pr4b-capacity

## Why G1 and G2 did not resolve: the storage path

**This is the substantive finding of PR4b, and it is a property of the environment rather than of
`alloca-go`.**

Reads during the measured interval are effectively zero (0.01–0.16 MiB/s), so the working set is
cache-resident and this is a write-path story. Goodput tracks *delivered write bandwidth* at a
near-constant 65–79 mutations per MiB across every topology, and within G1 it does so run by run:

| G1 run | write MiB/s | Goodput/s |
|---|---:|---:|
| `g1-s` | 15.30 | 1080.1 |
| `g1-h` | 15.07 | 1032.6 |
| `g1-s-confirm` | 15.32 | 1094.0 |
| `g1-h-confirm` | **18.28** | **1193.8** |

The run that "beat" its selected point is the run that obtained 19% more write bandwidth.

Across topologies, the device is never idle at any of them — `node_disk_io_time` utilisation is
≈1.0 everywhere — but it is **not saturated**: G4 extracts 2.5× G1's bandwidth from the same device
at roughly three times the queue depth.

| topology | authorities | disk queue depth | write MiB/s | run-to-run spread |
|---|---|---|---|---|
| G1 | 1 | 0.87–1.26 | 15.07–18.28 | **25.1%** (four identical runs) |
| G2 | 2 | 2.96–4.01 | 30.46–35.97 | 8.5% |
| G4 | 4 | 3.89–4.86 | 44.64–47.18 | **1.0%** |

**Reproducibility improves sharply with the number of authorities issuing I/O concurrently.** G1
drives one write stream at queue depth ≈1: per-I/O service-time variation on the storage path passes
straight through to throughput with no concurrency to average it out. G4's four independent streams
smooth the same variation.

The G1 spread is measured directly rather than inferred from the comparison — see
[`../pr4b-drift-g1/`](../pr4b-drift-g1/), four *identical* 600 s runs at 12 workers that came out
1255.9, 1023.1, 1202.1 and 1279.7/s.

**The consequence for `VAL-SCALE-6`.** `G1_local` is the denominator of both efficiencies and is the
least reproducible quantity in the experiment. On this environment neither `E2_local` nor `E4_local`
can be derived to a 5% margin, because their denominator cannot be measured to better than ~25%.
This is exactly the term [`environment.md`](../environment.md) names as the one the instruments
cannot see — Docker Desktop's default VHDX on the Windows host — and it is where PR2's unexplained
excursions also live. Independently provisioned per-unit storage is the condition that would remove
it, which is a direct input to the case for PR4c.

**Stated as a limitation, not a diagnosis.** That low queue depth is the *mechanism* by which the
storage path's variability reaches G1's throughput is consistent with every measurement above and is
not proven: the killing test — driving G1 with its data directory off the VHDX and observing whether
the spread collapses — was deliberately not run, because PR4b's budget was spent and the maintainer
closed execution rather than draw contingency (2026-08-19).

## Reading a run

Each directory holds `run.json` (manifest, per-group totals, certification), `conditioning.json`,
`phases.txt` (the boundaries every server-side observation is attributed against), `slices.txt` (ten
contiguous 60 s slices), `fixture.txt`, `seed.txt`, the CPU partition as declared and as observed,
and `panels/` (the retained time series). TSDB snapshots are not promoted here — they remain beside
the runs under `test/results/` and are ~123 MB each.

    ./test/scripts/itc-slices.py docs/measurements/pr4b-capacity/g4-s
    ./test/scripts/itc-classify.py docs/measurements/pr4b-capacity/g4-s

Every run is a "dip" in shape — spread 1.42–1.61 at G1, 1.37–1.42 at G4 — so no run belongs to a
visibly different regime, and the G1 outlier sits above the other three at every one of its ten
slices rather than diverging part-way.

## Co-varying properties that must be named beside any figure here

**Per-authority data volume changes with the topology by design.** Four organisation datasets sit on
one authority at G1 and one each at G4, so working-set size and cache residency per authority are
properties of the sharding experiment rather than variables held constant (§4.6.6).

**Every run declines across its window**, 0.69–0.81× from first to last 60 s slice. The comparison
quantity is the full-600 s horizon average from a fixed conditioned start precisely because the
trajectory is not stationary; slices describe and disqualify, and never select (§4.6.5). Whether an
indefinitely growing dataset is the right thing for this benchmark to represent is carried to
Analyse & Review.
