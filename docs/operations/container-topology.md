# Building and running the container topology

How to build the production-shaped image, raise the two-authority deployment on your own
machine, check it is actually serving what it claims, and tear it down again.

**This document owns the procedure, not the design.** Why the topology has two independent
writers, what a placement map is, and what Phase 1 supports are owned by
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md);
what PR3 builds against it is
[`../development/implementation/ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md). The per-line
reasoning for each container lives in the comments of
[`../../deploy/topology/docker-compose.yml`](../../deploy/topology/docker-compose.yml) and
[`../../Dockerfile`](../../Dockerfile), which are the authority on *why* each setting is
what it is. Where any of those disagree with this page, they win.

For the single-service measurement procedure — seeding, driving a run, reconciling it —
see [`load-harness.md`](load-harness.md). This page is about the containers underneath it.

## 0. The minimum path

Everything you need to raise the topology, prove it is serving what it claims, drive one run, and
tear it down. Nothing else on this page is required to get here — the rest is either reference or
an optional check, and each such section says which it is.

```sh
# 1. build the image                                                        (§3)
make image

# 2. raise the topology                                                     (§4)
make topo-up

# 3. copy the two addresses the line above printed                          (§4)
S1=localhost:8081
S2=localhost:8082

# 4. each unit is bound to the authority it claims                          (§5)
curl -sS $S1/meta | jq '{a: .placement.authority_id, orgs: .placement.organisations}'
curl -sS $S2/meta | jq '{a: .placement.authority_id, orgs: .placement.organisations}'

# 5. seed the fixture — -reset on the FIRST organisation of each authority  (§7)
seed() {
  go run ./cmd/alloca-seed ${3:+-reset} \
    -database-url "postgres://alloca:alloca@localhost:$1/alloca?sslmode=disable" \
    -org "$2" -slots 100
}
seed 15433 org-a reset
seed 15433 org-c
seed 15434 org-b reset
seed 15434 org-d

# 6. record what is running, then drive one bounded run                     (§7)
go build -o bin/alloca-load ./cmd/alloca-load
mkdir -p test/results
make topo-deployment > test/results/deployment.json
./bin/alloca-load \
  -placement deploy/topology/placement.json \
  -endpoint authority-1=http://$S1 -endpoint authority-2=http://$S2 \
  -workload multi-org-dispersed \
  -deployment test/results/deployment.json \
  -concurrency 32 -n 400 -slots 100 \
  -out test/results/topo-run.json

# 7. tear it down                                                           (§6)
make topo-down
```

Expect **400 `admitted_success`** and `measurement_sound: true`. If you get anything else, §8 is
the place to start.

Each step is explained in the section named beside it, and the explanations carry the traps —
particularly step 5, where `-reset` on the wrong call silently empties the previous
organisation's slots.

### What is optional

| Optional | When you want it | Where |
|---|---|---|
| the `docker` shim for WSL | `docker` is not found at all | §2 |
| `make image-provenance` | proving an image's revision and clean-tree stamp end to end | §3 |
| changing ports | 8081/8082 are already taken on your machine | §4 |
| the four routing behaviours | seeing placement, colocated booking, cross-authority refusal and misrouting behave differently — this is what the topology exists to demonstrate, so it is the first thing worth adding | §5 |
| failure isolation | seeing one authority fail without touching the other | §5 |

## 1. What is in the topology

Two independent PostgreSQL authorities, each with its own shard-affine service unit, and
one placement document mounted read-only into both. That is the smallest deployment in
which authority composition means anything: an organisation's rows live on exactly one
writer, a unit serves only the organisations its authority owns, and one authority failing
does not reach the other.

**Every port below is a default.** All of them are overridable (§4, *Changing ports*), so if you
changed one, read these as the shape rather than as your addresses — `make topo-up` prints the two
service addresses it actually published.

| Container | Role | Published on (default) |
|---|---|---|
| `alloca-authority-1-db` | PostgreSQL, authority 1 | `localhost:15433` |
| `alloca-authority-2-db` | PostgreSQL, authority 2 | `localhost:15434` |
| `alloca-authority-1-migrate` | one-shot schema migration, must exit 0 first | — |
| `alloca-authority-2-migrate` | one-shot schema migration, must exit 0 first | — |
| `alloca-service-1` | service unit bound to authority 1 | `localhost:8081`, metrics `9091` |
| `alloca-service-2` | service unit bound to authority 2 | `localhost:8082`, metrics `9092` |

The placement document ([`../../deploy/topology/placement.json`](../../deploy/topology/placement.json))
is routing version `pr3b-v1`:

| Organisation | Authority | Reached at (default) |
|---|---|---|
| `org-a`, `org-c` | `authority-1` | `localhost:8081` — `$S1` below |
| `org-b`, `org-d` | `authority-2` | `localhost:8082` — `$S2` below |

**Two organisations per authority is deliberate**, not padding. A pair on the *same*
authority is a supported cross-organisation booking (INV-13); a pair on *different*
authorities is the refusal Phase 1 exists to make explicit. A one-organisation-per-authority
map could not exercise the first case at all.

Migration is a separate one-shot container per authority rather than something the service
does at startup (ADR-0002). Each service waits for its own migration to *exit successfully*,
not merely to have started.

## 2. Prerequisites

Docker and Go. Nothing else — no Prometheus, no Grafana.

**Optional from here to the end of §2** — only if `docker` is not found at all.

On WSL2, `docker` is often missing at the start of a session:

```
The command 'docker' could not be found in this WSL 2 distro.
```

That message does not distinguish "Docker Desktop is not running" from "it is running but
WSL integration is off for this distro". Check which:

```sh
docker.exe ps
```

If that works, the daemon is up and only the WSL shim is missing. Either switch on WSL
integration in Docker Desktop settings, or put a one-line shim on `PATH`:

```sh
mkdir -p ~/bin
printf '#!/bin/sh\nexec "/mnt/c/Program Files/Docker/Docker/resources/bin/docker.exe" "$@"\n' > ~/bin/docker
chmod +x ~/bin/docker
PATH=~/bin:$PATH make topo-up
```

Ports published by Docker Desktop are reachable from WSL on `localhost`, so nothing else
needs changing. If `docker.exe ps` also fails, the daemon really is down.

## 3. Building the image

```sh
make image
```

Two stages. The builder carries the Go toolchain; the runtime is
`gcr.io/distroless/static-debian12:nonroot` — no shell, no package manager, no compiler.
Both `alloca-go` and `alloca-migrate` ship in the one image, so the migration that runs and
the binary that serves come from a single build.

The image is tagged twice: `alloca-go:dev`, and `alloca-go:<short-sha>` — with `-dirty`
appended when the working tree has uncommitted changes.

**`-dirty` is a warning, not an identifier.** Two different uncommitted trees produce the
same `abc1234-dirty` tag and the second build silently replaces the first, so two runs
carrying the same dirty tag are not known to have used the same image. Only the clean form
can be rebuilt from a commit. Note that *any* uncommitted change counts, including an
untracked scratch file and an edit to a document that cannot affect the binary — commit or
stash before building an image you intend to run an experiment against.

`make image` depends on `make build-context-check`, which refuses the build if
`.dockerignore` excludes a tracked file. That is not fussiness: the build context includes
`.git` so `go build` can stamp the revision, and `go build` decides `vcs.modified` from
`git status --porcelain` **inside the context** — so an excluded tracked file reads as
*deleted*, stamps the binary `modified=true`, and the manifest then refuses every run
against that image at `local`, the floor of the quotability ladder. The failure looks like
a harness bug, which is why it is gated at build time.

**Optional.** To check an image's provenance end to end:

```sh
make image-provenance
```

It builds an image, extracts the binary, and asserts the revision is this commit and
`modified=false`. It refuses to run on a dirty tree, where `modified=true` is the correct
answer and the control could prove nothing.

## 4. Up

```sh
make topo-up
```

This builds the image, brings the stack up detached, then polls `/readyz` on both service
ports from the host until each answers or 60 seconds pass. There is deliberately **no
container healthcheck**: the runtime image has no shell and no `curl`, and adding a
probe-only mode to the production binary to work around that would put a second, weaker
definition of "ready" inside the thing being measured. The ports are published anyway, so
the honest check is the one a client would make.

On success — with the default ports; it prints whichever it actually published:

```
topology up. authority-1 -> localhost:8081, authority-2 -> localhost:8082
```

**That last line is the authority on where the units are.** Take `$S1` and `$S2` from it below.

To see what everything is doing, including the migration containers that have already
exited:

```sh
make topo-ps
```

### Changing ports — optional

Only needed if a default port is already taken on your machine. Every port has an environment
default; override on the command line:

```sh
make topo-up SERVICE_1_PORT=18081 SERVICE_2_PORT=18082
```

Command-line and environment variables are exported to the recipe, so Compose and the
readiness poll both see the override and agree.

**It does not come back to your shell.** A variable passed to `make` reaches the recipe's
child processes, not the interactive shell you typed it in — so afterwards `$SERVICE_1_PORT`
is still unset for you, and anything deriving an address from it silently gets the default.
That is why the next section says to copy what `topo-up` printed rather than to recompute it.

The one thing to keep in step by hand is the *default*, which is written in two places —
`SERVICE_1_PORT ?= 8081` in the `Makefile` and `${SERVICE_1_PORT:-8081}` in the Compose file.
Change one without the other and the poll waits on a port nothing is published to.

`AUTHORITY_1_PGPORT` and `AUTHORITY_2_PGPORT` (15433/15434) and the metrics ports
(`SERVICE_1_METRICS_PORT`, `SERVICE_2_METRICS_PORT`) are Compose-only and have no Makefile
counterpart.

Ports are all below 49152 on purpose. Hyper-V and WSL2 reserve blocks inside the Windows
dynamic port range at boot, so a higher port works or not by luck of the reboot.

### Set the addresses once

**Every command from here on uses `$S1` and `$S2` rather than a literal port**, because the
sections below are the ones that break when you take the override above. Copy them from the last
line `make topo-up` printed:

```sh
S1=localhost:8081        # whatever `make topo-up` printed
S2=localhost:8082
```

Every `curl` below uses `-sS` rather than `-s` for the same reason: with `-s`, a connection to the
wrong port prints nothing at all, and `jq` then prints nothing, so a refused connection is
indistinguishable from a successful call that returned an empty result. `-sS` keeps the progress
meter suppressed and lets the error through.

## 5. Checking it is actually serving what it claims

Each unit reports its own identity at `/meta`:

```sh
curl -sS $S1/meta | jq '{authority: .placement.authority_id,
  routing: .placement.routing_version, orgs: .placement.organisations,
  revision, modified, schema: .database.schema_version}'
