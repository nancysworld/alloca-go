# AG-Sept PR2 — Single-instance frontier (scope)

**Status:** Complete — every decision in §5 settled, nothing open in §6. Results and the
frontier report are in [`../measurements/pr2-frontier/`](../measurements/pr2-frontier/)
**Budget:** 2.5 development days ([AG-Sept plan](ag-sept-plan.md) §14) — 1.0 for the retention
path and diagnostic panels, 1.5 for the sweeps, controls, and report
**Owner doc:** [ag-sept-plan.md](ag-sept-plan.md) §7 and §12.2 are normative for what this PR
measures; this note records only how PR2 discharges them and the choices made along the way.

## 1. Exit gate

From the plan, verbatim:

> each controlled workload has a repeatable one-instance result; the generator is ruled out;
> the time series expose or tightly bound the limiting mechanism; and the recommended
> operating point is reported or explicitly deferred with evidence.

Four clauses. The fourth is the one that decides whether PR2 succeeds honestly: "or explicitly
deferred with evidence" means a frontier PR2 cannot resolve is a valid outcome, provided the
bound is stated and shown. An unresolved frontier reported as a number would be the failure.

**What PR2 may not claim, whatever it measures.** The generator shares a host with the service
until PR4 (§14), so every figure here is a *bounded local* result. §12.2's headroom control is
what limits how much the co-resident generator can be distorting it, and that limitation
travels with each number rather than sitting in a footnote. `quotability.level` stays `local`
for PR2's runs by construction — `publishable` requires the separate compute of §10.

## 2. What PR2 delivers

| # | Deliverable | Plan reference |
|---|---|---|
| 1 | Minimal reproducible Prometheus retention path, version-controlled | §14 PR2 |
| 2 | Compact diagnostic panel set over the §6.1 signals, reproducible and version-controlled | §14 PR2 |
| 3 | The two remaining §6.4 service-shape fields — see §3.1, which are fewer than the plan assumes | §6.4 |
| 4 | Bounded one-instance sweeps for dispersed, hot-slot and hot-identity | §7 |
| 5 | §6.2 discharged end to end: throughput and p99 with telemetry on versus off, same dataset, concurrency and environment | §6.2 |
| 6 | Generator-bottleneck control, and demonstrated generator headroom | §12.2 |
| 7 | Peak observed throughput, SLO-safe capacity, recommended operating capacity — or a documented reason each remains unresolved | §7, `measurement-contract.md` §3 |
| 8 | Retained run reports, verdicts, environment details, and time-series exports | §14 PR2 |

## 3. What PR1 changed about PR2's scope

Two of PR2's scope lines were written before PR1 was built, and PR1 has moved the boundary
under both. Recording it here so the work is not done twice, and so the plan's §14 wording is
read against what actually exists.

### 3.1 Three of the five service-shape fields already populate themselves

The plan asks PR2 to "populate the §6.4 service-shape fields … PostgreSQL version, pool size
per replica, server `GOMAXPROCS`, timeout budget, and reservation TTL."

PR1 made the generator read the service's `/meta`, which already supplies **server
`GOMAXPROCS`, the timeout budget, and the reservation TTL** — and `local` already refuses a run
missing any of them. Building operator flags for those now would add a second, hand-typed
source for values the service already reports, which is the transcription risk PR1's §3.5
removed.

**What was left was two fields** — and PR2 removed the operator from both (Nancy's call,
2026-08-03). The service holds the connection, so it can report its own `server_version` and
pool ceiling; `/meta` now carries a `database` block and the manifest fills
`postgres_version` and `pool_size_per_replica` from it.

That takes PR2's operator-supplied field count to **zero**. The version is read once at
startup rather than per request — it cannot change under a live pool, and a `/meta` that issued
a database round trip would fail exactly when the database is the thing under pressure.

`aggregate_pool_size` is the one pool fact that remains, and it is not one a process can know:
the aggregate needs a replica count. It stays with the topology in PR3.

### 3.2 §6.1's signal list is already complete

