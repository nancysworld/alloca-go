# `test/` — manual, local verification

Nothing in this directory runs in CI, and nothing in it is compiled. It holds the checks a
person runs by hand against a service they started themselves.

| Path | What it is | How it runs |
|---|---|---|
| `scripts/smoke.sh` | end-to-end checks over a real socket against a running service | `make smoke`, with `make dev` in another terminal |
| `results/` | load-harness output (git-ignored scratch) | written by `alloca-load` and `alloca-verify` |

**Go tests are not here.** They live beside the code they exercise, under `internal/` and
`cmd/`, and they are what CI runs.

The layout and the reasoning behind this split are owned by
[`../docs/design/project-structure.md`](../docs/design/project-structure.md) §1; the
load-harness procedure is owned by
[`../docs/operations/load-harness.md`](../docs/operations/load-harness.md).
