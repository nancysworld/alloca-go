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
# It also enforces the three properties the partition exists for, none of which Docker can check
# because Docker sees each container's set and never the arrangement:
#
#   * no two sets overlap, or the groups are not independent;
#   * the capacity units this rung raises are the same size, or E2/E4 carry the imbalance;
#   * the generator is strictly larger than a capacity unit (§2.12), or it saturates first.
#
# Every violation is reported, not just the first: an operator rescaling a partition to a new
# machine should see the whole list rather than rediscover it one run at a time.
#
#   ./test/scripts/itc-cpu-layout.sh          # check and print
#   ITC_GROUPS=2 ./test/scripts/itc-cpu-layout.sh
#
# The generator-headroom control (ag-sept-pr4.md §2.14) is this script plus itc-run.sh with a
# wider generator set — the same cell rerun at ITC_CPUS_GENERATOR=8-15 to show whether the
# apparent frontier moves when the generator stops being the scarce thing.
#
# Exits non-zero with an explanation when the partition does not fit, overlaps, or violates a
# size rule.

set -euo pipefail

ITC_GROUPS="${ITC_GROUPS:-4}"

# Validated before any arithmetic, for two reasons. `(( ITC_GROUPS >= 2 ))` under `set -u` reports
# a non-numeric value as "unbound variable", naming neither the variable nor the fix. And an
# unrecognised *numeric* count is worse than unclear: G3 would silently check a two-unit partition
# while printing "G3", so the operator would read a pass for a layout that was never examined.
# itc-topology-check.sh and itc-seed.sh admit the same three rungs; all three must agree.
case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept/milestone-validation.md §4.6); got '$ITC_GROUPS'" >&2
     exit 1 ;;
esac

# The default partition is ag-sept-pr4.md §2.14's. Every set is overridable so the layout can be
# rescaled to a machine without editing a committed file — the shape of the rehearsal is a
# decision, but which CPUs it lands on is not.
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
declare -A unit_size=()
generator_size=0
highest=-1
fail=0

echo "rehearsal CPU partition for G${ITC_GROUPS} (machine has ${available} CPUs: 0-$((available - 1)))"
for entry in "${sets[@]}"; do
  name="${entry%%:*}"; spec="${entry##*:}"
  # Command substitution, not `mapfile < <(expand ...)`: a process substitution's exit status is
  # not the enclosing command's, so an unparseable spec used to print its error, yield an empty
  # set, and let the script exit 0 — reporting a partition it had rejected as valid.
  if ! expanded="$(expand "$spec")"; then
    echo "  ERROR: '$name' was given '$spec', which does not name a set of CPUs." >&2
    fail=1
    continue
  fi
  mapfile -t cpus <<< "$expanded"
  printf '  %-18s %-8s (%d CPU%s)\n' "$name" "$spec" "${#cpus[@]}" "$([ "${#cpus[@]}" -eq 1 ] || echo s)"
  if [[ "$name" == "generator/monitor" ]]; then
    generator_size=${#cpus[@]}
  else
    unit_size["$name"]=${#cpus[@]}
  fi
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

# Both size rules are stated relative to a capacity unit, so both need unit A to have expanded.
# When it did not, the run has already failed on that spec; comparing against the missing value
# would replace that specific, actionable error with bash's "unbound variable".
if [[ -n "${unit_size["capacity unit A"]:-}" ]]; then
  unit_cpus=${unit_size["capacity unit A"]}

# Capacity units must be like-for-like, and only the ones this rung raises are compared: refusing
# a G1 layout because unit C is sized differently would reject a partition G1 never touches.
#
# An uneven unit is not a smaller measurement, it is an uninterpretable one. E2 and E4 are ratios
# taken across units assumed identical, so a unit with an extra CPU raises the aggregate and the
# efficiency figure silently carries the imbalance instead of the architecture — which is the one
# thing those numbers exist to isolate.
  for name in "${!unit_size[@]}"; do
    if (( unit_size["$name"] != unit_cpus )); then
      echo "  ERROR: '$name' has ${unit_size[$name]} CPUs but 'capacity unit A' has ${unit_cpus};" >&2
      echo "         capacity units must be like-for-like (ag-sept-pr4.md §2.14), or E2/E4 carry" >&2
      echo "         the imbalance rather than the architecture." >&2
      fail=1
    fi
  done

# §2.12's rule, enforced rather than only described. The generator is the one host whose
# saturation would invalidate a point rather than describe one, so it must be strictly larger
# than the unit it drives — a generator sized like a capacity unit reaches its own limit first
# and the ladder then measures the harness. The error text below already told operators this;
# nothing checked it, so `ITC_CPUS_GENERATOR=8` passed.
  if (( generator_size <= unit_cpus )); then
    echo "  ERROR: the generator/monitor set has ${generator_size} CPU(s) and a capacity unit" >&2
    echo "         has ${unit_cpus}; the generator must be strictly larger (ag-sept-pr4.md" >&2
    echo "         §2.12), or a generator that saturates first measures itself." >&2
    fail=1
  fi
fi

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
