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

# 3. read the addresses off the running containers — do not type them       (§4)
port() { docker inspect --format \
  '{{with index .NetworkSettings.Ports "8080/tcp"}}{{(index . 0).HostPort}}{{end}}' "$1"; }
S1=localhost:$(port alloca-service-1)
S2=localhost:$(port alloca-service-2)
echo "$S1 $S2"

# 4. each unit is bound to the authority it claims                          (§5)
curl -sS $S1/meta | jq '{a: .placement.authority_id, orgs: .placement.organisations}'
curl -sS $S2/meta | jq '{a: .placement.authority_id, orgs: .placement.organisations}'

# 5. seed the fixture — -reset on the FIRST organisation of each authority  (§6)
seed() {
  go run ./cmd/alloca-seed ${3:+-reset} \
    -database-url "postgres://alloca:alloca@localhost:$1/alloca?sslmode=disable" \
    -org "$2" -slots 100
}
seed 15433 org-a reset
seed 15433 org-c
seed 15434 org-b reset
seed 15434 org-d

# 6. record what is running, then drive one bounded run                     (§6)
go build -o bin/alloca-load ./cmd/alloca-load
mkdir -p test/results/manual
make topo-deployment > test/observed/deployment.json
curl -sS http://localhost:9081/metrics > test/results/manual/s1-baseline.prom
curl -sS http://localhost:9082/metrics > test/results/manual/s2-baseline.prom
./bin/alloca-load \
  -placement deploy/topology/placement.json \
  -endpoint authority-1=http://$S1 -endpoint authority-2=http://$S2 \
  -workload multi-org-dispersed \
  -deployment test/observed/deployment.json \
  -concurrency 32 -n 400 -slots 100 \
  -out test/results/manual/topo-run.json

# 7. reconcile it against both authorities — after the run has exited       (§6.1)
curl -sS http://localhost:9081/metrics > test/results/manual/s1-after.prom
curl -sS http://localhost:9082/metrics > test/results/manual/s2-after.prom
go build -o bin/alloca-verify ./cmd/alloca-verify
./bin/alloca-verify \
  -run test/results/manual/topo-run.json \
  -placement deploy/topology/placement.json \
  -authority-db "authority-1=postgres://alloca:alloca@localhost:15433/alloca?sslmode=disable" \
  -authority-db "authority-2=postgres://alloca:alloca@localhost:15434/alloca?sslmode=disable" \
  -authority-metrics authority-1=test/results/manual/s1-after.prom \
  -authority-metrics authority-2=test/results/manual/s2-after.prom \
  -authority-metrics-baseline authority-1=test/results/manual/s1-baseline.prom \
  -authority-metrics-baseline authority-2=test/results/manual/s2-baseline.prom \
  -out test/results/manual/topo-verdict.json

# 8. tear it down                                                           (§7)
make topo-down
```

Expect **400 `admitted_success`** and `measurement_sound: true` from step 6, and every check
passing in step 7. If you get anything else, §8 is the place to start.

Each step is explained in the section named beside it, and the explanations carry the traps —
particularly step 5, where `-reset` on the wrong call silently empties the previous
organisation's slots.

### What is optional

| Optional | When you want it | Where |
|---|---|---|
| getting `docker` onto `PATH` in WSL | `docker` is not found at all | §2 |
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
| `alloca-service-1` | service unit bound to authority 1 | `localhost:8081`, metrics `9081` |
| `alloca-service-2` | service unit bound to authority 2 | `localhost:8082`, metrics `9082` |

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

If that works, the daemon is up and only the WSL integration is off. **Switch it on in Docker
Desktop's settings** — that puts a Linux `docker` on `PATH`, usually as `/usr/bin/docker`. Ports
published by Docker Desktop are reachable from WSL on `localhost`, so nothing else needs
changing. If `docker.exe ps` also fails, the daemon really is down.

**Do not reach for a `~/bin/docker` shim that execs `docker.exe`.** It runs, it builds, and it
poisons every image it produces. The Windows client reads the build context through the Windows
view of the WSL filesystem, where every file arrives mode `0755` against an index recording
`0644`; `go build` inside the builder stage runs `git status` on that context, sees a wholly
modified tree, and stamps the binary `vcs.modified=true`. `make image-provenance` then fails
with "the binary is stamped vcs.modified=true, but the checkout is clean", every run against the
image is refused at `local` on `service_source_modified`, and nothing in either message points
at the client. This page recommended the shim until 2026-08-11, which is how it was found.

If a build is stamped modified against a clean checkout, that is the first thing to check:

```sh
docker build --target builder -t alloca-probe .
docker run --rm -w /src alloca-probe sh -c \
  "git config --global --add safe.directory /src; git diff -- README.md | head -3"
