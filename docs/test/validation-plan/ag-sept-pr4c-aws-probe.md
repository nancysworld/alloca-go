# AG-Sept PR4c — AWS independent-unit probe

**Status:** Scheduled diagnostic validation for PR4c.  
**Purpose:** obtain the smallest useful independent-provisioning result before committing AG-Sept
time or quota to any different or larger experiment.  
**Design:** [`../../design/independent-capacity-probe.md`](../../design/independent-capacity-probe.md).  
**Governing validation plan:** [`ag-sept-validation-plan.md`](ag-sept-validation-plan.md) §4.6,
`VAL-SCALE-5`, `VAL-NEG-7`, `VAL-NEG-8`.  
**Measurement contract:** [`../../design/measurement-contract.md`](../../design/measurement-contract.md).

This is a **diagnostic probe, not a new route to `VAL-SCALE-5`**. A partial AWS topology remains a
partial topology. PR4c may provide evidence that a different future experiment would be worthwhile;
it cannot be renamed Tier 1 or Tier 2 because its result looks good.

## 1. Question

At one common non-canonical worker level, do two equivalent independently provisioned capacity units:

1. show individually intelligible behaviour when each is used as G1; and
2. when composed as G2, produce aggregate Goodput that can be interpreted relative to what those
   **same two units** produced separately?

The probe also asks how much unit/environment variation is already visible. It deliberately does not
ask for a precise efficiency estimate.

## 2. Preconditions

No interpreted probe begins until all of these hold:

- capacity units A and B are separate EC2 instances and use the same serving instance shape;
- A and B use separate storage allocations/paths with the same declared storage shape;
- one service replica and one PostgreSQL authority run on each active unit;
- the load generator/measurement stack runs on separate EC2 compute, never on A or B;
- A and B run the same Alloca-Go image, PostgreSQL version/configuration, schema version, pool policy,
  timeout policy and OS/bootstrap shape relevant to the measurement;
- the placement document and unit metadata agree on the active topology before load starts;
- node/service/PostgreSQL evidence required below is populated for every active serving host;
- clocks are synchronised sufficiently for the generator measurement window and host series to be
  aligned;
- the generator can reach every active unit and the verifier can reach every active authority;
- credentials, private keys and account-specific secrets are supplied outside retained artifacts.

A failure here costs no measured run. It is a provisioning/bootstrap failure to fix or record, not
a low capacity observation.

## 3. Common probe configuration

The first pass fixes:

```text
workload                 WL-MUT-DISP-4
workers_per_group (P)    12
measured window           120 s
pool_max_conns            8 per shard group
conditioning target       4,000 fresh mutations / organisation
fixture                    45,000 slots / organisation, capacity 20
service replicas/group     1
PostgreSQL authorities     1 / active group
```

`P=12` is the local experiment's common selected point and is used here only as a cheap first offered
closed-loop intensity. The 120 s window deliberately matches reconnaissance-scale cost, not the
canonical 600 s capacity horizon.

The three cells use **exactly the same P, duration, fixture, conditioning target, pool policy,
service image, PostgreSQL configuration and timeout policy**. If the evidence shows P or the pool is
an obvious measurement limiter, record that finding before choosing one bounded replacement pass;
do not tune one topology independently.

## 4. Run sequence

Drive exactly these first three measured cells:

### 4.1 `G1-A`

- capacity unit A active;
- one explicit sharded authority owns organisations A/B/C/D;
- capacity unit B is not serving the workload;
- fresh reset/reseed, conditioning to the common state target, state-preserving service/pool recycle,
  pre-measurement checks, 120 s measured interval, reconciliation.

### 4.2 `G1-B`

Same topology and procedure as `G1-A`, but capacity unit B is the serving unit. This is an
independent-unit baseline, not a repeat of A.

### 4.3 `G2-A+B`

- capacity units A and B active together;
- A owns organisations A/B;
- B owns organisations C/D;
- each group receives its own `workers_per_group=P` stream under the already established
  `VAL-NEG-8` generator design;
- fresh reset/reseed/conditioning on both authorities, state-preserving recycle, checks, 120 s
  measured interval, reconciliation.

State from either G1 cell is never reused in G2.

## 5. Evidence retained for every interpreted cell

Each cell retains enough evidence to answer both the system and environment sides of the question:

- `run.json`/equivalent manifest with workload, P, duration, fixture, placement, service revision and
  deployed image identity;
- observed serving-unit metadata proving the intended active unit set and one image/schema contract;
- EC2 serving shape and observed vCPU/memory resources for every active unit;
- storage shape and per-unit identity sufficient to show that A and B do not share one data volume;
- host CPU, memory, disk read/write throughput, disk utilisation/queue depth and network series per
  active serving unit;
