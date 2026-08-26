# Workload catalog

**Status:** Living — stable workload definitions reused across experiments.  
**Scope:** define named synthetic workloads whose domain semantics and demand shape remain stable while architecture, topology, resource allocation, and implementation technique change.  
**Does not own:** experiment topology, acceptance criteria, run duration, infrastructure, or measured results. Those belong to the applicable validation plan, deployment/design documents, and measurement reports.

## Why this catalog exists

Alloca-Go changes architecture deliberately. A capacity result is more useful when a later experiment can apply the **same workload** to a different placement model, database technique, service topology, or elasticity mechanism and compare the resulting evidence without also changing the demand being tested.

A workload therefore describes **what demand the system receives**, not **where that demand is placed**. Validation documents select a workload and map it onto a topology for a particular question.

The catalog may contain different workloads for different purposes. Reuse is preferred when the old workload still represents the question; a new workload gets a new identifier when its semantics or demand shape materially change.

## Catalog rules

Each workload records:

- a stable identifier and purpose;
- participating organisations and whether their work is independent;
- request mix and domain constraints;
- how demand is distributed among participants;
- which inputs are fixed and which are sweep parameters;
- exclusions that prevent the workload from silently testing a different problem.

Topology, shard count, replica count, host shape, cloud provider, database placement, run duration, and an experiment's pass/fail criteria are **not workload properties**.

## WL-MUT-DISP-4 — four-organisation dispersed mutation capacity

**Purpose:** reusable mutation-heavy capacity workload for comparing how independent organisation work behaves as writable-resource architecture changes.

### Population

Four synthetic organisations participate:

- `org-a`
- `org-b`
- `org-c`
- `org-d`

The four organisations are logically equivalent for this workload. Each owns an independent seeded population of slots and users, and the **per-organisation population size is fixed for a comparison**. It is chosen once to be sufficient for the maximum intended `G4` run, then the same A/B/C/D populations are reused unchanged for `G1`, `G2`, and `G4`; topology-specific fixture resizing would change the workload and invalidate the scale-efficiency comparison. No organisation depends on another organisation's slot or user state.

### Demand shape

- mutation path only for the capacity population;
- equal offered-load share per organisation;
- same request semantics and generator policy for A, B, C, and D;
- **same-organisation user/slot pairing for every request**: a user from `org-a` books an `org-a` slot, and likewise for B, C, and D, independent of how those organisations are placed onto database authorities;
- fresh idempotency keys for fresh attempts;
- enough independent slot/user identities to keep the dispersed workload from collapsing accidentally into a one-hot-slot or one-hot-identity serialization test;
- offered load/concurrency is a **sweep parameter**, not baked into the workload identity.

The same-organisation pairing is load-bearing for cross-topology comparison. The existing
`multi-org-dispersed` generator deliberately exercises colocated cross-organisation booking and
remains useful correctness coverage, but its request-pair mix changes when placement groups change.
It is therefore **not** the implementation of `WL-MUT-DISP-4`.

The intended comparison preserves the workload semantics, fixed per-organisation fixture populations, and per-organisation demand model while topology and available resources change.

### Exclusions

This workload does **not** include:

- cross-organisation reserve attempts, whether colocated or cross-authority;
- deliberate hot-slot or hot-identity concentration;
- read-heavy or mixed read/write traffic;
- failure injection;
- service-replica scaling as an independent variable;
- a production-traffic representativeness claim.

Those are separate workload or validation questions and should not be folded into `WL-MUT-DISP-4` merely to expand one experiment.

### Current use

AG-Sept Iteration C selects `WL-MUT-DISP-4` for the shard-group capacity comparison. The Iteration C validation plan owns the explicit 1/2/4-shard-group placement matrix; this catalog remains unchanged if a later experiment applies the same workload to a different architecture.
