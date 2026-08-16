# Running the load harness locally

How to drive one AG-Sept run on your own machine, and how to tell whether the run it
produced may be quoted.

**Two procedures live here.** They share the generator, the manifest and the quotability
ladder, and they measure different things:

| Procedure | Sections | Drives | What its numbers are |
|---|---|---|---|
| the **PR1 single-service run** | §1–§8 | one service and one database on this host | a reproducible observation of this machine; establishes no capacity |
| the **PR4a Iteration C rehearsal cell** | §9–§17 | 1, 2 or 4 shard groups pinned to disjoint CPU sets, with monitoring | rehearsal/diagnostic only; never a capacity, scale-efficiency or Tier-2 result |

§9 onwards assumes §1–§8 rather than repeating it: what the manifest records, what each
quotability level means (§4), and why both the service and the generator are built rather
than `go run` (§3) are the same facts in both procedures.

**This document owns the procedure, not the rules.** What a run must contain and when a
number may be quoted are owned by
[`../design/measurement-contract.md`](../design/measurement-contract.md); which controlled
workloads exist and what each proves is owned by
[`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md);
what PR1 built against them is recorded in
[`ag-sept-pr1.md`](../development/implementation/ag-sept-pr1.md), and what PR4a built in
[`ag-sept-pr4.md`](../development/implementation/ag-sept-pr4.md). Where those disagree with
this page, they win.

## 1. The four binaries

The harness is deliberately split, because the split is what makes a run publishable
(`measurement-contract.md` §13.1): the generator speaks only HTTP and holds **no database
credentials**, so it can later move to separate compute without changing anything.

| Binary | Role | Database access |
|---|---|---|
| `cmd/alloca-go` | the service under test; also serves `/metrics` on its own port | yes |
| `cmd/alloca-seed` | builds the fixture and asserts the validation plan §3.3 clean start | yes |
| `cmd/alloca-load` | the external generator; writes the run report | **no** |
| `cmd/alloca-verify` | reconciles the report against the metrics scrape and persisted state (measurement-contract §12) | yes |

## 2. Prerequisites

Docker and Go. Nothing else — no Prometheus, no Grafana; the scrape is read with `curl`.

## 3. One run, start to finish

Two terminals. The first holds the service; the rest is the run.

```sh
# terminal 1 — database, schema, service
make dev-measured
```

`make dev-measured` starts PostgreSQL, migrates it, **builds** the service, and serves it on
`:8080` with the metrics listener on `:9090`. Wait for `{"msg":"metrics listener starting"}`
before continuing.

**Not `make dev`.** That one serves via `go run`, which stamps no VCS data, so `/meta` reports
no revision — and the generator reads the identity of the code under test from `/meta`. A run
against a `go run` service cannot say which binary answered it, and is refused at level
`none`. `make dev` remains the right target for ordinary development, where nothing records
provenance.

```sh
# terminal 2
export DATABASE_URL='postgres://alloca:alloca@localhost:15432/alloca?sslmode=disable'
mkdir -p test/results/manual   # git-ignored, and absent on a fresh clone

# 0. build the generator — see "Why both the service and the generator are built" below
go build -o bin/alloca-load ./cmd/alloca-load

# 1. fixture + clean-start assertion
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5

# 2. the run itself
./bin/alloca-load -workload dispersed -concurrency 8 -n 60 -slots 20 -out test/results/manual/run.json

# 3. capture the server's own count, before anything else touches the service
curl -s http://localhost:9090/metrics > test/results/manual/metrics.txt

# 4. reconcile client, server and persisted totals
go run ./cmd/alloca-verify -run test/results/manual/run.json -metrics test/results/manual/metrics.txt \
  -org load-org -out test/results/manual/verdict.json
```

Each step exits non-zero when its result is not quotable, so `&&`-chaining them is safe:
a broken run stops the pipeline instead of handing you numbers from it.

`-metrics` is what makes the verdict a three-way agreement rather than a two-way one. Without
it the verifier still runs, but the client/server check fails and the run is **not quotable**
— deliberately, because measurement-contract §12 requires client totals, server totals and persisted state to
reconcile, and a gate that silently certified two of the three would be the weaker gate
wearing the stronger gate's name. The scrape is a file rather than a URL the verifier fetches
so that the numbers being reconciled are the ones taken at the end of the run, not whatever
the service reports whenever the verifier happens to run.

**Restart the service before *every* run, not just the second one.** `-reset` returns the
database to a clean fixture but cannot touch the server's in-process counters, and those
accumulate from process start — see §4. `make db-down` is not part of this: `db-up` already
removes any existing container before starting one.

The trap is not only a previous load run. **Anything** the service answered since it started
counts, and the natural thing to do after starting the service is the natural thing that
breaks this:

```sh
make dev-measured   # terminal 1
make smoke          # ← 13 requests, and the scrape will carry all of them
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

### Why both the service and the generator are built, not `go run`

**`go run` does not stamp VCS information into the binary.** `go build` does. Two different
fields depend on that stamp, and they are not the same field:

| Manifest field | Comes from | Answers |
|---|---|---|
| `service_commit_sha` | the service's `/meta` | **which code was measured** |
| `generator_commit_sha` | the generator's own build | which harness produced the numbers |

`service_commit_sha` is the one measurement-contract §11 means by "commit SHA". The generator reads it from
`/meta` over the same HTTP-only boundary it already uses, so the §13.1 separation is untouched
— it asks the service to describe itself rather than sharing state with it.

**Why they are separate.** A service left running from one commit while the harness is rebuilt
from another is the ordinary state of a working session:

```sh
make dev-measured                              # service at commit A
# ... edit, rebuild the generator ...
go build -o bin/alloca-load ./cmd/alloca-load  # generator at commit B
./bin/alloca-load ...                          # measures A, not B
```

An earlier version recorded the generator's revision as the identity of the code under test,
which in that sequence names commit B for a run that measured commit A. That is worse than an
empty field: the provenance is populated, clean-looking and wrong.

So build both, and rebuild each after changing it:

```sh
go build -o bin/alloca-load ./cmd/alloca-load   # generator
make dev-measured                               # service (builds it for you)
```

The same applies to a **dirty working tree**, on either side. The manifest records
`service_source_modified` and `generator_source_modified`, and a true value fails the gate: a
SHA that does not describe the binary that ran is worse provenance than no SHA at all, because
nothing about it looks wrong. Commit or stash before a run whose numbers you intend to keep.

`alloca-seed` and `alloca-verify` stay on `go run` — neither writes a manifest.

### What else `/meta` supplies

The same fetch populates the service-side fields the service already knows about itself, so
nobody has to retype them — a transcription error there is indistinguishable from a
measurement:

| Manifest field | `/meta` |
|---|---|
| `service_go_version` | `go_version` |
| `server_gomaxprocs` | `gomaxprocs` |
| `timeout_budget` | `request_budget` (rendered as sorted `key=value` pairs) |
| `reservation_ttl` | `reservation_ttl` |

`postgres_version`, `pool_size_per_replica` and `aggregate_pool_size` are *not* on `/meta` —
they are facts about the deployment that the service does not know — so those stay
operator-supplied: topology arrived with PR3b, and environment arrives with whatever scaling
work the next iteration selects.

If `/meta` cannot be read at all, the run still executes and still writes its report; it is
refused at level `none` with the reason naming the missing service identity. A run that cannot
say what it measured is exactly what the level exists to catch.

### The other two workloads

Same four steps; only step 2 changes. Each one needs its own seed — `-reset` before every
run, and between workloads as much as between repeats of one (§7).

```sh
# hot-slot — many identities, one slot
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
./bin/alloca-load -workload hot-slot -concurrency 8 -n 60 -slots 20 -slot slot-0 \
  -out test/results/manual/hot-slot.json
curl -s http://localhost:9090/metrics > test/results/manual/hot-slot-metrics.txt
go run ./cmd/alloca-verify -run test/results/manual/hot-slot.json \
  -metrics test/results/manual/hot-slot-metrics.txt -org load-org
```

```sh
# hot-identity — one identity, many slots
go run ./cmd/alloca-seed -reset -slots 20 -capacity 5
./bin/alloca-load -workload hot-identity -concurrency 8 -n 60 -slots 20 -user user-0 \
  -out test/results/manual/hot-identity.json
curl -s http://localhost:9090/metrics > test/results/manual/hot-identity-metrics.txt
go run ./cmd/alloca-verify -run test/results/manual/hot-identity.json \
  -metrics test/results/manual/hot-identity-metrics.txt -org load-org
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
jq -r '.checks[] | "\(.ok)\t\(.name)\t\(.detail)"' test/results/manual/verdict.json

# client — written by step 2
jq '.summary.completed_requests, .summary.successful_mutation_goodput' test/results/manual/run.json

# server — read the replay="false" series only; never add the replay="true" one to it
# nothing resets these counters, so a scrape taken across two runs reads 120, not 60
grep '^alloca_requests_total.*replay="false"' test/results/manual/metrics.txt
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
on what it is used *for*: a capacity result needs topology provenance, and an externally
presented claim additionally needs the generator off the service host
(`measurement-contract.md` §13.1). An unqualified boolean cannot express that, and the one this
replaced invited a co-resident smoke run to be read as publishable.

Both binaries record a level instead. The rules are `measurement-contract.md` §13.2; this is the
operator's view of them:

| Level | Means | Reached when |
|---|---|---|
| `none` | The run measured nothing usable | validation off or failed, run interrupted, warm-up rows unreconciled, reconciliation failed, or the units did not describe one deployment |
| `local` | A reproducible observation of this machine and run | both revisions recorded from clean trees, everything `/meta` reports about the service — including PostgreSQL version and pool size per replica — plus generator resources, workload/run shape, target and timestamp |
| `capacity` | May back a capacity result about the recorded topology | + the facts no endpoint reports: aggregate pool size, replica count, deployment topology, environment, routing version — plus placement for a multi-unit run and an image ID for a containerised one |
| `publishable` | Carries the provenance an externally presented claim needs | + `-generator-location` names a host separate from the service |

**A co-resident generator only blocks `publishable`.** It is not what holds a run at `local`; a
run on this workstation reaches `capacity` as soon as it records the deployment facts above.

```sh
jq -r '.quotability | "\(.level)\t\(.blocked_because)"' test/results/manual/run.json
```

**Early AG-Sept runs reach `local`, and that is the correct outcome, not a defect.** The fields
above `local` are the operator's account of the deployment, supplied by whichever work unit first
has something to say about them — placement, authority identity, topology and image identity
arrived with PR3b; replica count, aggregate pool capacity and environment are still to come. That
sequencing is scheduling (`ag-sept-plan.md` §4), not a property of this harness. The report says
so itself in `blocked_because`, naming each missing field, so an incomplete manifest reads as
scheduled rather than broken.

Both binaries take `-require` to set the bar, defaulting to `local`:

```sh
./bin/alloca-load -require local ...        # fails on missing service or generator provenance
./bin/alloca-load -require capacity ...     # additionally fails missing topology/environment
./bin/alloca-load -require publishable ...  # additionally fails a co-resident generator
```

The bar is declared at the call site because only the caller knows what the number is for. A
run below its `-require` level exits non-zero with the reason, and still writes its report.

One thing `publishable` does *not* mean: that the run is ready to publish. It checks the
provenance `measurement-contract.md` §13.1 requires, which is a declaration in the manifest.
**The whole ladder is provenance, not evidence** — the VAL-NEG-2 generator-headroom control and
the rest of measurement-contract §5 are what make the experiment admissible, and a run can sit at
the top of this ladder and still be inadmissible because its experiment was not controlled.

`alloca-verify` prints five checks — one per measurement-contract §12 database rule (INV-1, INV-5, INV-4,
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
grep '^alloca_requests_total' test/results/manual/metrics.txt
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

Mandatory and not descopable (`measurement-contract.md` §5 item 5; VAL-NEG-1): the harness must **fail**
when response validation is off, so a reported success cannot be an unchecked `200`.

```sh
./bin/alloca-load -workload dispersed -concurrency 8 -n 20 -slots 20 \
  -validate=false -out test/results/manual/control.json
curl -s http://localhost:9090/metrics > test/results/manual/control-metrics.txt
go run ./cmd/alloca-verify -run test/results/manual/control.json \
  -metrics test/results/manual/control-metrics.txt -org load-org
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
jq '.summary.totals' test/results/manual/run.json     # "replay": true on everything means nothing committed
```

Idempotency keys are `workload-seq-step` with no per-run nonce (`internal/loadgen/workload.go:39`),
so re-running the same `-workload` re-sends the keys the previous run already used and the
server correctly replays each recorded outcome instead of committing. The run then passes
reconciliation — no invariant is broken — while measuring nothing, which is the validation plan §3.3 trap
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
([`../design/measurement-contract.md`](../design/measurement-contract.md) §13.1). That compute is
no longer funded in AG-Sept — the deployment path that would have provided it is withdrawn
(scheduling: [`../planning/ag-sept-plan.md`](../planning/ag-sept-plan.md) §6.3) — so **no AG-Sept
run can reach `publishable`**, and the rule is honoured by labelling. Co-residency blocks that
level only: a run here still reaches `capacity` once it records the deployment provenance §13.2
asks for.

Two consequences for anything you keep:

- The manifest's operator-supplied fields — `replica_count`, `deployment_topology`,
  `environment`, `postgres_version`, pool sizes, timeout budget — are declared in the
  report but **not populated by any flag** in PR1, so they come out zero or empty. A local
  run is therefore self-describing about the generator and the workload, but not about the
  service's shape. That gap has to close before a run backs a published number (measurement-contract §11).
- Committed artifacts live in [`../measurements/`](../measurements/); the PR1 smoke run is
  [`pr1-smoke-run/`](../measurements/pr1-smoke-run/). Quote figures from an artifact, never
  from a terminal (`measurement-contract.md` §5 item 3).

`test/results/` is git-ignored scratch, and that is the whole distinction: a run only
becomes evidence by being copied into `../measurements/` on purpose. Nothing is lost by
deleting the directory, and nothing in it is quotable while it sits there.

## 9. The PR4a Iteration C rehearsal cell

Sections 1–8 drive one service on this host. This one drives the **Iteration C topology** —
one, two or four shard groups, each a service unit with its own PostgreSQL authority — with
every group pinned to CPUs no other group can touch, and the generator and monitoring stack
confined to CPUs of their own.

The containers underneath it are [`container-topology.md`](container-topology.md); the panels
it exports are [`dashboards.md`](dashboards.md); the decisions and the findings are
[`ag-sept-pr4.md`](../development/implementation/ag-sept-pr4.md); the 1/2/4 matrix and the
result model are
[`ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md) §4.6. This
section is only how to drive one.

