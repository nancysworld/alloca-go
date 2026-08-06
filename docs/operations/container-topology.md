# Building and running the container topology

How to build the production-shaped image, raise the two-authority deployment on your own
machine, check it is actually serving what it claims, and tear it down again.

**This document owns the procedure, not the design.** Why the topology has two independent
writers, what a placement map is, and what Phase 1 supports are owned by
[`../design-notes/horizontal-database-authority.md`](../design-notes/horizontal-database-authority.md);
what PR3 builds against it is
[`../planning/ag-sept-pr3-scope.md`](../planning/ag-sept-pr3-scope.md). The per-line
reasoning for each container lives in the comments of
[`../../deploy/topology/docker-compose.yml`](../../deploy/topology/docker-compose.yml) and
[`../../Dockerfile`](../../Dockerfile), which are the authority on *why* each setting is
what it is. Where any of those disagree with this page, they win.

For the single-service measurement procedure — seeding, driving a run, reconciling it —
see [`load-harness.md`](load-harness.md). This page is about the containers underneath it.

## 1. What is in the topology

Two independent PostgreSQL authorities, each with its own shard-affine service unit, and
one placement document mounted read-only into both. That is the smallest deployment in
which authority composition means anything: an organisation's rows live on exactly one
writer, a unit serves only the organisations its authority owns, and one authority failing
does not reach the other.

| Container | Role | Published on |
|---|---|---|
| `alloca-authority-1-db` | PostgreSQL, authority 1 | `localhost:15433` |
| `alloca-authority-2-db` | PostgreSQL, authority 2 | `localhost:15434` |
| `alloca-authority-1-migrate` | one-shot schema migration, must exit 0 first | — |
| `alloca-authority-2-migrate` | one-shot schema migration, must exit 0 first | — |
| `alloca-service-1` | service unit bound to authority 1 | `localhost:8081`, metrics `9091` |
| `alloca-service-2` | service unit bound to authority 2 | `localhost:8082`, metrics `9092` |

The placement document ([`../../deploy/topology/placement.json`](../../deploy/topology/placement.json))
is routing version `pr3b-v1`:

| Organisation | Authority | Reached at |
|---|---|---|
| `org-a`, `org-c` | `authority-1` | `localhost:8081` |
| `org-b`, `org-d` | `authority-2` | `localhost:8082` |

**Two organisations per authority is deliberate**, not padding. A pair on the *same*
authority is a supported cross-organisation booking (INV-13); a pair on *different*
authorities is the refusal Phase 1 exists to make explicit. A one-organisation-per-authority
map could not exercise the first case at all.

Migration is a separate one-shot container per authority rather than something the service
does at startup (ADR-0002). Each service waits for its own migration to *exit successfully*,
not merely to have started.

## 2. Prerequisites

Docker and Go. Nothing else — no Prometheus, no Grafana.

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

To check an image's provenance end to end:

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

On success:

```
topology up. authority-1 -> localhost:8081, authority-2 -> localhost:8082
```

To see what everything is doing, including the migration containers that have already
exited:

```sh
make topo-ps
```

### Changing ports

Every port has an environment default. Override on the command line:

```sh
make topo-up SERVICE_1_PORT=18081 SERVICE_2_PORT=18082
```

Command-line and environment variables are exported to the recipe, so Compose and the
readiness poll both see the override and agree. The one thing to keep in step by hand is
the *default*, which is written in two places — `SERVICE_1_PORT ?= 8081` in the `Makefile`
and `${SERVICE_1_PORT:-8081}` in the Compose file. Change one without the other and the
poll waits on a port nothing is published to.

`AUTHORITY_1_PGPORT` and `AUTHORITY_2_PGPORT` (15433/15434) and the metrics ports
(`SERVICE_1_METRICS_PORT`, `SERVICE_2_METRICS_PORT`) are Compose-only and have no Makefile
counterpart.

Ports are all below 49152 on purpose. Hyper-V and WSL2 reserve blocks inside the Windows
dynamic port range at boot, so a higher port works or not by luck of the reboot.

## 5. Checking it is actually serving what it claims

Each unit reports its own identity at `/meta`:

```sh
curl -s localhost:8081/meta | jq '{authority: .placement.authority_id,
  routing: .placement.routing_version, orgs: .placement.organisations,
  revision, modified, schema: .database.schema_version}'
curl -s localhost:8082/meta | jq '{authority: .placement.authority_id,
  routing: .placement.routing_version, orgs: .placement.organisations,
  revision, modified, schema: .database.schema_version}'
```

Expect unit 1 to name `authority-1` with `["org-a","org-c"]`, unit 2 `authority-2` with
`["org-b","org-d"]`, and both to agree on `routing_version`, `revision` and
`schema_version`. Those three agreeing is what makes the deployment *one* deployment;
nothing in a request total would reveal two units on different commits.

