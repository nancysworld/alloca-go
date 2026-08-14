# Test fixtures

Inputs a load run consumes, as opposed to output it produces. Output goes to
[`../results/`](../results/) and is git-ignored scratch; a run worth keeping is promoted into
[`../../docs/measurements/`](../../docs/measurements/) deliberately.

## `deployment.json` — an input, but not a fixture

**It is git-ignored, and that is deliberate.** It lives here because every multi-unit run reads
it, but it is *observed* rather than authored: `record-deployment.sh` inspects the running
containers and pins each unit's image ID.

That makes a committed copy actively dangerous. Image IDs change on every rebuild, so a stored
record goes stale silently, and a run could certify against a document describing an image that
is not the one under test. The whole reason the deployment record is separate from the manifest
declaration is to keep observed facts from inheriting declared facts' credibility
([`measurement-contract.md`](../../docs/design/measurement-contract.md) §6.4); committing a stale
observation would give that away from the other direction.

**Regenerate it before every run**, against whichever topology is up:

```bash
# the Iteration C rehearsal topology (G1, G2 or G4)
make itc-deployment ITC_GROUPS=4 > test/fixtures/deployment.json

# the two-authority container topology
make topo-deployment > test/fixtures/deployment.json
```

Both write to stdout, so the redirect is yours to aim. `alloca-load` refuses a multi-unit run
whose `/meta` reconciliation disagrees with this file, which is the check that catches a stale
one — but it catches it *after* the topology is up, so regenerating first is cheaper than
finding out.
