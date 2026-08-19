#!/usr/bin/env bash
#
# Exercise the PR4b reconnaissance walk against synthetic rate curves.
#
# **Why this exists.** The walk in `itc-local-experiment.sh recon` decides which two worker levels
# receive four retained 600 s runs each (ag-sept-validation-plan.md §4.6.4-§4.6.5). It runs
# unattended for the better part of an hour, and a defect in it costs that hour and then hands back
# the wrong bracket — the expensive kind of wrong, because the retained runs are sound and describe
# the wrong place. Reasoning about a two-directional search with two end refusals is not evidence
# that it works.
#
# **It drives the real code, not a copy.** `ITC_RECON_PROBE_CMD` substitutes where a rate comes
# from and nothing else, so every case below runs the same direction decision, the same walks, the
# same ladder-end refusals and the same lower-side check that a measured run does. A copy of the
# logic here would pass forever after the original changed.
#
#   ./test/scripts/itc-recon-walk-test.sh
#
# Each case asserts a *specific* property and is written so it fails when that property is removed:
# the peak cases pin the selected level, the tie case pins the lower-side refusal, and the two
# monotonic cases pin the refusals that stop the walk from reporting a ladder end as a frontier.

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

WORK="${TMPDIR:-/tmp}/itc-recon-walk-test.$$"
mkdir -p "$WORK"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

ok()   { printf '  ok    %s\n' "$*"; pass=$((pass + 1)); }
bad()  { printf '  !!    %s\n' "$*"; fail=$((fail + 1)); }

# A synthetic prober is a curve expressed as "level:rate" pairs. Levels absent from the curve
# return 1.0/s, which is well below anything the curve names — so a walk that probes somewhere the
# case did not anticipate produces an obviously wrong answer rather than a plausible one.
make_prober() {
  local path="$WORK/prober-$1.sh" curve="$2"
  cat > "$path" <<PROBER
#!/usr/bin/env bash
# \$1 is the group count, \$2 the worker level. The group count is ignored: these cases test the
# search, and a per-topology curve would test the harness instead.
for pair in $curve; do
  if [ "\${pair%%:*}" = "\$2" ]; then printf '%s\n' "\${pair#*:}"; exit 0; fi
done
printf '1.0\n'
PROBER
  chmod +x "$path"
  printf '%s\n' "$path"
}

# run_case drives one reconnaissance at G1 with a synthetic curve and captures everything.
#   $1 name  $2 curve  $3 ladder  $4 start
#
# Results come back in CASE_STATUS and CASE_OUT rather than on stdout. The first version of this
# printed the status for `$(run_case ...)` to capture, which put the CASE_OUT assignment in a
# subshell where it evaporated — so every failure branch reported an empty path and the first
# unconditional read of it aborted the run under `set -u`. Globals for both, or neither.
CASE_STATUS=""
CASE_OUT=""
run_case() {
  local name="$1" curve="$2" ladder="$3" start="$4" prober
  prober="$(make_prober "$name" "$curve")"
  CASE_OUT="$WORK/out-$name.txt"

  ITC_RECON_PROBE_CMD="$prober" \
  ITC_RECON_GROUPS=1 \
  ITC_RECON_LADDER="$ladder" \
  ITC_RECON_START="$start" \
  RESULTS_GROUP="selftest-$name" \
    ./test/scripts/itc-local-experiment.sh recon > "$CASE_OUT" 2>&1
  CASE_STATUS=$?
}

# selected reads the decision out of the retained report rather than out of the log, because the
# report is what a person acts on.
#
# **The field is extracted by position from the label, not as the last field on the line.** These
# read `$NF` until the report gained a parenthetical after each level ("selected S 12 (lowest level
# on the discovered plateau)"), at which point every assertion started comparing against `plateau)`
# and eight cases failed at once. A report is allowed to explain itself; a test that reads it must
# not depend on the explanation being absent.
selected() { sed -n 's/^  selected S  *\([0-9][0-9]*\).*/\1/p' "test/results/selftest-$1/recon-G1.txt" 2>/dev/null; }
deciding() { sed -n 's/^  deciding H  *\([0-9][0-9]*\).*/\1/p' "test/results/selftest-$1/recon-G1.txt" 2>/dev/null; }
probed()   { awk '/^  workers/{f=1;next} /^$/{f=0} f{print $1}' \
               "test/results/selftest-$1/recon-G1.txt" 2>/dev/null | sort -n | tr '\n' ' '; }

LADDER="2 4 8 12 16 24 32 48 64 96 128"

printf '\n--- peak above the start -----------------------------------------------------\n'
# Rises to 24 and then falls away. The walk must climb from 16 to 24, stop at 32, and select 24.
run_case peak-above "8:900 12:1100 16:1250 24:1500 32:1450 48:1300" "$LADDER" 16
if [ "$CASE_STATUS" -eq 0 ]; then ok "the walk completed"; else bad "exit $CASE_STATUS; see $CASE_OUT"; fi
[ "$(selected peak-above)" = "24" ] \
  && ok "selected S=24, the peak" || bad "selected S=$(selected peak-above), expected 24"
