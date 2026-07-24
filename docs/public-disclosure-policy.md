# Public-disclosure policy

This repository is intended to be safe for eventual public release. These rules
apply to **every file, commit message, branch name, PR/issue text, and generated
artifact**, from the first commit onward — not only at the moment visibility is
changed.

## Never record

Do not record confidential discussions, private product details, private
organisation details, recruitment activity, or the private origin of design
prompts. The single named exception is the predecessor project, below.

## Named predecessor: RuntimeIQ

**RuntimeIQ** is a private personal project by the same author; **RuntimeIQ-Alloca**
is its booking/reservation prototype and the direct predecessor of Alloca-Go. Naming
it here is deliberate and permitted. Alloca-Go inherits domain knowledge, experiment
questions, and design lessons from it, and the technical record is more honest — and
its evidence chain citable — for saying so than for hiding the lineage behind an
anonymous "earlier prototype". What Alloca-Go inherits is stated in
[`design/high-level-design.md`](design/high-level-design.md) §1.1.

This permission is narrow:

- **Permitted** — naming RuntimeIQ or RuntimeIQ-Alloca as the predecessor, describing
  what Alloca-Go learned from it, and citing its findings as prior evidence.
- **Required** — every RuntimeIQ figure, result, or observation carries
  `[PRIOR-UNREPRODUCED]` (see
  [`design/measurement-contract.md`](design/measurement-contract.md) §2) until an
  experiment in this repository reproduces it. The reproduction, not the prior figure,
  then becomes `[MEASURED]`.
- **Not permitted** — RuntimeIQ source code, verbatim private text, repository paths,
  hosts, credentials, unreleased plans, or any private organisation, product, or
  recruitment detail carried across with it. Design is reused; private material is
  not, and anything reused is restated synthetically here.
- **Unaffected** — RuntimeIQ's own release status. Naming it discloses the lineage
  and nothing else; it does not make RuntimeIQ public, and Alloca-Go going public
  first is expected.

## Additional discipline

- Use synthetic, generic workload descriptions. Do not present modelled scale
  assumptions as observed facts about external systems.
- Keep references to prior work limited to public technical evidence, measurements
  reproducible in this repository, or predecessor findings cited under the rule
  above. Label any prior or unreproduced figure as such.
- Measured evidence should be reproducible from this repository, or clearly
  marked as prior/unreproduced.
- Review current files, PR text, issue text, branch names, commit messages,
  generated artifacts, and reachable git history before changing repository
  visibility.

## Rationale

Anything committed can persist in git history even if later deleted. The safest
path is to never write it down. When in doubt, prefer a synthetic example and
repository-local evidence.