The operational endpoints are `/healthz` (process alive), `/readyz` (database reachable),
`/meta` (identity), and `/metrics` on the separate metrics port.

Four behaviours worth confirming by hand, because each is a property the topology exists to
have:

```sh
# 1. a same-organisation booking on its own authority succeeds
curl -s -X POST localhost:8081/v1/slots/org-a/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k1' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 2. a colocated cross-organisation booking also succeeds — org-a and org-c
#    share authority-1, so this is supported, and it is what INV-13 protects
curl -s -X POST localhost:8081/v1/slots/org-c/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k2' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 3. a cross-AUTHORITY booking is refused on policy — 409, not an edge error.
#    Routed to the user's own unit, which understands the request and declines it.
curl -s -X POST localhost:8081/v1/slots/org-b/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k3' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'

# 4. a MISROUTED request is refused at the edge — 400, a different failure
#    from 3: org-b is not served by this unit at all
curl -s -X POST localhost:8082/v1/slots/org-a/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k4' \
  -d '{"user_organisation_id":"org-a","user_id":"u-1"}'
```

Cases 3 and 4 look similar and are not. A cross-authority refusal (`409`,
`cross_authority_unsupported`) is the service applying Phase 1 policy to a request it
understood; a misroute (`400`, `invalid_request`) is the edge rejecting a request that
reached the wrong unit. Confusing them is how a routing bug gets recorded as a policy
result.

Slots must be seeded first — see §7.

### Failure isolation

Stopping one authority's database should leave its unit unready while the other keeps
serving:

```sh
docker stop alloca-authority-1-db
curl -s -o /dev/null -w '%{http_code}\n' localhost:8081/readyz   # 503
curl -s -o /dev/null -w '%{http_code}\n' localhost:8082/readyz   # 200
docker start alloca-authority-1-db
```

**A stopped container is not the only failure mode, and they do not behave alike.** Stopping
drops packets rather than refusing them, so a request hangs to the server deadline and
returns `timeout_server` rather than failing fast. Killing the container, or a database that
refuses connections, classifies differently. This is recorded as
[`ag-sept-pr3-scope.md`](../planning/ag-sept-pr3-scope.md) §6b, and any experiment must name
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
  -endpoint authority-1=http://localhost:8081 \
  -endpoint authority-2=http://localhost:8082 \
  -workload multi-org-dispersed \
  -deployment test/results/deployment.json \
  -concurrency 32 -duration 60s -slots 100 \
  -out test/results/topo-run.json
```

`make topo-deployment` inspects the running containers and records the immutable image ID
every unit must share — §6.4's identity of the deployed artifact. It is a separate step
rather than something `alloca-load` does, for two reasons: a process cannot see which image
wraps it, so a service asked this question could only repeat back an environment variable;
and reading it needs the Docker socket, which is root on the host and precisely what §6.3
keeps the generator away from so it can later move to separate compute.

It refuses a topology whose units are on different images. That is the failure the commit
SHA cannot see — the same code served from a stale `:dev` tag, or rebuilt on a newer base
layer, carries an identical revision on every unit.

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

Deliberately, and recorded in [`ag-sept-pr3-scope.md`](../planning/ag-sept-pr3-scope.md) §6a:

- **`cmd/alloca-verify` is still single-authority.** It takes one `--database-url` and one
  `--org`. The authority-aware verifier (`reconcile.RunTopology`) is built and tested as a
  package but has no CLI; its flags are shaped by how PR3c drives a run. So there is no
  one-command multi-authority reconciliation yet — verify per authority, or wait for PR3c.
- **The post-restoration resolution pass** — replaying ambiguous mutations after an
  authority returns, before the correctness verdict — is PR3c, along with the summary
  accounting it needs (§6c).

## 10. What has been run

Everything on this page was executed end to end against real containers on 2026-08-06, at
`57748fc`, on the WSL2 workstation:

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

Two things are worth recording because they were found by running rather than reading.

**The build-context defect is now confirmed in a real build, not inferred.** A clean
checkout — zero uncommitted changes — with the pre-fix `.dockerignore` produces a binary
stamped `vcs.modified=true`; with the current one, `modified=false`. The containerised
service reports `"modified": false` at `/meta`, which is what makes a run against it
quotable at all.

**The seeding recipe on this page was wrong until it was run.** `-reset` truncates the whole
authority, so passing it per organisation deleted the previous one's slots; the run then
returned exactly 200 of 400 as `unknown_target`, which reads as contention rather than as a
broken fixture. §7 now carries the corrected form and the check that catches it.

Still not covered here: `make test-integration` is a separate suite with its own database,
and CI runs it.

If you run something and it disagrees with this page, this page is wrong; fix it here.
