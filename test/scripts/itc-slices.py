#!/usr/bin/env python3
"""Report a sustained run as ten contiguous 60 s slices, not as one average.

`ag-sept/milestone-validation.md` §4.6.5 requires a 600 s run to be analysed as ten contiguous 60 s
slices, and is explicit about what they are: **observations of one trajectory, not ten
independent samples**. Nothing here averages them, compares them as repeats, or computes a
statistic across them — they are printed in order so a reader can see the shape.

That distinction is the whole reason the slices exist. PR4a's diagnosis (§3.11-§3.13) is the
record of a 60 s average describing three different things: a window that accelerated
throughout, one that degraded from its first sample, and one that dipped and recovered. Two of
those produce similar means, and the mean is what run.json holds.

    ./test/scripts/itc-slices.py <cell-dir>

Every number is re-derived from the cell's own retained panel CSVs, so a report may quote it.
"""

import csv
import pathlib
import sys
from datetime import datetime, timezone

SLICE_SECONDS = 60


def phases(cell):
    path = cell / "phases.txt"
    if not path.exists():
        sys.exit(f"{path} does not exist: without the measured boundary the slices cannot be "
                 f"anchored, and a slice anchored to the wrong instant is worse than none")
    out = {}
    for line in path.read_text().splitlines():
        key, _, value = line.partition("=")
        out[key.strip()] = value.strip()
    return out


def epoch(stamp):
    return datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc).timestamp()


def series(cell, name):
    """Read one panel as [(epoch, value)], dropping rows Prometheus returned as NaN."""
    path = cell / "panels" / f"{name}.csv"
    if not path.exists():
        return []
    rows = []
    with path.open() as handle:
        for row in csv.DictReader(handle):
            try:
                value = float(row["value"])
            except (TypeError, ValueError):
                continue
            if value != value:  # NaN
                continue
            rows.append((float(row["timestamp"]), value))
    return rows


def slice_mean(rows, start, end):
    """Mean of the samples inside one slice, or None when the slice has no sample.

    None is reported as '-' rather than as zero. A slice with no sample is an observation that
    was not made, and printing 0 would put a number in the reader's hand that the run never
    produced."""
    inside = [value for stamp, value in rows if start <= stamp < end]
    if not inside:
        return None
    return sum(inside) / len(inside)


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: ./test/scripts/itc-slices.py <cell-dir>")
    cell = pathlib.Path(sys.argv[1])

    boundaries = phases(cell)
    start, end = epoch(boundaries["measured_start"]), epoch(boundaries["measured_end"])
    duration = end - start
    count = max(1, round(duration / SLICE_SECONDS))

    # Scaled where the panel's unit is not the reported one. Prometheus holds these durations
    # in **seconds**; printing 0.02 under a column headed "ms" would understate every latency in
    # the report by a factor of a thousand, and it would look entirely plausible.
    panels = {
        "goodput/s": (series(cell, "goodput"), 1),
        "p95 ms": (series(cell, "latency_p95"), 1000),
        "p99 ms": (series(cell, "latency_p99"), 1000),
        "pool in-use": (series(cell, "pool_in_use"), 1),
        "pool idle": (series(cell, "pool_idle"), 1),
        "acq ms": (series(cell, "pool_acquire_mean"), 1000),
    }

    print(f"{cell}")
    print(f"measured interval {boundaries['measured_start']} .. {boundaries['measured_end']} "
          f"({duration:.0f}s, {count} slices of {SLICE_SECONDS}s)")
    if boundaries.get("conditioning_start"):
        print(f"conditioned {boundaries['conditioning_start']} .. {boundaries['conditioning_end']}, "
              f"pool recycled by {boundaries['recycle_end']}")
    print()

    header = f"{'slice':>6} {'t+s':>6}" + "".join(f"{name:>13}" for name in panels)
    print(header)
    print("-" * len(header))

    for index in range(count):
        lo = start + index * SLICE_SECONDS
        hi = lo + SLICE_SECONDS
        cells = []
        for name, (rows, scale) in panels.items():
            mean = slice_mean(rows, lo, hi)
            cells.append("-" if mean is None else f"{mean * scale:13.2f}")
        print(f"{index + 1:>6} {index * SLICE_SECONDS:>6}" + "".join(cells))

    # The trajectory's endpoints, stated rather than left to the reader's arithmetic. This is a
    # description of the shape, not a verdict: no threshold here refuses a run, and §4.6.5 leaves
    # stationarity to be argued from the evidence rather than asserted by a script.
    goodput, _ = panels["goodput/s"]
    if goodput:
        first = slice_mean(goodput, start, start + SLICE_SECONDS)
        last = slice_mean(goodput, end - SLICE_SECONDS, end)
        values = [slice_mean(goodput, start + i * SLICE_SECONDS,
                             start + (i + 1) * SLICE_SECONDS) for i in range(count)]
        values = [v for v in values if v is not None]
        if first and last and values:
            print()
            print(f"goodput first slice {first:.1f}/s, last {last:.1f}/s "
                  f"({last / first:.2f}x), min {min(values):.1f}, max {max(values):.1f}, "
                  f"spread {max(values) / min(values):.2f}x")


if __name__ == "__main__":
    main()
