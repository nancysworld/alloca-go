# AG-Sept PR4 — Independently provisioned shard-group capacity

**Type:** Implementation record, spanning PR4a/PR4b/PR4c
**Status:** merged — PR4a (#19), PR4b (#20), PR4c (#21).

**Scheduled under:** [`ag-sept/milestone-plan.md`](../../planning/ag-sept/milestone-plan.md) §3,
work units *PR4a — method qualification*, *PR4b — scheduler-partitioned evidence* and *PR4c —
independent probe attempt*. That is a scheduling and historical citation only; what this work had
to satisfy is owned by the documents below.

**Owner documents.** [`deployment-architecture.md`](../../design/deployment-architecture.md) §13
owns the capacity environment; [`horizontal-scaling.md`](../../design/horizontal-scaling.md)
§12–§13 owns the capacity-unit model; `REQ-SCALE-4` and `REQ-EVID-2`
([`system-requirements.md`](../../requirements/system-requirements.md)) own what must be true;
[`workload-catalog.md`](../../design/workload-catalog.md) owns `WL-MUT-DISP-4`;
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) §4.6 owns the
matrix, the two-tier result model, the selection rule, `VAL-SCALE-5` and `VAL-NEG-7`; and
[`measurement-contract.md`](../../design/measurement-contract.md) §11–§13 owns provenance and the
quotability ladder. The results are owned by
[`ag-sept-pr4-scheduler-partitioned-capacity.md`](../../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md).
Where this record disagrees with an owning document, the owning document wins.

This record keeps the implementation-specific lessons. It does not reconstruct the PRs.

## 1. Outcome

PR4a built and qualified the Iteration C measurement method on a scheduler-partitioned
workstation; PR4b executed the complete local `G1`/`G2`/`G4` comparison; PR4c stopped at
provisioning after the quota increase was declined (§5).

- **`G4_local` = 3493.9/s** is the one resolved capacity quantity (§3.28).
- **`G1` and `G2` are explicitly unresolved**, so `G1_local`, `G2_local` and — because `G1_local`
  is their denominator — `E2_local` and `E4_local` are withheld. **`VAL-SCALE-6` is executed and
  not discharged** (§3.28, §3.30).
- **`VAL-SCALE-5` remains unproven.** No independently provisioned performance cell ran. That
  closes the PR4c work unit, not the Iteration C Problem, which stays open.
- The limiting term is localised to the **shared write path**, not to `alloca-go` (§3.30). The
  killing test was deliberately not run.

Local rehearsal and sustained numbers are diagnostic evidence about the method. They cannot
discharge `VAL-SCALE-5`, never become Tier 2, and are never mixed with independently provisioned
points to derive an efficiency (§2.14).

**Report:**
[`ag-sept-pr4-scheduler-partitioned-capacity.md`](../../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md)
owns `G4_local`, the withheld quantities and the evidence boundary. Retained artifacts are in
[`pr4a-rehearsal/`](../../measurements/pr4a-rehearsal/),
[`pr4a-sustained/`](../../measurements/pr4a-sustained/),
[`pr4b-recon/`](../../measurements/pr4b-recon/),
[`pr4b-capacity/`](../../measurements/pr4b-capacity/),
[`pr4b-drift-g1/`](../../measurements/pr4b-drift-g1/) and
[`pr4c-quota/`](../../measurements/pr4c-quota/).

## 2. Implementation decisions that mattered

### 2.1 `VAL-NEG-7`'s host sensor is `node_exporter` on the existing PR2 substrate

`node_exporter` on every capacity-unit host and on the generator/monitoring host, retained through
the same per-cell export as everything else. The decisive reason is the gate that already exists
rather than cost: `sweep.sh` refuses a cell whose panel exports are missing, and separately refuses
one whose required panels retained **no samples**, so host evidence added to `panels.json` inherits
a refusal path instead of needing a new one.

**Rejected — CloudWatch as the primary instrument:** 5-minute basic granularity cannot describe a
saturation rung, and detailed monitoring still lands coarser than the retained series. **Deferred —
`postgres_exporter`**, on an evidence trigger rather than a date: the first capacity evidence
showing PostgreSQL needs finer attribution than the pool and service metrics provide. Deliberately
not filed in [`tech-debts.md`](../../planning/tech-debts.md), whose §1 admits gaps in behaviour
correct today under a stated condition — a scheduling choice does not belong there. Discharged in
§3.16.

### 2.2 Monitoring starts on the generator host, and moves only on evidence

The generator preflight required by `deployment-architecture.md` §13.2 runs **with the monitoring
stack active**, so the headroom demonstrated is the headroom the measured runs have; and **no query
runs during a measured rung**, because a query engine under variable load on the generator host is
the one part of this arrangement that could move while a rung is being measured. If a preflight or
a quoted point leaves generator headroom ambiguous, monitoring splits onto its own instance and the
affected points are requalified.

### 2.3 Scrape cadence is per job, and the host job is not the service job

The service is scraped at 1s deliberately: a cell runs for tens of seconds and a 15s interval would
alias the pool-pressure signal the frontier is read from. That reasoning is about the service and
does not transfer to `node_exporter`, whose default collector set is orders of magnitude more
series per target, so the host job gets its own ~5s interval and a restricted collector set.

### 2.4 The rate window becomes per panel, and the minimum rung is derived rather than asserted

Range queries start one full rate window after the measured phase opens so no exported point reads
warm-up samples, which makes **a rung's usable retained series shorter than the rung by one range**
— at 15s range and 5s step, a 60s rung retains ten points, not twelve. Panels therefore carry a
per-panel range, because a host series and a request-rate series do not want the same window, and
the minimum useful rung duration is *derived* from the resolution actually achieved. A rung length
asserted before the exporter has run against the real topology is a guess wearing a number.

### 2.5 The populated-series gate extends to the host panels

The gate required non-empty series for three panels that carry data in any cell whose service was
scraped. With four capacity units, a run in which **one** host's exporter was unreachable would
satisfy every existing check while losing precisely the evidence `VAL-NEG-7` exists to retain. It
is therefore per expected capacity-unit host, with the expected set taken from the same
operator-supplied input as the manifest (§2.7), so the gate and the provenance cannot disagree
about how many units the run had.

### 2.6 Panel expressions are scoped to their job and aggregated per unit

Four committed panels select metric families that **every** Prometheus-instrumented Go process
exports, including `node_exporter` and Prometheus itself. They were unambiguous only because
Prometheus scraped one job; adding a second silently changes what they mean, and **the defect would
not announce itself** — the CSV would still export, still be non-empty, and still pass every check.
They are scoped to `job="alloca-go"` as part of adding the host job, not afterwards, and the same
pass gives the service panels per-unit aggregation.

### 2.7 The manifest fields arrive as a JSON document, and two of the four are derived

`Manifest.Validate` gates `capacity` on four fields that nothing populated, which is why no run here
had ever reached `capacity`. They arrive as an operator-declared JSON document rather than four CLI
flags, because the fields describe one coherent run shape and should be validated atomically.

**It is a separate file from the deployment record, deliberately.** That record is *observed* —
written from the running containers, because a process cannot see which image wraps it (ADR-0003).
This one is *declared*. Merging them would put two provenance classes in one artifact, **and the
weaker would inherit the stronger one's credibility.**

**Two fields are derived instead.** `aggregate_pool_size` is summed over the units that served the
run rather than computed from one unit's `/meta`. `replica_count` equals the observed unit count
**for this topology only**, because a load balancer makes targets and replicas differ and no HTTP
client can see that, so a fan-out declaration may override it.

**The checks moved before the load**, so a malformed manifest costs zero experiment time rather
than failing certification after the ladder has been driven; a multi-unit run whose units cannot
all be read now refuses before load rather than warning to stderr and failing late. That refusal is
**not** keyed on `-require`: keying a gate on the flag that sets the exit-code floor is exactly how
the deployment-preflight bypass was reopened in PR3b and had to be closed again, because
`-require none` then disables the check rather than the certification level.

### 2.8 Clock synchronisation is a preflight check and retained environment evidence

The measured window comes from the generator's clock and the host series from each host's own, so
alignment decides which samples belong to a rung. The preflight asserts **healthy synchronisation**
rather than merely that the daemon is installed, and the observed offset is retained with the
environment evidence so a later reader can tell whether a window edge is trustworthy.

### 2.9 `G1` uses a sharded one-authority placement, never `domain.Unsharded`

`Unsharded` makes placement enforcement inert and reports `routing_version: "unsharded"`, so a `G1`
built that way would run a different code path from `G2`/`G4` and carry provenance that cannot be
compared with theirs. `G1`'s placement document names one authority owning all four organisations,
so routing stays active and only the topology changes. It records `placement_assignment` even
though a single-unit run is not asked for one, since `G1` would otherwise be the only capacity
point whose artifact cannot say what it served.

### 2.10 Reconciliation needs the authorities reachable from the verifier

`alloca-verify` connects directly to each authority's PostgreSQL, so network rules, DSN handling
and the verifier's own host placement are PR4a's work rather than PR4b's discovery. **A capacity
point that cannot be reconciled is not a capacity point.**

### 2.12 Capacity units are non-burstable, and the generator is larger than a unit

The vCPU budget is the maintainer's; the shape within it is implementation
(`deployment-architecture.md` §13.3). Against a 12-vCPU quota: four 2-vCPU capacity units and one
4-vCPU generator/monitoring host.

