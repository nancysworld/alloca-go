# Disk I/O series for the four identical runs — backfilled

Same provenance, same queries, same caveat as
[`../../pr4b-capacity/disk-io-backfill/README.md`](../../pr4b-capacity/disk-io-backfill/README.md),
which is the normative description: queried out of the surviving Prometheus TSDB after the runs
completed, because `panels.json` defined no disk panel at the time and no cell retained one.

[`summary.txt`](summary.txt) is the per-run mean over each measured interval. It is the file the
storage conclusion rests on for this control, because these four runs are identical in every
respect *except* what the device delivered:

```text
run           queue   write MiB/s   Goodput/s   mut/MiB
run-01         1.67         18.62      1255.9      67.4
run-02         0.96         15.33      1023.1      66.7
run-03         1.59         18.01      1202.1      66.7
run-04         1.75         19.12      1279.7      66.9
```

Goodput spans 25.1% across the four. Mutations per MiB written spans **1.0%**. The work done per
byte is constant; what varies is how many bytes the device accepted, and the slow run is the run
that got the least — at the lowest queue depth of the four.

This is correlation across four runs, not a demonstrated cause. The killing test — driving G1 with
its data directory off the VHDX and observing whether the spread collapses — was deliberately not
run (`ag-sept-pr4.md` §3.30).
