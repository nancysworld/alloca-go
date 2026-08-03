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
| `cmd/alloca-verify` | reconciles the report against the metrics scrape and persisted state (§6.5) | yes |

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
mkdir -p test/results   # git-ignored, and absent on a fresh clone

# 0. build the generator — see "Why the generator is built, not `go run`" below
go build -o bin/alloca-load ./cmd/alloca-load

# 1. fixture + clean-start assertion
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5

# 2. the run itself
./bin/alloca-load -workload dispersed -concurrency 8 -n 60 -slots 20 -out test/results/run.json

# 3. capture the server's own count, before anything else touches the service
curl -s http://localhost:9090/metrics > test/results/metrics.txt

# 4. reconcile client, server and persisted totals
go run ./cmd/alloca-verify -run test/results/run.json -metrics test/results/metrics.txt \
  -org load-org -out test/results/verdict.json
```

Each step exits non-zero when its result is not quotable, so `&&`-chaining them is safe:
a broken run stops the pipeline instead of handing you numbers from it.

`-metrics` is what makes the verdict a three-way agreement rather than a two-way one. Without
it the verifier still runs, but the client/server check fails and the run is **not quotable**
— deliberately, because §6.5 requires client totals, server totals and persisted state to
reconcile, and a gate that silently certified two of the three would be the weaker gate
wearing the stronger gate's name. The scrape is a file rather than a URL the verifier fetches
so that the numbers being reconciled are the ones taken at the end of the run, not whatever
the service reports whenever the verifier happens to run.

**Restart the service before *every* run, not just the second one.** `-reset` returns the
database to a clean fixture but cannot touch the server's in-process counters, and those
accumulate from process start — see §4. `make db-down` is not part of this: `db-up` already
removes any existing container before starting one.

The trap is not only a previous load run. **Anything** the service answered since it started
counts, and the natural thing to do after `make dev` is the natural thing that breaks this:

```sh
make dev     # terminal 1
make smoke   # ← 13 requests, and the scrape will carry all of them
```

`make smoke` issues 13 counted requests across reserve, confirm, cancel and `list_slots`. A
`-n 60` run against that service then scrapes 73, and `alloca-verify` fails the client/server
check — correctly, since `run.json` describes 60 of them. A hand-rolled `curl` against
`/v1/slots` does the same thing one request at a time.

So the order is: smoke-test if you want to, then **restart the service**, then seed, load and
scrape. The `curl` in step 3 is safe because it hits `:9090/metrics`, which is a separate
listener and is not itself counted.

Keep `-slots` the same across seed and load. The generator has no way to discover the
dataset, so a mismatch quietly aims traffic at slots that were never seeded.

### Why the generator is built, not `go run`

**`go run` does not stamp VCS information into the binary.** `go build` does. The manifest
reads its `commit_sha` from that stamp, so a run driven by `go run` records an *empty* commit
SHA — and a result whose code identity is unknown cannot be reproduced or compared, which is
the whole point of §6.4.

This is not theoretical: it is how `commit_sha: ""` reached PR1's first committed evidence.
Nothing complained, because an empty string is a perfectly valid JSON value.

`alloca-load` now refuses such a run outright — level `none`, exit 1 — so the failure is
loud. Build it once per change and re-run:

```sh
go build -o bin/alloca-load ./cmd/alloca-load
```

The same applies to a **dirty working tree**. The manifest records `source_modified`, and a
true value fails the gate: a SHA that does not describe the binary that ran is worse
provenance than no SHA at all, because nothing about it looks wrong. Commit or stash before a
run whose numbers you intend to keep.

`alloca-seed` and `alloca-verify` stay on `go run` — neither writes a manifest.

### The other two workloads

Same four steps; only step 2 changes. Each one needs its own seed — `-reset` before every
run, and between workloads as much as between repeats of one (§7).

```sh
# hot-slot — many identities, one slot
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
./bin/alloca-load -workload hot-slot -concurrency 8 -n 60 -slots 20 -slot slot-0 \
  -out test/results/hot-slot.json
