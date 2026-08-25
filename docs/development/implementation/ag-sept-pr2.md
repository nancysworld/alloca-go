# AG-Sept PR2 — Single-instance frontier

**Type:** Implementation record
**Status:** merged. Results are owned by
[`ag-sept-pr2-single-instance-frontier.md`](../../measurements/reports/ag-sept-pr2-single-instance-frontier.md),
with artifacts in [`pr2-frontier/`](../../measurements/pr2-frontier/).

**Scheduled under:** [`ag-sept/milestone-plan.md`](../../planning/ag-sept/milestone-plan.md) §3,
work unit *PR1–PR2 — measurement substrate and first frontier*. That is a scheduling and historical
citation only; what this work had to satisfy is owned by the documents below. PR2 was planned under
the superseded [`milestone-plan-v0.4.md`](../../planning/ag-sept/milestone-plan-v0.4.md) §14, which
remains the referent for the bare `§n` references in this record.

**Owner documents.** [`measurement-contract.md`](../../design/measurement-contract.md) §3 and §6
own the capacity vocabulary and the required indicators; VAL-NEG-2 in
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) owns the
generator control; [`dashboards.md`](../../operations/dashboards.md) owns the panel set. Where this
record disagrees with an owning document, the owning document wins.

**Reading the section references below.** A bare `§n` refers to
[`milestone-plan-v0.4.md`](../../planning/ag-sept/milestone-plan-v0.4.md), the plan in force when
PR2 was written, unless another document is named on the line — except within §5, where a bare
`§5.n` is this record's own subsection. Section numbers are preserved because other documents cite
into them; the gaps at §2, §6 and §7 are sections removed during the 2D compression pass.

## 1. Outcome

All four exit-gate clauses are met, the fourth by its second branch: **the recommended operating
point is explicitly deferred with evidence** (§5.6), which the gate allows. **A frontier PR2 cannot
resolve is a valid outcome, provided the bound is stated and shown; an unresolved frontier reported
as a number would be the failure.**

**PR2 identified *what* limits this deployment and bounded *how much*, but did not resolve a
capacity figure.** Two things stopped a number being defensible, and both are stated rather than
worked around: at one replica only one of the margin's three components is meaningful (§5.6), and
the environment moved throughput by ~2× for reasons outside the harness.

**What PR2 may not claim, whatever it measures.** The generator shares a host with the service for
the whole of AG-Sept, so every figure is a *bounded local* result, and that limitation travels with
each number rather than sitting in a footnote.

**Report:**
[`ag-sept-pr2-single-instance-frontier.md`](../../measurements/reports/ag-sept-pr2-single-instance-frontier.md)
owns the measured frontier, the limiting mechanism and the reported variance. Retained artifacts
are in [`pr2-frontier/`](../../measurements/pr2-frontier/).

## 3. What PR1 changed about PR2's scope

PR1's `/meta` work had already removed the operator from three of the five service-shape fields,
and PR2 removed them from the remaining two (maintainer decision, 2026-08-03), taking PR2's
operator-supplied field count to **zero**. The field list itself is owned by
`measurement-contract.md` §11. Two implementation points survive it:

- the database version is read **once at startup** rather than per request — it cannot change under
  a live pool, and **a `/meta` that issued a database round trip would fail exactly when the
  database is the thing under pressure**;
- `aggregate_pool_size` is the one pool fact a process cannot know, because the aggregate needs a
  replica count. It stayed with the topology in PR3.

**Two required outputs had no series behind them**, and are named as gaps rather than quietly
redefined. **Generator utilisation** is a short-lived per-cell process with no scrape endpoint, so
its CPU is a scalar in `run.json` exported into the cell's CSVs — a pushgateway for one number per
cell would be more observability platform than the scope allows. **Database utilisation**, as
distinct from pool pressure, had no source at all, which is a place where the plan's own two
sections disagreed about what was required.

## 4. Obligations PR1 deferred into this PR

