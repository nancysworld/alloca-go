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
        image topo-up topo-down topo-ps

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
# The PR3b two-authority topology: two PostgreSQL authorities, two shard-affine service
# units, one placement document. Separate from the observability stack so a topology can be
# raised and torn down without disturbing whatever is scraping it.
TOPOCOMPOSE  ?= deploy/topology/docker-compose.yml
# Immutable experiment tagging (§9.1). Defaults to the working tree's commit so a run's
# artifacts name an image that can be rebuilt; `-dirty` when the tree has uncommitted changes,
# which is a warning that the image is not reproducible from any commit.
#
# The dirty test is `git status --porcelain`, not `git diff --quiet`, for one reason: it must
# agree with the flag the *binary* carries. `go build` decides `vcs.modified` from
# `git status --porcelain` being non-empty, so it counts staged and untracked files, which
# `git diff --quiet` — worktree against index — does not see. A tag reading clean on an image
# whose /meta reports modified=true is worse than no tag: it is the one field an operator
# would use to decide the run is reproducible.
#
# **`-dirty` is a warning, not an identifier.** Two different uncommitted trees both tag
# `abc1234-dirty`, and the second build silently replaces the first under that tag — so two
# runs carrying the same dirty tag are not known to have used the same image, and must never
# be compared on the strength of it. Only the clean form satisfies §9.1's "an image that can
# be rebuilt". That is consistent with the manifest, which refuses `service_source_modified`
# at `local` and so declines to quote a dirty run at any level; the tag is what tells an
# operator *why* before they get that far.
#
# Note this counts *any* uncommitted change, including one to a document that cannot affect
# the binary. That is deliberate: `modified` is a claim about whether the tree matched the
# commit, and narrowing it to "changes I judge to affect the build" would report clean for a
# tree that is not. Commit or stash before building an image meant for an experiment.
ALLOCA_IMAGE_TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell test -z "$$(git status --porcelain 2>/dev/null)" || echo -dirty)
SERVICE_1_PORT   ?= 8081
SERVICE_2_PORT   ?= 8082

all: ci

## ci: run the full local gate, identical to CI (fmt, vet, lint, build, test, race)
ci: fmt-check vet lint build test test-race build-context-check

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

## obs-up: start Prometheus + Grafana for a measured run (ag-sept-plan §14 PR2)
#
# Prometheus scrapes the service on the *host*, not in this compose project: PR2 measures a
# locally built binary and containerising the service is PR3's variable, not PR2's.
#
# Grafana is provisioned from the repo, so there is nothing to click: the dashboard and its
# datasource exist on first start. It is the diagnostic view only — the evidence a report
# quotes is the CSVs and TSDB snapshot the sweep runner retains.
obs-up:
	docker compose -f $(OBSCOMPOSE) up -d
	@# Probe for an address that actually reaches the service, rather than assuming one.
	@# Tolerated on failure: the stack is still useful with the service down, and the script
	@# prints what to do. Re-run `make obs-target` once the service is up.
	@./test/scripts/obs-target.sh || true
	@echo "prometheus  http://localhost:9091"
	@echo "grafana     http://localhost:3000/d/alloca-frontier"

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
	ALLOCA_IMAGE_TAG=$(ALLOCA_IMAGE_TAG) docker compose -f $(TOPOCOMPOSE) up -d
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

## topo-down: stop the topology and remove its volumes
#
# -v because each run starts from a known fixture. A topology that kept its data between runs
# would make the first run of a session differ from the rest, which is the kind of difference
# that gets discovered halfway through interpreting a result.
topo-down:
	docker compose -f $(TOPOCOMPOSE) down -v --remove-orphans

## topo-ps: what the topology is doing, including the exited migration steps
topo-ps:
	docker compose -f $(TOPOCOMPOSE) ps --all
