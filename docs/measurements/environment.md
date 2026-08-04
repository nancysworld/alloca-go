# Measurement environment

The workstation every `[MEASURED]` figure in this directory was produced on, unless a report
says otherwise. Recorded because a throughput number without its machine is not reproducible,
and because "cores" is ambiguous here in a way that changes what a utilisation figure means.

**Captured 2026-08-04.** Values below are read from the machine, with the command that produced
each block, rather than transcribed from a spec sheet.

## The distinction that matters

> **The service is not measured against the host's CPU. It is measured against the 10 vCPUs
> WSL2 is allocated**, which is itself half the host's core count.

`process_cpu_seconds_total` is read inside WSL2, where `runtime.NumCPU()` is **10** because
`.wslconfig` says `processors=10`. So "1.2 cores" is 1.2 of that **10-vCPU allocation** — not
1.2 of the host's 20. A reader who assumes otherwise will draw a utilisation figure that is
wrong by 2×, in the direction that flatters the result.

Nothing in the reports depends on the host figure: the conclusion is that the service is far
from compute-bound, and it is further from it on the host than within the allocation. But the
denominator has to be stated, and it is the allocation.

## Host

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

```ini
[wsl2]
memory=12GB
processors=10
swap=2GB
localhostForwarding=true
```
<sub>`%USERPROFILE%\.wslconfig` on the Windows host.</sub>

```
nproc                : 10
model name           : Intel(R) Core(TM) i7-14700F
MemTotal             : 11 GiB visible
kernel               : 5.15.167.4-microsoft-standard-WSL2
go version           : go1.26.5 linux/amd64
```
<sub>`nproc`, `/proc/cpuinfo`, `free -h`, `uname -r`, `go version`</sub>

**`GOMAXPROCS` is therefore 10 by default** for both the service and the load generator, and
each run records its own observed value in `run.json`'s manifest (`server_gomaxprocs`,
`generator_gomaxprocs`, `generator_num_cpu`) — so a run made under a different allocation is
self-identifying rather than silently mixed in.

## PostgreSQL

Docker version 29.6.2, image `postgres:16-alpine`, server version **16.14**, started by
`make db-up` with no tuning applied. The settings that shaped the results:

| Setting | Value | Where it shows up |
|---|---|---|
| `max_connections` | 100 | caps the pool ladder — pool 80 plus the seeder and verifier fits; 160 would not |
| `shared_buffers` | 128 MB | the stock default, on a 32 GB host |
| `checkpoint_timeout` | 5 min | investigated as a cause of the §4 excursion, and ruled out |
| `max_wal_size` | 1 GB | requested checkpoints were observed under sweep write volume |

**Storage is Docker Desktop's default VHDX on the Windows host**, not a tuned volume. This is
the term the reports cannot see and repeatedly ran into: the write-ahead-log ceiling of §5 and
the unexplained excursions of §4 both live on this path, and neither a service exporter nor a
PostgreSQL exporter can observe it.

## What this environment is not

It is a **developer workstation**, and no figure measured here is a capacity claim. The load
generator shares the host with the service, so every run is `quotability.level: local` by
construction and refuses the `capacity` level by name. An untuned container on a laptop-class
storage path says nothing about a tuned database on provisioned storage.

Published capacity needs the separate generator compute of `ag-sept-plan.md` §10, which arrives
with PR4.

## Per-run records

This file is the shared description; it is not the authority for any individual run. Each run
carries its own environment in `run.json`'s manifest — service and generator commit SHAs, Go
versions, `GOMAXPROCS`, pool size, PostgreSQL version, timeout budget, reservation TTL — and
the manifest is what a report must be checked against. `pr1-telemetry-overhead/environment.txt`
is the same idea for a run that predates the manifest.
