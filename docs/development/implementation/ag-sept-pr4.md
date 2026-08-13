# AG-Sept PR4 — Independently provisioned shard-group capacity

**Type:** Implementation record, spanning PR4a/PR4b
**Status:** PR4a in progress. The local multi-group topology and workload machinery are being
exercised before any metered AWS capacity attempt; AWS capacity evidence remains conditional on
sufficient quota within the milestone timebox.
**Budget:** 4.0 development days ([AG-Sept plan](../../planning/ag-sept-plan.md) §2, a scheduling
fact) — 1.5 for PR4a's capacity-environment rehearsal/bootstrap work, up to 2.5 for PR4b's AWS
evidence and report when quota permits.
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
rehearsal and fixes or explicitly bounds what that rehearsal finds. An AWS bootstrap may follow when
it buys useful confidence while quota is pending. PR4b runs the independently provisioned AWS
capacity experiment only when sufficient quota exists to instantiate the complete environment.

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
selection rule. PR4a's responsibility is to rehearse the measurement machinery and retain enough
generator and resource evidence to distinguish a server frontier from a measurement-system
frontier once AWS can run. PR4b's responsibility is to apply the owning validation rule and keep
its conclusion bounded to the strongest evidence actually established.

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
Iteration C machinery before spending AWS time. WSL/Docker exposes **12 logical CPUs** for the
rehearsal and partitions them into non-overlapping scheduler-visible CPU sets:

```text
capacity unit A    CPUs 0-1    service A + PostgreSQL A
capacity unit B    CPUs 2-3    service B + PostgreSQL B
capacity unit C    CPUs 4-5    service C + PostgreSQL C
capacity unit D    CPUs 6-7    service D + PostgreSQL D
generator/monitor  CPUs 8-11   load generator + monitoring
```

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

The execution sequence is now:

```text
local 12-vCPU partitioned rehearsal
    -> fix anything the rehearsal discovers
    -> optional t2.micro AWS bootstrap if quota is still pending and the proof is useful
    -> c5 AWS capacity experiment if sufficient quota is available
    -> otherwise retain the quota limitation and explicit VAL-SCALE-5-unproven result
    -> PR5 closeout
```

The last branch is deliberate: successful AWS capacity measurement is a stronger desired result,
not an Iteration C exit gate. An external quota decision must not force AG-Sept either to wait
indefinitely or to promote shared-workstation evidence beyond what it proves.

## 3. Discovered during implementation

These are findings, not decisions taken in advance. Each changed something that had already
been designed, and each was invisible to review — all three were found by running something.

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

**Still open, and deliberately so:** a run of 200 refusals against an exhausted fixture certifies
at `capacity` with zero goodput. Nothing in the provenance ladder refuses it, and nothing should —
the run describes itself honestly and the outcome mix is right there in the totals. Whether an
all-refusal run may back a capacity *claim* is `measurement-contract.md` §5's question about
evidence, not that document's §13 about provenance, and PR4b answers it per capacity point rather
than here.

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

## 4. Open items

- **Rung duration** stays open until §2.4's preflight derives it.
- **Whether monitoring splits onto its own host** stays open until §2.2's preflight says whether it
  needs to.
- **`postgres_exporter`** is deferred with a trigger, not dropped (§2.1).
- **Whether an all-refusal run may back a capacity claim** (§3.5) is left to PR4b, per quoted
  point. The provenance ladder certifies it and should; the evidence question is
  `measurement-contract.md` §5's.
- **`make image-provenance` before every metered run** (§3.6), because an uncommitted file makes
  the run certify at `none` and the cost of learning that on AWS is the rung.
- **AWS quota** remains an external dependency for PR4b's independently provisioned capacity
  evidence. If it does not permit the complete G4 environment within the AG-Sept timebox, record
  the blocker and leave `VAL-SCALE-5` explicitly unproven rather than delaying PR5 or promoting the
  local rehearsal.
