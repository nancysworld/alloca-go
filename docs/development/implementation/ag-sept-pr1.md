# AG-Sept PR1 — Measurement substrate and load harness

**Type:** Implementation record
**Status:** Shipped and merged — every decision in §3 settled, exit gate discharged in §3.6
**Budget:** 2 development days (scheduled under
[`ag-sept-plan-v0.4.md`](../../planning/ag-sept-plan-v0.4.md) §14)
**Owner docs:** [`measurement-contract.md`](../../design/measurement-contract.md) — run manifest,
reconciliation, and quotability levels — and
[`ag-sept-validation-plan.md`](../../test/validation-plan/ag-sept-validation-plan.md) — controlled
workloads and negative controls — are normative for what this PR built. (PR1 was planned under
`ag-sept-plan-v0.4.md` §6, which carried those rules before they were migrated to their durable
owners.) This record covers only how PR1 discharged them and the choices settled along the way.

**Reading the section references below.** A bare `§n` in this record refers to
`ag-sept-plan-v0.4.md`, the plan in force when PR1 was written, unless another document is named
on the line. Those plan sections have since been migrated to the durable owners named above; the
bare references are retained because this record is a dated account of what the work was measured
against, not a current contract.

## 1. Exit gate

From the plan, unchanged:

> One controlled local run produces reconcilable machine-readable client, server, and
> persisted-state totals; the mandatory response-validation control passes; the manifest
> carries every field the generator can determine for itself; and generator and telemetry
> behaviour are observable.

Four things have to be true together. The first is what constrains the design: the run must
reconcile **client totals, server totals, and persisted state**, so something in the harness
needs to read the database after a run.

The third clause was missing from an earlier revision of this quote, which is worth recording
rather than silently correcting — it is the clause the review round of 2026-08-03 turned on,
and a scope note that misquotes its own exit gate cannot be used to check the gate.

## 2. What PR1 delivers

| # | Deliverable | Reference |
|---|---|---|
| 1 | Aggregated metrics recorder on the existing observation boundary, bounded label sets only | `observability.md` §2, §2.1; `measurement-contract.md` §6 |
| 2 | Per-call cost of synchronous telemetry against a real sink, deciding whether the asynchronous sink is built now. The end-to-end comparison under load is PR2's | VAL-NEG-3 |
| 3 | External load generator: workload shapes, closed-loop concurrency, synchronized start, valid idempotent requests, client-side outcome capture, machine-readable summary, own utilisation | `measurement-contract.md` §5, §13.1; validation plan §3 |
| 4 | Run manifest emitted with every run and **enforced**: every field the generator determines for itself, plus everything the service reports at `/meta`. The fields no endpoint reports stay operator-supplied and staged to PR2–PR4 | measurement-contract §11 |
| 5 | Correctness reconciliation used by every later run | measurement-contract §12 |
| 6 | Response-validation-active negative control | VAL-NEG-1, `measurement-contract.md` §5 item 5 |
| 7 | One controlled local smoke run exercising all of the above | `ag-sept-plan-v0.4.md` §14 PR1 |

Deliverable 6 is mandatory and not descopable: `measurement-contract.md` §5 item 5 requires a
control that **fails when response validation is silently disabled**, so a reported success
cannot be an unchecked `200`.

## 3. Decisions (Nancy's call)

§3.1–3.3 were settled 2026-07-31, before implementation, because each adds a dependency, a
binary or an endpoint. §3.4 and §3.5 were settled 2026-08-03 during review, and each carries
its own date — both changed a schema the later PRs consume, so they are recorded here rather
than left in a thread.

### 3.1 Metrics backend — `prometheus/client_golang`

The plan calls a Prometheus-compatible recorder "the preferred starting point" (§6.1), and
this is the first observability runtime dependency. Chosen over a hand-rolled recorder
because histogram bucketing and registry machinery are not where PR1's two days should go,
and because it scrapes cleanly under `kind`/EKS later without a second format.

Label sets stay bounded, per §6.1: `operation`, `outcome`, `replay` and similar closed sets
only — never user, slot, reservation, organisation, idempotency key, request identifier or
error text.

### 3.2 Reconciliation reads persisted state from a separate verifier binary

