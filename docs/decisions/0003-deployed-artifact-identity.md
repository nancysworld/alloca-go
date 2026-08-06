# 0003 — A run identifies its deployed artifact by observation, not by self-report

**Status:** Accepted (AG-Sept PR3b)
**Date:** 2026-08-06
**Milestone:** AG-Sept

## Context

Every quotable run must record what it measured. Two facts are needed and only one was
being captured.

**The code.** `service_commit_sha` comes from the VCS revision `go build` stamps into the
binary, read back from `/meta`. It is *observed*: the compiler runs git during the build,
and nothing has to be trusted for the value to be right.

**The artifact.** The commit SHA says nothing about the image wrapping the binary. The same
code served from a stale `:dev` tag left pointing at an older build, or rebuilt on a newer
base layer, carries an identical SHA on every unit — and nothing in the request totals would
reveal the difference. A run in that state reports one identity for a deployment that has
two.

The plan required both (`ag-sept-plan-new.md` §6.4, "commit SHA and image tag") and staged
image identity to PR3b. `Manifest.ImageTag` existed, was never populated by anything, and
carried a comment justifying the gap by citing an allowance — "§14 allows it to be
conditional for a local source build" — that exists in neither the current plan nor the
superseded one. The old plan says the opposite for the same PR.

Three constraints shaped the options:

1. **A process cannot see which image wraps it.** Nothing inside the container can read its
   own image identity; it can only be told.
2. **§6.3 keeps the generator credential-free**, holding no database access and speaking
   only HTTP, so that it can move to separate compute without changing. A Docker socket is
   root on the host — the strongest credential available.
3. **PR3b builds locally and publishes nothing.** A registry digest exists only after a
   push; a locally built image has an immutable content ID immediately.

## Decision

**The identity of the deployed artifact is observed from the host by inspecting the running
containers, recorded as a file, and folded into the manifest by the generator.**

`test/scripts/record-deployment.sh` reads the image ID each running unit was created from,
requires them to agree, and writes a deployment record. `alloca-load -deployment <file>`
puts it in the manifest as `image_id`, with `container_deployment` set.

Three things follow from that, each rejecting an alternative that was considered:

- **Not self-reported via `/meta`.** The service could be handed its tag as an environment
  variable and report it, which is the obvious design and the one to expect someone to
  propose again. It is refused because the value would be asserted, not observed, and would
  sit beside the compiler-observed commit SHA under names that do not admit the difference.
  That is the shape of the PR1 defect where `commit_sha` named the generator rather than the
  service under test — provenance that looks trustworthy and is not.
- **Not read by the generator itself.** That would need the Docker socket, breaking §6.3 far
  more seriously than a database credential would. The observation is taken where the
  privilege already exists and travels as a file; the file crosses the boundary, the socket
  does not. This also keeps working once the generator is on separate compute.
- **Not the tag.** `image_id` is the immutable content identity and is what a claim rests
  on. `image_tag` is recorded as a human-readable alias and is never required: a tag is
  mutable, two builds can wear the same one, and the second silently replaces the first.

The requirement is **conditional on `container_deployment`**. A run built and served from
source has no image to name, and demanding one would refuse every local run.

The mechanism is owned by [`internal/loadgen/deployment.go`](../../internal/loadgen/deployment.go)
and [`test/scripts/record-deployment.sh`](../../test/scripts/record-deployment.sh); the
procedure by [`../operations/container-topology.md`](../operations/container-topology.md) §7.
This ADR does not restate either.

## Consequences

**The plan changed.** §6.4 and §9.1 now say image ID or digest rather than tag, make the
requirement conditional on container deployment, and state that the value is observed rather
than reported. The unsupported §14 justification is removed.

**A containerised run gains a step.** `make topo-deployment` must run against the live
topology before the load run, and its output passed with `-deployment`. Forgetting it does
not corrupt a run — `container_deployment` stays false and the manifest simply does not
claim an image — but the run cannot then support a capacity claim about a containerised
deployment.

**Units on different images are now a refusal rather than an invisible condition.** This is
the case the commit SHA could not see, and it fails before a run rather than after.

**What this does not establish.** The record cannot prove itself. An operator who
hand-writes the file gets whatever they wrote, exactly as `generator_location` is a
declaration the manifest takes at face value. The claim is narrower and worth stating
plainly: *on the documented path the value is read from the running deployment rather than
asserted about it.* Closing that would need the verifier to re-observe independently, which
buys little while both run on one workstation.

**Registry digests are not used**, because nothing is published yet. `image_id` is a
Docker-local identity: reproducible on the machine that built it, not a globally resolvable
name.

## Revisit when

- **Images are published to a registry.** Once a push exists, the identity should become the
  registry digest, which is globally resolvable where an image ID is not. The plan already
  says "image ID or digest" so that no further amendment is needed — only the observer and
  the record change.
- **The generator moves to separate compute (§6.3).** The file-passing design is chosen to
  survive this, but the step that produces the file must then run on the service host and
  the artifact travel with the run. If that proves awkward, the alternative is a verifier-
  side observation rather than a generator-side one.
- **A deployment gains units that are legitimately not identical** — a canary, or a rolling
  replacement mid-run. The current record requires one image across all units, which is
  right for an experiment and wrong for a deployment being upgraded. That is a different
  experiment and should say so rather than loosening this check.
