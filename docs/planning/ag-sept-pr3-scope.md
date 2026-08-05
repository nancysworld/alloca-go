# AG-Sept PR3 — Horizontal database authority, Phase 1 (scope)

**Status:** Proposed — §6 needs Nancy before implementation starts
**Budget:** 7.2 development days across three PRs ([AG-Sept plan](ag-sept-plan-new.md) §4) —
2.5 for PR3a, 2.7 for PR3b, 2.0 for PR3c
**Owner doc:** [ag-sept-plan-new.md](ag-sept-plan-new.md) §8.2 and §6.5 are normative for what
this PR builds; this note records only how PR3 discharges them and the choices made along the
way
**Design input:**
[`../design-notes/horizontal-database-authority.md`](../design-notes/horizontal-database-authority.md)
owns the design. This note does not restate it, and where the two disagree the design note wins
once §6.1 is settled.

## 1. Exit gates

PR3 is three PRs because it has three separable exit gates, each of which can fail on its own.
From the plan, verbatim:

> **PR3a:** a service unit cannot be started against an inconsistent placement map or an
> incompatible schema; a misrouted request is refused rather than written; the booking policy is
> one named outcome across every closed set that must know about it; and no accepted AG-M1
> invariant has been weakened to make any of it fit.

> **PR3b:** a multi-authority run is reproducible from a version-controlled topology, produces
> an aggregated correctness verdict naming every authority it read, and is refused certification
> when the units disagree.

> **PR3c:** Phase 1 correctness and failure isolation are demonstrated on independent writable
> authorities; every accepted transaction semantic on the supported path is unchanged; and no
> result claims a throughput multiplier from this workstation.

The clause that decides whether PR3 succeeds honestly is PR3a's last one. Phase 1 is only worth
building if the local transaction path it composes is the *same* path AG-M1 proved. A Phase 1
that reached its topology by relaxing an invariant would have moved the problem, not scaled it.

**What PR3 may not claim, whatever it measures.** Both authorities, both service units, the
generator, and the telemetry stack share one 10-vCPU WSL2 allocation
([`../measurements/environment.md`](../measurements/environment.md)). Authority composition
cannot be quoted as a capacity multiplier from this machine, and PR2's unexplained ~2×
excursions are still open, so any single reading carries a ±2× caveat until PR4's node exporter
exists. PR3 is correctness-first by design, not by descope.

## 2. What PR3 delivers

| # | Deliverable | PR | Plan reference |
|---|---|---|---|
| 1 | Versioned placement map: loading, validation, immutability for a run, startup gate | 3a | §8.2 |
| 2 | Shard-affine service units — one authority, one pool, readiness bound to it | 3a | §8.2 |
| 3 | Server-side placement enforcement, with the §12.5 misrouting control | 3a | §8.2, §12.5 |
| 4 | Phase 1 booking policy as one named outcome through every closed set it touches | 3a | §8.2 |
| 5 | `/meta` extended with authority identifier, routing version, schema version | 3a | §6.4 |
| 6 | Placement invariants in the register, each with a discriminating test | 3a | §3.2 |
| 7 | Containerised two-authority topology, per-authority migration | 3b | §9.1 |
| 8 | Generator routes by organisation; multi-organisation and one-hot-organisation workloads | 3b | §5.6, §6.3 |
| 9 | Placement, authority count, and assignment in the manifest; multi-service certification | 3b | §6.4 |
| 10 | Authority-aware verifier with one aggregated verdict | 3b | §6.5 |
| 11 | Phase 1 correctness experiments and per-authority verdicts | 3c | §11 |
| 12 | Failure-isolation experiment — one authority down, then restored | 3c | §11 |

## 3. What the existing code makes cheap, and what it does not

Recorded so the estimates in §4 are auditable rather than asserted, and so the work is not
rediscovered.

### 3.1 Reconciliation is already organisation-scoped — the largest saving

`reconcile.Run(ctx, q Querier, org domain.OrganisationID, r loadgen.Report, scrapes Scrapes)`
takes an organisation, and `capacityCheck`, `idempotencyCheck`, and `claimsCheck` each take it
in turn (`internal/reconcile/reconcile.go`). The checks are already local to one organisation's
rows, which is exactly the granularity Phase 1 places on one authority.

Authority-aware verification is therefore mostly a loop over `(authority → organisations)` with
the right pool, plus verdict aggregation — not a rewrite. `cmd/alloca-verify` needs the
placement map in place of its single `--database-url` and `--org` flags.