**Read the evidence class before the recipe.** Every group shares one workstation, one WSL
kernel, one storage path and one page cache, so the partition bounds CPU and nothing else. A
cell's Goodput is rehearsal/diagnostic evidence: it cannot discharge `VAL-SCALE-5`, cannot
become a Tier-2 operating-point result, and is never mixed with AWS points to derive `E2` or
`E4` (`ag-sept-pr4.md` §2.14). What a rehearsal *can* establish is that the machinery —
placement, fixture, declaration, deployment record, provenance, cpuset partition, scrape
coverage, certification and retention — runs end to end before any of it is exercised on
metered infrastructure.

**A cell certifying at `capacity` has not measured capacity.** The level is a statement about
provenance: the declaration, the observed deployment and the per-unit `/meta` all line up.
Every cell in [`../measurements/pr4a-rehearsal/`](../measurements/pr4a-rehearsal/) reads
`capacity` and none of them backs a capacity number — each manifest's own `environment` string
says so. §4's ladder is unchanged here; what §9 adds is the reminder that provenance and
evidence are different gates, and only the first one is automated.

### The partition

```text
ITC_CPUS_A           capacity unit A     CPUs 0-1     alloca-service-1 + authority-1 PostgreSQL
ITC_CPUS_B           capacity unit B     CPUs 2-3     alloca-service-2 + authority-2 PostgreSQL
ITC_CPUS_C           capacity unit C     CPUs 4-5     alloca-service-3 + authority-3 PostgreSQL
ITC_CPUS_D           capacity unit D     CPUs 6-7     alloca-service-4 + authority-4 PostgreSQL
ITC_CPUS_GENERATOR   generator/monitor   CPUs 8-11    alloca-load, Prometheus, Grafana,
                                                      node_exporter
—                    (headroom)          CPUs 12-15   idle; the only spare capacity the
                                                      generator control widens into
```

