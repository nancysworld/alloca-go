# Deployment architecture

**Status:** Accepted — normative deployment design for the current Alloca-Go system.  
**Scope:** durable properties a deployment must preserve regardless of whether the current
mechanism is Docker Compose, a later orchestrator, or another production-shaped runtime.  
**Operational procedure:** [`../operations/container-topology.md`](../operations/container-topology.md).  
**Artifact identity decision:**
[`../decisions/0003-deployed-artifact-identity.md`](../decisions/0003-deployed-artifact-identity.md).

## 1. Why this document exists

`container-topology.md` correctly owns **how to build and run** the current local topology. The
Dockerfile and Compose configuration own their concrete implementation mechanics. Neither should
be the only source for the durable deployment properties the system depends on.

This document owns that missing layer: what a deployment unit is, how serving processes bind to
database authorities, how migration and serving lifecycles are separated, what readiness means,
what must be identifiable for evidence, and which properties any future orchestration mechanism
must preserve.

Containers are the current packaging mechanism. The architecture is intentionally stated so
replacing Compose does not require rediscovering these rules.

## 2. Deployment units

The current system has three authoritative deployment roles:

1. **service replica** — runs the Alloca-Go HTTP service for exactly one database authority;
2. **schema migration job** — applies the repository's ordered database migrations to one
   database authority and exits;
3. **PostgreSQL database authority** — the independently writable transaction domain that owns
   the organisations placed on it.

Metrics storage, dashboards, load generators, exporters, and verifiers are supporting
experiment/operations components. They do not become authoritative for booking correctness
merely because they share a deployment topology.

A packaging artifact may contain more than one executable role. Packaging them together does not
collapse lifecycle responsibilities: a migration invocation remains distinct from a serving
invocation.

## 3. Shard-affine serving

Each service replica is bound to one database authority and the organisations assigned to that
authority by the active placement version.

A serving replica therefore has:

- one database-authority identity;
- one database connection pool to that authority;
- one placement/routing version compatible with that assignment;
- readiness dependent on that authority;
- no automatic fallback writer.

Several replicas may share the same authority and form one shard group as defined by
[`horizontal-scaling.md`](horizontal-scaling.md).

A service that opens pools to every database authority would be a different deployment
architecture: it changes connection fan-out, readiness dependencies, failure blast radius, and
routing responsibilities. It must not arrive as an incidental implementation convenience.

## 4. Placement configuration is deployment input

Organisation placement is versioned configuration supplied to the deployment, not private
mutable state invented independently by each service process.

For one run/deployment:

- every participating service unit uses the same routing/placement version;
- each service unit receives the authority binding and organisation set it is expected to
  enforce;
- load generation and verification use the same logical placement;
- placement is immutable for the duration of a measured run;
- conflicting or incomplete assignments fail validation rather than being resolved by
  first-match behaviour.

Dynamic online placement changes and rebalancing are separate future architecture problems.

## 5. Schema migration is separate from serving

Serving replicas do not race to migrate their database at startup.

For each database authority:

1. the migration action runs against that authority;
2. migration success is established explicitly;
3. serving replicas start or become eligible for readiness only against a compatible schema.

The exact orchestrator dependency mechanism is replaceable. The durable property is that schema
ownership is explicit and the serving fleet does not turn startup concurrency into migration
concurrency.

This preserves ADR-0002's separation between PostgreSQL as transactional authority and
application serving processes.

## 6. Liveness and readiness

Deployment health has at least two meanings and must not collapse them:

- **liveness** — the service process is running and can answer its process-level health endpoint;
- **readiness** — the replica can serve requests for its assigned authority under the accepted
  deployment contract.

Readiness therefore includes the dependency that matters for this shard-affine architecture: the
replica's database authority must be reachable and schema-compatible according to the service's
readiness contract.

A replica whose authority is unavailable may remain live while becoming unready. Another
authority's healthy replica must not cause it to claim readiness through fallback.

The HTTP shapes and status semantics remain owned by [`api-surface.md`](api-surface.md).

