# 0003 — A run identifies its deployed artifact by observation, not by self-report

**Status:** Proposed (AG-Sept PR3b)
**Date:** 2026-08-06
**Milestone:** AG-Sept

## Context

Every quotable run must identify both the code that answered its requests and, when the
service is containerised, the deployed artifact that carried that code. Those are different
facts and neither implies the other.

**The code.** `service_commit_sha` is build-observed rather than operator-asserted: the Go
build command queries Git and stamps the visible revision and modified state into the binary,
and `/meta` reports that stamp. The observation is only as trustworthy as its build context.
The builder must receive the complete tracked tree and no undeclared build-affecting inputs;
otherwise the visible Git state may not describe the binary that was produced.

**The deployed artifact.** The commit SHA says nothing about the image wrapping the binary.
The same source revision may appear in different images because a stale mutable tag still
points at an older build, a base layer changed, or another build input changed. Request totals
and `/meta` cannot distinguish those artifacts.

The AG-Sept plan originally required both a commit SHA and an image tag and staged image
identity to PR3b. `Manifest.ImageTag` existed but was never assigned, while a comment defended
that omission by citing an allowance that existed in neither the current nor superseded plan.
The wording was also technically wrong: a tag is a mutable alias, not an immutable artifact
identity.

Three constraints shape the decision:

1. **A process cannot observe which image wraps it.** It can only repeat a value supplied to
   it, so an image identity reported through `/meta` would be asserted provenance.
2. **The generator remains credential-free and HTTP-only.** A Docker socket gives control of
   the host and must not be added merely to collect provenance.
3. **PR3b uses locally built images without a registry.** A registry digest is therefore not
   available, but Docker gives a running container an immutable local image ID.

## Decision

**A container-served run identifies its deployed artifact through a host-side observation of
the live service containers. The observation is completed and validated before any measured
request is sent, and the normalized per-unit result is retained in the run manifest.**

The observer records, for every service unit in the run:

- the unit identity needed to bind it to the routed HTTP target;
- the immutable local image ID, or a registry digest when one exists;
- optional human-readable aliases such as an image tag;
- enough observation metadata to make a stale or mismatched record diagnosable.

Before workload traffic begins, the harness must establish a one-to-one correspondence between
that observed service-unit set and the service targets selected by the run's routing topology.
Missing units, extra units, duplicate bindings, or units running different artifact identities
make the run invalid. The per-unit observation remains in the report so the evidence that all
units were inspected is not separated from the result.

The observation is produced by host-side tooling and passed to the generator as a file. The
Docker socket stays on the service host; the generator reads only the resulting record. This
preserves the HTTP-only boundary and continues to work when the generator moves to separate
compute.

Three alternatives are rejected:

- **No self-report via `/meta`.** Supplying a tag or digest through an environment variable
  and having the service repeat it would present asserted provenance beside the build-observed
  source identity without making the difference visible.
- **No Docker access in the generator.** Artifact observation does not justify giving the
  load generator host-control credentials.
- **No tag as identity.** A tag may be retained as an operator-facing alias, but no claim
  rests on it. The authoritative field is a content-addressed image ID or registry digest.

Deployment mode is explicit. A container deployment requires a valid deployment observation;
omitting it must not silently reinterpret the run as source-hosted. A source-hosted run has no
image to identify, but that mode must be declared rather than inferred from an absent field.
An unknown deployment mode or a missing required observation prevents the run from starting,
or makes it unquotable at every level.

The implementation mechanism is owned by
[`internal/loadgen/deployment.go`](../../internal/loadgen/deployment.go) and
[`test/scripts/record-deployment.sh`](../../test/scripts/record-deployment.sh); the operating
procedure is owned by
[`../operations/container-topology.md`](../operations/container-topology.md) §7. This ADR owns
the architectural contract, not their current file formats or command-line syntax.

## Consequences

**The plan uses image ID or digest, not image tag.** AG-Sept §6.4 and §9.1 distinguish source
identity from deployed-artifact identity. Tags are aliases only.

**Container experiments gain a mandatory preflight.** The live deployment is observed and
matched to the routed service targets before load begins. Forgetting or failing that step is a
loud invalid configuration, not a lower-provenance run that still retains the local evidence
level.

**One experiment uses one artifact identity.** Units running different images, or an
observation describing a different unit set from the run, are refused before measured
traffic. Canary and rolling-replacement experiments require a different explicit contract.

**The observation is not self-authenticating.** An operator can still hand-write or alter the
file. The claim is therefore precise: the documented path observes the live deployment rather
than asking the service to assert its wrapper. Stronger guarantees would require signed
attestation or an independent observer.

**An image ID identifies what ran; it does not prove reproducibility.** A Docker image ID is a
local content address, resolvable only while that image remains available on the machine. It
is not globally retrievable and does not establish that rebuilding the same commit will
produce identical bytes. Exact rebuild reproducibility is a separate concern involving base
image digests and every other build input.

**Source identity also has a declared trust boundary.** The VCS stamp is observed by the Go
build command, but the container build must exclude undeclared ignored inputs and include the
complete tracked tree. Build-context controls enforce that precondition; the stamp alone does
not.

## Revisit when

- **Images are published to a registry.** Replace the local image ID with the registry digest,
  which is globally resolvable and can travel with a deployment beyond one Docker host.
- **The generator moves to separate compute.** Keep observation on the service host and move
  the resulting deployment record with the run, or move independent observation to the
  verifier if that produces a cleaner trust boundary.
- **A deployment intentionally serves several artifact identities.** Canary, blue/green, or
  rolling-replacement experiments need a manifest and interpretation that model each cohort
  explicitly rather than weakening this ADR's one-artifact rule.
- **Evidence must resist operator modification.** Introduce signed provenance, registry
  attestations, or independent re-observation when the threat model requires more than an
  operationally observed record.
