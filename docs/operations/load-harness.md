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
export DATABASE_URL='postgres://alloca:alloca@localhost:15432/alloca?sslmode=disable'

# 1. fixture + clean-start assertion
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5

# 2. the run itself
go run ./cmd/alloca-load -workload dispersed -concurrency 8 -n 60 -slots 20 -out run.json

# 3. reconcile client totals against persisted state
go run ./cmd/alloca-verify -run run.json -org load-org
```

Each step exits non-zero when its result is not quotable, so `&&`-chaining them is safe:
a broken run stops the pipeline instead of handing you numbers from it.

To run it a second time, restart the service (`make dev` in terminal 1) rather than only
re-seeding. `-reset` returns the database to a clean fixture but cannot touch the server's
in-process counters, and those keep accumulating across runs — see §4. `make db-down` is
not part of this: `db-up` already removes any existing container before starting one.

Keep `-slots` the same across seed and load. The generator has no way to discover the
dataset, so a mismatch quietly aims traffic at slots that were never seeded.

### The other two workloads

Same three steps; only step 2 changes. Each one needs its own seed — `-reset` before every
run, and between workloads as much as between repeats of one (§7).

```sh
# hot-slot — many identities, one slot
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
go run ./cmd/alloca-load -workload hot-slot -concurrency 8 -n 60 -slots 20 -slot slot-0 \
  -out hot-slot.json
go run ./cmd/alloca-verify -run hot-slot.json -org load-org
```

```sh
# hot-identity — one identity, many slots
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
go run ./cmd/alloca-load -workload hot-identity -concurrency 8 -n 60 -slots 20 -user user-0 \
  -out hot-identity.json
go run ./cmd/alloca-verify -run hot-identity.json -org load-org
```

`-slot` and `-user` already carry these defaults. They are written out because the contended
authority is the whole point of the run, and reading it off the command beats remembering
which default applies to which workload.

`-slots` changes nothing about hot-slot's traffic — every request goes to `-slot` — but it is
recorded in the manifest as `dataset_slots`, so omitting it files the run under a 100-slot
dataset that was never seeded. Pass it on all three.

## 4. Reading the result

The exit gate is that **three independent counts agree**. Client and persisted state come
out of steps 2 and 3; the server's own total is the third, and it is the one that is easy
to forget. Only the server count prints as a bare number — the other two are fields inside
JSON, so here is how to read each of them:

```sh
# client — written by step 2
jq '.summary.completed_requests, .summary.successful_mutation_goodput' run.json

# server — read the replay="false" series only; never add the replay="true" one to it
# needs the service still running; nothing persists this after a restart, and nothing
# resets it either, so a second run against the same process reads 120, not 60
curl -s http://localhost:9090/metrics | grep '^alloca_requests_total.*replay="false"'