curl -sS $S2/meta | jq '{authority: .placement.authority_id,
  routing: .placement.routing_version, orgs: .placement.organisations,
  revision, modified, schema: .database.schema_version}'
```

Expect unit 1 to name `authority-1` with `["org-a","org-c"]`, unit 2 `authority-2` with
`["org-b","org-d"]`, and both to agree on `routing_version`, `revision` and
`schema_version`. Those three agreeing is what makes the deployment *one* deployment;
nothing in a request total would reveal two units on different commits.

The operational endpoints are `/healthz` (process alive), `/readyz` (database reachable),
`/meta` (identity), and `/metrics` on the separate metrics port.

**Optional, and the first thing worth adding.** Four behaviours confirmed by hand, because each
is a property the topology exists to have — the `/meta` check above proves the units are bound
correctly, and these prove the binding actually decides anything:

```sh
# 1. a same-organisation booking on its own authority succeeds
curl -sS -X POST $S1/v1/slots/org-a/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k1' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 2. a colocated cross-organisation booking also succeeds — org-a and org-c
#    share authority-1, so this is supported, and it is what INV-13 protects
curl -sS -X POST $S1/v1/slots/org-c/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k2' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 3. a cross-AUTHORITY booking is refused on policy — 409, not an edge error.
#    Routed to the user's own unit, which understands the request and declines it.
curl -sS -X POST $S1/v1/slots/org-b/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k3' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 4. a MISROUTED request is refused at the edge — 400, a different failure
#    from 3: org-b is not served by this unit at all
curl -sS -X POST $S2/v1/slots/org-a/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k4' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'
```

Cases 3 and 4 look similar and are not. A cross-authority refusal (`409`,
`cross_authority_unsupported`) is the service applying Phase 1 policy to a request it
understood; a misroute (`400`, `invalid_request`) is the edge rejecting a request that
reached the wrong unit. Confusing them is how a routing bug gets recorded as a policy
result.

Slots must be seeded first — see §7.

### Failure isolation — optional

Stopping one authority's database should leave its unit unready while the other keeps
serving:

```sh
docker stop alloca-authority-1-db
curl -sS -o /dev/null -w '%{http_code}\n' $S1/readyz   # 503
curl -sS -o /dev/null -w '%{http_code}\n' $S2/readyz   # 200
docker start alloca-authority-1-db
```

**A stopped container is not the only failure mode, and they do not behave alike.** Stopping
drops packets rather than refusing them, so a request hangs to the server deadline and
returns `timeout_server` rather than failing fast. Killing the container, or a database that
refuses connections, classifies differently. This is recorded as
[`ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md) §6b, and any experiment must name
which fault it injected.

