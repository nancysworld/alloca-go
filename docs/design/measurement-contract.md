# Measurement contract

**Status:** AG-M0 provisional
**Purpose:** define *how every future claim in this repository will be measured*, so
that a hypothesis can never silently graduate into a result. This document is
normative: later milestones and reports must conform to the evidence-labelling
convention (§2), the experiment template (§5), the run manifest (§11), the
reconciliation contract (§12), and the provisional targets (§7–§8) until evidence
revises them.

This document **owns** the project's measurement vocabulary, outcome taxonomy, experiment
template, service-level indicators, and evidence-admissibility rules directly. It does not
inherit them from a plan or roadmap: those are scheduling and exploration documents, and a
contract that depended on either could be changed by a replan.

---

## How to read this document

- §2 is the rule that keeps measured facts, prior evidence, calculations, and
  hypotheses visibly distinct.
- §5 is the checklist every experiment must satisfy before its numbers are quotable.
- §7 and §8 are **provisional hypotheses**, not commitments. They exist to drive
  experiments and will be retained, revised, or rejected with evidence in AG-M2/M4.
- §9 defines the project-level final validation bar and the evidence artifact that
  assembles the milestone results into one auditable conclusion.
- §11 and §12 are what an individual run must record and must self-check before its
  numbers may be quoted at all. §9 states the obligations; these two say what
  discharging them means.
- §13 names what a run's numbers may back — the quotability levels — and the generator
  provenance a published capacity claim requires.

---

## 2. Evidence-labelling convention (normative)

Every quantitative statement in this repository — in design docs, decision records,
reports, commit messages, and generated artifacts — carries exactly one label:

| Label | Meaning | Obligation |
|---|---|---|
| `[MEASURED]` | A result reproducible **from this repository**. | Cite the command or artifact path that reproduces it, plus the environment (via `/meta`). |
| `[PRIOR-UNREPRODUCED]` | A historical or predecessor input, **not yet reproduced here**. | Label as prior; never present as an Alloca-Go result. |
| `[DERIVED]` | A value calculated from measured inputs. | Show the formula and the input values it was computed from. |
| `[HYPOTHESIS]` | An expectation used to drive an experiment. | Never quote as an outcome; must name the experiment that will test it. |

Rules:
1. An unlabelled number in a report is a defect, not a shortcut.
2. `[DERIVED]` values must never be laundered into `[MEASURED]`; the derivation stays
   visible.
3. Predecessor-prototype figures ([`high-level-design.md`](high-level-design.md) §1.1)
   are `[PRIOR-UNREPRODUCED]` until an
   experiment in this repository reproduces them, at which point the reproduction —
   not the prior figure — becomes the `[MEASURED]` result.
4. This convention is also a public-disclosure safeguard: it prevents modelled
   assumptions from being presented as observed facts about external systems (see
   [`../public-disclosure-policy.md`](../public-disclosure-policy.md)).

---

## 3. Measurement vocabulary

All rates are per second over a stated measurement interval.

- **Offered load** — requests generated per second, regardless of whether the
  service can admit or complete them.
- **Completed throughput** — responses completed per second, separated by response
  and domain outcome (see §4).
- **Goodput** — correct, useful domain operations completed per second. For the current AG-Sept
  mutation-path experiments this means **definite, fresh successful mutations completed inside
  the stated measurement interval**. Timetable/read-path success is not folded into this mutation
  goodput scalar; read-path capacity is a separate workload question to define when that problem is
  scheduled. Expected sold-out / insufficient-balance responses are reported **separately**, never
  as infrastructure failures.
- **SLO-safe capacity** — the highest sustained goodput that satisfies *all* latency,
  timeout, correctness, saturation, and headroom gates for the interval.
- **Recommended operating capacity** — a conservative cap below SLO-safe capacity
  that reserves headroom for variance, rolling deployment, and loss of one unit.

### 3.1 Horizontal scale efficiency

```text
scale efficiency at N units = measured goodput at N units
                              --------------------------------
                              N × single-unit goodput
```

Reported **separately per experiment layer**:

- **Layer A** — stateless API work.
- **Layer B** — dispersed independent authorities (the primary test of horizontal
  composition).
- **Layer C** — one indivisible hot authority.

For Layer C, efficiency approaching `1/N` is the expected serialization ceiling, not
a system-wide scaling failure. A Layer C curve must **never** be presented as a
system-wide horizontal-scaling result.

### 3.2 Capacity-unit economics

Capacity/resource economics is a **selectable measurement dimension**, not a mandatory output of
every capacity experiment. When a validation explicitly selects it, each tested configuration
reports the relevant serving-capacity facts — for example successful ops/s, successful ops/s per
vCPU, cost/hour, cost per million successful ops, memory headroom, DB connections required,
p50/p95/p99 latency, timeout and unknown-outcome rates, and failure-domain/deployment implications.