# "old mode 100644 / new mode 100755" means the client, not your working tree.
```

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

### The Iteration C topologies — 1, 2 or 4 shard groups

`make topo-up` raises the two-authority PR3b topology and its `pr3b-v1` map, which is what
every earlier report reproduces against. Iteration C compares the *same* workload at one, two
and four shard groups ([`ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md)
§4.6), so it has its own target:

```sh
make itc-up ITC_GROUPS=4      # 1, 2 or 4
```

That selects the Compose profile and the matching `deploy/topology/placement-itc-g<n>.json`
together. **Do not mount a placement document by hand.** Only one direction of that mistake is
safe: four units under a two-authority map refuse to boot, because units 3 and 4 are assigned
no organisations. The other direction boots happily — one unit under the four-authority map
serves `org-a` and routes `org-b`, `org-c` and `org-d` to authorities that are not running, so
the run looks alive while measuring a quarter of its workload.

The variable is `ITC_GROUPS`, not `GROUPS`. `GROUPS` is a bash built-in array of your group
IDs and bash discards an assignment to it in silence, so the seeding script would receive your
GID instead of the group count.

Then seed the fixture. It is a separate step because the population is fixed for a comparison:
sized once for the largest intended `G4` run and reused unchanged at every capacity point, since
topology-specific resizing changes the workload rather than the topology.

```sh
ITC_GROUPS=4 SLOTS=200 ./test/scripts/itc-seed.sh
```

The script reads the same placement document and seeds each organisation into its own home
authority. It resets **once per authority, not once per organisation** — `alloca-seed -reset`
truncates `slots`, so resetting before each organisation would leave only the last one's fixture
standing. That loss is silent and asymmetric: at `G4` each organisation has its own database and
nothing is lost, while at `G1` all four share one and three of the four datasets vanish, which
depresses `G1` in the same direction as a genuine super-linear result.

Tear down every unit whichever profile raised it:

```sh
make itc-down
```

**Before a run whose numbers you intend to keep, prove the build is certifiable:**

```sh
git status --porcelain     # expect empty
make image-provenance      # fails if a clean checkout stamped modified=true
```

`go build` derives `vcs.modified` from `git status --porcelain`, which lists untracked files, so one
uncommitted scratch file stamps the binaries modified. `Manifest.Validate` refuses that at `local`
— the floor of the ladder — so the run certifies at `none` and backs nothing, however sound the
measurement was. The visible symptom is a `-dirty` suffix on the image tag.

### Changing ports — optional

Only needed if a default port is already taken on your machine. Every port has an environment
default; override on the command line:

```sh
make topo-up SERVICE_1_PORT=18081 SERVICE_2_PORT=18082
```

Command-line and environment variables are exported to the recipe, so Compose and the
readiness poll both see the override and agree.

**It does not come back to your shell.** A variable passed to `make` reaches the recipe and the
processes it starts — Compose and the readiness poll both see it, which is why they agree — but
not the interactive shell you typed it in. Afterwards `$SERVICE_1_PORT` is still unset for you,
so anything that recomputes an address from it silently gets the default. That is why the next
section reads the addresses off the containers instead of recomputing or transcribing them, and
why doing so makes this whole section harmless.

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
sections below are the ones that break when you take the override above.

**Read them off the running containers. Do not transcribe them from anything, including this
page:**

```sh
port() { docker inspect --format \
  '{{with index .NetworkSettings.Ports "8080/tcp"}}{{(index . 0).HostPort}}{{end}}' "$1"; }
S1=localhost:$(port alloca-service-1)
S2=localhost:$(port alloca-service-2)
echo "$S1 $S2"
```

A container's published port binding is the one authority on where it can be reached, and it is
the same fact `record-deployment.sh` records for the run manifest — so this cannot disagree with
what the run is later certified against.

