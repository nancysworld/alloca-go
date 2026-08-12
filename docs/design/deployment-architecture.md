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

## 12. Local topology versus capacity evidence

Running several service units and PostgreSQL authorities as containers on one workstation can
prove:

- packaging and startup dependencies;
- placement/routing enforcement;
- schema compatibility;
- authority separation at the transaction-domain level;
- failure containment;
- artifact/topology provenance;
- functional composition of replica and database-authority axes.

It cannot prove the Iteration C capacity-composition claim when service, generator, telemetry,
databases, storage path, and host resources contend inside one fixed machine allocation. Adding a
shard group in that environment redistributes resources rather than adding the controlled resource
envelope required by REQ-SCALE-4.

That does not invalidate the local topology evidence retained by Iteration B; it limits the claim
that topology can support.

## 13. Iteration C capacity environment

Iteration C requires three comparable topologies — 1, 2, and 4 shard groups — in which each added
shard group adds an equivalent service-and-database resource envelope and the generator is outside
those envelopes.

The environment choice is derived from that requirement rather than from a desire to add cloud
technology:

1. the existing workstation cannot grow its fixed 10-vCPU/host/storage allocation when another
   shard group is added;
2. the experiment therefore needs externally independent compute envelopes;
3. AG-Sept has an available reproducible mechanism for creating those envelopes: AWS EC2;
4. the smallest design that answers the question is selected rather than restoring the earlier
   EKS/RDS architecture.

**Iteration C therefore uses AWS EC2 as measurement infrastructure, not as a target production
architecture.** Kubernetes/EKS and managed PostgreSQL/RDS are not required by the Problem and stay
out of this experiment.

### 13.1 One capacity-unit host per shard group

For Iteration C, one shard-group capacity unit is deployed on one equivalently shaped EC2 instance
containing:

- one Alloca-Go service replica;
- one PostgreSQL authority owned by that shard group;
- the migration invocation for that authority as a separate lifecycle action;
- only the bounded per-host measurement support required by the validation plan.

Keeping one service replica per group holds the service-replica axis constant while the writable
resource axis changes. PR2 and PR3c already show service headroom for the mutation-heavy path; if
Iteration C exposes service compute as the limiting subsystem, that becomes evidence for a later
service-replica question rather than a reason to vary both axes in the same experiment.

The service and PostgreSQL authority may share the capacity-unit host because the unit being
measured is the combined shard-group envelope. The comparison must not repartition one fixed
compute/memory/storage allocation among more shard groups; each added group receives its own
controlled provisioned envelope. This does not require physically dedicated underlying hardware.

### 13.2 Separate generator host

The load generator runs on separate EC2 compute from all shard-group capacity units. It routes the
stable workload according to the selected placement map and must retain enough headroom at the
4-group frontier that generator saturation cannot explain the measured server result.

The generator host is measurement infrastructure and is not counted in `G1`, `G2`, or `G4`.

### 13.3 Like-for-like comparison

All shard-group hosts in one comparison use the same selected EC2 instance shape, service image,
PostgreSQL version/configuration, service/database limits, pool policy, and placement/schema
contract. The 1-group baseline is measured on the same AWS design as the 2- and 4-group points; a
local-workstation baseline is not mixed with AWS multi-group results.

The exact EC2 instance type, AWS region/AZ, operating-system image, ports, security-group rules, and
host bootstrap commands are implementation/operations parameters. Once selected for a retained
comparison they become fixed experiment inputs and provenance, not degrees of freedom between
cells.

### 13.4 What the AWS choice does not imply

Using EC2 here does not establish that Alloca-Go needs AWS, VMs, one-host-per-shard production
placement, or cloud-managed operations. It establishes only that Iteration C needs independently
growing comparable resource envelopes and that EC2 is the selected bounded mechanism for obtaining
them within AG-Sept.

A later architecture may use containers on dedicated hosts, Kubernetes, managed databases, bare
metal, or another environment if its own Problem and requirements justify them.

## 14. Implementation and operations boundary

The current implementation is allowed to choose details such as:

- Dockerfile stage layout;
- exact base image;
- Compose service names;
- host port numbers;
- health-poll implementation;
- Makefile targets;
- whether multiple executable roles are packaged in one image;
- local file paths for placement configuration;
- the concrete AWS bootstrap and networking commands used to instantiate the Iteration C design.

Those details belong to code/configuration and
[`../operations/container-topology.md`](../operations/container-topology.md) or the applicable AWS
operations note unless changing them would alter one of the architectural properties above.

This boundary is deliberate: the deployment design should remain valid when the current
container/cloud mechanics are replaced.