`make itc-layout ITC_GROUPS=n` prints the sets this machine will actually be partitioned into,
and prints only the units the rung uses.

`G1` uses unit A, `G2` uses A+B, `G4` uses all four; unused sets stay idle rather than being
borrowed. A service and its authority deliberately share one group's two CPUs, which is the
contention shape the planned EC2 capacity unit has.

**`ITC_CPUS_GENERATOR` names the whole measuring side, not just the generator.** Prometheus
scrapes every unit on a one-second interval and compacts its TSDB; unpinned it does that from
inside the capacity units' own CPUs, and it does it harder at `G4` than at `G1` — against
exactly the comparison `E2` and `E4` come from.

### What raises what

| Command | Raises | Pinned by |
|---|---|---|
| `make itc-rehearse ITC_GROUPS=n` | the image, `n` service units, `n` PostgreSQL authorities, `n` one-shot migrations | `docker-compose.rehearsal.yml` cpusets |
| `make obs-rehearse ITC_GROUPS=n` | Prometheus, Grafana, `node_exporter`, and the file_sd target list for exactly those `n` units | the observability rehearsal overlay |
| `./test/scripts/itc-run.sh` | nothing — it drives one cell against what is already up | `taskset` on the generator |

`make itc-up` and `make obs-up` raise the same containers **unpinned**. That is correct for
ordinary local work and wrong for a cell: the run addresses the right units, every routing and
provenance check passes, and the numbers carry contention no artifact records. §12's cpuset
check is what stops that reaching a report.

