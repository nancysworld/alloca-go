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

# The same margin reconnaissance brackets with. §4.6.5 owns it and defines it as a **preselected
# engineering materiality margin**: the smallest difference in sustained Goodput this experiment
# treats as a real difference in capacity, fixed in advance so a knee is not decided by a threshold
# chosen to fit the numbers.
#
# **Not a noise floor.** It is used by two gates that ask different questions — materiality (is H
# meaningfully above S?) and reproducibility (can this environment measure the point at all?) — and
# PR4b is why they are kept apart: four identical G1 runs spanned 25.1% while G4 reproduced to 1.4%,
# so no single figure bounds this machine's noise. G1's failure is the reproducibility gate doing
# its job, not evidence against the threshold.
#
# The number is stated in the output because a knee decided by an unstated margin cannot be checked.
MARGIN = 0.05

# §4.6.5's fixed Iteration C horizon. The tolerance matches the capacity stage's own duration check,
# so a run either stage would accept is a run this script accepts.
HORIZON_SECONDS = 600
HORIZON_TOLERANCE = 0.05

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

    # **The horizon is checked here, not only where runs are driven.** §4.6.5 defines the comparison
    # quantity as the full-600 s horizon average, so a shorter run answers a different question — but
    # it is still sound and still certifies `capacity`, so nothing else about it looks wrong. The
    # capacity stage validates duration when it drives a cell, and that protects neither a direct
    # invocation of this script against arbitrary manifests nor a cell kept by the resume path.
    # Every gate that judges the run that *happened* has to be paired with one that judges the run
    # that was asked for, and this is the pairing for duration.
    seconds = summary.get("duration_seconds", 0)
    if abs(seconds - HORIZON_SECONDS) > HORIZON_TOLERANCE * HORIZON_SECONDS:
        raise SystemExit(
            f"{run_dir}: measured {seconds:.0f}s against §4.6.5's fixed {HORIZON_SECONDS}s horizon. "
            f"A shorter or longer run is a sound measurement of a different quantity and cannot "
            f"enter this comparison.")

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

    # **The four runs must actually be one comparison.** Each role's worker level comes from the
    # manifest rather than from the directory name, because the name is what the operator intended
    # and the manifest is what ran. A point and its confirmation that disagree are not a repeated
    # measurement of anything, and an H at or below S is not a deciding point.
    #
    # This is defence in depth rather than the primary gate — the capacity stage validates each cell
    # as it is driven, and now also validates cells it keeps on resume. It is here because this
    # script is documented as runnable directly against any retained directory, so it must not
    # depend on having been reached through that stage.
    s_level, sc_level = runs["s"]["workers_per_group"], runs["s-confirm"]["workers_per_group"]
    h_level, hc_level = runs["h"]["workers_per_group"], runs["h-confirm"]["workers_per_group"]
    if s_level != sc_level:
        return None, (f"g{groups}: S ran at {s_level} workers/group and its confirmation at "
                      f"{sc_level}; they are not two observations of one point")
    if h_level != hc_level:
        return None, (f"g{groups}: H ran at {h_level} workers/group and its confirmation at "
                      f"{hc_level}; they are not two observations of one point")
    if not (isinstance(h_level, int) and isinstance(s_level, int) and h_level > s_level):
        return None, (f"g{groups}: H ran at {h_level} workers/group against S at {s_level}; "
                      f"H is by definition a higher level (§4.6.4)")
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

    # **§4.6.5's rule is pairwise: the deciding H run *and* its confirmation must each fail to beat
    # the selected point's runs.** So no H observation may materially exceed *any* S observation,
    # which is `max(H)` against `min(S)`.
    #
    # An earlier version compared `max(H)` against `max(S)` and claimed that using S's better
    # reading made the rule harder to pass. It makes it easier, and the difference is not academic:
    # with S = 100/105 and H = 110/105 both points reproduce within the margin and best-H is under
    # 5% above best-S, so that version resolved the knee — while the first H stands 10% above the
    # first S, which is exactly what §4.6.5 forbids. Comparing against S's *worse* reading is the
    # conservative side, because a knee must survive the least favourable pairing rather than the
    # most favourable one.
    s_worst = min(s1, s2)
    h_best = max(h1, h2)
    upper_ok = not materially_higher(h_best, s_worst)
    notes.append(f"no H beats any S: {'yes' if upper_ok else 'NO'} "
                 f"(best H {h_best:.1f}/s vs weakest S {s_worst:.1f}/s)")

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
    print(f"margin for 'materially': {MARGIN*100:.0f}% — a preselected engineering materiality")
    print("margin, not a noise floor; reproducibility is a separate gate (§4.6.5)")
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
