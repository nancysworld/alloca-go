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

Both are verbatim maintainer terminal captures, retained as given.

## What they establish

Both outputs are `GetServiceQuota` responses — the table title states it — so both report the
**applied** quota, not a pending or requested value. Both carry the same quota `Name`, which is
`L-1216C47A`'s own name. The applied value for this quota therefore **fell from 5.0 to 1.0** between
the two captures.

That is the fact PR4c's `STOP / DEFER` rests on: the bounded probe needed a 5-vCPU minimum topology
(2 + 2 + 1) and the account could not instantiate one 2-vCPU capacity unit.

## What they do not establish

- **The earlier capture does not evidence its own region or date.** It preserves output only — no
  command line, and the Service Quotas table format carries neither region nor timestamp. The
  2026-08-20 capture does show its command, which pins both `--region eu-west-2` and
  `--quota-code L-1216C47A`; the earlier one is attributed to the same quota by its `Name` alone.
- **Neither is full JSON**, because both used `--output table` (the later one also `--query`-filtered).
  Neither response body therefore contains the `QuotaArn` that would make the artifact
  self-describing.
- **The EC2 launch refusal is not retained here.** That the API refused one `c5.large` on 2026-08-20
  is recorded in prose in the PR4c documents and has no artifact in this repository.
- **Why the quota fell is not evidenced.** Applied quotas do not normally decrease. The requested
  change history would show whether a grant expired, was reversed, or the account was adjusted; it
  is not captured (see below).

## Still owed

These need credentials this session did not have. Run from a shell with a valid session:

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

**Capture the current state before the pending increase request is decided.** If it is granted, the
1.0 applied value becomes as unrecoverable as the 5.0 reading now is, and the load-bearing half of
this record would be permanently unretained.

The `sed` filter redacts the 12-digit AWS account identifier from any ARN while preserving region and
quota code, which is the part that carries evidentiary weight. It is applied under
[`../../public-disclosure-policy.md`](../../public-disclosure-policy.md) — environment detail is
recorded to the extent needed to interpret the fact and no further. Neither retained capture contains
an account identifier.

## Consequence

PR4c produced no AWS measurement. `VAL-SCALE-5` remains unproven, and independently provisioned
capacity evidence is post-AG-Sept work. The closure reasoning is owned by
[`../../planning/ag-sept-pr4c.md`](../../planning/ag-sept-pr4c.md); this directory only holds the
external fact it depends on.
