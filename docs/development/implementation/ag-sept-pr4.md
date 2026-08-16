# AG-Sept PR4 — Independently provisioned shard-group capacity

**Type:** Implementation record, spanning PR4a/PR4b
**Status:** PR4a in progress. The local multi-group topology and workload machinery are being
exercised and the measurement procedure qualified before any metered AWS capacity attempt; AWS
capacity evidence remains conditional on sufficient quota within the milestone timebox.
**Budget:** 4.0 development days ([AG-Sept plan](../../planning/ag-sept-plan.md) §2, a scheduling
fact), now a **shared PR4a/PR4b envelope**. The original 1.5-day PR4a / up-to-2.5-day PR4b split was
retired by maintainer decision on 2026-08-14; PR4a may consume more of the envelope for local
measurement qualification and PR4b correspondingly becomes a smaller bounded AWS execution pass.
**Owner docs:** [`deployment-architecture.md`](../../design/deployment-architecture.md) §13 owns
the capacity environment; [`horizontal-scaling.md`](../../design/horizontal-scaling.md) §12–§13
owns the capacity-unit model; `REQ-SCALE-4` and `REQ-EVID-2`
([`system-requirements.md`](../../requirements/system-requirements.md)) own what must be true;
[`workload-catalog.md`](../../test/workload-catalog.md) owns `WL-MUT-DISP-4`;
[`ag-sept-validation-plan.md`](../../test/validation-plan/ag-sept-validation-plan.md) §4.6 owns the
matrix, the Tier 1/Tier 2 result model, `VAL-SCALE-5` and `VAL-NEG-7`; and
[`measurement-contract.md`](../../design/measurement-contract.md) §11–§13 owns provenance and the
quotability ladder. This record covers only how PR4 discharges them and the choices made along the
way. Where it disagrees with an owning document, the owning document wins.

## 1. Exit gates

The gates are owned by [`ag-sept-plan.md`](../../planning/ag-sept-plan.md) §3 and are not restated
here. In short: PR4a first proves the 1/2/4 experiment machinery in the bounded local partitioned
rehearsal, then qualifies the measurement procedure far enough that a known local regime cannot
silently contaminate the AWS comparison. Mechanical findings are fixed or explicitly bounded; the
degraded/non-stationary regime is either fixed/explained or made reliably detectable/excludable.
An AWS bootstrap may follow when it buys useful confidence while quota is pending. PR4b runs the
independently provisioned AWS capacity experiment only when sufficient quota exists to instantiate
the complete environment.

When that complete environment exists, PR4b attempts the validation plan's Tier 1 capacity result
first; if a proven measurement-system limit prevents that result, it may retain the Tier 2
operating-point comparison without promoting it into capacity evidence, while `VAL-SCALE-5` remains
explicitly unproven. If quota prevents the complete G4 environment from existing within the
milestone timebox, neither local rehearsal numbers nor an incomplete AWS topology substitute for
the missing result: the quota blocker and `VAL-SCALE-5`-unproven outcome flow to PR5.

`publishable` remains the §13 provenance rung only. It is not a claim, and every quoted AWS result
still has to pass `measurement-contract.md` §5's evidence gates before anything leaves the project.

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

### 2.7 The manifest fields arrive as a JSON document, and two of the four stop being declared

`Manifest` declares `replica_count`, `deployment_topology`, `environment` and `aggregate_pool_size`,
and `Manifest.Validate` gates `capacity` on them — but nothing populates them, which is why no run
in this repository has ever reached `capacity`. PR4a supplies them.

They arrive as an operator-declared JSON document rather than as four CLI flags: the fields describe
one coherent run shape and should be parsed and validated atomically, and a version-controlled
document per capacity point is written once and reused by every rung of that point's ladder instead
of being retyped per run.

**It is a separate file from `-deployment`, deliberately.** That record is *observed* — written by
`record-deployment.sh` from the running containers, because a process cannot see which image wraps
it (ADR-0003). This one is *declared*. Merging them would put two provenance classes in one
artifact, and the weaker one would inherit the stronger one's credibility.

**Two of the four fields should be derived rather than declared, and one of them buys a new check.**

`aggregate_pool_size` is currently gated by the arithmetic `replica_count × pool_size_per_replica`,
where `pool_size_per_replica` is projected from **unit[0]'s** `/meta`. Every unit's `/meta` is
already fetched into `TopologyMeta.Units`, so the aggregate is instead **summed over the units that
served the run**: one fewer thing for an operator to state, and derived from the per-unit data
rather than from an invariant restated as arithmetic.

The invariant itself is already enforced — `runShapeMismatch` compares the pool ceiling across
units and a mismatch blocks certification, so a heterogeneous deployment cannot quote a number
today. What summing changes is only where the value comes from. **What does need to move is when
that check fires:** it is evaluated at certification, after the run, which is the same
fail-late problem the declared JSON is being preflighted to avoid. On metered infrastructure a
like-for-like violation must cost nothing, so the units-describe-one-deployment check runs in the
pre-load preflight as well (§2.7.1).

`replica_count` equals `UnitCount` — `len(targets)` — **for this topology**, because PR4 runs one
service replica per capacity unit and the generator addresses each unit directly. It is not
derivable in general: a load balancer in front of a unit makes targets and replicas differ, and no
HTTP client can see that. So it is derived from the observed unit set by default and may be
overridden only by an explicit fan-out declaration in the JSON. PR4 declares neither; a future
balanced topology has to state the fact that would otherwise be silently wrong.

What remains operator-declared is `environment` and `deployment_topology` — two strings with no
observable source.

### 2.7.1 The document is checked before any measured request, in two places

A malformed or inconsistent capacity manifest must cost zero experiment time rather than fail
certification after the ladder has been driven. The checks split by what they need:

- **parse, required fields, internal consistency** join `preflightDeployment` in the local,
  no-network preflight that already runs before anything is fetched;
- **cross-checks against observed reality** — declared replica count against the unit set, and the
  units-describe-one-deployment check that today only runs at certification — need `/meta`, so they
  run immediately after `FetchTopologyMeta` and still before the runner is handed the workload.
  Nothing occupies that slot today.

**An existing behaviour has to change with it.** A failure to read `/meta` from every unit
currently prints a warning to stderr and the run proceeds, failing later at certification. That is
the right trade on a workstation where the cost is a minute; it is the wrong one on metered
infrastructure where the cost is a ladder rung. A multi-unit run whose units cannot all be read
refuses before load.

That refusal must **not** be keyed on `-require`. Keying a gate on the flag that sets the exit-code
floor is exactly how the deployment-preflight bypass was reopened in PR3b and had to be closed again
in `ce7cd66`: `-require none` then disables the check rather than the certification level, and the
run it was protecting proceeds unprotected.

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

### 2.11 PR4 implements the validation plan's two-tier result model

The two-tier decision first recorded here now belongs normatively to
[`ag-sept-validation-plan.md`](../../test/validation-plan/ag-sept-validation-plan.md) §4.6. PR4b
attempts Tier 1 first **after the complete independently provisioned G4 measurement environment
exists**. Only when that complete environment is itself proven unable to drive the topology family
far enough for the saturation-selected capacity result may PR4b retain the Tier 2 operating-point
comparison defined there.

A quota or provisioning limit that prevents the complete G4 environment from existing is therefore
not a Tier-2 trigger. It produces no comparable common-`L` topology family; PR4 records the external
limitation and leaves aggregate capacity scaling and `VAL-SCALE-5` unproven.

This implementation record deliberately carries no duplicate formulas, threshold, or alternate
selection rule. PR4a's responsibility is to rehearse and qualify the measurement machinery and
retain enough generator/resource evidence to distinguish a server frontier from a measurement-
system or environment regime before AWS can run. PR4b's responsibility is to apply the owning
validation rule on independently provisioned compute and keep its conclusion bounded to the
strongest evidence actually established.

### 2.12 Capacity units are non-burstable, and the generator is larger than a unit

The vCPU budget is the maintainer's; the shape within it is implementation
(`deployment-architecture.md` §13.3). Against a 12-vCPU quota: four `c5.large` capacity units
(2 vCPU each) and one `c5.xlarge` generator/monitoring host (4 vCPU).

