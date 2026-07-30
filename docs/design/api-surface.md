# API surface

**Status:** Normative — current through AG-M1
**Scope:** the HTTP contract a client codes against: routes, request shapes, response
bodies, status mapping, limits, and the operational endpoints.

This document owns the **transport** contract and nothing below it. What an outcome
*means*, and which outcome a given domain situation produces, is owned by
[`transaction-semantics.md`](transaction-semantics.md) (§4–§5) and the taxonomy in
[`measurement-contract.md`](measurement-contract.md) §4. This document only says how those
answers appear on the wire.

The transport makes no domain decisions. A handler turns a request into one service
command and hands the answer to one mapping; whether a slot has capacity, whether a key is
a replay, and whether an identity's schedule is free are decided beneath it
([`project-structure.md`](project-structure.md) §4).

---

## 1. Versioning

The booking surface is versioned in the path (`/v1/…`). The operational endpoints are not:
`/healthz`, `/readyz` and `/meta` are the operational contract established in AG-M0 and
keep their names, because renaming them would break probes and dashboards for no gain.

`/v1` is the *transport* version. It is distinct from the idempotency request-hash
`contract_version` (currently `v2`, transaction-semantics §5.1), which versions the hashed
request rather than the URL. They move independently: v2 of the hash arrived in AG-M1 PR4
without a URL change.

---

## 2. Booking endpoints

| Method + path | Operation |
|---|---|
| `POST /v1/slots/{slot_organisation_id}/{slot_id}/reservations` | reserve |
| `POST /v1/reservations/{reservation_id}/confirm` | confirm |
| `POST /v1/reservations/{reservation_id}/cancel` | cancel |
| `GET /v1/slots?slot_organisation_id=…` | list slots (informational) |

A slot is addressed by **both halves of its identity**, because slot identifiers are
unique only within the owning organisation (§1.2). A reservation is addressed by its
server-minted identifier alone.

### 2.1 Request

Every mutation requires:

- an **`Idempotency-Key` header**. Absent ⇒ `invalid_request`. It is a header rather than
  a body field so it is visible to proxies and logs without parsing the body, and reads the
  same for every operation.
- a **JSON body naming the caller's identity**:

  ```json
  { "user_organisation_id": "org-1", "user_id": "user-1" }
  ```

  Both halves are required: a user is the pair `(user_organisation_id, user_id)` (§1.1).

Decoding is strict. Unknown fields are rejected, the body must contain exactly one JSON
object, and it is capped at 4 KiB. A caller who sends `userId` gets told that, rather than
a confusing complaint about a missing `user_id` they believe they sent.

Field **order and whitespace do not matter**. The request hash covers semantically
significant *typed* fields, not the JSON text, so a retry that reorders the body is a
replay and not an `idempotency_conflict`.

Nothing server-generated is accepted from a client: no timestamps, no reservation
identifier on reserve, and no TTL — the hold's lifetime is service-owned (§1.6).

`GET /v1/slots` takes no body and no idempotency key. `slot_organisation_id` is
**required**; "every slot in the database" is not a meaningful request for a multi-tenant
service.

### 2.2 Response

Every mutation returns the same body shape, success or refusal alike. A successful
reserve:

```json
{ "outcome": "admitted_success", "replay": false, "reservation_id": "res_58153a09…" }
```

A refusal:

```json
{ "outcome": "business_refusal", "reason": "no_capacity", "replay": false }
```

| Field | Always present? | Meaning |
|---|---|---|
| `outcome` | yes | the single terminal outcome (measurement-contract §4) |
| `replay` | yes | whether this was served from a recorded idempotency record — the orthogonal half of the classification, never an outcome of its own |
| `reason` | refusals only | a refusal's stable reason code |
| `reservation_id` | when relevant | set when a reserve created a hold, echoed on its replay, and identifying a successful cancel's target |
| `booking_id` | when relevant | set when a confirm created a booking, echoed on its replay |
| `message` | when relevant | client guidance, drawn from a closed set; see §2.4 |

`outcome` and `replay` are always serialised — `replay=false` is a *fact*, not an absence,
and a client must not have to infer it. The remaining fields are omitted when empty rather
than sent as `""`, so a client should treat absent and empty as the same thing.

Internal error text never appears. `message` is chosen from constants, never built from an
error, so no SQL, `pgx`, or invariant detail can reach a client.

### 2.3 Status mapping

The mapping is keyed by **outcome, not by operation**, which is what keeps it total and
table-testable. It is total over every outcome the contract declares — including those
AG-M1 never produces — so adding a producer later cannot fall through to a default.

