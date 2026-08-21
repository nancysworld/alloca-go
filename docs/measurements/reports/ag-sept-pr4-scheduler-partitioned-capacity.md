# AG-Sept PR4 — scheduler-partitioned shard-group capacity checkpoint

> ## The conclusion
>
> **The scheduler-partitioned Iteration C experiment is complete as local evidence, but Iteration C is not complete.**
>
> PR4a qualified the sustained G1/G2/G4 method. PR4b then executed the complete local comparison on one scheduler-partitioned workstation. `[DERIVED]` `G4_local` resolved at **3493.9 fresh mutations/s**, the mean of two retained selected-point observations; `G1` and `G2` did not resolve, so `G1_local`, `G2_local`, `E2_local`, and `E4_local` are deliberately withheld and `VAL-SCALE-6` remains not discharged.
>
> The strongest local diagnostic is not an efficiency number but an evidence boundary: `[DERIVED]` four identical G1 runs span **25.1%** while mutations per MiB written span only **1.0%**. Across the retained comparison, Goodput follows delivered write bandwidth at broadly stable work-per-byte. This localises the material variation to the workstation's shared write path rather than `alloca-go`, without proving the deeper storage mechanism.
>
> PR4c refined the independent-capacity method but produced **no performance cell** because the required environment could not be provisioned. `VAL-SCALE-5` therefore remains unproven. The independent-capacity question remains open and Iteration C should resume when an equivalent independently provisioned environment is available.

**Status:** evidence checkpoint, **not Analyse & Review**. This report records what PR4 has established so far while the experiment is fresh; it does not close the Iteration C Problem or the AG-Sept engineering loop.

**Evidence class:** local scheduler-partitioned characterisation. Nothing in this report is independently provisioned capacity evidence, and no local result is promoted into `VAL-SCALE-5`.

**Evidence labels:** retained run rates and recorded experiment configuration are `[MEASURED]`; arithmetic calculated from those inputs is `[DERIVED]`. Where a table mixes the two, the text immediately above it states which columns are derived. Interpretive statements are prose conclusions from those labelled inputs and are deliberately not presented as additional measured quantities. This follows [`../../design/measurement-contract.md`](../../design/measurement-contract.md) §2.

Primary artifacts:

- [`../pr4a-sustained/`](../pr4a-sustained/) — method qualification and pool-policy sensitivity;
- [`../pr4b-recon/`](../pr4b-recon/) — adaptive saturation reconnaissance; probe rates are not capacity results;
- [`../pr4b-capacity/`](../pr4b-capacity/) — twelve retained 600 s G1/G2/G4 comparison runs;
- [`../pr4b-drift-g1/`](../pr4b-drift-g1/) — four identical G1 reproducibility controls;
- [`../pr4c-quota/`](../pr4c-quota/) — provisioning-state evidence only, not a measured run.

The implementation history and detailed diagnosis remain in
[`../../development/implementation/ag-sept-pr4.md`](../../development/implementation/ag-sept-pr4.md).
The governing validation remains
[`../../planning/ag-sept/milestone-validation.md`](../../planning/ag-sept/milestone-validation.md) §4.6.

---

## 1. What PR4a qualified

PR4a changed the question from "can the topology start?" to "can a retained comparison be trusted?" Its durable output is the measurement method, not a capacity number.

The qualified local method includes the following `[MEASURED]` configuration and exercised controls:

- one independent closed-loop worker stream per shard group, with `workers_per_group` as the demand variable and a discriminating `VAL-NEG-8` control;
- a fixed per-organisation workload, `WL-MUT-DISP-4`, whose demand semantics do not change with topology;
- conditioning to **4,000** fresh mutations per organisation, followed by a state-preserving service/pool recycle before measurement;
- a **600 s** measured horizon with ten retained **60 s** slices, so the comparison sees the sustained trajectory rather than only its fast opening minute;
- `pool_max_conns=8` per shard group, selected by bounded G1 sensitivity and confirmed to have the same ordering at G4;
- derived fixture sizing and headroom rather than a hand-picked supply ceiling;
- scheduler partitioning and generator confinement, with each shard group assigned **two logical CPUs** and the generator confined separately;
- provenance, response validation, per-authority reconciliation, resource evidence, and run-intent checks for every retained cell.

