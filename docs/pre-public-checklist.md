# Pre-public checklist

Run this before changing the repository from private to public. It makes the
[public-disclosure policy](public-disclosure-policy.md) executable at the one
moment it really has to hold. Nothing here should surface a violation if the
policy has been followed per-commit; this is the final sweep.

## 1. Scan the working tree

- [ ] Grep for the predecessor name and any private codenames:
      `git grep -in -e 'RuntimeIQ' -e '<other private codenames>'`
      — confirm every hit is safe public technical reference, not private detail.
- [ ] Grep for likely leak markers:
      `git grep -in -e 'confidential' -e 'internal only' -e 'do not share' -e 'recruit' -e 'interview'`
- [ ] Search for secrets / credentials (keys, tokens, connection strings, `.env`
      contents, AWS account IDs) in tracked files.
- [ ] Confirm all workload data and scale numbers are synthetic, and no modelled
      assumption is presented as an observed fact about an external system.
- [ ] Confirm every prior-work figure is reproducible in this repo or explicitly
      labelled "prior, not yet reproduced".
- [ ] Strip any reviewer annotations or working notes not intended for release.

## 2. Scan history and metadata

- [ ] Reachable git history:
      `git log --all -p | grep -in -e 'RuntimeIQ' -e 'confidential' -e '<private codenames>'`
      — remember deleted-then-committed content still lives in history.
- [ ] Branch names, tags, commit messages.
- [ ] PR titles/descriptions and issue titles/bodies.
- [ ] Generated artifacts (reports, diagrams, exported data) under `docs/` and
      any build output that may be committed.

## 3. Decide

- [ ] If history contains anything that must not ship, decide between history
      rewrite (e.g. `git filter-repo`) or publishing a fresh squashed repository.
- [ ] Record who reviewed and the date here or in the release PR.

Anything committed can persist even after deletion. When in doubt, do not flip
visibility — resolve the finding first.