**No burstable instance may serve a measured run.** A credit-based instance sustains a fraction of
its vCPUs once credits drain, so a ladder bursts early and throttles later and the same rung
measures different capacity depending on how long that host had been up. `G1`, `G2` and `G4` run
different numbers of hosts with different credit histories, so the throttling lands unevenly across
exactly the comparison `E2` and `E4` are derived from — **and it would read as sub-linear scaling.**
The generator is deliberately not the shape of a capacity unit: it is the one host whose saturation
would invalidate a point rather than describe one.

**What 2-vCPU units mean for the result, recorded before the runs rather than after.** A service
and a PostgreSQL authority sharing two vCPUs make host CPU the likely limiting mechanism rather
than PostgreSQL's own frontier, so the result characterises composition of small capacity units and
its comparability with PR2's larger-envelope frontier is weak. A limitation, not a discovery.

### 2.14 PR4a rehearses the 12-vCPU shape locally before any AWS capacity attempt

**Maintainer decision, 2026-08-13:** prove the Iteration C machinery on the workstation's larger
resource envelope before spending metered time. The rehearsal partitions **12 of 16 CPUs** into
non-overlapping scheduler-visible sets:

```text
capacity unit A    CPUs 0-1     service A + PostgreSQL A
capacity unit B    CPUs 2-3     service B + PostgreSQL B
capacity unit C    CPUs 4-5     service C + PostgreSQL C
capacity unit D    CPUs 6-7     service D + PostgreSQL D
generator/monitor  CPUs 8-11    load generator + monitoring
(headroom)         CPUs 12-15   held idle; the generator control's only spare capacity
```

`G1` uses unit A, `G2` uses A+B, `G4` uses all four; unused sets stay idle. Docker **cpusets**, not
CPU-time quotas, are the mechanism, because the point is to stop shard groups sharing
scheduler-visible CPUs. Service and PostgreSQL within a group share their two CPUs on purpose,
matching the planned capacity-unit contention shape. Four CPUs stay outside so unpinned host work
has somewhere to go, and because they are the headroom the **generator-headroom control** widens
into — the cpuset analogue of `VAL-NEG-2`'s `GOMAXPROCS` control, and the only way a closed-loop
harness can show it was not measuring itself.

**The envelope change lands on comparability, not on this rehearsal.** PR2's frontier was measured
against a 10-vCPU envelope and this machine is now 16, so **any future local re-measurement is a
different environment from PR2's**. Treat the two as separate environments, not a before and after.

**Pinning the units is only half of the partition, and the measuring side is more than the
generator.** One cpuset variable names the set for generator, Prometheus and Grafana, and each
reached it by a different mechanism — every one a way to lose the partition silently. The generator
is a host process, so the scheduler places it on the units' CPUs unless `taskset` confines it,
which also fixes its `GOMAXPROCS` because Go reads the affinity mask at startup. Monitoring is a
separate Compose stack raised with no cpuset at all, and Prometheus is not idle — it scrapes every
unit and compacts its TSDB from inside the capacity units' own CPUs; **that load grows with the
number of units, so it biases `G4` harder than `G1`**, against exactly the comparison `E2` and `E4`
are derived from. And widening the generator set for the control must widen monitoring with it,
because moving only the generator would change two things at once.

**A partition that is legal is not a partition that was applied**, and nothing downstream can tell
the difference: an unpinned run addresses the right units, passes every routing check, and
certifies cleanly while carrying contention no artifact records. `itc-cpuset-check.sh` reads the
cpuset off each running container and refuses a run whose pinning disagrees, retaining its output
beside the run. A container whose cpuset cannot be *read* is reported as unverified rather than
absent, since treating an unreadable answer as a skip would make the check the thing it exists to
catch.

**The declaration deliberately does not name the generator's CPUs.** `environment` is one string
reused across `G1`, `G2`, `G4` and both sides of the headroom control, so a static set would be
false for half the runs it describes; the effective set is retained per run.

The rehearsal does **not** create independent hosts. All groups share the workstation, kernel,
storage path and caches, so local Goodput and apparent scale efficiency are rehearsal evidence
only, on the terms in §1.

## 3. Findings that changed the implementation or method

Each of these changed something already designed, and every one was found by running something.

### 3.1 The workload's demand ordering has to be the workload's own property

`WL-MUT-DISP-4` assigns demand round-robin over four organisations, so the order of that list
decides which organisation each request addresses. Deriving it by flattening the organisations each
authority owns makes the order a function of the topology whenever an authority does not own an
alphabetically contiguous run of them — `G1` would then be compared against a `G2` that had quietly
permuted the demand mapping, and **the difference would present as scale efficiency**. The shipped
PR3b map is exactly that shape, so this was not hypothetical. The ordering is now sorted by
organisation identifier, and the topology-invariance test uses a deliberately non-alphabetical
`G2`: an alphabetical one passes whether or not the property holds.

### 3.2 A discriminating test can be defeated by the arithmetic of its own fixture

The assertion that each organisation walks its whole seeded dataset passed against a deliberately
broken workload. Indexing slots by the global sequence number rather than the organisation's own
counter strides four at a time through the population, and whether that loses coverage depends on
`gcd(4, slots)`: at the five-slot fixture the assertion was written with, the stride still visited
all five; at eight it reaches a quarter of them. **A coverage assertion over a cyclic index is only
as strong as the relationship between the cycle lengths**, and nothing about reading the test
reveals which case it is in. Running mutations against the workload is the only reason it was
found.

### 3.3 `-reset` truncates slots, so the clean start is per authority and not per organisation

`Repo.Truncate` includes `slots`, so seeding four organisations each with `-reset` leaves only the
last one's fixture standing. The consequence is asymmetric across the matrix, which is what makes
it dangerous rather than merely wrong: at `G4` every organisation has its own authority and nothing
is lost, while at `G1` all four share one database and three datasets disappear. **A `G1` measured
that way would be depressed relative to `G4` in the same direction and of the same rough shape as a
genuine super-linear scaling result.**

### 3.4 `GROUPS` cannot be a shell variable name

`GROUPS` is a bash built-in array holding the caller's group IDs, and bash discards an assignment
to it without error. Make expands it itself and is unaffected, **so the Makefile would have worked
while the script it documents silently did not**. The variable is `ITC_GROUPS` throughout. Kept as
a naming hazard rather than a fixed bug: the next shell-facing knob wanting an obvious short name
can hit the same collision, and `shellcheck` is what caught it.

### 3.5 Run identity has to be structural, because a fully replayed run certifies at `capacity`

Two runs identical except whether the fixture had been re-seeded between them: the clean run
produced 400 goodput and 0 replays; the second produced **0 goodput and 400 replays**, and both
reported `measurement_sound: true` and certified at `capacity`. Every mutation in the second was
served from an idempotency record written by the run before it, because workloads derived keys from
workload name and sequence number with no per-run nonce. Seeding asserted against this; nothing
asserted against it at **run** time. Zero goodput is obvious by eye — **the dangerous shape is
partial**, where a fixture reset on some points and not others yields a plausible depressed number
that `E2`/`E4` carry as scale efficiency.

**Resolved by maintainer decision: fresh-run identity is structural, not procedural.** Idempotency
keys are scoped to a run id minted per invocation, and capacity certification refuses a run
reporting replays its workload did not intend. **The fix works, and not by the route the numbers
suggest**: re-running without re-seeding now produces no replays at all — the second run mints
entirely different keys — and instead produces genuine `schedule_conflict` refusals against a spent
fixture. The gain is legibility, because the old totals read `admitted_success, replay: true`,
which looks like successful bookings until someone notices goodput excludes replays. The
certification gate is therefore a backstop that never fires in the motivating scenario, covering
the narrower case where run identity fails to vary.

**The generalisation is the part worth carrying forward.** An all-refusal run against an exhausted
fixture still certifies at `capacity`, and that stays correct: the ladder describes how well a run
accounts for itself, and this one does so honestly. What it lacks is useful demand, an evidence
question — so the ladder is unchanged and the rule lands in `measurement-contract.md` §5 as a
useful-demand gate. **Partial exhaustion invalidates a capacity point too, and it invalidates the
rung *below* it**: the saturation rule selects a point by showing a higher rung produced no more
Goodput, so if that rung was short of fresh mutations rather than short of service, the point
beneath it was never established either. The population must be sized for the *deepest* rung the
ladder reaches.

### 3.6 One uncommitted file anywhere in the tree makes every run uncertifiable

`go build` decides `vcs.modified` from `git status --porcelain`, which lists **untracked** files as
well as modified ones. `Manifest.Validate` refuses `service_source_modified` at the floor of the
ladder, so the consequence is not a downgrade: **the run certifies at `none` and backs nothing at
all**, however sound the measurement was. Observed as a `G2` run certifying at `none` while every
*tracked* file was committed — the cause was an uncommitted overlay file added earlier that
session. The existing controls each cover a different property and none covers this one; the
build-context check asserts only that `.dockerignore` excludes no *tracked* file. What answers it
in one step is **`make image-provenance`**, which builds the image, extracts the binary and fails
when a clean checkout produces `modified=true`. Run it before any metered evidence run; the cheap
symptom is the `-dirty` suffix on the image tag, and finding that out on metered infrastructure
costs the rung.

### 3.7 The layout check described more properties than it enforced

`itc-cpu-layout.sh` exists to fail a bad CPU partition before an image build and four database
containers, and its comments named the properties it existed to protect. Writing tests for it found
it checked two — non-overlapping sets, and the partition fitting the machine — while four other
cases passed: a generator no larger than a capacity unit (§2.12's rule appeared in the script's own
failure text with nothing evaluating it); unequal capacity units, which make `E2`/`E4` carry an
imbalance rather than the architecture; an unrecognised group count, which checked a two-unit
partition and printed "G3" so the operator read a pass for a layout nothing had examined; and a
malformed CPU spec that **exited zero**, because the expansion ran inside a process substitution
whose exit status is not the enclosing command's, so an unparseable spec printed its complaint,
yielded an empty set, and let the script report the partition as valid.