The plan's §6.1 requires pool state and acquisition duration, process CPU and memory, and Go
runtime signals. PR1 registered all three — `collectors.NewGoCollector`,
`collectors.NewProcessCollector` and a `pgxpool` collector, in
`cmd/alloca-go/observability.go`. PR2 therefore builds panels over signals that already exist
rather than adding instrumentation, which is where the 1.0-day estimate for the retention path
becomes plausible.

The one required output with no Prometheus series behind it is **generator utilisation**. The
generator is a short-lived process per cell with no scrape endpoint, so its CPU is a scalar in
`run.json` rather than a time series. PR2 exports it into the cell's CSVs from the report
rather than adding a push path — a pushgateway for one number per cell would be more
observability platform than §14 allows.

Two other §7 outputs are worth naming as gaps rather than quietly redefining: **offered
requests** is likewise generator-side and comes from the report, and **database utilisation**
— as distinct from pool pressure — has no source at all, since the service exposes pool state
but nothing about the PostgreSQL process. §14 PR2's own wording says "database-pool pressure",
so this is a place where the plan's two sections disagree about what is required; it needs a
decision when the exporter lands.

## 4. Obligations PR1 deferred into this PR

These are not new scope. They are commitments PR1 made in code and docs, and PR2 is the PR
that owes them.

### 4.1 Warm-up must become quotable

`internal/loadgen/run.go` refuses any run whose warm-up discarded a response, because the
discarded responses leave rows the client totals no longer mention and reconciliation would
then fail a *correct* service. The comment names PR2 as the owner, "with the sweeps that need
them".

Sweeps need warm-up: a cold pool and an unwarmed process put their startup cost in the first
cell of every sweep, which is exactly where a frontier curve is read. So PR2 must make warm-up
reconcilable, not merely allowed. The mechanism is settled in §5.4.

### 4.2 DEBT-3's trigger fires here

[`tech-debts.md`](tech-debts.md) DEBT-3 records that `/meta` is read once before a run, so
`service_commit_sha` means "the service behind the target when the run began". Its first
listed trigger is **this PR**: "PR2 introduces long-running sweeps where an unattended service
restart becomes plausible during one measured run."

A sweep restarts the service between cells by design (counters are cumulative), so the window
for an unnoticed identity change is wider here than in PR1's single run. DEBT-3's Option 1 —
fetch `/meta` before *and* after each run and require the identity to match — is the smallest
fix, stays inside the HTTP-only boundary, and is in scope for PR2.

### 4.3 §6.2's service-side switch — added, with three modes