**No burstable instance may serve a measured run.** A `t3.medium` is the obvious 2-vCPU choice and
is credit-based: its sustained baseline is 20% of two vCPUs once credits drain. A saturation ladder
would burst early and throttle later, so the same rung measures different capacity depending on how
long that host had been up — and `G1`, `G2` and `G4` run different numbers of hosts with different
credit histories, so the throttling lands unevenly across exactly the comparison `E2` and `E4` are
derived from. It would read as sub-linear scaling. `c5`, `c6i` and `m5` are not credit-based.

The generator is deliberately **not** the same shape as a capacity unit. §13.2 exempts it from
like-for-like precisely so it can be sized for its job, and it is the one host whose saturation
would invalidate a point rather than describe one.

**What 2-vCPU units mean for the result, recorded before the runs rather than after.** A service and
a PostgreSQL authority sharing two vCPUs make **host CPU** the likely limiting mechanism, not
PostgreSQL's own frontier. The Problem is still answered if those units can be measured — the units
are independently provisioned, `E2`/`E4` are still derived, and `VAL-SCALE-5` asks for the limit to
be identified rather than for it to be any particular subsystem — but the result characterises
composition of small capacity units, and its comparability with PR2's 10-vCPU workstation frontier
is weak. Both belong in PR4b's limitations, and neither is a discovery.

### 2.13 A `t2.micro` bootstrap is optional while quota is pending

A 1-vCPU quota cannot measure capacity and can still run one instance, which is enough to prove the
AWS-specific bootstrap path: AMI, user data, Docker, image pull, the placement document,
`node_exporter`, chrony, security groups, and a single-unit smoke run. Burst throttling is irrelevant
where nothing is being measured.

This is now **conditional rather than mandatory**. The local rehearsal comes first (§2.14). If the
quota request is still pending afterwards and a one-instance AWS proof would remove useful
uncertainty, use one `t2.micro`; if sufficient non-burstable quota has already arrived, skip it and
move directly to the `c5` environment. A `t2.micro` result is never capacity evidence.

### 2.14 PR4a rehearses the 12-vCPU shape locally before any AWS capacity attempt

**Maintainer decision, 2026-08-13:** use the workstation's larger resource envelope to prove the
Iteration C machinery before spending AWS time. The rehearsal partitions **12 CPUs** into
non-overlapping scheduler-visible CPU sets:

```text
capacity unit A    CPUs 0-1     service A + PostgreSQL A
capacity unit B    CPUs 2-3     service B + PostgreSQL B
capacity unit C    CPUs 4-5     service C + PostgreSQL C
capacity unit D    CPUs 6-7     service D + PostgreSQL D
generator/monitor  CPUs 8-11    load generator + monitoring
(headroom)         CPUs 12-15   held idle; the generator control's only spare capacity
```

**The machine is 16 logical CPUs and the partition is 12.** WSL was reconfigured to 16 on
2026-08-13; the rehearsal's shape was deliberately *not* widened with it. Four CPUs stay outside
the partition for two reasons. Unpinned host work — the Docker daemon, WSL kernel threads,
anything the operator runs — has somewhere to go that is not a capacity unit's set or the
generator's. And they are the headroom the **generator-headroom control** widens into: the same
cell rerun with `ITC_CPUS_GENERATOR=8-15`, which is the cpuset analogue of `VAL-NEG-2`'s
`GOMAXPROCS` control. If Goodput does not move when the generator gets twice the CPUs, the
generator was not the binding constraint at that operating point; if it tracks the generator's
size, the cell was measuring the harness. A closed-loop harness cannot answer that from its own
numbers, and on a partitioned machine placement is the variable a single-host sweep does not
have.

**The envelope change is not free, and it lands on comparability rather than on this rehearsal.**
The workstation every earlier local measurement was taken in no longer exists: PR2's frontier was
measured against a 10-vCPU envelope (§2.12), and this machine is now 16. Rehearsal numbers were
never comparable with PR2's — they are diagnostic and confined to two-CPU units — so nothing
already recorded changes. What does change is that **any future local re-measurement is a
different environment from PR2's**, and a re-run of a PR2 cell on this workstation would not
refute or confirm the PR2 result. Treat the two as separate environments, not as a before and
after.

`G1` uses unit A only, `G2` uses A+B, and `G4` uses A+B+C+D. Unused capacity-unit CPU sets stay idle;
the active groups do not borrow them. Docker **cpusets**, not only CPU-time quotas, are the relevant
mechanism because the rehearsal is meant to stop shard groups from sharing scheduler-visible CPUs.
Service and PostgreSQL within one group intentionally share the same two-CPU set, matching the
planned EC2 capacity-unit contention shape.

This gives PR4a a cheap, repeatable place to exercise `G1/G2/G4`, placement, fixtures, workload
semantics, declaration/provenance, reconciliation, observability, generator isolation and the
operator-facing Make targets. It also gives us an early diagnostic signal about whether the planned
AWS experiment is likely to behave sensibly.

It does **not** create independent hosts. All groups still share the workstation, WSL VM/kernel,
storage path, caches and physical machine. Therefore local Goodput or apparent scale efficiency is
rehearsal/diagnostic evidence only: it cannot discharge `VAL-SCALE-5`, cannot become formal Tier 2,
and is never mixed with AWS points to derive `E2` or `E4`.

**Pinning the units is only half of the partition, and the measuring side is more than the
generator.** `ITC_CPUS_GENERATOR` names the set for everything that measures: `alloca-load`,
Prometheus and Grafana. Three separate mechanisms put them there, and each was a way to lose the
partition silently:

- **The generator is a host process**, so the scheduler places it on the units' CPUs unless told
  not to. `test/scripts/itc-run.sh` confines it with `taskset`, which also fixes its
  `GOMAXPROCS` because Go reads the affinity mask at startup.
- **Monitoring is a separate Compose stack.** `make obs-up` raises Prometheus and Grafana with no
  cpuset at all — correct for ordinary local work, wrong during a rehearsal. Prometheus is not
  idle: it scrapes every unit on a short interval and compacts its TSDB, and unpinned it does
  that from inside the capacity units' own CPUs. That load grows with the number of units, so it
  biases `G4` harder than `G1` — against exactly the comparison `E2` and `E4` are derived from.
  `make obs-rehearse` applies `deploy/observability/docker-compose.rehearsal.yml`.
- **The control has to move the whole measuring side.** Widening `ITC_CPUS_GENERATOR` to `8-15`
  for the generator-headroom control widens monitoring with it. Moving only the generator would
  change two things at once and the comparison would carry the second one.

**A partition that is legal is not a partition that was applied**, and nothing downstream can
tell the difference: an unpinned run addresses the right units, passes every routing check, and
certifies cleanly while carrying contention no artifact records.
`test/scripts/itc-cpuset-check.sh` therefore reads the cpuset off each running container and
refuses a run whose pinning disagrees with the partition; `itc-run.sh` calls it in preflight and
retains its output as `observed-cpusets.txt` beside the run. Monitoring is checked only when it
is running, because a Prometheus that was never raised cannot contend with anything — but a
container whose cpuset cannot be *read* is reported as unverified rather than as absent, since
treating an unreadable answer as a skip would make the check the thing it exists to catch.

**The declaration deliberately does not name the generator's CPUs.** `environment` is one string
reused across `G1`, `G2` and `G4` and across both sides of the headroom control, so a static
`8-11` in it would be false for half the runs it describes. The effective set is a per-run fact
and is retained per run (`cpu-partition.txt`, `observed-cpusets.txt`).

The execution sequence is now:

```text
local 12-vCPU partitioned rehearsal
    -> qualify the measurement procedure; fix, explain or bound material local regimes
    -> optional t2.micro AWS bootstrap if quota is still pending and the proof is useful
    -> bounded c5 AWS capacity experiment if sufficient quota is available
    -> otherwise retain the quota limitation and explicit VAL-SCALE-5-unproven result
    -> PR5 closeout
```

The last branch is deliberate: successful AWS capacity measurement is a stronger desired result,
not an Iteration C exit gate. An external quota decision must not force AG-Sept either to wait
indefinitely or to promote shared-workstation evidence beyond what it proves.

## 3. Discovered during implementation

These are findings, not decisions taken in advance. Each changed something that had already
been designed, and each was invisible to review — every one was found by running something.

### 3.1 The workload's demand ordering has to be the workload's own property

