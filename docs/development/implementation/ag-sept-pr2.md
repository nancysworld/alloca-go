# AG-Sept PR2 — Single-instance frontier

**Type:** Implementation record
**Status:** merged. Results and the frontier report are in
[`pr2-frontier/`](../../measurements/pr2-frontier/) and
[`ag-sept-pr2-single-instance-frontier.md`](../../measurements/reports/ag-sept-pr2-single-instance-frontier.md).

**Owner documents.** [`measurement-contract.md`](../../design/measurement-contract.md) §3 and §6
own the capacity vocabulary and the required indicators; VAL-NEG-2 in
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) owns the
generator control. The results are owned by the frontier report.

**Reading the section references below.** A bare `§n` refers to
[`milestone-plan-v0.4.md`](../../planning/ag-sept/milestone-plan-v0.4.md), the plan in force when
PR2 was written, unless another document is named on the line — except within §5, where a bare
`§5.n` is this record's own subsection. Section numbers are preserved because other documents cite
into them; the gaps at §2, §6 and §7 are sections removed during the 2D compression pass.

## 1. Exit gate and outcome

From the plan, verbatim:

> each controlled workload has a repeatable one-instance result; the generator is ruled out;
> the time series expose or tightly bound the limiting mechanism; and the recommended
> operating point is reported or explicitly deferred with evidence.

The fourth clause decides whether PR2 succeeds honestly: **a frontier PR2 cannot resolve is a
valid outcome, provided the bound is stated and shown. An unresolved frontier reported as a number
would be the failure.**

| Clause | Status |
|---|---|
| repeatable one-instance result per workload | **met** — dispersed, hot_slot and hot_identity each run twice, spreads reported |
| the generator is ruled out | **met** — VAL-NEG-2 control, throughput flat across a 10× change in generator compute |
| time series expose or tightly bound the limiting mechanism | **met** — pool saturation identified and confirmed by a pool-size ladder |
| recommended operating point reported **or explicitly deferred with evidence** | **deferred with evidence** — §5.6 |

**The honest summary is that PR2 identified *what* limits this deployment and bounded *how much*,
but did not resolve a capacity figure** — the outcome the exit gate was written to permit. Two
things stopped a number being defensible, and both are stated rather than worked around: at one
replica only one of the margin's three components is meaningful (§5.6), and the environment moved
throughput by ~2× for reasons outside the harness.

**What PR2 may not claim, whatever it measures.** The generator shares a host with the service for
the whole of AG-Sept, so every figure is a *bounded local* result and that limitation travels with
each number rather than sitting in a footnote.

## 3. What PR1 changed about PR2's scope

**Three of the five service-shape fields already populate themselves.** PR1 made the generator
read the service's `/meta`, which supplies server `GOMAXPROCS`, the timeout budget and the
reservation TTL, and `local` already refuses a run missing any of them. Building operator flags
for those would add a second, hand-typed source for values the service already reports — the
transcription risk PR1 removed.

**PR2 removed the operator from the remaining two** (maintainer decision, 2026-08-03): the service
holds the connection, so `/meta` now carries a `database` block and the manifest fills
`postgres_version` and `pool_size_per_replica` from it. That takes PR2's operator-supplied field
count to **zero**. The version is read once at startup rather than per request — it cannot change
under a live pool, and **a `/meta` that issued a database round trip would fail exactly when the
database is the thing under pressure.** `aggregate_pool_size` is the one pool fact a process
cannot know, since the aggregate needs a replica count; it stayed with the topology in PR3.

**The signal list was already complete**, so PR2 built panels over existing instrumentation rather
than adding any. Two gaps are named rather than quietly redefined: **generator utilisation** is a
short-lived per-cell process with no scrape endpoint, so its CPU is a scalar in `run.json` exported
into the cell's CSVs — a pushgateway for one number per cell would be more observability platform
than the scope allows; and **database utilisation**, as distinct from pool pressure, has no source
at all, which is a place where the plan's own two sections disagreed about what was required.

## 4. Obligations PR1 deferred into this PR

**Warm-up had to become quotable, not merely allowed.** `internal/loadgen/run.go` refused any run
whose warm-up discarded a response, because the discarded responses leave rows the client totals
no longer mention and reconciliation would then fail a *correct* service. Sweeps need warm-up: a
cold pool and an unwarmed process put their startup cost in the first cell of every sweep, which
is exactly where a frontier curve is read. Mechanism in §5.4.

