# Observed run state

What was actually running, captured from the live system at run time and consumed by a run.

**Everything here is git-ignored, and that is the point.** These are *observations*, not authored
inputs. The repository's committed counterparts are the **declared** documents —
[`../../deploy/topology/`](../../deploy/topology/)'s placement and declaration files — which state
what the operator intends. Keeping the two apart is what stops the weaker provenance class from
inheriting the stronger one's credibility
([`measurement-contract.md`](../../docs/design/measurement-contract.md) §6.4).

A committed observation would be worse than no observation. It goes stale the moment the system
changes, and a run could then certify against a record describing something that is not under
test — while looking exactly as authoritative as a fresh one.

Output goes to [`../results/`](../results/) instead, and a run worth keeping is promoted into
[`../../docs/measurements/`](../../docs/measurements/) deliberately.

## `deployment.json`

The deployment record: which units are serving, and the image ID each is actually running.
`record-deployment.sh` inspects the running containers, because a process cannot see which image
wraps it — anything the service reported would be an environment variable handed to it and
repeated back.

Regenerate it **before every run**, against whichever topology is up:

```bash
# the Iteration C rehearsal topology (G1, G2 or G4)
make itc-deployment ITC_GROUPS=4 > test/observed/deployment.json

# the two-authority container topology
make topo-deployment > test/observed/deployment.json
```

Both write to stdout, so the redirect is yours to aim. `alloca-load` refuses a multi-unit run
whose `/meta` reconciliation disagrees with this file, which is the check that catches a stale
one — but it catches it *after* the topology is up, so regenerating first is cheaper than finding
out.

## What else belongs here

Anything a run captures from the live system and then depends on. The clock-synchronisation
preflight (`ag-sept-pr4.md` §2.8), host facts from a future `node_exporter`, and image-provenance
captures are all the same class: observed, run-scoped, and never committed. Put them here rather
than beside the output they explain, so the split between "what we asserted" and "what we saw"
stays legible at a glance.
