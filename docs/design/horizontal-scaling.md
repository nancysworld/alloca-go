# Horizontal scaling architecture

**Status:** Accepted — normative umbrella for Alloca-Go horizontal scaling.  
**Scope:** define how stateless service replicas and independently writable PostgreSQL database
authorities compose as separate scaling axes, including routing order, shard groups, connection
budgets, failure boundaries, and the limits imposed by hot logical authorities.  
**Detailed database design:**
[`horizontal-database-authority.md`](horizontal-database-authority.md).  
**Transactional semantics:** [`transaction-semantics.md`](transaction-semantics.md).

## 1. Why this document exists

Alloca-Go has two different horizontal scaling mechanisms:

1. add **stateless service replicas** to increase application compute and availability; and
2. add **independently writable database authorities** to partition independent transactional
   state and database resource pressure.

Those mechanisms are related but not interchangeable. A second service process connected to the
same PostgreSQL writer does not create another write authority. A second PostgreSQL writer does
not make a single hot slot or one user's schedule mutations parallel.

The detailed database-authority model is already owned by `horizontal-database-authority.md`.
This document owns the missing whole-system layer: how service-replica scaling composes with
database-authority scaling without blurring their responsibilities or their evidence claims.

## 2. The two-axis model

Describe a topology as:

```text
N service replicas : M writable PostgreSQL database authorities
```

with the workload distribution named alongside it.

`N:M` is not itself a capacity claim. Interpretation depends on where requests land:

- dispersed requests across independent organisations;
- one hot organisation;
- one hot slot;
- one hot user identity;
- one or several replicas sharing one database authority;
- several database authorities sharing one physical host or resource envelope.

A valid scaling conclusion therefore states which axis changed, which shared resource remained
fixed, and which authority or resource became limiting.

## 3. Shard groups and routing order

A **shard group** is the set of stateless service replicas bound to one database authority and
one compatible placement/schema version.

```text
organisation placement
        |
        v
choose database authority / shard group
        |
        v
load-balance within that shard group
        |
   +----+----+
   |    |    |
 replica replica replica
   \    |    /
    \   |   /
 PostgreSQL database authority
```

The order is load-bearing:

1. identify the operation's routing root according to `horizontal-database-authority.md` —
   mutations use `user_organisation_id`, while slot reads use `slot_organisation_id`;
2. resolve that organisation to its database authority;
3. select the shard group that serves that authority;
4. load-balance across eligible replicas inside that group.

A global load balancer that chooses an arbitrary replica first and relies on the service to proxy
or fall through to another database authority would weaken placement enforcement and make failure
boundaries ambiguous. Authority selection therefore precedes replica selection.

Static routing is sufficient for the current evidence. A production routing service, dynamic
placement control plane, or service mesh is not required by this architecture.

## 4. Stateless service-replica semantics

A service replica is **stateless for correctness**:

- authoritative booking, idempotency, and schedule state live in the owning database authority;
- another compatible replica in the same shard group may serve the next request or replay
  without state transfer from the previous replica;
- restarting or removing a replica does not change the transaction protocol;
- replica-local caches, routing helpers, or metrics may exist only as replaceable optimisations
  and must not become the sole authority for a correctness decision.

The service process remains a modular monolith unless an independently deployable boundary is
justified by evidence. Replica scaling means multiple copies of the same accepted application
boundary, not implicit microservice decomposition.

## 5. Database-authority semantics

A database authority is one independently writable PostgreSQL transaction domain, as defined by
`horizontal-database-authority.md`.

Several replicas may share one database authority:

```text
replica 1a --+
replica 1b --+--> database authority 1
replica 1c --+
```

and still provide only one writable transaction domain.

Adding another database authority creates another independently writable domain:

```text
shard group 1 --> database authority 1
shard group 2 --> database authority 2
```

Phase 1 assigns complete organisation homes so every supported mutation remains local to one
database authority. Cross-database-authority booking is a separate coordination problem and
remains governed by `horizontal-database-authority.md`.

## 6. Correctness across replicas

All accepted transactional invariants remain properties of the logical/database authorities,
not of the number of API processes.

In particular:

- one hot slot remains serialized by its slot-capacity logical authority;
- claim-creating mutations for one `UserRef` remain serialized by the user-schedule authority;
- the claim-validity authority still decides schedule overlap;
- idempotency replay resolves at the mutation's stable user-home authority;
- authoritative time remains database-observed according to `transaction-semantics.md`;
- adding replicas must not introduce a second write path that bypasses those authorities.

