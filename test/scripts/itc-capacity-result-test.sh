#!/usr/bin/env bash
#
# Exercise the §4.6.5 selection rule and the §4.6.7 efficiency derivation against synthetic runs.
#
# **Why this exists.** `itc-capacity-result.py` decides whether Iteration C found a knee, and that
# decision is the difference between `VAL-SCALE-6` established and `VAL-SCALE-6` explicitly
# unresolved. The rule has four independent ways to fail — S does not reproduce, H does not
# reproduce, H beats S, a topology is missing runs — and each means something different from a low
# capacity number. A defect here does not produce an obviously wrong answer; it produces a
# plausible efficiency that no reader can distinguish from a real one.
#
# **The synthetic runs are manifests, not measurements.** Each case writes the minimum `run.json`
# the script reads, so the cases test the arithmetic and the rule rather than the harness. No cell
# is driven and nothing here describes any machine.
#
#   ./test/scripts/itc-capacity-result-test.sh
#
# Every case asserts a property that fails when that property is removed: the pass case pins the
# efficiencies, and each failure case pins both the refusal and the withholding of the figure that
# must not be quoted.

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

WORK="${TMPDIR:-/tmp}/itc-capacity-result-test.$$"
mkdir -p "$WORK"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
ok()  { printf '  ok    %s\n' "$*"; pass=$((pass + 1)); }
bad() { printf '  !!    %s\n' "$*"; fail=$((fail + 1)); }

# write_run fabricates one run's manifest: $1 root, $2 dir, $3 rate/s, $4 workers, and optionally
# $5 a quotability level (default capacity) and $6 measurement_sound (default true).
# $7 overrides the measured duration, for the horizon case; everything else defaults to a valid
# 600 s run so each case varies exactly one thing.
write_run() {
  local root="$1" dir="$2" rate="$3" workers="$4" level="${5:-capacity}" sound="${6:-true}" secs="${7:-600}"
  mkdir -p "$root/$dir"
  python3 - "$root/$dir/run.json" "$rate" "$workers" "$level" "$sound" "$secs" <<'PY'
import json, sys
path, rate, workers, level, sound, secs = sys.argv[1:7]
seconds = float(secs)
json.dump({
    "manifest": {"environment": "synthetic"},
    "summary": {
        "successful_mutation_goodput": int(round(float(rate) * seconds)),
        "duration_seconds": seconds,
        "workers_per_group": int(workers),
        "measurement_sound": sound == "true",
    },
    "quotability": {"level": level},
}, open(path, "w"))
PY
}

# A complete, well-behaved bracket at one topology: S reproduces, H reproduces, H does not beat S.
write_topology() {
  local root="$1" groups="$2" s_rate="$3" h_rate="$4" s_level="${5:-16}" h_level="${6:-24}"
  write_run "$root" "g${groups}-s"         "$s_rate" "$s_level"
  write_run "$root" "g${groups}-s-confirm" "$s_rate" "$s_level"
  write_run "$root" "g${groups}-h"         "$h_rate" "$h_level"
  write_run "$root" "g${groups}-h-confirm" "$h_rate" "$h_level"
}

run_result() {
  RESULT_OUT="$WORK/out-$1.txt"
  ./test/scripts/itc-capacity-result.py "$2" > "$RESULT_OUT" 2>&1
  RESULT_STATUS=$?
}

printf '\n--- a resolved bracket at every topology -------------------------------------\n'
ROOT="$WORK/resolved"
# G1 1000/s, G2 1800/s, G4 3200/s. E2 = 1800/2000 = 0.900, E4 = 3200/4000 = 0.800 exactly, so a
# defect in the arithmetic shows as a changed number rather than as a plausible one.
write_topology "$ROOT" 1 1000 1010
write_topology "$ROOT" 2 1800 1810
write_topology "$ROOT" 4 3200 3210
run_result resolved "$ROOT"
[ "$RESULT_STATUS" -eq 0 ] && ok "exit 0 on a complete resolved result" \
  || bad "exit $RESULT_STATUS; see $RESULT_OUT"
