# Measurement environment

The workstation every `[MEASURED]` figure in this directory was produced on, unless a report
says otherwise. Recorded because a throughput number without its machine is not reproducible,
and because "cores" is ambiguous here in a way that changes what a utilisation figure means.

Values below are read from the machine, with the command that produced each block, rather than
transcribed from a spec sheet.

**This workstation has had two guest CPU allocations, and they are two environments rather than a
before and after.** WSL was reconfigured from 10 to 16 logical CPUs on 2026-08-13
(`ag-sept-pr4.md` §2.14, which owns the decision and its comparability consequence):

| Guest allocation | Captured | Measurements taken under it |
|---|---|---|
| **10 vCPUs** | 2026-08-04 | PR1 smoke and telemetry overhead, PR2 frontier, PR2 generator and telemetry controls, PR3c Phase 1 |
| **16 vCPUs** | 2026-08-19 | PR4a rehearsal, PR4a sustained qualification and pool policy, and Iteration C onward |

The consequence is stated in §2.14 and is not softened here: a local re-run of a PR2 cell on the
current allocation would neither confirm nor refute the PR2 result. Nothing already recorded
changes, because no earlier figure was measured under 16; what changes is that the two groups of
runs may not be mixed. Every run also records its own observed values in `run.json`'s manifest
(`server_gomaxprocs`, `generator_gomaxprocs`, `generator_num_cpu`), so a run is self-identifying
even where a reader skips this table.

Both captures appear below. The **16-vCPU capture is current**; the 10-vCPU one is retained
because the measurements it describes are retained.

## Two distinctions that matter

**1. The denominator is the allocation, not the host.** `process_cpu_seconds_total` is read
inside WSL2, where `runtime.NumCPU()` is whatever `.wslconfig` says `processors` is — **10** for
the runs in the first row of the table above, **16** for the second. So a PR2 figure of "1.2
cores" is 1.2 within a **10-vCPU allocation**, not 1.2 of the host's 20. A reader who assumes the
host draws a utilisation figure wrong by 2×, in the direction that flatters the result. Take the
denominator from the run's own manifest rather than from this document's current value.

**2. Those vCPUs are shared by the whole guest, not reserved for the service.** The metric
covers the `alloca-go` process and nothing else. PostgreSQL, the load generator, Prometheus,
Grafana and Docker/WSL overhead are all outside it — and all of them run on the *same*
allocation: WSL2 uses one utility VM for every distro, so the `docker-desktop` distro shares it,
and a container on this machine reports the same `nproc` as the guest, not a separate allocation.

Iteration C narrows this second point without removing it. A partitioned run confines each shard
group and the generator to disjoint scheduler-visible CPU sets (`ag-sept-pr4.md` §2.14), so
"shared by the whole guest" no longer describes the units under test — but the kernel, the Docker
daemon, storage and page cache are still shared, which is why a local Iteration C result stays a
shared-host characterisation and not independently provisioned capacity evidence
(`ag-sept-validation-plan.md` §4.6.7).

> So a service-process CPU figure bounds **the application's demand**. It does **not** say how
> much of the allocation, the guest, or the host was idle. PR2 measures no total utilisation
> for any of the three.

This is not pedantry about wording. It is why §4's excursions — which slow the service, the
database *and* the generator simultaneously — are consistent with contention for one shared
10-vCPU pool.

**The instrument that would settle it now exists, but not for those runs.** A `node_exporter`
with a restricted collector set, its own scrape job and a per-cell gate that refuses a run
retaining no host samples was added for Iteration C (`ag-sept-pr4.md` §3.14). So an Iteration C
run carries host evidence and PR2's runs do not, and PR2's excursions remain unresolved rather
than retrospectively answerable. Note also that PSI is unavailable on this WSL2 kernel, so
`node_load1` is the coarser stand-in: it bounds the stall question rather than answering it.

## Host

**Captured 2026-08-04 and unchanged by the guest reconfiguration** — the physical machine was not
touched, only `.wslconfig`. `NumberOfCores` and `NumberOfLogicalProcessors` were re-read on
2026-08-19 and still report 20/20; the memory and OS lines are the original capture.

```
Name                      : Intel(R) Core(TM) i7-14700F
NumberOfCores             : 20
NumberOfLogicalProcessors : 20
MaxClockSpeed             : 2100
TotalPhysicalMemory       : 32 GB
OS                        : Microsoft Windows 11 Home
```
<sub>`powershell.exe -NoProfile -Command "Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors,MaxClockSpeed"`</sub>

Note the machine reports **20 logical processors for 20 cores**. The part's specification is
8 P-cores + 12 E-cores with SMT on the P-cores, which would give 28 threads — so SMT is
evidently not enabled here. Recorded as measured rather than as specified, since the reports
are read against what the machine actually offered.

## WSL2 guest — where the service, the generator and the tests run

### Current — 16 vCPUs, captured 2026-08-19