# persisted — step 3 again, this time reading the counts it reconciled against
go run ./cmd/alloca-verify -run run.json -org load-org | jq -r '.checks[].detail'
```

For the `dispersed` run above, all three say 60:

| Source | Reads | Says |
|---|---|---|
| Client | `run.json` → `completed_requests`, `successful_mutation_goodput` | `60`, `60` |
| Server | `alloca_requests_total{...,replay="false"}` | `60` |
| Persisted | `alloca-verify` → the counts quoted in each check's `detail` | `60` live claims, `60` records |

The `replay` label is what makes a second run legible. A `replay="true"` series is not a
failure — those requests reached the service and were correctly answered from the
idempotency record without committing anything, so they belong in neither the client's
goodput nor the persisted rows. Summing the two series and comparing that to 60 is the
mistake the gate is built to catch.

The persisted count has no field of its own: it is stated inside each check's `detail`
prose (`"60 live claims, no overlapping pair, ..."`). `ok` and `quotable` are the
machine-readable verdict; the numbers are there for you to compare against the other two
by eye.

`alloca-verify` prints one check per §6.5 rule (INV-1, INV-5, INV-4, INV-7) and a
top-level `"quotable"`. Both binaries write their JSON even when they fail — you need to
see *why*, not just that.

### Reading a contended workload

`dispersed` is the easy case: every request commits, so one number — 60 — appears in all
three places. Under `hot-slot` and `hot-identity` most requests are refused, goodput is no
longer the request count, and the gate becomes two comparisons rather than one:

- `completed_requests` against the **sum** of the server's `replay="false"` series, which is
  now split across several `outcome`/`reason` labels;
- `successful_mutation_goodput` against the persisted count in the verifier's `detail`.

Both runs above, at `-n 60 -slots 20 -capacity 5`:

| | `hot-slot` | `hot-identity` |
|---|---|---|
| Client `completed_requests` | `60` | `60` |
| Client `successful_mutation_goodput` | `5` | `1` |
| Client `totals` | 5 `admitted_success`, 55 `no_capacity` | 1 `admitted_success`, 59 `schedule_conflict` |
| Server, summed over `replay="false"` | `60` | `60` |
| Persisted (INV-1 / INV-4 `detail`) | `5` live reservations, `5` live claims | `1` live reservation, `1` live claim |

Goodput equals capacity for `hot-slot` and exactly one for `hot-identity`: those are the
shapes §5 predicts, and a run that misses them measured something other than what its
`-workload` says. The refusals are not failures — but they are still recorded outcomes, so
INV-5 reports 60 idempotency records for 60 fresh mutations in both runs, not 5 and 1.

Only the server side needs care. Its counters accumulate across runs and reset only when the
process restarts, so after the seed→load→verify cycle above the raw scrape holds every run
the process has served; it is the delta that must equal 60. Restarting the service between
runs is the way to keep reading it as an absolute.

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

The flag takes hyphens; the report writes underscores. A `-workload hot-slot` run records
`"workload": "hot_slot"` in its manifest, so a `jq select` or a `grep` over committed
artifacts has to match the underscored form.

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

**`ports are not available ... /forwards/expose returned unexpected status: 500`** — the
host has the port reserved, and on Windows that is usually not another process holding it.
Hyper-V and WSL2 reserve blocks inside the dynamic port range (49152–65535) at boot, and
Docker cannot bind anything inside one. Confirm before guessing:

```sh
netsh.exe interface ipv4 show excludedportrange protocol=tcp   # from WSL, note the .exe
```

If the port falls in a listed range, pick one below 49152 — the default `PGPORT` is 15432
for exactly this reason. Reservations only reshuffle on reboot, so a port in that range
that works today can fail tomorrow with no local change.

`DATABASE_URL` derives from `PGPORT`, so moving the container is one override:

```sh
make db-up PGPORT=15433
make migrate PGPORT=15433 && make run PGPORT=15433
```

Setting `DATABASE_URL` explicitly still overrides both, which is what points the same
targets at a database that is not the local container.

**A run reports goodput but the database does not move** — check `replay` in the totals:

```sh
jq '.summary.totals' run.json     # "replay": true on everything means nothing committed
```

Idempotency keys are `workload-seq-step` with no per-run nonce (`internal/loadgen/workload.go:39`),
so re-running the same `-workload` re-sends the keys the previous run already used and the
server correctly replays each recorded outcome instead of committing. The run then passes
reconciliation — no invariant is broken — while measuring nothing, which is the §5.3 trap
in a different costume. Re-seed with `-reset` between runs (it truncates
`idempotency_records`), or change `-workload`. The clean-start assertion does not cover
this: it guards the seed, not the load.

**`N live reservations persisted but only M fresh reserves were admitted`** — a workload ran
against the rows the previous one left. Reservations from the earlier run are still held
until their TTL expires, and confirmed ones never expire at all, so the verifier sees live
claims the current report never mentions and fails INV-1.

Nothing upstream warns you: `alloca-load` exits 0, and the report it writes already has the
wrong shape. The `hot-identity` run that produced this line reported 3 `no_capacity`
refusals from `slot-0` — an outcome `hot-identity` cannot generate on a clean fixture, since
its 60 requests spread one identity over 20 slots and are refused on the identity, not on
capacity. Those three came from `hot-slot` having filled `slot-0` minutes earlier. Re-seed
with `-reset` before each workload.

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
