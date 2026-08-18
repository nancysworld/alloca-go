#!/usr/bin/env bash
#
# Discriminating tests for itc-cpu-layout.sh.
#
# The layout check is what stops a bad CPU partition before an image build and four database
# containers, and it is bash — so a green Go suite says nothing about whether it still works
# (CLAUDE.md, "Validation discipline"). These cases run the real script and assert on its exit
# status and its message.
#
# It belongs in `make ci` for the same reason check-build-context.sh does: it needs nothing an
# operator would have to provide — no daemon, no database, no judgement — only bash, taskset and
# a few CPUs (`../../docs/design/project-structure.md` §1). The defect it catches is introduced
# by editing a script anyone can edit without ever raising a topology.
#
# **The machine's apparent size is controlled with taskset, not with an override inside the
# script.** `nproc` — and so the script's idea of how big the machine is — reports the CPUs
# available to the *calling process*, which an affinity mask changes, so `taskset -c 0-3`
# presents a genuine four-CPU machine without the script knowing it is under test. An
# `ITC_CPUS_AVAILABLE` seam would have been less work and is exactly the wrong shape: it is a
# documented way to tell the script the machine is bigger than it is, and nothing else checks
# the partition against the machine. A test hook that can defeat the property under test does
# not stay a test hook.
#
# Cases that assert the *shipped* partition need a machine the size the rehearsal assumes and
# are skipped on a smaller one. Every negative case is built to fit in four CPUs, so the checks
# themselves are gated everywhere, CI included.
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

SCRIPT=./test/scripts/itc-cpu-layout.sh

for tool in bash taskset nproc; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "itc-cpu-layout-test: $tool is not available; cannot exercise the layout check" >&2
    exit 1
  fi
done

available="$(nproc)"
passed=0; failed=0; skipped=0

# run <mask> <VAR=VALUE>... — run the layout script under an affinity mask, or unmasked when the
# mask is "-". The environment is deliberately not inherited beyond PATH: a set ITC_CPUS_* in the
# operator's shell would otherwise decide whether these cases pass.
run() {
  local mask="$1"; shift
  if [ "$mask" = "-" ]; then
    env -i PATH="$PATH" "$@" bash "$SCRIPT" 2>&1
  else
    env -i PATH="$PATH" "$@" taskset -c "$mask" bash "$SCRIPT" 2>&1
  fi
}

# needs <cpus> — skip a case the machine cannot present a mask for.
needs() {
  if [ "$available" -lt "$1" ]; then
    printf '  SKIP  %s (needs %s CPUs, machine has %s)\n' "$CASE" "$1" "$available"
    skipped=$((skipped + 1))
    return 1
  fi
  return 0
}

ok()   { printf '  ok    %s\n' "$1"; passed=$((passed + 1)); }
bad()  { printf '  FAIL  %s\n     %s\n' "$1" "$2"; failed=$((failed + 1)); }

# accepts <mask> <VAR=VALUE>...
accepts() {
  local mask="$1"; shift
  local output status
  output="$(run "$mask" "$@")"; status=$?
  if [ "$status" -eq 0 ]; then
    ok "$CASE"
  else
    bad "$CASE" "refused (exit $status) but must be accepted: $(echo "$output" | tr '\n' ' ')"
  fi
}

# refuses <expected-substring> <mask> <VAR=VALUE>...
refuses() {
  local want="$1" mask="$2"; shift 2
  local output status
  output="$(run "$mask" "$@")"; status=$?
  if [ "$status" -eq 0 ]; then
    bad "$CASE" "accepted, but the partition must be refused"
  elif ! printf '%s' "$output" | grep -qF "$want"; then
    bad "$CASE" "refused without explaining itself; no '$want' in: $(echo "$output" | tr '\n' ' ')"
  else
    ok "$CASE"
  fi
}

echo "itc-cpu-layout-test: machine has $available CPUs"

# --- the shipped partition -------------------------------------------------------------------
#
# ag-sept-pr4.md §2.14's partition, at every rung. This is the case that would actually stop a
# rehearsal, and it needs a machine the size the rehearsal assumes.
for groups in 1 2 4; do
  CASE="shipped partition is accepted at G$groups"
  needs 12 && accepts 0-11 "ITC_GROUPS=$groups"
done

# G1 and G2 must not be refused for CPUs they never raise. Units C and D are given specs that
# would fail every rule if they were checked; the rungs that do not start them must still pass.
for groups in 1 2; do
  CASE="inactive capacity units are not checked at G$groups"
  needs 12 && accepts 0-11 "ITC_GROUPS=$groups" "ITC_CPUS_C=nonsense" "ITC_CPUS_D=0-11"
done

