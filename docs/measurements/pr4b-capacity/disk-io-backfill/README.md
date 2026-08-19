# Disk I/O series — backfilled, and why that word is in the directory name

These CSVs were **not** exported when the runs were driven. They were queried out of the surviving
Prometheus TSDB on 2026-08-19, after the runs completed, and written here so that the storage
conclusion in [`../README.md`](../README.md) rests on a retained artifact rather than on a live
query.

## What went wrong, recorded rather than quietly fixed

`--collector.diskstats` was enabled on `node_exporter` throughout, so the series existed and was
scraped for every run. What did not exist was a **panel**: `deploy/observability/panels.json` defined
no disk panel, so `export-panels.sh` retained none, and no cell under `docs/measurements/` contained
one.

The consequence is the part worth recording. The storage numbers behind PR4b's conclusion were read
from a running Prometheus and quoted directly into the implementation record and the measurements
README. That is a number without a retained artifact, which `measurement-contract.md` §5 forbids, and
it survived review because the prose looked like every other evidence-backed table in the
repository. It was caught by the maintainer asking where the figures came from.

**The durable fix is upstream of this directory**: four disk panels are now defined in
`panels.json` — `host_disk_util`, `host_disk_queue`, `host_disk_read_bytes`,
`host_disk_write_bytes` — so every future cell retains them at run time and needs no backfill. This
directory exists only because these sixteen runs predate that fix.

They are also **plotted**, as two graphs in the dashboard's host section (maintainer decision,
2026-08-19, recorded at the graph bound in `internal/observability/panels_test.go`). Defining the
panels would have closed the retention gap on its own; plotting them closes the other half that
bound's comment names — an unplotted series is *recoverable*, not *noticed*, and the storage path
is now the leading candidate behind an unresolved local knee that a future degraded cell would need
to read without knowing to look for it.

## Provenance of these files

Queried against the Prometheus instance that scraped the runs, whose TSDB volume
(`alloca-obs_prometheus-data`) survived teardown because `obs-down` preserves it by design. The
queries are the panel definitions verbatim, at the same 30 s rate range, exported at a 5 s step to
match `export-panels.sh`:

```promql
host_disk_util        sum(rate(node_disk_io_time_seconds_total{job="node"}[30s]))
host_disk_queue       sum(rate(node_disk_io_time_weighted_seconds_total{job="node"}[30s]))
host_disk_read_bytes  sum(rate(node_disk_read_bytes_total{job="node"}[30s]))
host_disk_write_bytes sum(rate(node_disk_written_bytes_total{job="node"}[30s]))
```

The column names in each CSV are those four panel keys, so a future cell's `panels/` and these files
carry the same series under the same name.

Each `<run>.csv` covers exactly that run's measured interval, taken from its own `phases.txt` —
`measured_start` to `measured_end`, so the same boundary every other server-side observation is
attributed against. 120–121 samples per 600 s run.

**These figures differ slightly from the ones first quoted in prose.** The original ad-hoc queries
used a 60 s rate range and a 30–60 s step rather than the repository's own panel parameters, so they
were neither reproducible nor consistent with how any other retained series is aggregated. The
values here supersede them, and the corrected tables in `../README.md` and `ag-sept-pr4.md` §3.30
are derived from these files.

## What they show

[`summary.txt`](summary.txt) is the per-run mean over each measured interval.

The load-bearing observation is that **mutations per MiB written is near-constant** — 65.2–72.7
across all sixteen runs and every topology — while Goodput varies by 25% within G1 alone. The work
done per byte written does not change; what changes is how many bytes the device accepted.

Reads are 0.01–1.07 MiB/s against 15–49 MiB/s written, at most ~2% of I/O volume, which is what
makes this a write-path statement rather than a working-set one.

## Scope

Host-wide, not per-authority: `node_exporter` sees the machine's block devices, and all shard groups
share one Docker data root on one VHDX. So these series cannot attribute I/O to a particular
authority, and no claim here does.
