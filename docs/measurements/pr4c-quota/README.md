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

The first two are verbatim maintainer terminal captures of the **same four-command environment
probe** — availability zones, default VPC, default subnets, then the quota — run once before
planning and once on the day execution was attempted. The 2026-08-20 capture's shell echo is visibly
interleaved from a multi-line paste; it is kept unedited rather than tidied, because a capture that
has been cleaned up is no longer the thing that was observed.

**The two history captures are altered**, and each says so in its own first line: the 12-digit AWS
account identifier is replaced wherever it appears. That is the one exception to keeping captures
verbatim, and it is stated in the artifact rather than only here.

## What they establish

Both captures include their own command text, pinning `--region eu-west-2`, `--service-code ec2` and
`--quota-code L-1216C47A` in both cases. Both outputs are `GetServiceQuota` responses — the table
title states it — so both report the **applied** quota, not a pending or requested value, and both
carry that quota's own `Name`. Same quota, same region, same command, two dates: the applied value
**fell from 5.0 to 1.0**.

That is the fact PR4c's `STOP / DEFER` rests on: the bounded probe needed a 5-vCPU minimum topology
(2 + 2 + 1) and the account could not instantiate one 2-vCPU capacity unit.

## What they do not establish

- **Neither capture carries a timestamp.** The AWS CLI table format emits none, so both dates rest on
  the maintainer's report: the earlier capture is placed in the week preceding 2026-08-20 with no
  exact date, the later one on 2026-08-20.
- **Neither is full JSON**, because both used `--output table` with a `--query` projection. Neither
  response body therefore contains the `QuotaArn` that would carry region and quota identity inside
  the artifact rather than in the command line above it.
- **The availability-zone listing also differs, and that is unexplained.** The earlier capture returns
  three zones in `eu-west-2`; the later one returns four, adding `eu-west-2d`. The default VPC and all
  three default subnets are byte-identical across both, so this is the same account and the same
  region. Two unrelated properties of one account changing between two runs of one script is worth
  recording, and it is the reason the quota fall is described here as unexplained rather than merely
  undocumented. **Observed, not investigated** — see the stop below.
- **The EC2 launch refusal is not retained here.** That the API refused one `c5.large` on 2026-08-20
  is recorded in prose in the PR4c documents and has no artifact in this repository.
- **Why the quota fell is still not evidenced.** The requested-change history is now captured and
  shows no decrease was ever requested (see below), which narrows the cause without identifying it.
  Whether a grant expired, was reversed, or the account was adjusted is not visible in any artifact
  here.

## What the request history rules out

The quota-scoped capture is the **complete** requested-change history for `L-1216C47A`. It holds
exactly two records:

| Case | Desired | Created | Status |
|---|---:|---|---|
| `178654858600135` | 20.0 | 2026-08-12 | `CASE_CLOSED` (last updated 2026-08-14) |
| `178723066000473` | 6.0 | 2026-08-20 13:57 | **`CASE_OPENED`** |

Three consequences, and two of them contradict what the surrounding documents say:

- **No decrease was ever requested by the account.** Nothing in the account's own request history
  produced the 5.0 → 1.0 fall, which is why it stays unexplained rather than merely undocumented.
  This is a genuine negative result: it excludes the most ordinary explanation.
- **The 6-vCPU request is open, not declined.** It was created on the day execution was attempted and
  its status is `CASE_OPENED`. Any statement that it was declined is contradicted by this capture.
- **No separate 12-vCPU request exists.** The history holds one case at 20.0, consistent with the
  12-vCPU figure having been an appeal *within* case `178654858600135` — which is how
  [`ag-sept-pr4.md`](../../development/implementation/ag-sept-pr4.md) §5 describes that exchange.
  "Requests to 20 and then 12" reads as two requests; the record shows one case.

`CASE_CLOSED` is also not itself a denial — it records that the support case closed. The decline of
the 20.0 request rests on the support correspondence recorded in prose in §5, not on this field.

## Not captured, and deliberately not pursued

**Maintainer decision, 2026-08-20:** stop spending AG-Sept time on the AWS side. What is above is the
record. AG-Sept closes with the quota fact evidenced and its cause unexplained, which is sufficient
for a closure that claims no AWS measurement.

This is a stated stop, not a tech-debt item: there is no condition under which it becomes wrong, and
no trigger to revisit it inside this milestone.

One capture was considered and not taken — `get-service-quota --output json`, whose `QuotaArn` would
carry Region and quota identity inside the artifact rather than in the command line above it. The
command lines in the two applied-value captures already pin both, so it would add provenance that is
present by another route. If independently provisioned capacity is ever selected as post-AG-Sept
work, verify external provisioning prerequisites first, and capture the applied value **before** any
pending increase request is decided: once granted, the value observed at planning time becomes
unrecoverable, which is exactly how the 5.0 reading came to have no artifact for a week.

The `sed` filter redacts the 12-digit AWS account identifier from any ARN while preserving region and
quota code, which is the part that carries evidentiary weight. It is applied under
[`../../public-disclosure-policy.md`](../../public-disclosure-policy.md) — environment detail is
recorded to the extent needed to interpret the fact and no further.

**Disclosure note.** Neither capture contains an AWS account identifier or any credential. Both do
contain default-VPC and default-subnet resource IDs and their RFC 1918 CIDR blocks, retained because
truncating a capture would defeat the purpose of keeping one. These identify nothing without
credentials, but they are account-specific and are not needed to interpret the quota fact, so they
are flagged for the [`pre-public checklist`](../../pre-public-checklist.md) rather than decided here.

## Consequence

PR4c produced no AWS measurement. `VAL-SCALE-5` remains unproven, and independently provisioned
capacity evidence is post-AG-Sept work. The closure reasoning is owned by
[`../../planning/ag-sept-pr4c.md`](../../planning/ag-sept-pr4c.md); this directory only holds the
external fact it depends on.