## 10. Prerequisites for a cell

Docker, Go, `python3`, `curl`, and `taskset` (util-linux). `jq` for reading the result.

**Monitoring is not optional here, unlike in §3.** `itc-run.sh` refuses a cell it cannot prove
was observed, because a Prometheus scraping nothing returns empty results and no error — the
first driven `G4` cell completed, reconciled and certified while retaining no time series at
all (`ag-sept-pr4.md` §3.9). Driving without monitoring is allowed, but only by saying
`PROM_URL=` out loud (§13).

A machine with at least 12 logical CPUs for the committed partition, and 16 for the
generator-headroom control, which widens the measuring side onto `8-15`. Separately, the
machine's logical CPU count has to match what the declaration document claims (§12): the layout
check accepts a partition rescaled to a smaller host, and the declaration is what stops a
rescaled run certifying with a false description of the machine.

## 11. One cell, start to finish

```sh
# 0. provenance first: one uncommitted file makes every run certify at `none`
git status --porcelain          # expect no output at all, untracked files included
make image-provenance           # builds, extracts the binary, asserts modified=false

# 1. check the partition against this machine, before anything is built or raised
make itc-layout ITC_GROUPS=4

# 2. raise the pinned topology (builds the image, then polls /readyz on every unit)
make itc-rehearse ITC_GROUPS=4

# 3. raise the pinned monitoring stack — after the topology, never before
make obs-rehearse ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-11

# 4. record what the containers are actually serving
make itc-deployment ITC_GROUPS=4 > test/observed/deployment.json

# 5. build the generator — `go run` stamps no VCS data (§3)
go build -o bin/alloca-load ./cmd/alloca-load

# 6. drive one cell: preflight, reseed, baseline scrape, window, after scrape, export, report
ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-11 ./test/scripts/itc-run.sh
```

