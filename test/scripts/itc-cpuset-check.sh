#!/usr/bin/env bash
#
# Assert that every container in the rehearsal is actually pinned where the partition says.
#
# The layout check (itc-cpu-layout.sh) proves the partition is *legal* before anything starts.
# This one proves it was *applied*, by reading the cpuset off each running container — which is
# a different question with a different failure mode:
#
#   * `make itc-up` raises the same topology unpartitioned, so a rehearsal started with the
#     wrong target runs four capacity units across every CPU on the machine;
#   * `make obs-up` raises Prometheus and Grafana unpinned, and that is the easy mistake to
#     make, because it is the target every earlier PR used. Prometheus is not idle: it scrapes
#     each unit on a short interval and compacts its TSDB, and unpinned it does that from inside
#     the capacity units' own CPUs — harder at G4 than at G1, against exactly the comparison
#     E2 and E4 are derived from.
#
# Neither failure is visible downstream. The run addresses the right units, every routing check
# passes, certification is clean, and the numbers are contaminated by contention no artifact
# records. That is the shape this repository keeps meeting: a gate reporting success while the
# thing it gated never happened.
#
#   ITC_GROUPS=4 ./test/scripts/itc-cpuset-check.sh
#   ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-15 ./test/scripts/itc-cpuset-check.sh   # headroom control
#
# Monitoring is checked only when it is running: a Prometheus that was never raised cannot
# contend with anything, and a rehearsal is allowed to run without one.
#
# Exits non-zero naming every container whose pinning disagrees with the partition.

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

ITC_GROUPS="${ITC_GROUPS:-4}"

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept/milestone-validation.md §4.6); got '$ITC_GROUPS'" >&2
     exit 1 ;;
esac

ITC_CPUS_A="${ITC_CPUS_A:-0-1}"
ITC_CPUS_B="${ITC_CPUS_B:-2-3}"
ITC_CPUS_C="${ITC_CPUS_C:-4-5}"
ITC_CPUS_D="${ITC_CPUS_D:-6-7}"
ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"

if ! command -v docker >/dev/null 2>&1; then
  echo "itc-cpuset-check: docker is not available" >&2
  exit 1
fi

# Reduce a cpuset spec to a sorted list of individual CPUs, so "0-1", "0,1" and "1,0" compare
# equal. Only comparison is needed here — itc-cpu-layout.sh owns validation, and anything this
# cannot parse is returned marked so it fails the comparison loudly rather than silently
# matching.
normalise() {
  local spec="$1" part lo hi i out=()
  [ -n "$spec" ] || { printf '(unconfined)'; return; }
  local IFS=','
  read -ra parts <<< "$spec"
  for part in "${parts[@]}"; do
    if [[ "$part" == *-* ]]; then
      lo="${part%%-*}"; hi="${part##*-}"
      if ! [[ "$lo" =~ ^[0-9]+$ && "$hi" =~ ^[0-9]+$ ]] || (( lo > hi )); then
        printf 'unparseable:%s' "$spec"; return
      fi
      for ((i = lo; i <= hi; i++)); do out+=("$i"); done
    elif [[ "$part" =~ ^[0-9]+$ ]]; then
      out+=("$part")
    else
      printf 'unparseable:%s' "$spec"; return
    fi
  done
  printf '%s\n' "${out[@]}" | sort -n | uniq | paste -sd,
}

fail=0

# check <container> <expected-spec> <required|optional>
#
# An absent container and an unreadable one are different answers and must not share a branch.
# "Not running" is a legitimate result for monitoring and skips the check; anything else — a
# permission error on the socket, a daemon that is down — means the cpuset was *not verified*,
# and reporting that as a skip would make this script the very thing it exists to catch: a gate
# reporting success while the thing it gated never happened.
check() {
  local container="$1" expected="$2" presence="$3" actual stderr status

  stderr="$(mktemp)"
  actual="$(docker inspect -f '{{.HostConfig.CpusetCpus}}' "$container" 2>"$stderr")"
  status=$?
  if [ $status -ne 0 ]; then
    local message; message="$(cat "$stderr")"; rm -f "$stderr"
    # Docker says "No such object: <name>" for a container that does not exist. Match that
    # narrowly rather than treating every failure as absence.
    if printf '%s' "$message" | grep -qiE 'no such (object|container)'; then
      if [ "$presence" = optional ]; then
        printf '  %-24s not running (not checked)\n' "$container"
        return
      fi
      echo "  ERROR: $container is not running, but G$ITC_GROUPS needs it" >&2
      fail=1
      return
    fi
    echo "  ERROR: could not read $container's cpuset, so it is unverified — not absent:" >&2
    echo "         $message" >&2
    fail=1
    return
  fi
  rm -f "$stderr"

  local want got
  want="$(normalise "$expected")"
  got="$(normalise "$actual")"

  if [ "$want" = "$got" ]; then
    printf '  %-24s %s\n' "$container" "$got"
    return
  fi

  printf '  %-24s %s\n' "$container" "$got"
  if [ "$got" = "(unconfined)" ]; then
    echo "  ERROR: $container is not pinned at all; it can run on any CPU, including the" >&2
    echo "         capacity units'. Raise it with 'make itc-rehearse' / 'make obs-rehearse'," >&2
    echo "         not 'make itc-up' / 'make obs-up'." >&2
  else
    echo "  ERROR: $container is pinned to $got but the partition assigns it $want." >&2
  fi
  fail=1
}

echo "rehearsal cpusets for G${ITC_GROUPS} (expecting generator/monitor on ${ITC_CPUS_GENERATOR})"

for n in $(seq 1 "$ITC_GROUPS"); do
  case $n in
    1) set_cpus="$ITC_CPUS_A" ;;
    2) set_cpus="$ITC_CPUS_B" ;;
    3) set_cpus="$ITC_CPUS_C" ;;
    4) set_cpus="$ITC_CPUS_D" ;;
  esac
  check "alloca-service-$n"     "$set_cpus" required
  check "alloca-authority-$n-db" "$set_cpus" required
done

# The monitoring side shares the generator's set, and moves with it: widening
# ITC_CPUS_GENERATOR for the headroom control has to widen monitoring too, or the control
# changes the generator's share while monitoring keeps its old one.
check alloca-prometheus    "$ITC_CPUS_GENERATOR" optional
check alloca-grafana       "$ITC_CPUS_GENERATOR" optional
# node_exporter reads the *host's* namespaces, so what it reports does not depend on where it is
# pinned — but what it costs does. It scrapes six collectors every 5s, and an unpinned exporter
# spends that on whichever CPU the scheduler picks, which on this partition means a capacity
# unit's (ag-sept-pr4.md §2.14).
check alloca-node-exporter "$ITC_CPUS_GENERATOR" optional

exit $fail