[ "$(deciding peak-above)" = "32" ] \
  && ok "deciding H=32, the level above the peak" || bad "deciding H=$(deciding peak-above), expected 32"
grep -q '^  lower-side check    pass' "test/results/selftest-peak-above/recon-G1.txt" \
  && ok "lower-side check passed: 24 beats 16" || bad "lower-side check did not pass"
# 16 is probed as the start, 24 as the climb, 32 as the stop. 12 is not needed: 16 was measured on
# the way up and is already the lower neighbour of the peak. A walk that re-probes it is spending
# four minutes to answer a settled question.
[ "$(probed peak-above)" = "16 24 32 " ] \
  && ok "probed exactly 16 24 32 — no level driven twice" || bad "probed [$(probed peak-above)]"

printf '\n--- peak at the start --------------------------------------------------------\n'
# Nothing above 16 helps, so the walk must turn around and observe 12 before it may propose 16.
run_case peak-at-start "8:700 12:1000 16:1250 24:1240 32:1100" "$LADDER" 16
if [ "$CASE_STATUS" -eq 0 ]; then ok "the walk completed"; else bad "exit $CASE_STATUS; see $CASE_OUT"; fi
[ "$(selected peak-at-start)" = "16" ] \
  && ok "selected S=16" || bad "selected S=$(selected peak-at-start), expected 16"
[ "$(deciding peak-at-start)" = "24" ] \
  && ok "deciding H=24" || bad "deciding H=$(deciding peak-at-start), expected 24"
# The property that matters: 12 was actually driven. Without it S=16 would be proposed with its
# lower side never observed, which is the gap the check exists to close.
case " $(probed peak-at-start) " in
  *" 12 "*) ok "12 was probed, so the lower side of S was observed" ;;
  *) bad "12 was never probed: [$(probed peak-at-start)]" ;;
esac

printf '\n--- peak below the start -----------------------------------------------------\n'
# The frontier is at 8. The walk must descend 16 -> 12 -> 8, stop at 4, and select 8.
run_case peak-below "2:400 4:700 8:1400 12:1150 16:1000 24:900" "$LADDER" 16
if [ "$CASE_STATUS" -eq 0 ]; then ok "the walk completed"; else bad "exit $CASE_STATUS; see $CASE_OUT"; fi
[ "$(selected peak-below)" = "8" ] \
  && ok "selected S=8, below the start" || bad "selected S=$(selected peak-below), expected 8"
[ "$(deciding peak-below)" = "12" ] \
  && ok "deciding H=12" || bad "deciding H=$(deciding peak-below), expected 12"

printf '\n--- a plateau selects its lowest level ---------------------------------------\n'
# 16 reads 2% above 12, inside the environment's own demonstrated reproducibility, so the two are
# one plateau. S must be 12: both deliver the same Goodput and the lower does it with less
# queueing, so quoting 16 would attribute capacity to four workers that bought nothing.
run_case plateau "8:900 12:1225 16:1250 24:1240 32:1100" "$LADDER" 16
if [ "$CASE_STATUS" -eq 0 ]; then ok "the walk completed"; else bad "exit $CASE_STATUS; see $CASE_OUT"; fi
[ "$(selected plateau)" = "12" ] \
  && ok "selected S=12, the bottom of the plateau, not the 1250 reading at 16" \
  || bad "selected S=$(selected plateau), expected 12"
[ "$(deciding plateau)" = "16" ] \
  && ok "deciding H=16, higher and not materially better" || bad "deciding H=$(deciding plateau), expected 16"
grep -q '^  lower-side check    pass' "test/results/selftest-plateau/recon-G1.txt" \
  && ok "lower-side passes: 12 beats 8 materially" || bad "lower-side did not pass"
# 8 must have been driven: it is what establishes that 12 is the *bottom* of the plateau rather
# than a point part-way down it.
case " $(probed plateau) " in
  *" 8 "*) ok "8 was probed, establishing where the plateau ends" ;;
  *) bad "8 was never probed: [$(probed plateau)]" ;;
esac

printf '\n--- the observed G2/G4 shape, as a regression case --------------------------\n'
# The real 2026-08-19 G2 reconnaissance: 12 -> 2581.3, 16 -> 2637.3 (2.2% apart), 24 -> 2431.7.
# Under the first rule this refused its own candidate; under the plateau rule it must select 12.
# Retained as a case so the finding cannot be undone silently.
run_case g2-observed "8:2100 12:2581.3 16:2637.3 24:2431.7" "$LADDER" 16
if [ "$CASE_STATUS" -eq 0 ]; then ok "the walk completed"; else bad "exit $CASE_STATUS; see $CASE_OUT"; fi
[ "$(selected g2-observed)" = "12" ] \
  && ok "G2's observed shape selects S=12" || bad "selected S=$(selected g2-observed), expected 12"
[ "$(deciding g2-observed)" = "16" ] \
  && ok "and H=16" || bad "deciding H=$(deciding g2-observed), expected 16"