**Warm-up had to become quotable, not merely allowed.** The harness refused any run whose warm-up
discarded a response, because those responses leave rows the client totals no longer mention and
reconciliation would then **fail a *correct* service**. Sweeps need warm-up: a cold pool and an
unwarmed process put their startup cost in the first cell of every sweep, which is exactly where a
frontier curve is read. Mechanism in §5.4.

**DEBT-3's trigger fired here**: a sweep restarts the service between cells by design, so the window
for an unnoticed identity change is wider than in PR1's single run.

**The telemetry switch was added with three modes** (maintainer decision, 2026-08-03) — full,
metrics-only, and off. Metrics-only is the arm the comparison uses, because PR1 had already
measured that the log write is essentially all of the cost, so that arm isolates what matters while
leaving the run reconcilable.

**`off` refuses itself, deliberately.** With no aggregate series there is no server-side count, so
three-way agreement has only two — **and a verdict reached on two of three is the weaker gate
wearing the stronger one's name.** The manifest gate rejects it by name rather than letting it
surface as a confusing "server counted 0". It is a control, not a measurement. The mode is recorded
in the manifest as provenance, because a throughput figure measured with logging disabled is not
comparable to one measured with it on, and nothing downstream could otherwise tell them apart.

## 5. Decisions and findings

### 5.1 Sweeps are driven by a version-controlled script, not by hand

A cell is seed → restart → load → scrape → verify, and a missed restart contaminates a scrape.
Hand-driving a matrix would reproduce that failure at scale, and **a contaminated cell looks like a
result rather than an error.**

### 5.2 Pool size varies by DSN, not by new configuration

`pool_max_conns` is already a DSN parameter, so varying it needs no code and no new environment
variable — and it keeps the value the manifest records and the value the pool used the same string.

### 5.3 Grafana is the diagnostic view; CSVs and a retained snapshot are the evidence

Maintainer decision, 2026-08-03. Grafana is what you watch while a sweep runs, deliberately small —
**no alerts, no plugins, no template variables beyond what the panels need**, because those are
what turn a diagnostic aid into a general observability platform. The snapshot exporter is what the
report quotes: **a screenshot is not re-derivable; a CSV beside the snapshot it came from is.** Both
read the same query source, so the panel you looked at and the number you quoted cannot drift apart.

### 5.4 Warm-up is a separate phase, and the fixture is reset without restarting the service

Maintainer decision, 2026-08-03. The cell sequence is:

```text
seed  →  start/restart service  →  warm up  →  reset fixture (service keeps running)
      →  measured load  →  export  →  verify
```

Warm-up traffic never reaches reconciliation, because the reset
clears the rows it created before the measured phase begins — the cheaper of the two mechanisms,
since the alternative carried per-cell warm-up totals through to the verifier and changed the
schema every retained artifact is read with.

**The service is deliberately not restarted after warm-up**, and that is the point of the sequence
rather than an omission: restarting would discard exactly what warm-up establishes — a filled
connection pool, a settled heap, and whatever the runtime has already optimised. **A warm-up
followed by a restart measures a cold service again.**

**What that costs, and the change it forces.** Prometheus counters are cumulative and only a process
restart zeroes them, so a scrape taken after the measured phase carries the warm-up requests too,
while `run.json` describes the measured phase alone. The client/server comparison must therefore
move from an absolute count to a **delta between two scrapes**. That is a change to the verifier's
interface and to the arithmetic of the check, and it is the price of keeping the service warm.

It is also an improvement beyond warm-up: the operator guide had to insist on restarting before
*every* run precisely because the check read absolute counters, and a delta removes that
requirement along with the whole class of contaminated-scrape failures.

`[MEASURED]` — a warmed cell's scrapes read 11,480 before the measured phase and 76,379 after,
against a client-reported 64,899, and 76,379 − 11,480 = 64,899 **to the request**. Without the
baseline the check compares 76,379 against 64,899 and fails a correct service.

