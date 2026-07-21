# Local mirror of the CI gate (.github/workflows/ci.yml). Running `make ci`
# reproduces what CI enforces, satisfying the AG-M0 "build and test reproducibly"
# exit criterion locally as well as in CI.

GO      ?= go
PKGS    ?= ./...
BINARY  ?= alloca-go
CMD     ?= ./cmd/alloca-go

.PHONY: all ci fmt fmt-check vet lint build test test-race run tidy clean

all: ci

## ci: run the full local gate (matches CI)
ci: fmt-check vet build test test-race

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

## lint: run golangci-lint (requires golangci-lint installed locally)
lint:
	golangci-lint run

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

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	$(GO) clean
