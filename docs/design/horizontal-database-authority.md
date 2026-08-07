# Horizontal database authority and ownership axes

**Status:** Accepted — normative for the Phase 1 horizontal-database architecture.  
**Origin:** promoted from the AG-Sept horizontal-database design note after PR3a settled the placement and booking contracts.  
**Scope:** define how Alloca's logical correctness authorities map onto independently writable PostgreSQL database authorities, what same- and cross-database-authority booking mean, and which Phase 1 constraints must remain stable for a future cross-authority protocol.  
**Does not replace:** [`transaction-semantics.md`](transaction-semantics.md), which remains authoritative for the local transaction, state machines, exact lock participation/order, idempotency, authoritative time, and outcome semantics.

## 1. Why this design exists

AG-Sept originally planned to establish one service baseline and then add stateless service replicas against one PostgreSQL writer. PR2 changed the order of that investigation.

The measured single-instance report shows that one shared PostgreSQL writer becomes the aggregate limiting subsystem while `alloca-go` still has application-compute headroom. The exact measurements and database-side diagnosis remain owned by [`ag-sept-pr2-single-instance-frontier.md`](../measurements/reports/ag-sept-pr2-single-instance-frontier.md); this document owns the architectural consequence.

Adding service replicas can increase throughput while database concurrency remains below the writer's frontier. It cannot move the saturated ceiling of that same writer. End-to-end horizontal scalability therefore requires the ability to add independently writable database authority as well as stateless service compute.

The first design attempt went directly to cross-database booking. That exposed the real boundary: once slot capacity and one user's schedule live on different PostgreSQL transaction domains, the operation no longer fits one local `BEGIN ... COMMIT`. It needs an explicit distributed-coordination protocol with defined commit, replay, recovery, visibility, and partial-failure semantics.

The design is therefore phased:

- **Phase 1 — implemented by AG-Sept:** compose independent organisation-home database authorities while supporting bookings whose slot and user ownership axes resolve to the same database authority;
- **Phase 2 — later:** retain the same placement model and local path, and add an explicitly designed protocol for bookings whose two ownership axes resolve to different database authorities.

Phase 1 is not a mock of Phase 2. It is the durable local transaction path and the first horizontal database unit. Phase 2 must extend it rather than weaken or replace it.

## 2. What scalability means here

Alloca scalability is not one maximum-throughput number. It is the behaviour of several independently constrained dimensions:

- service compute can increase through stateless replicas;
- writable database capacity can increase through independent database authorities;
- unrelated organisations should be able to progress without sharing one PostgreSQL lock manager, WAL/commit stream, buffer/cache working set, connection ceiling, or storage bottleneck;
- one hot slot remains serialized by its slot-capacity logical authority;
- one hot user's claim creation remains serialized by the user-schedule serialization authority, while claim validity remains protected by the claim-validity authority;
- the service-to-database resource ratio is a property of a topology and workload, not a constant.

A useful scale experiment therefore names both dimensions, for example:

```text
N service replicas : M writable PostgreSQL database authorities
```

and states which workload distribution it supports. A dispersed multi-organisation workload, a hot organisation, a hot slot, and a hot identity do not share one meaningful scale-efficiency claim.

Local containers can prove routing, placement enforcement, authority isolation, schema compatibility, correctness, and failure containment. They cannot establish a production database-capacity multiplier while the service, generator, telemetry stack, PostgreSQL instances, WSL2, and storage path share one workstation allocation. Any capacity-composition claim requires controlled, independently provisioned resources.

## 3. Authority terminology and placement invariants

The word **authority** appears at three architectural levels. They must not be conflated.

### 3.1 Logical authority — who owns a correctness decision

A **logical authority** owns one correctness decision or invariant. Alloca's booking transaction has three:

| Logical authority | Correctness responsibility | Natural key | Ownership axis |
|---|---|---|---|
| slot-capacity authority | capacity is never oversold | `SlotRef = (slot_organisation_id, slot_id)` | slot axis |
| user-schedule serialization authority | claim-creating schedule mutations for one user are ordered before claim validation | `UserRef = (user_organisation_id, user_id)` | user axis |
| claim-validity authority | one user cannot hold overlapping active schedule claims | `UserRef` plus claim interval | user axis |

These remain three distinct logical authorities even when all of their rows live in one PostgreSQL database.

The slot row is the capacity authority: capacity-changing operations serialize on the slot aggregate lock.

The user identity row is the schedule-serialization authority for claim creation. Claim-creating operations — currently `reserve` — for one `UserRef` take the same identity lock before attempting claim insertion, so concurrent overlapping inserts do not use the GiST exclusion index itself as their ordering mechanism. Other claim lifecycle mutations are not redefined by this statement; [`transaction-semantics.md`](transaction-semantics.md) owns their exact lock participation and race semantics.

The `user_time_claims` relation and exclusion constraint are the claim-validity authority: they are the durable proof that the resulting schedule contains no overlapping active claims. Even if a writer skipped the identity lock, the exclusion constraint would preserve validity; the lost property would be orderly/liveness-safe claim creation, not schedule correctness.

The identity row and claim relation are therefore separate logical authorities because they guarantee different properties. They belong to one ownership axis because both govern the same user's schedule and are naturally keyed by the same `UserRef`.

### 3.2 Ownership axis — how logical authority naturally partitions

An **ownership axis** is the entity dimension by which a logical authority naturally partitions.

Alloca has two independent ownership axes:

```text
slot axis                         user axis
─────────                         ─────────
SlotRef                           UserRef
   │                                 │
   └─ slot-capacity authority        ├─ schedule-serialization authority
                                     └─ claim-validity authority
```

The three logical authorities therefore produce **two**, not three, database-placement axes.

The user-schedule serialization and claim-validity authorities are colocated by design in the current PostgreSQL model. For reserve, the identity lock orders claim creation before the exclusion-constraint validation; identity-scoped settlement, conflict decision, and claim creation then remain inside the same local transaction on user-home. Splitting the identity row and claims across independent writable databases is not physically impossible, but it would remove that local coordination and require another distributed protocol merely to preserve one user's schedule path. Phase 1 introduces no such protocol.

The deeper reason for the colocation is not the implementation detail of `SELECT ... FOR UPDATE`; it is that both logical authorities jointly govern one user schedule. The identity-row lock is the current mechanism that gives claim creation an ordered local path before validity is decided by the claim relation.

The two ownership axes are independent. A `UserRef` does not determine which `SlotRef` it will book, and a `SlotRef` does not determine which `UserRef` will consume it. A reserve operation joins them.

Client idempotency follows the user axis because the scoped key contains `UserRef` and every mutation and replay needs one stable home. It is part of the mutation protocol, not a fourth business logical authority in the three-authority model.

### 3.3 Database authority — where logical authorities can coordinate atomically

A **database authority** is one independently writable PostgreSQL transaction domain that is authoritative for a placed set of organisations.

A database authority provides one local commit/rollback boundary and one local concurrency/constraint domain for the rows it hosts. Its transactions can coordinate the logical authorities inside that database with ordinary PostgreSQL locks, constraints, foreign keys, and WAL-backed commit semantics.

A database authority is **not**:

- a service process;
- a service replica;
- a database connection pool;
- one organisation;
- one of the three logical authorities.

Several stateless service replicas may open separate pools to the same PostgreSQL writer and still share **one** database authority:

```text
service 1a ─┐
service 1b ─┼──> PostgreSQL database authority 1
service 1c ─┘
```

Conversely, two PostgreSQL writers are two database authorities even if one host runs both for a local experiment.

In this document, an unqualified `authority(org)` in a placement equation means **database authority**. Logical authority is always qualified explicitly.

### 3.4 Same- and cross-database-authority booking

Let `authority(org)` denote the database authority selected by the versioned organisation-placement map.

A reserve is **same-database-authority** when:

```text
authority(user_organisation_id) == authority(slot_organisation_id)
```

All three logical authorities still remain distinct, but the two ownership axes are hosted by one PostgreSQL transaction domain, so one local transaction can coordinate the reserve path.

Same-database-authority does **not** mean same organisation. For example:

```text
org-a -> authority-1
org-c -> authority-1
```

A user from `org-a` booking a slot owned by `org-c` is cross-organisation but same-database-authority, and remains supported.

A reserve is **cross-database-authority** when:

```text
authority(user_organisation_id) != authority(slot_organisation_id)
```

Then the user axis and slot axis are split across independent PostgreSQL commit domains:

```text
database authority 1                database authority 2
────────────────────                ────────────────────
user identity row                   slot row
user schedule claims                slot capacity
client idempotency
          \                           /
           \                         /
            distributed coordination
                 would be required
```

This split is the Phase 2 problem. Phase 1 refuses it rather than pretending two local transactions are one atomic booking.

The public reason name `cross_authority_unsupported` is retained for API compatibility; within the architecture it means **cross-database-authority unsupported**.

### 3.5 Correctness and placement invariants

Horizontal scaling does not weaken AG-M1 correctness:

- active reservations and bookings never exceed one slot's capacity;
- one logical mutation is recorded per scoped idempotency key;
- definite terminal outcomes remain replayable from the idempotency record;
- ambiguous commit outcomes remain safe to resolve by replaying the same key;
- reserve, confirm, cancel, expiry, and settlement races produce valid committed outcomes;
- one user cannot hold overlapping active schedule claims across every slot-owning organisation supported by the deployment;
- authoritative time and bounded timeout classification remain explicit;
- every admissible experiment reconciles client outcomes, server observations, and persisted state across every participating database authority.

The partitioned design adds these invariants:

1. **One organisation, one writable database home.** `authority(org)` returns exactly one writable home for the duration of a routing version. Slot-axis state owned by that organisation and user-axis state for identities issued by that organisation use that home.
2. **One `UserRef`, one user-axis home.** The user's identity-serialization row, schedule claims, and client idempotency records live on the user's home database authority and are never duplicated onto a slot authority.
3. **No failure fallback.** If an organisation's assigned database authority is unavailable, requests remain assigned there. They are never retried against another writer.
4. **One routing decision.** Services, generators, and verifiers use the same versioned organisation-to-database-authority placement for a run.
5. **One compatible schema.** Every participating database authority runs the schema version expected by the serving binary.
6. **Placement decides coordination.** Organisation-identifier inequality alone never defines a distributed operation; database-authority inequality does.
7. **No remote claim shortcut.** A slot-home database authority never creates the authoritative schedule claim for a user homed elsewhere.
8. **No split user axis.** Phase 1 never places one `UserRef`'s identity-serialization authority and claim-validity authority on different database authorities.

The user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. This design does not introduce a global person identity or merge distinct `UserRef` values that an external identity system may associate with one person.

## 4. Two phases of horizontal database scaling

### 4.1 Phase 1 — organisation-affine, same-database-authority booking

Phase 1 assigns each organisation to one writable PostgreSQL database authority. One database authority may host several organisations.

For an organisation placed on an authority:

- slots it owns and their slot-side lifecycle state live there;
- user identities issued under it, their identity-serialization rows, schedule claims, and client idempotency records live there;
- reservations and bookings are created only when the participating user and slot organisations resolve to that same database authority, so their complete local transaction state is colocated;
- expiry and settlement operate locally on that authority.

The supported booking condition is:

```text
authority(slot_organisation_id) == authority(user_organisation_id)
```

not:

```text
slot_organisation_id == user_organisation_id
```

Cross-organisation booking is an existing capability. Phase 1 preserves it whenever the participating organisations are colocated on the same database authority, including the original one-authority deployment where every organisation is colocated.

For a supported reserve, the local transaction coordinates the three logical authorities involved in claim creation:

```text
BEGIN
    lock slot row                  # slot-capacity logical authority
    lock user identity row         # schedule-serialization logical authority
    settle/check/create claim      # claim-validity logical authority
    write mutation + idempotency
COMMIT
```

The exact lock order and lifecycle-operation details remain owned by [`transaction-semantics.md`](transaction-semantics.md). The important horizontal-scaling property is that Phase 1 keeps the complete supported reserve sequence inside one database authority.

#### Phase 1 topology

```text
                     versioned organisation placement
                                   │
                     ┌─────────────┴─────────────┐
                     │                           │
                     ▼                           ▼
           shard-affine service 1      shard-affine service 2
                     │                           │
                     ▼                           ▼
       PostgreSQL database authority 1  PostgreSQL database authority 2
              organisations A, C               organisations B, D
```

A user from A may book a C-owned slot through database authority 1. A user from A cannot book a B- or D-owned slot in Phase 1 because that reserve would split the user and slot ownership axes across database authorities 1 and 2.

Each shard-affine service unit opens one database pool to one authority. This avoids a `service replicas × shard count × pool size` connection fan-out and gives readiness one clear database dependency.

Static routing is sufficient for the first evidence. A production routing gateway and dynamic placement service are not prerequisites for proving database-authority composition.

#### Mutation routing root

Mutations route by the user axis:

- reserve routes by `user_organisation_id`;
- confirm and cancel route by `user_organisation_id`;
- list-slots is a read and routes by `slot_organisation_id`.

User-home is the mutation routing root because it owns the user's schedule state and client-idempotency scope. Every mutation and replay therefore has one stable home even when the slot belongs to another colocated organisation.

For a same-database-authority reserve, user-home validates that the slot organisation resolves to the same database authority and executes the existing local transaction. It does not open a second pool or make a remote participant call.

Reservation and booking identifiers remain opaque logical identifiers. Phase 1 does not encode physical database location into them.

#### What Phase 1 proves

Phase 1 can establish that:

- independent organisation homes can be placed on independent PostgreSQL writers;
- same-database-authority operations preserve accepted local transaction semantics;
- colocated cross-organisation booking continues to exercise the global user-schedule invariant;
- failure of one database authority does not corrupt or block unrelated authorities;
- database capacity can be composed across independent organisation homes when the authorities receive independent resources;
- one hot organisation, slot, or identity remains bounded by its home database authority and logical serialization point by design.

The precise architectural claim is:

> Alloca horizontally composes independent database authorities across organisation homes while preserving one local PostgreSQL transaction whenever the slot and user ownership axes resolve to the same database authority.

Phase 1 does **not** prove that one organisation can span several writable database authorities or that a booking spanning two database authorities can commit safely.

### 4.2 Phase 2 — cross-database-authority booking

Phase 2 may later support:

```text
authority(slot_organisation_id) != authority(user_organisation_id)
```

The Phase 1 ownership model remains:

- slot-capacity authority stays on slot-home;
- the complete user axis — identity serialization, schedule claims, and client idempotency — stays on user-home;
- same-database-authority booking continues to use one local PostgreSQL transaction;
- only the cross-database path pays distributed-coordination cost.

A future protocol must define reserve, confirm, cancel, expiry, replay, visibility, compensation, and recovery under partial failure. Two-phase commit and durable saga/workflow protocols remain possible families; this design deliberately selects neither.

Phase 2 must not solve the problem by creating one shared global workflow database that becomes the writable authority for every cross-database booking. Durable coordination state must itself have a partitionable owner. User-home is the Phase 1 default candidate because mutation identity and replay already live there, but a future design may change that only with explicit evidence and a replacement correctness argument.

### 4.3 Phase 1 compatibility obligations

Phase 1 preserves these seams for Phase 2:

1. **Keep slot and user organisation identities distinct.** Commands and rows retain `slot_organisation_id` and `user_organisation_id`; they are never collapsed into one ambiguous `organisation_id`.
2. **Keep placement behind a routing boundary.** Business code asks which database authority owns an organisation; it does not derive DSNs directly.
3. **Keep the same-database-authority path intact.** A future coordinator invokes this path or equivalent participant commands rather than rewriting local transaction semantics.
4. **Keep the entire user axis on user-home.** Identity rows, claims, and client idempotency are not copied to slot-home.
5. **Keep identifiers location-independent.** Logical identity survives placement changes.
6. **Keep idempotency protocol-extensible.** Existing client idempotency stays on user-home; Phase 2 may add participant-command idempotency without changing the client contract.
7. **Keep verification database-authority-aware.** Local invariants are checked per database authority, then global run totals are compared once after quiescence.
8. **Preserve colocated cross-organisation behaviour.** Phase 1 refuses cross-database booking, not cross-organisation booking.
9. **Keep cross-database requests structurally expressible.** They return an explicit policy outcome rather than disappearing from the API model.

## 5. Phase 1 routing and lifecycle rules

### 5.1 Static placement for AG-Sept

The smallest topology uses a versioned static mapping:

```text
organisation A -> database authority 1
organisation B -> database authority 2
organisation C -> database authority 1
organisation D -> database authority 2
```

The mapping is immutable for a run. Every evidence artifact records its routing version and organisation-to-authority assignment. Setup fails if an organisation is absent, assigned ambiguously, or participating units do not agree with the versioned placement they claim to serve.

Static placement is deliberate. It is easier to inspect, controls skew explicitly, and does not imply that online rebalancing has been solved.

### 5.2 Shard-affine service units

Each service unit:

- serves only organisations assigned to its database authority;
- opens one PostgreSQL pool to that authority;
- reports its database-authority identifier and routing version through operational metadata;
- becomes unready when its authority is unavailable;
- never forwards a mutation to another database authority as a write fallback.

A request sent to a unit that does not own its routing organisation is a topology or deployment fault, not the `cross_authority_unsupported` business policy. It is refused before domain mutation and is never recorded as the user's durable domain outcome on the wrong database authority.

Additional stateless replicas may later share one database authority:

```text
service 1a ─┐
service 1b ─┼── PostgreSQL database authority 1
service 1c ─┘
```

That is service scaling **within** one database-authority unit; it does not create new writable database authority.

### 5.3 Cross-database-authority reserve refusal

Reserve resolves both placement keys on user-home:

```text
user_database_authority = authority(user_organisation_id)
slot_database_authority = authority(slot_organisation_id)
```

When equal, the request executes through the existing local transaction whether or not the organisation identifiers are equal.

When different, Phase 1 returns:

```text
outcome = business_refusal
reason  = cross_authority_unsupported
status  = 409
```

The refusal follows the normal mutation contract:

- it is durably recorded on **user-home** through the existing client-idempotency scope;
- replay of the same key routes to user-home and returns the recorded refusal;
- the refusal transaction performs no slot lookup, reservation, claim, booking, or other slot-home work;
- slot-home is not contacted.

The normative rule is:

> Reject and durably record the cross-database-authority policy outcome on user-home before any slot-home work or booking-state mutation.

Its client meaning is:

> This deployment supports booking only when the user and slot organisations share one writable database authority.

### 5.4 Confirm and cancel ownership

Confirm and cancel route by the asserted `user_organisation_id`. Their result must not depend on whether a wrong caller happens to be colocated with the reservation.

The lifecycle mutation therefore requires:

```text
reservation.UserRef == command.UserRef
```

A mismatch returns:

```text
outcome = business_refusal
reason  = unknown_target
status  = 404
```

The same semantic result holds whether the wrong identity is on another database authority or merely another organisation on the same authority. The check occurs before lifecycle mutation and avoidable slot contention.

This is ownership consistency, not authentication. The API still trusts the caller-supplied identity until authentication is added.

### 5.5 Reads, workers, readiness, and migrations

