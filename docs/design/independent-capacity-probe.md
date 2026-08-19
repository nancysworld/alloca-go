# Independent capacity-unit probe

**Status:** Proposed — PR4c Stage 0 design for Iteration C.  
**Scope:** the smallest independently provisioned AWS experiment that can discriminate resource
coupling from per-unit/environment variance before AG-Sept spends time on a complete G1/G2/G4
capacity campaign.  
**Requirements:** `REQ-SCALE-4`, `REQ-EVID-1`, `REQ-EVID-2` in
[`../requirements/system-requirements.md`](../requirements/system-requirements.md).  
**Umbrella design:** [`horizontal-scaling.md`](horizontal-scaling.md) §12 and
[`deployment-architecture.md`](deployment-architecture.md) §13.  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).

This document owns the **probe-specific architecture** only. It does not redefine the full Tier-1
G1/G2/G4 capacity method, the meaning of `VAL-SCALE-5`, the milestone budget, or AWS as a production
target.

## 1. Why the probe exists

PR4b answered the local question more usefully than expected: the scheduler-partitioned workstation
could run the complete topology family, but its shard groups still shared one host write path. G1
then reproduced poorly while G4 drove the shared device differently, so changing topology also
changed how the common storage resource was exercised. The local experiment therefore could not
separate shard-group composition from shared-environment behaviour strongly enough to derive the
intended efficiencies.

That finding justifies independent provisioning, but it does **not** imply that an independent AWS
capacity unit will be stable. Independence removes resource **coupling** between capacity units; it
does not remove run-to-run variance inside one unit. A cloud unit could still vary materially from
one run to the next, and four independent units would not make a noisy denominator precise by
construction.

PR4c therefore starts with a cheap discriminating question before any full capacity matrix:

> When the same two capacity-unit designs are measured separately and then composed, does G2
> deliver an aggregate signal that is intelligible relative to what those same units delivered
> alone, and how much unit/environment variation is already visible?

The first result decides whether more measurement is worth buying.

## 2. Stage-0 topology

The smallest useful topology is two equivalent capacity-unit hosts plus generator/measurement
compute outside both units:

```text
                 generator / measurement host
                         /             \
                        v               v
             capacity unit A      capacity unit B
             +-------------+      +-------------+
             | alloca-go   |      | alloca-go   |
             | PostgreSQL  |      | PostgreSQL  |
             | own storage |      | own storage |
             +-------------+      +-------------+
```

Each capacity unit is one EC2 instance containing exactly one Alloca-Go service replica and one
PostgreSQL authority, matching `deployment-architecture.md` §13.1. The generator/monitor host is a
third instance and is never counted as serving capacity.

The current account's small Standard On-Demand allowance is sufficient in principle for a **2 + 2 +
1 vCPU shape**: two equivalent two-vCPU serving units and one one-vCPU measurement unit, if the
selected instance families fit the account quota. That is an implementation constraint, not a
durable instance-type choice. The capacity units should use a non-burstable shape when the quota
allows it; the generator may be a small/burstable instance because it is measurement infrastructure
whose headroom is observed directly.

If the quota cannot instantiate two equivalent serving units plus separate generator compute, the
probe does not collapse them onto one host. That would recreate the resource-coupling question it
exists to remove.

## 3. What "independent" means here

The two serving units must have no intentionally shared serving resource envelope:

- separate EC2 instances;
- separate kernels and CPU/memory allocations;
- separate PostgreSQL processes and database filesystems;
- separate EBS volume allocations/paths for the storage used by each unit;
- no shared Docker volume, host filesystem, or PostgreSQL writer;
- no generator/Prometheus process on either serving unit.

The two units may still share AWS provider infrastructure such as an Availability Zone, network
fabric, or underlying storage fleet. This experiment does not claim physical isolation from the
cloud provider. Those provider layers are part of the recorded environment and are bounded through
per-host evidence when they become plausible explanations.

The storage **shape** must be equivalent across A and B: same volume class, size and any explicit
IOPS/throughput configuration. Whether PostgreSQL uses a dedicated data volume or the instance's own
root-volume set is an implementation choice for Stage 0, but the choice must be identical for A and
B and each unit's storage allocation must remain separate.

## 4. Use the same two units separately, then together

Stage 0 deliberately reuses the same physical AWS instances rather than comparing G2 with an
arbitrary third host.

The three measured probes are:

```text
G1-A      unit A alone, one authority owns organisations A/B/C/D
G1-B      unit B alone, one authority owns organisations A/B/C/D
G2-A+B    unit A owns A/B; unit B owns C/D
```

`G1-A` and `G1-B` are two observations of the **capacity-unit design on two independent units**, not
two repeated runs of one host. `G2-A+B` then composes exactly those two units. Placement is reset
between cells; persisted state is not carried from one topology into another.

