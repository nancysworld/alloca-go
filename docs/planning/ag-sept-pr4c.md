# AG-Sept PR4c — probe-first AWS independent verification

**Status:** Scheduled — bounded AWS independent-capacity probe.  
**Milestone:** AG-Sept, Iteration C.  
**Predecessor:** PR4b (#20), merged 2026-08-19.  
**Budget:** **1.0 development day**, transferred from AG-Sept contingency by maintainer decision on
2026-08-19.  
**Design:** [`../design/independent-capacity-probe.md`](../design/independent-capacity-probe.md).  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).  
**Milestone schedule/budget owner:** [`ag-sept-plan.md`](ag-sept-plan.md).

This is the focused PR4c execution plan. It does not replace the milestone plan's accounting or the
governing `VAL-SCALE-5` definition. PR4c is **the bounded AWS probe described here**; no subsequent
phase, larger matrix, or additional contingency draw is scheduled by implication.

Capacity units use numeric identities **CU1/CU2**. Organisations retain **A/B/C/D**. This distinction
is carried through probe names, placement, manifests and reports.

## 1. Why PR4c changed shape

PR4b was originally expected to be only a local rehearsal before AWS. It did more than that: the
complete local method ran, but the shared workstation write path materially affected the result and
G1 did not reproduce at the experiment's required resolution. The local environment therefore
answered the question that decides the next step: scheduler partitioning is not equivalent to
independently growing capacity-unit resource envelopes.

That points directly to AWS, but not directly to a long G1/G2/G4 campaign. PR4b also showed that an
individual capacity-unit observation can vary substantially. AWS removes cross-unit resource
coupling; it does not guarantee low run-to-run variance.

PR4c therefore follows an evidence-first sequence:

```text
small independent G1/G2 probe
        |
        v
first result + environment evidence
        |
        +--> enough to conclude / defer
        +--> one bounded discriminating repeat, only if a specific ambiguity demands it
        +--> record that a different future experiment may be worthwhile
```

The first result decides whether any further experiment should even be proposed. **No fuller AWS
campaign is currently planned.**

## 2. The scheduled experiment

PR4c is deliberately small:

```text
G1-CU1        independent capacity unit CU1 alone
G1-CU2        equivalent capacity unit CU2 alone
G2-CU1+CU2    those same two units composed
```

A separate generator/measurement instance drives all three. Under the account's current **5-vCPU
Standard On-Demand allowance**, the target shape is **2 + 2 + 1 vCPU** if available in compatible
instance families: two equivalent serving units plus one small generator/monitor unit. Exact
instance types are implementation choices and must be recorded rather than embedded here as durable
architecture.

The first measured pass is three **120 s diagnostic cells** at the validation plan's common
`workers_per_group=12`, after the normal explicit conditioning/recycle sequence. These are not the
canonical 600 s capacity points.

## 3. Implementation scope

Only build what the three-cell probe requires:

1. **AWS bootstrap**
   - instantiate two equivalent capacity-unit EC2 hosts and one separate generator/measurement host;
   - apply the minimum networking/security rules for service, metrics, SSH/bootstrap and verifier
     reachability;
   - install the existing container runtime/observability prerequisites;
   - prove teardown is deterministic so metered resources are not left running accidentally.
2. **Per-unit independent storage**
   - each serving unit receives its own storage allocation/path with the same declared shape;
   - no filesystem/volume is shared between CU1 and CU2.
3. **Deploy the existing shard-group design**
   - same Alloca-Go image and PostgreSQL configuration on both serving units;
   - per-authority migration, explicit placement, readiness and provenance checks before load.
4. **Adapt the qualified harness, do not redesign it**
   - route the existing independent `workers_per_group` streams to remote targets;
   - retain per-host/per-authority metrics and environment/provenance evidence;
   - reuse explicit conditioning, state-preserving recycle, response validation and reconciliation.