**The common shape is worth more than the four bugs: the script's prose was accurate and its code
did not implement it.** Every one of these properties was written down, in the file, next to code
that did not check it — the failure mode a comment cannot catch and a reader is least likely to,
**because the explanation reads as evidence that the check exists.**

All four are now enforced, every violation reported rather than only the first, and the gate is a
shell test wired into CI and `make ci` (`project-structure.md` §1 owns the rule). The machine's
apparent CPU count is controlled with `taskset` rather than an environment override, because
`nproc` reports the CPUs available to the calling process so an affinity mask presents a genuinely
smaller machine — **an override seam would have been a documented way to tell the script the
machine is bigger than it is.** Each check was then removed in turn and the case claiming to gate
it required to fail: seven controlled mutations, all detected.

### 3.8 The topology and the observability stack could never have run together

Raising both stacks failed outright on a port already allocated: the first service unit published
its metrics on the host port Prometheus publishes, and units 2–4 collided the same way. **It had
been latent since PR3b, because the two stacks had never been raised together** — PR2 measured a
host-run service with no overlap, PR3b containerised the topology into that port range, and PR3c
scraped those ports directly without ever starting Prometheus. Iteration C is the first
configuration needing both. The clearest symptom that this was a real ambiguity rather than a
coincidence: the same port number already meant two different things in two different documents.

Resolved by moving the topology's metrics ports to mirror the HTTP ports, so unit *n* serves on
`808n` and publishes metrics on `908n`. This is a **breaking change to a documented address**,
reversible through per-unit overrides, which is why the numbers moved to a pattern rather than to
arbitrary free ports. Deliberately not guarded by a check: Docker refuses a duplicate binding
loudly and names the port, so a guard would only convert a clear runtime failure into a slightly
earlier one.

### 3.9 The first driven cell was not observed, and nothing said so

The first `G4` rehearsal cell ran to completion, reconciled and certified at `capacity`. Grafana
was empty and Prometheus reported no error: it was healthy and scraping a target written days
earlier, the address of a host-run service from PR2's single-instance path that nothing serves now.

**Nothing downstream could report it.** A query against a job whose targets are all down returns an
empty result and no error, so every panel renders empty, every CSV exports with headers and no
rows, and the run's own artifacts are unaffected. **The cell was sound and quotable at its
provenance level while retaining no time series whatsoever** — the worst shape there is, because
nothing anywhere reports an error.

**Resolved by making the scrape path structural rather than discovered.** Prometheus joins the
topology's network and scrapes units by service name, so the addresses stop being a property of
this machine's networking. `file_sd` remains the abstraction rather than static targets
(**maintainer decision, 2026-08-13**), so the scrape configuration stays version-controlled and
deployment-agnostic and only the generated file changes between environments. Each target carries
an `authority` label, the stable per-unit identity — the address is not, so a panel keyed on
`instance` would not survive the move to the environment the experiment exists for (§2.6).

**The gate is a count, not a probe.** A cell is refused unless exactly `ITC_GROUPS` targets report
`up == 1`. "Prometheus is scraping something" passes while three of four units are missing, and **a
`G4` point measured with one unit unobserved is not a `G4` point — it is the rung below it wearing
the wrong label.** A rehearsal may still run unmonitored, but only by saying so.

### 3.10 The first complete G4 rehearsal certified correctly and consumed its entire fixture

The first full `G4` cell certified at `capacity` and admitted **exactly 16,000 mutations against a
16,000-unit fixture**, then refused 184,693 requests with `no_capacity`. The fixture was spent in
roughly the first five seconds; the remaining ~55 seconds measured how fast the service can
decline, which is a correct answer and a sound measurement, and is not throughput the service could
have delivered. **This is §3.5's useful-demand gate demonstrated on a workstation instead of on
metered infrastructure** — everything behaved as specified, and the point is that **nothing
external would have told us either.**

**Decisions taken (maintainer, 2026-08-13).** The rerun uses a fixture sized to hold even if every
request admits, and that size is explicitly **not** the PR4b fixture size, which is derived from the
deepest rung the ladder reaches and held identical across topologies. Reset and reseed become
explicit before every measured rung, owned by the runner rather than the operator — "reseed between
rungs" is exactly the step a twelve-cell ladder drops once, silently, after which every later point
is wrong. The generator-headroom control is deferred until there is a high-useful-demand `G4` point
to run it against; against this cell it would have proved nothing. The runner now reports the
discriminator after each cell — admitted against supply — and reports rather than refuses, because
useful demand is an evidence gate and not a provenance one.

### 3.11 A single reported rate does not describe a 60 s window

The first cell with useful demand throughout reported 110,172 mutations over 60.036 s — **1,835
mutations/s**, 100% `admitted_success`. **The strongest thing in the run is the agreement, not the
number**: the bracketing scrapes give a server-side delta per unit of 27,543 admitted, identical
across all four, summing exactly to the client-reported total. Two independent accountings agree,
and the four units are balanced to the request — which is what a round-robin over four
organisations, one homed per authority, must produce if placement and routing are correct.

**The window is not stationary, and 1,835/s is an average across a decay**: the scraped rate peaked
near 3,200/s and fell monotonically to about 1,300/s, roughly 2.5× within a single cell.

> **Those within-window figures are not re-derivable and must not be quoted.** This cell predates
> per-cell series retention, so the decay was read live from Grafana and is beyond its retention
> window; it motivated §3.12 but is not evidence. The endpoint totals are unaffected.

> **Hypothesis withdrawn.** This section originally proposed that cost per mutation rises with
> accumulated state, via an exclusion index growing in the measured write path. §3.12 refutes it: a
> 30 s cell reached *more* rows than a 60 s cell while still accelerating. Kept visible because the
> mechanism is real and it is the explanation someone will reach for again.

### 3.11.1 Two rungs cannot be compared while both are averages over non-stationary windows

A second rung at doubled concurrency bought 17.4% more Goodput while the tail grew two to three
times and p50 *fell* — the signature of queueing rather than of added capacity. **The pair does not
support a saturation argument**, because two rungs can differ by tens of percent while measuring
the same service over different portions of the same decay curve, and a saturation argument selects
an operating point precisely by claiming a higher rung produced no more *sustained* Goodput.
"Sustained" is the word doing the work, and it is the property these windows have not been shown to
have.

**Concurrency at the aggregate pool size is a boundary worth stating before the ladder is designed
rather than discovered inside it.** With as many workers as pooled connections, every worker can
hold a connection and the pool is exactly not a constraint; the next rung begins queueing. PR2
found the pool to be the frontier on a single instance, so a ladder stopping at or below this
boundary would establish nothing about where this topology's frontier is.

### 3.12 The window experiment answered a different question, and reproduced PR2's open anomaly

Three cells identical but for window length, intended to test whether reported Goodput depends on
how long a rung runs. All are retained at
[`pr4a-rehearsal/windows/`](../../measurements/pr4a-rehearsal/windows/), whose README carries the
evidence class.

**Those artifacts carry a wrong `topology` label — read it as `itc-g4`.** The scrape config set the
label job-wide at the PR2 value, so every Iteration C sample inherited PR2's identity, **and
nothing reported it because the label was present and well-formed**. The figures are unaffected,
but a snapshot separated from its directory would misdescribe itself. These cells are annotated
rather than re-run, because the regime they captured cannot be reproduced on demand.

**The 120 s cell is invalid and excluded**: it admitted its whole supply, so its rate is precisely
`supply ÷ duration` and describes the fixture rather than the service. It is still
`measurement_sound` — the accounting reconciles, **which is a different property from having
measured the service.**

**The remaining two were not the same experiment:**

| | 30 s cell | 60 s cell |
|---|---|---|
| Goodput | 2,874 → **3,208/s**, rising | 2,194 → **927/s**, falling |
| Process CPU | 1.68 → **2.11 cores**, rising | 1.46 → **0.70 cores**, falling |
| Pool acquire wait | 2.72 → **1.51**, falling | 3.95 → **7.12**, rising |
| Admitted | 92,125 | 83,794 |

One run accelerated throughout; the other degraded from its first exported sample and **admitted 9%
fewer mutations in twice the time**. Window length is confounded with which regime a run lands in,
so no rung comparison — and so no ladder — is possible until the two are separable. Throughput and
CPU fall together, so the service is doing less work while the request path slows: the degraded
cell must not be treated as an outlier and dropped.

**The pool series is the sharpest evidence, and its first interpretation was too strong.** Acquired
connections fall to 7 while acquire-wait nearly doubles, against a configured maximum of 16. **A
maximum is a ceiling, not evidence that sixteen connections currently exist**, so this does not
prove that nine are being withheld. What it establishes is narrower: workers wait to acquire while
fewer connections are acquired, making connection acquisition a concrete part of the diagnosis
rather than a generic "service saturation" story.

It matches PR2's open throughput anomaly on throughput ratio and service CPU, and **contradicts** it
on pool acquire-wait, unchanged in PR2 and nearly doubled here.

> **The generator row is withdrawn.** This originally counted the generator's CPU halving as a third
> matching element and called it the most distinctive marker of the set. It is not a marker at all:
> the generator is **closed-loop**, so when the service path slows, workers spend longer blocked in
> I/O and generator CPU tracks throughput *mechanically*, in a perfectly healthy generator. Kept
> visible because the reasoning was seductive — two independent-looking processes falling by the
> same factor reads like a common cause, and the coupling is invisible unless you read the
> generator's own loop.

