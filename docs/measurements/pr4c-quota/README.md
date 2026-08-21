# PR4c — EC2 Standard On-Demand vCPU quota captures

**This directory holds account-state evidence, not a measured run.** Nothing here carries a
`quotability.level`, a workload, or a measured interval; the measurement contract's run gates do not
apply. It exists because PR4c's closure rests on an external provisioning fact, and that fact had no
retained artifact.

The quota is `L-1216C47A`, **Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances**, in
`eu-west-2`. Its `Description` is *"Maximum number of vCPUs…"* and its dimension is `Resource: vCPU`:
the figure is a **vCPU count, not an instance count**. One `c5.large` is 2 vCPU, so an applied value
of 1.0 refuses even a single non-burstable serving unit.

## Contents

| File | Shows | Captured |
|---|---|---|
| [`applied-quota-earlier-5vcpu.txt`](applied-quota-earlier-5vcpu.txt) | applied value **5.0** | during the week preceding 2026-08-20; **exact date not recorded** |
| [`applied-quota-2026-08-20-1vcpu.txt`](applied-quota-2026-08-20-1vcpu.txt) | applied value **1.0** | 2026-08-20 |
| [`quota-change-history-2026-08-20.txt`](quota-change-history-2026-08-20.txt) | every requested change across **all** `ec2` quotas in the Region | 2026-08-20 |
| [`quota-change-history-L-1216C47A-2026-08-20.json.txt`](quota-change-history-L-1216C47A-2026-08-20.json.txt) | the same history **scoped to `L-1216C47A`**, as JSON | 2026-08-20 |

## Consequence

PR4c produced no AWS measurement. `VAL-SCALE-5` remains unproven, and independently provisioned
capacity evidence is post-AG-Sept work. The milestone status and budget are recorded in
[`../../planning/ag-sept/milestone-plan.md`](../../planning/ag-sept/milestone-plan.md); this directory only holds the
external fact they depend on.