Step 1 is cheap and catches the expensive mistake: Docker refuses an out-of-range cpuset on
its own, but only after it has built an image and started four database containers, and its
message names neither the partition nor the machine.

**Step 3 must follow step 2.** Prometheus joins the topology's Compose network
(`alloca-topology_default`) and scrapes the units by their Compose service names, so the
network has to exist first. That is what makes the scrape path structural rather than
discovered — a WSL restart reassigns host addresses and cannot invalidate a service name.
Grafana is deliberately not attached to that network: it talks to Prometheus.

**`ITC_CPUS_GENERATOR` appears twice and the two must agree.** Monitoring and the generator
are one measuring side, and moving only half of it changes two things at once. `make
itc-rehearse` prints the remaining commands with the value it was given, which is the copy to
take.

**Re-record step 4 after anything that recreates a container.** The record names each unit's
image ID and published address, and `alloca-load` refuses a run whose routed units are not
exactly the recorded ones — before any measured request.

**There is no seed step, and that is deliberate.** `itc-run.sh` reseeds immediately before
every measured window and fails the cell if the seed fails, because "reseed between rungs" is
the step a twelve-cell ladder drops once, silently, after which every later point is wrong
(`ag-sept-pr4.md` §3.10). `make itc-rehearse` prints `itc-seed.sh` among its next steps;
running it by hand is only useful for inspecting a fixture before driving anything, since the
measured cell reseeds regardless.

Everything from the reseed to the export is one script because the ordering is load-bearing:
the baseline scrape must precede the window, the after scrape must follow the generator's
**exit** rather than the end of the workload — `alloca-load` replays ambiguous mutations in a
post-run pass — and the panel export must be bounded to the measured phase.

### Driving `G1` or `G2` instead

Change `ITC_GROUPS` in every command of §11, including the deployment record. One value
selects the Compose profile, the placement document, the declaration, the scrape target list,
the recorded container set and the seeded authorities together, and that is the pairing the
targets exist to make impossible to get wrong.

The variable is `ITC_GROUPS`, never `GROUPS`: `GROUPS` is a bash built-in array of the
caller's group IDs, and bash discards an assignment to it without error, so the script would
receive your GID and Make would be unaffected.

## 12. What the preflight refuses, and what each check is for

Every check below runs **before the fixture is touched**, except the last two, which run after
the window. Each exists because the failure it catches is otherwise invisible downstream:
the cell completes, certifies, and produces a number nobody can tell is wrong.

| Check | Refuses when | What it costs when it is missing |
|---|---|---|
| `taskset` present | util-linux is not installed | the generator runs on the units' CPUs and dissolves the partition |
| generator built | `bin/alloca-load` is absent or not executable | — |
| placement, deployment record, declaration all present | any of the three is missing | a late refusal, after the window has been driven |
| declared CPU count vs `nproc` | `declaration-itc-g<n>.json` names a different machine | a rescaled partition certifies at `capacity` carrying a false environment string, in the one field a reader uses to judge whether the numbers transfer |
| clean working tree | `git status --porcelain` is non-empty | Go stamps *untracked* files as a modified tree, so the run certifies at `none` and backs nothing |
| CPU layout legal | sets overlap, units are unequal, the generator is no larger than a unit, the partition exceeds the machine, or a spec is malformed | an efficiency figure that carries an imbalance in the partition rather than the architecture |
| running topology is exactly `G<n>` | units from a larger rung are still up, or a unit serves another topology's routing version | the stale units consume the envelope this one is measured in |
| cpusets applied | any container's cpuset disagrees with the partition, or cannot be read | the stack was raised with `itc-up`/`obs-up`; the numbers carry unrecorded contention |
| scraped set is exactly this rung's authorities | a unit is missing, or a foreign target is healthy in the job | a missing unit retains no series and reports no error; a foreign target implies an unpinned process free to contend |
| panel export succeeded | Prometheus or the snapshot call failed | the cell keeps its scalars and loses its shape, and this workload's rate is not flat within a window |
| host panels non-empty | `node_exporter` retained no samples | the cell cannot separate a stall in the service from one in the machine under it — the `VAL-NEG-7` evidence and the degraded-regime diagnosis both |

Two things the table cannot carry.

**The scraped-set check compares identities *and* count, because each half is blind to what
the other catches.** A count alone is satisfied by the wrong set — a `G4` rehearsal missing
`authority-3` while carrying a stray healthy target still counts four. A set comparison
narrowed to this rung's own label cannot see anything outside the rung, which is exactly where
contamination lives. So the query is unfiltered and the refusal names any `FOREIGN` target it
found. What makes a stray target worth refusing over is not the extra scrape, which is
trivial: a host-run service answering on the metrics port is an unpinned process in the same
WSL environment, free to contend while every cpuset and topology check passes.

