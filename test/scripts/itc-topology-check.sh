#!/usr/bin/env bash
#
# Assert that exactly the selected Iteration C topology is running, and nothing else.
#
# Raising a smaller topology after a larger one does **not** stop the larger one's extra units.
# Compose profiles decide what `up` starts; they say nothing about what is already running, so
# `ITC_GROUPS=1` after `ITC_GROUPS=4` leaves three service + PostgreSQL pairs alive. Each keeps a
# connection pool, a page cache and an expiry loop, and all of it competes for the envelope the
# one-group point is supposed to be measured in — which is `VAL-NEG-7`'s subject and feeds
# straight into `E2`/`E4` (ag-sept-validation-plan.md §4.6).
#
# Nothing downstream catches it. `alloca-load` reads `/meta` only from the units it addresses, so
# a G1 run never looks at the leftovers and `topology_disagreement` stays empty. The run reports a
# clean single-authority topology while three extra units consume its host. Observed directly:
# after a G4 run, `ITC_GROUPS=1` left service-1 on routing version `itc-g1` and services 2-4 still
# serving `itc-g4`.
#
# So the check is a positive one — the running set must *equal* the expected set — rather than a
# search for known-bad leftovers. A check that only looked for what it expected to find would pass
# on the next way a unit survives.
#
#   ITC_GROUPS=4 ./test/scripts/itc-topology-check.sh
#
# Exits non-zero with the difference when the running topology is not the selected one.

set -euo pipefail

ITC_GROUPS="${ITC_GROUPS:-4}"

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$ITC_GROUPS'" >&2; exit 1 ;;
esac

if ! command -v docker >/dev/null 2>&1; then
  echo "itc-topology-check: docker is not available" >&2
  exit 1
fi

expected=""
for n in $(seq 1 "$ITC_GROUPS"); do
  expected="${expected}alloca-authority-${n}-db\nalloca-service-${n}\n"
done
expected="$(printf "%b" "$expected" | sort)"

# Long-running containers only: the migration containers run to completion and exit, and a
# finished migration is not a unit consuming anything.
#
# A failed `docker ps` must not be swallowed. Treating it as "nothing is running" would report
# every expected unit as missing and send the reader to the topology when the fault is the daemon
# — the same misdirection record-deployment.sh gives when the socket is unreadable and it says
# the container does not exist.
if ! running="$(docker ps --filter 'name=alloca-' --format '{{.Names}}')"; then
  echo "itc-topology-check: could not list containers; the Docker daemon is unreachable or the" >&2
  echo "  socket is not readable by this user. This is not a statement about the topology." >&2
  exit 1
fi
running="$(printf '%s' "$running" | sort)"

if [ "$running" = "$expected" ]; then
  echo "topology check: G${ITC_GROUPS} — exactly $((ITC_GROUPS * 2)) containers running, as selected"
else
  echo "itc-topology-check: the running topology is not the selected G${ITC_GROUPS}." >&2
  echo >&2
  extra="$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$running") || true)"
  missing="$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$running") || true)"
  [ -n "$extra" ] && {
    echo "  running but NOT part of G${ITC_GROUPS}:" >&2
    printf '%s\n' "$extra" | sed 's/^/    /' >&2
    echo "  These consume the resource envelope this topology is measured in. Raising a smaller" >&2
    echo "  topology does not stop a larger one's units; use 'make itc-down' then 'make itc-up'." >&2
  }
  [ -n "$missing" ] && {
    echo "  expected but NOT running:" >&2
    printf '%s\n' "$missing" | sed 's/^/    /' >&2
  }
  exit 1
fi

# Every serving unit must agree about which routing it is serving. A leftover unit that survived a
# topology change carries the *previous* placement document, and the boot line is where that shows.
version_seen=""
for n in $(seq 1 "$ITC_GROUPS"); do
  line="$(docker logs "alloca-service-${n}" 2>&1 | grep '"msg":"starting alloca-go"' | tail -1 || true)"
  if [ -z "$line" ]; then
    echo "itc-topology-check: alloca-service-${n} has not logged a start line yet" >&2
    exit 1
  fi
  version="$(printf '%s' "$line" | sed -n 's/.*"routing_version":"\([^"]*\)".*/\1/p')"
  if [ "$version" != "itc-g${ITC_GROUPS}" ]; then
    echo "itc-topology-check: alloca-service-${n} is serving routing version '${version}'," >&2
    echo "  but G${ITC_GROUPS} expects 'itc-g${ITC_GROUPS}'. This unit is running against a" >&2
    echo "  placement document from another topology." >&2
    exit 1
  fi
  version_seen="$version"
done

echo "topology check: all ${ITC_GROUPS} units serving routing version ${version_seen}"
