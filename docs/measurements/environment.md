# Measurement environment

The workstation every `[MEASURED]` figure in this directory was produced on, unless a report
says otherwise. Recorded because a throughput number without its machine is not reproducible,
and because "cores" is ambiguous here in a way that changes what a utilisation figure means.

**Captured 2026-08-04.** Values below are read from the machine, with the command that produced
each block, rather than transcribed from a spec sheet.

## Two distinctions that matter

**1. The denominator is the allocation, not the host.** `process_cpu_seconds_total` is read
inside WSL2, where `runtime.NumCPU()` is **10** because `.wslconfig` says `processors=10`. So
"1.2 cores" is 1.2 within a **10-vCPU allocation**, not 1.2 of the host's 20. A reader who
assumes the host draws a utilisation figure wrong by 2×, in the direction that flatters the
result.

**2. Those 10 vCPUs are shared by the whole guest, not reserved for the service.** The metric
covers the `alloca-go` process and nothing else. PostgreSQL, the load generator, Prometheus,
Grafana and Docker/WSL overhead are all outside it — and all of them run on the *same*
allocation: WSL2 uses one utility VM for every distro, so the `docker-desktop` distro shares it,
and a container on this machine reports `nproc` = **10**, the same ten.

> So a service-process CPU figure bounds **the application's demand**. It does **not** say how
> much of the allocation, the guest, or the host was idle. PR2 measures no total utilisation
> for any of the three.

This is not pedantry about wording. It is why §4's excursions — which slow the service, the
database *and* the generator simultaneously — are consistent with contention for one shared
10-vCPU pool, and why nothing in this repository can currently confirm or refute that. A node
exporter is the instrument that would.

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

wsl.exe -l -v        : Ubuntu (running), docker-desktop (running)
docker run alpine nproc : 10   <- the same ten, not a separate allocation
```
<sub>`nproc`, `/proc/cpuinfo`, `free -h`, `uname -r`, `go version`, `wsl.exe -l -v`</sub>

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

Published capacity needs the separate generator compute of `ag-sept-plan-new.md` §6.3. AG-Sept
does not fund it: the deployment path that would have supplied it is withdrawn
(`ag-sept-plan-new.md` §10), so every AG-Sept run stays `local` and the milestone closes with a
bounded local frontier rather than a published capacity number.

## Per-run records

This file is the shared description; it is not the authority for any individual run. Each run
carries its own environment in `run.json`'s manifest — service and generator commit SHAs, Go
versions, `GOMAXPROCS`, pool size, PostgreSQL version, timeout budget, reservation TTL — and
the manifest is what a report must be checked against. `pr1-telemetry-overhead/environment.txt`
is the same idea for a run that predates the manifest.