Cost figures are `[DERIVED]` from a **dated input block** matching the actual measured topology,
never timeless prices or assumptions inherited from an older deployment design. The input block
states the pricing snapshot date and location/region; currency and exchange-rate assumptions where
applicable; pricing mode; serving compute shape/count/duration; storage and I/O assumptions; and
network, load-balancing, telemetry, or other charges when material to the selected economics
question.

Measurement infrastructure such as the load generator or verifier is **not part of serving-system
cost** unless the selected economics question explicitly says otherwise. Its real experiment spend
may still be estimated and bounded separately as an operational concern. Provider-specific inputs
such as EC2, Fargate, RDS, another cloud, or bare-metal cost models belong to the experiment that
selects them; this contract owns the derivation discipline rather than one provider's product list.

---

## 4. Outcome taxonomy (normative)

Peak accepted request rate is not a capacity result. Every request is classified on
**two orthogonal dimensions**: its *terminal outcome* (what happened) and its
*disposition* (whether it was a fresh attempt or an idempotent replay). Modelling
these as one flat enum is a defect — a replay still returns a recorded terminal
outcome (an `admitted_success`, a `business_refusal`, or an earlier
`unknown_replayable`), so "replay" cannot be a peer of the very outcome it carries.
AG-M1 encodes both dimensions in the telemetry/schema contract; AG-M2+ report against
them.

### 4.1 Terminal outcome (exactly one per request)

| Outcome | Meaning | Counts toward |
|---|---|---|
| `admitted_success` | Correct domain mutation completed. | Goodput |
| `business_refusal` | Sold out / insufficient balance / conflict / unknown target — a valid domain answer. | Reported separately (not failure) |
| `invalid_request` | Rejected **before** domain processing: malformed body, missing required field, or missing idempotency key. Distinct from `business_refusal`, which is a valid *domain* answer to a well-formed request. | Reported separately (client/transport error; neither goodput nor failure) |
| `retry_after` | Bounded retry requested. | Bounded overload response |
| `admission_rejected` | Rejected because the admission queue is full. | Bounded overload response |
| `queue_position` | Admitted to a queue with a position / release token. | Bounded overload response |
| `unknown_replayable` | Commit outcome unknown; safe to replay with same idempotency key. | Tracked, must be replay-safe |
| `timeout_client` | Client deadline expired. | Timeout accounting |
| `timeout_server` | Server request deadline expired. | Timeout accounting |
| `timeout_db` | Database statement/lock timeout. | Timeout accounting |
| `timeout_lb` | Load-balancer-level timeout. | Timeout accounting |
| `internal_failure` | Unexpected server fault. | Failure |

`invalid_request` was added in AG-M1 (transaction-semantics §8) so that **every**
completed request carries exactly one terminal outcome and totals reconcile: a
request that never becomes a valid domain operation (unparseable, missing a required
field or idempotency key) must still be classified, but must not inflate
`business_refusal` — a *valid domain answer to a well-formed request*. The boundary is
deliberate: a **well-formed** request for a non-existent target, or one that reuses an
idempotency key for a different payload, is a domain **`business_refusal`** (with a
specific reason code); only a request the service cannot turn into a domain operation
at all is `invalid_request`. The fault line is separate again: an internal
invariant/accounting violation is an `internal_failure`, never a refusal.

### 4.2 Disposition dimension (orthogonal to the outcome above)

| Field | Values | Meaning |
|---|---|---|
| `replay` | `false` \| `true` | `false` = first observed attempt for this idempotency key; `true` = a replay that returned the **originally recorded** terminal outcome rather than performing a new mutation. |

A replay is therefore recorded as, e.g., `outcome=admitted_success, replay=true` —
never as a standalone `idempotent_replay`. This keeps two invariants simultaneously
measurable: (a) the true distribution of terminal outcomes (replays fold into their
recorded outcome), and (b) replay accounting itself (how many requests were served
from the idempotency record, and which original outcome each returned). A replay must
return exactly the recorded outcome; it must never re-run the mutation or resolve to a
different terminal outcome.

### 4.3 Overload objective (acceptance bar, normative)

Under overload, requests receive explicit bounded outcomes *before* they accumulate into
infrastructure or client timeouts.

This is an acceptance bar rather than a description of current behaviour: whether Alloca-Go meets
it is an open question, and reproducing the mechanism by which it fails is exploration the project
has not yet scheduled ([`high-level-design.md`](high-level-design.md) §1.1).

---

## 5. Experiment template (normative)

No experiment's numbers are quotable until it declares all of the following. Each
report reproduces this block.

1. **Hypothesis** — the `[HYPOTHESIS]` under test and its accept/reject condition.
2. **Inputs** — offered-load profile; closed-loop concurrency/worker population and/or open-loop
   arrival rate; synchronized-release barrier if used; think time; connection count; deadlines and
   retry policy; config values; all random seeds; and, when used, the conditioning rule and target
   that establish the measured starting state.