`WL-MUT-DISP-4` assigns demand round-robin over the four organisations, so the order of that
list decides which organisation each request addresses. Deriving the list the obvious way —
walking the placement's authorities and flattening the organisations each owns — makes the
order a function of the topology whenever an authority does not own an alphabetically
contiguous run of them. `G1` would then be compared against a `G2` that had quietly permuted
the demand mapping, and the difference would present as scale efficiency.

The shipped PR3b map is exactly that shape (`org-a`/`org-c` on one authority, `org-b`/`org-d`
on the other), so this was not a hypothetical arrangement. The ordering is now sorted by
organisation identifier and the topology-invariance test uses a deliberately non-alphabetical
`G2`; an alphabetical one passes whether or not the property holds.

**Consequence for PR4b:** the request-pair invariance is a property of the generator that is
now tested, so a Tier 1 or Tier 2 comparison does not additionally need to argue it.

### 3.2 A discriminating test can be defeated by the arithmetic of its own fixture

The assertion that each organisation walks its whole seeded dataset passed against a
deliberately broken workload. Indexing slots by the global sequence number rather than the
organisation's own counter strides four at a time through the population, and whether that
loses coverage depends on `gcd(4, slots)`: at the five-slot fixture the assertion was written
with, the stride still visited all five. At eight it reaches a quarter of them.

The gate was real and the fixture made it inert. It is recorded because the failure mode
generalises past this test: a coverage assertion over a cyclic index is only as strong as the
relationship between the cycle lengths, and nothing about reading the test reveals which case
it is in. The four mutations run against this workload are the reason it was found at all.

### 3.3 `-reset` truncates slots, so the clean start is per authority and not per organisation

`Repo.Truncate` includes `slots`, not only booking and idempotency state. Seeding four
organisations each with `-reset` therefore leaves only the last one's fixture standing.

The consequence is asymmetric across the matrix, which is what makes it dangerous rather than
merely wrong: at `G4` every organisation has its own authority and nothing is lost, while at
`G1` all four share one database and three of the four datasets disappear. A `G1` measured that
way would be depressed relative to `G4` by an artifact of the fixture, in the same direction
and of the same rough shape as a genuine super-linear scaling result.

### 3.4 `GROUPS` cannot be a shell variable name

`GROUPS` is a bash built-in array holding the caller's group IDs, and bash discards an
assignment to it without error: the value arrives in the script as the caller's GID. Make
expands `$(GROUPS)` itself and is unaffected, so the Makefile would have worked while the
script it documents silently did not. The variable is `ITC_GROUPS` throughout.

Recorded because it is a naming hazard rather than a bug that stays fixed: the next
shell-facing knob that wants an obvious short name can hit the same class of collision, and
`shellcheck` is what caught it.

### 3.5 A fully replayed run certifies at `capacity` with zero goodput — needs a decision

Two runs of `wl-mut-disp-4` against the live four-group topology, identical in every respect
except whether the fixture had been re-seeded between them:

| fixture | goodput | replays | `measurement_sound` | quotability level |
|---|---:|---:|---|---|
| clean | 400 | 0 | `true` | `capacity` |
| carried over from the previous run | **0** | **400** | `true` | `capacity` |

The second run committed nothing. Every mutation was served from an idempotency record written
by the run before it, because the workloads derive keys from workload name and sequence number
with no per-run nonce. `alloca-seed` documents this trap and asserts against it **at seed time**;
nothing asserts against it at **run** time, so the contaminated run reconciles cleanly, breaks no
invariant, reports `measurement_sound: true`, and reaches the same rung as the honest one.

Zero goodput is obvious enough to catch by eye. The shape that matters for PR4b is the partial
case: a fixture reset on some capacity points and not others yields a *plausible* depressed
number at the contaminated rung, and `E2`/`E4` would carry it as scale efficiency. `G1` is the
most exposed point, since it is the one most likely to be re-run while debugging.

**Resolved by maintainer decision: fresh-run identity is structural, not procedural.** Idempotency
keys are scoped to a run id minted per invocation, and capacity certification refuses a run
reporting replays its workload did not intend. Reset/reseed remains required fixture preparation
but is no longer the only thing standing between a rerun and a wrong number. The disposition
control is exempt by its own declaration, because replays are what it measures.

**The fix works, and not by the route the table above suggests.** Re-running the same experiment
without re-seeding now produces *no replays at all* — the second run mints entirely different keys,
so nothing can be served from the first run's records. What it produces instead is 200
`business_refusal` / `schedule_conflict`: the requests were genuinely attempted and genuinely
refused, because the fixture's capacity was already consumed.

That is the substantive improvement, and it is one of legibility rather than of arithmetic. Both
the old and new contaminated runs report zero goodput; the difference is what the totals say
happened. Before, they read `admitted_success, replay: true` — a run that looks like 200
successful bookings until someone notices goodput excludes replays. After, they read
`schedule_conflict`, which is what actually happened and is visible at a glance.

The certification gate is therefore a backstop rather than the primary defence: in the motivating
scenario it never fires, because there are no replays left to refuse. It covers the narrower case
where run identity fails to vary — a reused id, or a future change that drops the scoping — which
is exactly the case that would otherwise reintroduce the original defect silently.

**Resolved by maintainer decision, and generalised past the case that prompted it.** The run of 200
refusals against an exhausted fixture certifies at `capacity` with zero goodput, and that stays
correct: the provenance ladder describes how well a run accounts for itself, and this one accounts
for itself honestly with the outcome mix in its totals. What it lacks is useful demand, which is an
evidence question. So `Certify` and `measurement-contract.md` §13's ladder are unchanged, and the
rule lands in the two documents that own evidence:

- `measurement-contract.md` §5 now carries a **useful-demand / fixture-headroom gate**: an
  all-refusal run may be sound and may reach any provenance level, but cannot back a
  mutation-capacity claim;
- [`ag-sept-validation-plan.md`](../../test/validation-plan/ag-sept-validation-plan.md) §4.6 applies
  it to `WL-MUT-DISP-4`'s selected points and confirmation runs.

The generalisation is the part worth carrying forward, because the zero-goodput case is the one
nobody would have quoted anyway. **Partial exhaustion invalidates a capacity point too, and it
invalidates the rung *below* it.** The saturation rule selects a point by showing that a higher
rung produced no more Goodput; if that higher rung was short of fresh mutations rather than short
of service, it demonstrates a spent fixture and establishes nothing about the frontier — so the
point beneath it was never established either. A capacity point is only as good as the evidence of
the rung meant to exceed it.

That has a direct consequence for PR4a rather than only PR4b: the per-organisation population must
be sized for the *deepest* rung the ladder will reach, not for the selected point, and `-slots` is
the knob that has to be justified before the sweep rather than after it.

### 3.6 One uncommitted file anywhere in the tree makes every run uncertifiable

`go build` decides `vcs.modified` by running `git status --porcelain`, which lists **untracked**
files as well as modified ones. A single new file that has not been committed — a scratch script, a
config being drafted, a result written into a path `.gitignore` does not cover — therefore stamps
the service and generator binaries `vcs.modified=true`.

`Manifest.Validate` refuses `service_source_modified` at `local`, the floor of the ladder, so the
consequence is not a downgrade: **the run certifies at `none` and backs nothing at all**, however
sound the measurement was. This is the state PR4a's `publishable` provenance gate has to avoid, and
it is reachable by forgetting one `git add`.

Measured, not reasoned: in a synthetic repository a clean tree stamps `false` and adding one
untracked file stamps `true`. Observed here as a G2 run that certified at `none` with both
`service_source_modified` and `generator_source_modified` true, while every *tracked* file was
committed — the cause was an uncommitted overlay file added earlier in the same session.

The existing controls each cover a different property and none covers this one.
`make build-context-check` asserts `.dockerignore` excludes no *tracked* file. The `.dockerignore`
and `Makefile` comments describe the mechanism correctly but are about excluded tracked paths.
What does answer it, in one step, is **`make image-provenance`**: it builds the image, extracts the
binary, and fails when a clean checkout produces `modified=true`.

**Operational consequence for PR4b.** Run `git status --porcelain` and `make image-provenance`
before any metered evidence run, not after. The cheap symptom to watch for is the `-dirty` suffix
on the image tag, which `ALLOCA_IMAGE_TAG` derives from the same `git status --porcelain`; a run
whose image tag carries it will not certify, and finding that out on AWS costs the rung.

### 3.7 The layout check described more properties than it enforced

