# Four identical G1 runs — the control that measured G1's own reproducibility

Four 600 s runs driven 2026-08-19, identical in every respect: G1, 12 workers per shard group,
`pool_max_conns=8`, the same 45,000-slot fixture the retained comparison used, the same conditioning
sequence, the same ordinary measurement path. They differ only in when they ran.

    ./test/scripts/itc-local-experiment.sh drift

## Why it was driven

The retained comparison ([`../pr4b-capacity/`](../pr4b-capacity/)) left G1's knee unresolved: the two
`H` observations disagreed by 15.6% and the best `H` exceeded the weakest `S` by 10.5%. Two
explanations fit that equally well from the comparison alone, and they call for opposite responses.

The first was **run position**. §4.6.5 prescribes the order `S`, `H`, `S`-confirmation,
`H`-confirmation, so `S` always occupies positions 1 and 3 of a series and `H` always 2 and 4. The
observed rates rose with position — G2 monotonically at +0.0%, +2.8%, +8.5%, +9.7%, and G1's
position-4 run was its highest at +10.5% — which is on its own enough to manufacture the upper-side
failure with no difference between 12 and 16 workers existing at all. The two figures coincide
because they are the same pair of runs: the weakest `S` is position 1 and the best `H` is position 4.
Had that been the cause, the method was at fault and the remedy was to counterbalance the order.

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

## What the variance does and does not tell us

The retained CPU, memory, run-queue, backend, wait-event, and pool-acquire signals do not explain the
25.1% spread: CPU busy is 2.37–2.43 cores (of 16), memory available stays within 0.8%, run queue is
7.44–7.83, active backends 4.39–4.79, wait events 1.74–1.90, and pool acquire wait 3.79–3.83 ms. The
slow run also uses less service CPU (0.40 versus 0.43–0.47 cores), so the evidence does not support a
Go compute limit.

The strongest remaining covariance is on the shared write path. Reads are approximately zero,
writes vary from 15.53 to 19.17 MiB/s, and Goodput moves with delivered write bandwidth at broadly
stable work per written MiB. That localises the unexplained variation to the write-side path more
strongly than to the service or the retained CPU/memory signals, but it does **not** establish the
causal bottleneck. Lower write throughput can be a consequence of completing fewer mutations as
well as a cause of doing so, and the same host/storage stack delivers substantially more aggregate
write bandwidth in the G2/G4 runs. The exact mechanism — PostgreSQL write-path serialization,
latency, storage-stack behaviour, or something else — remains open.

The full evidence and its consequence for `VAL-SCALE-6` are in
[`../pr4b-capacity/README.md`](../pr4b-capacity/README.md); this directory measures the spread itself.

## Consequence for further capacity measurement

The local scheduler-partitioned environment remains useful for proving that the topology, workload,
observability, and measurement machinery work together before a more expensive experiment. It is
not accepted as the environment for deriving the next capacity denominator: the service and database
units still share host-level resources through WSL2/Docker Desktop, and the repeated G1 controls show
material variation that the retained signals do not explain.

Further capacity work therefore moves to an **independently provisioned cloud environment**, with the
load generator on separate compute. The next step is to establish a reproducible single-authority G1
frontier there before using that unit as the denominator for G2/G4 capacity composition.

## Reading a run

Layout matches the retained comparison: `run.json`, `phases.txt`, `slices.txt`, `fixture.txt`,
`panels/`, the CPU partition as declared and as observed. TSDB snapshots stay under `test/results/`.

    ./test/scripts/itc-slices.py docs/measurements/pr4b-drift-g1/run-02