curl -s http://localhost:9090/metrics > test/results/hot-slot-metrics.txt
go run ./cmd/alloca-verify -run test/results/hot-slot.json \
  -metrics test/results/hot-slot-metrics.txt -org load-org
```

```sh
# hot-identity — one identity, many slots
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
./bin/alloca-load -workload hot-identity -concurrency 8 -n 60 -slots 20 -user user-0 \
  -out test/results/hot-identity.json
curl -s http://localhost:9090/metrics > test/results/hot-identity-metrics.txt
go run ./cmd/alloca-verify -run test/results/hot-identity.json \
  -metrics test/results/hot-identity-metrics.txt -org load-org
```

`-slot` and `-user` already carry these defaults. They are written out because the contended
authority is the whole point of the run, and reading it off the command beats remembering
which default applies to which workload.

`-slots` changes nothing about hot-slot's traffic — every request goes to `-slot` — but it is
recorded in the manifest as `dataset_slots`, so omitting it files the run under a 100-slot
dataset that was never seeded. Pass it on all three.

## 4. Reading the result

The exit gate is that **three independent counts agree**, and step 4 now checks that rather
than leaving it to you: the client/server comparison is a check in the verdict like any
other. What follows is how to read the same three numbers yourself, which is what you want
when a check has failed and you need to see which way.

```sh
# every check and its numbers, including the client/server comparison
jq -r '.checks[] | "\(.ok)\t\(.name)\t\(.detail)"' test/results/verdict.json

# client — written by step 2
jq '.summary.completed_requests, .summary.successful_mutation_goodput' test/results/run.json

# server — read the replay="false" series only; never add the replay="true" one to it
# nothing resets these counters, so a scrape taken across two runs reads 120, not 60
grep '^alloca_requests_total.*replay="false"' test/results/metrics.txt
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
prose (`"60 live claims, no overlapping pair, ..."`). `ok` and `quotability.level` are the
machine-readable verdict; the numbers are there for when you need to see how a check
reached it.

### What the run may back: `quotability.level`

There is no `quotable: true` field, and deliberately so. Whether a number may be used depends
on what it is used *for*: §6.4 gates a capacity claim on topology provenance, and §6.3 gates
a published one on the generator running off the service host. An unqualified boolean cannot
express that, and the one this replaced invited a co-resident smoke run to be read as
publishable.

Both binaries record a level instead:

| Level | Means | Reached when |
|---|---|---|
| `none` | The run measured nothing usable | validation off or failed, run interrupted, warm-up rows unreconciled, reconciliation failed, or required generator provenance missing |
| `local` | A sound observation about this machine | every field the generator determines for itself is populated |
| `capacity` | May back a capacity claim about that topology | + PostgreSQL version, pool sizes, server `GOMAXPROCS`, timeout budget, reservation TTL, replica count, deployment topology, environment |
| `publishable` | Satisfies §6.3's provenance requirement | + `-generator-location` names a host separate from the service |

```sh
jq -r '.quotability | "\(.level)\t\(.blocked_because)"' test/results/run.json
```

**PR1 runs reach `local`, and that is the correct outcome, not a defect.** The generator is
an HTTP client and cannot discover the service's shape, so the fields above `local` are
supplied by an operator in the PR that first has something to say about them — service shape
in PR2, topology and image identity in PR3, environment in PR4 (`ag-sept-plan.md` §14). The
report says so itself in `blocked_because`, naming each missing field and the PR that owns
it, so an incomplete manifest reads as scheduled rather than broken.

Both binaries take `-require` to set the bar, defaulting to `local`:

```sh
./bin/alloca-load -require local ...        # PR1: fails on missing generator provenance
./bin/alloca-load -require publishable ...  # PR4: additionally fails a co-resident generator
```

The bar is declared at the call site because only the caller knows what the number is for. A
run below its `-require` level exits non-zero with the reason, and still writes its report.

