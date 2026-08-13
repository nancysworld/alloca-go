# Project structure

**Status:** Living — current through AG-Sept PR3b
**Scope:** the *source-code* architecture — directory purposes, package layout,
and the dependency rules that make the modular-monolith decision
([`../decisions/0001-modular-monolith-first.md`](../decisions/0001-modular-monolith-first.md))
enforceable at the source level rather than only as a logical diagram.

[`system-context.md`](system-context.md) describes the runtime/logical architecture
and authority boundaries; this document describes how that maps onto Go packages and
which imports are allowed. AG-M1 introduced the most consequential package boundaries
(domain, idempotency, repository, telemetry); AG-Sept adds the measurement side —
`loadgen` and `reconcile` — and the deployed topology those runs address.

---

## 1. Top-level directories

| Path | Purpose |
|---|---|
| `cmd/` | Executable composition roots — `main` packages only. One subdirectory per binary. |
| `internal/` | All application code. `internal/` prevents import by anything outside this module, keeping the package layout a private implementation detail. |
| `docs/` | Design docs, decision records, planning, reports, disclosure policy. |
| `test/` | **Operator-run verification, with one deliberate exception wired into CI.** See below. |
| `test/scripts/` | Operator and developer shell scripts invoked from the Makefile. Never application logic: anything a Go test or a Go binary should own belongs in `internal/` or `cmd/`. |
| `test/results/` | Local load-harness output (git-ignored). Scratch only — a run worth keeping is promoted into `docs/measurements/` deliberately. |
| `deploy/` | Deployment artifacts — Compose files, configuration, and the placement document. Never Go source. `deploy/topology/` is the PR3b two-authority topology; `deploy/observability/` is the PR2 diagnostic stack. |
| `.github/` | CI workflows. |
| `bin/` | Locally provisioned dev tools and built binaries (git-ignored); never source. |

### `test/` is the manual tier, not the automated one

The name invites two wrong assumptions, so both are answered here.

**It holds no Go tests.** Go test files live beside the code they exercise, under
`internal/` and `cmd/`, which is where `go test ./...` expects them and where a reader
looking for a package's tests will look first. Nothing under `test/` is compiled, and no Go
tooling treats the name specially — only `testdata/` is special to the toolchain.

**Three things under it run in CI, deliberately.** The gates are otherwise Go-only: `gofmt`,
`go vet`, `go build`, `go test ./...`, the race pass, the `-tags=integration` suite against a
PostgreSQL service, and `golangci-lint`. Beside them `.github/workflows/ci.yml` runs three shell
checks:

- `test/scripts/check-build-context.sh` asserts that `.dockerignore` excludes no tracked file —
  the property whose absence stamps every containerised run `vcs.modified=true` and makes it
  uncertifiable at any level (AG-Sept PR3b). Its end-to-end counterpart,
  `check-image-provenance.sh`, needs Docker and so stays a `make` target.
- `test/scripts/itc-cpu-layout-test.sh` asserts that `itc-cpu-layout.sh` still refuses a bad
  Iteration C CPU partition. That check is what stops a rehearsal running on a partition nothing
  validated, it is bash, and its comments once described four properties its code did not
  enforce (AG-Sept PR4a). It controls the machine's apparent CPU count with `taskset`, so it
  needs no override inside the script under test.
- `test/scripts/itc-topology-check-test.sh` asserts that `itc-topology-check.sh` still classifies
  running containers correctly — the units this rung raises, leftovers from a larger one, and
  containers belonging to another Compose project entirely. It stubs `docker`, because the
  property is set arithmetic over names rather than anything a daemon decides (AG-Sept PR4a).

Both qualify as gates for the same reason: they need nothing an operator would have to provide —
no daemon, no database, no judgement.

The split is by *what a check needs*, not by how much it is worth. A merge gate has to
reproduce on a clean runner with no operator present. Most of what lives in `test/` needs a
service somebody started, a database holding a fixture they seeded, and — for a load run —
judgement about whether the numbers mean anything at all (`../operations/load-harness.md` §8).
Those are properties of the check, not deficiencies to fix later.

