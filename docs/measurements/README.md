# Measurements

This directory holds the project's **empirical record**: the reports that state what was
measured, and the retained artifacts each report is derived from.

The split is the point.

- **[`reports/`](reports/)** — the prose. One report per PR that produced a result. This is
  what a reader reads.
- **[`environment.md`](environment.md)** — the original local measurement environment used by the
  earlier AG-Sept runs. Later experiments may use a different local allocation; each run's own
  manifest is authoritative for the environment and utilisation denominator of that run.
- **Everything else** — one directory per experiment, holding the files a run emitted. This is
  what a reader checks the prose against.

A report may quote no number that is not re-derivable from a retained file
([`measurement-contract.md`](../design/measurement-contract.md) §2, §5.3). That rule is why the
artifact directories are large and are kept anyway: without them a report is an assertion.

## Index

### Reports

| Report | Milestone | What it establishes |
|---|---|---|
| [AG-Sept PR2 — single-instance frontier](reports/ag-sept-pr2-single-instance-frontier.md) | AG-Sept | **The load-bearing result so far.** This machine reaches ~4,300 booking req/s and the limiting subsystem is PostgreSQL, not alloca-go — so service replicas raise throughput only up to the database's frontier and cannot lift the saturated ceiling beyond it. The sub-mechanism inside PostgreSQL (write-path contention, led by `LWLock:WALWrite`) is provisional pending database-side instrumentation. |
| [AG-Sept PR3c — Phase 1 correctness and failure isolation](reports/ag-sept-pr3c-phase1-correctness.md) | AG-Sept | Two independent writable authorities compose without weakening any accepted transaction semantic, and one failing does not reach the other: no request failed over to the surviving writer, and no organisation holds a row on an authority that does not own it. A third, separately retained failure run caught a real `unknown_replayable` and resolved it under its own key, with the measurement and reconciliation populations differing by exactly that replay — the client could not settle the outcome, and resolution proved the original had never committed. **Correctness only — it claims no capacity or throughput multiplier**, and none is available from a machine where both authorities, both service units and the generator contend for one 10-vCPU allocation. |

### Artifacts

