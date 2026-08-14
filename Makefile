# Local mirror of the CI gate (.github/workflows/ci.yml). Running `make ci`
# reproduces exactly what CI enforces — including golangci-lint at the same pinned
# version — satisfying the AG-M0 "build and test reproducibly" exit criterion both
# locally and in CI. A locally green `make ci` must not be able to fail remotely.

GO      ?= go
PKGS    ?= ./...
BINARY  ?= alloca-go
CMD     ?= ./cmd/alloca-go

# Pinned linter version. This MUST match the `version:` in .github/workflows/ci.yml
# so the local and remote lint gates are identical; bump them together.
GOLANGCI_LINT_VERSION ?= v2.12.2
TOOLBIN               := $(CURDIR)/bin
GOLANGCI_LINT         := $(TOOLBIN)/golangci-lint
# Version-stamped sentinel: changing GOLANGCI_LINT_VERSION changes this path, which
# forces a reinstall of the correct version rather than silently reusing an old one.
GOLANGCI_LINT_STAMP   := $(TOOLBIN)/.golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: all ci fmt fmt-check vet lint build test test-race test-integration \
        db-up db-down migrate run dev dev-measured smoke obs-up obs-target obs-down tidy tools clean \
        image topo-up topo-down topo-ps itc-up itc-down itc-deployment \
        itc-layout itc-layout-check itc-topology-check-test itc-obs-labels-check itc-rehearse obs-rehearse

