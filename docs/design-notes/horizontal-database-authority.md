# Horizontal database authority

**Status:** Proposed for AG-Sept PR3 review.  
**Scope:** frame horizontal database scaling as part of Alloca's end-to-end scalability story, state the correctness constraints it must preserve, and propose a leading partitioning model for review before PR3 implementation scope is committed.  
**Not yet normative:** this note does not replace [`transaction-semantics.md`](../design/transaction-semantics.md), select a coordination protocol, or authorise a production implementation.

## 1. Why this note exists

AG-Sept originally planned to measure one service instance and then add stateless service replicas against one PostgreSQL authority. PR2 changed the order of that investigation.

The single-instance frontier reaches approximately 4,300 dispersed booking requests per second on the measured workstation while `alloca-go` retains substantial application-compute headroom. Increasing the connection pool from 40 to 80 removes acquire wait and lifts throughput to the plateau; the pool stops binding and throughput still does not rise further. PostgreSQL is therefore the shared limiting subsystem in the measured topology. The exact database-side mechanism remains provisional, but the architectural result does not: service replicas can raise throughput while aggregate database concurrency is below that frontier, but they cannot move the saturated ceiling of one writable authority.

This does not mean the measured number is abnormal or that PostgreSQL is the wrong database. It means one PostgreSQL writer is a finite global boundary. If Alloca claims horizontal scalability for the whole system, it needs a route for scaling writable database authority as well as stateless service compute.

Database efficiency work remains valuable: remove demonstrated pathologies, avoid unnecessary writes, choose sensible storage and configuration, and establish an honest per-authority baseline. But optimisation cannot remove the structural ceiling of one writer. Horizontal authority scaling is therefore part of the scalability design, not an optional optimisation deferred until every single-node improvement has been exhausted.

## 2. What scalability means here

Alloca scalability is not one maximum-throughput number. It is the behaviour of several resource and authority dimensions:

- service compute can be increased with stateless replicas;
- writable database capacity can be increased by adding independent authorities;
- the service-to-database resource ratio can be measured and provisioned rather than assumed;
- independent slots, organisations, and user schedules should progress concurrently;
- one hot slot and one user's schedule remain deliberately serialised, because those are correctness authorities rather than accidental global locks.

A useful experiment may therefore report a topology such as:

```text
N service replicas : M writable PostgreSQL authorities
```

and state which workload and authority distribution that ratio supports. A dispersed workload and a one-hot-slot workload do not share one meaningful ratio or one scale-efficiency claim.

Local containers with explicit CPU and memory limits can establish logical authority separation and exercise cross-authority correctness. They cannot establish a production capacity number, and a local one-authority versus two-authority throughput comparison would be uninterpretable: the service, generator, telemetry stack, PostgreSQL instances, WSL2, and storage path still share one workstation allocation, while PR2's approximately 2x capacity excursion remains unexplained.

The first PR3 spike must therefore be **correctness-first**. Its purpose is to test routing, partial-failure semantics, and invariant reconciliation, not to claim that two local databases provide a measured throughput multiplier.

## 3. Correctness constraints do not change

Any horizontally scaled design must preserve the invariants already established by AG-M1:

- active reservations and bookings never exceed one slot's capacity;
- one logical mutation is recorded per scoped idempotency key;
- ambiguous outcomes remain replay-safe;
- reserve, confirm, cancel, expiry, and settlement races have valid committed outcomes;
- one user cannot hold overlapping active booking claims, including bookings for slots owned by different organisations;
- authoritative time and bounded timeout classification remain explicit;
- every admissible experiment reconciles outcomes with persisted state across every participating authority.

Scaling by weakening global schedule non-overlap is out of scope. A user cannot attend two overlapping classes merely because the classes are owned by different organisations.

The current user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. For one stable `UserRef`, schedule protection already spans every slot-owning organisation into which that user books. This note does not introduce a separate global `user_id` or attempt to merge distinct `UserRef` values that external identity systems may associate with the same real-world person.

## 4. Same-shard and cross-shard bookings

The current transaction contains two business authorities:

- slot capacity, identified by `(slot_organisation_id, slot_id)`;
- the global schedule for one stable `UserRef`.

Those authorities do not require distributed coordination for every booking. The important distinction is whether they route to the same writable shard.

### 4.1 Organisation-home authority

The leading placement hypothesis gives each organisation a writable home authority containing:

- slots and slot-side lifecycle state owned by that organisation;
- user identities and global schedule claims for users registered with that organisation;
- local idempotency and workflow records needed to preserve the current transaction semantics.

The slot side routes by `slot_organisation_id`. The user-schedule side routes by the stable home of `UserRef`, initially `user_organisation_id`.

This gives two execution paths.

**Same-shard booking**

