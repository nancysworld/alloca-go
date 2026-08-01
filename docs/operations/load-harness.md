# Running the PR1 load harness locally

How to drive one AG-Sept run on your own machine, and how to tell whether the run it
produced may be quoted.

**This document owns the procedure, not the rules.** What a run must contain and when a
number may be quoted are owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md) and
[`ag-sept-plan.md`](../planning/ag-sept-plan.md) §6; what PR1 built against them is recorded
in [`ag-sept-pr1-scope.md`](../planning/ag-sept-pr1-scope.md). Where those disagree with
this page, they win.

## 1. The four binaries

The harness is deliberately split, because the split is what makes a run publishable
(§6.3): the generator speaks only HTTP and holds **no database credentials**, so it can
later move to separate compute without changing anything.

| Binary | Role | Database access |
|---|---|---|
| `cmd/alloca-go` | the service under test; also serves `/metrics` on its own port | yes |
| `cmd/alloca-seed` | builds the fixture and asserts the §5.3 clean start | yes |
| `cmd/alloca-load` | the external generator; writes the run report | **no** |
| `cmd/alloca-verify` | reconciles the report against persisted state (§6.5) | yes |

## 2. Prerequisites

Docker and Go. Nothing else — no Prometheus, no Grafana; the scrape is read with `curl`.

## 3. One run, start to finish

Two terminals. The first holds the service; the rest is the run.

```sh
# terminal 1 — database, schema, service
make dev
```

`make dev` starts PostgreSQL, migrates it, then serves on `:8080` with the metrics
listener on `:9090`. Wait for `{"msg":"metrics listener starting"}` before continuing.

```sh
# terminal 2
export DATABASE_URL='postgres://alloca:alloca@localhost:55432/alloca?sslmode=disable'

# 1. fixture + clean-start assertion
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5

# 2. the run itself
go run ./cmd/alloca-load -workload dispersed -concurrency 8 -n 60 -slots 20 -out run.json

# 3. reconcile client totals against persisted state
go run ./cmd/alloca-verify -run run.json -org load-org
```

Each step exits non-zero when its result is not quotable, so `&&`-chaining them is safe:
a broken run stops the pipeline instead of handing you numbers from it.

Keep `-slots` the same across seed and load. The generator has no way to discover the
dataset, so a mismatch quietly aims traffic at slots that were never seeded.

## 4. Reading the result

The exit gate is that **three independent counts agree**. Client and persisted state come
out of steps 2 and 3; the server's own total is the third, and it is the one that is easy
to forget:

```sh
curl -s http://localhost:9090/metrics | grep '^alloca_requests_total'
```

For the run above, all three say 60:

| Source | Where |
|---|---|
| Client | `run.json` → `completed_requests`, `successful_mutation_goodput` |
| Server | `alloca_requests_total{operation="reserve",outcome="admitted_success",...}` |
| Persisted | `alloca-verify` verdict → four checks, each naming its invariant |

`alloca-verify` prints one check per §6.5 rule (INV-1, INV-5, INV-4, INV-7) and a
top-level `"quotable"`. Both binaries write their JSON even when they fail — you need to
see *why*, not just that.

## 5. Workload shapes

One mechanism each, and deliberately no combined mode: a composite moves several variables
at once and cannot be attributed (scope §3.1).

| `-workload` | What it contends | Typical shape at `-slots 20 -capacity 5` |
|---|---|---|
| `dispersed` | nothing — spread across the dataset | all admitted; add `-confirm` to drive reserve→confirm |
| `hot-slot` | one slot (`-slot`, default `slot-0`) | 5 admitted, the rest `business_refusal` / `no_capacity` |
| `hot-identity` | one identity (`-user`, default `user-0`) | 1 admitted, the rest `business_refusal` / `schedule_conflict` |

A refusal-heavy result is the workload working, not a failure — `hot-slot` saturating
capacity is the point of it. What would be a failure is those refusals not reconciling.

Other flags worth knowing: `-warm-up` discards responses completing inside the window (and
reports how many it dropped), `-timeout` is the per-request client timeout, and
`-generator-location` is recorded in the manifest.

## 6. The negative control

Mandatory and not descopable (`measurement-contract.md` §5.5): the harness must **fail**
when response validation is off, so a reported success cannot be an unchecked `200`.

```sh
go run ./cmd/alloca-load -workload dispersed -concurrency 8 -n 20 -slots 20 \
  -validate=false -out control.json
```

Expect exit 1 from `alloca-load`, and exit 1 again from `alloca-verify` on `control.json`
— the generator's refusal is carried forward rather than overridden by a reconciliation
that happens to be clean. If either exits 0, the control has stopped controlling anything;
that is a bug in the harness, not a run you may quote.

## 7. When it goes wrong

**`clean-start assertion failed: N live claims already exist`** — working as intended.
Re-run the seed with `-reset`. A confirmed claim is never expiry-reaped, so a rerun over
leftover rows returns zero admitted and all `schedule_conflict`: a result that breaks no
invariant, passes every gate, and measures nothing. This bites hardest after a `-confirm`
run, which leaves permanent rows by design.

**`ports are not available` from `make db-up`** — the host has 55432 reserved (common on
WSL2). Pick another port, and remember it is two overrides, not one:

```sh
make db-up PGPORT=5433
export DATABASE_URL='postgres://alloca:alloca@localhost:5433/alloca?sslmode=disable'
make migrate && make run   # both read DATABASE_URL from the environment
```

`PGPORT` only moves the container; the default `DATABASE_URL` still points at 55432, so
skipping the second line leaves everything connecting to nothing.

**Every request refused with `no_capacity` on `dispersed`** — the dataset is smaller than
you think, or the fixture was never reset. Check `-slots` matches between seed and load.

**Nothing at `:9090`** — `METRICS_ADDR` overrides the listener address; the service logs
where it bound at startup.

## 8. What a local run is not

It is a smoke run. The generator and the service share a machine, so the generator is
competing with the thing it measures — `run.json` reports the generator's own CPU per core
precisely so you can see that (0.02 in the PR1 artifact: nowhere near saturation, so that
run was not client-limited).

It establishes **no capacity**, and no number from it may be quoted as one. Capacity work
needs the generator on separate compute, which is PR2.

Two consequences for anything you keep:

- The manifest's operator-supplied fields — `replica_count`, `deployment_topology`,
  `environment`, `postgres_version`, pool sizes, timeout budget — are declared in the
  report but **not populated by any flag** in PR1, so they come out zero or empty. A local
  run is therefore self-describing about the generator and the workload, but not about the
  service's shape. That gap has to close before a run backs a published number (§6.4).
- Committed artifacts live in [`../measurements/`](../measurements/); the PR1 smoke run is
  [`pr1-smoke-run/`](../measurements/pr1-smoke-run/). Quote figures from an artifact, never
  from a terminal (`measurement-contract` §5.3).
