# The controls run that exercises VAL-COR-4's same-key replay clause

One run of the `controls` cell driven on 2026-08-12 at commit `1ad5054` against image
`alloca-go:1ad5054`, retained for two lines in its transcript:

```
cross-authority refusal is decided, not replayed: ok (409, "replay":false)
cross-authority refusal is marked as a replay: ok (409, "replay":true)
```

That is the observation the two retained passes do not contain. It was produced by the Iteration B
Analyse & Review, which found that `ag-sept/milestone-validation.md` §3.5's **same-key replay** clause
was the one clause of four that nothing on the deployed topology exercised — the refusal cell
drives 2,000 distinct keys and reposts none of them, so what it establishes is a persisted record.
Replay itself was proven a layer below the stack, by `TestCrossAuthorityRefusalIsReplayable` at the
service layer and `TestRefusalIsRecordedAndReplayed` on the PostgreSQL adapter, and composing those
with the persisted records is an argument rather than an observation. Control 3b closes that: it
reposts control 3's key through the deployed two-authority stack.

**Three assertions, because no two of them are enough.** The recorded reason without the replay
flag is exactly what a *re-decision* of the policy would also return; the flag without the reason
would accept a replay of some other recorded outcome; and both of those on the repost, without
`replay=false` on the first post, would still pass in a build that labelled *every* cross-authority
refusal a replay. The two flag readings are what make this a transition rather than a state.

**It is a controls run, not a measured cell.** No load was driven, no scrape pair was taken, and
nothing here enters a measured population — the directory holds the transcript alone, which is why
it does not have the shape of [`../pass-1/`](../pass-1/). The measured numbers for VAL-COR-4 remain
the two retained passes', unchanged and un-rerun. Nothing in this run was folded back into them.

**It revises nothing in the PR3c report.** That report describes the runs it describes, and its
§7.4 statement — that no *retained run at the time* reposts an already-recorded cross-authority key
— stays true of them. Its findings, readings and verdicts are untouched. The section was edited in
two ways that carry no reinterpretation: its heading now scopes the statement to those runs rather
than to the topology, and it carries a forward pointer here rather than a correction.

## Reproducing it

```sh
make topo-up                                        # clean tree required
make topo-deployment > test/observed/deployment.json
go build -o bin/alloca-load ./cmd/alloca-load       # both stamps must read vcs.modified=false
go build -o bin/alloca-verify ./cmd/alloca-verify
test/scripts/pr3c-experiments.sh controls
```

## What the assertions detect

Demonstrated by mutation against this topology rather than argued, because the run above passes
either way. Burning the refusal key *before* the response case 3 asserts on — one extra post of the
same request — makes that response a replay, and the run fails at the assertion that names the
property and at no other:

```
cross-authority refusal: ok (409, cross_authority_unsupported)

!! cross-authority refusal is decided, not replayed: got [409 {"outcome":"business_refusal",
   "reason":"cross_authority_unsupported","replay":true}], wanted it to carry "replay":false
```

The reason assertion above it still passes on the mutated run. That is the point of the addition:
the recorded reason cannot tell a decision from a replay, so on its own it would have accepted a
service that answered every cross-authority post from a record.

The mutation was reverted; nothing in this directory was produced by a mutated script.

The status of the validation itself is in
[`../../../planning/ag-sept/milestone-validation.md`](../../../planning/ag-sept/milestone-validation.md)
§9, and the closure that commissioned it is in
[`../../../requirements/ag-sept.md`](../../../requirements/ag-sept.md).
