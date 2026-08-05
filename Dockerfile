# Production-shaped image for alloca-go, to the container gate in ag-sept-plan-new.md §9.1:
# environment-driven configuration, health probes, graceful termination, and no
# development-only tooling in the runtime layer.
#
# Two stages. The builder carries the Go toolchain and the module cache; the runtime carries
# the binary and nothing else — no shell, no package manager, no compiler. A runtime layer
# with a shell is a debugging convenience that becomes a permanent attack surface, and an
# image that can rebuild itself is one whose contents are no longer what CI tested.

# The builder image is pinned to go.mod's own `toolchain` line, not to a floating major.
# go.mod records the exact compiler CI and every measurement use, precisely so a benchmark
# comparison never silently changes compiler patch versions — an image on a different one
# would reintroduce the drift that line exists to prevent. Bump them together.
FROM golang:1.26.5-alpine AS builder

# git is needed at *build* time only: `go build` reads the VCS revision from the repository
# and stamps it into the binary, which is what /meta reports and what the load harness records
# as the identity of the code under test. Without it every run would be uncertifiable
# (ag-sept-plan-new.md §6.4).
RUN apk add --no-cache git

WORKDIR /src

# Modules first, as their own layer: dependencies change far less often than source, so a
# source-only edit reuses the download instead of repeating it.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off so the result is a static binary the runtime stage can hold without libc.
#
# -trimpath keeps build-host paths out of the binary: they differ between machines and would
# make two builds of the same commit compare unequal, which is the opposite of what an
# immutably tagged experiment image is for.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -o /out/alloca-go ./cmd/alloca-go && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -o /out/alloca-migrate ./cmd/alloca-migrate

# --- runtime ---------------------------------------------------------------------------
#
# static-debian12 rather than a distro base: no shell, no package manager, and a non-root
# user by default. `nonroot` is uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot

# Both binaries ship in one image. alloca-migrate is a *separate step*, never run by the
# serving process (ADR-0002): several replicas racing the same DDL at rollout is a classic
# way to take a service down. Shipping them together means the migration that runs and the
# binary that serves come from one build, so the schema gate's exact-version check
# (INV-27) compares a version against the migrations its own build carries.
COPY --from=builder /out/alloca-go /usr/local/bin/alloca-go
COPY --from=builder /out/alloca-migrate /usr/local/bin/alloca-migrate

USER nonroot:nonroot

# Documentation only — publishing is the orchestrator's decision. 8080 serves the booking
# and operational surface; 9090 serves /metrics on its own listener so a scrape stays
# reachable when the booking port is saturated, which is exactly when an experiment most
# wants it.
EXPOSE 8080 9090

# No HEALTHCHECK directive: /healthz and /readyz are the operational contract, and Compose
# and Kubernetes both express probes better than Docker's built-in can. Encoding one here
# would put a second, weaker definition of "healthy" in the image.

ENTRYPOINT ["/usr/local/bin/alloca-go"]
