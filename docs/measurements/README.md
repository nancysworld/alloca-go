# Measurements

This directory holds the project's **empirical record**: the reports that state what was
measured, and the retained artifacts each report is derived from.

The split is the point.

- **[`reports/`](reports/)** — the prose. One report per PR that produced a result. This is
  what a reader reads.
- **Everything else** — one directory per experiment, holding the files a run emitted. This is
  what a reader checks the prose against.

A report may quote no number that is not re-derivable from a retained file
([`measurement-contract.md`](../design/measurement-contract.md) §2, §5.3). That rule is why the
artifact directories are large and are kept anyway: without them a report is an assertion.

## Index

### Reports

| Report | Milestone | What it establishes |
|---|---|---|
| [AG-Sept PR2 — single-instance frontier](reports/ag-sept-pr2-single-instance-frontier.md) | AG-Sept | **The load-bearing result so far.** This machine reaches ~4,300 booking req/s and the limit is PostgreSQL's write-ahead log, not alloca-go — so adding service replicas against one database raises availability, not throughput. |

### Artifacts

| Directory | Produced by | Contents |
|---|---|---|
| [`pr2-frontier/`](pr2-frontier/) | `test/scripts/sweep.sh` | The frontier sweeps: `dispersed/` (concurrency ladder), `pool/` + `pool-repeat/` (pool ladder), `plateau/` + `plateau-repeat/` (the two combined), `contended-1/` + `contended-2/` (hot-slot and hot-identity), and `postgres-waits/` (database-side sampling, diagnostic only) |
| [`pr2-generator-control/`](pr2-generator-control/) | `test/scripts/control-generator.sh` | The mandatory §12.2 generator-headroom control, at ~2,150 req/s |
| [`pr2-generator-control-plateau/`](pr2-generator-control-plateau/) | `test/scripts/control-generator.sh` | The same control re-run at the ~4,300 req/s operating point the PR2 conclusion rests on |
| [`pr2-telemetry/`](pr2-telemetry/) | `test/scripts/sweep.sh` | The §6.2 telemetry comparison: `full` and `metrics_only`, two passes each. §6.2 is **not discharged** — within-mode spread exceeded the between-mode delta, so no overhead figure is claimed |
| [`pr1-smoke-run/`](pr1-smoke-run/) | `cmd/alloca-load` | PR1's substrate smoke run. Proves the harness works; establishes **no** capacity number |
| [`pr1-telemetry-overhead/`](pr1-telemetry-overhead/) | `go test -bench` | Per-call cost of the telemetry `Tee` against a real file sink |

## What a sweep cell contains

Every cell directory is self-describing, so a figure can be checked without re-running
anything:

| File | Why it is kept |
|---|---|
| `run.json` | the client's own totals, plus the manifest (service SHA, pool size, PostgreSQL version, concurrency, window) and the `quotability` verdict |
| `cell.json` | the one-line summary the sweep's table is built from |
| `verdict.json` | the reconciliation checks — including server totals against client totals — that make the run admissible at all |
| `metrics-baseline.txt`, `metrics.txt` | the two scrapes bracketing the measured window; counters are cumulative, so the delta is the measurement |
| `panels/*.csv` + `panels/index.json` | the exported time series, with the **resolved** PromQL and window recorded beside the data — a rate over 5s and a rate over 60s are different measurements |
| `tsdb-snapshot/` | the Prometheus snapshot, so a series nobody thought to export is still recoverable. **Cumulative, not cell-local** — Prometheus blocks are shared history, so a snapshot carries other cells' windows too. Always bound a query by `panels/index.json`'s `window`. Redesign deferred to PR3; see the frontier report §6.2 |
| `warmup.json` | the discarded warm-up phase, kept because a warm-up that behaved oddly explains a strange cell — and in PR2 it turned out to be the key diagnostic |
| `*.log` | seed, service, load, export and verify output. **Git-ignored** — `service.log` alone is ~34 MB per cell at full telemetry |

## Conventions

**Every figure carries exactly one evidence label** — `[MEASURED]`, `[DERIVED]`,
`[PRIOR-UNREPRODUCED]`, `[HYPOTHESIS]` — per
[`measurement-contract.md`](../design/measurement-contract.md) §2. A model fit is never
`[MEASURED]`, however well it fits.

**Every run declares what it may be quoted for.** `run.json`'s `quotability.level` is one of
`none | local | capacity | publishable`, with `blocked_from` and `blocked_because` naming each
missing manifest field and the PR that owns it. **Every run in this directory is `local`**: the
load generator shared a host with the service, so nothing here is a publishable capacity claim.
A report may not promote a number past its artifact's level.

**Anomalies are retained, not dropped.** A cell that reads oddly stays in the record with its
diagnostic panels, and the report explains it. PR2's unexplained ~2× excursions are the worked
example — see that report's §4.

**Reports are named for the claim, not the schedule.** `ag-sept-pr2-single-instance-frontier.md`
says what it establishes; a file called `pr2.md` would make a later reader reconstruct the PR
sequence to know what the number is good for.

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