**DEBT-3's trigger fired here.** A sweep restarts the service between cells by design, so the
window for an unnoticed identity change is wider than in PR1's single run; the fix is to fetch
`/meta` before *and* after each run and require the identity to match.

**The telemetry switch was added with three modes** (maintainer decision, 2026-08-03) —
`full` (slog + Prometheus), `metrics_only` (Prometheus), `off` (none). `metrics_only` is the arm
the comparison uses, because PR1 already measured where the cost is: 1793 ns for the Tee against a
real file against 136 ns for the Prometheus recorder alone, so the log write is essentially all of
it and this arm isolates the cost that matters while leaving the run reconcilable.

**`off` refuses itself, deliberately.** With no aggregate series there is no server-side count, so
three-way agreement has only two — **and a verdict reached on two of three is the weaker gate
wearing the stronger one's name.** The manifest gate rejects it by name rather than letting it
surface as a confusing "server counted 0". It is a control, not a measurement, exactly as
`-validate=false` is treated. The mode is recorded in the manifest as provenance, because a
throughput figure measured with logging disabled is not comparable to one measured with it on and
nothing downstream could otherwise tell them apart.

## 5. Decisions and findings

### 5.1 Sweeps are driven by a version-controlled script, not by hand

A cell is seed → restart → load → scrape → verify, and a missed restart contaminates a scrape.
Hand-driving a matrix would reproduce that failure at scale, and **a contaminated cell looks like
a result rather than an error.**

### 5.2 Pool size varies by DSN, not by new configuration

`pool_max_conns` is already a DSN parameter, so varying it needs no code and no new environment
variable — and it keeps the value the manifest records and the value the pool used the same string.

### 5.3 Grafana is the diagnostic view; CSVs and a retained snapshot are the evidence

Maintainer decision, 2026-08-03. Grafana is what you watch while a sweep runs, deliberately small
— **no alerts, no plugins, no template variables beyond what the panels need**, because those are
what turn a diagnostic aid into a general observability platform. The snapshot exporter is what
the report quotes: **a screenshot is not re-derivable; a CSV beside the snapshot it came from is.**
Both read the same query source, so the panel you looked at and the number you quoted cannot drift
apart. The panel set itself is owned by [`dashboards.md`](../../operations/dashboards.md).

### 5.4 Warm-up is a separate phase, and the fixture is reset without restarting the service

Maintainer decision, 2026-08-03. The cell sequence is:

```text
seed  →  start/restart service  →  warm up  →  reset fixture (service keeps running)
      →  measured load  →  export  →  verify
```

Warm-up traffic never reaches reconciliation, because the reset clears the rows it created before
the measured phase begins — which is what makes this the cheaper of the two mechanisms, since the
alternative carried per-cell warm-up totals through to the verifier and changed the schema every
retained artifact is read with.

**The service is deliberately not restarted after warm-up**, and that is the point of the sequence
rather than an omission: restarting would discard exactly what warm-up establishes — a filled
connection pool, a settled heap, and whatever the runtime has already optimised. **A warm-up
followed by a restart measures a cold service again.**

**What that costs, and the change it forces.** Prometheus counters are cumulative and only a
process restart zeroes them, so a scrape taken after the measured phase carries the warm-up
requests too, while `run.json` describes the measured phase alone. The client/server comparison
must therefore move from an absolute count to a **delta between two scrapes**, one captured after
the reset and one after the load. That is a change to `alloca-verify`'s interface and to the
arithmetic of the check, and it is the price of keeping the service warm.

It is also an improvement beyond warm-up: PR1's operator guide had to insist on restarting the
service before *every* run precisely because the check read absolute counters, and a delta removes
that requirement along with the whole class of contaminated-scrape failures.

`[MEASURED]` — a warmed cell's scrapes read 11,480 before the measured phase and 76,379 after
against a client-reported 64,899, and 76,379 − 11,480 = 64,899 **to the request**. Without the
baseline the check compares 76,379 against 64,899 and fails a correct service.

### 5.5 Cells are bounded by duration, and the fixture must be sized to the window

