#!/usr/bin/env bash
#
# Exercise the capacity stage's bracket validation, without driving a cell.
#
# **Why this exists.** `ITC_CAPACITY_S`/`ITC_CAPACITY_H` let the maintainer run every arm at one
# common bracket, which is what the 2026-08-19 local reconnaissance needed: it selected S=12, S=8,
# S=12 across the three topologies, where the dissenting 8 read within 0.8% of its own 12 — inside
# the variation the same level shows between sessions. E2 and E4 compare topologies, so the arms have
# to sit at the same demand-per-group.
#
# That variable is also the one way to move the operating point by hand, so it is validated against
# each topology's own retained probes rather than trusted. This proves the validation still refuses
# what it cannot support. A guard that silently stopped guarding would leave a plausible efficiency
# measured at a level nothing selected.
#
#   ./test/scripts/itc-capacity-bracket-test.sh
#
# Every refusal below happens before any topology is raised, so no daemon is needed. The accept case
# pre-creates the four retained cells so the stage's resume branch skips them, which lets the
# validation be observed reaching its conclusion without a run.

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

WORK="${TMPDIR:-/tmp}/itc-capacity-bracket-test.$$"
mkdir -p "$WORK"
GROUP="selftest-bracket-$$"
trap 'rm -rf "$WORK" "test/results/$GROUP" "test/results/${GROUP}-recon"' EXIT

pass=0
fail=0
ok()  { printf '  ok    %s\n' "$*"; pass=$((pass + 1)); }
bad() { printf '  !!    %s\n' "$*"; fail=$((fail + 1)); }

RECON="test/results/${GROUP}-recon"
mkdir -p "$RECON" "test/results/$GROUP"

# A reconnaissance report in the shape the stage reads: G1's real 2026-08-19 probe table.
cat > "$RECON/recon-G1.txt" <<'REPORT'
PR4b reconnaissance, G1 (ag-sept-validation-plan.md §4.6.4)

  workers     probe/s  shape    spread  cell
  8            1342.8  flat       1.28  x/
  12           1411.9  flat       1.28  x/
  16           1307.0  flat       1.20  x/
  24           1225.9  flat       1.25  x/

  selected S          12   (lowest level on the discovered plateau)
  deciding H          16   (higher, and not materially better than S)
  lower-side check    pass — 12 beats 8 by more than 5%
REPORT

printf 'SLOTS=15000\nDEEPEST_LEVEL=16\nDERIVED_FROM=synthetic\n' \
  > "test/results/$GROUP/retained-fixture.env"

# run_capacity invokes the stage with a bracket override and captures everything.
run_capacity() {
  CAP_OUT="$WORK/out-$1.txt"
  shift
  env ITC_RECON_RESULTS_GROUP="${GROUP}-recon" RESULTS_GROUP="$GROUP" ITC_CAPACITY_GROUPS=1 "$@" \
    ./test/scripts/itc-local-experiment.sh capacity > "$CAP_OUT" 2>&1
  CAP_STATUS=$?
}

printf '\n--- a level this topology never probed -------------------------------------\n'
# 32 is not in G1's table. Accepting it would run the arm at a level with no local evidence at all.
run_capacity unprobed ITC_CAPACITY_S=32 ITC_CAPACITY_H=48
[ "$CAP_STATUS" -ne 0 ] && ok "refused: exit $CAP_STATUS" || bad "exit 0 — an unprobed level was accepted"
grep -q 'never probed 32 workers/group' "$CAP_OUT" \
  && ok "the refusal names the level and the topology" || bad "the refusal does not explain itself"
grep -q 'ITC_RECON_ONLY' "$CAP_OUT" \
  && ok "the refusal names how to get the evidence" || bad "the refusal does not say what to do"

printf '\n--- a level materially below this topology best ------------------------------\n'
# 16 read 1307.0 against G1's best of 1411.9 — 8.0% worse, so it is off the plateau. Running there
# would measure the arm below its own frontier and understate every efficiency dividing it. H=24 is
# probed and above 16, so this case reaches the plateau comparison rather than an earlier refusal —
# the first version used S=24/H=32 and refused on H=32 being unprobed, testing the wrong thing.
run_capacity off-plateau ITC_CAPACITY_S=16 ITC_CAPACITY_H=24
[ "$CAP_STATUS" -ne 0 ] && ok "refused: exit $CAP_STATUS" || bad "exit 0 — an off-plateau level was accepted"
grep -q 'is not on' "$CAP_OUT" \
  && ok "the refusal says the level is not on the plateau" || bad "the refusal does not say why"