**Fixture exhaustion is reported, not refused.** After the run, the script compares admitted
mutations against `SLOTS × CAPACITY × 4` and says plainly when the cell consumed its whole
supply. It reports rather than gating because useful demand is an evidence gate rather than a
provenance one (`measurement-contract.md` §5): an exhausted cell is still a legitimate,
self-describing artifact — it simply measured how fast the service can decline. Remember that
an exhausted rung also invalidates the rung *below* it, because a saturation argument rests on
the higher rung having been short of service rather than short of fixture.

## 13. The knobs

All of these are environment variables read by
[`../../test/scripts/itc-run.sh`](../../test/scripts/itc-run.sh):

| Variable | Default | Changes |
|---|---|---|
| `ITC_GROUPS` | `4` | the rung: 1, 2 or 4 shard groups |
| `WORKLOAD` | `wl-mut-disp-4` | the workload; `WL-MUT-DISP-4` is the one topologies are compared with |
| `CONCURRENCY` | `16` | closed-loop workers |
| `WINDOW` | `60s` | the measured window |
| `SLOTS`, `CAPACITY` | `3200`, `20` | the per-organisation fixture, and so the fresh-mutation supply |
| `REQUIRE` | `capacity` | the level below which the run exits non-zero |
| `PROM_URL`, `PROM_JOB` | `http://localhost:9091`, `alloca-go` | where the scrape gate looks |
| `RESULTS_GROUP`, `OUT` | `pr4a`, a timestamped directory under it | where the cell lands |
| `PLACEMENT`, `DEPLOYMENT`, `DECLARATION` | derived from `ITC_GROUPS` | the three provenance inputs |
| `ITC_CPUS_A`…`ITC_CPUS_D`, `ITC_CPUS_GENERATOR` | `0-1`…`6-7`, `8-11` | the partition |
| `SERVICE_n_PORT`, `SERVICE_n_METRICS_PORT` | `808n`, `908n` | where the cell addresses and scrapes each unit |

**The fixture supply is `SLOTS × CAPACITY × 4`** — 256,000 fresh mutations at the defaults,
and the cell prints admitted against it. `3200` is an interim value sized to one `c=16` cell,
**not** PR4b's fixture size: that has to be derived from the deepest rung its ladder reaches
and then held identical across `G1`, `G2` and `G4`.

**Concurrency 16 is exactly `aggregate_pool_size` at `G4`** — four connections per unit — so
16 workers can each hold a connection and the pool is precisely not a constraint. A ladder has
to cross that boundary deliberately rather than discover it; `c=32` is the first rung past it.

**`capacity` is the ceiling locally, not a conservative default.** The generator is co-resident
with the units it drives, and co-residency blocks `publishable` outright however clean
everything else is (§4), so requiring it would refuse every rehearsal cell for a reason the
rehearsal cannot fix.

**There is no warm-up flag, deliberately.** `-warm-up` drops responses from the client totals
while their rows stay in the database, which persisted-state reconciliation cannot square, so
it refuses the run at `none` (§5). Warming is a separate invocation followed by a reseed.

### The generator-headroom control

The cpuset analogue of `VAL-NEG-2`: rerun a cell with the measuring side given twice the CPUs
the partition holds idle.

```sh
make obs-rehearse    ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-15
ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-15 ./test/scripts/itc-run.sh
```

If Goodput does not move, the generator was not the binding constraint at that operating point
and the units' number stands; if it tracks the generator's size, the cell was measuring the
harness. **Widen both commands or neither** — moving only the generator changes two things and
the comparison carries the second one. Take the control against a stable, high-useful-demand
point: against a cell whose generator sat at 7.7% of a core it could prove nothing.

### Driving without monitoring

```sh
PROM_URL= ITC_GROUPS=4 ./test/scripts/itc-run.sh
```

Legitimate while shaking out the harness. The cell keeps its totals and its scrape pairs and
retains no series, so nothing quoted from it can describe a shape. What must never happen is a
run that believes it was observed when it was not, which is why this is a spoken argument
rather than the consequence of Prometheus being quietly absent.

## 14. What a cell leaves behind, and how to read it

```text
test/results/pr4a/itc-g4-<timestamp>/
```

| File | Holds |
|---|---|
| `run.json` | the client's totals, the full manifest and the `quotability` verdict |
| `generator-output.txt` | what the generator printed, including any refusal |
| `seed.txt` | the reseed transcript, per organisation and authority |
| `fixture.txt` | `SLOTS`, `CAPACITY` and the derived fresh-mutation supply |
| `cpu-partition.txt` | the partition this cell was *asked* for, plus `nproc` |
| `observed-cpusets.txt` | the cpusets read off the running containers — the applied partition, which is a different fact |
| `s<n>-baseline.prom`, `s<n>-after.prom` | the scrapes bracketing the window, per unit; counters are cumulative, so the delta is the measurement |
| `panels/*.csv` + `panels/index.json` | the exported series with the resolved PromQL, rate range, step and both windows recorded beside the data |
| `tsdb-snapshot/` | the Prometheus snapshot, so a series nobody thought to export is still recoverable |