So a red CI run means the code is broken; a green one is silent about whether the service
answers over a real socket, and silent about every number the harness produces. That is
what `make smoke` and the harness are for, and why they are invoked by a person.

When something here earns a merge gate, wire it into CI deliberately and record it above, as
`check-build-context.sh` is. The directory records what a check needs in order to run, never
how much it is trusted.

There is no `pkg/` directory: this module publishes no library API for external
consumers, so everything lives under `internal/`. A `pkg/` tree would be added only
if we deliberately export reusable packages, which is out of scope for AG-M0–M7.

---

## 2. `cmd/` versus `internal/`

- **`cmd/<binary>/main.go` is the composition root.** It is the only place allowed to
  wire concrete implementations together: read configuration, construct adapters
  (database pool, telemetry exporter), inject them into services, build the HTTP
  server, and manage process lifecycle (signals, graceful shutdown). It contains no
  domain logic and no business rules — only assembly.
- **`internal/` holds everything the composition root wires.** Packages under
  `internal/` must not import `cmd/`. A package that "needs something from main" is a
  signal that the dependency should be passed in (via a constructor argument or an
  interface), not reached for.

The service's composition root is `cmd/alloca-go/main.go`: it builds `config` → `postgres` →
`service` → `httpapi`, starts the expiry worker, and manages lifecycle. The measurement
binaries are composition roots of their own over the same `internal/` packages — the packages
stay ignorant of how they are assembled.

---

## 3. Intended internal package layout

Present (through AG-Sept PR3b):

```text
internal/
  config/       # env-driven configuration + validation
  buildinfo/    # runtime/build metadata for /meta (Go version, GOMAXPROCS, revision)
  domain/       # core: entities, invariants, and the interfaces it needs
    ports.go    # domain-OWNED interfaces (repositories, id-gen)
  idempotency/  # idempotency scope, request hashing, replay resolution
  service/      # application/use-case orchestration over the domain
  postgres/     # adapter: IMPLEMENTS domain-owned repository interfaces
  inmem/        # in-memory reference repository; a permanent test double
  httpapi/      # transport adapter: HTTP <-> service calls, outcome->status mapping
  worker/       # background expiry scheduling
  telemetry/    # observation types + Recorder port; slog implementation
  metrics/      # adapter leaf: the aggregated Recorder AG-Sept measures with
  ids/          # server-minted random identifiers (implements domain.IDGen)
  loadgen/      # external load harness: routing, workloads, run manifest, certification
  reconcile/    # post-run correctness self-check against persisted state and scrapes
  observability/# no shipped code: a test pinning the dashboard and the report to one query
```

Planned as AG-M2+ land (names indicative, boundaries normative):

```text
internal/
  admission/    # per-node request admission and ordering (AG-M2+)
```

The key structural rule is in §4: **the domain owns its interfaces; adapters
implement them.**

---

## 4. Allowed dependency directions

Dependencies point inward, toward the domain. The domain depends on nothing but the
standard library and its own types.

```text
cmd/alloca-go
    -> config, httpapi, service, domain, postgres, telemetry, worker, ids
                                  (wires everything)

httpapi
    -> service, domain, telemetry, config
                                  (calls use-cases; maps outcomes to HTTP)

worker
    -> domain, telemetry          (schedules background work through service ports
                                   it declares itself)

service
    -> domain, idempotency        (orchestrates domain operations)

domain
    -> (stdlib only)              (owns entities, invariants, and port interfaces)

postgres
    -> domain, config             (IMPLEMENTS domain-owned repository interfaces)

ids
    -> domain                     (IMPLEMENTS domain.IDGen)

telemetry
    -> domain                     (outcome vocabulary only; injected by cmd)

loadgen                           (AG-Sept: the external harness)
    -> domain, buildinfo, httpapi (an HTTP client of the service, not a part of it;
                                   httpapi is imported for StatusForOutcome alone, so
                                   response validation checks the service's own
                                   status<->outcome mapping rather than a copy of it)

reconcile                         (AG-Sept: the post-run self-check)
    -> domain, loadgen            (reads persisted state through a Querier it declares
                                   itself, so the pool is supplied by cmd and this
                                   package never imports postgres)

cmd/alloca-load    -> domain, loadgen
cmd/alloca-verify  -> domain, loadgen, reconcile
cmd/alloca-seed    -> config, domain, postgres
cmd/alloca-migrate -> postgres
```

