# Latency, timeouts, and retries

**Status:** AG-M0 provisional
**Decision scope:** initial latency objectives, request-deadline budget, and retry
policy for the Alloca-Go mutation path.

This document records why the first experiment configuration uses a relatively long
client deadline while retaining much tighter latency objectives. It is intentionally
provisional: every numeric value below is a `[HYPOTHESIS]` unless marked otherwise,
and AG-M2/AG-M3 measurements may retain, revise, or reject it.

See also:

- [`measurement-contract.md`](measurement-contract.md) for the normative evidence and
  experiment rules;
- [`system-context.md`](system-context.md) for the system and authority boundaries.

---

## 1. Decision summary

The initial mutation-path hypotheses are:

| Concern | Initial value | Meaning |
|---|---:|---|
| Healthy-operation p50 | `[HYPOTHESIS]` ≤ 50 ms | latency objective at the recommended operating point |
| Healthy-operation p95 | `[HYPOTHESIS]` ≤ 200 ms | latency objective at the recommended operating point |
| Healthy-operation p99 | `[HYPOTHESIS]` ≤ 500 ms | tail-latency objective at the recommended operating point |
| Client end-to-end deadline | `[HYPOTHESIS]` 6000 ms | degraded-operation safety ceiling, not an SLO |
| Server request deadline | `[HYPOTHESIS]` 5000 ms | leaves response-delivery margin before the client gives up |
| Admission-decision cap | `[HYPOTHESIS]` 250 ms | bound before work is accepted or explicitly rejected/deferred |
| DB-pool acquisition cap | `[HYPOTHESIS]` 500 ms | bound on waiting for a database connection |
| PostgreSQL `lock_timeout` | `[HYPOTHESIS]` 2000 ms | bound on each lock acquisition attempt |
| PostgreSQL `statement_timeout` | `[HYPOTHESIS]` 3000 ms | statement bound, greater than `lock_timeout` |
| Transaction/context budget | `[HYPOTHESIS]` 3500 ms | total database operation budget inside the server deadline |

The ordering invariant is normative even though the values are not:

```text
lock_timeout
    < statement_timeout
        <= transaction/context budget
            < server request deadline
                < client end-to-end deadline
```

Admission and pool waits occur outside the transaction budget. At their provisional
maxima, `250 + 500 + 3500 = 4250 ms` `[DERIVED]`, leaving approximately `750 ms`
`[DERIVED]` inside the server deadline for parsing, validation, application work,
response encoding, scheduling variance, and cancellation propagation. The remaining
`1000 ms` `[DERIVED]` between server and client deadlines is delivery margin.

The load-balancer idle timeout is not part of this strict per-request deadline chain.
It is a connection-inactivity control and must remain comfortably above the
application deadline, but the client and server request contexts are the primary
request-cancellation mechanisms.

---

## 2. Why the latency SLO and client deadline differ

The latency objectives describe the **healthy recommended operating point**. The
client deadline answers a different product question:

> After how long is the result no longer useful enough for the client to keep waiting?

A request that completes in 5 seconds is below the 6-second deadline, but it is not
SLO-compliant. Reports must retain the full latency distribution and may use the
following diagnostic bands:

| End-to-end latency | Classification |
|---:|---|
| ≤ 500 ms | healthy-operation objective |
| > 500 ms to 2 s | degraded |
| > 2 s to 5 s | severely degraded |
| > 5 s to 6 s | deadline danger zone |
| > 6 s | client timeout |

These bands are `[HYPOTHESIS]` diagnostic categories, not externally validated UX
standards.

A client deadline close to the 500 ms p99 target would turn ordinary tail-latency
excursions into failures and retries. Conversely, an unbounded or excessively long
deadline would retain client, server, connection, and database resources after the
result has ceased to be useful.

---

## 3. Evidence basis

### 3.1 Repository-local evidence

No Alloca-Go mutation-path latency has been measured yet. Therefore this document
contains no `[MEASURED]` timeout value.

### 3.2 Prior observations

The following observations inform the initial hypothesis but are not reproducible
from this repository:

- `[PRIOR-UNREPRODUCED]` In the predecessor RuntimeIQ/Alloca prototype, removing
  effective timeout bounds allowed latency to exceed 10 seconds under load.
- `[PRIOR-UNREPRODUCED]` A prior mobile booking workload was observed to wait roughly
  5–6 seconds at peak release time before returning an error, while ordinary requests
  commonly completed within approximately 2 seconds.
- `[PRIOR-UNREPRODUCED]` A historical game client used an approximately 6-second
  request timeout.

These observations do not prove that 6 seconds is correct for Alloca-Go. They make it
a more credible initial experiment value than 2 seconds, which risks terminating
work that could still complete usefully during a synchronized release.

### 3.3 External engineering guidance

The references support the method and ordering, not the exact numeric values:

