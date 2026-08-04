# Horizontal database authority

**Status:** Proposed for AG-Sept PR3 review.  
**Scope:** frame horizontal database scaling as part of Alloca's end-to-end scalability story, state the correctness constraints it must preserve, and propose a leading partitioning model for review before PR3 implementation scope is committed.  
**Not yet normative:** this note does not replace [`transaction-semantics.md`](../design/transaction-semantics.md), select a coordination protocol, or authorise an implementation.

## 1. Why this note exists

AG-Sept originally planned to measure one service instance and then add stateless service replicas against one PostgreSQL authority. PR2 changed the order of that investigation.

The single-instance frontier reaches approximately 4,300 dispersed booking requests per second on the measured workstation while `alloca-go` retains substantial application-compute headroom. The connection pool stops binding before throughput stops rising; PostgreSQL is the shared limiting subsystem. The exact database-side mechanism remains provisional, but the architectural result does not: service replicas can raise throughput while aggregate database concurrency is below that frontier, but they cannot move the saturated ceiling of one writable authority.

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

Local containers with explicit CPU and memory limits can establish relative resource balance and logical scale-out on the workstation. They do not establish a production capacity number because the containers still share the host, WSL2, and storage path. A later controlled environment may be needed for external capacity claims, but it is not required to answer the authority-design question first.

## 3. Correctness constraints do not change

Any horizontally scaled design must preserve the invariants already established by AG-M1:

- active reservations and bookings never exceed one slot's capacity;
- one logical mutation is recorded per scoped idempotency key;
- ambiguous outcomes remain replay-safe;
- reserve, confirm, cancel, expiry, and settlement races have valid committed outcomes;
- one user cannot hold overlapping active booking claims, including bookings for slots owned by different organisations;
- authoritative time and bounded timeout classification remain explicit;
- every measured run reconciles outcomes with persisted state.

Scaling by weakening global schedule non-overlap is out of scope. A user cannot attend two overlapping classes merely because the classes are owned by different organisations.

The current user identity remains:

```text
UserRef = (user_organisation_id, user_id)
```

The compound key avoids requiring `user_id` to be globally unique. For one stable `UserRef`, schedule protection already spans every slot-owning organisation into which that user books. This note does not introduce a separate global `user_id` or attempt to merge distinct `UserRef` values that external identity systems may associate with the same real-world person.

## 4. The two natural partition dimensions

The current three database mechanisms implement two business authorities.

### 4.1 Slot-capacity authority

The slot row serialises capacity decisions for:

```text
(slot_organisation_id, slot_id)
```

Its natural horizontal partition key is `slot_organisation_id`. Slots, slot-side lifecycle state, and capacity-changing work for one organisation can remain local to one writable PostgreSQL authority.

### 4.2 User-schedule authority

The `user_identities` row serialises schedule mutation for one `UserRef`; the `user_time_claims` exclusion relation proves global non-overlap for that user. These two relations are one logical authority and should remain colocated.

Its natural horizontal routing key is the stable `UserRef`. A physical router may group many identities onto one shard, for example by hashing the compound key. The required property is that every claim for one user reaches the same schedule authority; there must not be one central schedule database that merely becomes the next global writer.

### 4.3 Why simple organisation sharding is insufficient

A cross-organisation booking can have:

```text
slot_organisation_id != user_organisation_id
```

Sharding the entire existing schema by slot organisation makes slot capacity local but distributes one user's global schedule claims. Sharding the entire schema by user organisation makes schedule claims local but sends capacity work to the slot-owning organisation. Neither key contains the whole transaction without changing the invariant.

This is the constraint already deferred by [`user-schedule-non-overlap.md`](user-schedule-non-overlap.md): one booking may span the slot's organisation authority and the identity's schedule authority.

## 5. Leading partitioning hypothesis

The leading model separates the two authority families and scales each horizontally:

```text
                         reservation coordination
                                  │
                   ┌──────────────┴──────────────┐
                   │                             │
                   ▼                             ▼
       slot-capacity shards             user-schedule shards
       by slot_organisation_id           by stable UserRef

       slots                             user_identities
       slot-side reservations            user_time_claims
       bookings                          schedule-side workflow state
       slot-side workflow state
```