### 5.5 Cells are bounded by duration, and the fixture must be sized to the window

`-n` is the wrong bound for a sweep: it makes a cell's length vary *inversely* with throughput, so
**the fewest samples land at exactly the operating points a frontier is read from**, and no two
cells cover the same interval. PR1's 60-iteration smoke run finished in 0.06 s, which no `rate()`
window can resolve. Bounding by wall clock instead, workers finish the unit they are on rather than
the window cancelling requests in flight — **a context deadline would turn the tail of every cell
into timeouts the harness caused itself, at exactly the load where real timeouts matter.**

**Fixture sizing then becomes a sweep parameter, and this is the trap.** A 10 s cell completed
24,296 requests but goodput stopped at exactly 10,000 — the fixture's capacity. The remaining ~60%
of the window measured *refusal* throughput, not booking throughput, **and nothing in the run said
so, because refusals are a valid domain answer.** An iteration-bounded run hid this by being too
short to exhaust anything. The rule: **a cell whose goodput plateaus at exactly the fixture capacity
is measuring the fixture, not the service**, and the runner should refuse it rather than report it.

Cell overhead was measured at ~9–11 s fixed plus warm-up plus window, so **the binding constraint on
matrix size is analysis and report effort, not runtime** — the matrix should be cut for
interpretability, not for wall clock.

### 5.6 The recommended-operating-capacity number is deferred

Maintainer decision, 2026-08-03. `measurement-contract.md` §3 defines the term as a cap reserving
headroom for variance, rolling deployment, and loss of one unit. At one replica only variance is
measurable: a roll is a full outage and losing the only unit is total loss.

**So a single-instance figure could cover one of the three components the definition names, while a
reader would reasonably take it to cover all three.** Choosing a fraction would not fix that — it
would hide it behind a plausible number, which is the failure this project keeps catching.

The obligation that travelled onward is the **measured variance** of repeated cells at the operating
point, reported as a named component with its sample count — the one component meaningful at one
replica, and needed the moment a second replica makes the other two computable.

#### 5.6.1 Amended 2026-08-04 during review: only the *term* is deferred

The reasoning above stands, but as first written §5.6 let PR2 report a peak and stop — **and a
report that defers its headline is very easy to read as having found nothing.** The correction: the
deferral covers rolling deployment and loss of one unit; **it never covered variance, and it must
not be allowed to swallow the capacity question itself.**

PR2 therefore reports in its own right the measured ceiling of this machine and the plateau
demonstrating it is a ceiling rather than the corner of the grid that had been explored, what
decides that ceiling, the variance component, and the falsifiable prediction for the multi-instance
work that follows. The contract's term stays deferred under its own name, **so the vocabulary is not
quietly redefined to make a number reportable.**

### 5.7 The same behaviour reads as two different goodput figures

`[PRIOR-UNREPRODUCED]` — the first four-cell sweep ran before artifact retention, so its numbers
cannot be re-derived and the frontier report owns the results. One difference it left behind will
otherwise look like a contradiction: this record's `hot_slot` goodput is 5/s and the report's is
1.7/s. Both are the same behaviour — **goodput is the contended slot's capacity ÷ the window**, so
capacity 50 gives 5/s over a 10 s window and 1.7/s over the report's 30 s. Neither is wrong; **a
goodput figure without its window is not comparable to anything.**

### 5.8 Every figure is labelled against its artifact

`measurement-contract.md` §2 and §5 already require this, and **PR1's own scope note still managed
to quote a range that matched neither its table nor its artifact.** PR2's report therefore derives
every quoted number from a retained file by script, and the script is part of the deliverable.

## 8. Evidence boundary

Every figure carries its `measurement-contract.md` §2 label. No externally presented capacity number
is claimed: that requires separate generator compute, which AG-Sept does not have, so `publishable`
is out of reach for the whole milestone and PR2's runs sit at `local`.
