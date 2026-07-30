# Observation contract

**Status:** Normative — current through AG-M1
**Scope:** what the service emits about its own behaviour: the observation types, their
fields, the cardinality rule that constrains them, and how a metrics backend is added
without touching the code that emits them.

This document owns the **emission** contract. Which outcomes exist and what they mean is
owned by [`measurement-contract.md`](measurement-contract.md) §4; the service-level
indicators AG-M2 must expose are listed in its §6. This document says what AG-M1 actually
emits, in what shape, and what it deliberately does not.

---

## 1. Observations, not log lines

Callers depend on a `Recorder` port and hand it a **value describing what happened**, not
a formatted message:

```go
type Recorder interface {
    RecordRequest(ctx context.Context, obs RequestObservation)
    RecordExpiry(ctx context.Context, obs ExpiryObservation)
}
```

AG-M1 ships one implementation: structured `slog` output. The reason the port exists
anyway is cost of change — adding Prometheus or OpenTelemetry must be **one new `Recorder`
and one line in `cmd`**, not edits to every handler and the worker.

This is deliberately not a telemetry framework. There is no registry, no exporter
plumbing, and no configuration surface: two observation types, one interface, one
implementation.

An observation is a **report, never an input**. Nothing consults telemetry to make a
decision, which is why `internal/telemetry` is an adapter leaf wired in by `cmd`
([`project-structure.md`](project-structure.md) §4).

---

## 2. The cardinality rule (normative)

**Every field on an observation type is drawn from a closed set or is a measurement.**

Identities, slot identifiers, reservation identifiers, idempotency keys, request
identifiers, and error strings are **absent from the observation types**. A metrics
implementation therefore *cannot* label a time series with them even by accident — they
are not in the value it receives.

This makes the rule **structural rather than documentary**. A comment saying "do not label
with user id" relies on every future author reading it; a type that does not carry the user
id cannot be misused. Adding a high-cardinality field to these structs is the change that
must be refused in review.

Diagnostic context that is useful in a log and ruinous as a metric label travels in the
**context** instead. `telemetry.WithRequestID` / `telemetry.RequestID` carry a per-request
identifier that a logging recorder reads and a metrics recorder ignores.

---

## 3. `RequestObservation`

Emitted exactly once per request that reaches a booking or read handler.

| Field | Type | Notes |
|---|---|---|
| `Operation` | closed-set string | the request kind, never the URL path — which is unbounded once identifiers appear in it. See §3.1. |
| `Outcome` | `domain.Outcome` | the single terminal classification (measurement-contract §4) |
| `Reason` | `domain.Reason` | a refusal's stable code; empty otherwise |
| `Replay` | bool | served from a recorded idempotency record; the orthogonal half of the classification |
| `HTTPStatus` | int | the status written, or intended when the caller had already gone |
| `Duration` | duration | the handler, including the transaction it drove |

It is emitted **even when the response reached nobody**. A client that disconnects
mid-request is still classified (`timeout_client`) and still observed, because every
completed request must carry exactly one terminal outcome for the totals to reconcile. An
outcome that vanishes from the mix is worse than a slow one.

There is exactly **one place** a booking response is written, so the telemetry cannot
disagree with the status the client received.

### 3.1 `Operation` is a closed set, and it is wider than `domain.Operation`

The values are the three mutations — `reserve`, `confirm`, `cancel` — plus `list_slots` for
the read route.

It is a plain string rather than a `domain.Operation` because `domain.Operation` is the set
of *mutations the idempotency record accepts*, and a read is not one of them. Use
`telemetry.OperationListSlots` or `string(domain.OpReserve)` and its siblings; never a path
or anything caller-supplied.

**Consequence for AG-M2 (important).** Booking goodput must be summed over the **three
mutation operations**, not over all requests. A successful listing is `admitted_success` in
the sense that the service answered correctly, but it is not a booking. The alternative —
leaving reads unobserved — would have made a 500 on that route invisible, which is worse.

---

## 4. `ExpiryObservation`

Emitted once per expiry-worker iteration.

| Field | Type | Notes |
|---|---|---|
| `Slots` | int | candidate slots the iteration examined |
| `Expired` | int | holds transitioned to expired across them |
| `Failed` | bool | the iteration ended early in an error |
| `Duration` | duration | the whole iteration |

Counts, not identifiers: *which* slots were settled is a question for the database, not for
a time series.

A failed iteration still reports the work completed **before** the failure, so the
telemetry cannot disagree with the database about what was settled. The error itself is
logged separately as diagnostic context; `Failed` is the countable fact.

---

## 5. The AG-M1 log shape

One structured line per observation. The attribute keys are the label names a metrics
implementation would use, so a query written against these logs translates directly.

```json
{"msg":"request","operation":"reserve","outcome":"admitted_success","reason":"",
 "replay":false,"http_status":200,"duration_ms":8.52,"request_id":"ccd279ef8c75bbd8"}
```

```json
{"msg":"expiry_iteration","slots":3,"expired":4,"failed":false,"duration_ms":12.1}
```

- Durations are reported in **milliseconds as a float**, so sub-millisecond requests do not
  all collapse to zero.
- `request_id` appears only when present, as an omitted attribute rather than an empty
  string.
- A failed expiry iteration is logged at **info** level here; the worker logs the error
  itself at error level with its diagnostic context. Emitting both at error level would
  double-count one event in a log-based alert.

---

## 6. What AG-M1 does not emit

The measurement contract's §6 indicator list is an **AG-M2 obligation**, not a claim about
this milestone. AG-M1 emits the outcome mix, per-request duration, and the expiry
iteration; it does **not** emit:

- latency percentiles (p50/p95/p99) — the per-request durations are the raw material, and
  aggregation is AG-M2's job, deliberately not done in-process here;
- lock, queue, or connection-pool wait time, or database transaction time as separate
  measurements — `Duration` currently covers the handler as a whole;
- process or database resource indicators (CPU, memory, goroutines, GC, connections);
- admission queue depth and age, which have no producer until AG-M2.

No aggregation, retention, or dashboard exists yet. Nothing here is a measured result:
this milestone builds the **emission** path, and
[`measurement-contract.md`](measurement-contract.md) §2 governs what may later be claimed
from it.
