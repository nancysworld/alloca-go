# AG-Sept PR3 — Horizontal database authority, Phase 1

**Type:** Implementation record, spanning PR3a/3b/3c
**Status:** In progress. PR3a is merged (#13); PR3b is merged (#14, `57f501d`); PR3c is not
started. The
design was accepted before implementation began (formal design §8), and §6 holds no blocker —
its one remaining item is a starting fixture, not a contract.
**Budget:** 7.5 development days across three PRs ([AG-Sept plan](../../planning/ag-sept-plan-new.md) §4) —
3.0 for PR3a, 2.5 for PR3b, 2.0 for PR3c. **PR3a came in at 1.0**; the 2.0 difference went
to the plan's contingency, not to PR3b or PR3c.
**Owner doc:** [ag-sept-plan-new.md](../../planning/ag-sept-plan-new.md) §8.2 and §6.5 are normative for what
this PR builds; this record covers only how PR3 discharges them and the choices made along the
way.
**Design input:**
[`../../design/horizontal-database-authority.md`](../../design/horizontal-database-authority.md)
owns the design. It was frozen for this work as the design note at `a6e4c21` (indexed by
`5017fed`) and promoted to formal design afterwards, retaining its section numbering. Its §8
lists the settled Phase 1 contracts and the mechanics left to implementation. This record does
not restate the design, and where the two disagree the formal design wins.

## 1. Exit gates

PR3 is three PRs because it has three separable exit gates, each of which can fail on its own.
From the plan, verbatim:

> **PR3a:** a service unit cannot be started against an inconsistent placement map or an
> incompatible schema; a misrouted request is refused rather than written; the booking policy is
> one named outcome across every closed set that must know about it; and no accepted AG-M1
> invariant has been weakened to make any of it fit.

> **PR3b:** a multi-authority run is reproducible from a version-controlled topology, produces
> an aggregated correctness verdict naming every authority it read, and is refused certification
> when the units disagree.

> **PR3c:** Phase 1 correctness and failure isolation are demonstrated on independent writable
> authorities; every accepted transaction semantic on the supported path is unchanged; and no
> result claims a throughput multiplier from this workstation.

The clause that decides whether PR3 succeeds honestly is PR3a's last one. Phase 1 is only worth
building if the local transaction path it composes is the *same* path AG-M1 proved. A Phase 1
that reached its topology by relaxing an invariant would have moved the problem, not scaled it.

**What PR3 may not claim, whatever it measures.** Both authorities, both service units, the
generator, and the telemetry stack share one 10-vCPU WSL2 allocation
([`docs/measurements/environment.md`](../../measurements/environment.md)). Authority composition
cannot be quoted as a capacity multiplier from this machine, and PR2's unexplained ~2×
excursions are still open, so any single reading carries a ±2× caveat — which PR4's node
exporter does not discharge merely by existing, only by explaining, excluding, or bounding the
excursion. PR3 is correctness-first by design, not by descope.

## 2. What PR3 delivers

| # | Deliverable | PR | Plan reference |
|---|---|---|---|
| 1 | Versioned placement map: loading, validation, immutability for a run, startup gate | 3a | §8.2 |
| 2 | Shard-affine service units — one authority, one pool, readiness bound to it | 3a | §8.2 |
| 3 | Server-side placement enforcement, with the §12.5 misrouting control | 3a | §8.2, §12.5 |
| 4 | Booking policy on resolved authorities, and `cross_authority_unsupported` through every closed set it touches | 3a | §8.2 |
| 5 | Explicit `UserRef` ownership check on confirm and cancel | 3a | §8.2 |
| 6 | `/meta` extended with authority identifier, routing version, schema version | 3a | §6.4 |
| 7 | Placement invariants in the register, each with a discriminating test | 3a | §3.2 |
| 8 | Containerised two-authority topology, per-authority migration | 3b | §9.1 |
| 9 | Generator routes mutations by user organisation and reads by slot organisation; multi-organisation, one-hot-organisation, and cross-authority-refusal workloads | 3b | §5.6, §6.3 |
| 10 | Placement, authority count, and assignment in the manifest; multi-service certification | 3b | §6.4 |
| 11 | Authority-aware verifier with one aggregated verdict | 3b | §6.5 |
| 12 | Phase 1 correctness experiments and per-authority verdicts | 3c | §11 |
| 13 | Failure-isolation experiment — one authority down, ambiguous mutations replayed, then restored | 3c | §11 |
| 14 | Ambiguous-request register and `ResolveAmbiguous` (§5.7) | 3b | §6.5 |
| 15 | `classifyCommit` ambiguity fix and the INV-21 terminated-session fault test | 3b, early | §3.2 |

## 3. What the existing code makes cheap, and what it does not

Recorded so the estimates in §4 are auditable rather than asserted, and so the work is not
rediscovered.

### 3.1 Reconciliation is already organisation-scoped — the largest saving

`reconcile.Run(ctx, q Querier, org domain.OrganisationID, r loadgen.Report, scrapes Scrapes)`
takes an organisation, and `capacityCheck`, `idempotencyCheck`, and `claimsCheck` each take it
in turn (`internal/reconcile/reconcile.go`). The checks are already local to one organisation's
rows, which is exactly the granularity Phase 1 places on one authority.

So the plan's §6.5 contract — local safety invariants per authority, then aggregate persisted
and server totals compared once against global client totals — lands close to the existing
shape, and authority-aware verification is an extension rather than a rewrite.
`cmd/alloca-verify` needs the placement map in place of its single `--database-url` and `--org`
flags.

**This is an observation about cost, not a design.** Whether PR3b loops the existing entry point
per organisation, refactors the checks behind an authority-scoped querier, or restructures them
another way is PR3b's to choose. The normative requirement is the plan's §6.5 contract, and in
particular that one organisation's persisted rows are never compared against the run's
unpartitioned global summary.

### 3.2 Per-authority attribution comes free from per-service scrapes

Client-side totals carry no organisation or authority dimension: `loadgen.Total` is
`{Operation, Outcome, Reason, Replay, Count}` (`internal/loadgen/run.go:311`). Adding one would
touch the report schema, the manifest, and every artifact that reads them.

It is not needed. §8.2 makes every service unit shard-affine, so a unit's own Prometheus scrape
*is* its authority's traffic. Per-authority work is read from per-service scrapes, and
`serverTotalsCheck` sums across them. This is why the plan lists per-authority client-side
attribution under "not in PR3b" rather than as a cut — it would be a second, weaker source for
a fact the topology already supplies.

One consequence to implement carefully: `Scrapes` is a single `{Baseline, After}` pair
(`internal/reconcile/servertotals.go:129`) whose `measured()` detects a restarted process. With
N units, each unit's pair must be differenced *separately* and then summed — differencing the
sums would mask one unit restarting mid-run.

### 3.3 The generator is single-authority throughout

`loadgen.Client` holds one `baseURL` (`internal/loadgen/client.go:76`) and each workload
generator holds one `Org` (`internal/loadgen/workload.go:47,77,129`). Routing by organisation
means an org→endpoint map in the client and a multi-organisation dataset in the workloads. This
is the bulk of PR3b's generator cost.

### 3.4 `/meta` reports nothing about placement or schema

`metaResponse` inlines `buildinfo.Info` plus timing configuration and a database block
(`internal/httpapi/meta.go`, `internal/buildinfo/buildinfo.go`). There is no authority
identifier, routing version, or schema version. §5.1 of the formal design gates setup on schema
compatibility, and §5.2 on reporting authority identity — both are new fields, and both flow
into `loadgen/servicemeta.go`, which is what makes a run certifiable.

Migrations are applied by `cmd/alloca-migrate` as a deliberate separate step (ADR-0002), so
per-authority migration is a loop over DSNs, not a design change.

### 3.5 The refusal is a closed-set change, not a constant

`domain.Reason` is a closed set with a membership map (`internal/domain/outcome.go:49-57` and
`:120-128`), and the HTTP mapping is keyed by outcome and total over the contract
(`api-surface.md` §2.3). A new reason touches the domain set, the mapping, `api-surface.md`, the
invariant register, the `measurement-contract.md` §4 taxonomy, and the metric label allowlist.
The formal design names it `cross_authority_unsupported` and states the same list. Priced into
PR3a at 0.5 days, and part of why PR3a is not the smallest of the three.

## 4. Budget, by component

Estimates, not measurements. Recorded per component so an overrun is attributable to something.

**Half a day is the unit.** Finer granularity would be false precision: nothing here is
estimated well enough to distinguish 0.3 from 0.4, and a plan that pretends otherwise invites
its own overrun. Nancy's call, 2026-08-05.

**Two late additions are absorbed rather than added.** The ambiguous-request register (§5.7)
sits inside PR3b's generator line, and the post-restoration replay pass inside PR3c's
failure-isolation line. Both are small, and both make those two lines tight rather than
comfortable; if either overruns, the plan's contingency covers it before anything is descoped.

**The INV-21 discharge is explicitly not funded here.** Proving it needs a *deliberately timed*
connection loss during `COMMIT`, which a generic authority shutdown does not produce. If the
targeted fault-injection mechanism proves cheap it can be taken inside PR3c; if it does not, it
is a contingency draw of about 0.5 days or it is left open, exactly as it has been since AG-M1.
What PR3c must not do is claim the discharge from a generic shutdown.

**PR3a — 3.0 days allocated, 1.0 actual**

| Component | Days |
|---|---:|
| Placement map: type, config, validation, immutability, startup gate | 0.5 |
| Shard-affine binding and server-side enforcement of the assigned organisation set | 0.5 |
| `/meta` authority identifier, routing version, schema version | 0.5 |
| Booking policy on resolved authorities, and `cross_authority_unsupported` through the closed sets and status mapping | 0.5 |
| Explicit `UserRef` ownership check on confirm and cancel (§5.5) | 0.5 |
| Normative doc updates and the discriminating tests, each proved to fail without its property | 0.5 |

**PR3b — 2.5 days**

| Component | Days |
|---|---:|
| Containerised two-authority topology, per-authority migration, reproducible from version control | 0.5 |
| Generator org→endpoint routing and multi-organisation workloads | 0.5 |
| Multi-service manifest and certification — every unit's `/meta`, revision and schema agreement | 0.5 |
| Authority-aware verifier: placement input, per-authority loop, N scrape pairs, aggregated verdict | 1.0 |

**PR3c — 2.0 days**

| Component | Days |
|---|---:|
| Multi-organisation seeding across authorities, with at least two organisations colocated | 0.5 |
| Correctness matrix — the six cases in formal design §6 | 0.5 |
| Failure-isolation experiment: down, observe, restore, verify | 0.5 |
| Report with per-authority verdicts and the organisation-to-authority distribution | 0.5 |

## 5. Decisions taken

### 5.1 Three PRs, not one

The three exit gates fail independently, and the first is a contract change to a system with a
merged, tested invariant register. Bundling them would mean the first review of a placement
model arrives alongside a topology and a measurement run, which is how a contract error reaches
evidence. PR3a can also merge and sit: with the policy comparing resolved authorities (§5.5),
its booking half is inert in a single-authority deployment, where every organisation is
colocated and nothing is refused that is not refused today.

### 5.2 Routing lives in the generator *and* is enforced by the service

The formal design §4.1 allows static routing to live in the generator for the first experiment,
which is right — a production routing gateway is not needed to prove authority composition. But
if the generator is the *only* router, then "no supported request reached the wrong authority"
is a property of the generator's routing table, not of Alloca, and the placement invariant is
untested by construction.

Both, therefore: the generator routes, and each unit refuses what it does not own. The §12.5
misrouting control is what makes the second half real, and it is why that control is mandatory
rather than desirable.

### 5.3 Correctness evidence is not a source of budget

If PR3a–PR3c overrun, the difference comes out of PR4's measurement scope through the plan's
§16 descope order, not out of the gates that make a run admissible. Nancy's call, 2026-08-05.

### 5.4 Per-authority attribution is read from per-service scrapes

See §3.2. This is a design choice with a budget consequence, not a descope.

### 5.5 The three contract questions are settled, and one of them cost 0.5 days

Raised against the formal design in
[PR #12](https://github.com/nancysworld/alloca-go/pull/12) and settled by its revision of
2026-08-05. Recorded here because each decides what PR3a builds, and because two of them changed
the shape of the work rather than merely confirming it.

1. **The booking policy compares resolved authorities, not organisation identifiers.**
   `authority(slot_organisation_id) == authority(user_organisation_id)`, deliberately weaker
   than identifier equality. Cross-organisation booking is shipped, tested behaviour — INV-13,
   and `user-schedule-non-overlap.md` §3.3 records that its test exists precisely to stop a
   later change disabling it — so an unconditional same-organisation policy would have regressed
   the current one-authority deployment. It now survives wherever the two organisations are
   colocated, which also keeps INV-13 exercised end to end. **PR3c must therefore cover
   colocated cross-organisation booking as a success case, not only as a refusal.**
2. **Confirm and cancel gain an explicit `UserRef` ownership check**, returning the existing
   `unknown_target` on mismatch. The service has no such check today: `lockByReservation`
   (`internal/service/service.go:392`) and `tx.Reservation` (`internal/postgres/tx.go:178`)
   resolve by reservation identifier alone, and the caller's `UserRef` only scopes idempotency,
   so a confirm carrying a wrong identity succeeds. Without the check, sharding would make that
   outcome depend on whether two organisations happen to be colocated. This is a deliberate
   domain-contract correction, and it is the 0.5 days PR3a rose by — drawn from the plan's
   contingency rather than from another PR (`ag-sept-plan-new.md` §4). It is not authentication:
   a caller who knows both the reservation identifier and its exact owner can still act as that
   owner.
3. **An unavailable authority uses the existing infrastructure classifications** — `timeout_db`
   when a database bound fires, `internal_failure` for another definite fault. No new generic
   unavailable outcome is added to the closed set. The consequence lands on the evidence
   contract rather than the domain: failure-isolation runs report affected and unaffected
   populations separately and are not judged against the aggregate SLO gates that govern a
   healthy capacity run (formal design §6.1 and §7.7, plan §14 PR3c).

### 5.6 A misrouted request is `invalid_request` at the edge, with its own counter

Design note §5.2 classes a request arriving at a unit that does not own its user organisation as
a routing or deployment fault rather than the `cross_authority_unsupported` business policy, and
delegates the edge classification here. Within the closed outcome set the honest candidates are
`invalid_request` (400) and `internal_failure` (500). **PR3a uses `invalid_request`**, for two
reasons:

- the note forbids recording it as the user's durable domain outcome on the wrong authority, and
  `invalid_request` is precisely the outcome INV-7 exempts from the recording rule — it is
  rejected at the transport edge and may carry no scope to record against;
- routing identity is caller-asserted until authentication exists (formal design §7.6), so
  `internal_failure` would let a client drive the service's internal-failure rate, which is an
  SLO-relevant signal.

The deployment fault must still be visible to an operator, so the refusal increments a dedicated
bounded counter and logs the unit's authority and the requested organisation. A 400 that is
really a misconfiguration should not hide among client errors. The §12.5 misrouting control
asserts both the outcome and the counter.

### 5.7 Resolving `unknown_replayable` is a post-run pass, not a load control

The plan's §6.5 requires ambiguous mutations to be resolved by replaying their own idempotency
keys after an authority is restored. That needs the generator to retain the keys of
`unknown_replayable` responses during the run and reissue them afterwards.

The machinery is close to hand: keys are already derived deterministically per workload
(`key(name, seq, op)` in `internal/loadgen/workload.go`), and the `Replay` workload already
issues a second request under the same key and checks the response against §4.2. What is missing
is that `Response` and `Total` retain no key, so PR3b adds a small register of ambiguous
requests and PR3c adds the resolution pass.

**This is deliberately not group A's retry-on-timeout control.** That control shapes load during
a run — timeout, retry, amplification — and remains unassigned (`ag-sept-plan-new.md` §14). This
is a bounded post-restoration pass whose only purpose is to collapse ambiguity before the
correctness verdict. Conflating them is how group A creeps into a PR that cannot fund it.

## 6. Settled by the freeze, and the one thing left open

### 6.1 The ownership correction is an accepted API behaviour change

PR3a's booking half is inert in the current one-authority deployment, where every organisation
is colocated (§5.1). Its **ownership half is not**: today a caller holding a reservation
identifier can confirm or cancel it while asserting a different `UserRef`, and after PR3a that
returns `404 unknown_target`.

That is a live behaviour change on a merged contract rather than new functionality, so it was
raised for an explicit decision rather than allowed to arrive as a side effect of sharding. It
is **accepted** as part of the design freeze (formal design §8, decision 5; Nancy, 2026-08-05).
PR3a therefore carries its normative consequences with it: a line in the invariant register, the
behaviour stated in [`api-surface.md`](../../design/api-surface.md), and a discriminating test that
fails without the comparison.

### 6.2 Organisation count and skew — a starting fixture, not a contract

The formal design's map is now four organisations across two authorities (§5.1 of the formal design).
`[HYPOTHESIS]`: 2:2 by count and deliberately unequal by load, so the one-hot-organisation
workload and the distribution reporting of the plan's §5.6 have something to show. It stays a
run parameter that implementation may change on evidence, not an architectural commitment —
recorded only so the fixture is chosen rather than defaulted into.

### 6a. What PR3b carries early, and what stays PR3c

Nancy's call, 2026-08-05, after PR3a merged: rather than hold finished work on a branch of
its own, PR3b takes everything that already exists and PR3c keeps only what still has to be
built. Fewer branches, one review, and nothing parked where it cannot be seen.

**Already built and riding in PR3b** (branch `agent/ag-sept-pr3b`):

- the generator's **ambiguous-request register** and `ResolveAmbiguous`. The register was
  always PR3b's — the plan's §14 asks PR3b to "retain the idempotency keys of any
  `unknown_replayable` response so PR3c can replay them" — and the replay mechanism is
  built alongside it rather than split across two PRs for the sake of the boundary;
- the **`classifyCommit` ambiguity fix**: class 57 (operator intervention) and class 08
  (connection exception) SQLSTATEs are the session ending, not the server answering the
  `COMMIT`, so they are now `unknown_replayable` rather than definite failures;
- the **INV-21 terminated-session fault test** that found that defect, and INV-21's revised
  register entry. **It is deliberately not named for connection loss mid-`COMMIT`:** the
  session is destroyed with the transaction's work done and `COMMIT` not yet sent, so what
  is proven is that a commit attempted on a dead session is classified conservatively.
  Nothing interposes between the client sending `COMMIT` and the server acting on it, and
  the *acknowledgement-lost* half stays PR3c's, unproven.

The last two were nominally PR3c's and explicitly unfunded (§4). They are here because
they are *done*, and because the fix is a correctness change to merged code that should not
wait on a measurement PR. **Consequence, accepted rather than overlooked:** the fix reaches
`main` when PR3b merges, so until then `main` misclassifies a terminated session under
`COMMIT` as a definite failure. Low risk — no production deployment exists, and the experiment that
would deliberately trigger it is PR3c, which lands after.

**Still PR3c's, and still to be built:**

0. **wiring `RunTopology` into `cmd/alloca-verify`.** The package-level verifier is built and
   tested; the CLI still takes one `--database-url` and one `--org`. The flags it needs — a
   placement document and one DSN per authority — are shaped by how PR3c actually drives a
   run, so they are left to the PR that first has a run to drive rather than guessed at now;
1. the **post-restoration resolution pass** as a step of the failure-isolation experiment —
   calling `ResolveAmbiguous` after the authority is back and before the correctness
   verdict, and refusing to reconcile a run with anything left unresolved
   (`ag-sept-plan-new.md` §6.5). **This includes the logical-summary contract that pass
   needs, which PR3b deliberately does not define** — see §6c;
2. the **failure-isolation experiment** itself, with affected and unaffected populations
   reported separately as their own evidence class. **It must state which failure mode it
   injected**, because the outcome depends on it — see §6b;
3. the **Phase 1 correctness matrix** and per-authority verdicts;
4. **INV-21's remaining half.** The *commit-landed-but-acknowledgement-lost* case is still
   unproven and needs a proxy that drops the reply; the register says so, and PR3c must not
   claim the whole invariant on the strength of the half that is tested.

### 6b. The unavailable-authority outcome depends on *how* the authority is unavailable

Observed on the containerised topology while validating it (PR3b), and recorded here because
it changes what PR3c's expected-outcome set may assert.

Stopping an authority's container and issuing a request to its unit produced
**`timeout_server` (504)**, not one of the three outcomes the formal design enumerates for an
unavailable authority — `internal_failure`, `timeout_db`, `unknown_replayable` (formal design
§6.4). The unit's `/readyz` correctly reported 503 throughout, and the healthy authority
continued serving its own organisations, so failure *isolation* held exactly as designed.

The reason matters: a **stopped** container drops packets rather than refusing them, so the
connection attempt hangs instead of failing fast, and the per-request server deadline is the
first bound to fire. A container that is *killed*, or a database that refuses connections,
would fail immediately and classify differently.

Two consequences for PR3c:

1. **The experiment must name its failure mode** — stopped, killed, network-partitioned,
   or process-crashed — because they do not produce the same outcome mix. An expected-outcome
   set that lists three outcomes and meets a fourth reads as a defect when it is a different
   experiment.
2. **`db_acquire_cap` did not bound this wait**, and it is worth understanding before the
   experiment quotes anything. The observation is that a ~5 s server deadline elapsed where a
   500 ms acquisition cap might have been expected to fire first; the diagnosis is *not*
   established, and it may be that the cap does not cover establishing a new connection. This
   is adjacent to the PR2 deferral register's standing item that the timeout budget has never
   been observed doing its job under load (`ag-sept-plan-old.md` §14, group A). Investigate
   before asserting, and do not fix it on the strength of one observation.

### 6c. Resolution accounting is PR3c's, and PR3b stops short of it deliberately

Raised in review of PR3b (ChatGPT, 2026-08-06) and deferred with Nancy's agreement. Recorded
here so PR3c inherits the problem stated rather than discovers it.

`ResolveAmbiguous` replays each ambiguous mutation and returns `[]Resolution`. Those replays
are **real HTTP requests that PR3b does not account for anywhere**: they happen outside
`Runner`, they are not folded into `Report.Summary`, and `RunTopology` takes no resolution
input. So after a resolution pass the server's counters include the replay traffic while the
client totals still describe only the original run, and three-way reconciliation over that
state cannot be correct.

The *acknowledgement-lost* branch is the sharp end. A resolution returning `Replay=true`
proves the original mutation committed — the database holds one fresh logical mutation — but
appending the replay's response to the totals would leave `FreshMutations()` at zero for that
logical request, because the replay itself is not fresh. The mutation is real and the summary
would say it never happened.

**What PR3c must define**, as one contract rather than as four separate patches:

- resolution HTTP traffic incorporated into request and server accounting, so the scrape
  comparison is over the same population as the client totals;
- a logical mutation counted **once as fresh** when the replay proves the original committed;
- counted **once** when resolution performs it with `Replay=false`;
- anything still ambiguous rejected outright — `Unresolved` already reports it, and a run
  carrying any is not reconcilable.

It needs end-to-end tests on both branches: original committed then replayed, and original
rolled back then performed during resolution.

**Why not in PR3b.** The same reasoning that left `RunTopology`'s CLI flags to PR3c: the
shape of this contract depends on how the resolution pass is actually driven, and a summary
contract guessed at before its only caller exists is one PR3c would have to redesign while
also producing the experiment. The register's own defect — a failed replay re-registering
itself and growing the outstanding work on every pass — was a live bug in shipped code and
**was** fixed in PR3b; deduplication does not solve the accounting problem, and is not
claimed to.

### 6d. The deployed artifact is bound to the routed units, and checked before load

The architectural decision is [ADR-0003](../../decisions/0003-deployed-artifact-identity.md):
code identity and deployed-artifact identity are separate provenance facts, and the second
must be observed from outside the measured process rather than reported by it. What the ADR
deliberately does not fix is the mechanism, which is recorded here because two parts of it
were wrong in a way that read as correct.

**Observation alone is unbound evidence.** A record proving that some containers on this host
share an image says nothing about the units a run addressed. A stack raised yesterday, a
second stack on other ports, or a unit nothing routes to would all satisfy it. So each
observation now also carries the address its container publishes, read from the port binding
rather than assumed from the compose file, and the run requires an exact one-to-one
correspondence with the targets it is about to drive. Both directions fail for their own
reason: a routed unit nobody observed is an artifact the run cannot name — the whole gap §6.4
exists to close — while an observed unit nothing routes to means the record describes a
different topology, and a record wrong about the unit set is not evidence that its image
identity is right either.

**Ordering is the other half.** The check previously ran after `Runner.Run`, where it could
only annotate numbers that already existed: the operator discovered the mismatch once the
measurement had been taken. It now runs before any measured request, ahead even of the `/meta`
reads, so a wrong record costs a re-record rather than a run. That property is easy to lose in
a later refactor and is held by a test that asserts the units served **zero** requests when the
preflight refuses; moving the call back after the workload fails it with four requests per
unit rather than failing on the error text.

**Why the requirement keys on the topology, not on a flag.** The certification gate refuses a
containerised run whose `image_id` is empty, but it is armed by `container_deployment`, which
the record itself sets. Omitting the record therefore also disarmed the gate, and the run
reported a clean lower-provenance result whose missing artifact identity looked like a choice.
A run that reaches more than one unit is the containerised topology whatever the operator
remembered to pass, so that is what the requirement keys on.

**Why `-require` is not the exception it looks like.** The requirement first exempted
`-require none`, on the reading that a run declaring it supports no claim has nothing for
artifact identity to qualify. That reading is wrong about the flag. `-require` is the floor a
run must clear to exit zero, not a ceiling on what its report claims: certification always
computes the highest level the manifest and summary actually reach, so a stamped binary
passing `-require none` exits zero *and* writes a report certified at `local` or above —
naming no artifact. The exemption reopened exactly the bypass the requirement was added to
close, and under the phrasing an operator is most likely to reach for. There is no level at
which skipping the record is safe, so there is now no exception. A genuine
no-evidence mode would have to cap certification, which `-require` does not do; it can be
built when something needs it.

**What was deliberately not built.** No per-unit map in the manifest: once the preflight has
established full coverage and one common artifact, the per-unit observations are validation
evidence, and copying them into the report would be a second representation of what the single
`image_id` already states. No general `source | container` deployment-mode abstraction either;
making the one known containerised evidence path non-bypassable is what PR3b needs, and an
abstraction invented before a second caller exists is one the second caller redesigns.

The record schema, the flag, the matching rules, and the tests live in
`internal/loadgen/deployment.go`, `cmd/alloca-load/main.go` and
`test/scripts/record-deployment.sh`; the operating procedure is
[`../../operations/container-topology.md`](../../operations/container-topology.md) §7. None of
that is restated here.

## 7. Not in PR3

- cross-authority booking, distributed commit, or any saga (formal design §10, plan §15);
- splitting one organisation across authorities;
- online rebalancing, dual-write migration, or a placement change during a run;
- a production routing gateway or dynamic shard catalogue;
- a shared global workflow database;
- replica scaling, exporters, dashboards, or the connection-budget control — all PR4;
- any capacity or throughput-multiplier claim;
- per-authority client-side attribution (§3.2, §5.4);
- authentication, which §7.6 of the formal design correctly identifies as the real fix for
  caller-asserted routing identity and which no part of AG-Sept delivers.

## 8. Evidence labels

Every figure in PR3's report carries the `[HYPOTHESIS]`, `[MEASURED]`, `[DERIVED]`, or
`[PRIOR-UNREPRODUCED]` label required by
[`measurement-contract.md`](../../design/measurement-contract.md) §2. Organisation counts,
authority counts, and the placement map in this record are `[HYPOTHESIS]` until a run uses them.

Correctness verdicts are not measurements and carry no label: a gate passes or the run is not
admissible.