**What the rehearsal adds that PR2 could not — stated narrowly.** In PR2 the generator and service
shared one unpartitioned host, so "both slowed together" was compatible with them contending for
the same CPUs. Here they hold verified disjoint cpusets, so **exactly one thing is excluded: direct
competition for the same logical CPUs.** An earlier draft claimed more. Disjoint cpusets do not
exclude a shared kernel, a shared storage path, shared physical cores or SMT siblings, or host-side
contention above the VM, so PR2's host-level candidates are neither confirmed nor excluded — which
is why the host sensor became load-bearing rather than a refinement. The anomaly is also **not
specific to the single-instance deployment**: it appears across four independently pinned units.

### 3.13 Maintainer decision: qualify the degraded regime in PR4a before AWS

**Decision, 2026-08-14:** §3.12 is a PR4a blocker, not PR4b exploratory work. Two nominally
comparable cells landed in materially different regimes, and until that distinction is understood
or made observable, a `G1/G2/G4` matrix could compare different regimes and report the difference
as scale efficiency. **Moving the same ambiguity to independent hosts would make the environment
more expensive without making the result more interpretable.**

The regime must meet one of two bounded outcomes before PR4b: **(1)** a root cause demonstrated and
fixed, or **(2)** a reliable discriminator or admission rule making it detectable and excludable.
The second is deliberately acceptable — PR4a is not required to explain every performance
characteristic of the host platform, only to remove this ambiguity from the **method PR4b will
use**. The diagnostic order is bounded too: host sensor and richer pool state first, then decide
**from evidence** whether PostgreSQL-side instrumentation is necessary.

This is not a new Iteration C Problem. It is evidence from implementation exposing a blocker to
answering the existing Problem reliably, which is exactly when the schedule is expected to follow
the evidence rather than preserve a stale PR boundary.

### 3.13.1 The retained cells already answered the pool question, and narrowed it sharply

§3.13 asked for fuller pool population evidence as new instrumentation. **Two thirds of it was
already retained**: total and idle connection series sat in both cells' TSDB snapshots and had
never been exported to `panels/`. This is the snapshot doing exactly what it is kept for.

| | 30 s cell (healthy) | 60 s cell (degraded) |
|---|---|---|
| `total` (population) | **16, flat** | **16, flat** |
| `idle` | 16 → 4 → **0** | 16 → 3 → **9** |
| `acquired` | 0 → 12 → **16** | 0 → 13 → **7** |
| **mean acquire duration** | 0.53 → **0.47 ms** | 0.56 → **7.67 ms** |

**The population never fell**, and `total == idle + acquired` holds on every sample, so the
alternative §3.13 correctly insisted on — that the population had fallen, or was constructing — **is
refuted for this cell** without a repeat run. **And the degraded pool was not saturated**: the
healthy cell pins `idle` at 0 with `acquired` at the full 16, while the degraded cell holds 6–9
idle. Whatever is limiting it, it is not pool capacity. Mean acquire duration rose 16× while
connections sat idle and available.

**What that does not establish, and why the number cannot say.** `AcquireDuration` times the whole
`Acquire()` call, not blocked-waiting: the clock starts on entry and the elapsed time is added on
*every* successful path, including the one that constructs a brand-new connection. So 7.67 ms is
consistent with three mechanisms the retained series cannot separate — connection churn paying full
setup cost, contention inside the pool's own mutex, or wall-clock inflation from host-level
descheduling. The third is already weakened from inside this data: request p50 rose only 1.17×
across the same window in which acquire rose 16×, and uniform inflation would move both alike.

> **`EmptyAcquireWaitTime` is not blocked-waiting time either, and an earlier draft said it was.**
> Verified against the pinned `puddle` release: it accumulates on acquires that found no idle
> resource, covering *both* waiting for a release and constructing a new connection, because the
> clock is read after the constructor returns. **No single series in this set is a clean contention
> signal.**

**Localisation, stated at its actual strength.** The symptom is localised to the
**connection-acquire path**, a real narrowing from "something in the request path". It does **not**
place the fault inside the service and exclude PostgreSQL: the pool's constructor calls
`pgx.ConnectConfig`, so a construction event includes PostgreSQL and network connection
establishment. An earlier draft claimed the acquire path "is inside the service"; that is wrong
wherever construction is involved — precisely the case new-connection counting detects.

**Shipped.** Seven further `pgxpool.Stat` methods are exported and retained per cell, and no metric
decides it alone: empty-wait up with new-conns up implicates construction and churn, empty-wait up
with new-conns flat means acquires waited for a release, and acquire-duration up with empty-wait
flat puts the delay elsewhere. The dashboard separates occupancy from lifecycle from acquire cost,
because one graph carrying all three answered none of them.
`TestPoolCollectorDescribesEveryMetricItCollects` and its `Collect` counterpart pin the exported set
by name, **because a missing series here is invisible** — the scrape still succeeds and the
populated-series gate still passes for the metrics that *are* present. Both were proven
discriminating by removing a metric from each half.

**Two naming traps are kept rather than fixed.** Neither `alloca_db_pool_acquire_wait_seconds_total`
nor `alloca_db_pool_empty_acquire_wait_seconds_total` measures waiting, and both names say they do.
Renaming would break comparison against evidence already taken — the same reason §3.12's cells keep
their wrong topology label — so each help text states what it actually measures, the panel notes
carry the same warning, and `TestNeitherTimingMetricClaimsToBePureWaiting` fails if either reverts.
**A name that has already misled one reading will mislead another; the disclaimer travels with the
metric rather than living only here.**

**Sequencing.** The host sensor goes in **before** the next attempt to reproduce the regime. The
regime is rare, and a cell capturing pool evidence without host evidence would leave the same
ambiguity standing one run later.

### 3.14 `node_exporter` is in, and the local host cannot serve the instrument §2.3 named

Built as §2.1 specified: one exporter beside Prometheus, restricted collector set, its own 5 s job
(§2.3), host panels, and a per-cell gate refusing a run that retained no host samples (§2.5).
**One exporter, because the local rehearsal has one host** — a second would report the same kernel
twice. That the set is singular *is* the rehearsal's limitation, and the host job's label says so.

**PSI is not available on this kernel, and PSI was the instrument the diagnosis wanted.** §2.3
named `pressure` because it reports *contention* rather than consumption — a host can be far from
saturated and still be stalling. This kernel has no `/proc/pressure`: the collector loads and
reports its own failure while every other collector succeeds. It stays enabled, because it costs
one series, reports its own failure, and other kernels carry PSI. **`node_load1` is the
substitute**: run-queue depth rises when work is *waiting* rather than when work is being done, but
a one-minute average cannot resolve a stall inside a 30 s cell, so it bounds the question rather
than answering it.

**Superseded in part by §3.15**: the regime did recur, and these host panels are what excluded the
host — across degraded cells host CPU, run queue and steal stay flat while service CPU and iowait
both fall. The sensor did not name the cause; it removed a class of them.

### 3.15 The regime reproduces at G1, and both named mechanisms are refuted

Driving repeat series from a cold machine became one command, making two previously-prose couplings
mechanical: monitoring must be raised after the topology, and the generator cpuset has to reach
both the monitoring overlay and the run, **or half the measuring side lands on the units' own CPUs
while every cpuset, topology and certification check still passes.** Building it exposed a defect
hidden by build times — the wrapper handed off a second after starting Prometheus and the cell was
correctly refused by §3.9's gate; it had passed before only because the builds were cold. The
wrapper now waits for the observable condition rather than sleeping a fixed time.

**The regime reproduces at `G1`, so it does not require cross-group contention.** A `G1` series
degraded 2 cells in 10 and a second 1 in 4. At `G1` the service still shares two CPUs with its own
PostgreSQL while the other three pairs are gone, so **the fault lives inside a single
service+database pair.** The dip signature: Goodput falls ~5×, service CPU 4× and iowait 4×, while
host CPU busy, run queue and steal stay flat. Since `host_cpu_busy` *includes* iowait, host CPU
flat while the service gives up 0.33 cores and iowait falls means something non-service absorbed
roughly 0.26 cores without blocking on I/O. **The excursion begins before the measured phase
opens**: the first sample is already half the healthy rate.

**Checkpoints are refuted** — not necessary, since a degraded cell ran with none active; not
sufficient, since healthy cells ran inside them. **Autovacuum is refuted, and this needed an
instrument to say so.** PostgreSQL defaults `log_autovacuum_min_duration` to 10 minutes so an
ordinary autovacuum leaves no trace, while `log_checkpoints` defaults to on. **That asymmetry is
why checkpoints could be refuted from retained logs twice while autovacuum could be neither
confirmed nor refuted: the evidence for one existed and the evidence for the other never did.**
With the setting at 0 and read back from `pg_settings` to prove it took, autovacuum fires 5–7 times
inside every measured window of every cell while all ten stayed flat.

**It then stopped reproducing**: thirty consecutive cells at zero, two series at the exact
configuration that produced 2/10 that morning, host baselines indistinguishable. Outcome 1 receded
and outcome 2 now depends on a base rate that is itself unstable. A caution the sequence earned: a
phase knob was varied and the result reported as a refutation before it was noticed that the
phenomenon had already left. **A knob that moves two things at once cannot refute anything, and a
negative result during a quiet period is not a negative result.**

### 3.16 `postgres_exporter` is in, and its default collector set would have blinded it

§3.13's condition is discharged rather than waived: host and pool evidence exclude the host
(§3.15), the two named database mechanisms are refuted, and what remains is a database holding each
connection about five times longer for no reason any deployed instrument can see. What the exporter
adds that nothing else does is `wait_event_type` and `wait_event` — what a backend is blocked *on*
rather than that it is slow.