```ini
[wsl2]
memory=12GB
processors=16
swap=2GB
localhostForwarding=true
```
<sub>`%USERPROFILE%\.wslconfig` on the Windows host.</sub>

```
nproc                : 16
model name           : Intel(R) Core(TM) i7-14700F
MemTotal             : 11 GiB visible
kernel               : 5.15.167.4-microsoft-standard-WSL2
go version           : go1.26.5 linux/amd64
```
<sub>`nproc`, `/proc/cpuinfo`, `free -h`, `uname -r`, `go version`</sub>

Only the CPU allocation changed: `memory=12GB` is unchanged and the guest still sees 11 GiB, so
memory is **not** a variable between the two environments. Every Iteration C run additionally
retains the partition as declared and as observed, in `cpu-partition.txt` and
`observed-cpusets.txt` beside the run, with `nproc` recorded in the first.

**`GOMAXPROCS` is 16 by default** under this allocation, but no Iteration C measurement runs with
that default: each service and each PostgreSQL authority is confined to a two-CPU set and the
generator to four, so the retained runs record `server_gomaxprocs=2` and `generator_gomaxprocs=4`
(`ag-sept-pr4.md` §2.14).

### Superseded — 10 vCPUs, captured 2026-08-04

Retained because PR1, PR2 and PR3c were measured under it.

```ini
[wsl2]
memory=12GB
processors=10
swap=2GB
localhostForwarding=true
```

```
nproc                : 10
model name           : Intel(R) Core(TM) i7-14700F
MemTotal             : 11 GiB visible
kernel               : 5.15.167.4-microsoft-standard-WSL2
go version           : go1.26.5 linux/amd64

wsl.exe -l -v        : Ubuntu (running), docker-desktop (running)
docker run alpine nproc : 10   <- the same ten, not a separate allocation
```

**`GOMAXPROCS` was therefore 10 by default** for both the service and the load generator under
this allocation, and those runs were unpartitioned — `pr2-frontier` manifests record
`server_gomaxprocs=10` and `generator_num_cpu=10`, which is how a 10-vCPU run identifies itself
without reference to this document.

## PostgreSQL

Docker version 29.6.2, image `postgres:16-alpine`, server version **16.14**, started by
`make db-up` with no tuning applied. The settings that shaped the results:

| Setting | Value | Where it shows up |
|---|---|---|
| `max_connections` | 100 | caps PR2's pool ladder — pool 80 plus the seeder and verifier fits; 160 would not. Iteration C is nowhere near it: `pool_max_conns=8` per shard group is 8 against each authority's own 100, since every group has its own database |
| `shared_buffers` | 128 MB | the stock default, on a 32 GB host |
| `checkpoint_timeout` | 5 min | investigated as a cause of the §4 excursion, and ruled out |
| `max_wal_size` | 1 GB | requested checkpoints were observed under sweep write volume |

**Storage is Docker Desktop's default VHDX on the Windows host**, not a tuned volume. This is
the term the reports cannot see and repeatedly ran into: the write-ahead-log ceiling of §5 and
the unexplained excursions of §4 both live on this path, and neither a service exporter nor a
PostgreSQL exporter can observe it.

## What this environment is not

It is a **developer workstation**, and no figure measured here is an externally presented capacity
claim. An untuned container on a laptop-class storage path says nothing about a tuned database on
provisioned storage.

It does **not** follow that every run here is `local`. That was true of PR1, PR2 and PR3c and is
no longer true of Iteration C: the PR4a sustained runs certify **`capacity`**, because
co-residency is a `publishable` bar only and those runs populate the topology and environment
provenance `measurement-contract.md` §13.2 asks for. A `capacity`-level run is a result about *the
explicitly recorded environment* — this one, shared kernel and storage included — which is exactly
why `ag-sept-validation-plan.md` §4.6.7 keeps a local Iteration C result out of `VAL-SCALE-5`.

An externally presented capacity claim needs the separate generator compute of
`measurement-contract.md` §13.1. AG-Sept does not fund it — the deployment path that would have
supplied it is withdrawn (scheduling: `ag-sept-plan.md` §6.3) — so **no AG-Sept run reaches
`publishable`**, and the milestone closes with a bounded local frontier rather than an externally
presented capacity number.

Co-residency blocks that level and no other. A run on this workstation **can** reach `capacity`
once it records the topology and environment provenance §13.2 requires — the Iteration C runs do,
and the earlier runs retained here are `local` because those operator-supplied fields were not
populated when they were taken, not because the generator shared the host.

## Per-run records

This file is the shared description; it is not the authority for any individual run. Each run
carries its own environment in `run.json`'s manifest — service and generator commit SHAs, Go
versions, `GOMAXPROCS`, pool size, PostgreSQL version, timeout budget, reservation TTL — and
the manifest is what a report must be checked against. `pr1-telemetry-overhead/environment.txt`
is the same idea for a run that predates the manifest.