| Outcome | Status | Why |
|---|---|---|
| `admitted_success` | 200 | 200 rather than 201; the created entity's identifier is in the body either way |
| `business_refusal` | 409 | the request is well-formed and understood; the current state prevents it |
| `business_refusal` with `unknown_target` | 404 | "the thing you named does not exist" |
| `invalid_request` | 400 | rejected before the domain path |
| `unknown_replayable` | 500 | see §2.4 — the one message a client must act on |
| `internal_failure` | 500 | |
| `timeout_client` | 408 | best-effort delivery; the caller has already gone |
| `timeout_server`, `timeout_db`, `timeout_lb` | 504 | |
| `retry_after`, `admission_rejected`, `queue_position` | 503 | defined now, produced from AG-M2/AG-M3 |

`timeout_client` is reported as 408 rather than a non-standard 499. The status usually
reaches nobody; the observation is the record ([`observability.md`](observability.md)).

### 2.4 `unknown_replayable` is the one outcome with a required client action

The operation may already have committed. The safe recovery is replaying the **same**
idempotency key: the record either exists (it committed, and the replay returns the
original outcome) or it does not (it never committed, and the replay performs it). A *new*
key would be a new request and could double-book (§5.4). The response says so explicitly.

### 2.5 The read route makes no availability claim

`GET /v1/slots` reports what a slot **is** — identity, resource, capacity, and the
release/start/end window — and deliberately nothing about what is left. No remaining
count, no "bookable" flag.

Any such number would be derived without the slot lock and stale before the response was
written, which is exactly when it would matter. Reserve under the slot lock is the sole
authority on whether a unit can be held (§2), and this endpoint must not read as a second
opinion. Capacity is safe to report because it is the slot's configured size, not a
measurement of what remains.

Ordering is `(starts_at, slot_id)`: chronological, and deterministic because `slot_id` is
unique within the organisation the query already fixes.

There is **no pagination**. A fixed cap of 1000 applies and truncation is *reported*:

```json
{ "slots": [ … ], "truncated": true }
```

A client that cannot tell it received a partial list would treat it as the whole
catalogue. A cursor contract is query-platform work and is out of scope.

---

## 3. Operational endpoints

| Path | Contract |
|---|---|
| `GET /healthz` | liveness. 200 whenever the process can serve HTTP. It checks **no** dependency: a database blip must cause traffic to be withheld, not the process to be restarted. |
| `GET /readyz` | readiness. 200 `{"status":"ready"}` when the database can serve booking traffic; 503 `{"status":"unavailable"}` otherwise. |
| `GET /meta` | runtime provenance (Go version, observed `GOMAXPROCS`, revision) plus the request budget. |

`/readyz` **withholds the failure reason**. An infrastructure error can carry a DSN, host,
or user, and this is an unauthenticated endpoint. The cause is logged at warn level in the
same place the decision to withhold it is made, so it is recorded rather than lost.

The probe is bounded by `ReadinessTimeout` (default 1s), independently of the per-request
server deadline and validated at startup against the budget:

```text
db_acquire_cap < readiness_timeout <= server_deadline
```

The lower bound holds because the check acquires a connection *and* round-trips a
statement, so a bound equal to the acquisition cap could expire on the round trip after
acquisition had already succeeded within its own budget — reporting a healthy database as
unready. The upper bound holds because a probe that can outlast the service's own
per-request deadline is answering on the wrong timescale for something polled every few
seconds.

---

## 4. Identifiers

Reservation and booking identifiers are **server-minted and unguessable** — 128 bits of
`crypto/rand`, prefixed `res_` and `bk_` so the two kinds are distinguishable on sight. A
client never supplies one.

Path identifiers round-trip percent-encoding: Go's `ServeMux` decodes `%2F` within a
segment, so an organisation or slot identifier containing a separator addresses correctly.
One residual: an *empty* identifier cannot be expressed in a path — `/v1/slots//slot-1/…`
is answered with a 307 by `ServeMux`'s path cleaning before any handler runs — so
empty-identifier validation is unreachable via the URL.

---

## 5. What AG-M1 does not provide

- **No authentication or authorisation.** Identity is *asserted* by the caller in the
  request body. Unguessable identifiers are the only thing standing between a booking and
  a stranger: any caller who knows a reservation identifier can cancel it by asserting the
  owner's identity. This is a named gap for AG-M1, not a security design.
- **No slot management API.** Slots are created by the control plane or by a test.
- **No pagination or query language** on the read route (§2.5).
- **No admission control surface.** The 503 outcomes in §2.3 are mapped but unproduced
  until AG-M2/AG-M3.