```text
slot_organisation_id == user_organisation_id
```

The slot-capacity and user-schedule authorities are colocated. Reserve, confirm, and cancel can retain the current single-PostgreSQL transaction and existing correctness mechanisms.

**Cross-shard booking**

```text
slot_organisation_id != user_organisation_id
```

The user's schedule remains authoritative on the user's home shard while slot capacity remains authoritative on the slot-owning organisation's shard. The mutation therefore needs explicit coordination between two writable authorities.

This placement keeps the common local case simple without weakening the global schedule invariant. A user's schedule authority still sees every booking claim for that `UserRef`, including claims for slots owned by other organisations.

### 4.2 Workload assumption and generator gap

The repository's own representative workload gives the shape. In the fitness-club release, a member is registered with one club and usually books that club's sessions, which maps to the same-shard path. A member booking a session at a different club — a partner site, a network-wide class, a guest booking — maps to the cross-shard path.

For PR3 planning, use **approximately 10% cross-shard bookings** as an explicit scenario assumption. It is not measured Alloca evidence and is not claimed as a universal production rate. The load generator must make the rate configurable so the design can be exercised at least at:

```text
0%   cross-shard: local-path control
10%  cross-shard: working scenario assumption
100% cross-shard: coordination stress case
```

Every PR2 workload used one organisation value for both the slot and user sides, so all 2,350,742 measured requests were same-shard. PR2 therefore provides no evidence about cross-shard correctness, cost, or frequency.

The assumed rate affects architecture rather than merely tuning it. At approximately 10%, the current local transaction can remain the dominant path while the slower, explicitly compensated protocol is isolated to the minority cross-shard path. If a target domain later shows that most bookings are cross-shard, the coordination protocol becomes the critical path and the placement decision must be revisited.

### 4.3 Scaling and future subdivision

Organisation-home shards remove one global writer across independent organisations while preserving one authoritative schedule location per user. Unrelated organisations can progress on different PostgreSQL authorities; one hot slot and one user's schedule remain serialised by design.

Routing all users of one organisation to one home shard is the smallest hypothesis for PR3, not a claim that `user_organisation_id` is the final partition granularity. A very large organisation may later require a stable mapping that subdivides users by full `UserRef`. That rebalancing problem should not be introduced before the two-database correctness spike establishes whether the local/cross-shard split is viable.

## 5. Leading partitioning hypothesis

The leading topology is a set of organisation-home PostgreSQL authorities:

```text
                 same-shard booking
        ┌────────────────────────────────┐
        │ one local PostgreSQL transaction│
        ▼                                │
┌──────────────────────┐       ┌──────────────────────┐
│ organisation A shard │       │ organisation B shard │
│                      │       │                      │
│ A-owned slots        │       │ B-owned slots        │
│ A users' schedules   │       │ B users' schedules   │
│ local workflow state │       │ local workflow state │
└──────────┬───────────┘       └──────────┬───────────┘
           │                              │
           └──── cross-shard coordination┘
             A user booking a B slot
```

Each shard remains an ordinary writable PostgreSQL authority. Local correctness mechanisms remain familiar:

- slot row locks and capacity checks for locally owned slots;
- identity row locks and the GiST exclusion constraint for locally homed users;
- local transactions, PostgreSQL time, SQLSTATE classification, and idempotent commands inside each authority.

Aggregate write throughput can rise across independent organisation-home shards because unrelated work no longer shares one database process set, buffer pool, WAL stream, and storage path.

This does not make one hot authority parallel. One slot's capacity and one user's schedule still serialise by design.

## 6. The cross-shard coordination problem

A same-shard reserve retains the current local atomic boundary. A cross-shard reserve needs agreement from two writable authorities:

1. the user-home authority must grant a non-overlapping schedule claim;
2. the slot-owning authority must grant capacity.

Physical separation removes the single-transaction boundary for that path. A correct protocol must specify what happens when one side succeeds and the other refuses, times out, becomes unreachable, or commits while its acknowledgement is lost.

Two broad coordination families remain open.

### 6.1 Distributed atomic commit

A transaction manager or distributed PostgreSQL system coordinates both authorities and presents one atomic commit decision.

This preserves semantics closest to the current implementation, but introduces distributed commit availability, recovery of unresolved transactions, locks held across failures, cross-shard deadlocks, and product-specific compatibility constraints. It must not be selected merely because it hides routing behind one SQL endpoint.

### 6.2 Explicit leased, idempotent reservation protocol

Each authority commits a local, idempotent step under a shared reservation identifier. Temporary schedule claims and slot holds are leased; a durable workflow record retries completion or compensation, and reconciliation detects partial states.

