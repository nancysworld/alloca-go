# `test/` — manual, local verification

Almost nothing in this directory runs in CI, and nothing in it is compiled. It mostly holds the
checks a person runs by hand against a service they started themselves.

| Path | What it is | How it runs |
|---|---|---|
| `scripts/smoke.sh` | end-to-end checks over a real socket against a running service | `make smoke`, with `make dev` in another terminal |
| `scripts/check-build-context.sh` | asserts `.dockerignore` excludes no tracked file | CI, and `make ci` |
| `scripts/itc-cpu-layout-test.sh` | asserts the Iteration C layout check still refuses a bad CPU partition | CI, and `make ci` |
| `scripts/itc-cpu-layout.sh` | checks the rehearsal's CPU partition against this machine | `make itc-layout`, and by `itc-rehearse` before it raises anything |
| `scripts/itc-cpuset-check.sh` | asserts every running container is pinned where the partition says | by `itc-run.sh` in preflight; also by hand |
| `scripts/itc-run.sh` | drives one rehearsal cell with the generator confined to its own CPUs | by hand, after `make itc-rehearse` and `make obs-rehearse` |
| `results/` | load-harness output (git-ignored scratch) | written by `alloca-load` and `alloca-verify` |

The two CI entries are the deliberate exceptions, and the rule for adding another is owned by
[`../docs/design/project-structure.md`](../docs/design/project-structure.md) §1: a check earns a
merge gate when it needs nothing an operator would have to provide.

**Go tests are not here.** They live beside the code they exercise, under `internal/` and
`cmd/`, and they are what CI runs.

The layout and the reasoning behind this split are owned by
[`../docs/design/project-structure.md`](../docs/design/project-structure.md) §1; the
load-harness procedure is owned by
[`../docs/operations/load-harness.md`](../docs/operations/load-harness.md).
