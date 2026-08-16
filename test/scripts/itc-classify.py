#!/usr/bin/env python3
"""Summarise retained cells by the shape of their measured window, not by their average.

A cell's headline rate is an average, and ag-sept-pr4.md §3.11-§3.13 is the record of that
average describing three different things: a window that accelerated throughout, one that
degraded from its first sample, and one that dipped and recovered. Two of those produce similar
means. Nothing in run.json distinguishes them, because run.json holds totals.

So this reads each cell's own retained panel exports and reports the shape, the pool's state
while that shape happened, and whether the host was observed for the whole of it. It computes
nothing that is not already in the cell directory: every column is re-derivable from files a
report may quote.

    ./test/scripts/itc-classify.py test/results/pr4a/repeat-*/cell-*/
    ./test/scripts/itc-classify.py docs/measurements/pr4a-rehearsal/windows/*/

**The shape labels are descriptive, not a gate.** No document owns them and nothing refuses a
run on them; the threshold below is provisional, chosen so it separates the cells this repository
has already retained. Promoting any of this into an admission rule is a measurement-contract
decision, not a property of a reporting script.
"""

import csv
import collections
import json
import os
import sys

# Provisional. The retained cells sit at 1.08 (stationary), 2.07 (dip) and 2.40 (decay), so
# anything in the low 1.x is well clear of every excursion seen so far. It is a reporting
# boundary, not a gate: see the module docstring.
FLAT_SPREAD = 1.30


def read_panel(cell, key, combine=sum):
    """{timestamp: value} with a panel's series combined — the aggregate a shape is read from.

    Timestamped rather than positional because the panels do not share a time base: a rate panel
    is queried one rate-range after the window opens, while an instant selector like pool
    occupancy keeps the whole window. Comparing them by index would line up samples taken
    fifteen seconds apart.
    """
    path = os.path.join(cell, "panels", f"{key}.csv")
    if not os.path.exists(path):
        return {}
    by_time = collections.defaultdict(list)
    with open(path) as fh:
        for row in csv.DictReader(fh):
            by_time[int(row["timestamp"])].append(float(row["value"]))
    return {t: combine(v) for t, v in sorted(by_time.items())}


def shape_of(values):
    """rise | decay | flat | dip | spike — and the spread that decided it.

    **Monotonicity is tested before magnitude**, because a small monotonic trend and a small
    wobble are different findings and the spread cannot tell them apart: the retained 30 s cell
    accelerated across every one of its four samples while spreading only 1.12, and calling that
    flat would discard the observation the cell is kept for.

    Then order matters again. A dip that recovers ends at its maximum, so testing "ends high"
    first would label the excursion a healthy acceleration — the specific misreading this exists
    to prevent. Interior extremes are therefore checked before terminal ones.
    """
    if len(values) < 3:
        return "short", float("nan")
    lo, hi = min(values), max(values)
    if lo <= 0:
        return "zero", float("nan")
    spread = hi / lo
    deltas = [b - a for a, b in zip(values, values[1:])]
    if all(d > 0 for d in deltas):
        return "rise", spread
    if all(d < 0 for d in deltas):
        return "decay", spread
    if spread < FLAT_SPREAD:
        return "flat", spread
    last = len(values) - 1
    if 0 < values.index(lo) < last:
        return "dip", spread
    if 0 < values.index(hi) < last:
        return "spike", spread
    if values.index(lo) == last:
        return "decay", spread
    return "rise", spread


def expected_points(cell, key):
    """How many samples this panel's own query could return over this cell's window.

    Every input is recorded in the cell: `queried_from` is where the exporter opened this
    panel's range query — one of *its* rate ranges after the measured phase began, so no point
    reads pre-window samples — and `window.end` and `step` bound and space the rest. A panel
    returning fewer than this lost samples; a panel returning this many is complete, however few
    that is.
    """
    path = os.path.join(cell, "panels", "index.json")
    if not os.path.exists(path):
        return 0
    try:
        with open(path) as fh:
            idx = json.load(fh)
        panel = next(p for p in idx["panels"] if isinstance(p, dict) and p["key"] == key)
        started = panel.get("queried_from")
        if not started:
            return 0
        fmt = "%Y-%m-%dT%H:%M:%SZ"
        import datetime
        begin = datetime.datetime.strptime(started, fmt)
        end = datetime.datetime.strptime(idx["window"]["end"], fmt)
        step = float(idx["step"].rstrip("s"))
        return int((end - begin).total_seconds() / step) + 1
    except Exception:
        return 0


def ratio(values):
    """Peak against the opening sample: how far a series moved from where the window started."""
    if not values or values[0] == 0:
        return float("nan")
    return max(values) / values[0]


