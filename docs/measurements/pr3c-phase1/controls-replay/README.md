# The controls run that exercises VAL-COR-4's same-key replay clause

One run of the `controls` cell driven on 2026-08-12 at commit `3a74bc2` against image
`alloca-go:3a74bc2`, retained for a single line in its transcript:

```
cross-authority refusal is marked as a replay: ok (409, "replay":true)
```

That is the observation the two retained passes do not contain. It was produced by the Iteration B
Analyse & Review, which found that `ag-sept-validation-plan.md` §3.5's **same-key replay** clause
was the one clause of four that nothing on the deployed topology exercised — the refusal cell
drives 2,000 distinct keys and reposts none of them, so what it establishes is a persisted record.
Replay itself was proven a layer below the stack, by `TestCrossAuthorityRefusalIsReplayable` at the
service layer and `TestRefusalIsRecordedAndReplayed` on the PostgreSQL adapter, and composing those
with the persisted records is an argument rather than an observation. Control 3b closes that: it
reposts control 3's key through the deployed two-authority stack.

**Two assertions, because either alone passes for the wrong reason.** The recorded reason without
the replay flag is exactly what a *re-decision* of the policy would also return; the flag without
the reason would accept a replay of some other recorded outcome.

**It is a controls run, not a measured cell.** No load was driven, no scrape pair was taken, and
nothing here enters a measured population — the directory holds the transcript alone, which is why
it does not have the shape of [`../pass-1/`](../pass-1/). The measured numbers for VAL-COR-4 remain
the two retained passes', unchanged and un-rerun. Nothing in this run was folded back into them.

**It does not amend the PR3c report.** That report describes the runs it describes, and its §7.4
statement — that no *retained run at the time* reposts an already-recorded cross-authority key —
stays true of them. §7.4 carries a forward pointer here rather than a correction.

## Reproducing it

```sh
make topo-up                                        # clean tree required
make topo-deployment > test/results/deployment.json
go build -o bin/alloca-load ./cmd/alloca-load       # both stamps must read vcs.modified=false
go build -o bin/alloca-verify ./cmd/alloca-verify
test/scripts/pr3c-experiments.sh controls
```

The assertion discriminates, and that is checkable by hand against the same running topology: post
a *fresh* idempotency key to the same cross-authority path and the refusal returns
`"replay":false`, so the check fails specifically when the repost is not a replay.

```sh
K="disc-$(date -u +%s)"
for i in 1 2; do
  curl -sS -X POST http://localhost:8081/v1/slots/org-b/slot-0/reservations \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $K" \
    -d '{"user_organisation_id":"org-a","user_id":"disc"}'
  echo
done
# {"outcome":"business_refusal","reason":"cross_authority_unsupported","replay":false}
# {"outcome":"business_refusal","reason":"cross_authority_unsupported","replay":true}
```

The status of the validation itself is in
[`../../../test/validation-plan/ag-sept-validation-plan.md`](../../../test/validation-plan/ag-sept-validation-plan.md)
§9, and the closure that commissioned it is in
[`../../../requirements/ag-sept.md`](../../../requirements/ag-sept.md).