The two partition files are separate on purpose: a declared partition and an applied one are
different facts, and the declaration deliberately does not name the generator's CPUs, since the
headroom control widens them and a static string would be false for half the runs it describes.

```sh
CELL=test/results/pr4a/itc-g4-<timestamp>

# what the run may back, and what is holding it there
jq -r '.quotability | "\(.level)  blocked_from=\(.blocked_from)\n\(.blocked_because)"' $CELL/run.json

# the headline scalars
jq -r '.summary | "\(.completed_requests) completed, \(.successful_mutation_goodput) goodput, \(.duration_seconds)s"' $CELL/run.json
jq -c '.summary.latency_ms, .summary.generator' $CELL/run.json

# the outcome mix — a refusal is a recorded outcome, not a failure
jq -r '.summary.totals[] | "\(.count)\t\(.operation)\t\(.outcome)\treplay=\(.replay)"' $CELL/run.json
```

**The agreement is stronger evidence than the rate.** Difference each unit's scrape pair and
sum the four: that server-side total is an accounting independent of the client's, and a
correct placement must also leave the units balanced to the request.

```sh
admitted() {  # admitted() <file> — fresh admissions in one scrape
  awk '/^alloca_requests_total\{.*outcome="admitted_success".*replay="false"/ {s+=$NF} END {print s+0}' "$1"
}
for n in 1 2 3 4; do
  echo "authority-$n  $(( $(admitted $CELL/s$n-after.prom) - $(admitted $CELL/s$n-baseline.prom) ))"
done
```

In the retained `c=16` cell
([`../measurements/pr4a-rehearsal/points/c16/`](../measurements/pr4a-rehearsal/points/c16/))
that prints 27,543 four times, summing exactly to the client's 110,172.

**A single reported rate does not describe the window**, which is what the panel export is for.
Two retained cells identical but for window length, each read from its own
`panels/throughput.csv`: the 30 s cell **rose** from 2,875 to 3,208 req/s across its four
exported points, and the 60 s cell **fell** from 2,194 to 927 across its ten
([`../measurements/pr4a-rehearsal/windows/`](../measurements/pr4a-rehearsal/windows/)). Each
average is a figure across a slope, and the two landed in materially different regimes — the
open finding `ag-sept-pr4.md` §3.12 records, and the reason no rung comparison is possible
until the regimes are separable. Read the shape, not the mean:

```sh
jq -r '.rate_range, .step, (.window|tojson)' $CELL/panels/index.json
jq -r '.panels[] | "\(.points)\t\(.series)\t\(.key)"' $CELL/panels/index.json   # 0 points = retained nothing
column -s, -t $CELL/panels/throughput.csv | head
```

`panels/index.json` records two windows and they are not the same: `window` is the measured
phase and is the authority for what was measured, while `query_window` is what the range
queries actually cover — one rate range later, so no exported point can reach back into the
samples before the window opened. Quote against `window`, and bound any query of the snapshot
by it, because the snapshot is cumulative Prometheus history and carries other cells' windows
too.

Which panel answers which question, and which readings look sound and are not, is
[`dashboards.md`](dashboards.md).

## 15. Reconciliation is a separate step, and is not wired

`itc-run.sh` does not run `alloca-verify`, and no retained rehearsal cell carries a
`verdict.json` — a gap against both `measurement-contract.md` §12, which requires client,
server and persisted totals to reconcile, and `ag-sept-pr4.md` §2.10, which makes the
multi-authority verification path PR4a's work rather than PR4b's discovery. A cell's
`measurement_sound` covers the client/server accounting the generator itself can see; it is
not the database-side reconciliation.

The cell already writes every input the verifier needs, so the step is a command rather than a
change:

```sh
go build -o bin/alloca-verify ./cmd/alloca-verify

dsn() { echo "postgres://alloca:alloca@localhost:$1/alloca?sslmode=disable"; }
./bin/alloca-verify \
  -run $CELL/run.json \
  -placement deploy/topology/placement-itc-g4.json \
  -authority-db "authority-1=$(dsn 15433)" -authority-db "authority-2=$(dsn 15434)" \
  -authority-db "authority-3=$(dsn 15435)" -authority-db "authority-4=$(dsn 15436)" \
  -authority-metrics authority-1=$CELL/s1-after.prom \
  -authority-metrics authority-2=$CELL/s2-after.prom \
  -authority-metrics authority-3=$CELL/s3-after.prom \
  -authority-metrics authority-4=$CELL/s4-after.prom \
  -authority-metrics-baseline authority-1=$CELL/s1-baseline.prom \
  -authority-metrics-baseline authority-2=$CELL/s2-baseline.prom \
  -authority-metrics-baseline authority-3=$CELL/s3-baseline.prom \
  -authority-metrics-baseline authority-4=$CELL/s4-baseline.prom \
  -require capacity -out $CELL/verdict.json
```