printf '\n--- S never has an unobserved lower side ------------------------------------\n'
# **Stated as an invariant over every resolving case rather than checked once.** Mutation testing
# showed the single-case version passing for the wrong reason: deleting the explicit lower-side probe
# from the stage changed nothing, because every direction of the walk already measures the level
# below its own candidate. That makes "the lower neighbour is always in the table" a property of the
# search, worth asserting where it can catch a future walk that stops holding it.
#
# **It runs here, below every case it names.** It was originally placed above three of them, where
# `selected` returned empty, `lower_of` returned empty, and the `case` matched the empty string
# inside a padded list — so it reported five passes while testing nothing. An assertion that cannot
# fail is worse than a missing one, because it reads as coverage.
lower_of() {
  local ladder="$2" s; s="$(selected "$1")"
  [ -n "$s" ] || { printf 'NO-SELECTION' ; return; }
  awk -v s="$s" '{for(i=1;i<=NF;i++) if($i==s && i>1){print $(i-1); exit}}' <<< "$ladder"
}
for case_name in peak-above peak-at-start peak-below plateau g2-observed; do
  lower="$(lower_of "$case_name" "$LADDER")"
  if [ "$lower" = "NO-SELECTION" ] || [ -z "$lower" ]; then
    bad "$case_name: no selected S in the report, so this invariant tested nothing"
    continue
  fi
  case " $(probed "$case_name") " in
    *" $lower "*) ok "$case_name: S=$(selected "$case_name") has its lower neighbour $lower probed" ;;
    *) bad "$case_name: S=$(selected "$case_name") proposed with lower side $lower unobserved" ;;
  esac
done

printf '\n--- a plateau running off the bottom is refused ------------------------------\n'
# Flat all the way down a short ladder: nothing below is materially worse, so no level satisfies
# the lower-side condition and the frontier is outside the range. Refusing is the honest answer.
run_case plateau-off-bottom "8:1240 12:1225 16:1250 24:1100" "8 12 16 24" 16
[ "$CASE_STATUS" -ne 0 ] \
  && ok "refused: exit $CASE_STATUS" || bad "exit 0 — a ladder-bottom plateau was reported as a frontier"
grep -q "ITC_RECON_LADDER='1 " "$CASE_OUT" \
  && ok "the refusal names how to extend downward" || bad "the refusal does not say how to extend"

printf '\n--- the walk may not report a ladder end as a frontier ----------------------\n'
# Monotonic to the top of a short ladder. §4.6.4 sets no maximum, so the honest answer is that the
# bracket is outside the starting range — not that 32 is the frontier.
run_case runs-off-top "8:900 12:1100 16:1300 24:1600 32:2000" "8 12 16 24 32" 16
[ "$CASE_STATUS" -ne 0 ] \
  && ok "refused at the top: exit $CASE_STATUS" || bad "exit 0 — the ladder's top was reported as a frontier"
grep -q 'ITC_RECON_LADDER' "$CASE_OUT" \
  && ok "the refusal names the fix" || bad "the refusal does not say how to extend the ladder"

# Monotonic to the bottom, the same property in the other direction.
run_case runs-off-bottom "8:2000 12:1600 16:1300 24:1100" "8 12 16 24" 16
[ "$CASE_STATUS" -ne 0 ] \
  && ok "refused at the bottom: exit $CASE_STATUS" || bad "exit 0 — the ladder's bottom was reported as a frontier"

printf '\n--- a synthetic run cannot be mistaken for a measurement --------------------\n'
grep -q 'SYNTHETIC PROBER' "test/results/selftest-peak-above/recon-G1.txt" \
  && ok "the report is stamped synthetic" || bad "the report carries no synthetic stamp"
# The fence that matters most: a synthetic prober must not be able to write where a real probe does.
# The status is assigned before the grep, because `$?` after a second command is that command's.
real_before="$(ls -A test/results/pr4b-recon 2>/dev/null | wc -l)"
ITC_RECON_PROBE_CMD="$(make_prober fence "16:1")" ITC_RECON_GROUPS=1 \
  ./test/scripts/itc-local-experiment.sh recon > "$WORK/fence.txt" 2>&1
fence_status=$?
if [ "$fence_status" -ne 0 ] && grep -q 'synthetic prober may not write' "$WORK/fence.txt"; then
  ok "refuses the default pr4b-recon group, and says why"
else
  bad "a synthetic prober was not refused the real results group (exit $fence_status)"
fi
# **The refusal is only a fence if nothing was written before it.** Asserting the message alone
# passed while the stage was still leaving a fixture-sizing artifact in pr4b-recon/, because the
# check sat below the write. This is the property; the message is the explanation.
real_after="$(ls -A test/results/pr4b-recon 2>/dev/null | wc -l)"
[ "$real_before" = "$real_after" ] \
  && ok "wrote nothing into test/results/pr4b-recon/ before refusing" \
  || bad "the refused invocation left $((real_after - real_before)) file(s) in pr4b-recon/"

rm -rf test/results/selftest-*

printf '\n%d passed, %d failed\n\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
