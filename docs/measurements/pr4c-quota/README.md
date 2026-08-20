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
- **Why the quota fell is still not evidenced.** The later request-history captures document the
  Service Quotas increase cases returned by the API. They do not record arbitrary decrease requests,
  every support exchange, or the account action that produced the observed fall. Whether a grant
  expired, was reversed, or the account was adjusted is not visible in any artifact here.

## The request history, and what it does not say

The quota-scoped capture is the **complete** requested-change history for `L-1216C47A`. It holds
exactly two records:

| Case | Desired | Created | Status | Actual course (maintainer) |
|---|---:|---|---|---|
| `178654858600135` | 20.0 | 2026-08-12 | `CASE_CLOSED` (updated 2026-08-14) | declined; appealed at 12; declined |
| `178723066000473` | 6.0 | 2026-08-20 13:57 | `CASE_OPENED` | declined; appealed; **awaiting final decision as at 2026-08-20** |

**`Status` is the case's current state, not its decision history, and the two are easy to confuse.**
A request that was declined and then appealed reads `CASE_OPENED` — identical to one that was never
decided at all. Neither `Status` nor `LastUpdated` records the decline that preceded the appeal: the
6.0 record's `LastUpdated` is five seconds after its `Created`, while the decline and the appeal both
happened afterwards and left no trace in this API. The same is true in the other direction:
`CASE_CLOSED` is not itself a denial, only a closed case.

**Neither request's outcome can be read off this capture.** Both declines rest on the support
correspondence — recorded in prose in
[`ag-sept-pr4.md`](../../development/implementation/ag-sept-pr4.md) §5 for the 20.0 case — and the
6.0 appeal's outcome does not exist yet.

What the capture does establish:

- **The retained API output contains no requested value that explains the 5.0 → 1.0 fall.** This is
  a narrow statement about the captured Service Quotas **increase-request** history, not evidence
  that no decrease request, support action, grant expiry or account adjustment occurred through
  another route. The history documents the two increase cases; it does not establish the cause.
- **No separate 12-vCPU Service Quotas case exists.** The API history holds one case at 20.0. The
  maintainer record says that case was declined, appealed at 12, and declined again — which is how
  §5 describes the exchange. The later 6.0 case was immediately declined and then appealed. The API
  status fields do not preserve either appeal's decision sequence.

## Evidence gathered after the initial stop decision

**Maintainer timeline, 2026-08-20:** PR4c stopped before measurement when the 1-vCPU applied quota
made the planned environment impossible. Evidence gathering nevertheless continued after that stop:
the maintainer supplied the complete earlier four-command probe, retained the execution-time rerun,
and captured both the regional EC2 request history and the quota-scoped JSON response. The earlier
wording that the AWS side was "not pursued" is therefore not accurate.

Those later artifacts pin both applied-value observations to `eu-west-2` quota `L-1216C47A` and
document the two Service Quotas increase cases. Together with the maintainer record, they establish
the request sequence: the 20-vCPU case was declined, appealed at 12, and declined again; the separate
6-vCPU case was immediately declined and appealed, with a final decision still pending as at
2026-08-20. They still do not establish why the applied value fell.

PR4c's closure does not depend on resolving that causal question. No AWS measured cell exists, and
the external provisioning prerequisite failed during the milestone. The record is closed as an
AG-Sept outcome, not as a claim that no later evidence could be added or that AWS capacity can never
be revisited. A later quota decision may enable newly planned post-AG-Sept work; it cannot create a
retrospective PR4c measurement.

A full unprojected `get-service-quota --output json` response was not retained. Its `QuotaArn` would
carry Region and quota identity inside the response body rather than in the command line above it.
Both retained table captures already include command lines that pin Region and quota code, so PR4c
does not depend on that additional representation. If independently provisioned capacity is selected
as post-AG-Sept work, verify provisioning prerequisites and capture the applied value before any new
request decision changes it.

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