A hot logical authority therefore has an expected serialization ceiling. More request handlers
cannot parallelise one serial correctness decision; that is a property to measure and explain,
not a reason to weaken the authority.

## 7. Connection budgets are per database authority

Service replicas create database connections. Increasing replica count can therefore change two
things at once:

1. application compute/concurrency; and
2. pressure on the shared database connection/admission boundary.

For one shard group:

```text
aggregate_pool_capacity(authority)
    = sum(pool_size(replica) for replicas serving that authority)
```

With equal pool sizes this becomes:

```text
replicas_in_shard_group x pool_size_per_replica
```

It is **not** `all replicas x all authorities x pool size` because a shard-affine replica opens a
pool to its own authority rather than every database.

Replica scaling experiments must therefore include a connection-budget control when interpreting
service-compute gains: compare a roughly fixed aggregate authority budget with full per-replica
pool multiplication.

The exact pool sizes are configuration/experiment parameters. The durable design rule is that
connection pressure is accounted for per owning database authority.

## 8. Readiness and failure boundaries

A replica's readiness is bound to the database authority it serves. If that authority is
unavailable, the replica is not ready to serve that authority's organisations.

Two failures have different semantics.

### 8.1 Replica failure

If one replica fails while its database authority and another compatible replica in the same
shard group remain healthy, routing may select another replica. This is ordinary
stateless-compute redundancy; the writable authority has not moved.

### 8.2 Database-authority failure

If the owning database authority fails, its organisations remain assigned there. Requests are
**not** redirected to a different writer merely because another shard group is healthy.

Apparent availability obtained by writing an organisation's state on a second authority would
violate ownership and create a distributed-consistency problem rather than solve a routing
problem.

Failure isolation therefore means:

- unrelated authorities continue independently;
- affected organisations fail or time out according to the bounded outcome model;
- restoration resumes against the same authority;
- ambiguous mutations are resolved by idempotent replay, not speculative writes elsewhere.

## 9. Placement, replica identity, and compatibility

Every service unit participating in one deployment must expose enough bounded metadata to
establish:

- database authority identity;
- placement/routing version;
- schema compatibility;
- serving code revision;
- deployed artifact identity where the deployment substrate can provide it.

All replicas in one shard group must agree on the authority and compatible routing/schema
contract they serve. Multi-service measurement runs additionally follow the provenance rules in
`measurement-contract.md`.

Replica identity may be used as a bounded operational/measurement dimension. Organisation
identity remains inappropriate as a general metrics label because its cardinality grows with
tenants.

## 10. Composing the two axes

The intended Phase 1 topology is conceptually:

```text
                    versioned organisation placement
                         /                    \
                        v                      v
                 shard group 1          shard group 2
                /      |      \        /      |      \
              svc     svc     svc     svc     svc     svc
                \      |      /        \      |      /
              PostgreSQL 1             PostgreSQL 2
            database authority       database authority
```

Scale service compute **within** a shard group by changing replica count. Scale writable
database authority by adding another independently writable home and placing independent
organisations on it.

These axes compose only after their claims are understood separately. A composed experiment
must not attribute a throughput change to "horizontal scaling" without identifying whether the
additional useful capacity came from application compute, database-authority partitioning,
altered connection pressure, workload redistribution, or shared-host effects.

## 11. Shared physical resources do not erase logical boundaries

Two PostgreSQL authorities running as containers on one workstation remain separate transaction
domains and can prove placement, correctness, routing enforcement, and failure containment. They
do **not** provide independently provisioned database capacity when they share CPU, memory,
storage, kernel, or host-level contention.

Likewise, service replicas and the load generator sharing one host can prove functional
composition but may not support a production capacity multiplier.

The measurement contract determines how such evidence is labelled. This design requires the
logical topology to be named accurately.

## 12. What this design deliberately does not decide

This document does not require:

- Kubernetes;
- a service mesh;
- dynamic online rebalancing;
- automatic shard movement;
- multi-region writes;
- read replicas;
- a global routing control plane;
- a distributed transaction protocol for cross-database booking;
- a microservice split.

Those may become separate designs when a later engineering iteration establishes the problem,
requirements, and evidence that justify them.

## 13. Validation

The concrete AG-Sept validations for this architecture are owned by
[`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md),
including:

- replica scaling within one database authority;
- the connection-budget control;
- multi-authority supported correctness;
- misrouting and cross-authority refusal controls;
- database-authority failure isolation;
- a composed multi-authority/multi-replica run only if later analysis justifies it.

Measured conclusions remain in `docs/measurements/`, not here.