# Integration tests need a real PostgreSQL: the properties they prove (capacity safety
# under concurrent transactions, post-lock decision time, the scoped-key race) do not
# exist without one. They are behind the `integration` build tag so the default gate
# stays hermetic and fast.
DATABASE_URL ?= postgres://alloca:alloca@localhost:$(PGPORT)/alloca?sslmode=disable
# Where `make smoke` looks for a running service, and how long each of its requests waits.
# Raise MAX_TIME when the service is paused in a debugger: it is curl's own patience, so no
# server-side deadline affects it.
BASE         ?= http://localhost:8080
MAX_TIME     ?= 10
PGCONTAINER  ?= alloca-pg
PGIMAGE      ?= postgres:16-alpine
# Deliberately below 49152, the start of the Windows dynamic port range. Hyper-V and WSL2
# reserve blocks inside that range at boot, and a reserved port makes `docker run -p` fail
# with "ports are not available ... /forwards/expose returned unexpected status: 500" until
# the next reboot reshuffles the blocks. A port above 49152 therefore works or not by luck
# of the boot; this one is stable. `netsh interface ipv4 show excludedportrange protocol=tcp`
# lists the current reservations.
PGPORT       ?= 15432
# The PR2 observability stack. Separate from the service so `make dev-measured` and a
# measured run stay independent of whether anything is scraping.
OBSCOMPOSE   ?= deploy/observability/docker-compose.yml
# The rehearsal overlay for the observability stack: Prometheus and Grafana confined to the
# generator/monitor CPU set. Monitoring is part of the measuring side, not part of the
# environment — an unpinned Prometheus scrapes every unit from inside the units' own CPUs, and
# does it harder at G4 than at G1 (ag-sept-pr4.md §2.14).
OBSREHEARSALCOMPOSE ?= deploy/observability/docker-compose.rehearsal.yml
# The Compose files `obs-up` raises. `obs-rehearse` re-invokes obs-up with the overlay appended,
# for the same reason itc-rehearse does: one recipe raises the stack, and only the file list
# changes.
OBS_COMPOSE  ?= -f $(OBSCOMPOSE)
# The PR3b two-authority topology: two PostgreSQL authorities, two shard-affine service
# units, one placement document. Separate from the observability stack so a topology can be
# raised and torn down without disturbing whatever is scraping it.
TOPOCOMPOSE  ?= deploy/topology/docker-compose.yml
# The local partitioned rehearsal overlay: cpusets confining each shard group to its own CPUs
# (ag-sept-pr4.md §2.14). Applied by `itc-rehearse` and by nothing else — `itc-up` deliberately
# raises the same topology unpartitioned, because a cpuset that silently applied to every local
# run would make "the topology" mean two different things depending on the machine it was on.
REHEARSALCOMPOSE ?= deploy/topology/docker-compose.rehearsal.yml
# The Compose files `itc-up` raises. `itc-rehearse` re-invokes itc-up with the overlay appended,
# so the recipe that actually calls `compose up` stays in one place and does not need to know the
# rehearsal exists.
ITC_COMPOSE  ?= -f $(TOPOCOMPOSE)
# Immutable experiment tagging (§9.1). Defaults to the working tree's commit so a run's
# artifacts name the revision the image was built from; `-dirty` when the tree has uncommitted
# changes, which is a warning that no commit describes what went into it.
#
# The tag is a convenience label, not the artifact's identity. §9.1 and ADR-0003 identify the
# deployed artifact by its immutable image ID or digest, observed off the running container;
# a tag is a mutable alias that can be reassigned to different content.
#
# The dirty test is `git status --porcelain`, not `git diff --quiet`, for one reason: it must
# agree with the flag the *binary* carries. `go build` decides `vcs.modified` from
# `git status --porcelain` being non-empty, so it counts staged and untracked files, which
# `git diff --quiet` — worktree against index — does not see. A tag reading clean on an image
# whose /meta reports modified=true is worse than no tag: it is the one field an operator
# would use to decide the run came from a known commit.
#
# **`-dirty` is a warning, not an identifier.** Two different uncommitted trees both tag
# `abc1234-dirty`, and the second build silently replaces the first under that tag — so two
# runs carrying the same dirty tag are not known to have used the same image, and must never
# be compared on the strength of it. Only the clean form satisfies §9.1's requirement that the
# build be version-controlled. That is consistent with the manifest, which refuses
# `service_source_modified` at `local` and so declines to quote a dirty run at any level; the
# tag is what tells an operator *why* before they get that far.
#
# Note this counts *any* uncommitted change, including one to a document that cannot affect
# the binary. That is deliberate: `modified` is a claim about whether the tree matched the
# commit, and narrowing it to "changes I judge to affect the build" would report clean for a
# tree that is not. Commit or stash before building an image meant for an experiment.
ALLOCA_IMAGE_TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell test -z "$$(git status --porcelain 2>/dev/null)" || echo -dirty)
SERVICE_1_PORT   ?= 8081
SERVICE_2_PORT   ?= 8082
SERVICE_3_PORT   ?= 8083
SERVICE_4_PORT   ?= 8084
# Iteration C's shard-group count: 1, 2 or 4 (ag-sept-validation-plan.md §4.6). It selects the
# Compose profile and the placement document together, which is the pairing itc-up exists to
# make impossible to get wrong.
#
# **Not `GROUPS`.** That is a bash built-in array holding the caller's group IDs, and bash
# discards an assignment to it *silently* — `GROUPS=4 ./test/scripts/itc-seed.sh` arrives in the
# script as `1000`. Make expands `$(GROUPS)` itself and would have been unaffected, which is
# exactly what makes the trap worth avoiding by name: the Makefile would have worked while the
# script it documents did not.
ITC_GROUPS   ?= 4
# The rehearsal's CPU partition (ag-sept-pr4.md §2.14). These defaults are the same ones the
# overlay falls back to; they are named here as well so that one invocation validates and raises
# the same partition. Passing them explicitly is what stops the layout check from approving
# 0-1/2-3/4-5/6-7 while Compose pins something else.
#
# On this workstation CPUs 12-15 are deliberately outside the partition: they are the headroom
# the generator control widens into (ITC_CPUS_GENERATOR=8-15), and holding them idle otherwise
# keeps unpinned host work off the generator's own set.
ITC_CPUS_A   ?= 0-1
ITC_CPUS_B   ?= 2-3
ITC_CPUS_C   ?= 4-5
ITC_CPUS_D   ?= 6-7
ITC_CPUS_GENERATOR ?= 8-11

all: ci

## ci: run the full local gate, identical to CI (fmt, vet, lint, build, test, race)
ci: fmt-check vet lint build test test-race build-context-check itc-layout-check itc-topology-check-test itc-obs-labels-check

## fmt: format all Go files
fmt:
	$(GO) fmt $(PKGS)

## fmt-check: fail if any file is not gofmt-clean
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

## vet: run go vet
vet:
	$(GO) vet $(PKGS)