## 6. Down

```sh
make topo-down
```

`down -v`: the volumes go too. Each run starts from a known fixture, and a topology that
kept its data between runs would make the first run of a session differ from the rest —
the kind of difference that gets discovered halfway through interpreting a result.

## 7. Driving a multi-authority load run

Seeding is per authority and per organisation, because `alloca-seed` takes one DSN and one
organisation:

```sh
seed() {  # seed <pgport> <organisation> [reset]
  go run ./cmd/alloca-seed ${3:+-reset} \
    -database-url "postgres://alloca:alloca@localhost:$1/alloca?sslmode=disable" \
    -org "$2" -slots 100
}

seed 15433 org-a reset   # authority-1: reset once, on the first organisation
seed 15433 org-c
seed 15434 org-b reset   # authority-2: likewise
seed 15434 org-d
```

**`-reset` goes on the first organisation of each authority only, and nowhere else.** It
runs `TRUNCATE user_time_claims, user_identities, idempotency_records, bookings,
reservations, slots CASCADE` — the whole authority, not the organisation named on the
command line. Passing it on every call therefore deletes the slots seeded for the previous
organisation, leaving only the last one on each authority with any.

That failure does not announce itself. The run proceeds, the surviving half books normally,
and the missing half comes back `business_refusal / unknown_target` — a legitimate outcome
that looks like ordinary contention rather than a broken fixture. It was measured here at
exactly 200 of 400 requests before the recipe was corrected. Check the fixture rather than
trusting it:

```sh
docker exec alloca-authority-1-db psql -U alloca -d alloca -tAc \
  "select slot_organisation_id, count(*) from slots group by 1 order by 1"
# org-a|100
# org-c|100
```

The clean-start assertion runs with or without `-reset`, *after* seeding, and checks two
things: zero live claims **and** zero idempotency records. The second is the half that zero
claims does not imply — a record outlives the entity it describes, so a fixture whose holds
were all cancelled or expired holds no claims and still turns the next run into a replay
that commits nothing.

Skipping the seed step altogether is the dangerous case, because it does not fail loudly:
a confirmed claim has `expires_at IS NULL` and is never reaped, so re-running against a used
fixture yields a plausible all-`schedule_conflict` result that passes every correctness gate
and measures nothing.

Record what the containers are actually serving, then drive the run against both units,
routed by the same placement document the services enforce:

```sh
go build -o bin/alloca-load ./cmd/alloca-load

make topo-deployment > test/results/deployment.json

./bin/alloca-load \
  -placement deploy/topology/placement.json \
  -endpoint authority-1=http://$S1 \
  -endpoint authority-2=http://$S2 \
  -workload multi-org-dispersed \
  -deployment test/results/deployment.json \
  -concurrency 32 -n 400 -slots 100 \
  -out test/results/topo-run.json
```

**`-n 400`, not a duration, and the reason matters.** This is a smoke check of the topology, and
the seeded fixture holds a finite amount of capacity: 100 slots for each of four organisations at
capacity 20 is **8,000 units in total**. A bounded 400-request run consumes a twentieth of that
and every request can succeed, so `400 admitted_success` is a clean signal that routing, placement
and booking all work.

