# AG-Sept PR2 — Single-instance frontier (scope)

**Status:** Draft — §5 lists what is decided and §6 what is not
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

**What is actually left is two fields**, neither of which any endpoint reports:
`postgres_version` and the pool arithmetic (`pool_size_per_replica`, `aggregate_pool_size`).

### 3.2 §6.1's signal list is already complete

The plan's §6.1 requires pool state and acquisition duration, process CPU and memory, and Go
runtime signals. PR1 registered all three — `collectors.NewGoCollector`,
`collectors.NewProcessCollector` and a `pgxpool` collector, in
`cmd/alloca-go/observability.go`. PR2 therefore builds panels over signals that already exist
rather than adding instrumentation, which is where the 1.0-day estimate for the retention path
becomes plausible.

The one signal PR2 must still add is on the *generator* side — see §4.2.

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
reconcilable, not merely allowed. Two mechanisms are viable and §6 records which is chosen.

### 4.2 DEBT-3's trigger fires here

[`tech-debts.md`](tech-debts.md) DEBT-3 records that `/meta` is read once before a run, so
`service_commit_sha` means "the service behind the target when the run began". Its first
listed trigger is **this PR**: "PR2 introduces long-running sweeps where an unattended service
restart becomes plausible during one measured run."

A sweep restarts the service between cells by design (counters are cumulative), so the window
for an unnoticed identity change is wider here than in PR1's single run. DEBT-3's Option 1 —
fetch `/meta` before *and* after each run and require the identity to match — is the smallest
fix, stays inside the HTTP-only boundary, and is in scope for PR2.

### 4.3 §6.2 needs a service-side switch that does not exist

Discharging §6.2 end to end means running the same workload with telemetry enabled and
disabled. `buildRecorder` currently always returns `Tee{slog, prometheus}` with no way to turn
it off, so the comparison cannot be run at all today. PR2 adds that switch.

It must be a *service* switch, not a generator one, and it must be recorded in the manifest —
a run with telemetry disabled is not comparable to one without, and nothing downstream could
tell them apart from the totals.

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

### 5.3 Grafana is the diagnostic view; CSVs and a retained snapshot are the evidence (Nancy's call, 2026-08-04)

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

### 5.4 Every figure is labelled against its artifact

`measurement-contract.md` §2 and §5.3 already require this, and PR1's own scope note still
managed to quote a range that matched neither its table nor its artifact. PR2's report
therefore derives every quoted number from a retained file by script, and the script is part
of the deliverable.

## 6. Open — these need Nancy before or during implementation

Listed so they are decided rather than discovered. §6.1 and §6.2 block work; the rest can be
defaulted and revisited.

1. ~~**Panel form**~~ — settled 2026-08-04, see §5.3.
2. **Warm-up mechanism** — a separate warm-up phase followed by a fixture reset, or per-cell
   warm-up totals carried through to the verifier. The first is simpler and discards the
   warm-up traffic entirely; the second keeps it reconcilable but changes the report schema
   the verifier consumes.
3. **Sweep matrix size** — the budget is 1.5 days for sweeps, controls *and* report. A
   proposed bounded matrix is in §7; it needs a sanity check against how long one cell
   actually takes before it is trusted.
4. **Recommended-operating-capacity margin** — `measurement-contract.md` §3 defines it as "a
   conservative cap below SLO-safe capacity" without fixing the fraction. PR2 has to pick one
   and label it `[HYPOTHESIS]`, or defer the value with evidence.

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

## 8. Not in PR2

Replica scaling, Kubernetes, AWS, rich dashboards, alerting, and the optional synchronized
release wave (§5.4) — all per the plan. Also not in PR2: any published capacity number, which
requires the separate generator compute of §10.