def summarise(cell):
    row = {"cell": os.path.basename(os.path.normpath(cell)) or cell}

    try:
        with open(os.path.join(cell, "run.json")) as fh:
            run = json.load(fh)
        summary, manifest = run["summary"], run["manifest"]
        row["goodput"] = summary["successful_mutation_goodput"]
        row["rate"] = row["goodput"] / summary["duration_seconds"]
        row["p99"] = summary["latency_ms"]["p99"]
        row["level"] = run["quotability"]["level"]
        row["gen_cpu"] = summary["generator"]["cpu_utilisation_per_core"]
        row["units"] = manifest.get("unit_count", 0)
    except Exception as exc:  # a cell that cannot be read is reported, never skipped silently
        row["error"] = f"run.json unreadable: {exc}"
        return row

    # Fixture exhaustion invalidates the cell as evidence and, worse, the rung below it — so it
    # belongs beside the shape rather than in a separate pass nobody runs.
    supply = 0
    fixture = os.path.join(cell, "fixture.txt")
    if os.path.exists(fixture):
        for line in open(fixture):
            if line.startswith("fresh_mutation_supply="):
                supply = int(line.split("=", 1)[1])
    row["supply_used"] = row["goodput"] / supply if supply else float("nan")

    throughput = read_panel(cell, "throughput")
    if not throughput:
        row["shape"] = "no series"
        return row
    rate_window = sorted(throughput)
    values = [throughput[t] for t in rate_window]

    # An exhausted cell has no shape worth naming: its rate is supply divided by duration and
    # describes the fixture rather than the service (§3.10). Labelling it "dip" would dignify a
    # figure that must not be quoted at all, so exhaustion wins over the curve.
    if row["supply_used"] >= 1.0:
        row["shape"], row["spread"] = "spent", float("nan")
    else:
        row["shape"], row["spread"] = shape_of(values)
    row["first"], row["last"], row["min"] = values[0], values[-1], min(values)

    # The pool is what separated the degraded cell from the healthy one (§3.13.1): connections
    # sitting idle while acquire cost climbed. Both halves are needed — a high acquire cost on a
    # saturated pool is a busy service, and the same cost with connections free is not.
    #
    # Bounded by the rate series' own window, because the occupancy panels keep the full measured
    # phase and its opening sample is taken before the first worker has acquired anything. That
    # sample reads zero in use against a full pool, which would report every cell as having had
    # its whole pool idle.
    def within(series):
        return [v for t, v in series.items() if rate_window[0] <= t <= rate_window[-1]]

    acquire_mean = within(read_panel(cell, "pool_acquire_mean", max))
    row["acq_peak_ms"] = max(acquire_mean) * 1000 if acquire_mean else float("nan")
    row["acq_ratio"] = ratio(acquire_mean)
    in_use = within(read_panel(cell, "pool_in_use"))
    total = within(read_panel(cell, "pool_total"))
    if in_use and total:
        row["idle_max"] = max(t - u for u, t in zip(in_use, total))
        row["pool"] = f"{min(in_use):.0f}-{max(in_use):.0f}/{max(total):.0f}"

    # Host coverage, because a cell can pass the per-cell host gate — which asks only for *some*
    # samples — while missing the segment where the shape happened. A cell with no host panel at
    # all is a different statement: those predate the sensor (§3.14) and were never observed,
    # rather than having lost coverage.
    #
    # **Compared against what the panel's own query could yield, never against another panel.**
    # Panels do not share a rate range: the host CPU panels declare 30s against the node job's 5s
    # cadence, so the exporter opens their queries 30s into the window and a complete 60s cell
    # retains seven of them where a 15s panel retains ten. Reading that difference as lost
    # coverage is a mistake this script made and shipped — the gauge panels beside it held all
    # thirteen samples from the window's first second, which is the proof the host was scraped
    # throughout.
    host = read_panel(cell, "host_cpu_busy")
    expected = expected_points(cell, "host_cpu_busy")
    if not host:
        row["host_pts"] = "none"
    elif expected:
        row["host_pts"] = f"{len(host)}/{expected}"
        row["host_short"] = len(host) < expected
    else:
        row["host_pts"] = f"{len(host)}/?"
    return row


def main(argv):
    cells = argv[1:]
    if not cells:
        print(__doc__.strip().splitlines()[0], file=sys.stderr)
        print("usage: itc-classify.py <cell-dir> [cell-dir ...]", file=sys.stderr)
        return 2

    rows = [summarise(c) for c in cells if os.path.isdir(c)]
    if not rows:
        print("no cell directories given", file=sys.stderr)
        return 2

    header = (f"{'cell':22} {'rate/s':>7} {'shape':>6} {'spread':>6} {'first':>6} {'min':>6} "
              f"{'last':>6} {'acq ms':>6} {'acq x':>5} {'pool':>8} {'idle':>4} {'p99ms':>6} "
              f"{'fix%':>5} {'host':>6}")
    print(header)
    print("-" * len(header))
    for r in rows:
        if "error" in r:
            print(f"{r['cell']:22} {r['error']}")
            continue
        if r.get("shape") == "no series":
            print(f"{r['cell']:22} {r['rate']:7.0f} {'—':>6}  (endpoints only, no panel export)")
            continue
        def num(value, fmt):
            return format(value, fmt) if value == value else "—"  # NaN is the absent instrument

        flag = "*" if r.get("host_short") else " "
        print(f"{r['cell']:22} {r['rate']:7.0f} {r['shape']:>6} {num(r['spread'], '6.2f'):>6} "
              f"{r['first']:6.0f} {r['min']:6.0f} {r['last']:6.0f} "
              f"{num(r['acq_peak_ms'], '6.2f'):>6} {num(r['acq_ratio'], '5.2f'):>5} "
              f"{r.get('pool', '—'):>8} {num(r.get('idle_max', float('nan')), '4.0f'):>4} "
              f"{r['p99']:6.1f} {100 * r['supply_used']:5.1f} {r['host_pts']:>5}{flag}")

    shaped = [r for r in rows if r.get("shape") not in (None, "no series")]
    if shaped:
        tally = collections.Counter(r["shape"] for r in shaped)
        print()
        print("shapes: " + ", ".join(f"{k} {v}" for k, v in sorted(tally.items()))
              + f"   (of {len(shaped)} cells with a retained series)")
        rates = sorted(r["rate"] for r in shaped)
        print(f"rate/s: min {rates[0]:.0f}, max {rates[-1]:.0f}, "
              f"spread across cells {rates[-1] / rates[0]:.2f}x")
        if any(r.get("host_short") for r in shaped):
            print("* host CPU series is shorter than its own query could return: samples were "
                  "lost, and the cell was not observed for all of the window it claims")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
