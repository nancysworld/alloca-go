#!/usr/bin/env bash
#
# Asserts that a scraped sample can say which topology it came from.
#
# The defect this exists to catch shipped: `prometheus.yml` set `topology=single-instance-local`
# as a job-wide relabel, so every Iteration C rehearsal sample was stamped as the PR2
# single-instance experiment. It went unnoticed through implementation and review because the
# label was *present and well-formed* — every query succeeded, every CSV carried rows, and the
# populated-series gate passed. Only reading a retained value caught it.
#
# So this checks the property, not the plumbing: topology is owned by whichever document
# discovered the target, and its value distinguishes the rungs.
#
# Needs no daemon — itc-obs-targets.sh is pure text generation, and the other two assertions are
# static reads. That is why it can be a merge gate.
set -euo pipefail

cd "$(dirname "$0")/../.."

pass=0
fail=0

ok()   { echo "  ok    $1"; pass=$((pass + 1)); }
bad()  { echo "  FAIL  $1" >&2; fail=$((fail + 1)); }

check() { # description, expected, actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (want '$2', got '$3')"; fi
}

echo "itc-obs-targets-test: topology label ownership"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# --- every rung stamps its own topology, on every unit -----------------------------------------
for groups in 1 2 4; do
  out="$work/itc-g$groups.json"
  TARGETS_DIR="$work/targets-$groups" OUT="$out" ITC_GROUPS="$groups" \
    ./test/scripts/itc-obs-targets.sh >/dev/null 2>&1

  labelled="$(grep -c "\"topology\": \"itc-g$groups\"" "$out" || true)"
  check "G$groups stamps topology=itc-g$groups on all $groups target(s)" "$groups" "$labelled"

  # The rung has to be *in* the value. A constant like "iteration-c" would satisfy "a topology
  # label exists" while still merging G1, G2 and G4 into one indistinguishable population —
  # which is the same class of defect one level quieter.
  wrong=0
  for other in 1 2 4; do
    [ "$other" = "$groups" ] && continue
    grep -q "\"topology\": \"itc-g$other\"" "$out" && wrong=1
  done
  check "G$groups carries no other rung's topology" "0" "$wrong"

  # A sample that cannot say which unit it came from cannot be aggregated per authority.
  authorities="$(grep -c '"authority"' "$out" || true)"
  check "G$groups labels every target with its authority" "$groups" "$authorities"
done

# --- the job-wide relabel must not come back ---------------------------------------------------
# This is the actual regression. A static relabel silently overwrites whatever the target
# document said, so its mere presence re-breaks the property above however correct the targets are.
if grep -qE '^\s*-\s*target_label:\s*topology' deploy/observability/prometheus.yml; then
  bad "prometheus.yml sets topology job-wide (it must be owned by the target documents)"
else
  ok "prometheus.yml sets no job-wide topology relabel"
fi

# --- the PR2 host target keeps its own identity ------------------------------------------------
# Removing the relabel must not leave the single-instance path unlabelled; ownership moved, it
# did not disappear.
if grep -q '"topology": "single-instance-local"' test/scripts/obs-target.sh; then
  ok "obs-target.sh stamps topology=single-instance-local on the PR2 host target"
else
  bad "obs-target.sh no longer labels the PR2 host target with its topology"
fi

echo
echo "itc-obs-targets-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