`-n` is the wrong bound for a sweep: it makes a cell's length vary *inversely* with throughput, so
**the fewest samples land at exactly the operating points a frontier is read from**, and no two
cells cover the same interval. PR1's 60-iteration smoke run finished in 0.06 s, which no `rate()`
window can resolve. `-duration` bounds by wall clock instead, and workers finish the unit they are
on rather than the window cancelling requests in flight — a context deadline would turn the tail
of every cell into timeouts the harness caused itself, at exactly the load where real timeouts
matter.

**Fixture sizing then becomes a sweep parameter, and this is the trap.** A 10 s cell at
concurrency 16 completed 24,296 requests but goodput stopped at exactly 10,000 — the fixture's
capacity. The remaining ~60% of the window measured *refusal* throughput, not booking throughput,
**and nothing in the run said so, because refusals are a valid domain answer.** An
iteration-bounded run hid this by being too short to exhaust anything. The rule: **a cell whose
goodput plateaus at exactly the fixture capacity is measuring the fixture, not the service**, and
the runner should refuse it rather than report it.

Cell overhead was measured at ~9–11 s fixed plus warm-up plus window, so a few hundred cells is
hours rather than days. **The binding constraint on matrix size is analysis and report effort, not
runtime** — the matrix should be cut for interpretability, not for wall clock.

### 5.6 The recommended-operating-capacity number is deferred

Maintainer decision, 2026-08-03. `measurement-contract.md` §3 defines the term as a cap reserving
headroom for **variance, rolling deployment, and loss of one unit**. At one replica only variance
is measurable: a roll is a full outage and losing the only unit is total loss, so no margin covers
either.

**A single-instance figure could therefore cover one of the three components the definition names,
while a reader would reasonably take it to cover all three.** Choosing a fraction would not fix
that — it would hide it behind a plausible number, which is the failure this project keeps
catching. PR2 defers the headline number and says why, which the exit gate explicitly allows.

The obligation that travelled to the next PR is the **measured variance** of repeated cells at the
operating point, reported as a named component with its sample count — the one component that is
meaningful at one replica, and needed the moment a second replica makes the other two computable.

#### 5.6.1 Amended 2026-08-04 during review: only the *term* is deferred

The reasoning above stands, but as first written §5.6 let PR2 report a peak and stop — **and a
report that defers its headline is very easy to read as having found nothing.** The correction:
the deferral covers rolling deployment and loss of one unit; **it never covered variance, and it
must not be allowed to swallow the capacity question itself.**

PR2 therefore reports, in its own right, the measured ceiling of this machine and the plateau
demonstrating it is a ceiling rather than the corner of the grid that had been explored; what
decides that ceiling — PostgreSQL's write-ahead log, with the service at 12% CPU and the pool no
longer binding; the variance component at that ceiling; and the falsifiable prediction for the
multi-instance work that follows from it. The contract's *recommended operating capacity* term
stays deferred under its own name, **so the vocabulary is not quietly redefined to make a number
reportable.**

### 5.7 The same behaviour reads as two different goodput figures

`[PRIOR-UNREPRODUCED]` — the first four-cell sweep ran before artifact retention, so its numbers
cannot be re-derived and the [frontier report](../../measurements/reports/ag-sept-pr2-single-instance-frontier.md)
owns the results. One difference it left behind will otherwise look like a contradiction: this
record's `hot_slot` goodput is 5/s and the report's is 1.7/s. Both are the same behaviour —
**goodput is the contended slot's capacity ÷ the window**, so capacity 50 gives 5/s over a 10 s
window and 1.7/s over the report's 30 s. Neither is wrong; they are different windows, and a
goodput figure without its window is not comparable to anything.

### 5.8 Every figure is labelled against its artifact

`measurement-contract.md` §2 and §5 already require this, and **PR1's own scope note still managed
to quote a range that matched neither its table nor its artifact.** PR2's report therefore derives
every quoted number from a retained file by script, and the script is part of the deliverable.

## 8. Evidence boundary

Every figure carries its `measurement-contract.md` §2 label. No externally presented capacity
number is claimed: that requires the separate generator compute of `measurement-contract.md`
§13.1, which AG-Sept does not have, so `publishable` is out of reach for the whole milestone.
PR2's runs sit at `local`.