# The generator-headroom control (§2.14): the standard rehearsal rerun with the generator widened
# over the CPUs the partition holds idle. The layout has to accept it or the control cannot be
# performed at all.
CASE="generator-headroom control partition is accepted"
needs 16 && accepts 0-15 "ITC_GROUPS=4" "ITC_CPUS_GENERATOR=8-15"

# --- rescaling -------------------------------------------------------------------------------
#
# A partition rescaled to a smaller machine is legitimate while it keeps the rules. This is the
# positive case that fits in four CPUs, so it is the one proving on CI that the script accepts
# anything at all.
CASE="a rescaled partition that keeps the rules is accepted"
needs 4 && accepts 0-3 "ITC_GROUPS=2" "ITC_CPUS_A=0" "ITC_CPUS_B=1" "ITC_CPUS_GENERATOR=2-3"

# --- the machine -----------------------------------------------------------------------------
#
# Docker would eventually refuse an out-of-range cpuset, but only after an image build and four
# database containers, and its message names neither the partition nor the file that fixes it.
CASE="a partition larger than the machine is refused"
needs 2 && refuses "the partition needs CPU 11 but this machine has 2" 0-1 "ITC_GROUPS=4"

CASE="the refusal names the file that fixes it"
needs 2 && refuses "wslconfig" 0-1 "ITC_GROUPS=4"

# --- overlap ---------------------------------------------------------------------------------
#
# The property Docker structurally cannot check: it is given one container's cpuset at a time and
# never sees the arrangement. Two groups sharing a CPU are not independent, and making them
# independent is the whole reason the partition exists.
CASE="overlapping sets are refused"
needs 4 && refuses "must not overlap" 0-3 \
  "ITC_GROUPS=2" "ITC_CPUS_A=0" "ITC_CPUS_B=0" "ITC_CPUS_GENERATOR=1-2"

# --- the two size rules ----------------------------------------------------------------------
#
# §2.12: the generator is the one host whose saturation would invalidate a point rather than
# describe one. Equal is refused as well as smaller — the rule is strictly larger.
CASE="a generator smaller than a capacity unit is refused"
needs 4 && refuses "must be strictly larger" 0-3 \
  "ITC_GROUPS=1" "ITC_CPUS_A=0-1" "ITC_CPUS_GENERATOR=2"

# Sized to two CPUs rather than four on purpose. This is the boundary the rule turns on — equal
# is refused, not merely smaller — and a two-CPU case keeps it gated on the smallest runner CI
# might give us, where everything needing four would skip silently.
CASE="a generator equal to a capacity unit is refused"
needs 2 && refuses "must be strictly larger" 0-1 \
  "ITC_GROUPS=1" "ITC_CPUS_A=0" "ITC_CPUS_GENERATOR=1"

# E2 and E4 are ratios across units assumed identical, so an uneven unit does not make the
# measurement smaller, it makes it uninterpretable.
CASE="unequal capacity units are refused"
needs 4 && refuses "like-for-like" 0-3 \
  "ITC_GROUPS=2" "ITC_CPUS_A=0" "ITC_CPUS_B=1-2" "ITC_CPUS_GENERATOR=3"

# That same partition breaks the generator rule too, which is the point of reporting every
# violation rather than the first: an operator rescaling a layout should get the whole list.
CASE="every violation is reported, not just the first"
needs 4 && refuses "must be strictly larger" 0-3 \
  "ITC_GROUPS=2" "ITC_CPUS_A=0" "ITC_CPUS_B=1-2" "ITC_CPUS_GENERATOR=3"

# --- inputs ----------------------------------------------------------------------------------
#
# itc-topology-check.sh and itc-seed.sh admit 1, 2 and 4; this has to agree. The numeric case is
# the dangerous one: G3 used to check a two-unit partition while printing "G3", so an operator
# read a pass for a layout nothing had examined.
for groups in 0 3 5 abc; do
  CASE="ITC_GROUPS=$groups is refused"
  refuses "must be 1, 2 or 4" - "ITC_GROUPS=$groups"
done

# A spec the script cannot parse used to print its complaint and exit zero: `expand` ran inside a
# process substitution, whose status is not the enclosing command's, so the set came back empty
# and the partition was reported valid. The regression is the exit code, not the text.
for spec in x 3-1 1- 0,,2 -1; do
  CASE="a malformed CPU spec ('$spec') is refused"
  refuses "does not name a set of CPUs" - "ITC_GROUPS=1" "ITC_CPUS_A=$spec"
done

echo
printf 'itc-cpu-layout-test: %d passed, %d failed, %d skipped\n' "$passed" "$failed" "$skipped"
[ "$failed" -eq 0 ]