`httpapi` and `worker` declare the narrow interfaces they consume (`BookingService`,
`SlotLister`, `SlotSource`, `Settler`) **at the point of consumption**, and `cmd` supplies
the concrete `*service.Service` or `*postgres.Repo` that satisfies them. That is why
neither imports `postgres` despite depending on its behaviour.

### 4.1 Domain owns its interfaces

Repository (and other infrastructure) interfaces are declared in `internal/domain`
(e.g. `domain.ReservationRepository`), expressed only in domain types. `postgres`
imports `domain` and provides concrete implementations; `service` and `httpapi`
depend on the domain interface, never on `postgres`. This keeps the core free of
PostgreSQL-specific types (`pgx` rows, SQL errors, connection handles) and lets the
transactional core be tested and reasoned about without a database.

### 4.2 Prohibited directions (normative)

- `domain` must not import `httpapi`, `service`, `cmd`, `postgres`, `telemetry`, or
  any other adapter — nor any PostgreSQL-specific/`pgx` package.
- `service` must not import `httpapi`, `cmd`, or `postgres`.
- No `internal/` package may import `cmd/`.
- Adapters (`postgres`, `telemetry`, `metrics`) must not import `httpapi` or each other; they
  are leaves wired together only by `cmd`.
- `loadgen` must not import `postgres` or hold database credentials. It is a client of the
  service's HTTP contract, and a published capacity claim requires it to be able to run on
  separate compute (`measurement-contract.md` §13.1); reconciling client totals against persisted state is
  `reconcile`'s job, reached through the `Querier` it declares.
- Transport concerns (HTTP status codes, request/response encoding) stay in
  `httpapi`; database concerns (SQL, transactions, driver types) stay in `postgres`.
  Neither leaks into `domain` or `service`.

A dependency that would violate these is the trigger for a new ADR, not a quiet
exception.

### 4.3 The rules constrain the shipped graph, not test composition roots

A **vertical integration test is a composition root**, like `cmd`: proving the assembled
service works means wiring the transport, the service, and the database adapter together,
which no single package in §4 is allowed to do.

Such a test belongs in an **external test package** (`package httpapi_test` in the
`internal/httpapi` directory), never in the package under test. An external test package is
compiled into the test binary only and is not part of the package's dependency graph, so
the arrows above continue to hold for everything that ships:

```console
$ go list -deps ./internal/httpapi | grep postgres   # no output
```

Adding the same import to a `package httpapi` file — including a `_test.go` file in that
package — *would* violate §4.2, because it puts `postgres` in `httpapi`'s graph. The
distinction is the whole reason the external package is used.

Test-support helpers that need the database live on the adapter that owns it
(`postgres.Truncate`, `postgres.ClaimDatabase`), documented as existing for tests and never
called by the service. `postgres.ClaimDatabase` in particular must be shared rather than
reimplemented per suite: every integration suite truncates the whole database and
`go test ./...` runs package binaries in parallel, so the suites must serialize on one
advisory-lock key.

---

## 5. Executables

Additional binaries are `cmd/<name>/` composition roots that reuse `internal/` packages.
Present:

- `cmd/alloca-go/` — the service.
- `cmd/alloca-load/` — the external load generator (AG-Sept; the binary the AG-M0 draft
  anticipated as `cmd/loadgen`). It is a **separate binary** and, for any published capacity
  claim, runs on **separate compute** from the service. It holds no database credentials and
  imports neither `postgres` nor `service`.
- `cmd/alloca-verify/` — the post-run reconciler, which does hold credentials because reading
  persisted state is the whole point of it. Keeping it out of the generator is what lets the
  generator move.
- `cmd/alloca-migrate/`, `cmd/alloca-seed/` — migration and fixture tools.

