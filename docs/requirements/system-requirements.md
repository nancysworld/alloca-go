# Alloca-Go system requirements

**Status:** Living  
**Scope:** cross-cutting correctness, scalability, failure-containment, deployment, and evidence
requirements that should survive milestone replanning and replaceable implementation choices.

This document states **what must be true**. The linked design documents own how Alloca-Go
satisfies each requirement. Detailed transactional correctness remains defined by the `INV-*`
register in [`../design/transaction-semantics.md`](../design/transaction-semantics.md); it is not
duplicated here.

## 1. Problem

Alloca-Go must allocate scarce resources correctly under contention while allowing independent
work to benefit from additional compute and writable-state resources. Scaling, deployment, or
failure handling must not trade away the transactional guarantees already established for the
booking domain.

The project must also be able to distinguish a real system frontier from generator,
observability, connection-budget, or shared-environment limits, and must retain enough evidence
to support every quantitative claim it makes.

These are system problems rather than one milestone's schedule. AG-Sept currently exercises
several of them, but replanning AG-Sept must not change their meaning.

## 2. Correctness requirements

### REQ-COR-1 — Supported operations preserve transactional correctness

Every supported booking operation must preserve the accepted transactional invariant set,
state-machine semantics, idempotency behaviour, authoritative-time rules, bounded timeout
classification, and user-schedule guarantees.

Scaling topology, replica count, deployment mechanism, and fault handling must not weaken those
guarantees to gain throughput or simplify operations.

**Normative detail:** [`../design/transaction-semantics.md`](../design/transaction-semantics.md).

### REQ-COR-2 — Ambiguous mutations remain safely resolvable

When the service cannot know whether a mutation committed, the result must remain conservative
and replay-safe. The same logical mutation is resolved through the same idempotency identity;
an uncertain result must not be converted into an unconstrained second mutation.

**Normative detail:** `transaction-semantics.md` and
[`../design/measurement-contract.md`](../design/measurement-contract.md).

## 3. Scalability requirements

### REQ-SCALE-1 — Independent work can use independent resources

Where correctness does not require work to share one logical authority, the architecture must
allow independent work to benefit from additional service-compute and writable-database
resources.

The system must distinguish those scaling dimensions rather than treating "horizontal scaling"
as one undifferentiated mechanism or number.

**Design:** [`../design/horizontal-scaling.md`](../design/horizontal-scaling.md).

### REQ-SCALE-2 — Correctness does not depend on service-replica locality

Adding, removing, restarting, or load-balancing among compatible stateless service replicas must
not alter the correctness model. Authoritative mutation state and serialization must remain in
the appropriate durable authority rather than in replica-local memory.

**Design:** `horizontal-scaling.md`.

### REQ-SCALE-3 — Serial correctness authorities remain explicit

Adding compute or writable authorities must not imply that one indivisible hot slot, one user's
schedule, or another logically serial correctness decision has become parallel. Scaling claims
must identify where serialization is intrinsic to the accepted correctness model.

**Design:** `horizontal-scaling.md` and
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md).

## 4. Routing and failure requirements

### REQ-ROUTE-1 — Authoritative placement is enforced

Requests must be handled according to one explicit, version-compatible ownership/placement model.
A routing mistake must be detected rather than silently creating authoritative state in the
wrong writable domain.

**Design:** `horizontal-database-authority.md` and `horizontal-scaling.md`.

### REQ-FAIL-1 — Failure remains contained to the owning dependency boundary

Failure or saturation of one independently writable authority must not preserve apparent
availability by moving authoritative writes to another writer. Unrelated authorities whose own
dependencies remain healthy should be able to continue independently.

**Design:** `horizontal-database-authority.md` and `horizontal-scaling.md`.

## 5. Deployment requirements

### REQ-DEPLOY-1 — A deployment is operable as an identifiable system

A supported deployment must make the serving topology and its durable dependencies explicit,
provide truthful liveness/readiness and bounded shutdown behaviour, keep schema evolution under
an explicit lifecycle, and retain enough provenance to identify the code and deployed artifact
that produced measured evidence.

The exact container image layout, orchestrator, service names, ports, and startup scripts are
replaceable implementation choices unless they change those properties.

**Design:** [`../design/deployment-architecture.md`](../design/deployment-architecture.md).  
**Artifact decision:**
[`../decisions/0003-deployed-artifact-identity.md`](../decisions/0003-deployed-artifact-identity.md).

## 6. Evidence requirements

### REQ-EVID-1 — Quantitative claims are admissible and reproducible

Performance, capacity, scale-efficiency, latency, and failure-behaviour claims must not be
promoted beyond the evidence retained for them. Applicable runs must be reproducible,
provenance-carrying, correctly classified, and reconciled according to the measurement contract.

**Normative detail:**
[`../design/measurement-contract.md`](../design/measurement-contract.md).

### REQ-EVID-2 — Different limiting mechanisms remain different claims

Evidence must distinguish service-compute scaling, writable-database-authority composition, hot
logical-authority serialization, connection/admission effects, generator limits, telemetry cost,
and shared-environment contention where those distinctions affect interpretation.

A single headline multiplier must not collapse incomparable mechanisms into one system claim.

**Design:** `horizontal-scaling.md`.  
**Evidence definitions:** `measurement-contract.md`.
