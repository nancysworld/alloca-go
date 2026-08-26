# AG-Sept PR3 — Horizontal database authority, Phase 1

**Type:** Implementation record, spanning PR3a/3b/3c
**Status:** merged — PR3a (#13), PR3b (#14), PR3c (#16). Results are owned by
[`ag-sept-pr3c-phase1-correctness.md`](../../measurements/reports/ag-sept-pr3c-phase1-correctness.md),
with artifacts in [`pr3c-phase1/`](../../measurements/pr3c-phase1/).

**Scheduled under:** [`ag-sept/milestone-plan.md`](../../planning/ag-sept/milestone-plan.md) §3,
work unit *PR3a–PR3c + Iteration B A&R — database-authority composition*. That is a scheduling and
historical citation only; what this work had to satisfy is owned by the documents below.

**Owner documents.**
[`horizontal-database-authority.md`](../../design/horizontal-database-authority.md) and
[`deployment-architecture.md`](../../design/deployment-architecture.md) own the Phase 1 model;
REQ-ROUTE-1 and REQ-FAIL-1 own what must be true;
[`measurement-contract.md`](../../design/measurement-contract.md) §11–§12 owns run provenance and
reconciliation; [ADR-0003](../../decisions/0003-deployed-artifact-identity.md) owns deployed
artifact identity; [`api-surface.md`](../../design/api-surface.md) owns the API behaviour;
[`container-topology.md`](../../operations/container-topology.md) owns the operating procedure;
[`ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) owns the
validations. Where this record disagrees with an owning document, the owning document wins.

Section numbers are preserved because other documents cite into them; the gaps at §2 and §4 are
sections removed during the 2D compression pass.

## 1. Outcome

Three separable exit gates, one per work unit — placement and misrouting refusal (PR3a); a
reproducible multi-authority run refused certification when units disagree (PR3b); Phase 1
correctness and failure isolation on independent writable authorities (PR3c). All three are
discharged.

**The clause that decides whether PR3 succeeds honestly is PR3a's last one** — that no accepted
AG-M1 invariant was weakened to make the topology fit. Phase 1 is only worth building if the local
transaction path it composes is the *same* path AG-M1 proved; **a Phase 1 that reached its topology
by relaxing an invariant would have moved the problem, not scaled it.**

**What PR3 may not claim, whatever it measures.** Both authorities, both service units, the
generator and the telemetry stack share one 10-vCPU allocation
([`environment.md`](../../measurements/environment.md)). Authority composition cannot be quoted as a
capacity multiplier from this machine, and PR2's unexplained ~2× excursions remain open, so any
single reading carries a ±2× caveat. **PR3 is correctness-first by design, not by descope.**

**Report:**
[`ag-sept-pr3c-phase1-correctness.md`](../../measurements/reports/ag-sept-pr3c-phase1-correctness.md)
owns the correctness matrix, the per-authority verdicts and the failure-isolation result. Retained
artifacts are in [`pr3c-phase1/`](../../measurements/pr3c-phase1/).

## 3. What the existing code made cheap, and what it did not

Reconciliation was already organisation-scoped, so authority-aware verification was an extension
rather than a rewrite — the requirement being that **one organisation's persisted rows are never
compared against the run's unpartitioned global summary**. The generator, by contrast, was
single-authority throughout, so routing by organisation was the bulk of PR3b's cost. The new refusal
was a closed-set change rather than a constant, touching the domain set, the HTTP mapping, the
invariant register, the outcome taxonomy and the metric label allowlist — **which is why PR3a was
not the smallest of the three.**

### 3.2 Per-authority attribution comes free from per-service scrapes

Client-side totals carry no authority dimension, and adding one would touch the report schema, the
manifest, and every artifact that reads them. It is not needed: every service unit is shard-affine,
so **a unit's own scrape *is* its authority's traffic**. Per-authority client-side attribution would
be a second, weaker source for a fact the topology already supplies.

**One consequence has to be implemented carefully.** With N units, **each unit's baseline/after
scrape pair must be differenced separately and then summed — differencing the sums would mask one
unit restarting mid-run.**

## 5. Decisions taken

### 5.1 Three PRs, not one

The three gates fail independently, and the first is a contract change to a system with a merged,
tested invariant register. Bundling them would mean **the first review of a placement model arrives
alongside a topology and a measurement run, which is how a contract error reaches evidence.**

### 5.2 Routing lives in the generator *and* is enforced by the service

Static routing in the generator is enough for the first experiment. But **if the generator is the
only router, then "no supported request reached the wrong authority" is a property of the
generator's routing table, not of Alloca, and the placement invariant is untested by
construction.** Both, therefore: the generator routes, and each unit refuses what it does not own.
That is why the misrouting control is mandatory rather than desirable.

### 5.5 Three contract questions settled, and one enlarged PR3a

Settled against the formal design, 2026-08-05, which owns the decisions. Two changed the shape of
the work rather than confirming it, and the reasons are implementation's:

1. **The booking policy compares resolved authorities, not organisation identifiers.**
   Cross-organisation booking is shipped, tested behaviour, so an unconditional same-organisation
   policy **would have regressed the current one-authority deployment.** Comparing authorities keeps
   it alive wherever the two organisations are colocated — so colocated cross-organisation booking
   is a success case in the matrix, not only a refusal.
2. **Confirm and cancel gained an explicit `UserRef` ownership check.** The service had none: both
   paths resolved by reservation identifier alone and the caller's `UserRef` only scoped
   idempotency, so a confirm carrying a wrong identity succeeded. Without the check, **sharding
   would have made that outcome depend on whether two organisations happen to be colocated.** It is
   not authentication: a caller who knows both the reservation identifier and its exact owner can
   still act as that owner.
3. **An unavailable authority uses the existing infrastructure classifications**, so the consequence
   lands on the evidence contract rather than the domain: failure-isolation runs report affected and
   unaffected populations separately and are **not judged against the aggregate SLO gates that
   govern a healthy capacity run.**

### 5.6 A misrouted request is `invalid_request` at the edge, with its own counter

Within the closed outcome set the honest candidates were `invalid_request` (400) and
`internal_failure` (500). `invalid_request` wins because it is precisely the outcome INV-7 exempts
from the recording rule, and because **routing identity is caller-asserted until authentication
exists, so `internal_failure` would let a client drive the service's internal-failure rate** — an
SLO-relevant signal. The fault must still be visible, so the refusal increments a dedicated bounded
counter: **a 400 that is really a misconfiguration should not hide among client errors.**

### 5.7 Resolving `unknown_replayable` is a post-run pass, not a load control

`measurement-contract.md` §12 requires ambiguous mutations to be resolved by replaying their own
keys after an authority is restored, so the generator retains those keys during the run and
reissues them afterwards.

**This is deliberately not a retry-on-timeout control**: that shapes load *during* a run — timeout,
retry, amplification — and remains unassigned. Conflating them is how unfunded scope creeps into a
PR that cannot fund it.

## 6. Settled by the freeze, and what implementation found

### 6.1 The ownership correction is an accepted API behaviour change

PR3a's booking half is inert in a single-authority deployment; **its ownership half is not** — a
caller holding a reservation identifier could previously confirm or cancel it while asserting a
different `UserRef`, and after PR3a that returns `404 unknown_target`. A live behaviour change on a
merged contract rather than new functionality, so it was raised for an explicit decision **rather
than allowed to arrive as a side effect of sharding.**

### 6.2 Organisation count and skew — a starting fixture, not a contract

`[HYPOTHESIS]`: four organisations across two authorities, 2:2 by count and deliberately unequal by
load so the one-hot-organisation workload has something to show. A run parameter implementation may
change on evidence, not an architectural commitment — recorded only so the fixture is chosen rather
than defaulted into.

### 6a. What PR3b carried early

Maintainer decision, 2026-08-05. Two items were nominally PR3c's and travelled with PR3b because
they were *done*, and because the fix is a correctness change to merged code that should not wait on
a measurement PR: the **`classifyCommit` ambiguity fix** — operator-intervention and
connection-exception SQLSTATEs are the session ending, not the server answering the `COMMIT`, so
they are `unknown_replayable` rather than definite failures — and the **INV-21 terminated-session
fault test** that found it. **Consequence, accepted rather than overlooked: `main` misclassified a
terminated session under `COMMIT` as a definite failure until PR3b merged.**

**INV-21 is deliberately not claimed whole.** The test destroys the session with the transaction's
work done and `COMMIT` not yet sent, so what is proven is that a commit attempted on a dead session
is classified conservatively. The *acknowledgement-lost* half needs something interposed between
client and server and remains unproven. The one live `unknown_replayable` the experiments produced
does **not** discharge it: it resolved `replay=false`, meaning the original had never committed,
which is the opposite case.

### 6b. The unavailable-authority outcome depends on *how* the authority is unavailable

**The assumption going in** was that an unavailable authority produces one of three outcomes, and
this record previously attributed that enumeration to the formal design. **It is not there.** The
design fixes *containment* and the standing of `unknown_replayable`; it does not fix a closed outcome
set. The three-outcome expectation was implementation's, and stating it as the design's was an
over-attribution.

**What was observed.** Stopping an authority's container produced **`timeout_server` (504)** — a
fourth classification. `/readyz` correctly reported 503 throughout and the healthy authority kept
serving its own organisations, so failure *isolation* held exactly as designed.

**The refinement: the outcome is a property of the failure mode injected, not of authority
unavailability as such.** A *stopped* container drops packets rather than refusing them, so the
connection attempt hangs instead of failing fast and the per-request server deadline is the first
bound to fire; a *killed* container would classify differently. The durable claim is containment
plus the bounded outcome model; the specific outcome is an experiment parameter.

Two consequences: **the experiment must name its failure mode**, because an expected-outcome set
fixed in advance and then met by a different outcome **reads as a defect when it is really a
different experiment**; and **`db_acquire_cap` did not bound this wait** — a ~5 s server deadline
elapsed where a 500 ms acquisition cap might have fired first. That diagnosis is *not* established.
§6e gives it a better lead.

### 6c. Resolution accounting is PR3c's, and PR3b stopped short of it deliberately

Ambiguity resolution replays each ambiguous mutation, and **those replays are real HTTP requests
PR3b accounted for nowhere**. So after a resolution pass the server's counters include the replay
traffic while the client totals still describe only the original run, and three-way reconciliation
over that state cannot be correct.

**The acknowledgement-lost branch is the sharp end.** A resolution proving the original mutation
committed means the database holds one fresh logical mutation — but appending the replay's response
to the totals would leave fresh-mutation count at zero for that logical request, because the replay
itself is not fresh. **The mutation is real and the summary would say it never happened.**

Discharged in PR3c as one contract rather than four patches: resolution traffic incorporated into
request and server accounting; a logical mutation counted **once as fresh** when the replay proves
the original committed; counted **once** when resolution performs it; anything still ambiguous
rejected outright. The measured fields are untouched by construction, which is the property
`measurement-contract.md` §12 turns on.

**Why not in PR3b.** The shape of this contract depends on how the pass is actually driven, and **a
summary contract guessed at before its only caller exists is one PR3c would have had to redesign
while also producing the experiment.** The register's own defect — a failed replay re-registering
itself and growing the outstanding work on every pass — *was* fixed in PR3b; deduplication does not
solve the accounting problem and is not claimed to.

### 6d. The deployed artifact is bound to the routed units, and checked before load

ADR-0003 fixes that code identity and deployed-artifact identity are separate provenance facts and
the second must be observed from outside the measured process. What it deliberately does not fix is
the mechanism — and four parts of it were wrong in a way that read as correct.

**Observation alone is unbound evidence.** A record proving that some containers on this host share
an image says nothing about the units a run addressed: a stack raised yesterday, a second stack on
other ports, or a unit nothing routes to would all satisfy it. Each observation now carries the
address its container publishes, **read from the port binding rather than assumed from the compose
file**, and the run requires exact one-to-one correspondence with the targets it is about to drive.
An observed unit nothing routes to means the record describes a different topology — **and a record
wrong about the unit set is not evidence that its image identity is right either.**

**Ordering is the second half.** The check previously ran *after* the workload, where it could only
annotate numbers that already existed. It now runs before any measured request, so a wrong record
costs a re-record rather than a run — a property held by a test asserting the units served **zero**
requests when the preflight refuses.

**The gate was armed by the record it was gating.** It refuses a containerised run whose `image_id`
is empty, but it was armed by a field the record itself sets, so **omitting the record also disarmed
the gate**, and the run reported a clean lower-provenance result whose missing artifact identity
looked like a choice.

**`-require` is not the exception it looks like.** The requirement first exempted `-require none`,
on the reading that a run declaring it supports no claim has nothing for artifact identity to
qualify. **That reading is wrong about the flag.** `-require` is the floor a run must clear to exit
zero, not a ceiling on what its report claims: certification always computes the highest level the
manifest actually reaches, so a stamped binary passing `-require none` exits zero *and* writes a
report certified at `local` or above, naming no artifact. **The exemption reopened exactly the
bypass the requirement was added to close, under the phrasing an operator is most likely to reach
for.** There is now no exception.

**What was deliberately not built:** no per-unit map in the manifest, since once the preflight
establishes full coverage and one common artifact the per-unit observations are validation evidence
rather than a second representation of what `image_id` already states; and no general
deployment-mode abstraction, because **an abstraction invented before a second caller exists is one
the second caller redesigns.**

### 6e. What building and running PR3c found

In three cases the code was already wrong in a way no test then held.

**A replay that never reached the service was recorded as a resolution.** Resolution asked whether
the replay returned `unknown_replayable` and treated everything else as settled — but a replay
against an authority still down fails at the transport and classifies as a timeout, so the entry was
retired, nothing was reported outstanding, and the run certified over a key whose commit state
nobody had established. **It was also irreversible**: only a parsed `unknown_replayable` re-enters
the register, so retiring on a transport failure destroyed the only record that the mutation needed
replaying. The predicate is now whether a **definite domain answer** came back. *Operational
consequence, belonging in the experiment design rather than the code:* the failure-isolation run
must restore the authority **before the run ends**, because that is when the pass runs.

**Resolution responses were not validated at all**, on the accident that they happen after the
measured interval. A replay answered outside the outcome contract now makes the run unsound. The
measured invalid count is deliberately left alone: that field describes the measured interval.

**A partially scraped topology was reported as a service defect.** A unit whose scrape is missing
simply lowers the summed total — **arithmetically identical to a service that dropped requests** —
and the message sent the operator looking for a defect in a service that had behaved correctly.
That case is now refused by name.

**The resolution pass is deliberately not behind a flag**, for the same reason the deployment record
is not excused by `-require`: a healthy run registers nothing and the pass is a no-op, so the only
runs it touches are the ones `measurement-contract.md` §12 requires it for — while **an operator who forgot the flag would
hold an unreconcilable artifact whose register had already died with the process.**

**What blocked the experiments for half a day, because the shape recurs.** The harness was finished
and validated against the live topology long before any of it could be *evidence*: two provenance
stamps were false for unrelated reasons, and **neither announces itself until a verdict reads
`level: none`.** One was a Docker client reading the build context through a Windows filesystem
view, so every file arrived mode 0755 against an index of 0644 and `go build` stamped
`vcs.modified=true` from a spotless checkout. The lesson is the ordering: **check the two stamps
before driving anything.**

Three things the validation runs established for the experiment design, none of them a result:
**size the fixture against the run, not the reverse**, since a sold-out fixture turns a correctness
run into a measurement of capacity exhaustion; **verify promptly**, because unconfirmed holds expire
on the reservation TTL, so a verdict taken long afterwards is evidence about expiry rather than about
the run; and **`-reset` can deadlock against the running service**, since `TRUNCATE` takes ACCESS
EXCLUSIVE on every booking table while the expiry worker sweeps the same tables — observed once in
roughly ten seeds. The script retries once and says so; a second failure stops the run, because **a
fixture in an unknown state makes every cell below it meaningless.**

The `db_acquire_cap` question §6b leaves open is **not** settled, but the experiments give it a
better lead: across both retained passes the worst client-observed latency during the outage was
~503 ms against a 5 s server deadline and a 500 ms cap. That points the opposite way from §6b's
single observation, and the plausible reconciliation — warm pool versus a new connection — **is a
hypothesis to test, not a diagnosis to act on.**

### 6f. What producing the evidence taught, as distinct from what it says

**The `unknown_replayable` case arrived once, unbidden, and only once.** The validation plan says the
experiment need not manufacture one, and this is why that was the right call: **the fault produces it
stochastically, so an experiment *required* to produce one would have been tuned until it did.** Its
accounting is the whole §6c contract holding on a live fault.

**A number that does not move is worth as much as one that does.** `timeout_server` was exactly 304
in all three retained failure runs while `internal_failure` moved and volume moved by 4.2%. It held
in unretained runs too and was different when the fault was hand-timed — a lead worth writing down,
but one whose runs are not retained, so it is labelled as such rather than counted as evidence.

**Evidence was regenerated several times over artifact hygiene, not over results** — once because
retained transcripts were named `*.log`, which `.gitignore` excludes, and **a report citing files the
repository does not carry is an assertion.** The rule: **the artifact set and the script that
produces it must match exactly**, or reproduction instructions describe a run nobody can repeat.
Relatedly, retained artifacts are tracked files, so copying a run into `docs/measurements/` dirties
the tree and a later image build stamps `vcs.modified=true` from what looks like a clean checkout.
**Build, run, retain, commit, in that order.**

### 6g. The review round, and why every finding was the same defect

Four P1s on the experiment harness, sharing one shape: **a gate that reported success while the thing
it gated had not happened.**

- **The cell passed at `quotability.level: none`**, because verification ran with `-require none`.
  Reconciliation compares the numbers a run produced; certification decides whether they may be
  quoted at all, and **the two disagree exactly when a run is unsound, unresolved or drifted.** The
  comment defending `none` claimed a real floor would cost the diagnostic artifact — wrong about the
  verifier, which writes the verdict before enforcing the level. **A wrong justification in a comment
  is worse than none, because it answers the reviewer's question before they ask it.**
- **The readiness codes during the fault were recorded, not checked.** `printf` succeeds whatever
  `curl` returns, so the artifact could contradict the containment claim while the cell passed.
- **The verifier's provenance was never established.** The generator's stamp is carried into the
  report and refused at `local`; the verifier's is carried nowhere, so a stale or modified verifier
  could certify the whole matrix silently. **Asymmetries like that are where a provenance chain
  breaks.**
- **The row census was an artifact, not a gate**, and reconciliation cannot cover for it: each
  authority is counted only over the organisations placement gives it, so **a stray row on the wrong
  writer sits outside every scoped count while the aggregate still balances.**

**Writing the discriminating tests found two more defects**, which is the argument for writing them
rather than reasoning about them: the isolation assertion hardcoded which unit was expected to fail
while the container it stops is a variable, and a failed cell left the topology mid-fault. Each gate
was then proven by removing its property.

## 7. Not in PR3

- cross-authority booking, distributed commit, or any saga;
- splitting one organisation across authorities;
- online rebalancing, dual-write migration, or a placement change during a run;
- a production routing gateway or dynamic shard catalogue;
- a shared global workflow database;
- replica scaling, exporters, dashboards, or the connection-budget control;
- any capacity or throughput-multiplier claim;
- per-authority client-side attribution (§3.2);
- **targeted mid-`COMMIT` fault injection**, so INV-21's acknowledgement-lost half stays unproven
  (§6a) — stopping an authority proves containment, not that case, and a generic shutdown must not
  be reported as proof of it;
- authentication, which the formal design identifies as the real fix for caller-asserted routing
  identity and which no part of AG-Sept delivers.

## 8. Evidence labels

Every figure in PR3's report carries its `measurement-contract.md` §2 label. Organisation counts,
authority counts and the placement map in this record are `[HYPOTHESIS]` until a run uses them.
Correctness verdicts are not measurements and carry no label: a gate passes, or the run is not
admissible.