3. **Outputs** — the metrics collected (§6 SLIs) and the **path to the machine-readable
   raw results**. Reports quote from raw artifacts, not from memory.
4. **Validation** — how each response was checked: HTTP status *and* domain-outcome
   validation (§4). A run without response validation is not a capacity run.
5. **Negative controls** — at minimum:
   - a **deliberately under-provisioned load generator**, proving the generator was
     not the bottleneck at the reported operating point;
   - a **response-validation-active proof** — a control that fails when validation is
     silently disabled, so "success" cannot be an unchecked 200.
6. **Environment capture** — Go version, observed `GOMAXPROCS` and whether it was set
   explicitly, task/vCPU shape, DB pool size, and generator host resources. The
   service exposes this at `/meta`; generator-side facts are captured by the load
   system.
7. **Separation** — the report separates `[MEASURED]` results, `[DERIVED]`
   calculations, interpretation, and limitations into distinct sections.

**Explicit conditioning gate:** a stateful experiment may deliberately establish a representative
pre-measurement state before the performance interval begins, but only when that phase is part of
the declared experiment rather than discarded traffic. Conditioning must:

- have a predeclared, reproducible **state target** (prefer state/count based over elapsed time when
  the state itself is what matters);
- retain its requests/outcomes separately from measured performance;
- retain or reconstruct the persisted/counter baseline at the exact measured-start boundary;
- leave its mutations visible to final reconciliation;
- record any deterministic service/pool restart or other state-preserving transition between
  conditioning and measurement; and
- avoid logical interference with the measured population, for example by using a disjoint
  conditioning identity/key/slot namespace in the same physical tables when the experiment needs
  representative table state without consuming the measured identities.

Conditioning is **not measured Goodput or measured latency**. It exists to establish the declared
starting state. Conversely, an arbitrary warm-up whose requests mutate state and are then omitted
from both the measurement and reconciliation populations remains inadmissible. A harness flag named
`warm-up` does not become acceptable merely by renaming the traffic: the population boundary above
is the contract.

**Load-generator-as-bottleneck gate:** any published capacity claim must include
generator CPU/memory/connection/network telemetry, a generator-capacity sweep, the
under-provisioned negative control, and evidence that the selected generator
configuration has headroom at the reported server operating point.

**Useful-demand / fixture-headroom gate:** a **mutation-capacity** claim additionally requires
evidence that the fixture still had work left to give. A mutation the domain refuses because the
seeded state is spent — every slot full, every claimable row claimed — is a correct answer and a
sound measurement, but it is not throughput the service could have delivered. The run measured the
fixture's remaining headroom, not the system's capacity.

The gate has two parts, and the second is the one that is easy to miss:

- **An all-refusal run cannot back a mutation-capacity claim.** It may remain `measurement_sound`
  and reach any provenance level its manifest supports — the run described itself honestly and the
  outcome mix is in its totals. Provenance is not the question. It simply has no useful demand
  behind it, so there is no capacity in it to quote.
- **Partial exhaustion invalidates a mutation-capacity point too, and it invalidates the *rung
  below* it.** A saturation argument selects an operating point by showing that a *higher* rung
  produced no more sustained Goodput. If that higher rung was short of fresh mutations rather than
  short of service, it demonstrates an exhausted fixture and says nothing about where the server's
  frontier is — so the point beneath it was never established as the frontier at all. A capacity
  point is only as good as the evidence of the rung that was supposed to exceed it.

So a report quoting a mutation-capacity point must show that the selected point **and the rungs its
selection rests on** retained enough clean fixture state to offer fresh mutations throughout. When
a declared conditioning phase consumes the same finite fixture, its demand is part of that supply
calculation too. The cheap discriminator is the outcome mix: an unexpected population of
`business_refusal` attributable to spent fixture state, rather than to the contention the workload
is designed to create, invalidates the point rather than describing it.

This is an **evidence** gate, not a provenance one. §13's ladder is unchanged: such a run still
certifies at whatever level its manifest earns, because what it lacks is useful demand rather than
self-description.

---

## 6. Required service-level indicators

Telemetry must expose:

- end-to-end p50, p95, p99 latency;
- successful domain goodput;
- expected refusal / conflict rate;
- client, server, load-balancer, and database timeout rates;
- unknown commit outcomes;
- idempotent replay count and result;
- queue, lock, and connection-pool wait time;
- database transaction time;
- API CPU, memory, goroutine count, GC pressure, observed `GOMAXPROCS`;
- database CPU, connections, I/O, lock activity, transaction saturation;
- admission queue depth and age where applicable.