`itc-cpu-layout.sh` was written to fail a bad CPU partition before an image build and four
database containers. Its comments named the properties it existed to protect. Writing tests for
it found that it checked two of them — that the sets do not overlap, and that the partition fits
the machine — and that four other cases passed:

- **A generator no larger than a capacity unit.** §2.12's rule appeared in the script's own
  failure text — "keep the generator larger than one capacity unit … a generator that saturates
  first measures itself" — and nothing evaluated it. `ITC_CPUS_GENERATOR=8` was accepted.
- **Unequal capacity units.** `E2` and `E4` are ratios across units assumed identical, so a unit
  with an extra CPU raises the aggregate and the efficiency figure carries the imbalance rather
  than the architecture. A 2/3/2/2 partition was accepted.
- **An unrecognised group count.** `ITC_GROUPS=3` checked a two-unit partition and printed "G3",
  so the operator read a pass for a layout nothing had examined; `itc-topology-check.sh` and
  `itc-seed.sh` already refused the same value. A non-numeric value reached `(( ))` and produced
  bash's "unbound variable", naming neither the variable nor the fix.
- **A malformed CPU spec exited zero.** This is the dangerous one. `expand` ran inside
  `mapfile -t cpus < <(expand "$spec")`, and a process substitution's exit status is not the
  enclosing command's, so an unparseable spec printed its complaint, yielded an empty set, and
  let the script report the partition as valid.

The common shape is worth more than the four bugs: **the script's prose was accurate and its code
did not implement it.** Every one of these properties was written down, in the file, next to code
that did not check it — which is the failure mode a comment cannot catch and a reader is least
likely to, because the explanation reads as evidence that the check exists.

All four are now enforced, and every violation is reported rather than only the first, so an
operator rescaling the partition to a different machine gets the whole list.

**How they are gated.** `test/scripts/itc-cpu-layout-test.sh` runs the real script and is wired
into `.github/workflows/ci.yml` and `make ci`, which is the pattern `check-build-context.sh`
already established for a shell check that needs nothing an operator must provide
(`project-structure.md` §1 owns the rule and now records both). A Go test would have been the
easier way to reach `make ci` and is prohibited under `test/` for a good reason — Go tests belong
beside the code they exercise — so the gate follows the existing mechanism rather than an
exception to it.

The machine's apparent CPU count is controlled with `taskset` rather than an environment override
inside the script, because `nproc` reports the CPUs available to the calling process and an
affinity mask therefore presents a genuinely smaller machine. An `ITC_CPUS_AVAILABLE` seam would
have been less work and is the wrong shape: it is a documented way to tell the script the machine
is bigger than it is, and nothing else checks the partition against the machine.

Cases asserting the shipped 12-CPU partition skip on a smaller runner; every negative case is
sized to four CPUs and the generator boundary to two, so the rules stay gated on whatever CI
provides. Each check was then removed in turn and the case that claims to gate it required to
fail — seven controlled mutations, all detected.

### 3.8 The topology and the observability stack could never have run together

`make obs-rehearse` after `make itc-rehearse` failed outright:

```text
Bind for 0.0.0.0:9091 failed: port is already allocated
```

`alloca-service-1` published its metrics on host `9091`, and Prometheus publishes `9091`. Units
2–4 collided the same way with `9092`–`9094`, which Prometheus does not use but which left no
room to move it.

**It had been latent since PR3b, because the two stacks had never been raised together.** PR2
measured a service running on the host, with metrics on `9090` and Prometheus on `9091` — no
overlap. PR3b containerised the topology and gave each unit a published metrics port starting at
`9091`, and PR3c scraped those ports *directly* (`pr3c-experiments.sh` reads `M1`/`M2`) without
ever starting Prometheus. Iteration C is the first configuration that needs both, so it is the
first that could fail.

The clearest symptom that this was a real ambiguity rather than a coincidence: **`9091` already
meant two different things in two different documents.** `container-topology.md` §4 used
`curl http://localhost:9091/metrics` for service-1's metrics while `sweep.sh` and
`export-panels.sh` used `http://localhost:9091` for Prometheus.

**Resolved by moving the topology's metrics ports to `9081`–`9084`**, mirroring the HTTP ports
`8081`–`8084` so unit *n* serves on `808n` and publishes metrics on `908n`. Prometheus keeps
`9091`, which is the address every operator-facing reference already uses and the one a person
types into a browser. Updated with it: `pr3c-experiments.sh`'s `M1`/`M2` defaults, and
`container-topology.md`'s recipe and container table.

This is a **breaking change to a documented address**, and it is reversible: the ports are
`SERVICE_n_METRICS_PORT` overrides, so an existing recipe can be pinned to the old values rather
than edited. Anything holding the old numbers — a saved dashboard, a shell history, a note —
needs updating, which is why the numbers were moved to a pattern rather than to arbitrary free
ports.

Not guarded by a check, deliberately: Docker refuses a duplicate binding loudly and names the
port, so this failure cannot be mistaken for anything else or silently produce a bad result. The
guard would only convert a clear runtime failure into a slightly earlier one.

### 3.9 The first driven cell was not observed, and nothing said so

The first G4 rehearsal cell ran to completion, reconciled, and certified at `capacity`. Grafana
was empty, and Prometheus reported no error at any point.

Prometheus was healthy and scraping a target written on 2026-08-04: `172.21.44.28:9090`, the WSL
`eth0` address of a **host-run** service from PR2's single-instance path. Nothing serves that
address now — the service under test is four containers — and the address had almost certainly
been reassigned by the WSL reconfiguration besides. The scrape config's own comment predicted
exactly this: the discovered address "is reassigned whenever WSL restarts."

**Nothing downstream could report it.** A query against a job whose targets are all down returns
an empty result and no error, so every panel renders empty, every CSV exports with headers and no
rows, and the run's own artifacts are unaffected — `run.json` carries the totals, and
`alloca-verify` reads direct scrapes rather than Prometheus. The cell was sound and quotable at
its provenance level while retaining no time series whatsoever. `sweep.sh` records this shape as
the worst there is, because nothing anywhere reports an error.

**Resolved by making the scrape path structural rather than discovered.** Prometheus joins the
topology's Compose network and scrapes the units by their service names (`service-1:9090`); the
addresses stop being a property of this machine's networking, so a WSL restart cannot invalidate
them. Grafana is deliberately *not* attached — it talks to Prometheus, and putting a dashboard on
the measured topology's network buys nothing.

`file_sd` remains the abstraction rather than static targets in `prometheus.yml`
(**maintainer decision, 2026-08-13**): the scrape configuration stays version-controlled and
deployment-agnostic, and only the generated file changes between environments. Locally
`test/scripts/itc-obs-targets.sh` writes Compose service names; on EC2 the same script writes the
units' private addresses from `UNIT_n_ADDR`, with no other change. Each target carries an
`authority` label, which is the stable per-unit identity — the address is not, so a panel keyed on
`instance` would not survive the move to the environment the experiment exists for (§2.6).

**The gate is a count, not a probe.** `itc-run.sh` now refuses a cell unless exactly `ITC_GROUPS`
targets report `up == 1`. "Prometheus is scraping something" passes while three of four units are
missing, and a G4 point measured with one unit unobserved is not a G4 point — it is the rung
below it wearing the wrong label. A rehearsal may still run unmonitored, but only by saying so
(`PROM_URL=`), never by Prometheus being quietly absent.

The units join the existing `alloca-go` job rather than one of their own, because §2.6 scopes the
service panels to `job="alloca-go"` and reserves a second job for the host exporter. The PR2
host-run target is therefore a fifth member of the same job during a rehearsal, permanently down;
`itc-obs-targets.sh` refuses rather than deleting it, since `obs-target.sh` owns that file.

### 3.10 The first complete G4 rehearsal certified correctly and consumed its entire fixture

The first full G4 cell — clean provenance, all four units pinned, generator confined, declaration
and deployment reconciled — certified at `capacity`, blocked from `publishable` only by the
co-resident generator. It also admitted **exactly 16,000 mutations against a 16,000-unit fixture**
(4 organisations × 200 slots × 20 capacity) and then refused **184,693** requests with
`no_capacity`:

```text
reserve  admitted_success               16,000
reserve  business_refusal/no_capacity  184,693
```

At the 3,345 req/s the cell sustained, the fixture was spent in roughly the first five seconds.
The remaining ~55 seconds measured how fast the service can decline, which is a correct answer
and a sound measurement, and is not throughput the service could have delivered.