**The default collector set would have made the exporter useless exactly when it matters.** Holding
an `ACCESS EXCLUSIVE` lock makes a scrape with the `stat_user_tables` collector hang indefinitely,
so Prometheus marks the target down and retains nothing: **the instrument would go blind in
precisely the situation it was added to observe.** Measured here — 1.46 s per scrape unlocked,
hanging under a lock, 0.014 s under the identical lock with the collector disabled. It is off,
which costs the per-table vacuum counters; the autovacuum question those would have answered is
already settled by the server log, **which a lock cannot block**.

**One exporter per authority, pinned to the generator set, with its own target directory.** Pinned
because an exporter queries the database it observes, so unpinned it spends a capacity unit's CPU
observing that same unit — the one instrument whose collection lands on the measured thing, and the
reason its collector set is cut narrowly where the host exporter's is deliberately wide. Targets
come from `ITC_GROUPS` in the same script as the unit targets, so the database and service views of
a rung cannot disagree, and they go in a *separate* directory because the service job discovers
targets as a glob — a postgres target beside them would be scraped as a service unit and the scrape
gate would refuse every cell over a foreign entry it was correct to report.

**The whole section is plotted, a departure from "panel narrowly" — maintainer decision,
2026-08-17.** The dashboard graph bound went to 16 first, for the wait-event graph alone, on
§3.14's reading that the snapshot makes an unplotted series recoverable; it is now 20, the
remaining four plotted rather than left recoverable-in-principle. With checkpoints and autovacuum
both refuted and no candidate left, the next degraded cell has to be read *without* a hypothesis to
test, and §3.13.1's precedent says an unplotted series is recoverable, not that it is noticed.
**Recovery works when you know what to look for; this is the case where nobody does.** The bound
stays a bound, and the graph after these is another decision recorded in the test that enforces it.

The series wrapper now waits for both jobs before driving cell 1: the exporters are discovered on
the same refresh, so they are *usually* healthy at the same moment — **and usually is not a
property.** The populated-series gate requires the host panels and says nothing about the database
ones, so an exporter still starting when the first cell opens yields empty PostgreSQL panels and
nothing reports it — §3.9's failure shape, on the evidence path added to answer the question the
series exists for. Whether that gate should require the database panels is a measurement-contract
decision and is not settled here.

### 3.17 The regime's discriminator is logical buffer work, and the planner-state treatments are diagnostic

**The untreated baseline** is a 60-cell `G1` series: **10 degraded in 60**. It is the population
every treated series is compared against and must not be re-run with a treatment applied.

**The discriminator is buffer accesses per request, and the separation is total.** Healthy cells sit
at 81–88 (mean 86); degraded cells at 612–872 (mean 757), with no overlap. Cache hit ratio is
1.0000 both ways and physical reads are ~0.17/s both ways, so **no disk is involved at any point**.
Within a degraded cell logical block hits stay pinned near ~500k while buffers-per-request climbs
monotonically. The database is saturated at a roughly constant logical buffer rate, and throughput
is that rate divided by work-per-request.

**Refuted with direct evidence, not inference:** lock waits are zero in every cell; LWLock and IO
are *lower* when degraded; no checkpoint step falls in any degraded cell; the longest open
transaction is ~0.02 s everywhere. **Backends are running, not waiting**, which makes this a
work-volume problem rather than a contention one.

**What ANALYZE after the reseed can and cannot reach**, measured against a live authority:

| stage | `reltuples` | `relpages` | `pg_statistic` rows |
|---|---|---|---|
| after `TRUNCATE` | **-1** (unknown) | 0 | **survive** |
| after `ANALYZE` on the emptied tables | **0** | 0 | **still survive** |

ANALYZE at reseed repopulates row counts only for what is populated at that instant, which is the
slot fixture alone; the four tables the workload grows are empty when the reseed finishes, so it
pins them at zero rows while leaving column distributions describing the *previous* cell's data in
place. **That sharpens the experiment rather than invalidating it** — a null result would clear the
slot statistics as the cause and point at the tables that cannot be usefully analysed until they
have filled. Both treatments therefore ship as switches, off by default, non-canonical, recorded
per series and excluded from any canonical measurement, since each costs measurable overhead on the
database under test.

**The extension is verified by reading the view, not by creating it.** Without the preload,
`CREATE EXTENSION` succeeds and returns 0 while every subsequent query against the view fails — one
cell into the series.

**Pre-committed success criterion for §3.13 outcome 1**, agreed before the treated run: 0 of 60
cells degraded, **and** buffers-per-request staying in the healthy cluster, **and** the suspect
statement's estimates moving in the predicted direction. All three, not the first alone. Kept
separate on purpose: plan invalidation is the next controlled variant, not mixed into the first
test, because two treatments in one run cannot be attributed.

### 3.18 The treatment made it worse, which is what confirmed the mechanism

The ANALYZE treatment produced **3 degraded in 5** against the untreated 10 in 60. The §3.17
criterion is **not met** — and the result is the strongest evidence yet, because the treatment moved
the regime in the direction the mechanism predicts rather than leaving it unchanged.

**The extra work is attributable to a class of statement, not to "PostgreSQL".** Every statement
that finds a row by key gets 6–15× more expensive (the idempotency lookup worst, 11.8 → 182.0
buffers per call) while **every statement that only inserts is unchanged** at ≈1.0×. That is an
access-path change, and throughput is a monotone function of that lookup cost across the whole
series, from 11.8 buffers/call at 1293/s to 182.0 at 625/s. A `SELECT DISTINCT` was the first
suspect and is **not** the cause: it costs ~34,000 buffers per call but runs 12 times a cell, and
its total *falls* when degraded because it scales with throughput.

**Why ANALYZE at reseed makes it worse, stated precisely.** Untreated, `reltuples` of `-1` means
*unknown* and the planner estimates from the relation's current physical size, so it partly
self-corrects as the table fills. The treatment replaces that with a **confident zero**, and a
sequential scan of a table the planner believes is empty always beats an index scan.

**The first cell of every series is a natural contrast — not a control.** Volume destruction means
cell-01 runs against tables never analysed; later cells carry the previous cell's distributions
paired with `reltuples=0`, and the three degraded cells all sat in that second state. It is a
contrast rather than a control because cell-01's caches, connections and relation files are fresh
too, and nothing in the design isolates the statistics term.

**Consequences.** ANALYZE at reseed is excluded, and no fixture treatment is chosen until the plan
is observed (§3.19). The autoanalyze scale factor is **not** the next lever, and the arithmetic says
so: the threshold is `analyze_threshold + analyze_scale_factor × reltuples`, so at `reltuples` 0 or
-1 the scale-factor term contributes nothing and the threshold is already just the base 50 rows.
Autoanalyze should therefore fire almost immediately as the tables fill — **which makes *why the
degraded state persists for a full window* the question, not how to trigger analysis sooner.** An
earlier revision claimed cached plans are not replanned when statistics change; that is wrong for
ordinary prepared statements, which PostgreSQL 16 invalidates and replans (§3.19). RI-trigger plans
are a separate mechanism and are not covered by that observation.

### 3.19 The plan is the missing fact, and offline probing already narrowed it

**Maintainer direction, 2026-08-17:** do not choose a fixture treatment until the execution plan is
observed. Offline probing with `EXPLAIN (GENERIC_PLAN)`, which plans a parameterised statement
without values and without executing it:

| planner state | plan |
|---|---|
| populated, analysed | Index Only Scan |
| after `TRUNCATE` only (`reltuples=-1`, *unknown*) | **Index Only Scan** |
| after `TRUNCATE` + `ANALYZE` (`reltuples=0`) | **Seq Scan**, cost 0.00..0.00 |
| 40,000 rows present, statistics never refreshed (`reltuples` still 0) | **Index Only Scan** |

**Why the ANALYZE treatment hurt, demonstrated rather than argued:** `-1` means *unknown* and the
planner still prefers the index; `0` means *confidently empty*, and a sequential scan of a
zero-page relation costs 0.00 and beats any index.

**But the bad plan does not survive the table filling.** A statement prepared while the relation was
empty replanned to an Index Only Scan once 40,000 rows were present, in the same session, with
`reltuples` still 0 and no ANALYZE. **A Seq Scan can therefore only hold for the very beginning of a
window, which cannot explain a monotonic sixty-second decay.** §3.18's access-path story stands as
a description of the measurement; its mechanism does not yet stand as an explanation.

**So two probes, answering different halves.** A plan probe samples what a *fresh* plan would be
every 5 s, so a flip is visible against the throughput series rather than inferred from its
endpoints. An auto-explain probe logs real executed statements at 0.1% **sampling** with their
actual plan and buffer counts — sampled rather than thresholded on purpose, because **a duration
threshold selects the slow executions and so cannot show what a healthy execution's plan was**,
which is precisely the comparison wanted. `shared_preload_libraries` is assembled once from
whichever diagnostics are requested: passing it twice does not merge, so asking for both would
otherwise load one and surface the other's absence as a runtime error a cell in.

### 3.20 Root cause: a Seq Scan plan cached against an empty table, and outliving the planner's own correction

Untreated reproducer with all three probes, 60 cells: **7 degraded in 60**, comparable to the
untreated 10 in 60, so the diagnostics neither suppress nor provoke the regime, and healthy rates
sit inside the untreated band — which is what makes the plan evidence admissible.

**The executed plans differ, and by two orders of magnitude.** From auto-explain at 0.1% sampling,
segmented by cell window, for the idempotency lookup:

| | samples | access path | mean ms | mean buffers | max buffers |
|---|---|---|---|---|---|
| healthy cells | 4,130 | Index Scan (100%) | 0.0117 | 2.81 | 3 |
| degraded cells | 139 | **Seq Scan** | 0.8026 | **314.66** | 693 |
| degraded cells | 44 | Index Scan | 0.0096 | 3.00 | 3 |

Roughly three-quarters Seq Scan at ~314 buffers and one quarter Index Scan at ~3 reconciles §3.18's
~182 per call, and §3.17's 8.8× separation follows from it. **The flip is the recovery**: throughput
decays monotonically for as long as the Seq Scan is in use and jumps the moment the Index Scan
appears. The `decay`-shaped cells are those where the flip never happened inside the window.

**It is a cached plan, not a planner choice, and the probe pair proves it.** The plan probe reports
what a *fresh* plan would be; auto-explain reports what the connections actually ran:

| window offset | live rows | planner would choose | executions actually used |
|---|---|---|---|
| 0 s | 3 | Seq Scan | Seq Scan |
| 5 s | 4,912 | **Index Scan** | Seq Scan |
| 5–51 s | up to 17,852 | **Index Scan** | **Seq Scan** |
| 52 s onward | — | Index Scan | Index Scan |

**The planner corrected itself within five seconds of the window opening, and the pooled connections
went on executing the stale plan for another forty-six.** Neither probe alone could have shown this:
the planner-state probe would have said the plan was fine, and auto-explain alone could not have
distinguished a stale plan from a planner that kept choosing badly.

**The mechanism, end to end.** The reseed truncates the four mutation tables and repopulates only
the slot fixture, so a measured window opens with the idempotency table empty. The pooled
connections prepare the lookup against that empty relation and PostgreSQL caches a Seq Scan.
**Table growth alone does not invalidate a cached plan — only a relcache or statistics invalidation
does** — so every execution on that connection keeps scanning, and the scan's cost grows with the
table. When an invalidation finally arrives the plan is replaced and throughput snaps back.

**Status against §3.13.** Outcome 1's root cause is demonstrated. The *fix* is not: plans cannot
simply be made later, because the mutation tables are empty at window start **by design — they are
the workload's own output**. The candidates are a warm-up that populates the tables followed by an
invalidation before the measured phase, recycling the pool after that warm-up, or accepting the
regime and excluding it by the now-reliable discriminator. The first two change what the fixture
measures and are maintainer decisions, not implementation ones.

**This is a fixture artefact, not an `alloca-go` defect.** A deployment does not truncate its tables
and then immediately serve peak load against them. Nothing here is evidence about the service's
capacity.

### 3.21 The sustained runs decline smoothly, and it is not the cached-plan regime

The 600 s qualification runs both fall monotonically for roughly the first 300 s and then plateau:

| | slice 1 | slice 10 | ratio | spread |
|---|---:|---:|---:|---:|
| G1 | 1,200.8/s | 883.4/s | 0.74x | 1.41x |
| G4 | 3,442.2/s | 2,612.9/s | 0.76x | 1.32x |

**It is not §3.20's regime**, on three independent counts: that signature was ~112× more logical
buffer work per call, a timescale of tens of seconds, and a sharp recovery on invalidation. These
runs show buffer work per request rising about 3.7× at `G1` and 2.6× at `G4`, over ~300 s, with no
recovery anywhere.

What the retained evidence supports is **accumulated dataset growth**: host CPU is flat while
Goodput falls, so each request costs more CPU rather than the host having less to give, and the
curve flattens as the marginal cost of further growth does. Both topologies decline by almost the
same ratio, **which is what a workload-intrinsic effect does and not what a topology-specific fault
does.**

**Short cells cannot see it.** The 60 s runs reported 1,195/s at `G1` and 3,165/s at `G4`; the first
slices of the 600 s runs report 1,200/s and 3,442/s. **The short cells were measuring the opening
minute and reporting it as the regime.**

**Retained for Analyse & Review, not resolved here** (maintainer instruction, 2026-08-18). The
question is whether the benchmark should represent production semantics in which transactional
state genuinely grows without bound — so capacity is a function of dataset size and a declining
trajectory is the honest answer — or a bounded hot-state lifecycle where retention keeps the
working set stationary and a *mature-state* fixture is the appropriate benchmark. The two imply
different experiments and different meanings for a sustained number, and choosing between them is a
requirements and workload-envelope decision rather than an implementation one.

**Its methodological half is settled** (`ag-sept/milestone-validation.md` §4.6.5): because the trajectory is not
stationary within the window, the comparison quantity is the full-600 s horizon average from the
same fixed conditioned starting state, so every arm is read the same way and the slices describe
evolution rather than select a plateau — **which is what would otherwise let the choice of
sub-interval carry the very difference a comparison is trying to measure.**

### 3.22 The pool policy is frozen at 8, and 16 is worse than 8

Three retained 600 s `G1` runs, conditioned, driven through the same code path with the same
declared configuration and the same serving code
([`pr4a-sustained/`](../../measurements/pr4a-sustained/)):

| `pool_max_conns` | sustained | mean acquire | PG backends | run queue | p95 / p99 |
|---:|---:|---|---:|---:|---|
| 4 | 944.4/s | 9.8 -> 13.9 ms | 2.18 | 4.03 | 21.1 / 27.0 ms |
| **8** | **1,248.6/s** | 5.1 -> 6.6 ms | 4.78 | 7.26 | 17.0 / 25.0 ms |
| 16 | 1,046.6/s | ~0.04 ms | 7.42 | 14.33 | 24.0 / 35.3 ms |

**4 was an admission ceiling**: doubling it returned 32% more sustained Goodput at effectively
unchanged host CPU. **16 removed the admission queue entirely and paid for it downstream** — acquire
wait collapses to ~0.04 ms, no worker waits for a connection at all, and every downstream measure
worsens: 55% more active backends, double the run queue, p95 and p99 both 41% worse, and 16% less
sustained Goodput than 8. Host CPU stays flat across all three, so none of this is the host running
out of compute. **It is sixteen concurrent database users on a two-CPU authority queueing inside
PostgreSQL instead of queueing at the pool.** So 8 is not an obviously removable ceiling: past it
the binding constraint has already moved off admission and onto one authority's capacity to do
concurrent work.

**`G4` reproduces the ordering**, which closes the gap this section first recorded: the same three
values give 2,857.1 / **3,436.0** / 3,129.3 per second, and 8 is best on every axis — highest
sustained rate, flattest trajectory (0.81× slice 1→10), lowest percentiles, and the most even
distribution across the four authorities. So the frozen value is not a `G1` result imposed on the
composed topology.

**What "identical" does and does not cover.** The six arms were taken at four revisions, each from a
clean tree, each stamping `service_source_modified: false`. What is identical is the part that could
move a number: the diff over `internal/` and `cmd/` across the whole window is empty, so the serving
binary and the generator are unchanged in content. What differs is harness and documentation
revisions. **That is a weaker claim than "everything else identical", and it is the one the
artifacts support.**

**Decision (maintainer, 2026-08-18): `pool_max_conns=8` for every shard group at every topology.**
The capacity unit is meant to be the same unit at each topology, and a per-topology pool would make
the comparison one between two different units. Each run still records what the service actually
opened at `/meta`, so the variable is the request and `pool_size_per_replica` is the fact. **This is
configuration qualification, not pool optimisation**: no attempt was made to find the best value,
only a defensible one that is not obviously removable.

### 3.23 What PR4a handed to PR4b

The qualified configuration, all exercised end to end on the workstation: **independent per-group
demand** — one fixed worker pool, sequence and collector per shard group with group identity in the
idempotency key, and `VAL-NEG-8`'s control failing against the old shared-pool design
(`internal/loadgen/streams.go`); **explicit conditioning** — a per-organisation mutation target
rather than a duration, a disjoint slot/identity/key namespace, a state-preserving service/pool
recycle, and the measured run beginning from the conditioning artifact; **`pool_max_conns=8`**
(§3.22); **fixture sizing derived from a measured rate**, retained beside the runs; **the 600 s
shape** with ten contiguous 60 s slices, reported in order and never averaged across; and **one
host-executable entry point** that PR4b's capacity stage extends rather than replaces.

**Not established by PR4a**, and owned by PR4b: saturation reconnaissance, `S`/`H` selection, the
retained capacity comparison, and any efficiency figure. Nothing in
[`pr4a-sustained/`](../../measurements/pr4a-sustained/) may be promoted into one — each is a single
unrepeated observation of a qualification run.

### 3.24 The environment document had not received the machine it describes

[`environment.md`](../../measurements/environment.md) still recorded the earlier CPU count while
every run since 2026-08-13 had been taken on 16, and the retained artifacts said so directly. The
fact was never unrecorded — §2.14 owns both the reconfiguration and its comparability consequence.
**What had not happened is the propagation**, so the owning source and the retained evidence
disagreed.

**Why this was a PR4b blocker rather than tidying.** `VAL-SCALE-6` is by construction a claim about
*the explicitly recorded local environment*. A result whose environment record names the wrong CPU
allocation is not a smaller claim; **it is a claim about a machine that did not run it.** Two
further statements there were false rather than stale: that every run on this workstation is
`quotability.level: local` "by construction", which the six sustained runs contradict by certifying
`capacity`; and that a host exporter would settle PR2's excursions, when the correct statement is
that Iteration C runs carry host evidence and PR2's do not.

### 3.25 A single 120 s probe cannot resolve a 5% difference on this machine

Reconnaissance compares probes at neighbouring worker levels with "materially" set at 5% — twice the
2.6% agreement of ten identical `G4` cells. That figure was measured on 60 s cells driven back to
back within one session, **and it does not transfer.** `G1` at 16 workers per group was probed four
times under identical configuration: 1383.0, 1395.2, 1307.0 and 1444.7/s — range 10.5%, coefficient
of variation roughly 4%. **The 5% margin is therefore approximately one standard deviation of a
single probe.**