Further one-off operational tools follow the same pattern: a thin `main` under `cmd/` over
reusable `internal/` code.

---

## 6. Rules for adding packages and commands

1. **New capability →** put logic in an `internal/` package at the correct layer;
   keep `main` as wiring only.
2. **New binary →** add `cmd/<name>/main.go`; it wires `internal/` packages and owns
   lifecycle. No business logic in `cmd`.
3. **New infrastructure dependency (DB, cache, queue) →** define the interface the
   domain needs in `internal/domain`; implement it in a dedicated adapter package;
   wire it in `cmd`.
4. **Respect the arrows in §4.** If a change needs a prohibited import, stop and
   reconsider the boundary; record the decision in an ADR if the boundary must move.
5. **Keep transport and persistence types at the edges.** HTTP and SQL types must not
   appear in `domain` or `service` signatures.
6. **Every package earns its existence.** Prefer extending an existing package over
   adding an empty placeholder; the layout should track real code, not anticipated
   code.

---

## 7. Logical-module to package mapping

The boxes in [`system-context.md`](system-context.md) are logical/runtime
responsibilities, not a requirement for one directory per box. A logical module may
span several packages, while a package may support one narrow part of a larger
logical module. The mapping below is the current design guide; package names marked
planned remain indicative until code lands.

| Logical module in `system-context.md` | Primary source location | Supporting locations | Status / milestone |
|---|---|---|---|
| Service executable and lifecycle | `cmd/alloca-go` | `internal/config` | Implemented, AG-M0 |
| Operational surface (`/healthz`, `/readyz`, `/meta`) | `internal/httpapi` | `internal/buildinfo`, `internal/config` | Implemented, AG-M0 |
| Booking HTTP surface and outcome→status mapping | `internal/httpapi` | `internal/telemetry`; contract in [`api-surface.md`](api-surface.md) | Implemented, AG-M1 |
| Reservation and shared-resource domain services | `internal/domain`, `internal/service` | domain-specific files or subpackages | Implemented, AG-M1 (shared-resource: AG-M6) |
| Idempotency and replay resolution | `internal/idempotency` | domain-owned outcome types, `internal/postgres` persistence | Implemented, AG-M1 |
| Outcome classification | `internal/domain` | `internal/httpapi` maps outcomes to transport responses | Implemented, AG-M1 |
| Transactional repository contract | interfaces owned by `internal/domain` | consumed by `internal/service` | Implemented, AG-M1 |
| PostgreSQL transaction and cross-node authority adapter | `internal/postgres` | database migrations and configuration | Implemented, AG-M1 |
| Server-minted identifiers | `internal/ids` | implements `domain.IDGen` | Implemented, AG-M1 |
| Request admission and per-node ordering | `internal/admission` (indicative) | optional domain/service integration | Planned, AG-M2+ |
| Background expiry and settlement | expiry policy in `internal/service` (`SettleSlot`); scheduling in `internal/worker` | `internal/postgres` candidate query | Implemented, AG-M1 |
| Telemetry | `internal/telemetry` | observation boundary per [`observability.md`](observability.md); metrics backend AG-M2+ | Emission implemented, AG-M1 |
| External load generation | `cmd/alloca-load` | `internal/loadgen` — routing, workloads, run manifest, certification | Implemented, AG-Sept PR1–PR3b |
| Post-run reconciliation and the quotability verdict | `cmd/alloca-verify` | `internal/reconcile` over a `Querier` it declares; `internal/loadgen` for the manifest it checks against | Implemented, AG-Sept PR1–PR3b |
| Multi-authority deployment topology | `deploy/topology` | placement document enforced by `internal/domain`/`internal/httpapi` and routed by `internal/loadgen` | Implemented, AG-Sept PR3b |

The mapping does not weaken the dependency rules in §4. In particular, the fact
that a logical module spans packages does not permit transport or persistence types
to leak into the domain. Nor does a package name such as `admission` or `authority`
make it a fleet-wide correctness authority: PostgreSQL remains the cross-node
transactional authority unless a later ADR explicitly changes that boundary.

