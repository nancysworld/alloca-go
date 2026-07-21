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

.PHONY: all ci fmt fmt-check vet lint build test test-race run tidy tools clean

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

## run: build and run the service
run:
	$(GO) run $(CMD)

## tidy: tidy the module graph
tidy:
	$(GO) mod tidy

## clean: remove build artifacts and provisioned tools
clean:
	rm -f $(BINARY)
	rm -rf $(TOOLBIN)
	$(GO) clean
