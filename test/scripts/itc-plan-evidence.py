#!/usr/bin/env python3
"""Classify the plans a cell's pooled connections actually executed, by phase.

This answers one question, and it is the question the conditioning procedure exists to settle:
**does the measured interval still execute the Seq Scan that was cached against empty mutation
tables?** (ag-sept-pr4.md §3.20, ag-sept-validation-plan.md §4.6.2.)

Two sources, because neither is sufficient alone and the pair is what made the original diagnosis
possible:

  - `auto_explain` records what the pooled connections *did* execute. On its own it cannot
    distinguish a stale cached plan from a planner that keeps choosing badly.
  - the plan probe records what a *fresh* planner *would* choose at that moment. On its own it
    would have reported the plan as fine throughout, because the planner corrected itself within
    seconds while the pooled connections went on executing the old one.

The gap between them is the finding. This script reports both, bucketed by the phase boundaries
the cell retained, so "the measured window executed Index Scans" is a statement about the
measured window rather than about the run's average.

    ./test/scripts/itc-plan-evidence.py <cell-dir> <postgres-log> [<postgres-log> ...]

Exit status is 0 when the measured phase is clean, 1 when it executed a sequential scan of a
mutation table. That makes it usable as a qualification gate rather than only as a report.
"""

import re
import sys
import pathlib
from datetime import datetime, timezone

# The tables the workload grows during a window, and therefore the ones whose plans matter. A
# Seq Scan of `slots` is not the finding: that table is fully populated by the reseed and small
# enough that scanning it can be the right choice.
MUTATION_TABLES = ("idempotency_records", "reservations", "user_time_claims", "user_identities")

# PostgreSQL's default log_line_prefix in the official image is "%m [%p] ", so every entry opens
# with an ISO-ish timestamp. auto_explain emits the plan as a continuation of a LOG: line.
ENTRY = re.compile(r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\.\d+ \w+ \[\d+\]\s+(\w+):\s*(.*)$")


def parse_phases(cell):
    """Read the boundaries the cell retained. Missing file is fatal: without it every plan is
    unattributable, and reporting them as one population is exactly the averaging this exists to
    avoid."""
    path = pathlib.Path(cell) / "phases.txt"
    if not path.exists():
        sys.exit(f"{path} does not exist: the cell did not retain its phase boundaries, so no "
                 f"server-side observation in it can be attributed to a phase")
    phases = {}
    for line in path.read_text().splitlines():
        if "=" in line:
            key, _, value = line.partition("=")
            phases[key.strip()] = value.strip()
    return phases


def when(value):
    if not value:
        return None
    return datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)


def phase_of(stamp, phases):
    """Name the phase a log timestamp falls in.

    The recycle boundary matters more than it looks: a plan executed between the end of
    conditioning and the end of the recycle belongs to neither phase, and counting it as measured
    would attribute a connection that was about to be discarded to the measured window."""
    cond_start, cond_end = when(phases.get("conditioning_start")), when(phases.get("conditioning_end"))
    recycled, m_start, m_end = (when(phases.get("recycle_end")), when(phases.get("measured_start")),
                                when(phases.get("measured_end")))

    if cond_start and cond_end and cond_start <= stamp <= cond_end:
        return "conditioning"
    if cond_end and recycled and cond_end < stamp <= recycled:
        return "recycle"
    if m_start and m_end and m_start <= stamp <= m_end:
        return "measured"
    return "outside"


def read_plans(paths):
    """Yield (timestamp, plan-text) for every auto_explain plan in the logs."""
    for path in paths:
        stamp, collecting, body = None, False, []
        for raw in pathlib.Path(path).read_text(errors="replace").splitlines():
            match = ENTRY.match(raw)
            if match:
                if collecting and stamp:
                    yield stamp, "\n".join(body)
                collecting, body = False, []
                text = match.group(3)
                if match.group(2) == "LOG" and text.startswith("duration:") and "plan:" in text:
                    stamp = datetime.strptime(match.group(1), "%Y-%m-%d %H:%M:%S").replace(
                        tzinfo=timezone.utc)
                    collecting = True
            elif collecting:
                body.append(raw)
        if collecting and stamp:
            yield stamp, "\n".join(body)


def scanned_tables(plan):
    """Mutation tables this plan reads sequentially. Empty when it uses an index for all of them."""
    hits = set()
    for line in plan.splitlines():
        stripped = line.strip()
        if not stripped.startswith("Seq Scan on") and "-> Seq Scan on" not in stripped:
            continue
        for table in MUTATION_TABLES:
            if re.search(rf"Seq Scan on (public\.)?{table}\b", stripped):
                hits.add(table)
    return hits


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__.strip().splitlines()[-3].strip())

    cell, logs = sys.argv[1], sys.argv[2:]
    phases = parse_phases(cell)

    counts = {}
    for stamp, plan in read_plans(logs):
        phase = phase_of(stamp, phases)
        seq = scanned_tables(plan)
        bucket = counts.setdefault(phase, {"plans": 0, "seq": 0, "tables": set()})
        bucket["plans"] += 1
        if seq:
            bucket["seq"] += 1
            bucket["tables"] |= seq

    print(f"executed plans by phase ({cell}):")
    for phase in ("conditioning", "recycle", "measured", "outside"):
        bucket = counts.get(phase)
        if not bucket:
            continue
        tables = ", ".join(sorted(bucket["tables"])) or "-"
        print(f"  {phase:13s} plans={bucket['plans']:5d}  seq-scan={bucket['seq']:5d}  {tables}")

    # The probe is reported beside them rather than merged: it answers a different question, and
    # a run where the probe says Index Scan while the measured phase executed Seq Scan is exactly
    # the stale-cached-plan signature.
    probe = pathlib.Path(cell) / "plan-probe.txt"
    if probe.exists():
        text = probe.read_text(errors="replace")
        print(f"  plan probe:   {text.count('Seq Scan')} Seq Scan / "
              f"{text.count('Index Scan') + text.count('Index Only Scan')} Index Scan samples "
              f"(what a fresh planner would have chosen)")
    else:
        print("  plan probe:   not retained — run with PLAN_PROBE=1 to record what a fresh "
              "planner would have chosen")

    measured = counts.get("measured")
    if not measured or measured["plans"] == 0:
        print("\nINCONCLUSIVE: no executed plan was captured inside the measured window. "
              "auto_explain samples, so a short window or a low sample rate can retain none — "
              "raise PG_AUTO_EXPLAIN_SAMPLE or lengthen the window and repeat.")
        return 1

    if measured["seq"]:
        print(f"\nFAILED: the measured window executed {measured['seq']} sequential scans of "
              f"{', '.join(sorted(measured['tables']))}. Conditioning and the pool recycle did "
              f"not remove the cached empty-table plan from the connections that served it.")
        return 1

    print(f"\nOK: {measured['plans']} plans executed inside the measured window, none of them a "
          f"sequential scan of a mutation table.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