5. **Drive the three probe cells**
   - `G1-CU1`, `G1-CU2`, then `G2-CU1+CU2`;
   - retain raw rates, per-unit resource evidence and the diagnostic `D_unit_probe`/`R2_probe`
     derivation.
6. **Stop and decide**
   - report one of `PROCEED`, `BOUNDED REPEAT`, `STOP / DEFER` from the validation plan;
   - do not begin a 600 s matrix in the same execution session merely because the first result looks
     promising.

## 4. What PR4c deliberately does not build

- G4 under the current quota;
- the full Tier-1 S/H + both-confirmations matrix;
- statistical precision through many repeated runs;
- a new pool/worker tuning matrix;
- EKS, RDS, load balancers, autoscaling, service mesh or production-cloud architecture;
- the local off-VHDX `DEBT-8` killing test;
- `VAL-LOAD-1` open-loop work.

A fuller AWS capacity campaign is **not part of the current plan**. If this probe produces evidence
that makes a different experiment worth considering, that is a new planning decision made after
PR4c evidence exists.

## 5. Evidence and result boundary

PR4c reports:

```text
g1_CU1(12)
g1_CU2(12)
g2_CU1_CU2(12)
D_unit_probe
R2_probe
```

and the host/service/database evidence needed to interpret them.

It does **not** report `G1_aws`, `G2_aws`, `E2_aws`, or discharge `VAL-SCALE-5`. The probe is useful
precisely because it can be cheap without pretending to have the precision or saturation evidence
of the full method.

A positive probe result supports the architecture statement that independently provisioned shard
groups can compose useful capacity under the observed workload/environment. The strength of that
statement is bounded by one short observation per cell and the observed CU1/CU2 difference.

## 6. Budget

**Allocated: 1.0 day from the remaining AG-Sept contingency** (maintainer decision, 2026-08-19).
The milestone plan records the transfer; this is a funded work unit, not an implicit draw.

The day covers the first AWS bootstrap, minimal remote-harness adaptation, three short probes,
evidence retention, analysis/reporting, normal review/fix margin and—only if the first result leaves
one sharply stated ambiguity—a bounded discriminating repeat that still fits the same day.

The allocation does **not** pre-fund a complete G1/G2/G4 Tier-1 campaign or any other follow-on
experiment. If PR4c indicates that such work may be worthwhile, the proposal competes for budget
only after this PR's evidence is reviewed.

## 7. Exit gate

PR4c is complete when:

- the independent CU1/CU2 + separate-generator topology is reproducibly bootstrap/teardown-able;
- all three requested cells ran with the intended topology/configuration or are explicitly refused;
- interpreted cells pass response validation, conditioning/reconciliation, provenance and required
  per-host evidence checks;
- raw `G1-CU1`/`G1-CU2`/`G2-CU1+CU2` observations and `D_unit_probe`/`R2_probe` are retained with
  their diagnostic label;
- the first-result decision is recorded as `PROCEED`, `BOUNDED REPEAT`, or `STOP / DEFER`;
- `VAL-SCALE-5` remains explicitly unproven because this bounded probe is not the complete Tier-1
  validation;
- AWS resources are torn down after the bounded session unless a documented immediate corrective
  action inside this same funded scope requires them.

## 8. What `PROCEED` means

`PROCEED` is **not a scheduled next stage**. It means only that this probe found independent-unit
behaviour sufficiently intelligible that a later experiment could be worth proposing.

Questions the probe may answer include:

- are CU1 and CU2 close enough that one canonical G1 denominator might be defensible, or would a
  future method need to baseline multiple units individually?;
- is generator capacity adequate for G2 and plausibly larger topologies, or is measurement compute
  already the practical boundary?;
- does per-unit storage behave reproducibly enough that the 5% materiality resolution looks
  realistic, or should any future claim target coarser architecture discrimination instead?;
- does G2 expose a new bottleneck that makes a larger topology irrelevant to the current question?

Those are inputs to a **future decision**, not placeholders for predeclared PR4c stages.
