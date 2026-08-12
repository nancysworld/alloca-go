# AG-Sept PR4 — Independently provisioned shard-group capacity

**Type:** Implementation record, spanning PR4a/PR4b
**Status:** PR4a not started. This record opens with the decisions taken before implementation so
they are reviewable against the design that motivates them rather than discovered inside a diff.
**Budget:** 4.0 development days ([AG-Sept plan](../../planning/ag-sept-plan.md) §2, a scheduling
fact) — 1.5 for PR4a's capacity environment, 2.5 for PR4b's evidence and report.
**Owner docs:** [`deployment-architecture.md`](../../design/deployment-architecture.md) §13 owns
the capacity environment; [`horizontal-scaling.md`](../../design/horizontal-scaling.md) §12–§13
owns the capacity-unit model; `REQ-SCALE-4` and `REQ-EVID-2`
([`system-requirements.md`](../../requirements/system-requirements.md)) own what must be true;
[`workload-catalog.md`](../../test/workload-catalog.md) owns `WL-MUT-DISP-4`;
[`ag-sept-validation-plan.md`](../../test/validation-plan/ag-sept-validation-plan.md) §4.6 owns the
matrix, the capacity-point selection rule, `VAL-SCALE-5` and `VAL-NEG-7`; and
[`measurement-contract.md`](../../design/measurement-contract.md) §11–§13 owns provenance and the
quotability ladder. This record covers only how PR4 discharges them and the choices made along the
way. Where it disagrees with an owning document, the owning document wins.

## 1. Exit gates

Both gates are owned by [`ag-sept-plan.md`](../../planning/ag-sept-plan.md) §3 and are not restated
here. In short: PR4a must be able to instantiate the 1/2/4 topology family with equivalent
capacity-unit hosts and a preflighted separate generator, reach `publishable` **provenance**
readiness, and retain the resource evidence `VAL-NEG-7` requires; PR4b must obtain `G1/G2/G4` by
the common saturation rule and derive `E2/E4` without promoting any number past its evidence level.

`publishable` here is the §13 provenance rung only. It is not a claim, and PR4b still has to pass
`measurement-contract.md` §5's evidence gates before anything leaves the project.

## 2. Decisions taken before implementation

### 2.1 `VAL-NEG-7`'s host sensor is `node_exporter` on the existing PR2 substrate

Extend what PR2 built rather than opening a parallel AWS measurement path: `node_exporter` on every
capacity-unit host and on the generator/monitoring host, the service `/metrics` scrape kept as it
is, and the host series retained through the same per-cell CSV + TSDB snapshot export.

The decisive reason is not cost but the gate that already exists. `test/scripts/sweep.sh` refuses a
cell whose panel CSVs or TSDB snapshot are missing, and separately refuses one whose required
panels retained **no samples** — so host evidence added to `deploy/observability/panels.json`
inherits a refusal path instead of needing a new one. A run that silently lost its host series
cannot pass, which is exactly the property `VAL-NEG-7` needs and the property a hand-rolled
side-channel would not have.

It also answers a gap this project already measured rather than a hypothetical one: PR2's
unexplained ~2× excursions could not be diagnosed from service and database-process metrics,
because nothing in the deployment could see the host.

**Rejected — CloudWatch as the primary instrument.** Basic metrics are 5-minute granularity, which
cannot describe a saturation rung at all. Detailed monitoring costs money per instance and still
lands coarser than the retained series. CloudWatch remains useful operationally; it is not
`VAL-NEG-7` evidence.

**Deferred — `postgres_exporter`.** Service, pool and database-facing evidence already exist, and
the proven gap is the host. Its trigger is the first capacity ladders showing that PostgreSQL needs
finer attribution than the pool and service metrics provide; adding it before that is
instrumentation bought against a guess.

It is deliberately **not** filed in [`tech-debts.md`](../../planning/tech-debts.md). That register's
§1 admits gaps in behaviour that is correct today under a stated condition, and sends work not yet
started to the milestone plan instead — which already owns this one
([`ag-sept-plan.md`](../../planning/ag-sept-plan.md) §6.4 leaves the instrument to PR4a and names
`VAL-NEG-7` as the durable obligation). Filing it as debt would put a scheduling choice in a
register that exists for shipped trade-offs.

### 2.2 Monitoring starts on the generator host, and moves only on evidence

Prometheus and Grafana run on the separate generator/measurement host for PR4a. Two rules make that
honest rather than convenient:

- the generator preflight required by `deployment-architecture.md` §13.2 runs **with the monitoring
  stack active**, so the headroom demonstrated is the headroom the measured runs actually have;
- **no Grafana query runs during a measured rung.** Grafana is for diagnosis between rungs and for
  comparison after them. A query engine under variable load on the generator host is the one part
  of this arrangement that could move while a rung is being measured.

If a preflight or a quoted point leaves generator headroom ambiguous, monitoring splits onto its own
instance and the affected points are requalified. That is a decision on evidence, not a preference:
the alternative — a sixth instance bought up front — spends against an ambiguity that has not been
demonstrated.

### 2.3 Scrape cadence is per job, and the host job is not the service job

`deploy/observability/prometheus.yml` scrapes at **1s globally**, deliberately: a sweep cell runs
for tens of seconds and a 15s interval would alias the pool-pressure signal the frontier is read
from.
That reasoning is about the service, and it does not transfer to `node_exporter`, whose default
collector set is orders of magnitude more series per target across six targets.