## lint: run golangci-lint at the pinned version (auto-provisioned into ./bin)
lint: $(GOLANGCI_LINT_STAMP)
	$(GOLANGCI_LINT) run

## tools: (re)install pinned developer tools into ./bin
tools: $(GOLANGCI_LINT_STAMP)

$(GOLANGCI_LINT_STAMP):
	@echo "installing golangci-lint $(GOLANGCI_LINT_VERSION) -> $(TOOLBIN)"
	@mkdir -p $(TOOLBIN)
	@rm -f $(GOLANGCI_LINT) $(TOOLBIN)/.golangci-lint-*
	GOBIN=$(TOOLBIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@touch $(GOLANGCI_LINT_STAMP)

## build: compile all packages
build:
	$(GO) build $(PKGS)

## test: run unit tests
test:
	$(GO) test $(PKGS)

## test-race: run unit tests under the race detector
test-race:
	$(GO) test -race $(PKGS)

## test-integration: run the PostgreSQL integration tests (needs DATABASE_URL)
test-integration:
	DATABASE_URL="$(DATABASE_URL)" $(GO) test -tags=integration -race -count=1 ./...

## db-up: start a local PostgreSQL for integration tests
db-up:
	@docker rm -f $(PGCONTAINER) >/dev/null 2>&1 || true
	docker run -d --name $(PGCONTAINER) \
		-e POSTGRES_USER=alloca -e POSTGRES_PASSWORD=alloca -e POSTGRES_DB=alloca \
		-p $(PGPORT):5432 $(PGIMAGE)
	@echo "waiting for postgres..."
	@# -h 127.0.0.1 forces a TCP check, and that is the whole point rather than a detail.
	@# The official image's entrypoint runs an initialisation phase in which a temporary
	@# server is started with listen_addresses='' — reachable on the unix socket but NOT
	@# over TCP. A socket-based pg_isready therefore reports ready during init, before the
	@# real server accepts connections, and the next target fails with "connection reset by
	@# peer". Typing the commands by hand hides this; chaining them in `dev` does not.
	@#
	@# Bounded so a container that never becomes healthy fails with an explanation instead
	@# of hanging until someone notices.
	@for i in $$(seq 1 120); do \
		if docker exec $(PGCONTAINER) pg_isready -h 127.0.0.1 -p 5432 -U alloca -q; then \
			echo "postgres ready on port $(PGPORT)"; exit 0; \
		fi; \
		sleep 0.5; \
	done; \
	echo "postgres did not accept TCP connections within 60s; check: docker logs $(PGCONTAINER)"; \
	exit 1

## db-down: stop and remove the local PostgreSQL
db-down:
	@docker rm -f $(PGCONTAINER) >/dev/null 2>&1 || true

## migrate: apply database migrations (needs DATABASE_URL)
migrate:
	DATABASE_URL="$(DATABASE_URL)" $(GO) run ./cmd/alloca-migrate

## run: run the service against DATABASE_URL (does not migrate; see `dev`)
run:
	DATABASE_URL="$(DATABASE_URL)" $(GO) run $(CMD)

## dev: from cold — start the database, migrate it, then run the service
#
# The three steps stay separate targets, and `run` deliberately does not migrate on its
# own. ADR-0002 keeps schema changes out of the serving path so replicas never race the
# same DDL on startup; a `run` that quietly migrated would make local development the one
# place that rule does not hold, which is how the rule stops being believed.
#
# Recursive $(MAKE) rather than prerequisites, so the order holds under `make -j`.
#
# NOTE: this serves via `go run`, whose binary carries no VCS stamp, so /meta reports no
# revision and a load run against it cannot be certified. Use `dev-measured` for that.
dev:
	$(MAKE) db-up
	$(MAKE) migrate
	$(MAKE) run

## dev-measured: like `dev`, but serves a built binary so /meta reports a revision
#
# `go run` does not stamp VCS data, so a service started by `dev` cannot say which commit it
# is. The load harness reads that from /meta and records it as the identity of the code under
# test, so an unstamped service makes every run against it uncertifiable (level `none`).
#
# This exists as a separate target rather than as a change to `dev` because `go run` is the
# right default for ordinary development — it rebuilds on every start with no artifact to go
# stale — and the stamp only matters when a run's provenance will be recorded.
dev-measured:
	$(MAKE) db-up
	$(MAKE) migrate
	$(GO) build -o $(TOOLBIN)/$(BINARY) $(CMD)
	DATABASE_URL="$(DATABASE_URL)" $(TOOLBIN)/$(BINARY)

## obs-up: start Prometheus + Grafana for a measured run (measurement-contract §6)
#
# Prometheus scrapes the service on the *host*, not in this compose project: PR2 measures a
# locally built binary and containerising the service is PR3's variable, not PR2's.
#
# Grafana is provisioned from the repo, so there is nothing to click: the dashboard and its
# datasource exist on first start. It is the diagnostic view only — the evidence a report
# quotes is the CSVs and TSDB snapshot the sweep runner retains.
obs-up:
	ITC_CPUS_GENERATOR=$(ITC_CPUS_GENERATOR) docker compose $(OBS_COMPOSE) up -d
	@# Probe for an address that actually reaches the service, rather than assuming one.
	@# Tolerated on failure: the stack is still useful with the service down, and the script
	@# prints what to do. Re-run `make obs-target` once the service is up.
	@./test/scripts/obs-target.sh || true
	@echo "prometheus  http://localhost:9091"
	@echo "grafana     http://localhost:3000/d/alloca-frontier"

## obs-rehearse: raise Prometheus and Grafana confined to the generator/monitor CPUs
#
# The monitoring half of the rehearsal partition. `make obs-up` raises the same stack unpinned,
# which is correct for ordinary local work and wrong during a rehearsal: an unconfined
# Prometheus scrapes the capacity units from inside their own CPUs.
#
# Kept separate from itc-rehearse rather than folded into it, because the topology and the
# scrape stack are deliberately independent — a topology can be raised and torn down without
# disturbing whatever is scraping it. Raising them together would make a torn-down topology
# take the evidence path with it.
#
# The target list is generated first, from ITC_GROUPS, so the scrape set and the topology cannot
# disagree about how many units the run has. Doing it here rather than leaving it to the operator
# is the difference between a missing unit being caught by the run's scrape gate and being
# discovered when a report has a hole in it.
obs-rehearse:
	@ITC_GROUPS=$(ITC_GROUPS) ./test/scripts/itc-obs-targets.sh
	@$(MAKE) --no-print-directory obs-up \
	  OBS_COMPOSE="-f $(OBSCOMPOSE) -f $(OBSREHEARSALCOMPOSE)"
	@echo "  monitoring confined to CPUs $(ITC_CPUS_GENERATOR)"
	@echo "  scraping $(ITC_GROUPS) unit(s) as service-N:9090 on the topology network"

## obs-target: re-probe the scrape address (after a WSL restart, or a late service start)
obs-target:
	@./test/scripts/obs-target.sh

## obs-down: stop Prometheus + Grafana, keeping the retained TSDB
#
# The volume survives on purpose. A sweep's evidence is snapshotted out of it, and tearing
# the data down with the containers would discard the series a half-finished analysis still
# needs. `docker compose -f deploy/observability/docker-compose.yml down -v` is the explicit
# way to discard it.
obs-down:
	docker compose -f $(OBSCOMPOSE) down

## smoke: exercise a running service over a real socket (needs `make dev` elsewhere)
smoke:
	@BASE="$(BASE)" MAX_TIME="$(MAX_TIME)" DATABASE_URL="$(DATABASE_URL)" ./test/scripts/smoke.sh

## tidy: tidy the module graph
tidy:
	$(GO) mod tidy

## clean: remove build artifacts and provisioned tools
clean:
	rm -f $(BINARY)
	rm -rf $(TOOLBIN)
	$(GO) clean

## build-context-check: fail if .dockerignore excludes any tracked file
#
# In `ci` because the defect it catches is introduced by editing .dockerignore, which anyone
# can do without ever building an image — and the symptom appears much later, as a run the
# manifest refuses for reasons that look like a harness bug. Needs no Docker daemon.
build-context-check:
	@./test/scripts/check-build-context.sh

## itc-layout-check: prove the rehearsal's CPU layout check still refuses a bad partition
#
# In `ci` for the same reason build-context-check is: the check it guards is a shell script
# anyone can edit without raising a topology, and the defect only shows up later — as a
# rehearsal that ran on a partition nothing had actually validated. Needs bash, taskset and a
# few CPUs; no daemon, no database, no judgement (docs/design/project-structure.md §1).
#
# It is not covered by `go test ./...`: the layout check is bash, and the Go suite says nothing
# about whether it still enforces anything.
itc-layout-check:
	@./test/scripts/itc-cpu-layout-test.sh

## itc-topology-check-test: prove the topology check still classifies containers correctly
#
# In `ci` for the same reasons as the two checks above, and it stubs `docker` rather than calling
# it: the property is set arithmetic over container names, which needs no daemon. It exists
# because the check misclassified the observability stack as topology units and refused a correct
# G4 (ag-sept-pr4.md §3).
itc-topology-check-test:
	@./test/scripts/itc-topology-check-test.sh

## itc-obs-labels-check: prove a retained sample can say which topology produced it
#
# In `ci` because it needs no daemon: itc-obs-targets.sh is text generation, and the other two
# assertions read committed files. It exists because prometheus.yml set topology job-wide, which
# mislabelled every Iteration C sample as PR2's single-instance experiment and survived review —
# the label was present and well-formed, so every query and every gate passed (ag-sept-pr4.md §3).
itc-obs-labels-check:
	@./test/scripts/itc-obs-targets-test.sh

## image: build the production-shaped service image, tagged with the current commit
#
# The build context includes .git on purpose: `go build` stamps the VCS revision into the
# binary, /meta reports it, and the load harness records it as the identity of the code under
# test. An unstamped image serves runs that cannot be certified (§6.4).
#
# It depends on build-context-check because that stamp is only truthful if the context holds
# every tracked file: git reports an excluded tracked path as *deleted*, which stamps the
# binary modified=true and makes the image uncertifiable at the floor of the ladder. Better
# to refuse the build than to ship an image whose provenance quietly disqualifies every run
# made against it.
image: build-context-check
	docker build -t alloca-go:$(ALLOCA_IMAGE_TAG) -t alloca-go:dev .
	@echo "built alloca-go:$(ALLOCA_IMAGE_TAG)"

## image-provenance: build an image and prove its binary carries the expected VCS stamp
#
# The end-to-end control for §6.4: it extracts the binary and asserts the revision is this
# commit and modified=false. Refuses to run on a dirty tree, where modified=true is the
# correct answer and the control could prove nothing. Needs a Docker daemon, so it is not in
# `ci`; build-context-check covers the same rule there.
image-provenance:
	@./test/scripts/check-image-provenance.sh

## topo-up: build the image, then raise the two-authority topology and wait for readiness
#
# Readiness is polled from the host rather than by a container healthcheck: the runtime image
# has no shell and no curl by design, and adding a probe-only mode to the production binary
# would put a second definition of "ready" inside the thing being measured. The ports are
# published anyway, so the honest check is the one a client would make.
topo-up: image
	ALLOCA_IMAGE_TAG=$(ALLOCA_IMAGE_TAG) docker compose -f $(TOPOCOMPOSE) --profile g2 up -d
	@echo "waiting for both service units to report ready..."
	@for port in $(SERVICE_1_PORT) $(SERVICE_2_PORT); do \
		ok=0; \
		for i in $$(seq 1 60); do \
			if curl -fsS -m 2 "http://localhost:$$port/readyz" >/dev/null 2>&1; then \
				echo "  service on $$port ready"; ok=1; break; \
			fi; \
			sleep 1; \
		done; \
		if [ $$ok -ne 1 ]; then \
			echo "  service on $$port never became ready; check: docker compose -f $(TOPOCOMPOSE) logs"; \
			exit 1; \
		fi; \
	done
	@echo "topology up. authority-1 -> localhost:$(SERVICE_1_PORT), authority-2 -> localhost:$(SERVICE_2_PORT)"

## topo-deployment: record what the running topology is actually serving (§6.4 image identity)
#
# Inspects the live containers and writes the immutable image ID every unit must share, along
# with the address each one publishes. It is a separate step, not something alloca-load does,
# for two reasons: a process cannot see which image wraps it, so the service could only repeat
# back an environment variable; and reading it needs the Docker socket, which is root on the
# host and the last thing a generator that must later move to separate compute should hold
# (§6.3).
#
# Pass the result to a run with `alloca-load -deployment <file>`. The addresses are what let it
# check that the record describes exactly the units the run is about to drive, before any
# measured request — so re-record after anything that recreates a container.
topo-deployment:
	@./test/scripts/record-deployment.sh

## itc-up: raise the Iteration C topology at ITC_GROUPS=1|2|4 with its matching placement map
#
# One target rather than three recipes, because the group count and the placement document are
# the pair a hand-run command gets wrong. A four-unit topology under a two-authority map refuses
# to boot — units 3 and 4 are assigned nothing — which is the *safe* failure. The dangerous one
# is the other direction: a one-unit topology under the four-authority map boots, serves org-a,
# and routes org-b/c/d at authorities that are not running. That is a run which looks alive and
# is measuring a quarter of the workload.
#
# ITC_GROUPS also picks the profile, so the two cannot drift apart. Ports are published for
# every unit the profile raises and no others.
itc-up: image
	@case "$(ITC_GROUPS)" in \
	  1) profile="" ;; \
	  2) profile="--profile g2" ;; \
	  4) profile="--profile g4" ;; \
	  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$(ITC_GROUPS)'"; exit 1 ;; \
	esac; \
	stale=""; \
	for n in 1 2 3 4; do \
	  if [ $$n -gt $(ITC_GROUPS) ]; then \
	    stale="$$stale service-$$n authority-$$n-db authority-$$n-migrate"; \
	  fi; \
	done; \
	if [ -n "$$stale" ]; then \
	  echo "removing units outside G$(ITC_GROUPS):$$stale"; \
	  docker compose -f $(TOPOCOMPOSE) --profile g2 --profile g4 rm -sfv $$stale >/dev/null; \
	fi; \
	echo "raising the $(ITC_GROUPS)-group topology with deploy/topology/placement-itc-g$(ITC_GROUPS).json"; \
	ALLOCA_IMAGE_TAG=$(ALLOCA_IMAGE_TAG) \
	ALLOCA_PLACEMENT_DOC=./placement-itc-g$(ITC_GROUPS).json \
	ITC_CPUS_A=$(ITC_CPUS_A) ITC_CPUS_B=$(ITC_CPUS_B) \
	ITC_CPUS_C=$(ITC_CPUS_C) ITC_CPUS_D=$(ITC_CPUS_D) \
	docker compose $(ITC_COMPOSE) $$profile up -d
	@ports=""; \
	for n in $$(seq 1 $(ITC_GROUPS)); do \
	  case $$n in 1) p=$(SERVICE_1_PORT) ;; 2) p=$(SERVICE_2_PORT) ;; \
	              3) p=$(SERVICE_3_PORT) ;; 4) p=$(SERVICE_4_PORT) ;; esac; \
	  ports="$$ports $$p"; \
	done; \
	for port in $$ports; do \
	  ok=0; \
	  for i in $$(seq 1 60); do \
	    if curl -fsS -m 2 "http://localhost:$$port/readyz" >/dev/null 2>&1; then \
	      echo "  service on $$port ready"; ok=1; break; \
	    fi; \
	    sleep 1; \
	  done; \
	  if [ $$ok -ne 1 ]; then \
	    echo "  service on $$port never became ready; check: docker compose -f $(TOPOCOMPOSE) logs"; \
	    exit 1; \
	  fi; \
	done; \
	echo "$(ITC_GROUPS)-group topology up on:$$ports"
	@ITC_GROUPS=$(ITC_GROUPS) ./test/scripts/itc-topology-check.sh