Swap in `-duration 60s` and the run does roughly 300,000 requests at this concurrency: the first
8,000 succeed, and **every one after that is a correct `no_capacity` refusal**. The gates still
pass — refusals are legitimate completed outcomes and `measurement_sound` stays true — but 97% of
the run is a sold-out fixture, and its throughput and latency describe capacity exhaustion rather
than booking. That is precisely the failure
[`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md)
§3.1 requires the dispersed workload to avoid, and it is the same shape as the seeding bug in §10:
a plausible result that measures something other than what the reader thinks.

If you do want a duration-bounded run here, seed enough capacity to outlast it and say so in the
report — do not read goodput from a run whose fixture sold out.

`make topo-deployment` inspects the running containers and records the immutable image ID
every unit must share — measurement-contract §11's identity of the deployed artifact. It is a separate step
rather than something `alloca-load` does, for two reasons: a process cannot see which image
wraps it, so a service asked this question could only repeat back an environment variable;
and reading it needs the Docker socket, which is root on the host and precisely what
`measurement-contract.md` §13.1 keeps the generator away from so it can later move to separate
compute.

It refuses a topology whose units are on different images. That is the failure the commit
SHA cannot see — the same code served from a stale `:dev` tag, or rebuilt on a newer base
layer, carries an identical revision on every unit.

The record also names the address each container publishes, and `alloca-load` checks that
those are exactly the units it is about to drive **before it sends a measured request**. Three
things therefore fail the run up front rather than after the numbers exist:

- a routed unit the record says nothing about — the run would measure an artifact it cannot
  name;
- a recorded unit the run does not route to — the record describes some other topology,
  usually because it was taken before the stack was raised again;
- a run across several units with no `-deployment` at all. No value of `-require` excuses it:
  that flag sets the level a run must clear to exit zero, not a ceiling on what its report
  certifies, so `-require none` would still produce a report claiming `local` or above while
  naming no artifact.

Re-record after anything that recreates a container. `make topo-up` following a rebuild gives
the units new image IDs, and a record from the previous stack will be refused rather than
quietly describing the wrong one.

Why it works this way, and the alternatives rejected, are
[ADR-0003](../decisions/0003-deployed-artifact-identity.md).

Build the generator rather than `go run`-ing it: `go run` does not stamp VCS data, so the
report cannot say which harness produced it.

`-placement` and `-target` are mutually exclusive — the first routes each organisation to
its own authority's endpoint, the second sends everything to one service.

The three multi-organisation workloads:

| `-workload` | What it drives |
|---|---|
| `multi-org-dispersed` | supported traffic across both authorities, mixing same-organisation and colocated cross-organisation bookings |
| `hot-organisation` | one organisation carries the whole load, so one authority is busy and its peers are not — the shape the failure-isolation experiment needs |
| `cross-authority-control` | the Phase 1 refusal, reported as its own evidence class and never mixed into the supported workload |

The report records what the run actually reached: `authority_count`, `routing_version`,
`placement_assignment`, `placement_digest`, and `topology_disagreement` — empty when the
units described one deployment.

## 8. When it goes wrong

**A service never becomes ready.** `make topo-ps` first. If the migration container did not
exit 0, the service never started — the schema gate refuses to serve against a schema it
does not carry migrations for.

```sh
docker compose -f deploy/topology/docker-compose.yml logs authority-1-migrate
docker compose -f deploy/topology/docker-compose.yml logs service-1
```

**The migration failed with "connection reset by peer".** The official Postgres image runs
an initialisation phase with a temporary server on the unix socket only. The healthcheck
forces a TCP check (`-h 127.0.0.1`) precisely to avoid reporting ready during it; if you
have edited that healthcheck, put it back.

**A unit refuses to start, saying it owns no organisations.** `ALLOCA_AUTHORITY_ID` names an
authority the placement map does not assign anything to — usually a typo in one or the
other. Failing at boot is the intended behaviour; a unit serving nothing is worse.

**A run reports `topology_disagreement`.** The units are not one deployment. The message
names which two units disagree and about what — revision, routing version, schema version,
a repeated authority, or a run-shaping setting like GOMAXPROCS or telemetry mode. The
common cause after a code change is one unit still running the previous image.

**Every run is refused at `local` for `service_source_modified`.** The image was built from
a dirty tree, or the build context is missing tracked files. Run
`./test/scripts/check-build-context.sh`, then `make image-provenance` on a clean checkout.

**Ports fail to bind after a Windows reboot.** A reserved dynamic-port block. See §4.

## 9. What is not wired yet

Deliberately, and recorded in [`ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md) §6a:

- **`cmd/alloca-verify` is still single-authority.** It takes one `--database-url` and one
  `--org`. The authority-aware verifier (`reconcile.RunTopology`) is built and tested as a
  package but has no CLI; its flags are shaped by how PR3c drives a run. So there is no
  one-command multi-authority reconciliation yet — verify per authority, or wait for PR3c.
- **The post-restoration resolution pass** — replaying ambiguous mutations after an
  authority returns, before the correctness verdict — is PR3c, along with the summary
  accounting it needs (§6c).

## 10. What has been run

Everything on this page was executed end to end against real containers on 2026-08-06, at
`57748fc`, on the WSL2 workstation. That is the full pass; the provenance path was re-run at
the merge head afterwards, recorded below it:

| Step | Result |
|---|---|
| `make image` | 14.7 s cold, 8.3 s warm — the ~110 MB context costs seconds |
| `make image-provenance` | passes: revision `57748fc`, `modified=false` |
| `make topo-up` | both units ready |
| `make topo-deployment` | one image ID across both units |
| seeding, corrected recipe | 100 slots for each of the four organisations |
| `multi-org-dispersed`, 400 requests | 400 `admitted_success`, `measurement_sound: true` |
| §5 routing checks 1–4 | 200, 200, 409 `cross_authority_unsupported`, 400 `invalid_request` |
| failure isolation | unit 1 `503`, unit 2 `200` and still booking; `200` again after restart |

The provenance path was re-run on the merge head, `ce7cd66`, on 2026-08-07, after the
observation was bound to the routed units and moved ahead of the workload:

| Step | Result |
|---|---|
| `make image-provenance` | passes: revision `ce7cd66`, `modified=false` |
| both units' `/meta` | `authority-1`/`authority-2`, one `pr3b-v1`, schema 1 each |
| `make topo-deployment` | `sha256:52015bb7…` on both containers, matching both targets |
| `multi-org-dispersed`, 100 requests | 100 `admitted_success`, 0 invalid, no drift, no disagreement |
| the report | `container_deployment: true`, the observed `image_id`, `unit_count: 2`, certified `local` |
| the same run with no `-deployment` and `-require none` | refused, exit 1, **no report written** |

The last row is the control for §7's rule that no level excuses the record. It stops before
any measured request, which is why there is no report to inspect — a refusal that produced
one would mean the check had moved back after the workload.

Four things are worth recording because they were found by running rather than reading.

**The build-context defect is now confirmed in a real build, not inferred.** A clean
checkout — zero uncommitted changes — with the pre-fix `.dockerignore` produces a binary
stamped `vcs.modified=true`; with the current one, `modified=false`. The containerised
service reports `"modified": false` at `/meta`, which is what makes a run against it
quotable at all.

**The seeding recipe on this page was wrong until it was run.** `-reset` truncates the whole
authority, so passing it per organisation deleted the previous one's slots; the run then
returned exactly 200 of 400 as `unknown_target`, which reads as contention rather than as a
broken fixture. §7 now carries the corrected form and the check that catches it.

**The page offered a port override and then ignored it** (found 2026-08-10). *Changing ports*
showed `make topo-up SERVICE_1_PORT=18081 SERVICE_2_PORT=18082`, and every section after it
hardcoded the 8081/8082 defaults — so taking the override the page recommends broke §5, §6, §7 and
the load invocation. It failed **silently**: `curl -s` prints nothing on a refused connection, so
`jq` printed nothing, and a wrong port was indistinguishable from an empty result. §4 now sets
`$S1`/`$S2` once, every command uses them, and every `curl` is `-sS`.

**The load command and the results table below described different experiments** (found
2026-08-10). The command was duration-bounded (`-duration 60s`) while the table records
iteration-bounded runs of 400 and 100 requests. They are not interchangeable here: the fixture
holds `[DERIVED]` 8,000 units of capacity (100 slots × 4 organisations × capacity 20), so a
60-second run at concurrency 32 exhausts it early and spends the overwhelming majority of its
requests on correct `no_capacity` refusals — a sound run whose throughput describes a sold-out
fixture rather than booking. §7 now carries `-n 400` and says why. The run that exposed this was
not retained (`test/results/` is git-ignored), so no figure from it is quotable; the arithmetic
above is derived from the fixture this page defines.

Still not covered here: `make test-integration` is a separate suite with its own database,
and CI runs it.

If you run something and it disagrees with this page, this page is wrong; fix it here.