**The diagnostic view stays scoped to these.** Any dashboard the project retains for measured
runs is an instrument for diagnosing a run in progress, not an operations console: it renders the
indicators above and the saturation signals a frontier argument rests on, and it does not carry
alerting, templating, annotation, or shared-library machinery. The bound matters because rich
dashboards arrive by accretion — one individually reasonable panel at a time — and a view that
must be scrolled is no longer the thing an operator watches while a sweep runs. Reports quote the
retained artifacts, never a screenshot of this view.

Adding to it is a scope decision. Which milestone builds or extends it is scheduling, and belongs
in the plan.

---

## 7. Provisional SLOs `[HYPOTHESIS]`

These are **provisional hypotheses** used to drive experiments — not production
commitments and not measured results. They apply to the domain mutation path and will
be retained, revised, or rejected with evidence in AG-M2 (local frontier) and AG-M4
(ratification).

The p50/p95/p99 values are **healthy-operation latency objectives at the recommended
operating point**. They are distinct from the client end-to-end deadline in §8, which
is a *degraded-operation safety ceiling* and explicitly **not** an acceptable latency
SLO: a request completing just under the deadline is not SLO-compliant. The design
note [`latency-timeouts-and-retries.md`](latency-timeouts-and-retries.md) records the
evidence basis and diagnostic latency bands.

| SLI | Provisional target | Notes |
|---|---|---|
| e2e latency p50 | ≤ 50 ms | healthy-operation objective at recommended operating point |
| e2e latency p95 | ≤ 200 ms | healthy-operation objective |
| e2e latency p99 | ≤ 500 ms | healthy-operation tail objective |
| Explicit-outcome rate | ≥ 99.9% of admitted requests | every admitted request gets a classified outcome (§4) within its deadline |
| Server / DB timeout rate | ≤ 0.5% | at or below the recommended cap |
| Unknown commit outcome | ≤ 0.05% | must be replay-safe |
| Business refusal | reported separately | a valid domain answer, never counted as failure |

There is no throughput SLO yet: SLO-safe capacity is a *measured* output of AG-M2,
not an input we assert here.

---

## 8. Timeout budget `[HYPOTHESIS]`

A nested deadline chain. **The nesting/ordering, classification, and idempotency rules
are the contract; the specific millisecond values are hypotheses** that AG-M2 sweeps
locally and AG-M3 validates end-to-end through the real AWS/client path. The decision
basis, prior observations, external references, and revision rules live in the design
note [`latency-timeouts-and-retries.md`](latency-timeouts-and-retries.md).
**This section — measurement-contract §8 — restates its normative outcome for this contract.**

**Ordering invariant (normative):**

```text
lock_timeout
    < statement_timeout
        <= transaction/context budget
            < server request deadline
                < client end-to-end deadline
```

so that the *innermost* responsible layer times out first and returns an explicit,
classified outcome (§4) — never a generic outer timeout.

| Layer | Provisional value | Role |
|---|---:|---|
| Client end-to-end deadline | 6000 ms | degraded-operation safety ceiling (not a latency SLO) |
| Server request deadline | 5000 ms | leaves response-delivery margin before the client gives up |
| Admission decision cap | 250 ms | bound before work is accepted or explicitly rejected/deferred |
| DB connection acquisition cap | 500 ms | bound on waiting for a pooled connection |
| PostgreSQL `lock_timeout` | 2000 ms | bound on each lock acquisition attempt |
| PostgreSQL `statement_timeout` | 3000 ms | statement bound, greater than `lock_timeout` |
| Transaction / context budget | 3500 ms | total DB operation budget inside the server deadline |

**Budget arithmetic.** Admission and pool waits occur *outside* the transaction
budget. At their provisional maxima, `250 + 500 + 3500 = 4250 ms` `[DERIVED]`, leaving
approximately `750 ms` `[DERIVED]` inside the 5000 ms server deadline for parsing,
validation, application work, response encoding, scheduling variance, and cancellation
propagation, and `1000 ms` `[DERIVED]` of delivery margin between the server and
client deadlines.

**Explicit distinctions (normative):**

- The **load-balancer idle timeout is not a member of this per-request deadline
  chain.** It is a connection-inactivity control and must sit comfortably above the
  application deadline, but the client and server request contexts are the primary
  request-cancellation mechanisms.
- A **timeout must not trigger an unconstrained new mutation.** Timeout and
  `unknown_replayable` outcomes are resolved or replayed using the *same* idempotency
  key (§4.2), never by issuing a fresh mutation with a new key.
- **Retries are bounded, jittered, and owned at exactly one layer.** Lower and higher
  layers must not independently retry the same mutation. The retry policy is specified
  in [`latency-timeouts-and-retries.md`](latency-timeouts-and-retries.md) §4.