grep -q 'E2_local = 1800.0 / (2 x 1000.0) = 0.900' "$RESULT_OUT" \
  && ok "E2_local = 0.900" || bad "E2_local wrong: $(grep E2_local "$RESULT_OUT")"
grep -q 'E4_local = 3200.0 / (4 x 1000.0) = 0.800' "$RESULT_OUT" \
  && ok "E4_local = 0.800" || bad "E4_local wrong: $(grep E4_local "$RESULT_OUT")"
grep -q 'does not discharge VAL-SCALE-5' "$RESULT_OUT" \
  && ok "the result states it is not independently provisioned evidence" \
  || bad "the result does not qualify itself"

printf '\n--- H beats S: the knee is not where reconnaissance put it -------------------\n'
ROOT="$WORK/h-beats-s"
write_topology "$ROOT" 1 1000 1200      # H is 20% higher — well past the 5% margin
write_topology "$ROOT" 2 1800 1810
write_topology "$ROOT" 4 3200 3210
run_result h-beats-s "$ROOT"
[ "$RESULT_STATUS" -ne 0 ] && ok "refused: exit $RESULT_STATUS" \
  || bad "exit 0 — a higher H was accepted as a knee"
grep -q 'KNEE UNRESOLVED' "$RESULT_OUT" \
  && ok "the knee is reported unresolved" || bad "the output does not say the knee is unresolved"
# The property that matters most: G1 is the denominator of both efficiencies, so an unresolved G1
# must withhold them rather than compute them from a level nothing selected.
grep -q 'G1_local is the denominator of both and is not resolved' "$RESULT_OUT" \
  && ok "both efficiencies withheld when the denominator is unresolved" \
  || bad "an efficiency was derived from an unresolved G1"
grep -q 'E4_local = [0-9]' "$RESULT_OUT" \
  && bad "an efficiency figure was printed anyway" || ok "no efficiency figure appears at all"

printf '\n--- a crossed pair: H beats S while the extremes look fine -------------------\n'
# **The case that exposed the rule being weaker than §4.6.5, not conservative** (review finding,
# 2026-08-19). S = 100/105 and H = 110/105: both points reproduce within 5%, and best-H (110) is
# under 5% above best-S (105) — so comparing extreme against extreme resolves the knee. But the
# first H stands 10% above the first S, which is exactly what §4.6.5's pairwise rule forbids: the
# deciding H *and* its confirmation must each fail to beat the selected point's runs.
ROOT="$WORK/crossed-pair"
write_run "$ROOT" "g1-s"         100 12
write_run "$ROOT" "g1-s-confirm" 105 12
write_run "$ROOT" "g1-h"         110 16
write_run "$ROOT" "g1-h-confirm" 105 16
run_result crossed-pair "$ROOT"
# **Assert the knee, not the exit status.** This root holds G1 only, so the script exits non-zero
# either way for the missing topologies — an exit-code assertion here passes for the wrong reason,
# which mutation testing showed it doing.
grep -q 'KNEE UNRESOLVED' "$RESULT_OUT" \
  && ok "the knee is unresolved" \
  || bad "an H standing 10% above its corresponding S resolved the knee"
grep -q 'G1_local = withheld' "$RESULT_OUT" \
  && ok "and G1_local is withheld" || bad "G1_local was reported from a crossed pair"
grep -q 'no H beats any S: NO' "$RESULT_OUT" \
  && ok "the pairwise test is the one reported" || bad "the output does not report the pairwise test"
# The discriminating detail: the comparison must be against S's *weakest* reading, not its best.
grep -q 'weakest S 100.0' "$RESULT_OUT" \
  && ok "compared against the weakest S, which is the conservative side" \
  || bad "the comparison did not use the weakest S: $(grep 'beats any S' "$RESULT_OUT")"

printf '\n--- a sound run of the wrong horizon is refused ------------------------------\n'
# 60 s cells are sound, certify `capacity`, and answer a different question. The stage validates
# duration when it drives a cell; this script is documented as runnable directly, so it must not
# depend on having been reached that way.
ROOT="$WORK/short-horizon"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g1-h" 1010 16 capacity true 60
run_result short-horizon "$ROOT"
[ "$RESULT_STATUS" -ne 0 ] && ok "refused: exit $RESULT_STATUS" \
  || bad "exit 0 — a 60 s manifest entered a 600 s horizon comparison"
