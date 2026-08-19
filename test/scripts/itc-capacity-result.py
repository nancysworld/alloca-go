#!/usr/bin/env python3
"""Decide the Iteration C knee from retained runs, and derive the local scale efficiencies.

`ag-sept-validation-plan.md` §4.6.5 defines the comparison quantity as the **full-600 s horizon
average** of fresh-mutation Goodput from a fixed conditioned start, and §4.6.7 derives

    E2_local = G2_local / (2 x G1_local)
    E4_local = G4_local / (4 x G1_local)

Nothing here reads a sub-interval. The slices in a run's `slices.txt` describe and disqualify; they
never select, and no statistic is computed across them.

**Why a script rather than arithmetic in a report.** The selection rule has four ways to fail and
only one to pass, and each failure means something different — an unresolved knee is not a low
capacity result. Deciding it by eye across twelve runs is how a disagreement gets averaged into a
number. Every figure below is re-derived from `run.json`, so a report may quote it.

    ./test/scripts/itc-capacity-result.py test/results/pr4b-capacity

The directory holds one subdirectory per retained run, named `g<groups>-<role>` where role is one
of `s`, `h`, `s-confirm`, `h-confirm`.
"""

import json
import pathlib
import sys

# The same margin reconnaissance brackets with, and for the same reason: ten identical G4 cells in
# the healthy regime agreed to within 2.6% (docs/measurements/pr4a-rehearsal/repeats/), so 5% is
# roughly twice the environment's own demonstrated reproducibility.
#
# §4.6.5 says "materially higher" and "disagree materially" without fixing a number, so this is an
# operationalisation of the rule rather than the rule itself. It is stated in the output for that
# reason: a reader must be able to see which threshold decided a knee.
MARGIN = 0.05

ROLES = ("s", "h", "s-confirm", "h-confirm")


def cell_of(run_dir):
    """The cell directory holding the run, whether it is the directory itself or its `cell-01`.

    The capacity stage names each run's series directory after the run's role, so the artifact
    lands at `g1-s/cell-01/`; a run promoted into `docs/measurements/` is flattened to `g1-s/`.
    Both layouts are read rather than one being declared canonical, because the promotion step
    would otherwise silently change what this script can analyse.
    """
    if (run_dir / "run.json").exists():
        return run_dir
    nested = run_dir / "cell-01"
    if (nested / "run.json").exists():
        return nested
    return None


def horizon_average(run_dir):
    """The full-measured-interval Goodput rate, from the run's own manifest.

    §4.6.5's comparison quantity. `successful_mutation_goodput` counts fresh mutations only, and
    `duration_seconds` is the measured interval — so this is the whole horizon from the conditioned
    start, not a plateau and not a mean of slices.
    """
    manifest = json.loads((run_dir / "run.json").read_text())
    summary = manifest["summary"]
    quotability = manifest.get("quotability", {})

    if not summary.get("measurement_sound", False):
        raise SystemExit(f"{run_dir}: measurement_sound is false. A run whose responses went "
                         f"unvalidated backs no claim and must not enter a comparison.")
    if quotability.get("level") not in ("capacity", "publishable"):
        raise SystemExit(f"{run_dir}: certifies at '{quotability.get('level')}', below capacity. "
                         f"§13.2 makes that a run which cannot say what topology it measured.")

    return {
        "rate": summary["successful_mutation_goodput"] / summary["duration_seconds"],
        "goodput": summary["successful_mutation_goodput"],
        "seconds": summary["duration_seconds"],
        "workers_per_group": summary.get("workers_per_group"),
        "level": quotability.get("level"),
    }


def load_topology(root, groups):
    """Every retained run for one topology, or None if the set is incomplete.

    Incomplete is reported rather than worked around: §4.6.5 requires both sides confirmed, and
    three runs out of four cannot establish a knee however good the three look.
    """
    runs = {}
    for role in ROLES:
        cell = cell_of(root / f"g{groups}-{role}")
        if cell is None:
            return None, f"missing g{groups}-{role}"
        runs[role] = horizon_average(cell)
    return runs, None


def materially_higher(a, b):
    return a > b * (1 + MARGIN)


def reproduces(a, b):
    """Two observations of one point agree within the margin, in either direction."""
    return not materially_higher(a, b) and not materially_higher(b, a)