- **Every numeric value remains revisable** after AG-M2/AG-M3 evidence, while the
  nesting invariant, outcome classification, idempotency behaviour, and evidence
  requirements remain normative. Changing a value without recording the evidence and
  rationale is a contract violation.

The service's outer HTTP server timeouts (`internal/config`) are the coarse,
connection-level layer beneath this budget; the per-request context deadlines that
carry the values above are introduced with the transactional core in AG-M1.

### 8.1 Startup validation (AG-M1 obligation, normative)

The ordering above is a *checked invariant of the running service*, not only a
documented intention. AG-M1 introduces the per-request deadline configuration
(client, server, admission, DB-pool, `lock_timeout`, `statement_timeout`,
transaction/context budget) and **must validate it at startup**, failing fast with an
explicit error — the same discipline `config.Load` already applies to zero/negative
connection-level timeouts (`internal/config`). A configuration that violates any
clause below must be rejected before the service accepts traffic.

The validation must enforce the **per-request nesting** and the **write-phase
relationship** — but must *not* mechanically compare every connection-level timeout
with the business deadline.

1. **Per-request chain** (from §8) — the authoritative business-operation deadlines:

   ```text
   lock_timeout < statement_timeout <= transaction/context budget
       < server request deadline < client end-to-end deadline
   ```

2. **Connection-level phases are sized independently.** The per-request Go context is
   the authoritative business-operation deadline; the `http.Server` timeouts are coarse
   transport / resource-protection bounds, each governing a different phase. Only
   `WriteTimeout` has a required relationship to the business deadline; the others are
   sized by their own concern and must **not** be compared mechanically with it.

   | `http.Server` timeout | Governs | Relationship to the per-request deadline |
   |---|---|---|
   | `ReadHeaderTimeout` | Receipt of request headers | Independent — protects header receipt; unrelated to request execution time |
   | `ReadTimeout` | Reading the full request (headers + body) | Sized to the maximum supported request-upload duration / body size; not compared to the business deadline |
   | `WriteTimeout` | Handler execution + response write | **> server request deadline + explicit response-writing margin**, so the Go context deadline fires first and yields a classified `timeout_server` / `timeout_db` (§4) rather than a connection-write teardown |
   | `IdleTimeout` | Keep-alive inactivity between requests | Independent — not part of the nested mutation deadline chain |

   The load-balancer idle timeout is likewise a connection-inactivity control, outside
   the per-request chain, and must sit above the client end-to-end deadline.

The single durable principle: the per-request Go context is the business deadline, so
for the write phase it must fire before `WriteTimeout` (making an overrun a classified
outcome, not a torn connection); the header, body-read, and idle phases are governed by
their own transport bounds and are not nested in the mutation chain. AG-M1 should cover
this with a unit test over representative valid and invalid configurations, and expose
the resolved, validated budget so experiments can record it alongside `/meta`.

**Clause 1 is enforced wherever a budget is consumed, not only at the startup gate.**
The startup check is the primary gate, but a budget is a value that travels away from
the `Config` it was resolved from, and a component that renders it into another system's
settings cannot tell whether validation ran. `lock_timeout` and `statement_timeout` are
the sharpest case: PostgreSQL reads `0ms` as *no bound*, so an unvalidated budget does
not fail — it produces a working session with the database bounds silently disabled, and
the resulting unbounded lock wait is indistinguishable in the outcome mix (§4) from a
request that merely took a long time. Clause 1 therefore lives on the budget type itself
(`config.RequestBudget.Validate`), and each consumer that turns a budget into enforced
timeouts re-checks it — the connection pool does so before constructing the pool.

---

## 9. Final system validation and evidence artifact (normative)

The project is not validated by compilation, passing unit tests, peak accepted request
rate, or agreement between reviewers and AI agents. Its final claims are credible only
when the implemented system, persisted state, telemetry, and reproducible experiment
artifacts agree under concurrency, faults, load, and independent inspection.

The final validation artifact assembles the milestone reports into one auditable
conclusion. It must establish all of the following, or state explicitly which item
remains unproven:

1. **Semantic correctness.** The domain state machines and every named correctness
   gate are covered by deterministic unit, race, and property-style tests where
   applicable.
2. **Transactional correctness.** The same invariants are exercised against real
   PostgreSQL concurrency, including capacity-edge reserves, duplicate requests,
   confirm/cancel races, confirm/expiry races, rollback, timeout, and commit-ambiguity
   paths.
3. **Independent reconciliation.** After correctness and stress runs, database queries
   independently verify capacity, state-machine, booking/reservation, and idempotency
   invariants rather than trusting HTTP responses or application telemetry alone. §12 is
   the contract each run discharges this against.
4. **Fault and recovery behaviour.** Client cancellation, server deadline, pool and DB
   timeout, worker delay, process interruption, dependency unavailability, lost
   response, and unknown commit cases are exercised; each test records persisted state,
   client-visible outcome, replay/recovery behaviour, and telemetry classification.
