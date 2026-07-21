# Measurement contract

**Status:** AG-M0 provisional
**Purpose:** define *how every future claim in this repository will be measured*, so
that a hypothesis can never silently graduate into a result. This document is
normative: later milestones and reports must conform to the evidence-labelling
convention (§2), the experiment template (§5), and the provisional targets (§7–§8)
until evidence revises them.

It formalises the vocabulary in the roadmap
([`../planning/alloca-go-roadmap.md`](../planning/alloca-go-roadmap.md) §5–§6) into
an enforceable contract. Where this document and the roadmap differ, this document
governs measurement practice.

---

## How to read this document

- §2 is the rule that keeps measured facts, prior evidence, calculations, and
  hypotheses visibly distinct.
- §5 is the checklist every experiment must satisfy before its numbers are quotable.
- §7 and §8 are **provisional hypotheses**, not commitments. They exist to drive
  experiments and will be retained, revised, or rejected with evidence in AG-M2/M4.

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
3. Prior-prototype figures (roadmap §2.1) are `[PRIOR-UNREPRODUCED]` until an
   experiment in this repository reproduces them, at which point the reproduction —
   not the prior figure — becomes the `[MEASURED]` result.
4. This convention is also a public-disclosure safeguard: it prevents modelled
   assumptions from being presented as observed facts about external systems (see
   [`../public-disclosure-policy.md`](../public-disclosure-policy.md)).

---

## 3. Measurement vocabulary

Formalises roadmap §5. All rates are per second over a stated measurement interval.

- **Offered load** — requests generated per second, regardless of whether the
  service can admit or complete them.
- **Completed throughput** — responses completed per second, separated by response
  and domain outcome (see §4).
- **Goodput** — correct, useful domain operations completed per second. Expected
  sold-out / insufficient-balance responses are reported **separately**, never as
  infrastructure failures.
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

Each tested configuration reports: successful ops/s, successful ops/s per vCPU,
cost/hour, cost per million successful ops, memory headroom, DB connections
required, p50/p95/p99 latency, timeout and unknown-outcome rates, and
failure-domain/deployment implications.

Cost figures are `[DERIVED]` from a **dated input block**, never timeless prices. The
input block must state: pricing snapshot date and AWS region; on-demand / Savings
Plan / Spot assumptions; ECS/Fargate task shape, count, and run duration; RDS engine,
instance class, deployment mode, storage, and I/O assumptions; ALB, data-transfer,
CloudWatch logs/metrics/trace costs; and currency/exchange-rate assumptions.

---

## 4. Outcome taxonomy (normative)

Peak accepted request rate is not a capacity result. Every request resolves to
exactly one of these mutually-exclusive outcomes, and telemetry must count each
separately. AG-M1 implements this taxonomy; AG-M2+ report against it.

| Outcome | Meaning | Counts toward |
|---|---|---|
| `admitted_success` | Correct domain mutation completed. | Goodput |
| `business_refusal` | Sold out / insufficient balance / conflict — a valid domain answer. | Reported separately (not failure) |
| `retry_after` | Bounded retry requested. | Bounded overload response |
| `admission_rejected` | Rejected because the admission queue is full. | Bounded overload response |
| `queue_position` | Admitted to a queue with a position / release token. | Bounded overload response |
| `unknown_replayable` | Commit outcome unknown; safe to replay with same idempotency key. | Tracked, must be replay-safe |
| `timeout_client` | Client deadline expired. | Timeout accounting |
| `timeout_server` | Server request deadline expired. | Timeout accounting |
| `timeout_db` | Database statement/lock timeout. | Timeout accounting |
| `timeout_lb` | Load-balancer-level timeout. | Timeout accounting |
| `internal_failure` | Unexpected server fault. | Failure |
| `idempotent_replay` | A replay that returned the original recorded outcome. | Tracked separately |

**Overload objective (acceptance bar, roadmap §6.2):** under overload, requests
receive explicit bounded outcomes *before* they accumulate into infrastructure or
client timeouts.

---

## 5. Experiment template (normative)

No experiment's numbers are quotable until it declares all of the following. Each
report reproduces this block.

1. **Hypothesis** — the `[HYPOTHESIS]` under test and its accept/reject condition.
2. **Inputs** — offered-load profile; closed-loop concurrency and/or open-loop
   arrival rate; synchronized-release barrier if used; think time; connection count;
   deadlines and retry policy; config values; and all random seeds.