The consequence was observed rather than predicted: two reconnaissance passes over `G1` disagreed
about the *ordering* of 12 and 16, one measuring 16 above 12 by 7.0% and selecting S=16, the other
measuring 12 above 16 by 8.0% and selecting S=12. Both were "flat" in shape and both passed every
gate.

**The readings are noise, not drift**, and that was measured rather than assumed: a drift would be
monotonic, and these fall and then rise with the highest last. The diagnostic is a re-probe of an
already-probed level, which selects nothing and writes to a differently-named file so it cannot be
read as a bracket. Reconnaissance therefore **brackets a region rather than locating a point**, and
any distinction it draws inside 5% is a coin-toss. That is now a stated limitation of `VAL-SCALE-6`.

### 3.26 The selection rule was one-sided, and the plateau it missed is the local shape

The rule as written tested only the upper side: `S` is selected when `H` and its confirmation fail
to produce materially higher sustained Goodput. Nothing tested that `S` beats a *lower* level, so a
candidate sitting past the peak passes unchallenged **and every efficiency dividing it inherits the
error in the direction that understates capacity.**

**Maintainer decision, 2026-08-19:** reconnaissance must also show the level below its candidate to
be materially worse. It fired on its first real use, refusing to spend four retained 600 s runs on a
2.2% difference the machine cannot distinguish from noise. That refusal exposed a definitional gap:
when throughput is flat across several levels, which one is `S`? **Maintainer decision, 2026-08-19:**
the lowest level on the discovered plateau, its immediately lower level materially worse, and `H` a
higher level that is not materially better — because every level on a plateau delivers the same
Goodput and the lowest does it with the least queueing, so selecting a higher one attributes
capacity to workers that bought nothing. Both live in `ag-sept/milestone-validation.md` §4.6.4,
which owns the method; the code cites the plan rather than defining the rule.

### 3.27 One bracket for every arm, because the per-topology difference was not real

With the plateau rule applied, reconnaissance selected S=12 at `G1`, S=8 at `G2` and S=12 at `G4`,
and 12 is the measured throughput maximum at all three:

```text
workers      G1        G2        G4
   4          -    2056.7         -
   8     1342.8    2596.7    3603.5
  12     1411.9    2617.8    3827.7      <- maximum at every topology
  16     1307.0    2559.0    3657.5
  24     1225.9    2567.8    3588.6
```

`G2` dissented only because its 8 read within **0.8%** of its own 12, while `G1`'s read 5.1% below
and `G4`'s 6.2% below. Against §3.25's ~4% single-probe noise, 0.8% is not a discrimination.

**Maintainer decision, 2026-08-19: every arm runs at S=12, H=16.** `E2` and `E4` compare topologies,
so arms measured at different demand-per-group would carry that difference into the efficiency
itself, which `ag-sept/milestone-validation.md` §2.3 forbids. The bracket overrides are the one way
to move the retained operating point by hand, so the capacity stage validates them against each
topology's own probes — a level the topology never probed is refused, and so is one it measured
materially below its own best — and the accepted bracket is logged with the rate it was checked
against.

### 3.28 The retained comparison: `G4` resolved, `G1` and `G2` did not

Twelve retained 600 s runs, each from its own reset/reseed/conditioning sequence and each checked
against the run that was *asked for* rather than the run that happened. All twelve certified
`capacity`, ran the full 600 s at the intended worker level on the intended fixture, and reconciled.

```text
              S      H   S-conf  H-conf    S repro   H repro   any H beats any S
  G1     1080.1 1032.6   1094.0  1193.8       1.3%     15.6%      yes, 10.5%
  G2     2259.0 2323.1   2450.8  2477.7       8.5%      6.7%      yes,  9.7%
  G4     3494.8 3509.5   3492.9  3543.5       0.1%      1.0%      no,   1.4%
```

**`G4_local` = 3493.9/s**, the mean of two selected-point observations agreeing to 0.1%, with a
deciding higher point that reproduces to 1.0% and does not beat it. **`G1` and `G2` are explicitly
unresolved**, so `G1_local` and `G2_local` are withheld and, because `G1_local` is the denominator
of both, `E2_local` and `E4_local` with them. `G1` fails on `H`'s reproducibility and on the
upper-side test; `G2` fails on both points' reproducibility *and* the upper-side test. The
upper-side figures are `ag-sept/milestone-validation.md` §4.6.5's pairwise comparison, `max(H)`
against `min(S)`, which §3.32 records
as a correction to an implementation that compared extreme against extreme; neither topology's
disposition changes, because both were already unresolved on reproducibility.

**No retained resource signal distinguishes `G1`'s four runs**: host CPU busy 2.3–2.4 cores of 16,
memory available within 0.2%, run queue 7.4–8.0, active backends 4.4–5.0, and all four took exactly
four requested checkpoints. Every run at every topology is a "dip" in shape, so none belongs to a
visibly different regime, and `G1`'s outlier sits above the other three at all ten of its slices
rather than diverging part-way.

### 3.29 Run position was the wrong explanation, and the control refuted it

The prescribed order puts `S` at positions 1 and 3 and `H` at 2 and 4. The observed rates rose with
position — `G2` monotonically at +0.0%, +2.8%, +8.5%, +9.7%, and `G1`'s position-4 run highest at
+10.5% — which is on its own enough to manufacture the upper-side failure that left `G1` unresolved,
**with no difference between 12 and 16 workers existing at all**. Under the pairwise rule the two
are the same comparison: "best `H` exceeds weakest `S` by 10.5%" and "position 4 exceeded position 1
by 10.5%" are one measurement read two ways. That is what made the hypothesis worth a control rather
than an argument.

**Four identical 600 s `G1` runs at 12 workers refuted it**
([`pr4b-drift-g1/`](../../measurements/pr4b-drift-g1/)): 1255.9, 1023.1, 1202.1, 1279.7 — not
monotonic, with the deep outlier at position 2. `G2`'s monotonic appearance was coincidence. What
the control established instead is stronger and worse: **`G1`'s run-to-run spread is 25.1% under
identical conditions**, five times the margin, so the 15.6% disagreement that left its knee
unresolved sits comfortably inside it and **the `G1` knee was never resolvable at this margin by any
run order.**

The hypothesis was recorded before its killing test rather than after, and the test was named in the
same edit that recorded it. That is the only reason it cost one control run rather than a
counterbalanced re-drive of eight.

### 3.30 The cause is the storage path, and it is why `VAL-SCALE-6` is not discharged

**The evidence for this section had to be recovered after the runs, and that is a finding in its own
right — see §3.31.** Every figure below comes from
[`pr4b-capacity/disk-io-backfill/`](../../measurements/pr4b-capacity/disk-io-backfill/). Reads are
at most ~2% of I/O volume, so this is a write-path story.

**Goodput tracks delivered write bandwidth, and the ratio barely moves.** Mutations per MiB written
is 65.2–72.7 across all sixteen runs and every topology. Within `G1` it holds run by run while
Goodput does not — the run that "beat" its selected point obtained the most write bandwidth, 17.86
MiB/s at 1193.8/s against 15.39–16.78 at 1032.6–1094.0. The control is sharper still: across four
*identical* runs Goodput spans 25.1% while mutations per MiB spans **1.0%**. **The work done per
byte written is constant; what varies is how many bytes the device accepted.**

The device is never idle at any topology — utilisation ≈1.0 everywhere — but it is **not
saturated**, which the topologies establish against each other:

```text
topology  authorities  disk queue depth   write MiB/s   spread over 4 retained runs
  G1           1         0.94 - 1.37      15.39-17.86            15.6%
  G2           2         3.31 - 4.44      31.67-36.18             9.7%
  G4           4         4.52 - 5.00      48.30-48.93             1.4%
```

`G4` extracts roughly three times `G1`'s write bandwidth from the same device at three to four times
the queue depth. **Utilisation alone would have supported the opposite conclusion**, which is why
utilisation and queue depth are now defined as panels and plotted on one graph.

**What the spread column does and does not support.** Each topology's four runs span two worker
levels, so it is the range of a retained population rather than four repetitions of one point, and
`G2`'s and `G4`'s figures rest on four observations each — too few to bound a distribution. The
per-point pairing is *not* monotonic: S-vs-S-confirm is 1.3% at `G1`, 8.5% at `G2`, 0.1% at `G4`.
`G1`'s 1.3% is precisely what prompted the drift control, and it was luck. So `G1`'s poor
reproducibility is measured and not in doubt, and `G4`'s tightness is consistent with a
better-behaved distribution without being a measurement of one. **That reproducibility improves
*because* more authorities issue I/O concurrently is a reading of the queue-depth and bandwidth
columns beside those spreads, not a result established by them.** Confirming it needs the control
`G1` received, driven at `G2` and `G4` as well; it was not.

`G1_local` is the denominator of both efficiencies and is the least reproducible quantity in the
experiment. **On this environment neither `E2_local` nor `E4_local` can be derived to a 5% margin,
because their denominator cannot be measured to better than ~25%.** That is a property of the
environment rather than of `alloca-go`, and it lands on exactly the term
[`environment.md`](../../measurements/environment.md) names as the one the instruments cannot see —
the shared virtual disk on the host — where PR2's unexplained excursions also live. Independently
provisioned per-unit storage is the condition that removes it, a direct input to the case for PR4c.

**Recorded as a limitation, not a diagnosis.** That low queue depth is the *mechanism* by which the
storage path's variability reaches `G1`'s throughput is consistent with every measurement above and
is not proven. The killing test — driving `G1` with its data directory off the shared virtual disk
and observing whether the spread collapses — was deliberately **not** run.

