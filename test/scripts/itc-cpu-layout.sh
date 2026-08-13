#!/usr/bin/env bash
#
# Check the rehearsal's CPU partition against the machine, and print it.
#
# The partition in ag-sept-pr4.md §2.14 assigns four two-CPU capacity units and a larger
# generator/monitoring set. Whether those CPUs exist is a property of the machine, and on WSL it
# is a property of `.wslconfig` rather than of the hardware — so a partition that does not fit is
# a configuration mismatch with a specific fix, not a hardware limit.
#
# Docker already refuses an out-of-range cpuset ("Requested CPUs are not available"), so this is
# not what stops a bad partition from running. What it adds is the *reason*: it names the file to
# change, and it fails before an image build and four database containers have been started.
#
# It also enforces the property the partition exists for — that no two sets overlap — which
# Docker cannot check, because Docker sees each container's set and never the arrangement.
#
#   ./test/scripts/itc-cpu-layout.sh          # check and print
#   ITC_GROUPS=2 ./test/scripts/itc-cpu-layout.sh
#
# Exits non-zero with an explanation when the partition does not fit or overlaps.

set -euo pipefail

ITC_GROUPS="${ITC_GROUPS:-4}"

# The default partition is ag-sept-pr4.md §2.14's. Every set is overridable so the layout can be
# rescaled to a
# machine without editing a committed file — the shape of the rehearsal is a decision, but which
# CPUs it lands on is not.
ITC_CPUS_A="${ITC_CPUS_A:-0-1}"
ITC_CPUS_B="${ITC_CPUS_B:-2-3}"
ITC_CPUS_C="${ITC_CPUS_C:-4-5}"
ITC_CPUS_D="${ITC_CPUS_D:-6-7}"
ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"

available="$(nproc)"

# expand "0-1,4" into the list of CPU ids it names.
expand() {
  local spec="$1" out=() part lo hi
  IFS=',' read -ra parts <<< "$spec"
  for part in "${parts[@]}"; do
    if [[ "$part" == *-* ]]; then
      lo="${part%%-*}"; hi="${part##*-}"
      if ! [[ "$lo" =~ ^[0-9]+$ && "$hi" =~ ^[0-9]+$ ]] || (( lo > hi )); then
        echo "itc-cpu-layout: '$spec' is not a CPU range" >&2; return 1
      fi
      for ((i = lo; i <= hi; i++)); do out+=("$i"); done
    else
      if ! [[ "$part" =~ ^[0-9]+$ ]]; then
        echo "itc-cpu-layout: '$spec' is not a CPU range" >&2; return 1
      fi
      out+=("$part")
    fi
  done
  printf '%s\n' "${out[@]}"
}

# Only the sets the requested group count actually raises are checked. A G1 rehearsal must not be
# refused because CPUs for units C and D do not exist — it never starts them.
sets=("capacity unit A:$ITC_CPUS_A")
(( ITC_GROUPS >= 2 )) && sets+=("capacity unit B:$ITC_CPUS_B")
(( ITC_GROUPS >= 4 )) && sets+=("capacity unit C:$ITC_CPUS_C" "capacity unit D:$ITC_CPUS_D")
sets+=("generator/monitor:$ITC_CPUS_GENERATOR")

declare -A owner=()
highest=-1
fail=0

echo "rehearsal CPU partition for G${ITC_GROUPS} (machine has ${available} CPUs: 0-$((available - 1)))"
for entry in "${sets[@]}"; do
  name="${entry%%:*}"; spec="${entry##*:}"
  mapfile -t cpus < <(expand "$spec")
  printf '  %-18s %-8s (%d CPU%s)\n' "$name" "$spec" "${#cpus[@]}" "$([ "${#cpus[@]}" -eq 1 ] || echo s)"
  for cpu in "${cpus[@]}"; do
    (( cpu > highest )) && highest=$cpu
    if [[ -n "${owner[$cpu]:-}" ]]; then
      echo "  ERROR: CPU $cpu is in both '${owner[$cpu]}' and '$name'; the sets must not overlap," >&2
      echo "         or the groups are not independent and the rehearsal measures nothing." >&2
      fail=1
    fi
    owner[$cpu]="$name"
  done
done

if (( highest >= available )); then
  echo >&2
  echo "itc-cpu-layout: the partition needs CPU $highest but this machine has ${available} (0-$((available - 1)))." >&2
  echo >&2
  echo "  On WSL the count is set by .wslconfig on the Windows side, not by the hardware:" >&2
  echo >&2
  echo "      [wsl2]" >&2
  echo "      processors=$((highest + 1))" >&2
  echo >&2
  echo "  Then 'wsl --shutdown' from Windows and reopen the shell. Note this changes the" >&2
  echo "  workstation envelope every earlier local measurement was taken in." >&2
  echo >&2
  echo "  To rescale the partition to this machine instead, set ITC_CPUS_A..D and" >&2
  echo "  ITC_CPUS_GENERATOR. Keep the generator larger than one capacity unit" >&2
  echo "  (ag-sept-pr4.md §2.12): a generator that saturates first measures itself." >&2
  fail=1
fi

exit $fail