**This command has not been executed against an Iteration C cell** (§17). It is the four-unit
form of the two-unit invocation `container-topology.md` §6.1 documents and
`pr3c-experiments.sh` runs, and the constraints described there apply unchanged: every unit's
scrape or none, verify promptly before unconfirmed holds expire, and `-placement` is mutually
exclusive with `-database-url`/`-org`.

Drop the unused `-authority-*` lines for `G1` and `G2`, and change the placement document with
the rung.

## 16. When a cell goes wrong

**`prometheus is up, but the scraped units are not exactly the ones this rung raises`** — the
refusal prints the expected and scraped sets. A `FOREIGN[...]` entry is a healthy target in the
job that does not belong to this rung, and the usual one is the PR2 host-run target; remove
`deploy/observability/targets/alloca-go.json` and raise the stack with `make obs-rehearse`,
which does not probe for it. A *missing* entry is the opposite failure, and the more dangerous
one: regenerate the list with `itc-obs-targets.sh` and give Prometheus its refresh interval.

**`the cell retained no host samples for: ...`** — `node_exporter` is not being scraped. It is
`VAL-NEG-7`'s host sensor and the instrument the degraded-regime diagnosis needs, so a cell
without it cannot separate a stall in the service from one in the machine under it.

```sh
curl -s http://localhost:9091/api/v1/targets | grep -A2 '"job":"node"'
```

**`working tree is not clean`** — including untracked files. `go build` derives `vcs.modified`
from `git status --porcelain`, so one scratch file stamps both binaries modified, and
`Manifest.Validate` refuses that at `local`, the floor of the ladder. The cheap symptom is a
`-dirty` suffix on the image tag.

**`<declaration> declares N logical CPUs; this machine exposes M`** — edit the declaration to
describe this machine, or drive the cell on the machine it describes. It is checked rather than
generated on purpose: generating it would make the declared document a second observed one, and
the split between what the operator asserts and what the harness observed is the point of it.

**`the containers are not pinned where the partition says`** — the stack was raised with `make
itc-up`/`make obs-up`. Raise it with `make itc-rehearse`/`make obs-rehearse`.

**`the running topology is not the selected G<n>`** — units from a larger rung are still up.
Raising a smaller topology does not stop a larger one's units: `make itc-down`, then raise the
rung you want. The same check refuses a unit serving another topology's routing version, which
is a unit still running against a previous placement document.

**`this cell exhausted its fixture`** — the cell measured refusal throughput after its supply
ran out, and it still certifies. Raise `SLOTS` and re-run before quoting anything, and discard
the rung below it too.

**A cell reports far less Goodput than a comparable one, with process CPU down as well** —
this is the open degraded regime (`ag-sept-pr4.md` §3.12), not an outlier to drop. Throughput
and CPU falling together is evidence that the service is doing less work while the request path
slows. Read the pool panels before concluding anything: a *saturated* pool reads as acquired
meeting total, while this regime holds connections idle with `total` flat at its ceiling and
mean acquire duration climbing — 0.56 ms to 7.67 ms across the retained 60 s cell
(`ag-sept-pr4.md` §3.13.1). Keep the cell, with its host panels and snapshot; a degraded cell
carrying both is what the diagnosis needs.

**`Bind for 0.0.0.0:9091 failed: port is already allocated`** — historical, and fixed by moving
the topology's metrics ports to `9081`–`9084` so unit *n* serves on `808n` and publishes
metrics on `908n`. If you meet it, something is pinned to the old numbers: a saved dashboard, a
shell history, or a `SERVICE_n_METRICS_PORT` override. Prometheus keeps `9091`.

**Ports fail to bind after a Windows reboot** — a reserved dynamic-port block; §7 has the
`netsh.exe` incantation. Every port in this topology is below 49152 for that reason.

## 17. What has been executed on this page

Written on 2026-08-16 at `e123035`, from the scripts and Makefile targets it documents.

| Step | Status |
|---|---|
| `make itc-layout` at `ITC_GROUPS=1`, `2` and `4` | **run** on a 16-CPU machine; each reports the partition §9 describes, listing only the units its rung uses |
| §11 steps 0 and 2–6 | **not run in this pass.** It is the sequence `make itc-rehearse` prints and the one that produced the five cells in [`../measurements/pr4a-rehearsal/`](../measurements/pr4a-rehearsal/) on 2026-08-13, at an earlier revision |
| §14's reading commands | **run against retained cells** — `points/c16/` and `windows/30s/` — not against a live cell directory. The per-unit recipe reproduces 27,543 × 4 = 110,172 from that cell's scrape pairs |
| §15's reconciliation command | **never executed against an Iteration C cell.** No retained cell carries a `verdict.json` |

Every figure quoted above is re-derivable from a retained artifact: the `c=16` per-unit
agreement (27,543 × 4 = 110,172) from that cell's four scrape pairs, and both window slopes
from the cells' own `panels/throughput.csv`. The within-window decay recorded in
`ag-sept-pr4.md` §3.11 is deliberately **not** quoted here — that cell predates per-cell series
retention, and the implementation record marks those figures as observations that cannot be
re-derived.

If you run something and it disagrees with this page, this page is wrong; fix it here.
