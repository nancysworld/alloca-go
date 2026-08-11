# The failure-isolation run that produced a real `unknown_replayable`

One run of the failure-isolation cell driven on 2026-08-11 produced a single
`unknown_replayable` — a mutation whose outcome the client could not settle — and the post-run
resolution pass settled it. It is retained here because it is the only *live* demonstration of
the `measurement-contract.md` §12 population contract: the measured and reconciliation
populations differ by exactly that one replay.

**It is not INV-21's outstanding case.** The replay returned `replay=false`, so the original had
never committed and the resolution performed the mutation itself. INV-21's unproven half is the
opposite — a commit that landed while its acknowledgement was lost, which returns `replay=true`
and which nothing in this repository can yet produce.

It is a full cell directory with the same files as
[`../pass-1/failure-isolation/`](../pass-1/failure-isolation/), produced by
`test/scripts/pr3c-experiments.sh failure` at commit `2991940` against image
`alloca-go:2991940`, with the same fault as the retained passes: `alloca-authority-2-db` stopped
10 s into a 40 s window, isolation asserted, then held down for a further configured 15 s before
restoration — so the transcripts show a stop-to-start interval a second or two longer than 15.

**It predates the harness hardening, and that is worth knowing before reading it.** At
`2991940` the script recorded the readiness codes during the fault and the per-authority row
census without checking either, ran the verifier with a floor of `none`, and did not capture
the verifier's own provenance. The two passes beside it were produced after all four became
failing gates (`d8a6b2e`). So for *this* run, containment and partition are observations rather
than assertions — they read correctly, and nothing enforced that they would.

What the run is kept for does not depend on any of that. The accounting claim is read from its
own `run.json` (client totals and `ambiguity_resolutions`) and the persisted-row counts in its
`verdict.json`, both retained here and checkable without re-running anything.

**Two transcripts were renamed, and nothing else was changed.** This run predates a correction
to the script: it wrote `experiments.log` and `generator.log`, which `.gitignore` excludes as
`*.log`, so retaining them under those names would have left the report citing files the
repository does not carry. They are `experiments.txt` and `generator-output.txt` here, byte for
byte as the run wrote them, and the script now emits those names itself.

**A commit-ambiguous outcome is not manufactured and is not required.**
`ag-sept-validation-plan.md` VAL-COR-6 discharges the accounting on deterministic end-to-end
tests precisely so that no experiment has to produce one to order; the other retained runs of
this cell produced none. Read this directory as one observation of the contract holding on a
real fault, not as a rate.

The reading is in
[`../../reports/ag-sept-pr3c-phase1-correctness.md`](../../reports/ag-sept-pr3c-phase1-correctness.md)
§5.2.