A likely safety-first reserve ordering is a leased schedule claim before a leased slot hold: an orphaned temporary schedule claim reduces availability until expiry, while an externally usable slot hold without schedule protection could violate global non-overlap. That ordering is only a starting hypothesis; confirm, cancel, expiry, lost acknowledgement, and recovery states must be written explicitly.

Lease expiry does **not** repair every failure. In the current model, confirming a schedule claim sets `expires_at = NULL`; the confirmed claim is intentionally permanent and is not reaped by `SettleClaims`. If the schedule side becomes confirmed while the slot side fails permanently, recovery requires an explicit, durable, idempotent cross-authority cancellation or deletion. Until that compensation completes, the orphaned claim can block the user from overlapping bookings. The protocol therefore needs:

- durable workflow ownership for every cross-shard reservation;
- idempotent forward and compensating commands on both authorities;
- explicit handling for permanent confirmed claims, not only leased pending claims;
- a recovery worker that does not depend on the user's next reserve;
- cross-authority reconciliation capable of detecting and repairing partial states.

A result must not become externally visible as confirmed merely because one authority has reached its local confirmed state. The externally visible decision and the convergence rule remain open design questions.

This family fits mechanisms Alloca already uses—holds, expiry, idempotency, replay, settlement, and classified ambiguous outcomes—but it changes the correctness model from one local commit to one distributed protocol. It therefore needs a failure-state design and discriminating tests before implementation.

This note treats the leased protocol as the leading hypothesis for the minority cross-shard path, not a decided design.

## 7. Smallest decisive feasibility spike

The first spike should use:

- two PostgreSQL instances: one user-home authority and one different slot-owning authority;
- one service process with two hard-coded DSNs and no routing tier;
- a generator with a configurable cross-shard rate, including the 0%, 10%, and 100% cases;
- one durable reservation identifier and the minimum workflow state needed to exercise replay and compensation;
- a verifier that connects to both databases and reconciles capacity, schedule non-overlap, and partial workflow state.

The decisive test is correctness under injected partial failure:

1. commit the schedule-side step;
2. make the slot authority unavailable before its corresponding commit or acknowledgement;
3. replay and recover the logical reservation;
4. reconcile both databases;
5. prove global user non-overlap, slot-capacity safety, replay safety, and eventual removal of any invalid orphaned state.

The spike should also inject the inverse partial state if the chosen protocol can produce it. It must not report a local two-database throughput multiplier: the shared workstation makes that comparison uninterpretable, and throughput is not the decision being tested.

If the invariants survive the failure matrix with a credible recovery mechanism, the leased family is worth designing further. If they do not, reject it before spending the milestone on routing, rebalancing, deployment, or observability.

## 8. Questions PR3 must answer before implementation scope

The next review should answer or narrow these questions:

1. Is organisation-home placement the right first decomposition for preserving the local path while isolating cross-shard coordination?
2. Is approximately 10% cross-shard traffic a useful working scenario, and which additional rates are necessary to prevent the design from depending on it?
3. Which idempotency and workflow records belong on each side, and which authority owns the durable coordination decision?
4. Is distributed atomic commit or an explicit leased protocol the more credible fit for Alloca's guarantees and time budget?
5. What are the reserve, confirm, cancel, expiry, compensation, and recovery states under every partial failure?
6. At what point is a result externally visible as reserved or confirmed?
7. How are permanent confirmed schedule claims compensated when the slot-side outcome cannot be completed?
8. What cross-authority verifier queries and assertions gate every admissible experiment?
9. Does the two-database failure spike above discriminate between the coordination families strongly enough?
10. What evidence is actually needed for that decision, without producing another oversized dashboard and artifact surface?

## 9. Immediate next step

Review this revised design note before freezing a complete PR3 implementation matrix or rewriting the remaining AG-Sept PR scopes.

After review, create a bounded PR3 scope for the correctness-first two-database feasibility spike. Its purpose should be to test the hardest coordination and invariant edge, not to productionise sharding, Kubernetes, AWS, routing, rebalancing, observability, and performance measurement at once.

## 10. Explicit non-goals of this draft

This draft does not:

- choose a distributed database product;
- implement a general shard router or production transaction coordinator;
- define production shard counts, cross-shard rates, or resource ratios;
- claim a throughput gain from two PostgreSQL containers on one workstation;
- promise that horizontal database scaling fits wholly inside AG-Sept;
- weaken global user schedule non-overlap;
- reopen the accepted same-shard transaction semantics before a replacement cross-shard protocol is proven;
- add PostgreSQL exporters, Grafana panels, a large load matrix, AWS, or Kubernetes;
- solve user-shard subdivision or rebalancing before the two-database correctness hypothesis is tested;
- predict the remaining PR sequence before this decision is reviewed.

The design is intentionally small because PR2 demonstrated that evidence may change the order of the work. PR3 should proceed one decision at a time.