grep -q "measured 60s against §4.6.5's fixed 600s horizon" "$RESULT_OUT" \
  && ok "the refusal names the horizon and the section that fixes it" \
  || bad "the refusal does not explain itself"

printf '\n--- the four runs must be one comparison -------------------------------------\n'
# A point and its confirmation at different levels are not two observations of one point, however
# well they agree.
ROOT="$WORK/split-point"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g1-s-confirm" 1000 14
run_result split-point "$ROOT"
grep -q 'not two observations of one point' "$RESULT_OUT" \
  && ok "S at 12 with its confirmation at 14 is refused" || bad "a split point was accepted"

# H at or below S is not a deciding point, whatever its rate says.
ROOT="$WORK/h-not-higher"
write_topology "$ROOT" 1 1000 1010 16 16
run_result h-not-higher "$ROOT"
grep -q 'H is by definition a higher level' "$RESULT_OUT" \
  && ok "H at the same level as S is refused" || bad "H at S's level was accepted"

printf '\n--- S does not reproduce -----------------------------------------------------\n'
ROOT="$WORK/s-unstable"
write_topology "$ROOT" 1 1000 1010
write_topology "$ROOT" 2 1800 1810
write_topology "$ROOT" 4 3200 3210
# Replace G4's confirmation with a reading 20% away from its own selected point.
write_run "$ROOT" "g4-s-confirm" 2600 16
run_result s-unstable "$ROOT"
[ "$RESULT_STATUS" -ne 0 ] && ok "refused: exit $RESULT_STATUS" \
  || bad "exit 0 — a point that disagrees with its own confirmation was accepted"
grep -q 'S reproduces: NO' "$RESULT_OUT" \
  && ok "the non-reproducing point is named" || bad "the output does not name it"
grep -q 'E4_local withheld' "$RESULT_OUT" \
  && ok "E4_local withheld" || bad "E4_local was derived from a point that did not reproduce"
# E2 is unaffected and must still be reported: one unresolved topology does not invalidate another.
grep -q 'E2_local = 1800.0 / (2 x 1000.0) = 0.900' "$RESULT_OUT" \
  && ok "E2_local still reported — one unresolved topology does not withhold the others" \
  || bad "E2_local was withheld along with E4_local"

printf '\n--- H does not reproduce -----------------------------------------------------\n'
ROOT="$WORK/h-unstable"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g1-h-confirm" 700 24     # the two H observations disagree by 44%
run_result h-unstable "$ROOT"
grep -q 'H reproduces: NO' "$RESULT_OUT" \
  && ok "the disagreeing H is named" || bad "a 44% disagreement between H observations passed"
[ "$RESULT_STATUS" -ne 0 ] && ok "refused: exit $RESULT_STATUS" || bad "exit 0"

printf '\n--- an incomplete bracket is not a result ------------------------------------\n'
ROOT="$WORK/incomplete"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g2-s" 1800 16            # one run of four
run_result incomplete "$ROOT"
grep -q 'G2: incomplete' "$RESULT_OUT" \
  && ok "the incomplete topology is named" || bad "three missing runs went unreported"
grep -q 'E2_local withheld' "$RESULT_OUT" \
  && ok "E2_local withheld" || bad "E2_local was derived from one run out of four"

printf '\n--- soundness and provenance are checked before any arithmetic ---------------\n'
ROOT="$WORK/unsound"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g1-s" 1000 16 capacity false
run_result unsound "$ROOT"
grep -q 'measurement_sound is false' "$RESULT_OUT" \
  && ok "an unsound run is refused, not averaged" || bad "an unsound run entered the comparison"

ROOT="$WORK/below-capacity"
write_topology "$ROOT" 1 1000 1010
write_run "$ROOT" "g1-s" 1000 16 local
run_result below-capacity "$ROOT"
grep -q "certifies at 'local', below capacity" "$RESULT_OUT" \
  && ok "a run below the capacity level is refused" \
  || bad "a local-level run was used for a capacity result"

printf '\n%d passed, %d failed\n\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
