# Pre-public checklist

Run this before changing the repository from private to public. It makes the
[public-disclosure policy](public-disclosure-policy.md) executable at the one
moment it really has to hold. Nothing here should surface a violation if the
policy has been followed per-commit; this is the final sweep.

Keyword scans are review aids, not zero-hit gates. The policy and checklist may
legitimately contain words such as `confidential`, `recruitment`, or `interview`,
and `RuntimeIQ` is an explicitly permitted predecessor reference; every hit must be
inspected in context.

## 1. Scan the working tree

- [ ] Grep for the predecessor name and any private codenames:
      `git grep -in -e 'RuntimeIQ' -e '<other private codenames>'`
      — RuntimeIQ **may** be named as the predecessor project
      ([policy](public-disclosure-policy.md#named-predecessor-runtimeiq)); this is not
      a zero-hit gate. Confirm each hit is either lineage framing or a finding labelled
      `[PRIOR-UNREPRODUCED]`, and never source, private paths, hosts, or private
      product/organisation detail. Private codenames other than RuntimeIQ have no such
      exemption and must not appear at all.
- [ ] Grep for likely leak markers:
      `git grep -in -e 'confidential' -e 'internal only' -e 'do not share' -e 'recruit' -e 'interview'`
      — inspect all hits; policy text itself may match.
- [ ] Search for secrets or credentials: keys, tokens, connection strings, `.env`
      contents, AWS account IDs, private endpoints, and exported configuration.
- [ ] Decide on account-specific cloud resource identifiers: VPC, subnet, instance and
      security-group IDs, and their CIDR blocks. They are not secrets and identify nothing
      without credentials, so this is a judgement rather than a removal rule — but decide it
      deliberately. Known holding:
      [`measurements/pr4c-quota/`](measurements/pr4c-quota/) retains default-VPC and
      default-subnet IDs inside verbatim AWS CLI captures.
- [ ] Confirm all workload data and scale numbers are synthetic, and no modelled
      assumption is presented as an observed fact about an external system.
- [ ] Confirm every prior-work figure is reproducible in this repository or
      explicitly labelled "prior, not yet reproduced in this repository".
- [ ] Strip reviewer annotations or working notes not intended for release.

## 2. Scan history and metadata

- [ ] Reachable git history:
      `git log --all -p | grep -in -e 'RuntimeIQ' -e 'confidential' -e '<private codenames>'`
      — remember deleted-then-committed content still lives in history. Apply the same
      predecessor rule as above: the name is allowed, the private detail is not.
- [ ] Branch names, tags, and commit messages.
- [ ] PR titles, descriptions, reviews, comments, and issue text.
- [ ] Generated artifacts such as reports, diagrams, exported data, raw benchmark
      files, logs, and any build output that may be committed.

## 3. Decide

- [ ] If history contains anything that must not ship, decide between history
      rewrite (for example, `git filter-repo`) and publishing a fresh squashed
      repository.
- [ ] Record who reviewed the release candidate and the review date in the release
      PR or another public release record.

Anything committed can persist even after deletion. When in doubt, do not change
visibility—resolve the finding first.