---

## 8. Candidate service-extraction seams `[HYPOTHESIS]`

ADR 0001 chooses a modular monolith first. The purpose of the module boundaries is
to keep deployment and transactions simple now while preserving evidence-driven
options later. The entries below are **candidate seams, not a committed microservice
plan**. They do not assert that extraction will occur, how many services will exist,
or which transport or deployment technology would be used.

A service boundary should normally align with a clear authority, scaling,
availability, security, or ownership boundary. Splitting merely by technical layer
(for example HTTP, business logic, and repository as separate services) is not an
acceptable extraction rationale because it adds distributed failure modes without
creating autonomous ownership.

| Candidate seam | Current modular-monolith form | Possible future form | Evidence that could justify extraction |
|---|---|---|---|
| Booking authority | `internal/domain` + `internal/service` + booking HTTP handlers + `internal/postgres` adapter | Independently deployed booking service owning reservation/slot invariants | Booking has a distinct scaling or availability profile; a stable transaction/data-ownership boundary exists; independent deployment materially reduces risk |
| Admission / waiting room | Per-node `internal/admission` package calling booking in process | Fleet-wide admission or waiting-room service owning queue/admission tokens | Per-node admission cannot bound synchronized load across replicas; experiments show a shared queue is required; user-visible queue semantics become product requirements |
| Expiry / settlement workers | Worker scheduling in the same deployable, invoking domain/application services | Separate worker deployment or service role | Background work interferes with request-path latency, needs different scaling, or requires independent failure isolation |
| Shard routing | Static/in-process routing based on configuration and authority keys | Independently managed shard-router/control-plane component | Authority count/topology becomes dynamic; routing changes require coordination beyond static deployment configuration |
| Telemetry pipeline | In-process instrumentation and exporter | Separate collector/ingestion tier | Export or ingestion load measurably affects request latency, reliability, or deployment independence |
| Idempotency | Domain-local idempotency module and records owned with the mutation | Potential shared capability only if multiple independently deployed mutation owners require it | Several services need one replay authority and centralized ownership is demonstrably safer than service-local idempotency; otherwise idempotency stays with the mutation owner |

### 8.1 Extraction triggers

Extraction requires evidence recorded in an ADR. One or more of the following may
justify it:

1. **Different scaling profile** — one module saturates while the rest retain useful
   headroom.
2. **Different availability or failure-isolation requirement** — one workload must
   fail, restart, or deploy independently without impairing booking.
3. **Clear authority and data ownership** — the candidate can own its invariants and
   data without distributed transactions through the normal request path.
4. **Deployment autonomy** — independent release cadence materially improves safety
   or delivery.
5. **Security/compliance boundary** — materially different access or isolation is
   required.
6. **Organisational ownership** — a durable team boundary requires independent
   operational responsibility.

Absent such evidence, extraction is rejected because it introduces network
latency, partial failure, retries, API/version compatibility, distributed tracing,
deployment coordination, and cross-service consistency costs without demonstrated
benefit.

### 8.2 Preparing seams without simulating a distributed system

Modules communicate through narrow domain/application interfaces and domain types,
not shared mutable globals, another module's tables, transport-specific DTOs, or
package cycles. These interfaces are ordinary in-process Go calls today. We do **not**
add HTTP/RPC, serialization, message brokers, or generic "remote" abstractions merely
because a package might someday be extracted.

If evidence later justifies extraction, a transport adapter may implement the same
application capability across a process boundary. The extraction must preserve or
explicitly redesign authority ownership; replacing a function call with a network
call is not by itself a valid architecture change.

### 8.3 Decisions deliberately deferred

AG-M0 does not choose:

- the eventual number of services;
- REST versus gRPC or messaging;
- a service-per-package or database-per-service policy;
- Kubernetes or any other deployment topology;
- a centralized idempotency service;
- a fleet-wide queue before experiments demonstrate the need.

Every candidate and trigger in this section is revisable as measurements and
operational evidence accumulate. A future extraction is a new architectural
decision and requires its own ADR.