## 7. Graceful termination

The deployment must allow the service to stop accepting new work and honour the application's
graceful-shutdown sequence rather than relying on abrupt process destruction as the ordinary
termination path.

Graceful termination does not change transaction semantics: transactions that have committed
remain durable; ambiguous commit acknowledgements remain resolved through the idempotency
protocol; shutdown does not invent a second compensation mechanism.

An orchestrator may impose a termination grace period, but that period must be compatible with
the application's own request/shutdown bounds rather than silently cutting them shorter.

## 8. Configuration boundary

Runtime environment-specific values are deployment configuration rather than source edits. At
minimum the deployment must be able to supply values that differ by service unit or environment,
including:

- database connection information;
- database-authority identity;
- placement/routing configuration;
- ports and operational endpoints where applicable;
- request/database timeout configuration;
- telemetry mode and bounded sink configuration where applicable.

Secrets and private endpoints are not committed merely to make a topology reproducible.
Reproducibility means the configuration *shape* and non-secret values needed to understand the
run are retained, while secret material is supplied through the environment's secret mechanism.

## 9. Artifact identity and provenance

A deployment used for measured evidence must distinguish **code identity** from **deployed
artifact identity**.

- the source revision identifies the code represented by the binary;
- a container image ID or immutable registry digest identifies the container artifact that
  actually served the run;
- a mutable tag is an alias and is insufficient as the sole artifact identity.

The deployed artifact identity is observed from the deployment substrate rather than trusted as
a value self-reported by the process inside the artifact. ADR-0003 owns the decision and
rejected alternatives; `measurement-contract.md` owns when a run must record it.

All service units participating in one comparable topology must expose or retain enough metadata
to detect incompatible code or schema rather than letting a mixed deployment look like one
homogeneous service.

## 10. Runtime artifact properties

The serving runtime should be production-shaped: it requires only the runtime assets needed to
execute the service role and should not depend on development tooling being present in order to
serve traffic.

That principle does not require one specific base image or image layout. The Dockerfile is
authoritative for the current mechanism. Changing base image, build stages, or packaging is
implementation work unless it changes provenance, security/trust assumptions, runtime
dependencies, or another contract in this document.

## 11. Orchestration requirements

Any orchestration mechanism used for the current Phase 1 architecture must preserve these
properties:

1. independently writable database authorities remain distinct;
2. migration is executed per authority before serving claims readiness;
3. each service replica is bound to exactly one authority;
4. authority placement is explicit and version-compatible;
5. service replicas may be multiplied within a shard group without changing correctness
   ownership;
6. loss of one database authority does not redirect its organisations to another writer;
7. liveness/readiness and graceful termination remain observable;
8. the deployed artifact and topology can be identified for retained evidence.

Docker Compose is sufficient for the current local evidence because it can preserve those
properties. Kubernetes, EKS, a service mesh, GitOps, or custom operators are not architectural
requirements.

## 12. Local topology versus production-capacity evidence

Running several service units and PostgreSQL authorities as containers on one workstation can
prove:

- packaging and startup dependencies;
- placement/routing enforcement;
- schema compatibility;
- authority separation at the transaction-domain level;
- failure containment;
- artifact/topology provenance;
- functional composition of replica and database-authority axes.

It cannot by itself prove a production capacity multiplier when service, generator, telemetry,
databases, storage path, and host resources contend inside one machine allocation.

That limitation belongs in evidence interpretation, not deployment design. The measurement
contract determines the admissible claim level.

## 13. Implementation and operations boundary

The current implementation is allowed to choose details such as:

- Dockerfile stage layout;
- exact base image;
- Compose service names;
- host port numbers;
- health-poll implementation;
- Makefile targets;
- whether multiple executable roles are packaged in one image;
- local file paths for placement configuration.

Those details belong to code/configuration and
[`../operations/container-topology.md`](../operations/container-topology.md) unless changing them
would alter one of the architectural properties above.

This boundary is deliberate: the deployment design should remain valid when the current
container mechanics are replaced.