5. **SLO and overload frontier.** Increasing offered load establishes the healthy,
   SLO-safe, saturated, and overloaded regions. The report names the measured SLO-safe
   capacity and a conservative recommended operating cap, and shows that overload
   produces bounded explicit outcomes rather than uncontrolled timeout growth.
6. **Scale-shape separation.** Stateless API capacity, dispersed independent
   authorities, and one hot authority are reported as separate experiment layers. A hot-authority
   serialization ceiling is never presented as a system-wide scaling result. When capacity/resource
   economics is selected by a validation, it is reported as a separate derived layer rather than
   folded into the scaling result.
7. **Production-shaped validation.** The relevant conclusions are repeated through the
   deployed network path with multiple API instances, PostgreSQL, load balancer,
   external load generation, migrations, and production-oriented telemetry.
8. **Reproducibility and provenance.** Every material claim identifies the commands and
   raw artifact paths required to reproduce it, and carries the run manifest §11
   requires. Reports retain the evidence labels of §2.
9. **Adversarial review and limitations.** The conclusion records independent attempts
   to falsify the design, implementation, generator validity, reconciliation, and
   interpretation. Known limits, negative results, unresolved risks, and deferred work
   are part of the artifact, not omitted from it.

The expected final report lives at
`docs/reports/alloca-go-final-validation.md`. Milestone reports may establish parts of
this contract progressively; the final report cites those reports and their raw
artifacts rather than duplicating or relabelling their evidence. Passing this gate does
not mean the system has no limits. It means its correctness, operating frontier,
failure behaviour, scale boundaries, and remaining uncertainty are stated with
repository-local evidence that can survive independent attempts at falsification.

---

## 10. What AG-M0 does and does not establish

- **Establishes:** the labelling convention, vocabulary, outcome taxonomy, experiment
  template with negative controls, the SLI list, the provisional SLO/timeout
  hypotheses, and the final validation contract — i.e. the rules future evidence must
  satisfy.
- **Does not establish:** any `[MEASURED]` capacity, latency, throughput, or cost
  result. Every number in §7 and §8 is `[HYPOTHESIS]`; no measurement has been taken.

---

## 11. Run manifest (normative)

This is the **vocabulary** of run provenance: the facts a manifest can carry. It is not a flat
checklist that every run must complete. **Which subset gates which claim level is owned by
§13.2**, and some fields are conditional on the topology — placement applies only to a run that
addressed several units, image identity only to a containerised one.

The fields are:

- commit SHA, and — for a run served by containers — the **image ID or digest** of the
  artifact that served it. The two are different facts and neither implies the other:
  the SHA is stamped into the binary and identifies the *code*, so the same code served from
  a stale tag, or rebuilt on a different base layer, carries an identical SHA. The identity
  is an **image ID or registry digest, never a tag** — a tag is a mutable alias that two
  builds can wear, and the second silently replaces the first. It is **observed from the
  host** by inspecting the running containers, never self-reported by the service: a process
  cannot see which image wraps it, so anything it reported would be an environment variable
  repeated back. A run built and served from source has no image to name and is not asked
  for one. The decision and the alternatives it rejects are
  [ADR-0003](../decisions/0003-deployed-artifact-identity.md);
- Go version and observed `GOMAXPROCS`;
- replica count and application resources;
- PostgreSQL version and configuration identity;
- pool size per replica and aggregate expected pool capacity;
- workload and dataset parameters;
- offered rate and/or closed-loop worker/concurrency population;
- duration and, when used, the conditioning declaration/state target, the measured-start boundary,
  and any legacy warm-up declaration;
- timeout budget and reservation TTL;
- generator location, resources, and utilisation;
- deployment topology and timestamp;
- **authority count, the routing/placement version, and the organisation-to-authority
  assignment the run used**.

Secrets and private endpoints must not be committed.

**No run may be quoted as a capacity claim while a field its topology requires is
unpopulated.** A generator is an HTTP client and cannot discover the service's shape for
itself, so which fields a given milestone's runs can populate is a **scheduling** question,
answered by the plan
([`../planning/ag-sept-plan.md`](../planning/ag-sept-plan.md), *Manifest and reconciliation
staging*). The rule above is not staged: it holds against whatever the topology of the moment
requires. What a run may claim once its fields are populated is §13.

**Multi-service runs need one further rule.** When several service units serve one run, the
manifest records every unit's `/meta`, and the run is uncertifiable if the units disagree on
commit revision or report incompatible schema versions. One topology, one binary, one schema.

---

## 12. Correctness reconciliation (normative)

Every measured run carries a self-check. At minimum it must reconcile:

- consumed slot capacity against admitted reservation mutations;
- distinct logical idempotency keys against committed mutations and replays;
- live claims against admitted reservations per identity and interval;
- every completed request against the closed terminal-outcome set of §4.1, with `replay`
  folded in as the orthogonal flag §4.2 defines rather than double-counted.

