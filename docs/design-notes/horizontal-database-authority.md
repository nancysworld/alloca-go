# Horizontal database authority

**Status:** Proposed for AG-Sept PR3 review.  
**Scope:** define a phased route from one PostgreSQL writer to multiple writable authorities while preserving Alloca's invariants and the accepted same-database transaction semantics.  
**Not yet normative:** this note does not replace [`transaction-semantics.md`](../design/transaction-semantics.md), authorise production sharding, or select a future cross-shard coordination protocol.

## 1. Why this note exists

AG-Sept originally planned to establish one service baseline and then add stateless service replicas against one PostgreSQL authority. PR2 changed the order of that investigation.

The measured single-instance report shows that one shared PostgreSQL authority becomes the aggregate limiting subsystem while `alloca-go` still has application-compute headroom. The exact database-side mechanism and all measured values remain owned by [`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md); this design note carries only the architectural conclusion.

Adding service replicas can raise throughput while database concurrency remains below that authority's frontier. It cannot move the saturated ceiling of the same writable authority. End-to-end horizontal scalability therefore needs a way to add writable database authority as well as stateless service compute.

The first version of this note investigated cross-shard booking immediately. Review exposed the cost accurately: once slot capacity and one user's schedule live on different PostgreSQL authorities, the operation needs distributed commit or a durable compensating workflow. Neither coordination family fits the remaining AG-Sept budget with enough correctness evidence.

The revised decision is therefore phased:

- **Phase 1 — AG-Sept:** compose independent organisation-home authorities while supporting same-shard booking only;
- **Phase 2 — later:** retain that placement and add an explicitly designed cross-shard booking protocol when the project can fund its failure semantics and recovery model.

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
- one user cannot hold overlapping active schedule claims within the booking policy the phase supports;
- authoritative time and bounded timeout classification remain explicit;
- every admissible experiment reconciles outcomes with persisted state on every participating authority.

The partitioned design adds these placement invariants:

1. **One organisation, one writable home authority.** At any instant, every authoritative row for an organisation is written on exactly one assigned PostgreSQL authority.
2. **One `UserRef`, one schedule authority.** Every schedule claim for one stable `UserRef` lives on exactly one user-home authority, always. A slot-owning authority must never create an authoritative schedule claim for a user homed elsewhere.
3. **No failure fallback.** If an organisation's assigned authority is unavailable, requests fail with a classified unavailable outcome. They must never be retried against another writable shard.
4. **One routing decision.** All service and experiment components use the same versioned organisation-to-authority map for a run.
5. **One compatible schema.** Every authority participating in one deployment or experiment runs a schema version compatible with the serving binary.

The current user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. This note does not introduce a separate global person identity or merge distinct `UserRef` values that an external identity system may associate with the same person.

## 4. Two phases of horizontal database scaling

### 4.1 Phase 1 — organisation-affine, same-shard booking

Phase 1 assigns each organisation to one writable PostgreSQL home authority. That authority contains the organisation's complete transactional state:

- its slots and slot-side lifecycle rows;
- its registered users and identity-serialization rows;
- those users' schedule claims;
- reservations and bookings;
- idempotency records;
- expiry and settlement state.

For AG-Sept, the booking policy requires:

```text
slot_organisation_id == user_organisation_id
```

A cross-organisation reserve is rejected before repository work with an explicit stable business refusal. It is not routed to either side and cannot create partial state.

Because both business authorities are colocated, every supported reserve, confirm, cancel, expiry, and settlement operation keeps the current single-PostgreSQL transaction. Existing row locks, the identity lock, the GiST exclusion constraint, PostgreSQL authoritative time, foreign keys, idempotency records, commit-ambiguity classification, and rollback behaviour remain intact.

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

Each service instance remains shard-affine and opens one database pool. This avoids a `service replicas × shard count × pool size` connection fan-out and gives readiness one clear dependency.

For the first AG-Sept experiment, static routing may live in the load generator or experiment harness. A production routing gateway and dynamic placement service are not required to prove authority composition.

The current API already carries the routing information needed by the supported policy:

- reserve routes by the organisation shared by `slot_organisation_id` and `user_organisation_id` after equality validation;
- confirm and cancel route by `user_organisation_id` from the request body;
- list-slots routes by its required `slot_organisation_id` query parameter.

Reservation and booking identifiers remain server-minted opaque identifiers. Phase 1 does not encode physical shard location into them; the stable organisation identity remains the routing key, so a later placement change does not require changing entity identity.

#### What Phase 1 proves

Phase 1 can establish that:

- complete organisation authorities can be placed on independent PostgreSQL writers;
- supported operations preserve the accepted local transaction semantics unchanged;
- a failure of one authority does not corrupt or block unrelated authorities;
- aggregate database capacity can be composed across independent organisations when the authorities receive independent resources;
- one hot organisation, slot, or identity remains bounded by its home authority, by design.

The precise claim is:

> Alloca horizontally composes independent organisation authorities.

Phase 1 does **not** prove that one organisation can use several writable database authorities or that every booking pattern is horizontally partitionable.

### 4.2 Phase 2 — cross-shard booking

Phase 2 may later support:

```text
slot_organisation_id != user_organisation_id
```

The Phase 1 placement remains unchanged:

- slot capacity stays authoritative on the slot organisation's home shard;
- every schedule claim for the user stays authoritative on the user's home shard;
- the same-shard path continues to use one local PostgreSQL transaction;
- only the cross-shard path pays the cost of distributed coordination.

A future cross-shard protocol must explicitly define reserve, confirm, cancel, expiry, replay, visibility, compensation, and recovery under partial failure. Distributed atomic commit and a durable saga remain candidate families; this note deliberately selects neither.

Phase 2 must not introduce a shared global workflow database that becomes the writable authority for every cross-shard booking. Any durable coordination state must be partitioned with one of the participating authorities, most plausibly the user-home authority, and participant commands must be idempotent under stable logical identifiers.

### 4.3 Phase 1 compatibility obligations

Phase 1 must leave Phase 2 open by preserving the following seams:

1. **Keep slot and user organisation identities distinct in every command and row.** Phase 1 validates equality as policy; it must not collapse the fields into one ambiguous `organisation_id`.
2. **Keep organisation placement behind a stable routing boundary.** Callers ask for the authority assigned to an organisation; business code does not derive a DSN directly.
3. **Keep the same-shard service path intact.** A future coordinator dispatches to it or to equivalent participant commands rather than rewriting local transaction semantics.
4. **Keep schedule authority on the user home.** Phase 1 must not optimise by duplicating claims onto slot shards.
5. **Keep identifiers globally collision-resistant and location-independent.** Logical identity must survive a future routing or placement change.
6. **Keep idempotency domain-local but protocol-extensible.** Existing client idempotency remains on the user-home transaction; Phase 2 may add separate participant-command idempotency without changing the client contract.
7. **Make verification authority-aware.** Phase 1 verifies each shard and aggregates the verdict after the run is quiesced. Phase 2 can extend the same verifier with cross-authority workflow assertions.
8. **Represent unsupported cross-shard booking as an explicit policy outcome.** Do not delete the two-organisation data model or make the request structurally impossible merely to simplify Phase 1.

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

### 5.3 Cross-organisation refusal

A reserve with different user and slot organisations is well-formed but unsupported by the Phase 1 booking policy. The service should return one stable business-refusal reason owned by the domain and mapped through the existing closed HTTP outcome model.

The exact reason name is an implementation-scope decision, but its semantics must be unambiguous:

> This deployment supports booking only when the user and slot organisations share one writable authority.

Confirm and cancel continue to validate ownership and idempotency on the routed user-home shard. A caller supplying the wrong organisation reaches an authority on which the reservation does not exist and receives the existing safe unknown-target behaviour.

## 6. Phase 1 experiment and evidence gates

The minimum local experiment should contain:

- a minimal pair of independently migrated PostgreSQL authorities;
- one shard-affine service unit per authority;
- several organisations assigned across the authorities;
- a generator that routes requests by the versioned placement map and generates same-shard traffic only;
- a verifier that connects to every authority involved in the run and aggregates per-shard correctness results.

The local experiment is correctness-first. It must prove:

- no supported request reaches the wrong authority;
- cross-organisation reserve is refused before persistence;
- capacity, schedule, idempotency, lifecycle, and outcome-reconciliation gates pass independently on every shard;
- taking one authority unavailable affects only its assigned organisations;
- restoration does not require writing those organisations on another shard;
- schema and routing metadata are recorded with the result.

Verification follows a quiesced consistency rule:

1. stop issuing new requests;
2. drain in-flight requests and settlement work;
3. read each authority after the supported workflows have reached terminal local state;
4. apply local capacity, schedule, idempotency, and outcome reconciliation;
5. aggregate the verdict without treating sequential cross-database reads as one atomic snapshot.

A local shared-workstation run must not claim a throughput multiplier. A capacity-composition result requires independent database resources, controlled service resources, generator headroom, and the evidence contract's normal SLO and quotability gates.

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

The current API accepts user organisation from the request body because authentication is not yet implemented. That is adequate for the synthetic experiment but not a production trust boundary. A later authenticated system should route from trusted identity claims.

### 7.7 Resource confounding

Adding both a service process and a PostgreSQL instance can make a throughput comparison ambiguous. The capacity experiment must hold service compute comparable or report the service-to-database resource ratio explicitly.

## 8. Questions for review

1. Is organisation-home placement the correct Phase 1 unit for preserving every supported transaction on one authority?
2. Should the first experiment use shard-affine service units, as proposed, rather than one process with pools to every shard?
3. Is rejecting cross-organisation reserve as a business refusal the correct API semantics for Phase 1?
4. Are the one-organisation and one-`UserRef` placement invariants strong enough to prevent accidental split authority?
5. Do the Phase 1 compatibility obligations preserve the right seams for a later cross-shard protocol without building that protocol now?
6. What is the smallest authority-aware verifier extension that makes the local run admissible?
7. Which service and database resources must be controlled before aggregate authority capacity can be quoted?
8. Is any item in the proposed Phase 1 scope still coordination work disguised as routing or verification?

## 9. Immediate next step

Review this phased decision before freezing the PR3 implementation scope.

After review, define a bounded AG-Sept PR3 around Phase 1 only:

- explicit same-shard booking policy;
- static versioned organisation placement;
- shard-affine service/database units;
- multi-authority generator routing;
- authority-aware verification;
- correctness and failure-isolation evidence first;
- database capacity composition when the environment can support a valid comparison;
- stateless service scaling only if the Phase 1 database work leaves sufficient milestone budget.

## 10. Explicit non-goals

Phase 1 does not:

- support cross-organisation booking;
- choose or implement two-phase commit, a saga, or another distributed transaction protocol;
- split one organisation across writable database authorities;
- build a production routing service or dynamic shard catalogue;
- solve online rebalancing or dual-write migration;
- add a shared global workflow database;
- claim a local shared-workstation throughput multiplier;
- make one hot slot or one hot identity parallel;
- require AWS, Kubernetes, PostgreSQL exporters, or a large dashboard surface before correctness evidence exists;
- weaken the global schedule invariant for any booking pattern the phase claims to support.

Phase 2 remains possible because Phase 1 preserves distinct user and slot identities, stable organisation placement, one user-home schedule authority, opaque logical identifiers, the complete same-shard transaction path, and authority-aware verification. It is deferred because its coordination and recovery cost does not fit AG-Sept, not because the data model has been simplified until it can no longer express it.