**An earlier version of this section told you to copy the addresses `make topo-up` printed, into a
block containing the defaults. That was wrong**, and wrong in the way this page keeps being wrong:
it worked if you had not overridden the ports and silently pointed you at nothing if you had, so
the walkthrough passed or failed depending on which of the two paths above you took. A value you
are asked to substitute by hand is a value that will eventually not be substituted.

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
#    share authority-1, so this is supported, and it is what INV-13 protects.
#    NOTE the different user: u-2, not u-1. See below.
curl -sS -X POST $S1/v1/slots/org-c/slot-0/reservations \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: k2' \
  -d '{"user_organisation_id":"org-a","user_id":"u-2"}'

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

Expected in order: **`200` admitted, `200` admitted, `409 cross_authority_unsupported`,
`400 invalid_request`.**

**Case 2 books as `u-2`, and that is not cosmetic.** The seeded `slot-0` of each organisation
covers very nearly the same hour, so a user who took case 1's slot already holds an overlapping
claim. Running case 2 as `u-1` returns `409 business_refusal / schedule_conflict` — INV-13
working exactly as designed, since a claim is keyed by the *caller's* organisation and therefore
protects that user across every organisation they book into. It is a correct refusal that looks
like a broken topology, so the check that demonstrates colocated booking must not collide with the
check before it.

Cases 3 and 4 look similar and are not. A cross-authority refusal (`409`,
`cross_authority_unsupported`) is the service applying Phase 1 policy to a request it
understood; a misroute (`400`, `invalid_request`) is the edge rejecting a request that
reached the wrong unit. Confusing them is how a routing bug gets recorded as a policy
result.

Slots must be seeded first — see §6.

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

**A *measured* failure-isolation run adds one requirement to the sequence above: restore the
authority while the run is still going.** The generator replays ambiguous mutations when the
workload ends (§6.2), and it can only do that against an authority that is answering; a run whose
replays find the database still down is refused as unresolved. Stop the container inside the
measured window, start it again inside the same window, and let the run finish on a healthy
topology.

## 6. Driving a multi-authority load run

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

mkdir -p test/results/manual   # git-ignored, and absent on a fresh clone
make topo-deployment > test/observed/deployment.json

./bin/alloca-load \
  -placement deploy/topology/placement.json \
  -endpoint authority-1=http://$S1 \
  -endpoint authority-2=http://$S2 \
  -workload multi-org-dispersed \
  -deployment test/observed/deployment.json \
  -concurrency 32 -n 400 -slots 100 \
  -out test/results/manual/topo-run.json
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

The four multi-organisation workloads:

| `-workload` | What it drives |
|---|---|
| `multi-org-dispersed` | supported traffic across both authorities, mixing same-organisation and colocated cross-organisation bookings |
| `hot-organisation` | one organisation carries the whole load, so one authority is busy and its peers are not — the shape the failure-isolation experiment needs |
| `cross-authority-control` | the Phase 1 refusal, reported as its own evidence class and never mixed into the supported workload |
| `wl-mut-disp-4` | the Iteration C capacity workload: four organisations, equal share, every user paired only with its **own** organisation's slots |

**`wl-mut-disp-4` and `multi-org-dispersed` are not interchangeable, and the difference is the
point.** `multi-org-dispersed` deliberately mixes colocated cross-organisation bookings in, so
how many of its requests are same-organisation depends on how many organisations share an
authority. Comparing `G1` against `G4` with it would change the workload and the topology at
once, and the scale-efficiency figure would carry both. `wl-mut-disp-4` derives its pairs from
the organisation's own population and never consults the placement map, so the same sequence
number produces an identical request at every topology — which is what makes `E2` and `E4` a
measurement of the architecture. Use `multi-org-dispersed` for Phase 1 correctness coverage and
`wl-mut-disp-4` for anything compared across topologies.

The report records what the run actually reached: `authority_count`, `routing_version`,
`placement_assignment`, `placement_digest`, and `topology_disagreement` — empty when the
units described one deployment.

### 6.1 Reconciling the run against both authorities