| Directory | Produced by | Contents |
|---|---|---|
| [`pr2-frontier/`](pr2-frontier/) | `test/scripts/sweep.sh` | The frontier sweeps: `dispersed/` (concurrency ladder), `pool/` + `pool-repeat/` (pool ladder), `plateau/` + `plateau-repeat/` (the two combined), `contended-1/` + `contended-2/` (hot-slot and hot-identity), and `postgres-waits/` (database-side sampling, diagnostic only) |
| [`pr3c-phase1/`](pr3c-phase1/) | `test/scripts/pr3c-experiments.sh` | Two passes of the Phase 1 matrix on the two-authority topology: `controls` assertions, `correctness`, `distribution` (one-hot organisation), `refusal` (cross-authority control) and `failure-isolation` (one authority stopped and restored mid-run). Each cell holds its run, verdict and both units' scrape pairs; the failure cell adds the per-authority row census and both units' readiness during the fault. [`failure-with-ambiguity/`](pr3c-phase1/failure-with-ambiguity/) is a **third** failure run, kept beside the passes rather than inside one: its fault produced a real `unknown_replayable` (resolved `replay=false`, so no commit had landed), and it is the only live demonstration of the §12 population contract. It predates the harness hardening, and its own README says what that costs. [`controls-replay/`](pr3c-phase1/controls-replay/) is a later **controls-only** run holding one transcript and no measured cell: it carries the same-key repost that VAL-COR-4's replay clause needs and that neither pass contains, added when the Iteration B A&R found the gap |
| [`pr4a-rehearsal/`](pr4a-rehearsal/) | `test/scripts/itc-run.sh` | The local 12-vCPU rehearsal of the Iteration C 1/2/4 topology: `points/` holds the two operating points (`c=16`, `c=32`), `windows/` three cells differing only in window length. **Rehearsal/diagnostic only — no capacity claim rests on any of them.** The 120 s cell is fixture-bound and its rate must not be quoted; the `points/` cells hold endpoints only, so their within-window shape is not re-derivable. Kept because the 60 s cell reproduces PR2's open throughput anomaly — throughput and service CPU both roughly halving, while acquired connections fall to 7 *and* acquire-wait rises, which makes connection availability part of the diagnosis. Two readings are explicitly **not** supported: the generator's CPU drop is not evidence (`alloca-load` is closed-loop, so it falls with throughput mechanically), and a configured max of 16 is a ceiling rather than a population, so "connections exist and are withheld" does not follow. `windows/` are the first cells here to retain per-cell panel series rather than endpoints alone. **Every retained sample here carries `topology=single-instance-local`, which is wrong** — see the directory README |
| [`pr4a-sustained/`](pr4a-sustained/) | `test/scripts/itc-local-experiment.sh` | Six retained 600 s G1/G4 qualification runs used to validate the corrected conditioned measurement path and freeze `pool_max_conns=8`. All certify at `capacity` provenance, but they remain **qualification evidence, not the PR4b capacity result**; no `E2_local`/`E4_local` is derived from them. |
| [`pr4b-recon/`](pr4b-recon/) | `test/scripts/itc-local-experiment.sh recon` | Adaptive `workers_per_group` reconnaissance reports for G1/G2/G4. **No rate here may be quoted or entered into `E2`/`E4`** — a 120 s probe reads the early part of a declining trajectory, and its only output is the two levels it names. Also records the limitation reconnaissance discovered about itself: G1 at 16 workers, probed four times under identical configuration, spans 10.5%, so **the 5% margin is about one standard deviation of a single probe**. |
| [`pr4b-capacity/`](pr4b-capacity/) | `test/scripts/itc-local-experiment.sh capacity` | The twelve retained 600 s G1/G2/G4 runs. **`VAL-SCALE-6` is executed and NOT discharged.** `G4_local` = 3493.9/s resolved; `G1` and `G2` are explicitly unresolved, so `G1_local`, `G2_local`, `E2_local` and `E4_local` are all withheld. Goodput tracks delivered write bandwidth at a near-constant 65–72 mutations per MiB, which places the limit on the shared storage path rather than on `alloca-go`. The disk series behind that conclusion are in [`pr4b-capacity/disk-io-backfill/`](pr4b-capacity/disk-io-backfill/) and were recovered from the surviving TSDB **after** the runs, because no disk panel existed to retain them at the time — see `ag-sept-pr4.md` §3.31. |
| [`pr4b-drift-g1/`](pr4b-drift-g1/) | `test/scripts/itc-local-experiment.sh drift` | Four *identical* 600 s G1 runs, the control that measured G1's own reproducibility at **25.1%** and refuted run position as the explanation for the unresolved knee. **Not a capacity result and it selects nothing**; `G1_local` is withheld rather than estimated from this population. |
| [`pr2-generator-control/`](pr2-generator-control/) | `test/scripts/control-generator.sh` | The mandatory VAL-NEG-2 generator-headroom control, at ~2,150 req/s |
| [`pr2-generator-control-plateau/`](pr2-generator-control-plateau/) | `test/scripts/control-generator.sh` | The same control re-run at the ~4,300 req/s operating point the PR2 conclusion rests on |
| [`pr2-telemetry/`](pr2-telemetry/) | `test/scripts/sweep.sh` | The VAL-NEG-3 telemetry comparison: `full` and `metrics_only`, two passes each. VAL-NEG-3 is **not discharged** — within-mode spread exceeded the between-mode delta, so no overhead figure is claimed |
| [`pr1-smoke-run/`](pr1-smoke-run/) | `cmd/alloca-load` | PR1's substrate smoke run. Proves the harness works; establishes **no** capacity number |
| [`pr1-telemetry-overhead/`](pr1-telemetry-overhead/) | `go test -bench` | Per-call cost of the telemetry `Tee` against a real file sink: `raw.txt` holds the five samples, and [`environment.txt`](pr1-telemetry-overhead/environment.txt) the machine and exact command. It predates the run manifest, which is why it carries its own environment file |

## What a sweep cell contains

Every cell directory is self-describing, so a figure can be checked without re-running
anything:

| File | Why it is kept |
|---|---|
| `run.json` | the client's own totals, plus the manifest (service SHA, pool size, PostgreSQL version, concurrency, window) and the `quotability` verdict |
| `cell.json` | the one-line summary the sweep's table is built from |
| `verdict.json` | the reconciliation checks — including server totals against client totals — that make the run admissible at all |
| `metrics-baseline.txt`, `metrics.txt` | the two scrapes bracketing the measured window; counters are cumulative, so the delta is the measurement |
| `panels/*.csv` + `panels/index.json` | the exported time series, with the **resolved** PromQL and window recorded beside the data — a rate over 5s and a rate over 60s are different measurements. `index.json` records two windows: `window` is the cell's measured phase and the authority for what was measured; `query_window` is what the range queries actually cover, one rate range later, so no exported point can reach back into warm-up |
| `tsdb-snapshot/` | the Prometheus snapshot, so a series nobody thought to export is still recoverable. **Cumulative, not cell-local** — Prometheus blocks are shared history, so a snapshot carries other cells' windows too. Always bound a query by `panels/index.json`'s `window`. Redesign remains deferred and is not yet scheduled to an iteration; see the frontier report §6.2 |
| `warmup.json` | the discarded warm-up phase, kept because a warm-up that behaved oddly explains a strange cell — and in PR2 it turned out to be the key diagnostic |
| `*.log` | seed, service, load, export and verify output. **Git-ignored** — `service.log` alone is ~34 MB per cell at full telemetry |

## Conventions

**Every figure carries exactly one evidence label** — `[MEASURED]`, `[DERIVED]`,
`[PRIOR-UNREPRODUCED]`, `[HYPOTHESIS]` — per
[`measurement-contract.md`](../design/measurement-contract.md) §2. A model fit is never
`[MEASURED]`, however well it fits.

**Every run declares what it may be quoted for.** `run.json`'s `quotability.level` is one of
`none | local | capacity | publishable`, with `blocked_from` and `blocked_because` naming each
missing manifest field. Older artifacts predate some manifest fields and therefore certify only at
the level their own `run.json` records. The retained [`pr4a-rehearsal/`](pr4a-rehearsal/) and
[`pr4a-sustained/`](pr4a-sustained/) runs certify at `capacity`; this is a provenance statement,
not promotion of either set into a capacity result. In every retained local experiment here the
generator shares the workstation with serving components, which puts `publishable` out of reach
([`measurement-contract.md`](../design/measurement-contract.md) §13.2). A report may not promote a
number past its artifact's level.

**`quotability.level` is a provenance verdict, not an evidence class.** `capacity` means the
manifest is complete enough for a capacity claim — declaration, observed deployment and per-unit
reconciliation all agree. It does not mean the run *is* capacity evidence. The PR4a rehearsal and
sustained qualification sets are worked examples: both can certify `capacity` while remaining
non-capacity evidence by experiment purpose. Read the run's `manifest.environment` alongside its
level; the two answer different questions.

**Anomalies are retained, not dropped.** A cell that reads oddly stays in the record with its
diagnostic panels, and the report explains it. PR2's unexplained ~2× excursions are the worked
example — see that report's §4.

**Reports are named for the claim, not the schedule.** `ag-sept-pr2-single-instance-frontier.md`
says what it establishes; a file called `pr2.md` would make a later reader reconstruct the PR
sequence to know what the number is good for.

**A utilisation figure names its denominator.** The run or report must state the CPU allocation
used for that figure rather than assuming the current host configuration. Earlier AG-Sept reports
use the 10-vCPU WSL2 allocation recorded in [`environment.md`](environment.md); later PR4a artifacts
carry their own environment declaration and observed CPU partition in `run.json`. The per-run
manifest is authoritative when the shared environment note and a later experiment differ.

## Adding a report

1. Run the experiment with a version-controlled script, into a new artifact directory here.
   `sweep.sh` requires a clean working tree — Go stamps an untracked file as a modified tree,
   which would refuse every cell at level `none`.
2. Commit the artifacts first, so the report is written against what is retained rather than
   against a terminal.
3. Write `reports/<milestone>-<pr>-<claim>.md`, deriving every number from a retained file.
4. Add it to the index above, and link it from whichever design document owns the concern it
   changes — a result nobody can find from the design record will not be found.

## Disclosure

Workloads are **synthetic engineering models**, never descriptions of any organisation's
system. Environment details are recorded to the extent needed to interpret a number and no
further; see [`../public-disclosure-policy.md`](../public-disclosure-policy.md).