### 3.2 Per-authority attribution comes free from per-service scrapes

Client-side totals carry no organisation or authority dimension: `loadgen.Total` is
`{Operation, Outcome, Reason, Replay, Count}` (`internal/loadgen/run.go:311`). Adding one would
touch the report schema, the manifest, and every artifact that reads them.

It is not needed. §8.2 makes every service unit shard-affine, so a unit's own Prometheus scrape
*is* its authority's traffic. Per-authority work is read from per-service scrapes, and
`serverTotalsCheck` sums across them. This is why the plan lists per-authority client-side
attribution under "not in PR3b" rather than as a cut — it would be a second, weaker source for
a fact the topology already supplies.

One consequence to implement carefully: `Scrapes` is a single `{Baseline, After}` pair
(`internal/reconcile/servertotals.go:129`) whose `measured()` detects a restarted process. With
N units, each unit's pair must be differenced *separately* and then summed — differencing the
sums would mask one unit restarting mid-run.

### 3.3 The generator is single-authority throughout

`loadgen.Client` holds one `baseURL` (`internal/loadgen/client.go:76`) and each workload
generator holds one `Org` (`internal/loadgen/workload.go:47,77,129`). Routing by organisation
means an org→endpoint map in the client and a multi-organisation dataset in the workloads. This
is the bulk of PR3b's generator cost.

### 3.4 `/meta` reports nothing about placement or schema

`metaResponse` inlines `buildinfo.Info` plus timing configuration and a database block
(`internal/httpapi/meta.go`, `internal/buildinfo/buildinfo.go`). There is no authority
identifier, routing version, or schema version. §5.1 of the design note gates setup on schema
compatibility, and §5.2 on reporting authority identity — both are new fields, and both flow
into `loadgen/servicemeta.go`, which is what makes a run certifiable.

Migrations are applied by `cmd/alloca-migrate` as a deliberate separate step (ADR-0002), so
per-authority migration is a loop over DSNs, not a design change.

### 3.5 The refusal is a closed-set change, not a constant

`domain.Reason` is a closed set with a membership map (`internal/domain/outcome.go:49-57` and
`:120-128`), and the HTTP mapping is keyed by outcome and total over the contract
(`api-surface.md` §2.3). A new reason touches the domain set, the mapping, `api-surface.md`, the
invariant register, the `measurement-contract.md` §4 taxonomy, and the metric label allowlist.
Priced into PR3a at roughly 0.4 days, and the reason PR3a is not smaller than PR3b.

## 4. Budget, by component

Estimates, not measurements. Recorded at this granularity so an overrun is attributable.

**PR3a — 2.5 days**

| Component | Days |
|---|---:|
| Placement map: type, config, validation, immutability, startup gate | 0.5 |
| Shard-affine binding and server-side enforcement of the assigned organisation set | 0.4 |
| `/meta` authority identifier, routing version, schema version | 0.3 |
| Booking policy through the closed sets and the status mapping | 0.4 |
| Ownership decision from §6.1, implemented as settled | 0.3 |
| Normative doc updates: api-surface, invariant register, measurement contract, design-note links | 0.4 |
| Discriminating tests, each proved to fail without its property | 0.2 |

**PR3b — 2.7 days**

| Component | Days |
|---|---:|
| Containerised two-authority topology, per-authority migration, reproducible from version control | 0.6 |
| Generator org→endpoint routing and multi-organisation workloads | 0.7 |
| Multi-service manifest and certification — every unit's `/meta`, revision and schema agreement | 0.6 |
| Authority-aware verifier: placement input, per-authority loop, N scrape pairs, aggregated verdict | 0.8 |

**PR3c — 2.0 days**

| Component | Days |
|---|---:|
| Multi-organisation seeding across authorities | 0.3 |
| Correctness matrix — dispersed, one-hot-organisation, cross-organisation refusal | 0.5 |
| Failure-isolation experiment: down, observe, restore, verify | 0.5 |
| Report with per-authority verdicts and the organisation-to-authority distribution | 0.7 |

## 5. Decisions taken

### 5.1 Three PRs, not one

The three exit gates fail independently, and the first is a contract change to a system with a
merged, tested invariant register. Bundling them would mean the first review of a placement
model arrives alongside a topology and a measurement run, which is how a contract error reaches
evidence. PR3a can also merge and sit — it is inert in a single-authority deployment if §6.1
resolves the policy as placement-derived.

### 5.2 Routing lives in the generator *and* is enforced by the service