The six PR4a sustained qualification runs are explicitly **not** a capacity result. Their role is to demonstrate that the corrected method runs cleanly at the canonical horizon and to freeze configuration before the final comparison.

### 1.1 Two methodological findings worth carrying forward

First, PR4a found and removed a benchmark-induced cached-plan artefact: immediate peak load against freshly truncated mutation tables could prepare a sequential-scan plan against an empty relation and keep executing it after a fresh planner would choose the index. Explicit conditioning plus a state-preserving pool recycle removes that artificial start state from the measured phase. This is a benchmark/method defect, not an `alloca-go` capacity result.

Second, every sustained qualification run declines materially across its **600 s** `[MEASURED]` window as mutation state grows. Iteration C therefore compares the **full-horizon average from the same conditioned starting state** rather than selecting a convenient plateau or sub-window. The retained slices describe the trajectory; they do not choose the answer. Whether an indefinitely growing dataset is the right long-term workload model remains an open workload-envelope question rather than something PR4 silently decides.

---

## 2. The scheduler-partitioned comparison

PR4b drove twelve retained **600 s** `[MEASURED]` runs on 2026-08-19: four each at G1, G2, and G4 — selected point `S`, deciding higher point `H`, and an independent confirmation of each.

Configuration common to the retained family `[MEASURED]`:

- `WL-MUT-DISP-4`;
- conditioning target: **4,000** fresh mutations per organisation;
- state-preserving service/pool recycle before measurement;
- measured horizon: **600 s**;
- `pool_max_conns=8` per shard group;
- fixture: **45,000 slots per organisation**, capacity **20**;
- `S=12`, `H=16` workers per group at every topology;
- one service replica + one PostgreSQL authority per shard group;
- generator and serving groups scheduler-partitioned on the same workstation.

All twelve runs certified `capacity` on the provenance ladder, ran the intended horizon/fixture/worker level, and reconciled. That certification describes provenance completeness; it does **not** make a scheduler-partitioned run independently provisioned capacity evidence.

The four rate columns below are `[MEASURED]`. The three comparison columns are `[DERIVED]` from those retained rates under milestone-validation §4.6.5.

| topology | S (12) | H (16) | S confirm | H confirm | S repro | H repro | any H beats any S |
|---|---:|---:|---:|---:|---:|---:|---:|
| G1 | 1080.1 | 1032.6 | 1094.0 | 1193.8 | 1.3% | **15.6%** | **yes, 10.5%** |
| G2 | 2259.0 | 2323.1 | 2450.8 | 2477.7 | **8.5%** | **6.7%** | **yes, 9.7%** |
| G4 | 3494.8 | 3509.5 | 3492.9 | 3543.5 | **0.1%** | **1.0%** | no, 1.4% |

The comparison rule uses a **5%** `[HYPOTHESIS]` preselected engineering materiality margin: each point must reproduce inside that margin, and no higher-point observation may materially exceed any selected-point observation. The 5% value is a decision threshold, not an estimate of environment noise.

**Resolved:**

- `[DERIVED]` `G4_local = (3494.8 + 3492.9) / 2 = 3493.9/s`; the two selected-point observations agree to `[DERIVED]` 0.1%, while the higher point reproduces to `[DERIVED]` 1.0% and does not beat the selected point materially.

**Not resolved:**

- G1: `[DERIVED]` the two H observations disagree by 15.6%, and the best H exceeds the weakest S by 10.5%;
- G2: `[DERIVED]` neither S nor H reproduces inside 5%, and the upper-side test also fails at 9.7%.

Therefore **`G1_local` and `G2_local` do not exist as accepted capacity quantities for this experiment**. Because `G1_local` is the denominator for both scale efficiencies, **`E2_local` and `E4_local` are withheld rather than estimated**.