A run with unreconciled client totals, server totals, or persisted state is not quotable.

### 12.1 Conditioning, measurement, and resolution are separate populations

A stateful experiment may have three traffic populations around one persisted-state trajectory:

- the **conditioning population** runs before the measured interval to establish the declared
  starting state. Its requests, outcomes and logical mutations are retained separately; they do
  not enter measured Goodput, latency, outcome rates or measured duration;
- the **measurement population** is the requests completed inside the stated measurement interval.
  It owns measured `Completed`, `Goodput`, latency, terminal-outcome/timeout rates, replay counts,
  and every rate whose denominator is that interval;
- the **resolution population** is any post-run same-key traffic needed to settle
  `unknown_replayable` outcomes after the measured interval. It belongs to recovery/reconciliation,
  not measured performance.

The final persisted state may contain mutations from all three. Therefore a conditioned experiment
must retain the conditioning summary and the persisted/server-counter **baseline at measured start**,
then reconcile measured and resolution deltas against that baseline. It is equally valid to retain
a reconstructible conditioning state whose exact contribution can be proven at verification; what
is not valid is to require an empty database at the end of conditioning or to omit conditioning
mutations from the accounting because they occurred before the performance clock started.

A deterministic pool/service recycle between conditioning and measurement is allowed when it
preserves database state. Its occurrence and readiness boundary are retained as experiment facts.
It must not reseed, truncate, or otherwise change the declared conditioned state invisibly.

This is why explicit conditioning is not the same thing as discarded warm-up. **No request that
mutates state may disappear from the populations needed to reconstruct final state.** The exact Go
representation is an implementation choice; the contract requires the population boundary and
starting-state baseline to be auditable.

### 12.2 Ambiguous-mutation resolution

Ambiguity resolution extends one logical mutation across multiple HTTP attempts, but post-run
resolution does not rewrite the performance history of the measured interval.

For an original measured request that returned `unknown_replayable`:

1. it contributes **one measured request and zero measured Goodput**. That remains true even if a
   later resolution proves that its mutation committed; the client did not receive a definite
   successful outcome inside the measured interval;
2. if same-key resolution returns `replay=true`, the idempotency record proves the original attempt
   committed. Reconciliation therefore counts exactly one final logical mutation for that key. The
   resolution HTTP request is a replay in the resolution/reconciliation population, not a second
   logical mutation and not measured-window Goodput;
3. if same-key resolution returns `replay=false`, the original attempt did not leave a recorded
   mutation and the resolution request performs it after the measured interval. Reconciliation
   again counts exactly one final logical mutation for that key, while measured-window Goodput
   remains unchanged;
4. a key that remains ambiguous after the resolution pass establishes no final logical-mutation
   count and makes the run unreconcilable and therefore unquotable.

**Denominators must follow the population they describe.** Measured performance may report the
original ambiguous attempt as `0` Goodput over `1` measured request. Recovery analysis may report
that one eventual logical mutation required two HTTP attempts when one post-run resolution was
needed. It must not report `0/2` or `1/2` as measured Goodput, because the second request is outside
the measured interval. Likewise, post-run resolution never changes the measured duration or
retroactively changes measured terminal outcomes.

The retained artifact must make conditioning (when present), measured performance, resolution HTTP
traffic, and final logical-mutation reconciliation reconstructible without silently combining them.

**Multiple authorities extend the contract, not the mechanism:**

1. the run is quiesced, and any `unknown_replayable` mutation is resolved by replaying its own
   idempotency key before verification begins;
2. **local safety invariants are checked independently on each authority** — capacity, schedule
   non-overlap, idempotency, and lifecycle are all local properties of the rows one authority
   owns;
3. **persisted and server totals are aggregated across authorities and compared once** with the
   run's reconciliation population — once, not per authority, because the client's totals are a
   property of the run rather than of any one authority. For a conditioned experiment this means
   comparing the retained measured-start baseline plus measured/resolution deltas with final state;
   measured performance fields remain scoped to the measurement population above;
4. **each service unit's scrape pair is differenced independently before the sum is taken.**
   Differencing the sums instead would let one unit restarting mid-run vanish into another
   unit's counters, which is the one arithmetic error this contract exists to prevent;
5. the verdict aggregates without treating sequential cross-database reads as one atomic
   snapshot.

The architectural requirement is that a multi-organisation run must never compare one
organisation's persisted rows against the run's unpartitioned global summary. In particular,
looping an organisation-scoped entry point against an unchanged global report is **not** a
discharge of this contract — it would compare one organisation's rows with every
organisation's totals. The verifier's data structures, query factoring, and scrape aggregation
are otherwise the implementation's to choose
([`horizontal-database-authority.md`](horizontal-database-authority.md) §6.3).