Discharging §6.2 end to end means running the same workload with telemetry enabled and
disabled, and `buildRecorder` had no way to turn it off. `ALLOCA_TELEMETRY` now selects one of
three (Nancy's call, 2026-08-03):

| Mode | Recorder | Reconcilable | What it answers |
|---|---|---|---|
| `full` (default) | slog + Prometheus | yes | production shape |
| `metrics_only` | Prometheus | yes | what the *log sink* costs |
| `off` | none | **no** | what no observation at all costs |

**`metrics_only` is the arm §6.2's comparison uses**, because PR1 already measured where the
cost is: 1793 ns for the Tee against a real file, 136 ns for the Prometheus recorder alone. The
log write is essentially all of it, so this arm isolates the cost that matters while leaving
the run reconcilable.

**`off` refuses itself, deliberately.** With no aggregate series there is no server-side count,
so §6.5's three-way agreement has only two — and a verdict reached on two of three is the
weaker gate wearing the stronger one's name. The manifest gate rejects `telemetry_mode: off`
with that reason rather than letting it surface as a confusing "server counted 0". It is a
control, not a measurement, which is exactly how `-validate=false` is treated.

The mode is recorded in the manifest as provenance: a throughput figure measured with logging
disabled is not comparable to one measured with it on, and without the field nothing
downstream could tell them apart.

## 5. Decisions taken

### 5.1 Sweeps are driven by a version-controlled script, not by hand

A sweep cell is seed → restart service → load → scrape → verify, and PR1's own operator guide
shows how easily a missed restart contaminates a scrape. Hand-driving a matrix would reproduce
that failure at scale, and the §5.3 clean-start hazard makes a contaminated cell look like a
result rather than an error. The sweep runner is therefore a script under `test/scripts/`,
emitting one directory of artifacts per cell.

### 5.2 Pool size varies by DSN, not by new configuration

`pool_max_conns` is already a DSN parameter consumed by `postgres.OpenPool`, so varying pool
size across sweep cells needs no code and no new environment variable. This also keeps the
value the manifest records and the value the pool actually used the same string.

### 5.3 Grafana is the diagnostic view; CSVs and a retained snapshot are the evidence (Nancy's call, 2026-08-03)

Both, with different jobs, because they answer different questions.

**Grafana**, provisioned automatically from a dashboard JSON in the repo, is what you watch
while a sweep runs — the thing that shows a pool saturating before the throughput curve
flattens. Deliberately small: **one** dashboard, the scoped panels only, backed by the same
canonical PromQL the exporter uses. **No alerts, no plugins, no template variables beyond what
the panels need, no polish.** Those are the features that turn a diagnostic aid into the
"general observability platform" §14 excludes.

**The snapshot exporter** is what the report quotes. At the end of each cell the runner saves a
Prometheus TSDB snapshot and exports each panel's query result as CSV into that cell's artifact
directory. A screenshot is not re-derivable; a CSV beside the snapshot it came from is, and
`measurement-contract.md` §5.3 requires reports to quote from raw artifacts rather than from a
view.

The two share one query source, so the panel you looked at and the number you quoted cannot
drift apart.

### 5.4 Warm-up is a separate phase, and the fixture is reset without restarting the service (Nancy's call, 2026-08-03)

The cell sequence is:

```text
seed  →  start/restart service  →  warm up  →  reset fixture (service keeps running)
      →  measured load  →  export  →  verify
```

Warm-up traffic never reaches reconciliation, because the reset clears the rows it created
before the measured phase begins. So `Report`'s schema and `alloca-verify` are untouched, which
is what makes this the cheaper of the two mechanisms — the alternative carried per-cell warm-up
totals through to the verifier and changed the schema every retained artifact is read with.

**The service is deliberately not restarted after warm-up**, and that is the point of the
sequence rather than an omission. Restarting would discard exactly what warm-up establishes: a
filled connection pool, a settled heap, and whatever the runtime has already optimised. A
warm-up followed by a restart measures a cold service again.

**What that costs, and the change it forces.** Prometheus counters are cumulative and only a
process restart zeroes them, so a scrape taken after the measured phase carries the warm-up
requests too — while `run.json` describes the measured phase alone. `serverTotalsCheck` compares
those two and would fail every warmed cell, reporting a correct service as unreconciled.

So the client/server comparison must move from an absolute count to a **delta between two
scrapes**: one captured after the reset and before the measured load, one after. That is a
change to `alloca-verify`'s interface — a second scrape argument — and to the arithmetic of the
check, and it is the price of keeping the service warm.

It is also an improvement beyond warm-up. PR1's operator guide has to insist on restarting the
service before *every* run precisely because the check reads absolute counters; a delta removes
that requirement and with it the whole class of contaminated-scrape failures the guide's §7
catalogues.

### 5.5 Sweep cells are bounded by duration, and the fixture is sized to the window (2026-08-03)

`-n` is the wrong bound for a sweep. It makes a cell's length vary *inversely* with
throughput — the faster the service goes the sooner the cell ends — so the fewest samples land
at exactly the operating points a frontier is read from, and no two cells cover the same
interval. PR1's 60-iteration smoke run finished in 0.06s, which no `rate()` window can resolve.

`alloca-load -duration` bounds by wall clock instead. Workers finish the unit they are on and
then stop, rather than the window cancelling requests in flight — a context deadline would turn
the tail of every cell into timeouts the harness caused itself, at exactly the load where real
timeouts matter.

**Measured overhead per cell** (2026-08-03, this workstation, dispersed at concurrency 16):

| Step | Cost |
|---|---|
| seed (`-slots 3000 -capacity 50`) | ~2.3s |
| service restart + readiness | ~1–2s |
| reset between warm-up and measured phase | ~2.3s |
| export (14 queries + TSDB snapshot) | ~2–3s |
| verify | ~1s |
| **fixed total** | **~9–11s** |

So a cell costs its warm-up plus its window plus ~10s. At 20s warm-up and 60s measured that is
~90s, and a few hundred cells is hours of unattended machine time rather than days. **The
binding constraint on matrix size is analysis and report effort, not runtime** — which means
the matrix should be cut for interpretability, not for wall clock.

**Fixture sizing becomes a sweep parameter, and this is the trap.** A 10s cell at concurrency
16 completed 24,296 requests but goodput stopped at exactly 10,000 — the fixture's capacity
(200 slots × 50). The remaining ~60% of the window measured *refusal* throughput, not booking
throughput, and nothing in the run says so: every check passes, because refusals are a valid
domain answer.

An iteration-bounded run hid this by being too short to exhaust anything. A duration-bounded
one will exhaust any fixture that is not sized to `window × expected throughput`, so the seed
must scale with the cell. Seeding is cheap (150,000 units in ~2.3s), so the rule is to
over-provision and check: **a cell whose goodput plateaus at exactly the fixture capacity is
measuring the fixture, not the service**, and the sweep runner should refuse it rather than
report it.

### 5.6 The recommended-operating-capacity number is deferred to PR3 (Nancy's call, 2026-08-03)

`measurement-contract.md` §3 defines it as "a conservative cap below SLO-safe capacity that
reserves headroom for **variance, rolling deployment, and loss of one unit**." At one replica,
two of those three cannot be reserved against at all:

| Component | At N=1 |
|---|---|
| Variance | measurable — repeat cells and compute the spread |
| Rolling deployment | a roll is a full outage; no margin covers it |
| Loss of one unit | losing the only unit is total loss; no margin covers it |

A single-instance figure could therefore cover one of the three components the definition
names, while a reader would reasonably take it to cover all three. Choosing a fraction would
not fix that — it would hide it behind a plausible number, which is the failure this project
keeps catching.

**PR2 therefore defers the headline number and says why**, which the plan's own exit gate
allows: "the recommended operating point is reported **or explicitly deferred with evidence**".

**What PR2 still owes PR3**, so this is picked up rather than rediscovered:

- the **measured variance** of repeated cells at the recommended operating point, reported as a
  named component with its sample count — it is the one component that is meaningful at one
  replica, and PR3 needs it the moment a second replica makes the other two computable;
- the **SLO-safe capacity** the margin would be taken from, which PR2 does report;
- this section, cited from the plan's §14 PR3 scope so the obligation travels with the PR
  sequence rather than living only here.

#### 5.6.1 Amended 2026-08-04 (Nancy's call, during review): only the *term* is deferred

The reasoning above stands and the `measurement-contract.md` §3 quantity is still not computable
at one replica. But as first written, §5.6 let PR2 report a peak and stop — and a report that
defers its headline is very easy to read as having found nothing. Nancy's correction during
review: **the deferral covers rolling deployment and loss of one unit; it never covered
variance, and it must not be allowed to swallow the capacity question itself.**

PR2 therefore now reports, loudly and in its own §5:

- the **measured ceiling of this machine** and the plateau demonstrating it is a ceiling rather
  than the corner of the grid that had been explored;
- **what decides that ceiling** — PostgreSQL's write-ahead log, with the service at 12% CPU and
  the connection pool no longer binding;
- the **variance component** at that ceiling, which is what §5.6 always owed PR3;
- the **falsifiable prediction** for PR3's multi-instance work that follows from it.

The contract's *recommended operating capacity* term stays deferred, under its own name, so the
vocabulary is not quietly redefined to make a number reportable. See
[`../measurements/pr2-frontier/README.md`](../measurements/pr2-frontier/README.md) §5.4–§5.6.

### 5.7 What a sweep cell produced, measured 2026-08-03

A four-cell sweep ran end to end, which is what turned the sequence in §5.4 from a design into
a verified one. Two things it established that could not be reasoned about:

**The baseline delta is exact.** A warmed cell's scrapes read 11,480 before the measured phase
and 76,379 after; the client reported 64,899, and 76,379 − 11,480 = 64,899 to the request.
Without the baseline the check compares 76,379 against 64,899 and fails a correct service, so
this is the measurement that proves warm-up is reconcilable rather than merely permitted.

**The results are physically sensible, which is the first evidence the harness measures the
right thing.** `dispersed` reached ~2,200 req/s with goodput equal to throughput; `hot_slot` at
the same concurrency reached ~630 req/s with goodput of 5/s — the single contended row
admitting its capacity and refusing the rest, which is §5.2's expected serialization rather
than a fault. Generator CPU stayed at 0.008–0.019 per core throughout, so nothing here is
generator-limited; the §12.2 control still has to establish that properly.

### 5.8 Every figure is labelled against its artifact

`measurement-contract.md` §2 and §5.3 already require this, and PR1's own scope note still
managed to quote a range that matched neither its table nor its artifact. PR2's report
therefore derives every quoted number from a retained file by script, and the script is part
of the deliverable.

## 6. Open — these need Nancy before or during implementation

Listed so they are decided rather than discovered. The warm-up mechanism blocked
implementation of the sweep runner and is now settled in §5.4; the remaining choices can be
defaulted after the first ranging cells.

1. ~~**Panel form**~~ — settled 2026-08-03, see §5.3.
2. ~~**Warm-up mechanism**~~ — settled 2026-08-03, see §5.4.
3. ~~**Sweep matrix size**~~ — measured 2026-08-03, see §5.5. Cell cost is ~10s of fixed
   overhead plus warm-up plus window, so runtime does not bound the matrix; the remaining
   choice is which cells are worth interpreting, and that is settled once the ranging pass
   runs.
4. ~~**Recommended-operating-capacity margin**~~ — deferred to PR3, see §5.6.

Nothing in this list now blocks implementation.

## 7. Proposed sweep matrix `[HYPOTHESIS]`

Bounded deliberately: the plan asks to "vary concurrency or offered rate, pool size, and
application resources **only as needed** to identify or tightly bound the frontier."

| Axis | Values | Why these |
|---|---|---|
| Workload | dispersed, hot-slot, hot-identity | §5's three controls; each isolates one mechanism |
| Concurrency | to be set by a ranging pass, then refined around the knee | a fixed ladder spends cells where nothing changes |
| Pool size | default, plus one smaller and one larger | separates application compute from database admission |
| Telemetry | on, off — dispersed only | §6.2 needs one comparison, not one per workload |

Application-CPU variation (§12.4) is *desirable*, not mandatory, and is the first thing to drop
if the budget bites.

## 8. Exit gate — how it stands

| Clause | Status |
|---|---|
| each controlled workload has a repeatable one-instance result | **met** — dispersed, hot_slot and hot_identity each run twice, spreads reported |
| the generator is ruled out | **met** — §12.2 control, throughput flat across a 10x change in generator compute |
| the time series expose or tightly bound the limiting mechanism | **met** — pool saturation identified and confirmed by a pool-size ladder |
| the recommended operating point is reported **or explicitly deferred with evidence** | **deferred with evidence** — §5.6 and the frontier report §5 |

The fourth clause is discharged by its second branch, which the plan explicitly allows. Two
things stopped a number being defensible, and both are stated rather than worked around: at one
replica only one of the margin's three components is meaningful, and §4 of the frontier report
shows the environment moving throughput by ~2x for reasons outside the harness.

The honest summary is that PR2 identified *what* limits this deployment and bounded *how much*,
but did not resolve a capacity figure — which is the outcome the exit gate was written to
permit.

## 8. Not in PR2

Replica scaling, Kubernetes, AWS, rich dashboards, alerting, and the optional synchronized
release wave (plan §5.4) — all per the plan. Also not in PR2: any published capacity number, which
requires the separate generator compute of §10.