def decide(runs):
    """Apply §4.6.5's selection rule. Returns (resolved, G_local, notes)."""
    s1, s2 = runs["s"]["rate"], runs["s-confirm"]["rate"]
    h1, h2 = runs["h"]["rate"], runs["h-confirm"]["rate"]
    notes = []

    # **Both S and the deciding H must reproduce** (ag-sept-plan.md §3, PR4b). A point that does not
    # reproduce cannot decide anything, and averaging the disagreement would hide it.
    s_ok = reproduces(s1, s2)
    h_ok = reproduces(h1, h2)
    notes.append(f"S reproduces: {'yes' if s_ok else 'NO'} "
                 f"({s1:.1f}/s vs {s2:.1f}/s, {abs(s1-s2)/min(s1,s2)*100:.1f}% apart)")
    notes.append(f"H reproduces: {'yes' if h_ok else 'NO'} "
                 f"({h1:.1f}/s vs {h2:.1f}/s, {abs(h1-h2)/min(h1,h2)*100:.1f}% apart)")

    # **Neither H observation may produce materially higher sustained Goodput than the selected
    # point's runs.** Compared against the *higher* S observation, which is the conservative
    # direction: it makes the rule harder to pass, so a knee is not established by picking S's
    # worse reading.
    s_best = max(s1, s2)
    h_best = max(h1, h2)
    upper_ok = not materially_higher(h_best, s_best)
    notes.append(f"H does not beat S: {'yes' if upper_ok else 'NO'} "
                 f"(best H {h_best:.1f}/s vs best S {s_best:.1f}/s)")

    resolved = s_ok and h_ok and upper_ok
    if not resolved:
        notes.append("KNEE UNRESOLVED — investigate or move the bracket (§4.6.5). This is not a "
                     "low capacity result; it is the absence of one.")

    # The reported G_local is the mean of the two selected-point observations. Both are separate
    # 600 s runs of the same point from the same conditioned start, so neither is more canonical
    # than the other, and reporting one would discard half the evidence. They are printed
    # individually as well, because a mean whose inputs are hidden cannot be checked.
    return resolved, (s1 + s2) / 2, notes


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__.strip().splitlines()[-1].strip())
    root = pathlib.Path(sys.argv[1])
    if not root.is_dir():
        sys.exit(f"{root} is not a directory")

    print(f"Iteration C local capacity result, from {root}")
    print(f"comparison quantity: full-measured-interval Goodput, ag-sept-validation-plan.md §4.6.5")
    print(f"margin for 'materially': {MARGIN*100:.0f}% (operationalises §4.6.5; see the source)")
    print()

    topologies = {}
    for groups in (1, 2, 4):
        runs, why = load_topology(root, groups)
        if runs is None:
            print(f"G{groups}: incomplete — {why}")
            continue

        print(f"G{groups}:")
        for role in ROLES:
            r = runs[role]
            print(f"  {role:<10} {r['rate']:>9.1f}/s   {r['goodput']:>10,} mutations over "
                  f"{r['seconds']:.0f}s at {r['workers_per_group']} workers/group [{r['level']}]")
        resolved, g_local, notes = decide(runs)
        for note in notes:
            print(f"  {note}")
        print(f"  G{groups}_local = {g_local:.1f}/s" if resolved
              else f"  G{groups}_local = withheld: the knee is unresolved")
        print()
        if resolved:
            topologies[groups] = g_local

    # **Efficiencies only when every point they divide is resolved.** An efficiency built from an
    # unresolved knee reads as a scaling result while being a statement about an arbitrary level,
    # and it is the number a report is most likely to quote.
    print("scale efficiency (§4.6.7):")
    if 1 not in topologies:
        print("  withheld: G1_local is the denominator of both and is not resolved")
        return 1

    g1 = topologies[1]
    status = 0
    for groups, label in ((2, "E2_local"), (4, "E4_local")):
        if groups in topologies:
            efficiency = topologies[groups] / (groups * g1)
            print(f"  {label} = {topologies[groups]:.1f} / ({groups} x {g1:.1f}) = {efficiency:.3f}")
        else:
            print(f"  {label} withheld: G{groups}_local is not resolved")
            status = 1

    print()
    print("This is VAL-SCALE-6 and describes the explicitly recorded local environment only")
    print("(docs/measurements/environment.md). It is not independently provisioned capacity")
    print("evidence, does not discharge VAL-SCALE-5, and is never mixed with a Tier 1 point.")
    return status


if __name__ == "__main__":
    sys.exit(main())