**Execution stopped here.** The drift control is preserved, the storage-path limitation documented,
`G4` established alone, the `G1`/`G2`-derived efficiencies withheld, and `VAL-SCALE-6` marked
unresolved and not discharged. Chasing the mechanism further would have extended a local
investigation without improving the evidence Iteration C actually targets.

### 3.31 A load-bearing number was quoted from a live query, not from an artifact

The storage figures above were originally read out of a running Prometheus and written straight into
this record and the measurements README. No cell under `docs/measurements/` contained a disk series,
so none of those numbers had a retained artifact — `measurement-contract.md` §5's requirement — **and
the tables looked exactly like every other evidence-backed table in the repository.** It was caught
by the maintainer asking where the figures came from, not by any gate.

Three separate faults, and only the first is about storage:

- **The disk collector was enabled but `panels.json` defined no disk panel**, so nothing was
  retained. The comment justifying a wide collector set — "collect broadly, panel narrowly: the
  snapshot retains everything scraped" — was true and insufficient. The *cell's* panels are what a
  report cites, and the snapshots that would have justified these numbers are ~123 MB per run and
  are not promoted. **The rule needs its second half: promote a series to a panel the moment a claim
  rests on it.**
- **The ad-hoc queries did not use the repository's own aggregation**, taking a different rate range
  and step from every retained panel, so the figures were not comparable with any other series here
  and shifted by up to 5% when re-derived correctly.
- **The spread column compared three different quantities** — four identical runs at `G1`, an
  S-vs-S-confirm pair at `G2`, an H-vs-H-confirm pair at `G4` — assembled after the fact into a
  monotonic-looking column. Computed consistently the monotonic reading weakens, and on the
  per-point pairing it inverts. **That is the shape of a result found by choosing comparisons rather
  than by making them.**

The durable fixes are upstream: four disk panels are now defined in `panels.json`, so every future
cell retains them at run time, and they are **plotted** (maintainer decision, 2026-08-19, recorded
at the graph bound in `internal/observability/panels_test.go`). Defining them closes the retention
half; plotting closes the half that bound's own comment names — that an unplotted series is
*recoverable* but not *noticed*, which is the condition a future degraded cell would be read under.
The sixteen runs that predate the fix carry a `disk-io-backfill/` directory whose README states that
the series were recovered from the surviving TSDB after the fact, with the queries used.

### 3.32 Review found two gates that would have admitted a wrong result

Independent review raised seven findings, four of them `P1`. Two were defects in gates that had
tests, passed them, and were wrong anyway.

**The knee rule was weaker than the validation plan, not conservative.** The implementation compared
the best `H` observation against the best `S` observation, with a comment asserting that using `S`'s
better reading made the rule harder to pass. **It makes it easier.** The reviewer's counterexample:
`S` = 100/105 and `H` = 110/105, where both points reproduce inside the margin and best-`H` is under
5% above best-`S`, so the knee resolves — while the first `H` stands 10% above the first `S`, which
the rule forbids. The rule is pairwise: no `H` observation may materially exceed *any* `S`
observation, which is `max(H)` against `min(S)`. The reasoning in the comment was backwards, and no
test caught it because every case in the suite had its extremes on the same side as its pairs.

**A retained cell was accepted on resume without any intent check.** The capacity stage validated
duration, worker level and fixture for a cell it had just driven, and kept an existing cell on
sight. Resume exists so an interrupted fourteen-run sequence need not restart — **but the cell it
keeps is from an *earlier* invocation, which is exactly when the bracket or fixture may have
changed.** A 600 s `S` at the old level, sound and `capacity`-certified, would then have been mixed
with confirmations from the new one. Both reviewers found this independently, and the bracket
self-test **demonstrated** the hole rather than catching it: its accepted case pre-created every
role at 12 workers while `H` was 16, and the stage was content. The fix is one `assert_cell_intent`
used by the driving path, the resume path and the drift stage, so no path judges a cell more loosely
than another. The result script also enforces the horizon and cross-checks that the four runs are
one comparison, because it is documented as runnable directly against any retained directory and
must not depend on having been reached through the stage.

**The margin's rationale was falsified by this milestone's own evidence.** The plan called 5%
"derived" from the 2.6% agreement of ten identical `G4` cells and said a difference must exceed
"what this machine can distinguish from noise". PR4b then measured four identical `G1` runs spanning
25.1%. The correction is a distinction rather than a new number: **5% is a preselected engineering
materiality margin, and reproducibility is a separate admissibility gate.** Materiality asks whether
a difference is large enough to matter; reproducibility asks whether the environment can measure the
point at all. Read that way `G1`'s failure strengthens the method — the margin did not fail, the
environment failed the admissibility gate, and the rule withheld a number rather than reporting one
it could not support.

**The common bracket was a maintainer decision the validation plan did not own.** §3.27 recorded it
while `ag-sept/milestone-validation.md` §4.6.4 still said reconnaissance selects per topology, so
for `G2` the claim that the defined method ran end to end was not literally true. §4.6.4 now owns
the exception, with three conditions
the stage already enforced and a fourth it did not: the comparison must *record* that it ran at a
common bracket, which `common-bracket.txt` now does beside the runs. Two smaller findings: the
storage wording was stronger in the plan and status table than the mechanism paragraph it
summarised, and is now uniformly "localises the limit to the shared write path" with the unrun
killing test named; and `VAL-NEG-8`'s status row still read "not yet executed" though PR4a
implemented and mutation-proved it, which matters because it is a prerequisite for interpreting
`VAL-SCALE-6`.

**What this says about the gates rather than the findings.** Both `P1` defects were in code with
passing discriminating tests. The knee rule's tests all placed their extremes on the same side as
their pairs, so a rule about pairs was never exercised as one; the resume path's test asserted the
outcome it wanted while constructing a fixture that could only reach it through the hole.
**Mutation testing does not help here — mutating a rule the suite never distinguishes from its
weaker form produces a passing mutation, which reads as evidence the rule is unnecessary.** What
found these was a reader reasoning about the rule's *intent* against a counterexample of their own
construction.

## 4. Evidence boundary and open items

What PR4 established is in §1; the authoritative version is
[`ag-sept-pr4-scheduler-partitioned-capacity.md`](../../measurements/reports/ag-sept-pr4-scheduler-partitioned-capacity.md),
and `VAL-SCALE-6`'s status is owned by
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) §9.

Still open at the close of PR4:

- **What the benchmark should represent** when transactional state grows without bound (§3.21).
  `ag-sept/milestone-validation.md` §4.6.5 fixes the comparison quantity so the arms are
  comparable; it does not settle whether an
  indefinitely growing dataset is the right thing to measure. Carried to Analyse & Review.
- **A validated fix for the cached-plan fixture artefact** (§3.20). The root cause is demonstrated;
  the candidate treatments change what the fixture measures and are maintainer decisions.
- **The reproducibility rate of the degraded regime is not established** (§3.15). Observed 2 in 10
  and 1 in 4 on one morning and 0 in 30 the same afternoon at identical configuration — too few
  cells to state a rate, and an admission rule derived from an unstable base rate would be worse
  than none. It no longer gates PR4b, because §3.20 removed the starting state rather than bounding
  the regime.
- **The storage-path mechanism is a limitation, not a diagnosis** (§3.30). The killing test — `G1`
  with its data directory off the shared virtual disk — was not run, and neither was the
  identical-run control at `G2` and `G4`.
- **The generator-headroom control** (§2.14) is runnable but has not been taken against a stable
  high-useful-demand `G4` point, so it would risk comparing two regimes by accident.
- **Whether monitoring splits onto its own host** (§2.2) stays open until a preflight says it needs
  to.
- **Per-unit panel aggregation and the dashboard retitle** (§2.6) are not done. The units are
  scraped and labelled by `authority`, but the committed dashboard is still PR2's single-instance
  one.
- **Generator currency is checked only on the Iteration C path.** Every driving stage of the
  Iteration C entry point rebuilds the generator and refuses a binary stamped `vcs.modified=true`;
  the older direct-drive scripts still only check existence, so §3.6's failure remains reachable
  there.
- **Independently provisioned capacity evidence** (`VAL-SCALE-5`) remains unproven and depends on an
  environment that does not yet exist (§5).

## 5. AWS Standard On-Demand quota increase declined — 2026-08-16

AWS Support declined the requested increase to the Running On-Demand Standard instance quota in
`eu-west-2` after review of the appeal, on the grounds that the account does not yet have enough
established service usage and on-time billing history. AWS may reassess after that history exists.

**Interpretation:** an external account-maturity constraint, not evidence about Alloca's
architecture, capacity, or the validity of the Iteration C experiment. It does not trigger the
validation plan's Tier 2, which requires the complete independently provisioned measurement
environment to exist before Tier 2 can be considered.

**Maintainer decision:** do not appeal again immediately. Continue the local
measurement-qualification work, which is unmetered and had already exposed material experiment
defects. A bounded bootstrap may still use the quota already available if it removes
provisioning-specific uncertainty, but any such bootstrap is operational evidence only and never
capacity evidence.

PR4c therefore remained conditional on enough quota to instantiate the **complete** independently
provisioned `G1`/`G2`/`G4` environment. That environment did not exist inside the AG-Sept timebox,
so PR4 records the blocker and leaves aggregate capacity scaling and `VAL-SCALE-5` explicitly
unproven. **Neither the shared-workstation result nor a partial topology is promoted as a substitute
for it.** The denial changed the feasibility and likely size of the cloud pass; it did not change
the milestone's scope or the validation rule.
