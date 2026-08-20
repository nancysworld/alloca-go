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

| File | Applied value | Captured |
|---|---:|---|
| [`applied-quota-earlier-5vcpu.txt`](applied-quota-earlier-5vcpu.txt) | 5.0 | during the week preceding 2026-08-20; **exact date not recorded** |
| [`applied-quota-2026-08-20-1vcpu.txt`](applied-quota-2026-08-20-1vcpu.txt) | 1.0 | 2026-08-20 |

Both are verbatim maintainer terminal captures, retained as given. Each is the **same four-command
environment probe** — availability zones, default VPC, default subnets, then the quota — run once
before planning and once on the day execution was attempted. The 2026-08-20 capture's shell echo is
visibly interleaved from a multi-line paste; it is kept unedited rather than tidied, because a
capture that has been cleaned up is no longer the thing that was observed.

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
- **Why the quota fell is not evidenced.** Applied quotas do not normally decrease. The requested
  change history would show whether a grant expired, was reversed, or the account was adjusted; it
  is not captured (see below).

## Not captured, and deliberately not pursued

**Maintainer decision, 2026-08-20:** stop spending AG-Sept time on the AWS side. The two captures
above are the record; the fuller ones below were not taken and are not owed. AG-Sept closes with the
quota fact evidenced and its cause unexplained, which is sufficient for a closure that claims no AWS
measurement.

This is a stated stop, not a tech-debt item: there is no condition under which it becomes wrong, and
no trigger to revisit it inside this milestone. If independently provisioned capacity is ever
selected as post-AG-Sept work, external provisioning prerequisites get verified first and these are
the commands for it:

```sh
# Full unfiltered JSON: QuotaArn records region and quota identity in the artifact itself.
aws service-quotas get-service-quota \
  --region eu-west-2 --service-code ec2 --quota-code L-1216C47A --output json \
  | sed 's/:[0-9]\{12\}:/:<account-id>:/' \
  > applied-quota-$(date -u +%Y-%m-%dT%H%MZ).json

# Requested-change history: the record of what was asked for, granted, or declined.
aws service-quotas list-requested-service-quota-change-history \
  --region eu-west-2 --service-code ec2 --output json \
  | sed 's/:[0-9]\{12\}:/:<account-id>:/' \
  > quota-change-history-$(date -u +%Y-%m-%dT%H%MZ).json
```

Should that ever happen, capture the applied value **before** any pending increase request is
decided: once granted, the value observed at planning time becomes unrecoverable, which is exactly
how the 5.0 reading came to have no artifact for a week.

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
