# 0003 — A run identifies its deployed artifact by observation, not by self-report

**Status:** Proposed
**Date:** 2026-08-06
**Milestone:** AG-Sept

## Context

A quotable experiment must identify what it measured. For a deployed service there are
two distinct identities:

- **code identity** — the source/build revision represented by the running binary;
- **deployed-artifact identity** — the concrete artifact that carried that binary into
  the measured deployment.

Neither identity implies the other. The same source revision can appear in different
deployed artifacts because build inputs, base layers, packaging, or deployment state may
differ.

The process itself is not an authoritative observer of the artifact that wraps it. A
containerised service can repeat an image tag or digest supplied through configuration,
but that is self-report of an asserted value, not observation of the deployed artifact.
Likewise, a mutable label such as an image tag cannot serve as artifact identity because
it can be reassigned independently of artifact content.

The evidence model therefore needs an identity for the deployed artifact that is both
appropriate to the deployment environment and observed from outside the measured process.

## Decision

For quotable deployed-service experiments, Alloca records **code identity and deployed-
artifact identity as separate provenance facts**.

Deployed-artifact identity is obtained through **deployment-external observation**: a
layer capable of observing the artifact that is actually deployed supplies the identity.
The measured service does not establish authoritative artifact provenance by reporting a
value injected into itself.

For content-addressable deployment artifacts, the authoritative identity is an immutable,
content-addressed identifier appropriate to that environment, such as a container image ID
or registry digest. Human-readable tags or deployment labels may be recorded as aliases but
do not substitute for artifact identity.

The mechanism used to observe, transport, bind, validate, and record that identity is an
implementation concern. The current AG-Sept implementation is documented in
[`../development/implementation/ag-sept-pr3.md`](../development/implementation/ag-sept-pr3.md), with operating
procedure in [`../operations/container-topology.md`](../operations/container-topology.md).

## Consequences

**Provenance crosses an explicit observation boundary.** Code identity may be observed by
the build/runtime path, while deployed-artifact identity requires an observer outside the
measured process. A deployment cannot claim artifact provenance merely because the service
repeats an operator-supplied value.

**Tags are aliases, not evidence identities.** Operationally convenient names remain useful,
but claims about what ran rest on an immutable artifact identifier.

**Artifact identity is not reproducibility.** Identifying the exact artifact that ran does
not establish that the same source revision can later reproduce identical bytes. Build
reproducibility is a separate property with its own inputs and controls.

**Observation is not attestation.** Deployment-external observation strengthens provenance,
but does not by itself make the resulting record cryptographically trustworthy. Stronger
threat models may require signed attestations or independent verification.

**Heterogeneous deployments need an explicit evidence model.** A deployment intentionally
serving several artifact identities, such as a canary or rolling replacement, cannot be
silently represented as one artifact. Its evidence model must identify the participating
cohorts explicitly.

## Rejected alternatives

- **Service self-report as authoritative artifact provenance.** The process cannot
  independently observe the deployment wrapper whose identity it is being asked to prove.
- **Mutable image tags or deployment labels as artifact identity.** Their binding to content
  can change without the artifact changing, or vice versa.
- **Conflating source revision with deployed artifact.** Source identity does not uniquely
  identify packaging and deployment content.

## Revisit when

Reopen this decision when:

- a deployment model has no suitable externally observable immutable artifact identity;
- the project requires cryptographically verifiable provenance rather than operational
  observation;
- heterogeneous-artifact experiments become a normal deployment mode and need a richer
  evidence model; or
- repository-local evidence shows that the observation boundary materially interferes with
  the experiment it is intended to qualify.

A future accepted ADR supersedes this decision rather than rewriting it.