That is why `VAL-SCALE-6` is *executed but not discharged*.

---

## 3. Why the local denominator is not trustworthy

The retained G1 result initially suggested a possible run-order effect because later runs were faster. PR4b tested that explanation instead of counterbalancing the whole matrix.

Four additional, identical G1 runs at **12 workers** `[MEASURED]` produced:

```text
1255.9/s   1023.1/s   1202.1/s   1279.7/s
```

The sequence is not monotonic. Run position is therefore refuted as the explanation.

`[DERIVED]` The range is **25.1% under identical conditions**, five times the 5% materiality margin. The `[DERIVED]` 15.6% disagreement that leaves the original G1 knee unresolved sits comfortably inside the measured environment variation. Reordering the original four cells could not make that denominator trustworthy.

No retained service/CPU/memory signal distinguishes those runs materially: host CPU, memory, run queue, active backends, wait events, and checkpoint count remain broadly alike. The large difference appears instead on the shared write path.

### 3.1 The write-path covariance

`[MEASURED]` Reads are **0.01–1.07 MiB/s** against roughly **15–49 MiB/s** written during the retained family, so the run is overwhelmingly write-side by volume.

Across the retained PR4b population, Goodput follows delivered write bandwidth while `[DERIVED]` mutations per MiB written stays in a **65.2–72.7** band. In the table below, write bandwidth and Goodput are `[MEASURED]`; mutations/MiB is `[DERIVED]` as `Goodput / write_MiB_per_s`.

| G1 run | write MiB/s | Goodput/s | mutations/MiB |
|---|---:|---:|---:|
| `g1-s` | 16.17 | 1080.1 | 66.8 |
| `g1-h` | 15.39 | 1032.6 | 67.1 |
| `g1-s-confirm` | 16.78 | 1094.0 | 65.2 |
| `g1-h-confirm` | **17.86** | **1193.8** | 66.8 |

For the identical-run control, `[DERIVED]` Goodput spans 25.1% while mutations per MiB spans only 1.0%. The amount of mutation work represented by each written MiB is stable; the amount of write bandwidth delivered by the shared device is not.

Across topology families, greater queue depth is accompanied by greater delivered bandwidth. Queue-depth and write-bandwidth ranges are `[MEASURED]`; the final spread column is `[DERIVED]` as max/min over each topology's four retained comparison runs.

| topology | authorities | disk queue depth | write MiB/s | spread over its 4 retained runs |
|---|---:|---:|---:|---:|
| G1 | 1 | 0.94–1.37 | 15.39–17.86 | 15.6% |
| G2 | 2 | 3.31–4.44 | 31.67–36.18 | 9.7% |
| G4 | 4 | 4.52–5.00 | 48.30–48.93 | 1.4% |

This evidence **localises the material variation to the shared write path rather than the Go service**. It does not establish the deeper mechanism. In particular, the proposition that low queue depth is what makes G1 more variable, or that Docker Desktop's VHDX is the root cause, remains an interpretation consistent with the evidence rather than a demonstrated result. The killing test — moving G1's data directory off the VHDX and asking whether the spread collapses — was not run.

The disk series supporting this section were recovered after the runs from retained TSDB snapshots because disk panels had not yet been promoted into the per-cell exports. The recovery method and corrected queries are retained in [`../pr4b-capacity/disk-io-backfill/`](../pr4b-capacity/disk-io-backfill/). Future runs now retain and plot disk utilisation, queue depth, read rate, and write rate directly.

---

## 4. What the scheduler partition does and does not establish

Scheduler partitioning was valuable because it removed one known confound: the generator and serving units did not compete for the same WSL-visible logical CPUs. It also let the complete G1/G2/G4 topology family and the real measurement machinery be exercised cheaply and repeatedly.

It does **not** turn one workstation into independently growing capacity units. G1, G2, and G4 still share:

- one WSL/Linux kernel;
- one Docker Desktop host environment;
- one physical memory hierarchy;
- one shared storage/VHDX path;
- host-level scheduling and I/O effects above the individual cpusets.

Most importantly for the observed result, adding shard groups changes how many PostgreSQL authorities issue I/O concurrently against that shared write path. It therefore changes the environment seen by the denominator itself instead of adding a like-for-like independent resource envelope.

The local experiment is consequently useful **scheduler-partitioned characterisation**, not a substitute for the independently provisioned comparison required by `REQ-SCALE-4` / `VAL-SCALE-5`.

---

## 5. PR4c and the unfinished Iteration C question

PR4c corrected the independent probe so each capacity unit keeps the same workload/state slice when measured alone and when composed. That method is owned by
[`../../design/independent-capacity-probe.md`](../../design/independent-capacity-probe.md) and
[`../../planning/ag-sept/milestone-validation-pr4c-aws-probe.md`](../../planning/ag-sept/milestone-validation-pr4c-aws-probe.md).

PR4c did **not** execute it. The available EC2 Standard On-Demand vCPU quota could not provision the minimum topology; [`../pr4c-quota/`](../pr4c-quota/) retains that account-state evidence. There is no AWS run artifact, no AWS Goodput value, no AWS efficiency, and no partial topology that can stand in for the missing comparison.

The work-unit stop is therefore not an Iteration C technical verdict. The remaining question is still the one Iteration C was designed to answer: how useful mutation capacity composes when equivalent shard groups receive genuinely independent serving resource envelopes.

The continuation condition is provider-neutral:

> **Resume Iteration C when an equivalent independently provisioned environment can be created with separate per-unit serving/storage resources and separate generator capacity.**

AWS after quota approval is one way to satisfy that condition. Another cloud or equivalent environment is technically acceptable if it satisfies the same design and validation contracts. Changing provider must not change the meaning of the experiment.

Formal Analyse & Review should happen only after that continuation reaches a real decision point — either the intended evidence is obtained, or the Problem itself is explicitly reconsidered.

---

## 6. Reproduction and reading order

For the local checkpoint:

1. read [`../pr4a-sustained/README.md`](../pr4a-sustained/README.md) for the qualified 600 s method and the `pool_max_conns=8` evidence;
2. read [`../pr4b-recon/README.md`](../pr4b-recon/README.md) for how S/H were bracketed and why reconnaissance rates are not quoted as capacity;
3. read [`../pr4b-capacity/README.md`](../pr4b-capacity/README.md) and re-derive the retained knee decision with:

   ```text
   ./test/scripts/itc-capacity-result.py docs/measurements/pr4b-capacity
   ```

4. inspect [`../pr4b-drift-g1/`](../pr4b-drift-g1/) for the identical-run reproducibility control;
5. inspect [`../pr4b-capacity/disk-io-backfill/`](../pr4b-capacity/disk-io-backfill/) for the recovered write-path evidence and its retention caveat.

For the pending independent comparison, start from the design/validation pair linked in §5 rather than treating the local topology as a template to scale outward mechanically.

---

## 7. Checkpoint statement

At this checkpoint, the evidence supports the following and no more:

- the Iteration C sustained measurement method is qualified and reproducible as a procedure;
- the complete scheduler-partitioned G1/G2/G4 family was executed and reconciled;
- `[DERIVED]` `G4_local = 3493.9/s` is the one resolved local knee, from `(3494.8 + 3492.9) / 2` `[MEASURED]` selected-point inputs;
- G1 and G2 are unresolved and all local efficiencies depending on G1 are withheld;
- `[DERIVED]` G1 has 25.1% identical-run variation from the four `[MEASURED]` rates in §3;
- the retained evidence localises that variation to the shared write path rather than `alloca-go`, without proving the deeper storage mechanism;
- no independently provisioned capacity result exists; `VAL-SCALE-5` remains unproven;
- **Iteration C remains open and is waiting for the environment needed to continue its independent-capacity validation.**

Publication of the repository does not change any of those statements and is not an engineering-loop closure event.
