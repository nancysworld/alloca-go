# The failure-isolation run that produced an ambiguous commit

One run of the failure-isolation cell out of nine driven on 2026-08-11 produced a single
`unknown_replayable`, and the post-run resolution pass settled it. It is retained here because
it is the only *live* demonstration of the `measurement-contract.md` §12 population contract:
the measured and reconciliation populations differ by exactly that one replay.

It is a full cell directory with the same files as
[`../pass-1/failure-isolation/`](../pass-1/failure-isolation/), produced by
`test/scripts/pr3c-experiments.sh failure` at the same commit (`2991940`) against the same
image (`alloca-go:2991940`), with the same fault: `alloca-authority-2-db` stopped 10 s into a
40 s window and started 15 s later.

**Two transcripts were renamed, and nothing else was changed.** This run predates a correction
to the script: it wrote `experiments.log` and `generator.log`, which `.gitignore` excludes as
`*.log`, so retaining them under those names would have left the report citing files the
repository does not carry. They are `experiments.txt` and `generator-output.txt` here, byte for
byte as the run wrote them, and the script now emits those names itself.

**Ambiguity is not manufactured and is not required.** `ag-sept-validation-plan.md` VAL-COR-6
discharges the accounting on deterministic end-to-end tests precisely so that no experiment has
to produce an ambiguous commit to order; the eight other runs produced none. Read this
directory as one observation of the contract holding on a real fault, not as a rate.

The reading is in
[`../../reports/ag-sept-pr3c-phase1-correctness.md`](../../reports/ag-sept-pr3c-phase1-correctness.md)
§5.2.