**This is the useful-demand gate demonstrated on a workstation instead of on metered
infrastructure**, which is what §2.14 says the rehearsal is for. Everything behaved exactly as
`measurement-contract.md` §5 specifies: the run described itself honestly, its outcome mix is in
its totals, and it certified at whatever its manifest earned — because what it lacks is useful
demand rather than self-description. Nothing was broken. The point is that **nothing external
would have told us either**, and the same cell on four `c5.large` hosts would have cost the rung.

**Decisions taken (maintainer, 2026-08-13):**

- **The same-cell rerun uses `SLOTS=3200`, `CAPACITY=20`** — 256,000 fresh mutations against the
  200,693 requests this cell completed, which holds even under the conservative assumption that
  every request admits.
- **3200 is explicitly not the PR4b fixture size.** The final value is derived from the deepest
  rung the intended ladder reaches and then kept identical across `G1`, `G2` and `G4`. Sizing to
  the selected point is unsafe in a way that is easy to miss: an exhausted rung *above* the point
  invalidates the point itself, because the saturation argument rests on that higher rung having
  been short of service rather than short of fixture.
- **Reset and reseed are explicit before every measured rung and every confirmation run**, so no
  ladder point inherits depleted state. `itc-run.sh` owns the reseed rather than leaving it to the
  operator — "reseed between rungs" is exactly the step a twelve-cell ladder drops once, silently,
  after which every later point is wrong.
- **The generator-headroom control is deferred** until there is a high-useful-demand `G4` point to
  run it against. Against this cell it would have proved nothing: the generator sat at 12.5% of
  one core per core, so widening its CPU set could not have moved anything.

`itc-run.sh` now also reports the discriminator after each cell — admitted against supply — and
says plainly that an exhausted cell backs no capacity number. It reports rather than refuses,
because §5 is an evidence gate and not a provenance one, and a run that hits the fixture ceiling
is still a legitimate artifact of the experiment that produced it.

### 3.11 The rehearsal's first valid operating point, and what it is not

With `SLOTS=3200` the same G4 cell ran with useful demand throughout
([`docs/measurements/pr4a-rehearsal/points/c16/`](../../measurements/pr4a-rehearsal/points/c16/),
service and generator both `7e46f0f`, both unmodified, certified `capacity`):

| | |
|---|---|
| Goodput | 110,172 mutations over 60.036 s — **1,835 mutations/s** |
| Outcome mix | 100% `admitted_success`; no refusals, replays or invalid responses |
| Fixture | 110,172 of 256,000 consumed — 43%, with headroom throughout |
| Latency | p50 6.8 ms, p95 20.3 ms, p99 38.7 ms, max 69.1 ms |
| Generator | 7.7% of one core per core, `GOMAXPROCS` 4 |

**The strongest thing in the run is the agreement, not the number.** The bracketing scrapes give a
server-side delta per unit of **27,543 admitted, identical across all four**, summing exactly to
the client-reported 110,172. Two independent accountings agree, and the four capacity units are
balanced to the request — which is what `WL-MUT-DISP-4`'s round-robin over four organisations,
one homed per authority, must produce if placement and routing are correct.

**The window is not stationary, and 1,835/s is an average across a decay.** The scraped rate peaks
near **3,200/s a few seconds in and falls monotonically to about 1,300/s** by the end of the 60 s
window — roughly 2.5× within a single cell. Latency rises across the same span from the other
side, with p50, p95 and p99 all climbing (p99 approximately 0.02 s to 0.045 s), and per-unit
resident memory grows about 19 MB to 23 MB.

The pool is not the cause at this rung: connections in use oscillate around 2–4 against a maximum
of 4 per unit, and acquire-wait stays flat until after the load stops.

> **These within-window figures are not re-derivable and must not be quoted.** This cell predates
> per-cell series retention (`a678153`), so it holds bracketing scrapes and no panel export. Every
> shape statement in the two paragraphs above — the decay, the latency climb, the memory growth,
> the pool observation — was read live from Grafana and is now beyond its retention window. They
> are recorded as **observations that motivated §3.12**, not as evidence: `measurement-contract.md`
> §2 and §5.3 admit no figure that a retained artifact cannot reproduce. The endpoint totals
> (110,172 mutations, the per-unit 27,543 agreement, the latency percentiles) are unaffected —
> those come from `run.json` and the scrape pair. §3.12's cells are the retained version of this
> observation, and any decay claim that survives must rest on them.

> **Hypothesis withdrawn.** This section originally proposed that cost per mutation rises with
> accumulated state — the `btree_gist` exclusion index on `user_time_claims` growing under
> 110,172 inserts, in the measured write path. **§3.12 refutes it**: a 30 s cell reached *more*
> rows (92,125) than a 60 s cell (83,794) while still accelerating, so row count does not explain
> the rate. The withdrawal is kept visible rather than edited away, because the reasoning was
> plausible, the mechanism is real, and it is the explanation someone will reach for again.

What is not in doubt is the shape: a single reported rate does not describe this window.

### 3.11.1 A second rung, and why the pair cannot yet be compared

`c=32`, same fixture and reseed
([`docs/measurements/pr4a-rehearsal/points/c32/`](../../measurements/pr4a-rehearsal/points/c32/),
certified `capacity`):

| | `c=16` | `c=32` |
|---|---|---|
| Goodput | 1,835/s | 2,153/s (+17.4%) |
| p50 | 6.8 ms | 5.8 ms |
| p95 | 20.3 ms | 48.1 ms |
| p99 | 38.7 ms | 60.3 ms |
| Generator | 7.7%/core | 9.0%/core |

Per-unit balance held exactly (32,323 / 32,323 / 32,322 / 32,322). Doubling concurrency bought
17.4% more Goodput while the tail grew two to three times and p50 *fell* — the signature of
queueing rather than of added capacity, and `c=32` is the first rung past `aggregate_pool_size`.

**Both figures are averages over non-stationary windows, so the pair does not yet support a
saturation argument.** Two rungs can differ by tens of percent while measuring the same service
over different portions of the same decay curve, and a saturation argument selects an operating
point precisely by claiming a higher rung produced no more sustained Goodput. "Sustained" is the
word doing the work, and it is the property these windows have not been shown to have. The rungs
are recorded as observations; no frontier is claimed from them.

Characterising the decay is therefore a prerequisite for the ladder, not a refinement of it
(§4).

**This is not a capacity point, and no ladder rests on it yet.** It is a single operating point at
concurrency 16. Nothing in it indicates saturation: every request was admitted, latency is well
inside both §7 gates, and the generator was at 7.7%. A mutation-capacity claim requires a rung
*above* the selected point that produced no more sustained Goodput, and no such rung has been run.

**Concurrency 16 equals `aggregate_pool_size` 16**, which is worth stating before the ladder is
designed rather than discovered inside it. Four connections per unit against sixteen closed-loop
workers means every worker can hold a connection and the pool is exactly not a constraint at this
rung; the next rung begins queueing. PR2 found the connection pool to be the frontier on a single
instance, so a ladder that stops at or below this boundary would establish nothing about where
this topology's frontier is.

**Evidence class.** Rehearsal/diagnostic only (§2.14): all four groups share one workstation,
kernel, storage path and page cache, so the partition bounds CPU and nothing else. It cannot
discharge `VAL-SCALE-5`, cannot become a Tier-2 result, and is never mixed with AWS points to
derive `E2` or `E4`. It is also a **single reading**, unreproduced — and the workstation's
throughput moved by roughly 2× between PR2 runs, which is the variance any local number here
inherits until reproduced.

What it does establish is the thing PR4a exists for: the G1/G2/G4 machinery — placement, fixture,
workload semantics, declaration, observed deployment, provenance, cpuset partition, generator
confinement, scrape coverage and certification — runs end to end and produces an internally
consistent artifact, before any metered AWS time is spent.

### 3.12 The window experiment answered a different question, and reproduced PR2's open anomaly

Three cells at `c=16`, `SLOTS=3200`, identical but for window length, each reseeded from the same
state. The intent was to test whether reported Goodput depends on how long a rung runs (§3.11).

All three are retained at
[`docs/measurements/pr4a-rehearsal/windows/`](../../measurements/pr4a-rehearsal/windows/) —
[`30s/`](../../measurements/pr4a-rehearsal/windows/30s/),
[`60s/`](../../measurements/pr4a-rehearsal/windows/60s/) and
[`120s-exhausted/`](../../measurements/pr4a-rehearsal/windows/120s-exhausted/) —
with their panel exports and TSDB snapshots. Every figure below is re-derivable from those
files; that directory's README carries the evidence class and the reading caveats.