So: keep `alloca-go` at 1s; give the host job its own `scrape_interval` of ~5s and a restricted
collector set (cpu, meminfo, diskstats, netdev, filesystem, pressure). Prometheus supports a
per-job interval, so this needs no second Prometheus.

### 2.4 The rate window becomes per panel, and the minimum rung is derived rather than asserted

`panels.json` carries **one** `export_range` (15s) for every panel, and `export-panels.sh` starts
its range queries one full rate window after the measured phase opens — deliberately, so no exported
point reads warm-up samples from before the window. The consequence is that a rung's usable retained
series is shorter than the rung by one range: at the committed 15s range and 5s step, a 60s rung
retains ten points, not twelve.

Two changes follow. Panels get a **per-panel or per-class range** with the current value as the
default, because a host series and a request-rate series do not want the same window. And the
**minimum useful rung duration is derived during PR4a preflight** from the retained-series
resolution actually achieved — the largest range in use, the step, and the number of points a
bounded interpretation needs — rather than fixed at a round number now. A rung length asserted
before the exporter has been run against the real topology is a guess wearing a number.

### 2.5 The populated-series gate extends to the host panels

Today `sweep.sh` requires non-empty series for exactly three panels — `throughput`, `pool_max`,
`process_cpu` — chosen because they carry data in any cell whose service was scraped. That set is
now insufficient in a specific way: with four capacity units, a run in which **one** host's
`node_exporter` was unreachable would satisfy every existing check while losing precisely the
evidence `VAL-NEG-7` exists to retain.

The gate therefore becomes per expected capacity-unit host: the required host panels must be
non-empty **for every host the topology declares**, not merely non-empty in aggregate. The expected
host set comes from the same operator-supplied topology input as the manifest (§2.7), so the gate
and the provenance cannot disagree about how many units the run had.

### 2.6 Panel expressions are scoped to their job and aggregated per unit

Four committed panels — `process_cpu`, `process_memory`, `go_goroutines`, `go_gc_pause` — select
metric families that **every** Prometheus-instrumented Go process exports, including
`node_exporter` and Prometheus itself. They are unambiguous today only because Prometheus scrapes
exactly one job. Adding a second job silently changes what they mean, which is a defect that would
not announce itself: the CSV would still export, still be non-empty, and still pass every check.

They are therefore scoped to `job="alloca-go"` as part of adding the host job, not afterwards. The
same pass gives the service panels per-unit aggregation, since `pool_in_use`, `pool_max` and the
process panels return one series per target once four units are scraped, and a report needs both the
per-authority series and the aggregate.

`TestDashboardMatchesCanonicalPanels` keeps the dashboard and the exporter reading the same source,
so the change lands in `panels.json` and the dashboard is regenerated.

### 2.7 Operator-supplied manifest fields arrive as a JSON document, not flags

`Manifest` declares `replica_count`, `deployment_topology`, `environment` and `aggregate_pool_size`,
and `Manifest.Validate` gates `capacity` on them — but nothing populates them, which is why no run
in this repository has ever reached `capacity`. PR4a supplies them.

They arrive as an operator-supplied JSON document alongside the existing `-deployment` record rather
than as four CLI flags. Flags are typed per run; `aggregate_pool_size` must equal
`replica_count × pool_size_per_replica` and the manifest checks it, so a mistyped value fails the
run **after** it has been driven — the expensive moment on metered infrastructure. A version-
controlled document per topology is written once per capacity point, reviewed once, and reused by
every rung of that point's ladder.

### 2.8 Clock synchronisation is a preflight check and retained environment evidence

The measured window comes from the generator's clock and the host series from each host's own, so
alignment decides which samples belong to a rung. Every host runs chrony against the AWS time
source, and PR4a's preflight asserts **healthy synchronisation** rather than merely that the daemon
is installed. The observed offset is retained with the environment evidence, so a later reader can
tell whether a window edge is trustworthy.

### 2.9 `G1` uses a sharded one-authority placement, never `domain.Unsharded`

`Unsharded` is a distinct state: it makes placement enforcement inert and reports
`routing_version: "unsharded"`. A `G1` built that way would run a different code path from `G2` and
`G4` and carry provenance that cannot be compared with theirs. `G1`'s placement document names one
authority owning `org-a` through `org-d`, so routing stays active and only the topology changes.

`Manifest.Validate` requires `placement_assignment` only when a run addressed more than one unit, so
`G1` is not asked for it. It records it anyway: `G1` would otherwise be the only capacity point
whose artifact cannot say what it served.

### 2.10 Reconciliation needs the authorities reachable from the verifier

`alloca-verify` connects directly to each authority's PostgreSQL. That is localhost today and four
remote endpoints on EC2, so security-group rules, DSN handling and the verifier's own host placement
are PR4a's work rather than PR4b's discovery. Reconciliation is required at every quoted point, and
a capacity point that cannot be reconciled is not a capacity point.

## 3. Open items

- **Rung duration** stays open until §2.4's preflight derives it.
- **Whether monitoring splits onto its own host** stays open until §2.2's preflight says whether it
  needs to.
- **`postgres_exporter`** is deferred with a trigger, not dropped (§2.1).