This paired shape answers a stronger diagnostic question than `G2 / (2 x one arbitrary G1)` because
one unusually fast or slow baseline host cannot silently become the denominator for both units.

It does **not** change the canonical Tier-1 definition yet. Stage 0 exists to learn whether a later
Tier-1 experiment needs more per-unit baseline replication; that decision is made from the probe
rather than assumed in advance.

## 5. Probe workload and run shape

Stage 0 reuses the qualified Iteration C semantics so the only intentional architecture change is
the resource envelope:

- workload: `WL-MUT-DISP-4`;
- one service replica + one PostgreSQL authority per active shard group;
- independent `workers_per_group` streams;
- same service image, PostgreSQL version/configuration, timeout policy and placement semantics;
- `pool_max_conns=8` as the qualified local starting policy;
- explicit state-based conditioning followed by the state-preserving service/pool recycle;
- the same fixture/state accounting and reconciliation rules as PR4a/PR4b;
- no Grafana/query workload during a measured interval.

The probe is intentionally **short and non-canonical**. Validation fixes a 120 s measured window and
one common probe level `P`, initially `workers_per_group=12` from the local common bracket. The three
cells must use the same `P`; otherwise the derived composition ratio would mix topology with demand.

`pool_max_conns=8` and `P=12` are inherited starting points, not claims that AWS has the same
frontier as the workstation. If the first AWS evidence shows either parameter is an obvious
measurement limiter, Stage 0 may be re-driven after the reason is recorded. It does not silently
turn into a tuning matrix.

## 6. Evidence retained per unit

Every interpreted probe keeps the same evidence classes needed to distinguish a server result from
an environment result:

- run manifest, source revision, deployed image identity and placement assignment;
- EC2 instance shape, vCPU count and memory allocation;
- storage class/configuration and the fact that A/B use separate allocations;
- host CPU, memory, disk throughput/utilisation/queueing and network counters for each serving unit;
- service/runtime, pool and PostgreSQL panels per authority;
- conditioning boundary and start/end persisted-state evidence;
- generator CPU/network/resource evidence and request accounting;
- clock-synchronisation evidence sufficient to align the measured window;
- final per-authority reconciliation.

Account identifiers, credentials, private keys and other secret/private cloud values are not part of
reproducibility and are not committed.

## 7. Stage-0 outputs

For the common probe level `P`, report the three full-window fresh-mutation Goodput observations:

```text
g1_A(P)
g1_B(P)
g2_AB(P)
```

Report the two-unit baseline difference explicitly, for example as the symmetric relative spread:

```text
U1_probe = |g1_A - g1_B| / mean(g1_A, g1_B)
```

and derive the paired composition ratio:

```text
R2_probe = g2_AB(P) / (g1_A(P) + g1_B(P))
```

`R2_probe` is **diagnostic only**. It is not `E2_aws`, does not establish saturation, does not
establish a capacity multiplier, and cannot discharge `VAL-SCALE-5`. The raw per-unit values are
reported beside it so the ratio cannot hide a noisy or asymmetric denominator.

Where possible, also retain G2's per-authority contribution so an aggregate that looks reasonable
cannot hide one constrained unit behind another.

## 8. There is no precision target for Stage 0

PR4b demonstrated why precision must not be assumed from one environment. Stage 0 therefore has no
5%, 10%, or other pass threshold for `U1_probe` or `R2_probe`.

The decision is architectural and prospective:

- **clear composition signal + intelligible A/B behaviour** → a longer/repeated independent
  experiment may be worth its quota and time;
- **large A/B variation** → first decide whether repeated baselines or a better-controlled AWS
  resource shape can resolve the denominator; do not manufacture precision by averaging an
  unplanned population;
- **G2 materially below what A+B suggest** → inspect per-unit resource and workload evidence before
  deciding whether the architecture, storage/network environment, generator, or configuration is
  responsible;
- **generator/provenance/reconciliation failure** → the probe is uninterpretable, regardless of its
  throughput number.

A single ambiguous result permits at most a bounded follow-up chosen after the evidence is seen. It
does not automatically launch the complete Tier-1 matrix.

## 9. Relationship to Tier 1

The full independent-capacity claim remains exactly where the existing architecture and validation
plan put it: a complete equivalent G1/G2/G4 environment with the retained capacity method,
reproducibility gates, resource-envelope control and independent generator compute.

Stage 0 is a **pre-Tier diagnostic**. Its value is to answer whether the independently provisioned
environment is promising enough to justify that campaign and, if it is, which experimental risk to
address first. If quota remains too small for G4, Stage 0 may still produce useful architecture
evidence, but `VAL-SCALE-5` remains explicitly unproven.