**Those artifacts are labelled `topology=single-instance-local`, which is wrong — read it as
`itc-g4`.** `prometheus.yml` set `topology` job-wide at the PR2 value, so every Iteration C
sample inherited PR2's identity, and nothing reported it because the label was present and
well-formed. The figures are unaffected — `authority`, `unit` and `instance` are all correct, and
`run.json`'s manifest is the authoritative environment identity — but a snapshot separated from
its directory would misdescribe itself. Topology ownership has moved to the target documents and
`itc-obs-targets-test.sh` guards it; these cells are annotated rather than re-run, because the
degraded regime they captured cannot be reproduced on demand.

**The 120 s cell is invalid and is excluded.** It admitted **exactly 256,000 of 256,000** — its
whole supply — and then took a further 128,239 `no_capacity` refusals, so its rate of 2,133/s is
precisely `supply ÷ duration` and describes the fixture rather than the service. The exhaustion
reporter added in §3.10 fired, which is the first time that check has caught a live cell. Note
that the cell is still `measurement_sound`: the accounting reconciles, which is a different
property from having measured the service.

**The remaining two were not the same experiment.** Read from the per-cell panel exports:

| | 30 s cell | 60 s cell |
|---|---|---|
| Goodput | 2,874 → **3,208/s**, rising | 2,194 → **927/s**, falling |
| Process CPU | 1.68 → **2.11 cores**, rising | 1.46 → **0.70 cores**, falling |
| Pool acquire wait | 2.72 → **1.51**, falling | 3.95 → **7.12**, rising |
| Pool connections in use | 15, 15, 13, **16** of 16 | 10, 9, … **7** of 16 |
| Admitted | 92,125 | 83,794 |

One run accelerated throughout; the other degraded from its first exported sample and **admitted
9% fewer mutations in twice the time**. Window length is therefore confounded with which regime a
run lands in, and the original question is unanswered: no rung comparison — and so no ladder — is
possible until the two are separable.

**Throughput and CPU fall together**, 2.4× against 2.1×. That is evidence that the service is doing
less work while the request path slows, and it is why the degraded cell must not be treated as an
outlier and dropped. It does not by itself identify which layer below or around the service caused
the stall.

**The pool series is the sharpest evidence, and it is new — but its first interpretation was too
strong.** In the degraded cell, acquired connections fall to **7** while acquire-wait nearly
doubles, against a configured aggregate maximum of 16. `MaxConns=16` is a ceiling, not evidence
that sixteen connections currently exist, so this observation does **not** prove that nine existing
connections are being withheld. The missing distinction is the pool's actual population and state:
total, idle and constructing connections, together with acquisition/lifecycle counters. What the
retained cell establishes is narrower and still important: workers are waiting to acquire while
fewer connections are acquired, so connection acquisition/availability becomes a concrete part of
the degraded-regime diagnosis rather than a generic “service saturation” story.

**It matches PR2's open throughput anomaly on two of its signature elements and contradicts a
third.**

| PR2's signature | this cell |
|---|---|
| throughput falls to 0.25–0.54 of the healthy rate | 0.45 — matches |
| service process CPU roughly halves | 1.46 → 0.70 cores, 0.48 — matches |
| ~~the generator's CPU halves too~~ | 0.1146 → 0.0588 per core — **withdrawn, carries no information** (below) |
| pool acquire-wait is *unchanged* | 3.95 → 7.12, nearly doubled — **contradicts** |

> **The generator row is withdrawn.** This section originally counted the generator's CPU halving
> as a third matching element, and called it the most distinctive marker of the set. It is not a
> marker at all. `alloca-load` is **closed-loop**: a fixed pool of workers each send a request,
> block on the response, and send the next (`internal/loadgen/run.go`; open-loop rate control is
> named there as a later addition). When the service path slows, completions fall and those
> workers spend correspondingly longer blocked in I/O, so generator CPU tracks throughput
> *mechanically* — in a perfectly healthy generator, with nothing whatever reaching CPUs 8–11.
> A 0.51 ratio beside a 0.45 throughput ratio is the expected arithmetic, not a coincidence
> needing explanation.
>
> The withdrawal is kept visible rather than edited away because the reasoning was seductive: two
> independent-looking processes falling by the same factor reads like a common cause, and the
> closed-loop coupling that makes it inevitable is invisible unless you go and look at the
> generator's own loop.

The two surviving matches are weaker evidence than three, and the pool divergence remains real
and unexplained. This record does not assert the two are the same fault.

**What the rehearsal adds that PR2 could not — stated narrowly.** In PR2 the generator and the
service shared one unpartitioned host, so "both slowed together" was compatible with them simply
contending for the same CPUs. Here the generator is confined to CPUs 8–11 and every unit to 0–7,
disjoint sets that `itc-cpuset-check.sh` verified for this run. **What that excludes is exactly
one thing: direct competition for the same WSL-visible logical CPUs between the generator and the
units.** That is worth having, because it was a live candidate in PR2 and could not be tested
there.

It is not more than that, and the earlier draft of this section claimed more. Disjoint cpusets do
**not** exclude a shared kernel, a shared storage path, shared physical cores or SMT siblings
behind the logical CPUs, or Windows-side contention above the VM — every one of which reaches
processes that share no logical core. So the host-level candidates PR2 named (WSL2 CPU steal,
Docker Desktop storage-path latency, Windows-side contention) are **neither confirmed nor
excluded** by this run. They remain the open question, which is why the host sensor below is now
load-bearing rather than a refinement.

It also shows the anomaly is **not specific to the single-instance deployment** — it appears
across four independently pinned units, each with its own PostgreSQL.

**Consequences.** `node_exporter` (§2.1) stops being deferrable: it is the host-level sensor this
diagnosis needs. The service/pool side also needs enough pgxpool state to distinguish the configured
maximum from the actual connection population — at minimum total/idle/constructing state plus the
existing acquisition signal and lifecycle/reconnection evidence. `postgres_exporter` is **not yet
made mandatory by this observation alone**: add it if host + fuller pool evidence still leaves the
stall at the PostgreSQL boundary unresolved. One run per point cannot separate a regime from a
trend, so each window needs repeating. And the 120 s point needs a fixture that cannot bound it.

### 3.13 Maintainer decision: qualify the degraded regime in PR4a before AWS

**Decision, 2026-08-14:** §3.12 is a PR4a blocker, not PR4b exploratory work.

The reason is experimental rather than architectural. Two nominally comparable local `c=16` cells
landed in materially different regimes: one accelerated while another degraded from its first
retained samples. Until that distinction is understood or made observable, a `G1/G2/G4` AWS matrix
could compare different regimes and report the difference as scale efficiency. Moving the same
ambiguity to independent hosts would make the environment more expensive without making the result
more interpretable.

PR4a therefore expands from **local machinery rehearsal** to **local measurement qualification +
AWS readiness**. Before PR4b starts, the degraded regime must meet one of these bounded outcomes:

1. a root cause is demonstrated and fixed; or
2. the cause is not fully eliminated, but a reliable discriminator/preflight/run-admission rule
   makes the regime detectable and excludable (or otherwise conservatively bounded) so an AWS
   topology comparison cannot unknowingly mix it with the healthy regime.

The second outcome is deliberately acceptable. PR4a is not required to explain every performance
characteristic of WSL2, Docker Desktop or the host; it is required to remove this known ambiguity
from the **method PR4b will use**.

The diagnostic order is also bounded. Add the already-selected `node_exporter` path and richer
pgxpool population/state evidence first, repeat comparable cells enough to capture and distinguish
regimes, then decide from evidence whether PostgreSQL-side instrumentation such as
`postgres_exporter` is necessary. Do not add the exporter merely because PostgreSQL is plausible.
The local generator-headroom control follows once a high-useful-demand point is stable enough for
that comparison to mean something.

**Scheduling consequence.** The existing **4.0 development days for PR4 remain unchanged**, but
the original 1.5-day PR4a / up-to-2.5-day PR4b split is no longer binding. The workstation has
proved a useful, repeatable, unmetered diagnostic environment while AWS quota approval is slower
than planned, so PR4a may consume more of the shared envelope and PR4b correspondingly shrinks into
a bounded cloud execution pass. Contingency is not touched unless the combined PR4 work exceeds its
4.0-day allocation.