A report says what the client observed. It does not say whether the databases agree, and a
multi-authority run has three counts to bring together rather than two — client, both units'
own counters, and the rows on each authority
([`measurement-contract.md`](../design/measurement-contract.md) §12). `alloca-verify -placement`
is that step, and it is what turns a run into evidence:

```sh
./bin/alloca-verify \
  -run test/results/manual/topo-run.json \
  -placement deploy/topology/placement.json \
  -authority-db "authority-1=postgres://alloca:alloca@localhost:15433/alloca?sslmode=disable" \
  -authority-db "authority-2=postgres://alloca:alloca@localhost:15434/alloca?sslmode=disable" \
  -authority-metrics authority-1=test/results/manual/s1-after.prom \
  -authority-metrics authority-2=test/results/manual/s2-after.prom \
  -authority-metrics-baseline authority-1=test/results/manual/s1-baseline.prom \
  -authority-metrics-baseline authority-2=test/results/manual/s2-baseline.prom \
  -out test/results/manual/topo-verdict.json
```

The verdict names every authority it read, carries each one's local safety checks — capacity,
schedule non-overlap, one key one outcome — and then compares the *summed* persisted and server
counts once against the run's client totals. It exits non-zero when the run does not reach the
level `-require` asks for.

The organisation set comes from the placement document, not from the databases. An authority
asked to discover its own scope would silently absorb rows that are on the wrong authority,
which is the one failure placement exists to prevent.

Four things this step is particular about, each of which otherwise produces a wrong answer that
looks right:

- **`-placement` and `-database-url`/`-org` are mutually exclusive**, as they are in the
  generator. The single-authority flags verify one database against a report describing several,
  which is the comparison §12 forbids;
- **every unit's scrape, or none.** Each unit's before/after pair is differenced on its own and
  only then summed, so a unit whose scrape is missing lowers the total — arithmetically identical
  to a service that dropped requests. A partial set is refused by name rather than reported as a
  disagreement;
- **scrape after the process has exited, not when the workload ends.** `alloca-load` replays any
  ambiguous mutation after the measured interval (§6.2), and those requests reach the service. A
  scrape taken before it exits misses them and the counts disagree by exactly the number of
  replays;
- **verify promptly.** Unconfirmed holds expire on the reservation TTL — two minutes by default,
  and `/meta` reports it — so live reservations decay after the run while the idempotency records
  and claims stay. The check tolerates fewer reservations than admitted mutations, because that
  is what expiry looks like, but a verdict taken an hour later is evidence about expiry rather
  than about the run.

### 6.2 Ambiguous mutations are replayed before the report is written

When the service answers a mutation `unknown_replayable`, the commit may or may not have landed
and the client cannot tell. Such a run is not reconcilable until each of those keys has been
replayed and settled, and the register that knows the keys lives in the generator process.

`alloca-load` therefore replays them itself, after the measured interval and before it writes the
report. There is no flag: a healthy run registers nothing and the pass costs nothing. It prints
what it did:

```
alloca-load: replayed 3 ambiguous mutation(s), 0 still unresolved
```

The replays are kept out of measured performance — goodput, latency and the interval describe
what happened during the run — and appear in the report's `ambiguity_resolutions`, which is what
the reconciliation population and the final logical-mutation count are built from.

**Anything still unresolved refuses the run.** The usual cause is resolving too early: an
authority that is still down cannot answer a replay, and a replay that never reached the service
settles nothing. So an experiment that takes an authority away must **restore it before the run
ends** — not after `alloca-load` exits. `-resolve-timeout` bounds the pass so a large register
against a dead authority cannot hang the harness.

## 7. Down

The last step, once you have the report you came for:

```sh
make topo-down
```

`down -v`: the volumes go too. Each run starts from a known fixture, and a topology that
kept its data between runs would make the first run of a session differ from the rest —
the kind of difference that gets discovered halfway through interpreting a result.

**Leaving it up between runs is fine, but reseed with `-reset` before the next one.** The
clean-start assertion refuses a fixture holding live claims or idempotency records, which is
what stops a rerun replaying the previous run's keys and measuring nothing (§6).

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

Both entries that stood here — the multi-authority verifier CLI and the post-restoration
resolution pass — were built in PR3c and are documented in §6.1 and §6.2.