1. **AWS Well-Architected — Set client timeouts**
   explains that a timeout that is too high retains resources, while a timeout that
   is too low creates artificial failures, additional retries, higher backend load,
   and potentially complete outages. It recommends workload-specific validation
   rather than relying on generic defaults.
   <https://docs.aws.amazon.com/wellarchitected/latest/framework/rel_mitigate_interaction_failure_client_timeouts.html>

2. **AWS Well-Architected — Control and limit retry calls**
   recommends bounded retries, exponential backoff, jitter, and retrying at only one
   layer to avoid multiplicative retry storms. It also requires idempotency before
   retrying mutating requests.
   <https://docs.aws.amazon.com/wellarchitected/latest/framework/rel_mitigate_interaction_failure_limit_retries.html>

3. **Amazon Builders' Library — Timeouts, retries, and backoff with jitter**
   describes selecting a tolerable false-timeout rate from measured latency
   percentiles, adding network allowance where appropriate, and using backoff and
   jitter to avoid correlated retry amplification.
   <https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/>

4. **Google Cloud Spanner — Deadline exceeded**
   recommends setting a deadline to the maximum time for which a response remains
   useful. It explicitly warns that an artificially short deadline followed by an
   immediate retry can prevent operations from completing and add wasted work.
   <https://cloud.google.com/spanner/docs/deadline-exceeded>

5. **gRPC — Deadlines**
   recommends explicitly setting a realistic deadline from network and processing
   expectations, then validating it through load testing. It also describes deadline
   propagation and cancellation of work after a caller has given up.
   <https://grpc.io/docs/guides/deadlines/>

6. **PostgreSQL — Client connection defaults**
   documents that `lock_timeout` applies only while acquiring locks and should be
   lower than `statement_timeout`; otherwise the statement timeout fires first and
   the lock timeout provides little value.
   <https://www.postgresql.org/docs/current/runtime-config-client.html>

---

## 4. Retry policy

A timeout is not permission to launch an unconstrained new attempt. The first
Alloca-Go client/load-system policy is:

1. **Business refusal:** do not retry automatically.
2. **`retry_after`:** retry only after the server-provided delay, with jitter and a
   bounded overall attempt budget.
3. **Admission rejection:** retry only under an explicit experiment/product policy;
   never immediately synchronize all clients into another wave.
4. **Client timeout or unknown commit outcome:** resolve or replay using the same
   idempotency key. Never create a new logical mutation with a new key merely because
   the response was not observed.
5. **Automatic retry count:** `[HYPOTHESIS]` at most one automatic retry for a single
   user action, and only when the outcome classification declares it safe.
6. **Retry layer:** one owner only. Lower and higher layers must not independently
   retry the same mutation.
7. **Backoff:** bounded exponential backoff with jitter unless an explicit
   server-provided retry time supersedes it.

AG-M1 must make mutation replay safe before AG-M2 enables retry experiments.

---

## 5. Overload behaviour

The 6-second client deadline must not become permission for a 6-second invisible
server queue. Under overload, the service should quickly choose one of these bounded
outcomes:

- execute within the admitted budget;
- return an explicit `retry_after` or admission rejection;
- return a queue/release token if a later milestone implements a user-visible waiting
  protocol.

The waiting-room case is a different interaction model: a request returns a token
quickly and queue progress is observed separately. It does not keep an HTTP mutation
silently in flight for the entire user-visible wait.

---

## 6. Experiments and accountability

The values in §1 must be revised only through recorded evidence. AG-M2 should run a
client-deadline sweep including at least `[HYPOTHESIS]` 2, 3, 4, 5, 6, 8, and 10
seconds while holding the workload definition constant.

Each point must report:

- completion and goodput by classified domain outcome;
- p50/p95/p99 and latency histograms beyond p99;
- client, server, pool, lock, statement, and transaction timeout counts;
- requests that would have completed usefully after a shorter candidate deadline
  (false timeouts);
- unknown commit outcomes and successful idempotent resolution/replay;
- retry amplification: original attempts, retries, and total backend work;
- queue, pool, and lock occupancy/wait distributions;
- generator and service resource saturation.

The initial 6-second value is accepted only if the evidence shows that it provides a
better balance than shorter and longer candidates: low false-timeout rate, bounded
resource occupancy, no uncontrolled retry amplification, and useful classified
outcomes before the deadline.

AG-M3 must repeat the deadline validation through the real internet-facing AWS path,
because local tests do not reproduce client-network and load-balancer effects.

---

## 7. Revision policy

All numeric values in this document are intentionally revisable. A later report or
ADR may replace them when it records:

- the experiment and raw artifact paths;
- the measured latency and false-timeout distribution;
- retry-amplification and resource-occupancy effects;
- the chosen product usefulness boundary;
- the rationale for retaining or changing each deadline.

Revising a hypothesis in response to evidence is success. Quietly changing a timeout
without preserving the evidence and rationale is a contract violation.