3. **Outputs** — the metrics collected (§6 SLIs) and the **path to the machine-readable
   raw results**. Reports quote from raw artifacts, not from memory.
4. **Validation** — how each response was checked: HTTP status *and* domain-outcome
   validation (§4). A run without response validation is not a capacity run.
5. **Negative controls** — at minimum:
   - a **deliberately under-provisioned load generator**, proving the generator was
     not the bottleneck at the reported operating point (roadmap AG-M2);
   - a **response-validation-active proof** — a control that fails when validation is
     silently disabled, so "success" cannot be an unchecked 200.
6. **Environment capture** — Go version, observed `GOMAXPROCS` and whether it was set
   explicitly, task/vCPU shape, DB pool size, and generator host resources. The
   service exposes this at `/meta`; generator-side facts are captured by the load
   system.
7. **Separation** — the report separates `[MEASURED]` results, `[DERIVED]`
   calculations, interpretation, and limitations into distinct sections (roadmap §10).

**Load-generator-as-bottleneck gate:** any published capacity claim must include
generator CPU/memory/connection/network telemetry, a generator-capacity sweep, the
under-provisioned negative control, and evidence that the selected generator
configuration has headroom at the reported server operating point.

---

## 6. Required service-level indicators

Telemetry must expose (roadmap §6.1):

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

---

## 7. Provisional SLOs `[HYPOTHESIS]`

These are **provisional hypotheses** used to drive experiments — not production
commitments and not measured results. They apply to the domain mutation path at the
recommended operating point, and will be retained, revised, or rejected with
evidence in AG-M2 (local frontier) and AG-M4 (ratification).

| SLI | Provisional target | Notes |
|---|---|---|
| e2e latency p50 | ≤ 50 ms | at recommended operating point |
| e2e latency p95 | ≤ 200 ms | |
| e2e latency p99 | ≤ 500 ms | |
| Explicit-outcome rate | ≥ 99.9% of admitted requests | every admitted request gets a classified outcome (§4) within its deadline |
| Server / DB timeout rate | ≤ 0.5% | at or below the recommended cap |
| Unknown commit outcome | ≤ 0.05% | must be replay-safe |
| Business refusal | reported separately | a valid domain answer, never counted as failure |

There is no throughput SLO yet: SLO-safe capacity is a *measured* output of AG-M2,
not an input we assert here.

---

## 8. Timeout budget `[HYPOTHESIS]`

A nested deadline chain. **The ordering invariant is the contract; the specific
millisecond values are hypotheses** that AG-M3 validates end-to-end against real ALB,
Go context, and PostgreSQL behaviour.

**Invariant:** each inner deadline is strictly less than its enclosing deadline, with
margin, so that the *innermost* responsible layer times out first and returns an
explicit outcome (§4) — never a generic outer timeout.

| Layer | Provisional value | Ordering constraint |
|---|---|---|
| Client deadline | 2000 ms | < ALB idle timeout |
| ALB idle timeout | 3000 ms | > client deadline |
| Server request context | 1500 ms | < client deadline |
| Admission wait cap | 200 ms | inside server context |
| DB connection acquire | 250 ms | inside server context |
| Lock acquisition (`lock_timeout`) | 500 ms | inside server context |
| Statement (`statement_timeout`) | 800 ms | ≥ lock timeout, inside server context |
| Transaction budget | ≤ 1000 ms | inside server context |

Rationale for the shape (not the exact numbers): the server context (1500 ms) sits
below the client deadline (2000 ms) so the server can emit a classified
`timeout_server` before the client abandons; the DB-side limits (lock 500 ms,
statement 800 ms, txn ≤ 1000 ms) sit inside the server context so a database stall
surfaces as `timeout_db` rather than a context cancellation of unknown cause; the ALB
idle timeout (3000 ms) exceeds the client deadline so the balancer is never the first
to give up.

The service's outer HTTP server timeouts (`internal/config`) are the coarse,
connection-level layer of this budget; the per-request context deadlines that carry
the values above are introduced with the transactional core in AG-M1.

---

## 9. What AG-M0 does and does not establish

- **Establishes:** the labelling convention, vocabulary, outcome taxonomy, experiment
  template with negative controls, the SLI list, and the provisional SLO/timeout
  hypotheses — i.e. the rules future evidence must satisfy.
- **Does not establish:** any `[MEASURED]` capacity, latency, throughput, or cost
  result. Every number in §7 and §8 is `[HYPOTHESIS]`; no measurement has been taken.