grep -q 'below its own frontier' "$CAP_OUT" \
  && ok "and names the consequence" || bad "the refusal does not name the consequence"
grep -q 'against its own best of' "$CAP_OUT" \
  && ok "and quotes both rates so the judgement can be checked" || bad "the refusal quotes no rates"

printf '\n--- H must be above S --------------------------------------------------------\n'
run_capacity h-below ITC_CAPACITY_S=12 ITC_CAPACITY_H=8
[ "$CAP_STATUS" -ne 0 ] && ok "refused: exit $CAP_STATUS" || bad "exit 0 — H below S was accepted"
grep -q 'is not above' "$CAP_OUT" \
  && ok "the refusal names the ordering requirement" || bad "the refusal does not explain itself"

printf '\n--- H materially better than S is the bracket not being found ----------------\n'
# **The same comparison catches this, and that is the point of the case.** A separate "H beats S"
# check was removed as unreachable: `best_rate` is the maximum over probed levels and H is one of
# them, so an H that materially beats S has already put S materially below best. This case proves
# §4.6.4's bracket-not-found condition is still refused after that removal, rather than having gone
# with it. G2's synthetic table has 16 well above 12, so S=12/H=16 is exactly that shape.
cat > "$RECON/recon-G2.txt" <<'REPORT'
PR4b reconnaissance, G2 (ag-sept-validation-plan.md §4.6.4)

  workers     probe/s  shape    spread  cell
  8            1000.0  flat       1.10  x/
  12           1100.0  flat       1.10  x/
  16           1400.0  flat       1.10  x/

  selected S          8   (lowest level on the discovered plateau)
  deciding H          12   (higher, and not materially better than S)
  lower-side check    pass — 8 beats 4 by more than 5%
REPORT
CAP_OUT="$WORK/out-h-better.txt"
env ITC_RECON_RESULTS_GROUP="${GROUP}-recon" RESULTS_GROUP="$GROUP" ITC_CAPACITY_GROUPS=2 \
  ITC_CAPACITY_S=12 ITC_CAPACITY_H=16 \
  ./test/scripts/itc-local-experiment.sh capacity > "$CAP_OUT" 2>&1
CAP_STATUS=$?
[ "$CAP_STATUS" -ne 0 ] && ok "refused: exit $CAP_STATUS" || bad "exit 0 — an H that beats S was accepted"
grep -q "bracket not found" "$CAP_OUT" \
  && ok "the refusal cites §4.6.4's 'bracket not found'" || bad "the refusal does not cite the rule"
grep -q 'move the bracket' "$CAP_OUT" \
  && ok "and says to move the bracket up rather than retaining four runs" \
  || bad "the refusal does not say what to do instead"

printf '\n--- S without H is refused ---------------------------------------------------\n'
run_capacity s-only ITC_CAPACITY_S=12
[ "$CAP_STATUS" -ne 0 ] && ok "refused: exit $CAP_STATUS" || bad "exit 0 — S without H was accepted"
grep -q 'without its deciding point' "$CAP_OUT" \
  && ok "the refusal says why a lone S cannot resolve a knee" || bad "the refusal does not explain"

printf '\n--- the accepted bracket, observed without driving anything -------------------\n'
# Pre-create the four retained cells so the resume branch skips every run. What is asserted is that
# validation accepted 12/16 for G1 and said so, quoting this topology's own probe rate.
for role in s h s-confirm h-confirm; do
  mkdir -p "test/results/$GROUP/g1-$role/cell-01"
  python3 - "test/results/$GROUP/g1-$role/cell-01/run.json" <<'PY'
import json, sys
json.dump({"manifest": {}, "summary": {
    "successful_mutation_goodput": 600000, "duration_seconds": 600.0,
    "workers_per_group": 12, "measurement_sound": True},
    "quotability": {"level": "capacity"}}, open(sys.argv[1], "w"))
PY
done
run_capacity accepted ITC_CAPACITY_S=12 ITC_CAPACITY_H=16
grep -q 'common bracket S=12 H=16' "$CAP_OUT" \
  && ok "the bracket is accepted and recorded" || bad "the accepted bracket was not logged"
grep -q 'this topology probed 12 at 1411.9/s against its own best 1411.9/s' "$CAP_OUT" \
  && ok "and it quotes this topology's own probe rate, not the override" \
  || bad "the log does not show the evidence the override was checked against"
grep -q 'tearing down the topology' "$CAP_OUT" \
  && bad "a topology was raised despite every run already being retained" \
  || ok "no cell was driven: the resume branch skipped all four"

printf '\n%d passed, %d failed\n\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
