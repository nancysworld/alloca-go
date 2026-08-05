# Horizontal database authority

**Status:** Proposed for AG-Sept PR3 review.  
**Scope:** define a phased route from one PostgreSQL writer to multiple writable authorities while preserving Alloca's invariants and accepted same-database transaction semantics.  
**Not yet normative:** this note does not replace [`transaction-semantics.md`](../design/transaction-semantics.md), authorise production sharding, or select a future cross-shard coordination protocol.

## 1. Why this note exists

AG-Sept originally planned to establish one service baseline and then add stateless service replicas against one PostgreSQL authority. PR2 changed the order of that investigation.

The measured single-instance report shows that one shared PostgreSQL authority becomes the aggregate limiting subsystem while `alloca-go` still has application-compute headroom. The exact database-side mechanism and all measured values remain owned by [`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md); this design note carries only the architectural conclusion.

Adding service replicas can raise throughput while database concurrency remains below that authority's frontier. It cannot move the saturated ceiling of the same writable authority. End-to-end horizontal scalability therefore needs a way to add writable database authority as well as stateless service compute.

The first version of this note investigated cross-shard booking immediately. Review exposed the cost accurately: once slot capacity and one user's schedule live on different PostgreSQL authorities, the operation needs distributed commit or a durable compensating workflow. Neither coordination family fits the remaining AG-Sept budget with enough correctness evidence.

The revised decision is therefore phased:

- **Phase 1 — AG-Sept:** compose independent organisation-home authorities while supporting bookings whose user and slot organisations resolve to the same writable authority;
- **Phase 2 — later:** retain that placement and add an explicitly designed cross-authority booking protocol when the project can fund its failure semantics and recovery model.

Phase 1 is not a temporary mock of Phase 2. It is the durable local transaction path and the first horizontal database unit. Phase 2 must extend it without reopening or weakening that path.

## 2. What scalability means here

Alloca scalability is not one maximum-throughput number. It is the behaviour of several resource and authority dimensions:

- service compute can be increased with stateless replicas;
- writable database capacity can be increased by adding independent authorities;
- independent organisations should progress without sharing one PostgreSQL process set, buffer pool, WAL stream, connection ceiling, or storage path;
- one hot slot and one user's schedule remain deliberately serialised, because those are correctness authorities rather than accidental global locks;
- the service-to-database resource ratio must be measured per topology and workload rather than assumed.

A useful experiment may report a topology such as:

```text
N service replicas : M writable PostgreSQL authorities
```

and state which authority distribution it supports. A dispersed multi-organisation workload, one hot organisation, one hot slot, and one hot identity do not share one meaningful scale-efficiency claim.

Local containers can establish routing, authority isolation, schema compatibility, and correctness. They cannot establish a production database-capacity multiplier when the service, generator, telemetry stack, PostgreSQL instances, WSL2, and storage path share one workstation allocation. Any publishable capacity-composition claim requires independently provisioned database resources and a controlled service-compute comparison.

## 3. Correctness and placement invariants

Horizontal scaling does not change the AG-M1 correctness requirements:

- active reservations and bookings never exceed one slot's capacity;
- one logical mutation is recorded per scoped idempotency key;
- ambiguous outcomes remain replay-safe;
- reserve, confirm, cancel, expiry, and settlement races have valid committed outcomes;
- one user cannot hold overlapping active schedule claims across every slot-owning organisation supported by the deployment;
- authoritative time and bounded timeout classification remain explicit;
- every admissible experiment reconciles outcomes with persisted state on every participating authority.

The partitioned design adds these placement invariants:

1. **One organisation, one writable home authority.** At any instant, every authoritative row for an organisation is written on exactly one assigned PostgreSQL authority.
2. **One `UserRef`, one schedule authority.** Every schedule claim for one stable `UserRef` lives on exactly one user-home authority, always. A slot-owning authority must never create an authoritative schedule claim for a user homed elsewhere.
3. **No failure fallback.** If an organisation's assigned authority is unavailable, requests remain routed to that authority and use the existing infrastructure classification: `timeout_db` when a database bound fires, or `internal_failure` for another definite infrastructure fault. They must never be retried against another writable shard.
4. **One routing decision.** All service and experiment components use the same versioned organisation-to-authority map for a run.
5. **One compatible schema.** Every authority participating in one deployment or experiment runs a schema version compatible with the serving binary.
6. **Placement decides coordination.** A booking uses the local transaction path when the user and slot organisations resolve to the same authority. Different organisation identifiers alone do not make a booking cross-shard.

The current user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. This note does not introduce a separate global person identity or merge distinct `UserRef` values that an external identity system may associate with the same person.

## 4. Two phases of horizontal database scaling

Let `authority(org)` denote the writable authority selected by the deployment's versioned placement map.

### 4.1 Phase 1 — organisation-affine, same-authority booking

Phase 1 assigns each organisation to one writable PostgreSQL home authority. One authority may host several organisations. It contains the complete transactional state needed for those organisations:

- their slots and slot-side lifecycle rows;
- their registered users and identity-serialization rows;
- those users' schedule claims;
- reservations and bookings whose participating organisations are colocated there;
- idempotency records;
- expiry and settlement state.

For AG-Sept, the supported booking policy is:

```text
authority(slot_organisation_id) == authority(user_organisation_id)
```

This is deliberately not the stronger condition:

```text
slot_organisation_id == user_organisation_id
```

Cross-organisation booking is already a shipped and tested capability. A user registered with organisation A may book a slot owned by organisation B while the schedule claim remains keyed by the user's `UserRef`. Phase 1 preserves that behaviour whenever A and B are assigned to the same writable authority, including the existing one-authority deployment where every organisation is colocated.

A reserve is refused before repository work only when the two organisations resolve to different authorities. It is not partially routed to either side and cannot create distributed partial state.

Because both business authorities are colocated for every supported booking, reserve, confirm, cancel, expiry, and settlement keep one PostgreSQL transaction. Existing row locks, the identity lock, the GiST exclusion constraint, PostgreSQL authoritative time, foreign keys, idempotency records, commit-ambiguity classification, and rollback behaviour remain intact.

#### Phase 1 topology

```text
                    versioned organisation placement
                                  │
                    ┌─────────────┴─────────────┐
                    │                           │
                    ▼                           ▼
          shard-affine service A      shard-affine service B
          one database pool           one database pool
                    │                           │
                    ▼                           ▼
          PostgreSQL authority A      PostgreSQL authority B
          organisations A, C          organisations B, D
```

A user from A may book a C-owned slot through authority A. A user from A cannot book a B- or D-owned slot in Phase 1 because that booking would span authorities A and B.

Each service instance remains shard-affine and opens one database pool. This avoids a `service replicas × shard count × pool size` connection fan-out and gives readiness one clear dependency.

For the first AG-Sept experiment, static routing may live in the load generator or experiment harness. A production routing gateway and dynamic placement service are not required to prove authority composition.

The current API carries enough organisation identity to route the supported operations:

- reserve routes to the slot organisation's authority, then validates that the user organisation resolves to the same authority;
- confirm and cancel route by `user_organisation_id` from the request body;
- list-slots routes by its required `slot_organisation_id` query parameter.

Reservation and booking identifiers remain server-minted opaque identifiers. Phase 1 does not encode physical shard location into them; the stable organisation identity remains the routing key, so a later placement change does not require changing entity identity.

#### What Phase 1 proves

Phase 1 can establish that:

- complete organisation authorities can be placed on independent PostgreSQL writers;
- same-authority operations preserve the accepted local transaction semantics;
- the existing cross-organisation schedule invariant remains exercised when participating organisations are colocated;
- a failure of one authority does not corrupt or block unrelated authorities;
- aggregate database capacity can be composed across independent organisations when the authorities receive independent resources;
- one hot organisation, slot, or identity remains bounded by its home authority, by design.

The precise claim is:

> Alloca horizontally composes independent organisation authorities while preserving one local transaction for every booking whose authorities are colocated.

Phase 1 does **not** prove that one organisation can use several writable database authorities or that a booking spanning two writable authorities can commit safely.

### 4.2 Phase 2 — cross-authority booking

Phase 2 may later support:

```text
authority(slot_organisation_id) != authority(user_organisation_id)
```

The Phase 1 placement remains unchanged:

- slot capacity stays authoritative on the slot organisation's home shard;
- every schedule claim for the user stays authoritative on the user's home shard;
- the same-authority path continues to use one local PostgreSQL transaction;
- only the cross-authority path pays the cost of distributed coordination.

A future cross-authority protocol must explicitly define reserve, confirm, cancel, expiry, replay, visibility, compensation, and recovery under partial failure. Distributed atomic commit and a durable saga remain candidate families; this note deliberately selects neither.

Phase 2 must not introduce a shared global workflow database that becomes the writable authority for every cross-authority booking. Any durable coordination state must be partitioned with one of the participating authorities, most plausibly the user-home authority, and participant commands must be idempotent under stable logical identifiers.

### 4.3 Phase 1 compatibility obligations

Phase 1 must leave Phase 2 open by preserving the following seams:

1. **Keep slot and user organisation identities distinct in every command and row.** Phase 1 compares their resolved authorities; it must not collapse the fields into one ambiguous `organisation_id`.
2. **Keep organisation placement behind a stable routing boundary.** Callers ask for the authority assigned to an organisation; business code does not derive a DSN directly.
3. **Keep the same-authority service path intact.** A future coordinator dispatches to it or to equivalent participant commands rather than rewriting local transaction semantics.
4. **Keep schedule authority on the user home.** Phase 1 must not optimise by duplicating claims onto slot shards.
5. **Keep identifiers globally collision-resistant and location-independent.** Logical identity must survive a future routing or placement change.
6. **Keep idempotency domain-local but protocol-extensible.** Existing client idempotency remains in the local transaction; Phase 2 may add separate participant-command idempotency without changing the client contract.
7. **Make verification authority-aware.** Phase 1 verifies each shard and aggregates the verdict after the run is quiesced. Phase 2 can extend the same verifier with cross-authority workflow assertions.
8. **Preserve cross-organisation behaviour when colocated.** The deployment policy refuses only cross-authority booking; it must not delete the two-organisation data model or remove the existing invariant tests.
9. **Represent unsupported cross-authority booking as an explicit policy outcome.** The request remains structurally expressible so Phase 2 can later support it without an API redesign.

## 5. Phase 1 routing and lifecycle rules

### 5.1 Static placement for AG-Sept

The smallest experiment uses a versioned static mapping:

```text
organisation A -> authority A
organisation B -> authority B
organisation C -> authority A
```

The mapping is immutable for the duration of a run. Every artifact records its routing version and authority assignment. Setup fails if an organisation is absent, assigned more than once, or if participating authorities report incompatible schema versions.

Modulo hashing is not required for the first experiment. A static assignment is easier to inspect, controls tenant skew, and does not pretend that online rebalancing has been solved.

### 5.2 Shard-affine service units

Each service unit:

- serves only the organisations assigned to its database authority;
- opens one PostgreSQL pool;
- reports its authority identifier and routing version through operational metadata;
- becomes unready when that authority is unavailable;
- never forwards a mutation to another shard as a fallback.

Once database authority composition succeeds, AG-Sept may add stateless replicas within each shard group if budget remains:

```text
service A1 ─┐
service A2 ─┼── PostgreSQL authority A
service A3 ─┘
```

That is application scaling inside one authority unit. It is deliberately subsequent to proving that independent database authorities route and reconcile correctly.

### 5.3 Reserve routing and cross-authority refusal

Reserve first resolves the two placement keys:

```text
slot_authority = authority(slot_organisation_id)
user_authority = authority(user_organisation_id)
```

When they are equal, the request executes through the existing local transaction, whether or not the organisation identifiers themselves are equal.

When they differ, Phase 1 returns:

```text
outcome = business_refusal
reason  = cross_authority_unsupported
status  = 409
```

`cross_authority_unsupported` is a new normative domain reason, not an implementation-only label. Phase 1 must update the closed `domain.Reason` set, reason validation, HTTP/API documentation, invariant register, measurement contract where reasons are enumerated, telemetry allowlists, reconciliation, and direct tests. The refusal is recorded through normal idempotency semantics and must occur before either authority performs persistence work.

Its client meaning is:

> This deployment supports booking only when the user and slot organisations share one writable authority.

### 5.4 Confirm and cancel ownership

Confirm and cancel route by the asserted `user_organisation_id`, but the current service does not validate that the loaded reservation belongs to the supplied `UserRef`; today that value scopes idempotency only. Sharding must not make the result of a wrong identity depend on whether two organisations happen to be colocated.

Phase 1 therefore adds an explicit ownership check after loading the reservation and before any lifecycle mutation:

```text
reservation.UserRef == command.UserRef
```

A mismatch returns the existing `unknown_target` business refusal. This intentionally changes the current behaviour, where a caller with a reservation identifier can supply a different `UserRef` and still mutate the reservation. It is a domain-contract correction with normative documentation and tests, not an existing validation being preserved.

The check gives the same result in both placements:

- if the asserted user organisation routes to another authority, the reservation is absent and the request returns `unknown_target`;
- if the wrong identity is colocated with the reservation, the explicit comparison returns the same `unknown_target`;
- the correct `UserRef` routes to the reservation's authority and proceeds through the existing idempotent lifecycle path.

This is not authentication. The API still trusts the caller to assert a `UserRef`; a caller who knows both the reservation identifier and its exact owner can still act as that owner until authentication is added.

### 5.5 Reads, workers, readiness, and migrations

- list-slots routes by `slot_organisation_id` and reads one authority;
- expiry and settlement workers operate only on the shard-affine service unit's authority;
- readiness reports the availability of that service unit's one database dependency;
- every participating authority is migrated before traffic starts and reports a compatible schema version.

None of these paths requires a global write authority.

## 6. Phase 1 experiment and evidence gates

The minimum local experiment should contain:

- a minimal pair of independently migrated PostgreSQL authorities;
- one shard-affine service unit per authority;
- several organisations assigned across the authorities, including at least two organisations colocated on one authority;
- a generator that routes requests by the versioned placement map;
- a verifier that connects to every authority involved in the run and aggregates per-shard correctness results.

The correctness matrix must exercise:

1. same-organisation, same-authority booking succeeds through the local transaction;
2. cross-organisation, same-authority booking succeeds and preserves global schedule non-overlap for the user;
3. cross-authority reserve returns `cross_authority_unsupported` before persistence;
4. confirm and cancel with the correct `UserRef` preserve current lifecycle and replay semantics;
5. confirm and cancel with a wrong `UserRef` return `unknown_target` regardless of colocation;
6. capacity, schedule, idempotency, lifecycle, and outcome reconciliation pass independently on every authority.

Verification follows a quiesced consistency rule:

1. stop issuing new requests;
2. drain in-flight requests and settlement work;
3. read each authority after the supported workflows have reached terminal local state;
4. apply local capacity, schedule, idempotency, and outcome reconciliation;
5. aggregate the verdict without treating sequential cross-database reads as one atomic snapshot.

### 6.1 Failure-isolation evidence

Taking one authority unavailable is a separate failure-domain experiment, not a quotable steady-state capacity run. Requests for organisations assigned to the unavailable authority use the existing `timeout_db` or `internal_failure` classifications according to the failure mechanism; introducing a new generic unavailable outcome is not part of Phase 1.

The failure experiment must show that:

- the failed authority is never bypassed by writing its organisations elsewhere;
- service units for healthy authorities remain ready and continue processing their assigned organisations;
- healthy-authority state reconciles correctly;
- affected and unaffected request populations are reported separately;
- expected failed-authority outcomes are not presented as an SLO-compliant capacity result.

### 6.2 Capacity-composition evidence

A local shared-workstation run must not claim a throughput multiplier. A capacity-composition result requires independent database resources, controlled service resources, generator headroom, and the evidence contract's normal SLO and quotability gates. All authorities must be healthy for that run.

## 7. Risks and explicit limitations

### 7.1 One large organisation remains one authority

Phase 1 scales across independent organisations, not inside one organisation. A very large organisation can still reach the frontier of its assigned PostgreSQL authority.

Later subdivision by full `UserRef`, slot groups, or another stable placement would reopen the cross-authority transaction problem. It is not hidden inside Phase 1.

### 7.2 Placement skew

Equal organisation counts do not imply equal load. Static placement must account for known heavy organisations in the experiment, and every result must report the organisation-to-authority distribution it measured.

### 7.3 Routing split-brain

Different service or generator instances using different placement maps could write one organisation to multiple authorities and silently divide its source of truth.

The routing version must therefore be immutable for a run, validated at startup, and recorded in operational metadata and result artifacts.

### 7.4 Rebalancing is deferred

Moving an organisation between authorities requires transferring slots, reservations, bookings, claims, idempotency history, and lifecycle state without allowing concurrent writes on both sides. Phase 1 uses fixed placement and resets or reseeds between topology changes.

### 7.5 Global reads become fan-out work

The current booking path is organisation-scoped and remains local. Future cross-organisation administration, analytics, and search require fan-out reads or a separate read model. They must not become a hidden global write authority.

### 7.6 Caller-asserted routing identity

The current API accepts user organisation from the request body because authentication is not yet implemented. The new reservation-owner comparison makes placement behaviour deterministic but does not create a production trust boundary. A later authenticated system should route from trusted identity claims.

### 7.7 Authority failure and outcome accounting

An authority outage makes requests for its organisations fail through existing infrastructure outcomes. That is the expected blast radius of shard-affine placement, but it means failure-isolation runs cannot be judged by the same aggregate SLO gates as healthy capacity runs. The experiment contract must keep those evidence classes separate.

### 7.8 Resource confounding

Adding both a service process and a PostgreSQL instance can make a throughput comparison ambiguous. The capacity experiment must hold service compute comparable or report the service-to-database resource ratio explicitly.

## 8. Questions for review

1. Is organisation-home placement the correct Phase 1 unit for preserving every supported transaction on one authority?
2. Should the first experiment use shard-affine service units, as proposed, rather than one process with pools to every shard?
3. Is placement equality, rather than organisation-ID equality, the correct Phase 1 support boundary?
4. Is `cross_authority_unsupported` the correct normative refusal for a well-formed reserve that spans two writable authorities?
5. Is adding explicit `UserRef` ownership validation to confirm and cancel the correct way to avoid placement-dependent behaviour?
6. Are the one-organisation and one-`UserRef` placement invariants strong enough to prevent accidental split authority?
7. Do the Phase 1 compatibility obligations preserve the right seams for a later cross-authority protocol without building that protocol now?
8. What is the smallest authority-aware verifier extension that makes the local run admissible?
9. Which service and database resources must be controlled before aggregate authority capacity can be quoted?
10. Is any item in the proposed Phase 1 scope still coordination work disguised as routing or verification?

## 9. Immediate next step

Review this phased decision before freezing the PR3 implementation scope.

After review, define a bounded AG-Sept PR3 around Phase 1 only:

- static versioned organisation placement;
- shard-affine service/database units;
- same-authority local booking, including cross-organisation colocation coverage;
- explicit cross-authority refusal and its normative contract updates;
- explicit confirm/cancel ownership validation;
- multi-authority generator routing;
- authority-aware verification;
- correctness and failure-isolation evidence first;
- database capacity composition when the environment can support a valid comparison;
- stateless service scaling only if the Phase 1 database work leaves sufficient milestone budget.

## 10. Explicit non-goals

Phase 1 does not:

- support booking when the user and slot organisations resolve to different writable authorities;
- choose or implement two-phase commit, a saga, or another distributed transaction protocol;
- split one organisation across writable database authorities;
- build a production routing service or dynamic shard catalogue;
- solve online rebalancing or dual-write migration;
- add a shared global workflow database;
- add a new generic unavailable outcome for shard failure;
- claim a local shared-workstation throughput multiplier;
- make one hot slot or one hot identity parallel;
- require AWS, Kubernetes, PostgreSQL exporters, or a large dashboard surface before correctness evidence exists;
- weaken the global schedule invariant for any booking pattern the phase claims to support.

Phase 2 remains possible because Phase 1 preserves distinct user and slot identities, stable organisation placement, one user-home schedule authority, opaque logical identifiers, the complete same-authority transaction path, and authority-aware verification. It is deferred because its coordination and recovery cost does not fit AG-Sept, not because the data model or current same-authority behaviour has been simplified until it can no longer express it.