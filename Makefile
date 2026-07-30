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
        db-up db-down migrate run dev smoke tidy tools clean

# Integration tests need a real PostgreSQL: the properties they prove (capacity safety
# under concurrent transactions, post-lock decision time, the scoped-key race) do not
# exist without one. They are behind the `integration` build tag so the default gate
# stays hermetic and fast.
DATABASE_URL ?= postgres://alloca:alloca@localhost:55432/alloca?sslmode=disable
# Where `make smoke` looks for a running service, and how long each of its requests waits.
# Raise MAX_TIME when the service is paused in a debugger: it is curl's own patience, so no
# server-side deadline affects it.
BASE         ?= http://localhost:8080
MAX_TIME     ?= 10
PGCONTAINER  ?= alloca-pg
PGIMAGE      ?= postgres:16-alpine
PGPORT       ?= 55432

all: ci

## ci: run the full local gate, identical to CI (fmt, vet, lint, build, test, race)
ci: fmt-check vet lint build test test-race

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
dev:
	$(MAKE) db-up
	$(MAKE) migrate
	$(MAKE) run

## smoke: exercise a running service over a real socket (needs `make dev` elsewhere)
smoke:
	@BASE="$(BASE)" MAX_TIME="$(MAX_TIME)" DATABASE_URL="$(DATABASE_URL)" ./scripts/smoke.sh

## tidy: tidy the module graph
tidy:
	$(GO) mod tidy

## clean: remove build artifacts and provisioned tools
clean:
	rm -f $(BINARY)
	rm -rf $(TOOLBIN)
	$(GO) clean
