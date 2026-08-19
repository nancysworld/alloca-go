# AG-Sept PR4c — probe-first AWS independent verification

**Status:** Proposed execution plan — design/validation ready for review before AWS implementation.  
**Milestone:** AG-Sept, Iteration C.  
**Predecessor:** PR4b (#20), merged 2026-08-19.  
**Design:** [`../design/independent-capacity-probe.md`](../design/independent-capacity-probe.md).  
**Validation:** [`../test/validation-plan/ag-sept-pr4c-aws-probe.md`](../test/validation-plan/ag-sept-pr4c-aws-probe.md).  
**Milestone schedule/budget owner:** [`ag-sept-plan.md`](ag-sept-plan.md).

This is a focused PR4c execution plan. It does not replace the milestone plan's accounting or the
governing `VAL-SCALE-5` definition. The milestone plan must receive the final budget/status update
before metered AWS execution begins.

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
        +--> stop/defer
        +--> one bounded discriminating repeat
        +--> justify fuller retained AWS experiment
```

The first result decides the second spend.

## 2. Stage 0 — the scheduled first experiment

Stage 0 is deliberately small:

```text
G1-A      one independent serving unit A
G1-B      one equivalent independent serving unit B
G2-A+B    the same two units composed
```

A separate generator/measurement instance drives all three. Under the account's current **5-vCPU
Standard On-Demand allowance**, the target shape is **2 + 2 + 1 vCPU** if available in compatible
instance families: two equivalent serving units plus one small generator/monitor unit. Exact
instance types are implementation choices and must be recorded rather than embedded here as durable
architecture.

The first measured pass is three **120 s diagnostic cells** at the validation plan's common
`workers_per_group=12`, after the normal explicit conditioning/recycle sequence. These are not the
canonical 600 s capacity points.

## 3. Stage-0 implementation scope

Only build what the three-cell probe requires:

1. **AWS bootstrap**
   - instantiate two equivalent capacity-unit EC2 hosts and one separate generator/measurement host;
   - apply the minimum networking/security rules for service, metrics, SSH/bootstrap and verifier
     reachability;
   - install the existing container runtime/observability prerequisites;
   - prove teardown is deterministic so metered resources are not left running accidentally.
2. **Per-unit independent storage**
   - each serving unit receives its own storage allocation/path with the same declared shape;
   - no filesystem/volume is shared between A and B.
3. **Deploy the existing shard-group design**
   - same Alloca-Go image and PostgreSQL configuration on both serving units;
   - per-authority migration, explicit placement, readiness and provenance checks before load.
4. **Adapt the qualified harness, do not redesign it**
   - route the existing independent `workers_per_group` streams to remote targets;
   - retain per-host/per-authority metrics and environment/provenance evidence;
   - reuse explicit conditioning, state-preserving recycle, response validation and reconciliation.
5. **Drive the three Stage-0 cells**
   - G1-A, G1-B, then G2-A+B;
   - retain raw rates, per-unit resource evidence and the diagnostic `U1_probe`/`R2_probe` derivation.
6. **Stop and decide**
   - report one of `PROCEED`, `BOUNDED REPEAT`, `STOP / DEFER` from the validation plan;
   - do not begin a 600 s matrix in the same execution session merely because the first result looks
     promising.

## 4. What Stage 0 deliberately does not build

- G4 under the current quota;
- the full Tier-1 S/H + both-confirmations matrix;
- statistical precision through many repeated runs;
- a new pool/worker tuning matrix;
- EKS, RDS, load balancers, autoscaling, service mesh or production-cloud architecture;
- the local off-VHDX `DEBT-8` killing test;
- `VAL-LOAD-1` open-loop work.

The full AWS capacity campaign remains a **second decision**, not hidden scope in this PR4c starting
plan.

## 5. Evidence and result boundary

Stage 0 reports:

```text
g1_A(12)
g1_B(12)
g2_AB(12)
U1_probe
R2_probe
```

and the host/service/database evidence needed to interpret them.

It does **not** report `G1_aws`, `G2_aws`, `E2_aws`, or discharge `VAL-SCALE-5`. The probe is useful
precisely because it can be cheap without pretending to have the precision or saturation evidence
of the full method.

A positive Stage-0 result supports the architecture statement that independently provisioned shard
groups can compose useful capacity under the observed workload/environment. The strength of that
statement is bounded by one short observation per cell and the observed A/B variation.

## 6. Budget decision before execution

The milestone plan currently allocates **0.0 days** to PR4c and retains **2.0 days of contingency**.
Starting the planning branch does not silently change that accounting.

**Recommended allocation for Stage 0: 1.0 day from contingency.** That funds the first AWS bootstrap,
minimal remote-harness adaptation, the three short probes, evidence retention, analysis/reporting and
normal review/fix margin. First-time cloud setup makes 0.5 day a deliberate shortfall even though the
measured cells themselves are short.

The recommendation is intentionally **Stage-0 only**. If the first result justifies a full retained
Tier-1 attempt, its scope and budget are decided separately after the evidence exists. No remaining
contingency is pre-committed to it here.

No metered AWS execution should begin until the maintainer accepts a budget and the milestone plan's
numeric accounting is updated.

## 7. Stage-0 exit gate

Stage 0 is complete when:

- the independent A/B + separate-generator topology is reproducibly bootstrap/teardown-able;
- all three requested cells ran with the intended topology/configuration or are explicitly refused;
- interpreted cells pass response validation, conditioning/reconciliation, provenance and required
  per-host evidence checks;
- raw G1-A/G1-B/G2 observations and `U1_probe`/`R2_probe` are retained with their diagnostic label;
- the first-result decision is recorded as `PROCEED`, `BOUNDED REPEAT`, or `STOP / DEFER`;
- `VAL-SCALE-5` remains explicitly unproven unless a later, separately planned Tier-1 experiment
  actually discharges it;
- AWS resources are torn down after the bounded session unless a documented immediate follow-up
  requires them.

## 8. If Stage 0 says PROCEED

Do **not** assume the next step is automatically the original twelve-run G1/G2/G4 matrix. Use the
probe to choose the smallest next experiment that can change the architecture conclusion.

Questions Stage 0 may answer first include:

- are A and B close enough that one canonical G1 denominator is defensible, or should a fuller AWS
  method baseline multiple units individually?;
- is generator capacity adequate for G2 and likely G4, or must it be resized before any retained
  capacity attempt?;
- does per-unit storage behave reproducibly enough that the 5% materiality resolution is realistic,
  or should the final claim target coarser architecture discrimination rather than precision?;
- does G2 expose a new bottleneck that makes G4 premature even if quota arrives?

Only after those are answered do we choose whether to wait for the requested quota increase and
attempt complete G1/G2/G4 Tier 1, run one bounded repeat, or close Iteration C with the strongest
available evidence.