One thing `publishable` does *not* mean: that the run is ready to publish. It checks the
provenance §6.3 requires, which is a declaration in the manifest. The §12.2
generator-headroom control is evidence rather than provenance, and it arrives with PR4.

`alloca-verify` prints five checks — one per §6.5 database rule (INV-1, INV-5, INV-4,
INV-7) and the client/server comparison, which carries no invariant because it is a
statement about instrumentation rather than about the domain. Both binaries write their
JSON even when they fail — you need to see *why*, not just that.

A scrape taken without restarting the service is the common way to fail the new check: the
counters are cumulative, so the scrape carries every run the process has served while
`run.json` describes one. The check's `detail` says so when the totals differ, because that
is the first thing to suspect and not the last.

To confirm it rather than assume it, subtract your client totals from the scrape and look at
what is left:

```sh
grep '^alloca_requests_total' test/results/metrics.txt
```

A residual that is only `reserve`/`admitted_success` is an earlier load run. A residual
carrying `confirm`, `cancel`, `list_slots` and a spread of refusal reasons is `make smoke`,
which contributes exactly 13. Either way the fix is the same — restart the service and re-run
from the seed — but knowing which tells you whether anything else on the machine is talking
to the service.

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

Other flags worth knowing: `-timeout` is the per-request client timeout, and
`-generator-location` is recorded in the manifest.

`-warm-up` exists but **makes the run not quotable in PR1**, deliberately. It drops responses
completing inside the window from the client totals while their reservations, claims and
idempotency records stay in the database, so persisted-state reconciliation would compare all
the rows against post-warm-up totals and fail a service that did nothing wrong. Refusing the
run is the honest response to that; reporting one that cannot be reconciled is not. Making
warm-up quotable needs a separate warm-up phase with a reset between, or per-cell warm-up
totals carried through to the verifier, and belongs with the sweeps in PR2 that need it.

## 6. The negative control

Mandatory and not descopable (`measurement-contract.md` §5.5): the harness must **fail**
when response validation is off, so a reported success cannot be an unchecked `200`.

```sh
./bin/alloca-load -workload dispersed -concurrency 8 -n 20 -slots 20 \
  -validate=false -out test/results/control.json
curl -s http://localhost:9090/metrics > test/results/control-metrics.txt
go run ./cmd/alloca-verify -run test/results/control.json \
  -metrics test/results/control-metrics.txt -org load-org
```

Expect exit 1 from `alloca-load`, and exit 1 again from `alloca-verify` on `control.json`
— the generator's refusal is carried forward rather than overridden by a reconciliation
that happens to be clean. Both report `quotability.level: "none"`, and soundness is checked
before provenance, so the reason names the disabled validation rather than whatever else the
manifest is missing. If either exits 0, the control has stopped controlling anything; that is
a bug in the harness, not a run you may quote.

Pass `-metrics` here too, even though the verifier would exit 1 without it. A control that
fails for the wrong reason is not a control: the point is to watch the generator's verdict
survive a *clean* reconciliation, and omitting the scrape would fail the run on a missing
input before that verdict is ever tested. Check `quotability.blocked_because` names the
generator, not the scrape.

## 7. When it goes wrong

**`clean-start assertion failed: N live claims already exist`** — working as intended.
Re-run the seed with `-reset`. A confirmed claim is never expiry-reaped, so a rerun over
leftover rows returns zero admitted and all `schedule_conflict`: a result that breaks no
invariant, passes every gate, and measures nothing. This bites hardest after a `-confirm`
run, which leaves permanent rows by design.

