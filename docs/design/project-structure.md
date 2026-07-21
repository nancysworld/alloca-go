# Project structure

**Status:** AG-M0 draft
**Scope:** the *source-code* architecture — directory purposes, package layout,
and the dependency rules that make the modular-monolith decision
([`../decisions/0001-modular-monolith-first.md`](../decisions/0001-modular-monolith-first.md))
enforceable at the source level rather than only as a logical diagram.

[`system-context.md`](system-context.md) describes the runtime/logical architecture
and authority boundaries; this document describes how that maps onto Go packages and
which imports are allowed. AG-M1 introduces the most consequential package
boundaries (domain, idempotency, repository, telemetry), so the rules are fixed here
before that code lands.

---

## 1. Top-level directories

| Path | Purpose |
|---|---|
| `cmd/` | Executable composition roots — `main` packages only. One subdirectory per binary. |
| `internal/` | All application code. `internal/` prevents import by anything outside this module, keeping the package layout a private implementation detail. |
| `docs/` | Design docs, decision records, planning, reports, disclosure policy. |
| `.github/` | CI workflows. |
| `bin/` | Locally provisioned dev tools (git-ignored); never source. |

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

Current composition root: `cmd/alloca-go/main.go` builds `config` → `httpapi` and
runs the server. As AG-M1 adds a database and services, `main` grows the wiring for
those; the packages themselves stay ignorant of how they are assembled.

---

## 3. Intended internal package layout

Present (AG-M0):

```text
internal/
  config/      # env-driven configuration + validation
  buildinfo/   # runtime/build metadata for /meta (Go version, GOMAXPROCS, revision)
  httpapi/     # HTTP transport: routing, handlers, server construction
```

Planned as AG-M1+ land (names indicative, boundaries normative):

```text
internal/
  config/
  buildinfo/
  httpapi/                 # transport adapter: HTTP <-> domain calls, outcome mapping
  domain/                  # core: entities, invariants, and the interfaces it needs
    <e.g. reservation, booking, inventory, balance>
    ports.go               # domain-OWNED interfaces (repositories, clock, id-gen)
  service/                 # application/use-case orchestration over the domain
  idempotency/             # idempotency scope, request hashing, replay resolution
  postgres/                # adapter: IMPLEMENTS domain-owned repository interfaces
  telemetry/               # metrics/traces/logging wiring (OpenTelemetry-compatible)
```

The key structural rule is in §4: **the domain owns its interfaces; adapters
implement them.**

---

## 4. Allowed dependency directions

Dependencies point inward, toward the domain. The domain depends on nothing but the
standard library and its own types.

```text
cmd/alloca-go
    -> config, httpapi, service, domain, postgres, telemetry   (wires everything)

httpapi
    -> service, domain            (calls use-cases; maps outcomes to HTTP)

service
    -> domain                     (orchestrates domain operations)

domain
    -> (stdlib only)              (owns entities, invariants, and port interfaces)

postgres
    -> domain                     (IMPLEMENTS domain-owned repository interfaces)

telemetry
    -> (stdlib + OTel libs)       (injected into others by cmd)
```

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
- Adapters (`postgres`, `telemetry`) must not import `httpapi` or each other; they are
  leaves wired together only by `cmd`.
- Transport concerns (HTTP status codes, request/response encoding) stay in
  `httpapi`; database concerns (SQL, transactions, driver types) stay in `postgres`.
  Neither leaks into `domain` or `service`.

A dependency that would violate these is the trigger for a new ADR, not a quiet
exception.

---

## 5. Future executables

Additional binaries are added as `cmd/<name>/` composition roots that reuse
`internal/` packages. Planned:

- `cmd/loadgen/` — the external Go load generator (AG-M2). It is a **separate binary**
  and, for any published capacity claim, runs on **separate compute** from the
  service. It may import shared `internal/` packages (e.g. domain outcome types for
  response validation) but must not import the service's transport or database
  adapters.

Migrations, one-off operational tools, etc. follow the same pattern: a thin `main`
under `cmd/` over reusable `internal/` code.

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