`cmd/alloca-verify` reads the generator's machine-readable client summary and a saved
`/metrics` scrape, queries the database directly, and emits the reconciliation report of
measurement-contract §12. Chosen so the load generator holds **no database credentials** and
stays genuinely external, which `measurement-contract.md` §13.1 requires for publishable runs.

The scrape is passed as a file (`-metrics`) rather than fetched by the verifier from the
service. Two reasons, and the second is the load-bearing one: the verifier would otherwise
need network reach to the service as well as to the database, and — since these counters are
cumulative and never reset — a scrape taken whenever the verifier happens to run is not the
scrape that describes the run being verified. Capturing it as a step of the run makes the
artifact and the moment it was taken the same thing.

Known wrinkle, carried rather than solved: on AWS the verifier needs network reach to RDS,
so it runs from wherever that is available rather than alongside the generator. Revisit at
PR4 if that proves awkward — a read-only service endpoint remains the fallback.

### 3.3 Telemetry — bounded local sink now, asynchronous sink only if measured to matter

§6.2 requires showing observation is not the request-path bottleneck, not that a particular
mechanism exists. PR1 points structured logs at a bounded local sink and measures request
latency with logging on versus off, reporting the delta as `[MEASURED]`.

If the delta is material, the bounded asynchronous sink (drop policy plus shutdown flush)
follows in PR2. If it is not, `observability.md` §5.1 stays deferred — but deferred on
evidence rather than on assumption, which is the change that matters.

#### Result — the asynchronous sink stays deferred

`[MEASURED]` — raw results:
[`docs/measurements/pr1-telemetry-overhead/raw.txt`](../../measurements/pr1-telemetry-overhead/raw.txt),
environment: [`environment.txt`](../../measurements/pr1-telemetry-overhead/environment.txt).
Source `internal/telemetry/overhead_test.go`; reproduce with:

```console
$ go test ./internal/telemetry/ -bench Recorder -benchmem -run '^$' -count 5
```

Five runs, so the figures below are the **median with the observed range**, not one sample.
Quoted from the artifact rather than from a terminal, per `measurement-contract.md` §5 item 3.

| Recorder | Sink | ns/op (median) | range | B/op | allocs/op |
|---|---|---:|---:|---:|---:|
| `Nop` (call floor) | — | 0.20 | 0.196–0.207 | 0 | 0 |
| `SlogRecorder` | `io.Discard` | 772 | 762–777 | 88 | 2 |
| `SlogRecorder` | **file** | 1566 | 1556–1607 | 88 | 2 |
| Prometheus recorder | — | 136 | 135–138 | 0 | 0 |
| `Tee` | `io.Discard` | 964 | 961–976 | 88 | 2 |
| **`Tee` (what the service runs)** | **file** | **1793** | 1743–1900 | 88 | 2 |
| `Tee`, parallel | `io.Discard` | 227 | 216–231 | 88 | 2 |

**The sink column is the point.** An earlier revision of this record measured only against
`io.Discard` and quoted 994 ns as the cost of what the service runs. That removed the one
part of emission the service cannot avoid: a synchronous write to a descriptor. Pricing it
against a real file roughly doubles the figure — `Tee` goes from 964 ns to 1793 ns, and the
write is the whole of the difference. The `io.Discard` rows are kept because isolating
formatting from the sink is still the right way to see *where* the cost is; they are no
longer the number the conclusion rests on.

`[DERIVED]` — median `Tee` cost against a real sink is 1793 ns, or **≈0.18% of a 1 ms
request** (1793 ns ÷ 1 ms), and less of a slower one. Observation is not the request-path
bottleneck at any load this milestone will reach, so the bounded asynchronous sink is **not
built in PR1**, and `observability.md` §5.1 stays deferred on evidence rather than on the
assumption it was deferred on originally. The conclusion survived the correction, which is
worth stating plainly: the earlier number was wrong by a factor of 1.9 and would have led to
the same decision, so the reason to fix it is that the next measurement it feeds might not be
so forgiving.

The spread is worth noting for what it says about method rather than about telemetry: the
five `Tee`/`io.Discard` samples span 961–976 ns and the five file-sink samples span
1743–1900 ns, so a single sample from either could be quoted several percent away from the
median. One sample would have been quoted as fact. It would not have changed this conclusion,
but the habit it represents is the one that eventually does.

The numbers in this paragraph were themselves wrong until 2026-08-03 — it claimed a span of
980–1004 ns, which matches neither row of the table above nor the artifact both are drawn
from. Left recorded rather than quietly corrected, because a paragraph arguing against
quoting from memory is the worst place to quote from memory.