## itc-rehearse: raise the Iteration C topology with each shard group pinned to its own CPUs
#
# `itc-up` plus the cpuset overlay (ag-sept-pr4.md §2.14), and the layout check in front of both.
#
# **The check runs before the image build, not after it.** Docker refuses an out-of-range cpuset
# on its own, but only once it has built an image and started four database containers, and its
# message names neither the partition nor the file that sets the machine's CPU count. Ordering
# the check first is most of what the target is for.
#
# **Recursive $(MAKE) rather than prerequisites, so the order holds under `make -j`.** Checking
# the layout after the image has been built and the containers started would forfeit the reason
# the check exists, and parallel prerequisites are free to do exactly that.
#
# itc-up is re-invoked with the overlay appended rather than duplicated here, so the recipe that
# raises the topology stays in one place. `make itc-up` on its own still raises it unpartitioned.
#
# Pinning the units is only half of the partition: the generator is a host process and the
# scheduler will put it on the units' CPUs unless told not to. test/scripts/itc-run.sh confines
# it with taskset, which is why the generator's set is printed here rather than assumed.
itc-rehearse:
	@$(MAKE) --no-print-directory itc-layout
	@$(MAKE) --no-print-directory itc-up \
	  ITC_COMPOSE="-f $(TOPOCOMPOSE) -f $(REHEARSALCOMPOSE)"
	@echo
	@echo "partitioned rehearsal up at G$(ITC_GROUPS). The generator is NOT confined by this"
	@echo "target — it is a host process, and an unconfined one dissolves the partition:"
	@echo
	@echo "    make obs-rehearse ITC_CPUS_GENERATOR=$(ITC_CPUS_GENERATOR)"
	@echo "    ITC_GROUPS=$(ITC_GROUPS) ./test/scripts/itc-seed.sh"
	@echo "    make itc-deployment ITC_GROUPS=$(ITC_GROUPS) > test/observed/deployment.json"
	@echo "    ITC_GROUPS=$(ITC_GROUPS) ITC_CPUS_GENERATOR=$(ITC_CPUS_GENERATOR) ./test/scripts/itc-run.sh"
	@echo
	@echo "  generator/monitor CPUs: $(ITC_CPUS_GENERATOR)  (alloca-load, Prometheus, Grafana)"
	@echo "  headroom control:       rerun obs-rehearse AND itc-run.sh with a wider"
	@echo "                          ITC_CPUS_GENERATOR, so the whole measuring side moves"