This is not a new Iteration C Problem. It is evidence from implementation exposing a blocker to
answering the existing Problem reliably, which is exactly when the schedule is expected to follow
the evidence rather than preserve a stale PR boundary.

### 3.13.1 The retained cells already answer the pool question, and narrow it sharply

§3.13 asks for "fuller pgxpool population/state evidence" as new instrumentation. **Two thirds of
it was already retained.** `alloca_db_pool_total_connections` and
`alloca_db_pool_idle_connections` are scraped by the service's pool collector and sit in both
cells' TSDB snapshots; they were simply never exported to `panels/`. This is the snapshot doing
exactly what it is kept for — recovering a series nobody thought to export
(`docs/measurements/README.md`, *What a sweep cell contains*).

Queried from the retained snapshot, bounded by each cell's own `panels/index.json` window,
aggregated across all four units:

| | 30 s cell (healthy) | 60 s cell (degraded) |
|---|---|---|
| `max` (ceiling) | 16, flat | 16, flat |
| `total` (population) | **16, flat** | **16, flat** |
| `idle` | 16 → 4 → **0** | 16 → 3 → **9** |
| `acquired` | 0 → 12 → **16** | 0 → 13 → **7** |
| acquires/s | 997 → **3,209** | 641 → 2,194 → **928** |
| **mean acquire duration** | 0.53 → **0.47 ms** | 0.56 → **7.67 ms** |

**The population never fell.** `total` is pinned at 16 for every sample of both windows, and
`total == idle + acquired` holds on 13/13 and 7/7 samples. So the alternative §3.13 correctly
insisted on — that the pool's population had fallen, or was constructing/reconnecting — **is
refuted for this cell**. It does not need a repeat run to settle.

**The degraded pool was not saturated.** The healthy cell pins `idle` at 0 with `acquired` at the
full 16, which is what a service genuinely using its pool to the ceiling looks like. The degraded
cell holds **6–9 connections idle** while `acquired` sits at 7–10 of 16. Whatever is limiting it,
it is not pool capacity.

**Mean acquire duration rose 16× while connections sat idle and available** — 0.47 ms to 7.67 ms,
climbing monotonically. That is the sharp, new observation, and it is a *per-acquire mean*
(`rate(duration)/rate(count)`), not the aggregate rate the earlier draft quoted.

**What it does not establish, and why the number cannot say.** `AcquireDuration` is the duration
of the whole `Acquire()` call, not blocked-waiting time: `puddle/pool.go` starts the clock on
entry and adds the elapsed time on *every* successful path, including the one that constructs a
brand-new connection. So 7.67 ms is consistent with at least three different mechanisms, and the
retained series cannot separate them:

1. **connection churn** — idle connections destroyed by lifetime/idle limits and re-established,
   so the acquire pays a full connection setup;
2. **contention inside the pool's own mutex/semaphore**;
3. **wall-clock inflation** from host-level descheduling, which would inflate every measured
   duration without any pool pathology at all.

Candidate 3 is the one the host sensor exists to test, but it is already weakened from inside
this data: request p50 rose only 1.17× (4.36 → 5.08 ms) across the same window in which acquire
rose 16×. Uniform wall-clock inflation would move both alike. It does not rule out a *selective*
host effect, and it is not by itself a refutation.

**The instrumentation this actually justifies is now specific, and it is small.** `pgxpool.Stat`
already exposes everything needed; the collector exports six of its thirteen methods. Adding
these separates all three candidates:

| Missing metric | What it contributes |
|---|---|
| `EmptyAcquireWaitTime()` | time on acquires that found no idle connection. **Not pure contention** — see below |
| `EmptyAcquireCount()` | how many acquires found no idle connection, whether they waited or constructed |
| `NewConnsCount()` | constructions started. The series that separates the two things the metric above conflates |
| `MaxLifetimeDestroyCount()`, `MaxIdleDestroyCount()` | whether the pool was destroying connections underneath the flat `total` |
| `ConstructingConns()` | construction in flight at sample time |
| `CanceledAcquireCount()` | acquires abandoned under context cancellation |

> **`EmptyAcquireWaitTime` is not blocked-waiting time either, and an earlier draft of this
> section said it was.** Verified against the pinned `puddle v2.2.2`: the counter accumulates on
> acquires that found no idle resource, and that covers *both* waiting for one to be released and
> constructing a new one — on the construction path the clock is read after the constructor
> returns, so the full construction time lands in it (`pool.go`, the `emptyAcquireWaitTime +=
> waitTime` after `initResourceValue`). No single series in this set is a clean contention signal.

**No metric decides this alone; the combination does.** Read together:

| Observation | Reading |
|---|---|
| empty-wait ↑ **and** new-conns ↑ | construction/churn is implicated |
| empty-wait ↑ **and** new-conns flat | acquires waited for an existing connection to be released |
| acquire-duration ↑ **and** empty-wait flat | the delay is elsewhere in the immediate acquire path, rather than in pool-empty waiting or construction |

**Localisation, stated at its actual strength.** The symptom is localised to the **connection-
acquire path**, which is a real narrowing from "something in the request path". It does **not**
place the fault inside the service and exclude PostgreSQL: pgxpool's resource constructor calls
`pgx.ConnectConfig` (`pgxpool/pool.go`), so a construction event includes PostgreSQL and network
connection establishment — TCP, TLS and authentication. An earlier draft of this section claimed
the acquire path "is inside the service"; that is wrong wherever construction is involved, which
is precisely the case `NewConnsCount` exists to detect.

So `postgres_exporter` stays **evidence-triggered**, per §3.13: current evidence does not justify
it, and equally does not exclude PostgreSQL participation. What decides it is the next cell.

**Shipped.** All seven are exported by the pool collector, and the diagnostic series are retained
per cell as `pool_total`, `pool_idle`, `pool_constructing`, `pool_new_conns`, `pool_destroys` and
`pool_empty_acquire_wait`. The dashboard now separates **occupancy** (acquired, idle, total) from
**lifecycle** (constructing, new, destroyed) from **acquire cost**, because one graph carrying all
three answered none of them: on a four-unit rung it rendered a dozen identically-labelled lines
plus four flat ones from `pool_max`, a per-unit constant that has been dropped from the plot
entirely — the configured ceiling is in each run's manifest as `aggregate_pool_size`.

Per-acquire cost is no longer its own panel; divide `pool_acquire_wait` by
`rate(alloca_db_pool_acquires_total)`, which the snapshot retains. That keeps the dashboard inside
its 8-graph scope bound while spending the room on the three questions §3.13.1 actually asks. `TestPoolCollectorDescribesEveryMetricItCollects` and its `Collect` counterpart
pin the exported set by name, because a missing series here is invisible: the scrape still
succeeds and the populated-series gate still passes for the metrics that *are* present. Both were
proven discriminating by removing a metric from each half.

**Two naming traps are kept rather than fixed.** Neither
`alloca_db_pool_acquire_wait_seconds_total` nor `alloca_db_pool_empty_acquire_wait_seconds_total`
measures waiting, and both names say they do. They are left alone because the retained cells in
`docs/measurements/` and the committed panels query them, and renaming would break comparison
against evidence already taken — the same reason §3.12's cells keep their wrong topology label.
Instead each help text states what it actually measures and names the series needed to interpret
it, the panel notes carry the same warning, and
`TestNeitherTimingMetricClaimsToBePureWaiting` fails if either reverts. A name that has already
misled one reading will mislead another; the disclaimer travels with the metric rather than
living only here.

**Sequencing.** `node_exporter` goes in **before** the next attempt to reproduce the degraded
regime, not after. The regime is rare, and a cell that captures pool evidence without host
evidence would leave the same ambiguity standing one run later — the point is for a single
degraded cell to carry both.

### 3.14 `node_exporter` is in, and the local host cannot serve the instrument §2.3 named

Built as §2.1 specified: `node_exporter` beside Prometheus, a restricted collector set, its own
scrape job at 5 s (§2.3), host panels, and a per-cell gate that refuses a run which retained no
host samples (§2.5). Verified live against the running G4 rehearsal — both jobs healthy, all four
host panels returning points over a range query.