- list-slots routes by `slot_organisation_id` and reads one database authority;
- expiry and settlement workers act only on their shard-affine unit's database authority;
- readiness describes the availability of that unit's database dependency;
- every participating database authority is migrated before serving and must run the schema version the binary expects.

None requires a global write authority.

## 6. Phase 1 evidence gates

The minimum topology contains:

- at least two independently migrated PostgreSQL database authorities;
- one shard-affine service unit per authority for the initial experiment;
- several organisations distributed across them, including at least two colocated organisations;
- a generator routing mutations by user-home and reads by slot-home;
- a verifier reading every participating database authority and producing one aggregate correctness verdict.

### 6.1 Supported correctness workload

The supported workload contains only bookings satisfying:

```text
authority(slot_organisation_id) == authority(user_organisation_id)
```

It exercises:

1. same-organisation, same-database-authority booking;
2. cross-organisation, same-database-authority booking and user schedule non-overlap;
3. confirm and cancel with the correct `UserRef`;
4. confirm and cancel with a wrong `UserRef`, producing `unknown_target` independently of colocation;
5. capacity, schedule, idempotency, lifecycle, and outcome reconciliation on every database authority.

### 6.2 Cross-database-authority refusal control

A separate bounded control sends reserves satisfying:

```text
authority(slot_organisation_id) != authority(user_organisation_id)
```

It proves that user-home records `cross_authority_unsupported`, replay is stable, no booking state is created, and slot-home receives no work for the refused mutation.

This control is reported separately from supported-workload goodput and latency because a cheap deliberate refusal must not flatter a capacity result.

### 6.3 Database-authority-aware reconciliation

Verification uses a quiesced rule:

1. stop issuing new requests;
2. drain in-flight requests and settlement work;
3. resolve any `unknown_replayable` mutation by replaying the same idempotency key after the affected authority returns;
4. read each database authority after workflows have reached stable local state;
5. check local safety invariants independently on every authority;
6. difference each service unit's scrape pair independently, then aggregate;
7. aggregate persisted and server totals across authorities and compare them once with the run's global client totals;
8. never pretend sequential reads across independent databases form one atomic snapshot.

The key correctness rule is that a multi-organisation run never compares one organisation's persisted rows with the run's unpartitioned global client totals.

### 6.4 Failure-isolation evidence

Taking one database authority unavailable is a failure-domain experiment, not a steady-state capacity claim.

The exact infrastructure outcome depends on the injected fault and the layer whose bound fires; a stopped container, refused connection, killed backend, and lost commit acknowledgement need not classify identically. The experiment must name the injected failure mode rather than assuming one generic "authority unavailable" outcome.

It must show that:

- the failed database authority is never bypassed by writing its organisations elsewhere;
- healthy database authorities remain ready and continue serving their organisations;
- affected and unaffected request populations are reported separately;
- ambiguous mutations are resolved under the same keys after restoration;
- restored and healthy state reconciles correctly;
- failed-authority traffic is not presented as SLO-compliant steady-state capacity.

### 6.5 Capacity-composition evidence

A shared-workstation topology does not establish a database-capacity multiplier. Capacity composition requires independently provisioned database resources, controlled service resources, generator headroom, healthy authorities, and the normal measurement/quotability gates.

## 7. Risks and explicit limitations

### 7.1 One large organisation remains one database authority

Phase 1 scales across organisation homes, not inside one organisation. A sufficiently large organisation can still reach its assigned database authority's frontier.

Subdividing one organisation by full `UserRef`, slot groups, or another key would reopen cross-database coordination and rebalancing questions.

### 7.2 Placement skew

Equal organisation counts do not imply equal load. Placement must account for heavy organisations, and every result records the organisation-to-database-authority distribution it measured.

### 7.3 Routing split-brain

Different service or generator instances using different placement maps could route one organisation inconsistently and divide its source of truth.

The routing version and placement content are therefore immutable for a run, validated at startup/preflight, and recorded in operational metadata and evidence artifacts.

### 7.4 Rebalancing is deferred