**`clean-start assertion failed: N idempotency records already exist`** — the same assertion,
second half. Zero live claims does not imply a clean fixture: a record outlives the entity it
describes, on purpose, so that a late retry still replays. A fixture with every hold expired
or cancelled therefore passes the claim count while still holding the keys that would turn
the next run into a replay of the last one. `-reset` clears both.

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
jq '.summary.totals' test/results/run.json     # "replay": true on everything means nothing committed
```

Idempotency keys are `workload-seq-step` with no per-run nonce (`internal/loadgen/workload.go:39`),
so re-running the same `-workload` re-sends the keys the previous run already used and the
server correctly replays each recorded outcome instead of committing. The run then passes
reconciliation — no invariant is broken — while measuring nothing, which is the §5.3 trap
in a different costume. Re-seed with `-reset` between runs (it truncates
`idempotency_records`), or change `-workload`.

Seeding first is what prevents this: the clean-start assertion counts idempotency records as
well as claims, so a contaminated fixture fails at the seed rather than producing a run that
has to be diagnosed afterwards. You reach this entry by loading against a fixture nobody
re-seeded — which is why step 1 is not optional even when the database "looks" empty.

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

**`listen tcp :8080: bind: address already in use`** (and the same for `:9090`) — an earlier
service is still running. This is the common way to meet it, because §3 tells you to restart
the service between runs: the new one is started before the old one is gone, and `make dev`
reports it only at the last step, after `db-up` has already replaced the container.

Note what that ordering costs. `db-up` removes and recreates the container before `run`
fails, so the old process is now holding the ports *and* pointing at a database that no
longer exists. It has to go regardless of the port conflict — and do not let `/healthz`
reassure you: it is a liveness check that touches nothing, so it answers `200` from a
process whose pool is pointing at a deleted container. `/readyz` is the one that consults
the database.

```sh
ss -ltnp | grep -E ':8080|:9090'   # names the pid holding each port
kill <pid>                          # then: make run — db-up and migrate already ran
```

A service started with `&` or `nohup` outlives the terminal that launched it and is
reparented to `init`, so it will not appear in the job list of the shell you are typing in
and closing that terminal does not stop it. `ss` is the reliable way to find it; the
process's parent being `1` is the sign it was detached rather than started by `make dev`.

**`run was interrupted: N of M logical iterations completed`** — the run caught SIGINT or
SIGTERM and stopped early. The report is still written, and still describes what it did; it
simply may not be quoted, because its duration covers a smaller experiment than its manifest
claims and every rate derived from it would describe a run that never happened. Note the
count is *logical iterations*, not requests: a `-confirm` workload issues two requests per
iteration, so the request total cannot show the shortfall on its own.

**Nothing at `:9090`** — `METRICS_ADDR` overrides the listener address; the service logs
where it bound at startup.

## 8. What a local run is not

It is a smoke run. The generator and the service share a machine, so the generator is
competing with the thing it measures — `run.json` reports the generator's own CPU per core
precisely so you can see that (0.02 in the PR1 artifact: nowhere near saturation, so that
run was not client-limited).

It establishes **no capacity**, and no number from it may be quoted as one. PR2 measures the
one-instance frontier, but with the generator still on this machine, so its result is a
*bounded local* one: the generator-headroom control limits how far the co-resident generator
can be distorting it. A publishable capacity claim needs the generator on separate compute
([`ag-sept-plan.md`](../planning/ag-sept-plan.md) §6.3), which arrives with PR4 and only if
its gate passes (§14).

Two consequences for anything you keep:

- The manifest's operator-supplied fields — `replica_count`, `deployment_topology`,
  `environment`, `postgres_version`, pool sizes, timeout budget — are declared in the
  report but **not populated by any flag** in PR1, so they come out zero or empty. A local
  run is therefore self-describing about the generator and the workload, but not about the
  service's shape. That gap has to close before a run backs a published number (§6.4).
- Committed artifacts live in [`../measurements/`](../measurements/); the PR1 smoke run is
  [`pr1-smoke-run/`](../measurements/pr1-smoke-run/). Quote figures from an artifact, never
  from a terminal (`measurement-contract` §5.3).

`test/results/` is git-ignored scratch, and that is the whole distinction: a run only
becomes evidence by being copied into `../measurements/` on purpose. Nothing is lost by
deleting the directory, and nothing in it is quotable while it sits there.