Each shard remains an ordinary writable PostgreSQL authority. Local correctness mechanisms remain familiar:

- slot row locks and capacity checks on slot shards;
- identity row locks and the GiST exclusion constraint on schedule shards;
- local transactions, PostgreSQL time, SQLSTATE classification, and idempotent commands inside each authority.

Aggregate write throughput can then rise across independent organisations and independent user schedules because unrelated work no longer shares one database process set, buffer pool, WAL stream, and storage path.

This does not make one hot authority parallel. One slot's capacity and one user's schedule still serialise by design.

## 6. The coordination problem

A reserve now needs agreement from two writable authorities:

1. the user-schedule authority must grant a non-overlapping claim;
2. the slot-capacity authority must grant capacity.

The current implementation obtains both inside one PostgreSQL transaction. Physical partitioning removes that local atomic boundary. A correct design must specify what happens when one side succeeds and the other refuses, times out, becomes unreachable, or commits while its acknowledgement is lost.

Two broad coordination families remain open:

### 6.1 Distributed atomic commit

A transaction manager or distributed PostgreSQL system coordinates both authorities and presents one atomic commit decision.

This preserves semantics closest to the current implementation, but introduces distributed commit availability, recovery of unresolved transactions, locks held across failures, cross-shard deadlocks, and product-specific compatibility constraints. It must not be selected merely because it hides routing behind one SQL endpoint.

### 6.2 Explicit leased, idempotent reservation protocol

Each authority commits a local, idempotent step under a shared reservation identifier. Temporary claims and holds are leased; a durable coordinator retries completion or compensation, and reconciliation repairs partial states.

A likely safety-first reserve ordering is schedule claim before slot hold: an orphaned temporary schedule claim reduces availability until its lease expires, while an externally usable slot hold without schedule protection could violate global non-overlap. Confirmation and cancellation require their own explicit convergence rules; the protocol cannot be assumed correct from the reserve happy path alone.

This family fits mechanisms Alloca already uses—holds, expiry, idempotency, replay, settlement, and classified ambiguous outcomes—but it changes the correctness model from one local commit to one distributed protocol. It therefore needs a failure-state design and discriminating tests before implementation.

This note treats the leased protocol as the leading hypothesis, not a decided design.

## 7. Questions PR3 must answer before implementation scope

The next review should answer or narrow these questions:

1. Is the two-authority partition above the right logical decomposition, or is there a better placement that preserves both invariants?
2. Should schedule shards route by a hash of full `UserRef`, by `user_organisation_id`, or another stable mapping? What rebalancing property is required now?
3. Which relations and idempotency/workflow records belong on each side?
4. Is distributed atomic commit or an explicit leased protocol the more credible fit for Alloca's guarantees and time budget?
5. What are the reserve, confirm, cancel, expiry, and recovery states under partial failure?
6. At what point is a result externally visible as reserved or confirmed?
7. Which authority owns the final workflow decision and reconciliation?
8. What is the smallest two-database feasibility test that could reject the candidate design quickly?
9. What evidence is actually needed for that decision, without producing another oversized dashboard and artifact surface?

## 8. Immediate next step

Review this design note first. Do not yet freeze a complete PR3 implementation matrix or rewrite the remaining AG-Sept PR scopes.

After review, create a bounded PR3 scope for the smallest decisive feasibility spike. Its purpose should be to test the hardest coordination and invariant edge, not to productionise sharding, Kubernetes, AWS, routing, rebalancing, observability, and performance measurement at once.

## 9. Explicit non-goals of this draft

This draft does not:

- choose a distributed database product;
- implement shard routing or a transaction coordinator;
- define production shard counts or resource ratios;
- promise that horizontal database scaling fits wholly inside AG-Sept;
- weaken global user schedule non-overlap;
- reopen the accepted single-database transaction semantics before a replacement protocol is proven;
- add PostgreSQL exporters, Grafana panels, a large load matrix, AWS, or Kubernetes;
- predict the remaining PR sequence before this decision is reviewed.

The design is intentionally small because PR2 demonstrated that the evidence may change the order of the work. PR3 should proceed one decision at a time.