Moving an organisation between database authorities requires transferring slot-axis state, user-axis state, reservations, bookings, claims, idempotency history, and lifecycle state while preventing concurrent writes on both sides. Phase 1 uses fixed placement.

### 7.5 Global reads become fan-out work

The booking path stays local to one database authority in Phase 1. Future cross-organisation administration, analytics, or search may require fan-out reads or a separate read model. Such read infrastructure must not become a hidden global write authority.

### 7.6 Caller-asserted routing identity

The current API accepts user organisation from the request because authentication is not yet implemented. Placement enforcement and reservation ownership make behaviour deterministic, but they do not establish a production trust boundary.

### 7.7 Authority failure and ambiguity

An outage is intentionally bounded to the organisations placed on that database authority. `unknown_replayable` remains an unresolved commit fact rather than a definite terminal mutation result; reconciliation must preserve that distinction until same-key replay resolves it.

### 7.8 Resource confounding

Adding a PostgreSQL writer and a service process together changes more than one resource. Capacity experiments therefore hold service compute comparable or report the service-to-database resource ratio explicitly.

## 8. Settled architectural decisions

For Phase 1:

1. Alloca has **three logical authorities** over **two independent ownership axes**.
2. The slot-capacity logical authority belongs to the slot axis keyed by `SlotRef`.
3. User-schedule serialization and claim validity belong to the user axis keyed by `UserRef`; they remain colocated in one database authority so the local reserve protocol can order claim creation and enforce validity without a distributed user-schedule protocol.
4. A **database authority** is an independently writable PostgreSQL transaction domain, not a service replica or a logical authority.
5. Organisation-home placement maps both ownership axes to database authorities at organisation granularity.
6. Same-database-authority booking is defined by placement equality, not organisation-ID equality.
7. User-home owns mutation routing, the complete user axis, and client idempotency.
8. `cross_authority_unsupported` is a replayable `409 business_refusal` recorded on user-home without contacting slot-home.
9. Confirm and cancel require the reservation's `UserRef` to match the command's `UserRef`; mismatch is `404 unknown_target`.
10. Database-authority failure never triggers write fallback.
11. Supported-workload measurements and cross-database refusal controls are separate evidence classes.
12. Reconciliation checks local invariants per database authority and compares aggregate persisted/server totals with global client totals after quiescence.

Implementation mechanics may change without reopening this design provided those contracts remain true.

## 9. Current implementation mapping

The Phase 1 implementation is split across:

- PR3a: versioned organisation placement, shard-affine enforcement, same-/cross-database booking policy, user-home routing contract, and confirm/cancel ownership;
- PR3b: multi-database-authority container topology, placement-aware generator, topology certification, deployed-artifact provenance, and aggregate verifier;
- PR3c: final correctness, refusal, replay-resolution, and failure-isolation evidence.

The formal architecture in this document survives those PR boundaries. The PR scope notes own implementation staging; this document owns the model they must preserve.

## 10. Explicit non-goals

Phase 1 does not:

- support booking when user and slot ownership axes resolve to different writable database authorities;
- choose or implement two-phase commit, saga, or another distributed transaction protocol;
- split the user identity row and its schedule claims across database authorities;
- split one organisation across writable database authorities;
- build a production routing service or dynamic shard catalogue;
- solve online rebalancing or dual-write migration;
- add a shared global workflow database;
- add a new generic unavailable outcome for database-authority failure;
- claim a shared-workstation throughput multiplier;
- make one hot slot or one hot identity parallel;
- require AWS or Kubernetes before correctness evidence exists;
- weaken the user schedule invariant for any booking pattern Phase 1 claims to support.

Phase 2 remains possible because Phase 1 preserves distinct slot and user identities, two explicit ownership axes, stable organisation placement, the complete user axis on user-home, opaque logical identifiers, the full same-database-authority transaction path, and database-authority-aware verification. It is deferred because distributed coordination and recovery require their own correctness model, not because the Phase 1 data model has been simplified until cross-database booking is impossible.