**What this does not show, and it is the part that matters.** These are microbenchmarks of a
*healthy* sink. Two things stay open, and PR1 does not close either.

The risk `observability.md` §5.1 actually names is *backpressure* — a stalled reader holding
the request after its transaction has committed — and no benchmark of a healthy sink can
bound that. What is established is narrower than "synchronous emission is safe": emission is
not *inherently* expensive, so if a later run shows latency the observation path explains,
the cause is a stalled sink and the fix is the asynchronous one, not a cheaper encoder.
Revisit at the first remote sink, which is when a stall stops being hypothetical.

The second is the §6.2 claim itself. A per-call cost measured in isolation does not
demonstrate that telemetry is not the request-path bottleneck under load: that needs a
workload-level comparison — same dataset, concurrency and environment, telemetry on versus
off, reporting the throughput and p99 delta. **PR1 does not do that, and does not claim
§6.2 is discharged.** What PR1 discharges is the decision §6.2 gates — whether to build the
asynchronous sink now — on the evidence that a healthy sink costs under 0.2% of a request.
The end-to-end comparison belongs with the sweeps that can run it, and was scoped to PR2 in
[`ag-sept-plan-v0.4.md`](../../planning/ag-sept-plan-v0.4.md) §14.

### 3.4 Quotability is a level, not a boolean (settled 2026-08-03)

The harness first recorded `quotable: true|false`. That field could not be answered honestly,
because measurement-contract §11 gates a *capacity* claim on topology provenance and
`measurement-contract.md` §13.1 gates a *publishable*
one on the generator running off the service host — so "is this quotable?" has no answer
until the claim is named. A PR1 smoke run with an empty commit SHA and no service-shape
fields nonetheless reported `quotable: true`, which is what surfaced the problem.

Reports now carry `quotability.level` — `none`, `local`, `capacity`, `publishable` — with the
next level up and what blocks it. Both binaries take `-require` so the bar is declared by the
caller, who is the only one who knows what the number is for. The ladder and the per-level
field lists are in
[`docs/operations/load-harness.md`](../../operations/load-harness.md) §4; the staging they
implement is the plan's staging — `ag-sept-plan-v0.4.md` §14 when this was written, and
`ag-sept-plan.md` §4 now.

Levels are named for the claim rather than for the PR that first reaches them. A report in
`docs/measurements/` outlives the schedule, and `"PR1"` would oblige a later reader to
reconstruct this PR's scope before knowing what the number is good for.

**PR1 runs reach `local`, which is the intended outcome.** What PR1 newly *enforces* is every
field obtainable without an operator: a run is `none`, and exits non-zero, when either
binary's revision is missing or either was built from a modified working tree. Both were
reachable before — the documented operator path used `go run`, which does not stamp VCS data —
and the manifest test checked that each JSON key was present rather than populated, so an
empty string passed.

### 3.5 Service provenance is read from `/meta`, not from the generator (settled 2026-08-03)

The manifest's commit SHA was read from `buildinfo.Collect` inside `alloca-load`, so the field
documented as the identity of the code under test named the *generator's* binary. A service
left running from one commit while the harness is rebuilt from another — the ordinary state of
a working session — then produces a report naming a commit that was never measured. That is
worse than the empty SHA §3.4 fixed, because the value is populated and looks trustworthy, and
§3.4 had just made it load-bearing.

The service publishes its own revision at `/meta`, so the generator reads it there. This does
not weaken §6.3: the generator gains no credentials and no shared state, it asks the service to
describe itself over the contract it already uses to drive load. `internal/buildinfo` says this
is what `/meta` is for.

The two provenances are now recorded under names that cannot be confused —
`service_commit_sha` / `generator_commit_sha`, and likewise for the dirty-tree flag and the Go
version. `local` requires the service's, from a clean tree.

**This moved three fields out of the staging.** `server_gomaxprocs`, `timeout_budget` and
`reservation_ttl` come from the same `/meta` fetch, so they are discovered rather than
transcribed and PR2 no longer supplies them: a value the service already reports is one nobody
should retype, since a typo there is indistinguishable from a measurement. `postgres_version`
and the pool arithmetic stay operator-supplied — no endpoint reports them — along with topology
(PR3) and environment (PR4).

