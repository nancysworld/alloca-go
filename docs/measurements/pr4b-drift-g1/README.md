# Four identical G1 runs — the control that measured G1's own reproducibility

Four 600 s runs driven 2026-08-19, identical in every respect: G1, 12 workers per shard group,
`pool_max_conns=8`, the same 45,000-slot fixture the retained comparison used, the same conditioning
sequence, the same ordinary measurement path. They differ only in when they ran.

    ./test/scripts/itc-local-experiment.sh drift

## Why it was driven

The retained comparison ([`../pr4b-capacity/`](../pr4b-capacity/)) left G1's knee unresolved: the two
`H` observations disagreed by 15.6% and the higher one exceeded the best `S` by 9.1%. Two
explanations fit that equally well from the comparison alone, and they call for opposite responses.

The first was **run position**. §4.6.5 prescribes the order `S`, `H`, `S`-confirmation,
`H`-confirmation, so `S` always occupies positions 1 and 3 of a series and `H` always 2 and 4. The
observed rates rose with position — G2 monotonically at +0.0%, +2.8%, +8.5%, +9.7%, and G1's
position-4 run was its highest at +10.5% — which is on its own enough to manufacture a 9.1% "H beats
S" with no difference between 12 and 16 workers existing at all. Had that been the cause, the method
was at fault and the remedy was to counterbalance the order.

The second was that **G1 simply does not reproduce at 600 s**, in which case no run order helps.

Holding the worker level fixed distinguishes them: with the role removed, whatever remains is the
environment.

## What it established

```text
  run       start    pos     rate/s   vs pos 1
  run-01    13:30      1     1255.9      +0.0%
  run-02    13:43      2     1023.1     -18.5%
  run-03    13:55      3     1202.1      -4.3%
  run-04    14:08      4     1279.7      +1.9%

  spread 25.1% across 4 identical runs
  monotonic with position: no
```

**The position hypothesis is refuted.** The series is not monotonic, and the deep outlier is at
position 2. G2's monotonic appearance in the comparison was coincidence.

**G1's run-to-run spread is 25.1% under identical conditions** — five times the 5% margin §4.6.5
operationalises "materially" as. The 15.6% disagreement that left G1's knee unresolved is comfortably
inside it, so that knee was never resolvable at this margin, by any run order.

## What it is not

**Not a capacity result, and it selects nothing.** Identical runs describe the measurement
environment, not a bracket or an operating point. No rate here enters `E2`/`E4`, and `G1_local`
remains withheld rather than being estimated from this population — averaging four readings of a
25%-wide distribution would produce a number whose confidence interval nobody has computed.

Each run certifies `capacity` on the provenance ladder, which as elsewhere is a statement about what
the run can say about itself and not about what was measured.

## Where the variance comes from

Not from the host: CPU busy 2.37–2.43%, memory available within 0.8%, run queue 7.44–7.83, active
backends 4.39–4.79, wait events 1.74–1.90, pool acquire wait 3.79–3.83 ms — all flat across the four.
The slow run used *less* service CPU (0.40 vs 0.43–0.47) and did less database write work, which is
the signature of a downstream stall rather than a service limit.

It comes from the storage path. Reads are ~0, writes are 15.53–19.17 MiB/s, and Goodput tracks
delivered write bandwidth. The full evidence and its consequence for `VAL-SCALE-6` are in
[`../pr4b-capacity/README.md`](../pr4b-capacity/README.md); this directory is the measurement of the
spread itself.

## Reading a run

Layout matches the retained comparison: `run.json`, `phases.txt`, `slices.txt`, `fixture.txt`,
`panels/`, the CPU partition as declared and as observed. TSDB snapshots stay under `test/results/`.

    ./test/scripts/itc-slices.py docs/measurements/pr4b-drift-g1/run-02