What remains deliberately unbuilt is recorded in
[`ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md) §7, and one item is worth naming
here because an operator will look for it: there is **no targeted mid-`COMMIT` fault injection**.
Stopping an authority proves containment, not the acknowledgement-lost half of INV-21, which
needs something interposed between client and server. A generic shutdown must not be reported as
proof of that fault.

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

The whole page was walked again from `topo-down` on 2026-08-10 at `164e72e`, which is where the
§0 minimum path and the port and `-n 400` corrections come from:

| Step | Result |
|---|---|
| `make topo-up` from clean | image `164e72e`, both units ready on the default ports |
| both units' `/meta` | `authority-1`/`["org-a","org-c"]`, `authority-2`/`["org-b","org-d"]`, one `pr3b-v1`, schema 1, `modified: false` |
| seeding, four organisations | `org-a\|100`, `org-c\|100` on authority-1; `org-b\|100`, `org-d\|100` on authority-2 |
| `make topo-deployment` | one image ID across both containers, matching both targets |
| `multi-org-dispersed`, `-n 400` | **400 `admitted_success`**, `measurement_sound: true`, 0 invalid, certified `local` |
| §5 routing checks 1–4 | `200`, `200` (as `u-2`), `409 cross_authority_unsupported`, `400 invalid_request` |
| failure isolation | unit 1 `503`, unit 2 `200` and still booking; `200` again after restart |

**Case 2 was wrong on this page until it was run.** It reused `u-1` from case 1, and the two
`slot-0`s overlap, so it returned `409 schedule_conflict` — INV-13 refusing correctly, in a way
that reads as a broken topology. It now books as `u-2`, and §5 says why. That is the third recipe
on this page to have shipped broken and been caught only by execution, after the `-reset` scope and
the port override.

The last row of the previous table is the control for §6's rule that no level excuses the record.
It stops before any measured request, which is why there is no report to inspect — a refusal that produced
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
broken fixture. §6 now carries the corrected form and the check that catches it.

**The page offered a port override and then ignored it** (found 2026-08-10). *Changing ports*
showed `make topo-up SERVICE_1_PORT=18081 SERVICE_2_PORT=18082`, and every section after it
hardcoded the 8081/8082 defaults — so taking the override the page recommends broke §5 and the
load run of §6. It failed **silently**: `curl -s` prints nothing on a refused connection, so
`jq` printed nothing, and a wrong port was indistinguishable from an empty result. §4 now sets
`$S1`/`$S2` once, every command uses them, and every `curl` is `-sS`.

**The load command and the results table below described different experiments** (found
2026-08-10). The command was duration-bounded (`-duration 60s`) while the table records
iteration-bounded runs of 400 and 100 requests. They are not interchangeable here: the fixture
holds `[DERIVED]` 8,000 units of capacity (100 slots × 4 organisations × capacity 20), so a
60-second run at concurrency 32 exhausts it early and spends the overwhelming majority of its
requests on correct `no_capacity` refusals — a sound run whose throughput describes a sold-out
fixture rather than booking. §6 now carries `-n 400` and says why. The run that exposed this was
not retained (`test/results/` is git-ignored), so no figure from it is quotable; the arithmetic
above is derived from the fixture this page defines.

**§6.1 and §6.2 were added in PR3c and executed on 2026-08-11**, at `5f61db5`, against the live
topology. The verification command in §6.1 was run exactly as written — same flags, same order,
paths pointed at a real cell — and returned 11 of 11 checks at `local`. The resolution pass in
§6.2 was exercised repeatedly by the failure-isolation cell, including one run where a fault
produced a genuine `unknown_replayable` and the pass settled it
([`ag-sept-pr3c-phase1-correctness.md`](../measurements/reports/ag-sept-pr3c-phase1-correctness.md)
§5.2).

One honest limit on that: the `curl` and `docker inspect` steps of §0 could not be run *verbatim*
from the agent environment, which reaches published ports through a proxy and cannot address the
Docker socket from inside its sandbox. They were exercised in equivalent form. If you are the
first person to run §0 end to end on a workstation, that is still worth doing — this page's
history is mostly recipes that survived review and failed on execution.

Still not covered here: `make test-integration` is a separate suite with its own database,
and CI runs it.

If you run something and it disagrees with this page, this page is wrong; fix it here.