- service/process, pool and PostgreSQL panels per authority;
- generator process/host resource series and per-group request/outcome accounting;
- conditioning population and measured-start/end persisted-state evidence;
- per-authority reconciliation and final logical-mutation accounting;
- enough clock evidence to bind all series to the same measured interval.

A graph is optional; the retained machine-readable series are not.

## 6. Admissibility checks

A cell is **uninterpretable** when any of these fails:

- response validation or measurement soundness;
- intended run duration/configuration does not match what the cell actually ran;
- placement or active unit set differs from the requested topology;
- fixture exhaustion or unexpected policy refusals alter the workload;
- conditioning/reconciliation populations do not reconcile;
- one required host/resource series is absent;
- A and B are not equivalent in the declared capacity-unit configuration;
- the generator is a plausible limiter at the observed request rate;
- an active serving unit is materially constrained by a provisioning fault that is not part of the
  intended capacity-unit shape.

The probe does **not** require the 5% PR4b reproducibility gate because it contains one observation
per unit/topology and does not claim capacity. The absence of that gate is exactly why no probe rate
may be promoted to `G1_aws`, `G2_aws` or `E2_aws`.

## 7. Derived diagnostic quantities

Let the full-120 s fresh-mutation Goodput observations be:

```text
g1_A(P)
g1_B(P)
g2_AB(P)
```

Report all three raw observations first.

Then derive:

```text
U1_probe = |g1_A - g1_B| / ((g1_A + g1_B) / 2)
R2_probe = g2_AB / (g1_A + g1_B)
```

`U1_probe` describes the difference between the two nominally equivalent AWS units in this one pass.
It is **not** an estimate of run-to-run variance or a confidence interval.

`R2_probe` asks whether the two actual units, when composed, deliver aggregate Goodput commensurate
with the sum of the observations those same units produced separately. It is labelled `[DERIVED]`
and **must never be called `E2_aws`**.

Where the harness exposes per-authority G2 Goodput, report A and B separately beside the aggregate.
A balanced sum and an asymmetric sum are different architecture evidence.

## 8. Interpretation and decision after the first result

There is no numerical pass/fail threshold. PR4c records one of three explicit decisions:

### PROCEED

Use when A/B behaviour is sufficiently intelligible and the G2 composition signal is sufficiently
clear that a different, longer or repeated independent measurement **may** be worth proposing.
`PROCEED` creates no follow-on stage, budget or execution commitment.

### BOUNDED REPEAT

Use when one small repeat can discriminate a specific ambiguity — for example whether a surprising
A/B difference repeats, or whether the one-vCPU generator was the limiter. Name the hypothesis and
the killing observation before the repeat. Do not start a generic replication campaign. The repeat
must remain inside PR4c's existing 1.0-day allocation.

### STOP / DEFER

Use when the AWS units themselves are too variable, the generator/environment is inadequate, the
composition signal is not interpretable, or the remaining quota cannot support a useful conclusion.
Retain the result and leave `VAL-SCALE-5` unproven.

The decision compares the **size of the composition signal with the observed unit/environment
variation**, rather than forcing both through an arbitrary precision threshold.

## 9. Relationship to existing validation IDs

- **`VAL-NEG-8`** is reused, not reopened: the independent per-group demand-stream property is
  already mutation-proved. PR4c observes its deployed accounting but does not need a new slow-group
  proof unless the generator implementation changes.
- **`VAL-NEG-7`** is exercised in miniature for A/B equivalence and per-host evidence, but is not
  discharged for `VAL-SCALE-5`: the complete G1/G2/G4 family has not run.
- **`VAL-SCALE-5`** remains unproven regardless of `R2_probe`. Only the governing validation plan's
  complete independently provisioned Tier-1 method can discharge it.
- **Tier 2 does not apply** to this probe. Tier 2 requires a complete G4 environment that exists but
  is measurement-limited; current quota preventing G4 is a provisioning limit, not a Tier-2 trigger.

## 10. Scope guard

PR4c is three short measured cells plus the bootstrap/smoke needed to make them trustworthy, with at
most one hypothesis-driven bounded repeat if the first result specifically requires it. It does
**not** include:

- a G4 topology under the current quota;
- a 600 s S/H + S/H-confirm matrix;
- precision estimation by many repeated runs;
- a new worker/pool optimisation exercise;
- EKS, RDS, autoscaling, service mesh or production-cloud architecture;
- `VAL-LOAD-1` open-loop work;
- investigation of the local VHDX mechanism carried as `DEBT-8`.

Any of those requires a new decision after PR4c evidence exists; none is a predeclared next stage.
