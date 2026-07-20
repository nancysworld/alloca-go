# Public-disclosure policy

This repository is intended to be safe for eventual public release. These rules
apply to **every file, commit message, branch name, PR/issue text, and generated
artifact**, from the first commit onward — not only at the moment visibility is
changed.

## Never record

Do not record confidential discussions, private product details, private
organisation details, recruitment activity, or the private origin of design
prompts.

## Additional discipline

- Use synthetic, generic workload descriptions. Do not present modelled scale
  assumptions as observed facts about external systems.
- Keep references to prior work limited to public technical evidence or
  measurements reproducible in this repository. Label any prior or unreproduced
  figure as such.
- Measured evidence should be reproducible from this repository, or clearly
  marked as prior/unreproduced.
- Review current files, PR text, issue text, branch names, commit messages,
  generated artifacts, and reachable git history before changing repository
  visibility.

## Rationale

Anything committed can persist in git history even if later deleted. The safest
path is to never write it down. When in doubt, prefer a synthetic example and
repository-local evidence.