The design note §4.1 allows static routing to live in the generator for the first experiment,
which is right — a production routing gateway is not needed to prove authority composition. But
if the generator is the *only* router, then "no supported request reached the wrong authority"
is a property of the generator's routing table, not of Alloca, and the placement invariant is
untested by construction.

Both, therefore: the generator routes, and each unit refuses what it does not own. The §12.5
misrouting control is what makes the second half real, and it is why that control is mandatory
rather than desirable.

### 5.3 Correctness evidence is not a source of budget

If PR3a–PR3c overrun, the difference comes out of PR4's measurement scope through the plan's
§16 descope order, not out of the gates that make a run admissible. Nancy's call, 2026-08-05.

### 5.4 Per-authority attribution is read from per-service scrapes

See §3.2. This is a design choice with a budget consequence, not a descope.

## 6. Open — these need Nancy before or during implementation

### 6.1 The three contract questions from the design-note review

Raised against `horizontal-database-authority.md` in
[PR #12](https://github.com/nancysworld/alloca-go/pull/12) and unresolved at the time of
writing. Each changes what PR3a builds, so each needs an answer before PR3a starts rather than
during it.

1. **Confirm/cancel ownership.** The note's §5.3 says confirm and cancel "continue to validate
   ownership". They do not: `lockByReservation` (`internal/service/service.go:392`) and
   `tx.Reservation` (`internal/postgres/tx.go:178`) resolve by reservation identifier alone, and
   the caller's `UserRef` only scopes idempotency. A confirm carrying the wrong
   `user_organisation_id` succeeds today and would return `unknown_target` under Phase 1
   routing. **Add a real ownership check, or record that routing becomes the de-facto check and
   that this outcome differs by topology?** Adding the check is roughly +0.4 days on PR3a and
   is a domain change with its own normative updates.
2. **Is the booking policy unconditional or derived from placement?** The note states it
   unconditionally (§4.1, §10), which regresses the single-authority deployment: cross-
   organisation booking is shipped, tested behaviour (INV-13, and
   `user-schedule-non-overlap.md` §3.3 records that its test exists precisely to stop a later
   change disabling it). Deriving the refusal from placement — refuse only when the two
   organisations resolve to different authorities — keeps today's semantics in a one-authority
   deployment and keeps INV-13 exercised end to end. **Recommended: placement-derived.**
3. **Which outcome does an unavailable authority produce?** The note's placement invariant 3
   requires a "classified unavailable outcome"; no such outcome exists in the closed set
   (`internal/domain/outcome.go:9-35`), and a dead authority currently produces
   `internal_failure` or `timeout_db` — which would fail the run's admissibility gates during
   exactly the failure-isolation experiment PR3c needs. **Name the existing outcome, or add one
   and say how per-authority failure is accounted for in the gates.**

### 6.2 How many organisations, and how skewed

The design note's example map is three organisations across two authorities. The correctness
gates do not need more, but the one-hot-organisation workload and the distribution reporting of
§5.6 are more informative with a deliberate skew. Proposed `[HYPOTHESIS]`: four organisations,
2:2 by count and deliberately unequal by load. Cheap to change; recorded so the run is not
designed by accident.

### 6.3 Whether PR3a merges before PR3b exists

PR3a is inert in a single-authority deployment if §6.1.2 resolves as placement-derived, so it
can merge alone. If it resolves as unconditional, PR3a changes behaviour for the current
deployment and should wait for PR3b so the two land together.

## 7. Not in PR3

- cross-shard booking, distributed commit, or any saga (design note §10, plan §15);
- splitting one organisation across authorities;
- online rebalancing, dual-write migration, or a placement change during a run;
- a production routing gateway or dynamic shard catalogue;
- a shared global workflow database;
- replica scaling, exporters, dashboards, or the connection-budget control — all PR4;
- any capacity or throughput-multiplier claim;
- per-authority client-side attribution (§3.2, §5.4);
- authentication, which §7.6 of the design note correctly identifies as the real fix for
  caller-asserted routing identity and which no part of AG-Sept delivers.

## 8. Evidence labels

Every figure in PR3's report carries the `[HYPOTHESIS]`, `[MEASURED]`, `[DERIVED]`, or
`[PRIOR-UNREPRODUCED]` label required by
[`measurement-contract.md`](../design/measurement-contract.md) §2. Organisation counts,
authority counts, and the placement map in this note are `[HYPOTHESIS]` until a run uses them.

Correctness verdicts are not measurements and carry no label: a gate passes or the run is not
admissible.