**One exporter, because the local rehearsal has one host.** §2.1 asks for it on every
capacity-unit host and on the generator/monitor host. On this workstation all four shard groups
and the generator share a single WSL VM, so one exporter observes all of them and a second would
report the same kernel twice. That the set is singular *is* the rehearsal's limitation, stated
plainly rather than papered over: it is why these runs are rehearsal evidence and not capacity
evidence, and the label says so — the host job carries `topology=rehearsal-host`.

**PSI is not available on this kernel, and PSI was the instrument the diagnosis wanted.** §2.3
named `pressure` among the collectors, and it is the most direct answer to the §3.12 question,
because it reports *contention* rather than consumption — a host can be far from saturated and
still be stalling. The WSL2 kernel has no `/proc/pressure`: the collector loads and reports
`node_scrape_collector_success{collector="pressure"} 0`, every other collector reporting 1.

This is exactly the kind of thing PR4a exists to find before AWS time is spent. Two consequences:

- The collector stays enabled. It costs one series, it reports its own failure, and the AWS
  hosts' kernels are expected to carry PSI — so nothing needs changing there.
- **`node_load1` is the substitute** while PSI is missing, panelled as *Host run queue*. Run-queue
  depth against CPU count is the classic oversubscription signal and, unlike utilisation, it rises
  when work is *waiting* rather than when work is being done. It is coarser than PSI: a one-minute
  average cannot resolve a stall inside a 30 s cell, so it bounds the question rather than
  answering it. A pressure panel supersedes it the moment a host serves one.

**The collector set is deliberately wider than the panel set.** `diskstats`, `netdev` and
`filesystem` are scraped and not plotted. Collect broadly, panel narrowly: the snapshot retains
everything scraped, and §3.13.1 is the worked example of recovering a series nobody thought to
export — the pool population question was answered from a retained snapshot without re-running a
cell.

**What this does not do.** It does not diagnose §3.12. The degraded regime has not recurred since
the sensor was added, and one host sensor on a shared-kernel workstation cannot separate a
Windows-side effect from a WSL-side one anyway. What it does is make the next degraded cell carry
both pool and host evidence, which is the §3.13 sequencing requirement and the reason it went in
before the next reproduction attempt rather than after.

## 4. Open items

- **Rung duration** stays open until §2.4's preflight derives it.
- **Within-window decay must be characterised before any ladder** (§3.11). Goodput falls ~2.5×
  inside a single 60 s cell, so a rung's reported average depends on how long it ran, and two
  rungs are not comparable until that dependence is quantified or removed. The 30 s / 60 s / 120 s
  experiment this item called for **has now been run and did not answer it** (§3.12): the cells
  landed in different regimes, so window length is confounded with regime and the dependence is
  still unquantified. The ladder still needs either a defined steady-state slice or a bounded
  fixture-state budget per rung, and now also needs the regime separated first.
- ~~**The rate series is not retained per cell.**~~ **Done** (`a678153`). `itc-run.sh` exports the
  per-cell panel CSVs and a TSDB snapshot alongside the bracketing scrapes, so a cell now carries
  its shape and not just its endpoints. §3.12 is the first analysis that depends on it, and its
  three cells are retained at
  [`docs/measurements/pr4a-rehearsal/windows/`](../../measurements/pr4a-rehearsal/windows/). The
  two §3.11 cells predate the change and are retained at
  [`points/`](../../measurements/pr4a-rehearsal/points/) with bracketing scrapes only — which is
  why their decay is described from Grafana and is **not** re-derivable from a retained artifact.
- **The saturation ladder** has not been run, and cannot be until the two items above are settled.
  Concurrency 16 sits exactly at `aggregate_pool_size`, so the ladder must deliberately cross it;
  `c=32` (§3.11.1) is the first rung past it and is recorded as an observation, not a rung.
- **Final `SLOTS`** is still to be derived from the deepest rung that ladder reaches, then held
  identical across `G1`, `G2` and `G4` (§3.10). 3200 is an interim value sized to one c=16 cell.
- **The generator-headroom control** is runnable but should be taken against a stable high-useful-
  demand `G4` point after the degraded-regime qualification, so the control does not compare two
  different regimes by accident.
- **Per-unit panel aggregation and the dashboard retitle** (§2.6) are not done. The units are
  scraped and labelled by `authority`, but the committed dashboard is still PR2's
  single-instance one.
- **Generator currency is unchecked.** `itc-run.sh` verifies the generator binary exists, not that
  it matches the tree, so a stale binary that still accepts its arguments would run and certify
  while `generator_commit_sha` honestly records a revision that is not the one under test.
  `sweep.sh` has the same shape. Whether a mismatch should refuse or warn is a measurement-contract
  question.
- **Whether monitoring splits onto its own host** stays open until §2.2's preflight says whether it
  needs to.
- ~~**`node_exporter` is no longer deferrable**~~ **Done** (§3.14). Built with a restricted
  collector set, its own 5 s job, host panels and a per-cell gate that refuses a run retaining no
  host samples. **PSI is unavailable on the WSL2 kernel** and `node_load1` is its coarser stand-in,
  which bounds the stall question rather than answering it — a pressure panel supersedes it on any
  host whose kernel serves `/proc/pressure`.
- **Fuller pgxpool state is required for the local diagnosis.** The existing acquired/max/wait
  series cannot distinguish “connections exist but are unavailable” from a pool whose actual
  population has fallen or is constructing/reconnecting. Retain enough total/idle/constructing and
  lifecycle evidence to make that distinction.
- **`postgres_exporter` remains conditional** (§3.13). Add it inside PR4a only if node + fuller
  pool evidence still leaves a material PostgreSQL-side ambiguity; do not instrument by guess.
- **The degraded regime must be qualified before any ladder or PR4b** (§3.12–§3.13). Repeat
  comparable cells enough to establish the discriminating signal; the goal is root cause/fix or a
  reliable exclusion/bounding rule, not an unlimited WSL investigation. Re-run the 120 s point only
  with a fixture that cannot bound it (`SLOTS=8000` gives 640,000).
- **Per-organisation fixture size for the sweep** (§3.5) must be justified against the deepest rung
  the ladder will reach, not the selected point, now that fixture exhaustion at a higher rung
  invalidates the point below it. Derived during PR4a preflight alongside the rung duration (§2.4).
- **`make image-provenance` before every metered run** (§3.6), because an uncommitted file makes
  the run certify at `none` and the cost of learning that on AWS is the rung.
- **AWS quota** remains an external dependency for PR4b's independently provisioned capacity
  evidence. If it does not permit the complete G4 environment within the AG-Sept timebox, record
  the blocker and leave `VAL-SCALE-5` explicitly unproven rather than delaying PR5 or promoting the
  local rehearsal.

## 5. AWS Standard On-Demand quota increase declined — 2026-08-16

AWS Support declined the requested increase to the **Running On-Demand Standard
(A, C, D, H, I, M, R, T, Z)** instance quota in `eu-west-2` after review of the appeal. The support
response explicitly says the Alloca use case was noted, but the account does not yet have enough
established AWS service usage and on-time billing history for the increase to be approved. AWS may
reassess after that account history exists.

**Interpretation:** this is an external account-maturity/provisioning constraint, not evidence about
Alloca's architecture, capacity, or the validity of the Iteration C experiment. It does not trigger
Tier 2: the validation plan requires the complete independently provisioned G4 measurement
environment to exist before Tier 2 can be considered.

**Maintainer decision:** do not spend the current PR4a budget on another immediate appeal. Continue
the local measurement-qualification work, which is unmetered and has already exposed material
experiment defects and the degraded-regime ambiguity. A bounded AWS bootstrap may still use the
quota already available if it removes AWS-specific uncertainty, but any such bootstrap is
operational evidence only and never capacity evidence. Build genuine AWS usage/billing history and
re-request the quota later; AG-Sept does not wait on that approval.

PR4b therefore remains conditional on enough quota to instantiate the **complete** independently
provisioned G1/G2/G4 environment. If that environment is still unavailable inside the AG-Sept
timebox, PR4 records the AWS blocker, leaves aggregate capacity scaling and `VAL-SCALE-5` explicitly
unproven, and proceeds to PR5. Neither the shared-workstation rehearsal nor a partial AWS topology
is promoted as a substitute result.

**Budget consequence:** none. The existing shared 4.0-day PR4a/PR4b envelope remains the scheduling
owner; this denial changes the probability and likely size of the PR4b cloud pass, not the milestone
budget or the validation rule.
