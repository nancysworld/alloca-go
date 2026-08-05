# Horizontal database authority

**Status:** Accepted in principle for AG-Sept PR3 implementation.  
**Scope:** define a phased route from one PostgreSQL writer to multiple writable authorities while preserving Alloca's invariants and accepted same-database transaction semantics.  
**Not yet normative:** this note does not replace [`transaction-semantics.md`](../design/transaction-semantics.md), authorise production sharding, or select a future cross-authority coordination protocol.

## 1. Why this note exists

AG-Sept originally planned to establish one service baseline and then add stateless service replicas against one PostgreSQL authority. PR2 changed the order of that investigation.

The measured single-instance report shows that one shared PostgreSQL authority becomes the aggregate limiting subsystem while `alloca-go` still has application-compute headroom. The exact database-side mechanism and all measured values remain owned by [`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md); this design note carries only the architectural conclusion.

Adding service replicas can raise throughput while database concurrency remains below that authority's frontier. It cannot move the saturated ceiling of the same writable authority. End-to-end horizontal scalability therefore needs a way to add writable database authority as well as stateless service compute.

The first version of this note investigated cross-authority booking immediately. Review exposed the cost accurately: once slot capacity and one user's schedule live on different PostgreSQL authorities, the operation needs distributed commit or a durable compensating workflow. Neither coordination family fits AG-Sept with enough correctness evidence.

The decision is therefore phased:

- **Phase 1 - AG-Sept:** compose independent organisation-home authorities while supporting bookings whose user and slot organisations resolve to the same writable authority;
- **Phase 2 - later:** retain that placement and add an explicitly designed cross-authority booking protocol when the project can fund its failure semantics and recovery model.

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

Local containers can establish routing, authority isolation, schema compatibility, correctness, and failure containment. They cannot establish a production database-capacity multiplier when the service, generator, telemetry stack, PostgreSQL instances, WSL2, and storage path share one workstation allocation. Any publishable capacity-composition claim requires independently provisioned database resources and a controlled service-compute comparison.

## 3. Correctness and placement invariants

Horizontal scaling does not change the AG-M1 correctness requirements:

- active reservations and bookings never exceed one slot's capacity;
- one logical mutation is recorded per scoped idempotency key;
- definite terminal outcomes remain replayable from the idempotency record;
- ambiguous commit outcomes remain safe to resolve by replaying the same key;
- reserve, confirm, cancel, expiry, and settlement races have valid committed outcomes;
- one user cannot hold overlapping active schedule claims across every slot-owning organisation supported by the deployment;
- authoritative time and bounded timeout classification remain explicit;
- every admissible experiment reconciles outcomes with persisted state on every participating authority.

The partitioned design adds these placement invariants:

1. **One organisation, one writable home authority.** At any instant, every authoritative row for an organisation is written on exactly one assigned PostgreSQL authority.
2. **One `UserRef`, one schedule and idempotency authority.** Every schedule claim and client idempotency record for one stable `UserRef` lives on that user's home authority.
3. **No failure fallback.** If an organisation's assigned authority is unavailable, requests remain routed to that authority. They must never be retried against another writable shard.
4. **One routing decision.** All service and experiment components use the same versioned organisation-to-authority map for a run.
5. **One compatible schema.** Every authority participating in one deployment or experiment runs a schema version compatible with the serving binary.
6. **Placement decides coordination.** A booking uses the local transaction path when the user and slot organisations resolve to the same authority. Different organisation identifiers alone do not make a booking cross-authority.
7. **No remote claim shortcut.** A slot-owning authority must never create an authoritative schedule claim for a user homed elsewhere. This is vacuous for supported Phase 1 bookings because both authorities are colocated, but it is a binding Phase 2 compatibility rule.

The current user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. This note does not introduce a separate global person identity or merge distinct `UserRef` values that an external identity system may associate with the same person.

## 4. Two phases of horizontal database scaling

Let `authority(org)` denote the writable authority selected by the deployment's versioned placement map.

### 4.1 Phase 1 - organisation-affine, same-authority booking

Phase 1 assigns each organisation to one writable PostgreSQL home authority. One authority may host several organisations. It contains the complete transactional state needed for those organisations:

- their slots and slot-side lifecycle rows;
- their registered users and identity-serialization rows;
- those users' schedule claims and client idempotency records;
- reservations and bookings whose participating organisations are colocated there;
- expiry and settlement state.

For AG-Sept, the supported booking policy is:

```text
authority(slot_organisation_id) == authority(user_organisation_id)
```

This is deliberately not the stronger condition:

```text
slot_organisation_id == user_organisation_id
```

Cross-organisation booking is already a shipped and tested capability. A user registered with organisation A may book a slot owned by organisation C while the schedule claim remains keyed by the user's `UserRef`. Phase 1 preserves that behaviour whenever A and C are assigned to the same writable authority, including the existing one-authority deployment where every organisation is colocated.

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

Each service instance remains shard-affine and opens one database pool. This avoids a `service replicas x shard count x pool size` connection fan-out and gives readiness one clear dependency.

For the first AG-Sept experiment, static routing may live in the load generator or experiment harness. A production routing gateway and dynamic placement service are not required to prove authority composition.

#### Mutation routing root

Client mutation routing follows the user-home authority:

- reserve routes by `user_organisation_id` from the request body;
- confirm and cancel route by `user_organisation_id` from the request body;
- list-slots remains a read and routes by its required `slot_organisation_id` query parameter.

User-home is the routing root because it owns the client idempotency scope and the schedule authority. This gives every mutation and replay one stable home even when the slot organisation is elsewhere.

For a same-authority reserve, the user-home service validates that the slot organisation resolves to the same authority and then executes the existing local transaction. It does not need a second pool or remote call.

Reservation and booking identifiers remain server-minted opaque identifiers. Phase 1 does not encode physical shard location into them; stable organisation identity remains the routing key, so a later placement change does not require changing entity identity.

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

### 4.2 Phase 2 - cross-authority booking

Phase 2 may later support:

```text
authority(slot_organisation_id) != authority(user_organisation_id)
```

The Phase 1 placement and routing ownership remain unchanged:

- slot capacity stays authoritative on the slot organisation's home shard;
- every schedule claim and client idempotency record stays authoritative on the user's home shard;
- the same-authority path continues to use one local PostgreSQL transaction;
- only the cross-authority path pays the cost of distributed coordination.

A future cross-authority protocol must explicitly define reserve, confirm, cancel, expiry, replay, visibility, compensation, and recovery under partial failure. Two-phase commit and a durable saga remain candidate families; this note deliberately selects neither.

Phase 2 must not introduce a shared global workflow database that becomes the writable authority for every cross-authority booking. Any durable coordination state must be partitioned with one of the participating authorities. The Phase 1 decision makes user-home the default owner unless later evidence rejects it, and participant commands must be idempotent under stable logical identifiers.

### 4.3 Phase 1 compatibility obligations

Phase 1 must leave Phase 2 open by preserving the following seams:

1. **Keep slot and user organisation identities distinct in every command and row.** Phase 1 compares their resolved authorities; it must not collapse the fields into one ambiguous `organisation_id`.
2. **Keep organisation placement behind a stable routing boundary.** Callers ask for the authority assigned to an organisation; business code does not derive a DSN directly.
3. **Keep the same-authority service path intact.** A future coordinator dispatches to it or to equivalent participant commands rather than rewriting local transaction semantics.
4. **Keep schedule and client-idempotency authority on user-home.** Phase 1 must not duplicate claims or client idempotency records onto slot shards.
5. **Keep identifiers globally collision-resistant and location-independent.** Logical identity must survive a future routing or placement change.
6. **Keep idempotency protocol-extensible.** Existing client idempotency remains on user-home; Phase 2 may add separate participant-command idempotency without changing the client contract.
7. **Make verification authority-aware.** Phase 1 verifies local invariants on every shard and aggregates the verdict after the run is quiesced. Phase 2 can extend the same verifier with cross-authority workflow assertions.
8. **Preserve cross-organisation behaviour when colocated.** The deployment policy refuses only cross-authority booking; it must not delete the two-organisation data model or remove the existing invariant tests.
9. **Represent unsupported cross-authority booking as an explicit policy outcome.** The request remains structurally expressible so Phase 2 can later support it without an API redesign.

## 5. Phase 1 routing and lifecycle rules

### 5.1 Static placement for AG-Sept

The smallest experiment uses a versioned static mapping:

```text
organisation A -> authority A
organisation B -> authority B
organisation C -> authority A
organisation D -> authority B
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

A request sent to a service unit that does not own its user organisation is a routing or deployment fault, not the `cross_authority_unsupported` business policy. It must be refused without booking-state mutation and must not be recorded as the user's durable domain outcome on the wrong authority. The exact edge classification and enforcement mechanism belong to PR3a.

Once database authority composition succeeds, AG-Sept may add stateless replicas within each shard group if budget remains:

```text
service A1 ─┐
service A2 ─┼── PostgreSQL authority A
service A3 ─┘
```

That is application scaling inside one authority unit. It is deliberately subsequent to proving that independent database authorities route and reconcile correctly.

### 5.3 Cross-authority reserve refusal

Reserve first resolves the two placement keys on the user-home service:

```text
user_authority = authority(user_organisation_id)
slot_authority = authority(slot_organisation_id)
```

When they are equal, the request executes through the existing local transaction, whether or not the organisation identifiers themselves are equal.

When they differ, Phase 1 returns:

```text
outcome = business_refusal
reason  = cross_authority_unsupported
status  = 409
```

`cross_authority_unsupported` is a new normative domain reason, not an implementation-only label.

The refusal follows the normal mutation contract:

- it is recorded on the **user-home authority** through the existing client idempotency scope;
- replay of the same key routes to user-home and returns the recorded refusal;
- the refusal transaction performs no slot lookup, reservation, claim, booking, or other slot-authority work;
- the slot authority is not contacted.

The phrase "before persistence" is therefore not the rule. The rule is:

> Reject and durably record the policy outcome on user-home before any slot-authority work or booking-state mutation.

The exact repository helper and transaction shape belong to PR3a. The normative contract updates include the closed domain reason set, HTTP/API documentation, invariant register, telemetry reason validation, reconciliation, and direct tests. A measurement-contract change is required only where that document intentionally enumerates refusal reasons.

Its client meaning is:

> This deployment supports booking only when the user and slot organisations share one writable authority.

### 5.4 Confirm and cancel ownership

Confirm and cancel route by the asserted `user_organisation_id`, but the current service does not validate that the reservation belongs to the supplied `UserRef`; today that value scopes idempotency only. Sharding must not make the result of a wrong identity depend on whether two organisations happen to be colocated.

Phase 1 therefore requires an explicit ownership comparison before any lifecycle mutation or avoidable slot contention:

```text
reservation.UserRef == command.UserRef
```

A mismatch returns:

```text
outcome = business_refusal
reason  = unknown_target
status  = 404
```

This intentionally changes the current behaviour from success to `404 unknown_target` when a caller supplies the wrong `UserRef`. It is a domain-contract correction with normative documentation and tests, not an existing validation being preserved.

The check gives the same result in both placements:

- if the asserted user organisation routes to another authority, the reservation is absent and the request returns `unknown_target`;
- if the wrong identity is colocated with the reservation, the explicit comparison returns the same `unknown_target`;
- the correct `UserRef` routes to the reservation's authority and proceeds through the existing idempotent lifecycle path.

The design requires the check before lifecycle mutation and unnecessary contention. Whether PR3a extends an existing reservation lookup or introduces another small read is an implementation decision.

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
- a generator that routes mutation requests by user organisation and read requests by their read authority;
- a verifier that connects to every authority involved in the run and produces one aggregate correctness verdict.

The correctness evidence separates supported traffic from policy controls.

### 6.1 Supported correctness workload

The supported workload contains only bookings satisfying:

```text
authority(slot_organisation_id) == authority(user_organisation_id)
```

It must exercise:

1. same-organisation, same-authority booking through the local transaction;
2. cross-organisation, same-authority booking and global schedule non-overlap for the user;
3. confirm and cancel with the correct `UserRef`, preserving lifecycle and replay semantics;
4. confirm and cancel with a wrong `UserRef`, returning `404 unknown_target` regardless of colocation;
5. capacity, schedule, idempotency, lifecycle, and outcome reconciliation on every authority.

### 6.2 Cross-authority refusal control

A separate bounded control sends reserves satisfying:

```text
authority(slot_organisation_id) != authority(user_organisation_id)
```

It must prove that:

- user-home records `cross_authority_unsupported` with normal idempotency semantics;
- replay returns the recorded refusal;
- no reservation, schedule claim, booking, or slot-side mutation is created;
- the slot authority receives no request for the refused booking.

The control is reported separately from supported-workload goodput and latency. Deliberate policy refusals must not distort a capacity comparison.

### 6.3 Authority-aware reconciliation

Verification follows a quiesced consistency rule:

1. stop issuing new requests;
2. drain in-flight requests and settlement work;
3. resolve any `unknown_replayable` mutation by replaying the same idempotency key after the authority is available;
4. read each authority after supported workflows have reached terminal local state;
5. check local safety invariants independently on every authority;
6. aggregate persisted and server totals across authorities and compare them once with the run's global client totals;
7. aggregate the verdict without treating sequential cross-database reads as one atomic snapshot.

The exact verifier data structures, query factoring, and scrape aggregation belong to PR3b. The architectural requirement is that a multi-organisation run must not compare one organisation's persisted rows against the run's unpartitioned global summary.

### 6.4 Failure-isolation evidence

Taking one authority unavailable is a separate failure-domain experiment, not a quotable steady-state capacity run.

Requests for organisations assigned to the unavailable authority may use the existing infrastructure classifications according to where failure occurs:

- `internal_failure` when the authority cannot be reached before a transaction begins;
- `timeout_db` when a database acquisition, lock, or statement bound fires;
- `unknown_replayable` when the connection is lost while commit may have succeeded but its acknowledgement is unavailable.

The failure experiment must show that:

- the failed authority is never bypassed by writing its organisations elsewhere;
- service units for healthy authorities remain ready and continue processing their assigned organisations;
- affected and unaffected request populations are reported separately;
- after restoration, ambiguous mutations are replayed with the same keys and resolved safely;
- healthy and restored authority state reconciles correctly;
- expected failed-authority outcomes are not presented as an SLO-compliant capacity result.

A deliberately timed connection loss during commit may discharge INV-21 as a by-product if PR3c can prove that exact fault. A generic authority shutdown does not claim that proof automatically; the fault-injection mechanism and test belong to implementation.

### 6.5 Capacity-composition evidence

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

The current API accepts user organisation from the request body because authentication is not yet implemented. The reservation-owner comparison makes placement behaviour deterministic but does not create a production trust boundary. A later authenticated system should route from trusted identity claims.

### 7.7 Authority failure and outcome accounting

An authority outage makes requests for its organisations fail through existing infrastructure outcomes. That is the expected blast radius of shard-affine placement, but it means failure-isolation runs cannot be judged by the same aggregate SLO gates as healthy capacity runs.

`unknown_replayable` is not a definite terminal mutation result. It is evidence that the client must replay the same key to determine whether a durable result exists. Reconciliation must preserve that distinction rather than treating an ambiguous first attempt as either an unrecorded definite outcome or a second logical mutation.

### 7.8 Resource confounding

Adding both a service process and a PostgreSQL instance can make a throughput comparison ambiguous. The capacity experiment must hold service compute comparable or report the service-to-database resource ratio explicitly.

## 8. Settled decisions and implementation handoff

The following architectural questions are settled for Phase 1:

1. organisation-home placement is the horizontal database unit;
2. placement equality, not organisation-ID equality, defines the supported local booking path;
3. user-home owns mutation routing, schedule claims, and client idempotency;
4. `cross_authority_unsupported` is a replayable `409 business_refusal` recorded on user-home without contacting slot-home;
5. confirm and cancel require matching `UserRef`, with mismatch returning `404 unknown_target`;
6. authority failure never triggers fallback and may surface `internal_failure`, `timeout_db`, or `unknown_replayable`;
7. supported-workload measurements and cross-authority refusal controls are separate evidence classes;
8. reconciliation checks local invariants per authority and compares aggregate persisted/server totals with global client totals after quiescence.

Implementation owns:

- placement-map representation and validation mechanics;
- how a shard-affine unit enforces its assigned organisation set;
- the repository helper used to record the cross-authority refusal;
- the cheapest pre-contention ownership lookup for confirm and cancel;
- generator, manifest, scrape, and verifier data structures;
- exact fault injection for the failure-isolation experiment and any INV-21 proof;
- orchestration details within the accepted container and time-budget constraints.

These mechanics may change without reopening the architecture, provided they preserve the settled contracts above.

## 9. Immediate next step

Freeze the PR3 implementation scope against this decision and proceed in three stages:

- PR3a: placement, routing policy, refusal contract, ownership correction, and discriminating tests;
- PR3b: containerised multi-authority topology, placement-aware generator, manifest, and aggregate verifier;
- PR3c: correctness, refusal, replay, and failure-isolation evidence.

Stateless service scaling follows only after Phase 1 database authority succeeds and the milestone budget still supports it.

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
- require AWS or Kubernetes before correctness evidence exists;
- weaken the global schedule invariant for any booking pattern the phase claims to support.

Phase 2 remains possible because Phase 1 preserves distinct user and slot identities, stable organisation placement, one user-home schedule and idempotency authority, opaque logical identifiers, the complete same-authority transaction path, and authority-aware verification. It is deferred because its coordination and recovery cost does not fit AG-Sept, not because the data model or current same-authority behaviour has been simplified until it can no longer express it.