---

## 13. Generator provenance and quotability levels (normative)

§5's bottleneck gate asks for *evidence* that the generator had headroom at the reported
operating point. This section asks the prior question — what a run's numbers may back at all —
which is a matter of **provenance** and is settled before any evidence is weighed.

> **The ladder is a soundness and provenance ladder. It is not, by itself, a complete
> publication or measurement-validity gate.** A level says the run is sound and describes itself
> well enough to support a class of claim. Whether the *experiment* supports the claim is §5's
> question, and reaching the top of this ladder does not answer it.

### 13.1 A published claim needs the generator on separate compute

A load generator co-resident with the service under test contends with it for CPU, memory,
network stack, and scheduler attention. The result is then partly a measurement of the
generator, and the two contributions cannot be separated after the run.

**A capacity claim may be published only when the generator ran on compute separate from the
service.** A co-resident run remains a sound, useful, reproducible observation about that
machine. It is not a published capacity number, and neither a complete manifest nor a
demonstration of generator headroom converts one into the other.

The manifest records the generator's location (§11). That is a **declaration** — all a manifest
can carry — and it is provenance rather than evidence. It does not stand in for §5's
under-provisioned-generator control, and a published claim needs both.

Where a milestone cannot supply separate compute, the rule is honoured **by labelling**: the run
is reported at the level its provenance actually reaches and is never presented as a published
capacity claim. Withdrawing the compute withdraws the claim, not the rule.

### 13.2 Quotability levels

"Is this run quotable?" has no answer until the claim is named. A run therefore carries an
ordered **level**, and sits at the highest one whose requirements its manifest and summary
satisfy:

| Level | What it may support | Additional bar |
|---|---|---|
| `none` | no experimental claim | the run is unsound, or sound but below the `local` bar |
| `local` | a reproducible observation of this measured machine and run | soundness; service identity and the service-discovered shape available through `/meta`; generator identity and resources; workload and run shape; target and timestamp |
| `capacity` | a capacity result about the explicitly recorded topology and environment | `local`, plus aggregate/topology/environment provenance; routing and placement where applicable; immutable image identity for containerised runs |
| `publishable` | provenance eligible to support an externally presented, project-level capacity claim — **provided the experiment also satisfies §5's evidence gates** | `capacity`, plus the generator declared to run on compute separate from the service (§13.1) |

`publishable` describes the **provenance** a claim leaving this project needs. It does not mean
"appears in a public repository": this repository is public, and most runs in it are correctly
`local` or `capacity`.

Four consequences follow, and each is a mistake this ladder exists to prevent:

- **A co-resident generator does not force a run down to `local`.** Co-residency is a
  `publishable` bar only. A run that fully records its topology and environment reaches
  `capacity` with the generator on the same host.
- **Co-residency does block `publishable`**, however complete the rest of the manifest is.
- **Reaching `publishable` does not discharge §5.** Generator headroom, the under-provisioned
  control, and response-validation proof are evidence; this ladder is provenance. A run can hold
  top-of-ladder provenance and still be inadmissible because its experiment was not controlled.
- **`none` has two independent causes**, and `local` is the floor of the ladder rather than a rung
  above `none`:
  1. **the run is unsound** — response validation off or failed, the run interrupted,
     reconciliation failed, or participating units that do not describe one deployment; or
  2. **the run is sound but does not reach the `local` bar** — it cannot say what it measured, so
     it still backs no claim.

  Soundness is checked **first**, and is **not tradable against provenance**: a run whose
  responses went unvalidated is not rescued by a complete manifest. But the converse does not
  hold — being sound does not buy a level. A run missing `service_commit_sha` is `none` too,
  because a measurement that cannot name the code it measured describes an unknown.

  The two are distinguishable in the report rather than merged: `blocked_because` carries the
  soundness reason in the first case and the missing fields in the second.

Which field gates which level is enforced by `Manifest.Validate`, and §11's list is not a flat
requirement of every run — see §11's own note. The division of labour is that everything `/meta`
hands over for free is checked at `local`, because a field that costs nothing to record should
not gate a higher tier than one that costs an operator's attention; the operator-supplied
aggregate, topology, and environment facts gate `capacity`; and conditional fields
(placement for multi-unit runs, image identity for containerised runs) apply only where the
topology makes them meaningful.

Levels are named for **the claim**, never for the milestone or PR that first reaches them. A
report outlives the schedule, and a level named after a PR would oblige a later reader to
reconstruct that PR's scope before learning what the number is good for. Which milestone reaches
which level is a scheduling fact and belongs in the plan.

A run resting below the top of the ladder is the expected state while a milestone's provenance is
still being staged. A report therefore records the level reached **and what blocks the next one**,
so an incomplete level reads as scheduled rather than broken.
