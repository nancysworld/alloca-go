# PR4b saturation reconnaissance

Short adaptive `workers_per_group` probes driven 2026-08-19 to bracket saturation at G1, G2 and G4
(`ag-sept-validation-plan.md` §4.6.4).

## Evidence class: reconnaissance, and nothing more

**No rate in this directory may be quoted, entered into `E2`/`E4`, or compared with a 600 s horizon
average.** A 120 s probe reads the early, high part of a trajectory that PR4a measured declining to
roughly 0.75× by 600 s. The only output of reconnaissance is the two worker levels it names; the
retained runs in [`../pr4b-capacity/`](../pr4b-capacity/) are the evidence.

## What it selected

| report | probed levels | S | H | lower-side |
|---|---|---|---|---|
| [`recon-G1.txt`](recon-G1.txt) | 8, 12, 16, 24 | 12 | 16 | 12 beats 8 by 5.1% |
| [`recon-G2.txt`](recon-G2.txt) | 4, 8, 12, 16, 24 | 8 | 12 | 8 beats 4 by 26.3% |
| [`recon-G4.txt`](recon-G4.txt) | 8, 12, 16, 24 | 12 | 16 | 12 beats 8 by 6.2% |

**12 workers per group is the measured throughput maximum at all three topologies**, and the
retained comparison was driven at S=12, H=16 everywhere. G2 selected 8 only because its 8 read
within 0.8% of its own 12 — inside single-probe noise — so the differing selection was where a noisy
boundary test stopped rather than a property of the topology (`ag-sept-pr4.md` §3.27).

Fixture sizing for the probes is derived in
[`probe-fixture-sizing.txt`](probe-fixture-sizing.txt). It is deliberately its own size and is *not*
the retained comparison's fixture, which §4.6.6 sizes from the bracket reconnaissance had not yet
found.

## The limitation this reconnaissance discovered about itself

[`reprobe-G1.txt`](reprobe-G1.txt) re-drives G1 at 16 workers, a level already probed, to ask whether
readings drift across a session or vary run to run. Together with the earlier passes, G1 at 16 was
measured four times under identical configuration:

```text
1383.0/s   1395.2/s   1307.0/s   1444.7/s
```

Range 10.5%, coefficient of variation roughly 4%, falling and then rising — **noise, not drift**.
**The 5% margin is therefore approximately one standard deviation of a single probe.**

The consequence was observed rather than predicted: two passes over G1 disagreed about the *ordering*
of 12 and 16, one measuring 16 above 12 by 7.0% and the other 12 above 16 by 8.0%. Both were "flat"
in shape and both passed every gate.

**Reconnaissance therefore brackets a region; it does not locate a point**, and any distinction it
draws inside 5% is a coin-toss. That is a stated limitation of `VAL-SCALE-6` rather than an implicit
assumption, and it is why §4.6.5 settles a knee from 600 s runs with independent confirmations
instead.

## Why the probe cells are not promoted

Each probe is one 120 s cell and there are thirteen of them. They are non-evidence by definition and
carry ~123 MB of TSDB snapshot each, so only the reports are retained here. The cells remain beside
the runs under `test/results/pr4b-recon/`, and each report names the cell directory that produced
every row of its probe table.