## itc-layout: check the rehearsal's CPU partition against this machine, and print it
#
# Separate from itc-rehearse so the partition can be checked without raising anything — the
# useful thing to run first on a machine whose CPU count has just changed.
itc-layout:
	@ITC_GROUPS=$(ITC_GROUPS) \
	 ITC_CPUS_A=$(ITC_CPUS_A) ITC_CPUS_B=$(ITC_CPUS_B) \
	 ITC_CPUS_C=$(ITC_CPUS_C) ITC_CPUS_D=$(ITC_CPUS_D) \
	 ITC_CPUS_GENERATOR=$(ITC_CPUS_GENERATOR) \
	 ./test/scripts/itc-cpu-layout.sh

## itc-deployment: record the deployment of exactly the selected Iteration C topology
#
# record-deployment.sh defaults to the PR3b pair, which is wrong in both directions here: at G1 it
# looks for a unit that is not running and fails, and at G4 it records two units for a four-unit
# run. The second is the dangerous one — alloca-load compares the record against the units it
# addresses, so an under-recorded G4 is caught, but only after the operator has spent the time to
# find out. Deriving CONTAINERS from ITC_GROUPS removes the choice.
itc-deployment:
	@case "$(ITC_GROUPS)" in \
	  1|2|4) ;; \
	  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$(ITC_GROUPS)'" >&2; exit 1 ;; \
	esac; \
	units=""; \
	for n in $$(seq 1 $(ITC_GROUPS)); do units="$$units alloca-service-$$n"; done; \
	CONTAINERS="$$units" ./test/scripts/record-deployment.sh

## itc-down: stop every Iteration C unit, whichever profile raised it, and remove its volumes
#
# --profile g4 unconditionally: `down` must remove containers the *current* invocation might not
# have raised, and a profile-less down would leave units 2-4 running while reporting success.
itc-down:
	docker compose -f $(TOPOCOMPOSE) --profile g2 --profile g4 down -v --remove-orphans

## topo-down: stop the topology and remove its volumes
#
# -v because each run starts from a known fixture. A topology that kept its data between runs
# would make the first run of a session differ from the rest, which is the kind of difference
# that gets discovered halfway through interpreting a result.
topo-down:
	docker compose -f $(TOPOCOMPOSE) --profile g2 --profile g4 down -v --remove-orphans

## topo-ps: what the topology is doing, including the exited migration steps
topo-ps:
	docker compose -f $(TOPOCOMPOSE) ps --all