**Consequence for the operator path.** `make dev` serves via `go run`, so `/meta` reports no
revision and no run against it can be certified. `make dev-measured` builds the service first;
`dev` is deliberately unchanged, because `go run` is the right default for development, where
nothing records provenance.

The limitation this leaves is recorded as **DEBT-3** in
[`tech-debts.md`](../../planning/tech-debts.md): `/meta` is read once before the run, so the identity means
"the service behind the target when the run began" rather than a proof that one binary served
the whole sample.

### 3.6 Exit gate — discharged

`[MEASURED]` — artifacts in [`docs/measurements/pr1-smoke-run/`](../../measurements/pr1-smoke-run/):
`run.json` (generator report), `verdict.json` (reconciliation), `metrics.txt` (server scrape).
Local PostgreSQL 16, one replica, dispersed workload, 20 slots, capacity 5, concurrency 8,
60 iterations.

The gate has four clauses, and each is discharged by a separate artifact rather than by one
run that passed:

**1. Client, server and persisted-state totals reconcile.** All three independently say 60:

| Source | Value |
|---|---|
| Client (`run.json`) | `completed_requests: 60`, `successful_mutation_goodput: 60` |
| Server (`metrics.txt`) | `alloca_requests_total{operation="reserve",outcome="admitted_success",replay="false"} 60` |
| Persisted (`verdict.json`) | 60 live reservations, 60 idempotency records, 60 live claims |

All five checks pass. Four name the invariant they exercise — INV-1, INV-5, INV-4, INV-7 —
and the fifth is the client/server comparison, which names none because it is a statement
about instrumentation rather than about the domain.

That fifth check is why the first row of this table is evidence rather than an assertion.
An earlier revision of the verifier consumed only the client report and the database, so the
server column was compared by eye and the run could have been certified with the scrape
disagreeing or absent. `alloca-verify` now takes the scrape as an input (`-metrics`), sums
the `alloca_requests_total` cells, and compares them cell by cell; a run supplied with no
scrape is **not quotable**, because two counts agreeing out of three is not the rule measurement-contract §12
states.

**2. The response-validation control passes.** End-to-end, not only in unit tests:
`-validate=false` against the live service produced a run marked not quotable, `alloca-load`
exited 1, and `alloca-verify` also exited 1 — the generator's refusal is carried forward
rather than overridden by a clean reconciliation.

**3. Generator and telemetry behaviour are observable.** The generator reports its own CPU
utilisation (0.039 per core here — nowhere near saturation, so this run is not
client-limited), and the server exposes request, pool and expiry-worker series on a separate
listener.

**4. The run says what it may back.** `quotability.level` is `local`, and `blocked_because`
names each field standing between it and `capacity` together with the PR that supplies it.
That is the intended outcome for PR1, not a shortfall — see §3.4.

`service_commit_sha` is read from the service's own `/meta` and matches the commit this
evidence was generated from, with `service_source_modified` false — so the run identifies the
binary that actually answered it, not the harness that drove it. `generator_commit_sha` is
recorded separately and happens to be the same commit here, which is the *coincidence* of a
single-commit session rather than the design: the two are distinct facts and the manifest
keeps them apart. See §3.5.

**What this run is not.** It is a smoke run: 60 requests at concurrency 8 on one host, with
the generator and service sharing a machine. It proves the substrate works end to end; it
establishes no capacity, and no number in it may be quoted as one. PR2 measures the
one-instance frontier, still co-resident and therefore still bounded rather than published.

When this was written, v0.4 expected separate generator compute to arrive with the AWS
deployment. **It does not arrive in AG-Sept at all** — that path was withdrawn
(`ag-sept-plan.md` §6.3), so **no AG-Sept run can reach `publishable`** and the
`measurement-contract.md` §13.1 rule is honoured by labelling rather than by satisfying it. That
is the only level co-residency blocks; PR1's own runs sit at `local` because the deployment
provenance above that level was not yet populated, which is a different reason.

## 4. Non-goals

Sweeps, capacity claims, replica counts and any published number belong to PR2 and later.
PR1 produces the substrate and one smoke run, not a result.

## 5. Evidence labels

Every quantitative value this PR emits is `[MEASURED]` from a named artifact or
`[HYPOTHESIS]`, per `measurement-contract.md` §2. PR1 quotes no capacity number.
