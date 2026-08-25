# AG-Sept PR1 — Measurement substrate and load harness

**Type:** Implementation record
**Status:** merged. Exit gate discharged (§1); artifacts in
[`pr1-smoke-run/`](../../measurements/pr1-smoke-run/) and
[`pr1-telemetry-overhead/`](../../measurements/pr1-telemetry-overhead/).

**Owner documents.** [`measurement-contract.md`](../../design/measurement-contract.md) owns the
run manifest, reconciliation and the quotability ladder;
[`load-harness.md`](../../operations/load-harness.md) §4 owns the operator-facing ladder and its
per-level field lists;
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) owns the
controlled workloads and negative controls.

**Reading the section references below.** A bare `§n` refers to
[`milestone-plan-v0.4.md`](../../planning/ag-sept/milestone-plan-v0.4.md), the plan in force when
PR1 was written, unless another document is named on the line.

## 1. Exit gate, and what discharged it

From the plan, unchanged:

> One controlled local run produces reconcilable machine-readable client, server, and
> persisted-state totals; the mandatory response-validation control passes; the manifest
> carries every field the generator can determine for itself; and generator and telemetry
> behaviour are observable.

**The third clause was missing from an earlier revision of this quote.** Recorded rather than
silently corrected: it is the clause the review round of 2026-08-03 turned on, and **a scope note
that misquotes its own exit gate cannot be used to check the gate.**

`[MEASURED]` — discharged by four separate artifacts rather than by one run that passed, retained
in [`pr1-smoke-run/`](../../measurements/pr1-smoke-run/): client, server and persisted state each
independently report 60; `-validate=false` produced a not-quotable run with both binaries exiting
1, so the generator's refusal is carried forward rather than overridden by a clean reconciliation;
generator CPU was 0.039 per core; and the run certifies at `local`, which is the intended outcome
rather than a shortfall.

**What this run is not.** A smoke run of 60 requests on one host with the generator co-resident.
It proves the substrate works end to end; it establishes no capacity, and no number in it may be
quoted as one.

## 2. Implementation decisions that mattered

### 2.1 The verifier is a separate binary, and the scrape is passed to it as a file

`cmd/alloca-verify` queries the database directly, so the load generator holds **no database
credentials** and stays genuinely external.

The scrape is passed as a file rather than fetched by the verifier, and the second reason is the
load-bearing one: **these counters are cumulative and never reset**, so a scrape taken whenever the
verifier happens to run is not the scrape that describes the run being verified. Capturing it as a
step of the run makes the artifact and the moment it was taken the same thing.

### 2.2 Quotability is a level, not a boolean

The harness first recorded `quotable: true|false`, and that could not be answered honestly: a
*capacity* claim is gated on topology provenance and a *publishable* one on the generator running
off the service host, so **"is this quotable?" has no answer until the claim is named.** What
surfaced it was a smoke run with an empty commit SHA that nonetheless reported `quotable: true`.

The ladder that replaced it is owned by `load-harness.md` §4. Two things about it belong here.
**Levels are named for the claim rather than for the PR that first reaches them**, because a report
in `docs/measurements/` outlives the schedule. And both failure modes PR1 newly enforces against
were reachable before: the documented operator path used `go run`, which does not stamp VCS data,
and **the manifest test checked that each JSON key was *present* rather than *populated***, so an
empty string passed.

### 2.3 Service provenance is read from `/meta`, not from the generator

The manifest's commit SHA was read from `buildinfo.Collect` inside `alloca-load`, so the field
documented as the identity of the code under test named the **generator's** binary. A service left
running from one commit while the harness is rebuilt from another — the ordinary state of a working
session — then produces a report naming a commit that was never measured. **That is worse than the
empty SHA §2.2 fixed, because the value is populated and looks trustworthy**, and §2.2 had just
made it load-bearing. The two are now recorded under names that cannot be confused.

**This also moved three fields out of the operator's hands**, since they come from the same `/meta`
fetch and are discovered rather than transcribed: **a typo in a transcribed value is
indistinguishable from a measurement.**

**Consequence for the operator path.** `make dev` serves via `go run`, so no run against it can be
certified; `make dev-measured` builds the service first. `dev` is deliberately unchanged, because
`go run` is the right default for development, where nothing records provenance. The residual
limitation — `/meta` is read once, so the identity means "the service behind the target when the
run began" — is **DEBT-3**.

## 3. Findings that changed the implementation or method

### 3.1 Benchmarking against `io.Discard` priced out the only part the service cannot avoid

`[MEASURED]` — [`pr1-telemetry-overhead/`](../../measurements/pr1-telemetry-overhead/) owns the
figures. The `Tee` the service runs costs **964 ns against `io.Discard` and 1793 ns against a real
file**; an earlier revision measured only against `io.Discard` and quoted that as the cost of what
the service runs, **removing the one part of emission the service cannot avoid — a synchronous
write to a descriptor.** The `io.Discard` figure is kept because isolating formatting from the sink
is still the right way to see *where* the cost is; it is no longer the number the conclusion rests
on.

**The conclusion survived the correction, and that is the reason to state it plainly:** the earlier
number was wrong by a factor of 1.9 and would have led to the same decision, so the reason to fix
it is that the next measurement it feeds might not be so forgiving. On the corrected figure the
asynchronous sink stays deferred **on evidence rather than on the assumption it was originally
deferred on**.

**The numbers in this paragraph were themselves wrong until 2026-08-03** — an earlier revision
quoted a spread matching neither its own table nor the artifact both were drawn from. Left recorded
rather than quietly corrected, because a paragraph arguing against quoting from memory is the worst
place to quote from memory.

### 3.2 A healthy-sink microbenchmark cannot bound backpressure, and does not discharge the gate

The risk `observability.md` §5.1 actually names is **backpressure** — a stalled reader holding the
request after its transaction has committed — and no benchmark of a healthy sink can bound it. What
is established is narrower than "synchronous emission is safe": emission is not *inherently*
expensive, so if a later run shows latency the observation path explains, **the cause is a stalled
sink and the fix is the asynchronous one, not a cheaper encoder.**

**A per-call cost measured in isolation also does not demonstrate that telemetry is not the
request-path bottleneck under load.** PR1 does not do that comparison and does not claim the gate
is discharged; what it discharges is the *decision* the gate exists to gate — whether to build the
asynchronous sink now.

### 3.3 Two counts agreeing out of three is not the rule

An earlier revision of the verifier consumed only the client report and the database, so the server
column was compared by eye and **a run could have been certified with the scrape disagreeing or
absent.** The verifier now takes the scrape as an input and compares cell by cell; a run supplied
with no scrape is not quotable. That is why §1's reconciliation is evidence rather than an
assertion.

## 4. Evidence boundary

PR1 produces the substrate and one smoke run, not a result, and quotes no capacity number.

`publishable` is out of reach for the whole milestone: the separate generator compute it requires
was withdrawn from AG-Sept, so the rule is honoured by labelling rather than by satisfying it.
PR1's own runs sit at `local` for a different reason — the deployment provenance above that level
was not yet populated.
