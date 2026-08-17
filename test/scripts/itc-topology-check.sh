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
# **Scoped to this Compose project, not to the `alloca-` name prefix.** The prefix matched every
# container the project has ever named — `alloca-prometheus` and `alloca-grafana` from the
# observability stack, and `alloca-pg` from `make db-up` — so once Iteration C started raising
# monitoring alongside the topology (which it must), this check refused a correct G4 and told the
# operator to tear the topology down. The equality property is about the *topology*: exactly the
# units the selected rung raises, and no leftovers from a larger one. Companions belong to the
# separate check below.
#
# A failed `docker ps` must not be swallowed. Treating it as "nothing is running" would report
# every expected unit as missing and send the reader to the topology when the fault is the daemon
# — the same misdirection record-deployment.sh gives when the socket is unreadable and it says
# the container does not exist.
if ! running="$(docker ps --filter 'label=com.docker.compose.project=alloca-topology' \
                          --format '{{.Names}}')"; then
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

# Scoping the check above to the topology's project removed something worth keeping: it used to
# catch *anything* named `alloca-` sharing the machine. That was the wrong mechanism — it refused
# the monitoring the rehearsal now requires — but the concern was real, because a container the
# rehearsal did not raise still consumes the envelope the topology is measured in.
#
# So companions are enumerated rather than ignored. Prometheus, Grafana and node_exporter are
# expected: they are the monitoring half of the generator/monitor set, and whether they are
# *pinned* there is itc-cpuset-check.sh's question, not this one. Anything else — `alloca-pg` from
# `make db-up` is the likely one — is a container nobody accounted for, running unpinned on the
# capacity units' CPUs.
#
# node_exporter is a companion the rehearsal **requires**, not merely tolerates. It is VAL-NEG-7's
# host sensor (ag-sept-pr4.md §2.1), and a cell driven without it retains no host evidence — which
# itc-run.sh refuses outright (§2.5). Refusing it here would have made the two gates contradict
# each other: one demanding the exporter be running, the other demanding it be stopped.
if ! others="$(docker ps --filter 'name=alloca-' --format '{{.Names}}' \
               | grep -vxF "$(printf '%s\n' "$running")" || true)"; then
  others=""
fi
# The postgres exporters are companions too (ag-sept-pr4.md §3.16), but unlike the three above they
# are **per authority**, so the tolerated set is derived from ITC_GROUPS rather than fixed. An
# exporter numbered within the rung is monitoring; one numbered above it is a leftover from a
# larger rung, querying a database that is no longer running — the same failure the unit equality
# check exists for, one project across.
#
# **Tolerated, not required.** `make itc-up` runs this check before `obs-rehearse` has raised
# anything, which is the documented order, so demanding the exporters here would refuse the very
# sequence the repository prescribes. That the *run* needs them is itc-series.sh's settle-wait and
# itc-run.sh's business, not this script's — the same division that keeps Prometheus optional here
# while a cell that retained no host samples is refused there.
companions='alloca-prometheus|alloca-grafana|alloca-node-exporter'
for n in $(seq 1 "$ITC_GROUPS"); do
  companions="${companions}|alloca-postgres-exporter-${n}"
done

unexpected="$(printf '%s\n' "$others" \
  | grep -vxE "$companions" | grep . || true)"
if [ -n "$unexpected" ]; then
  echo "itc-topology-check: containers are running that the rehearsal did not raise:" >&2
  printf '%s\n' "$unexpected" | sed 's/^/    /' >&2
  echo "  Each consumes the envelope this topology is measured in, and none of them is pinned" >&2
  echo "  by the rehearsal partition. Stop them, or the numbers carry contention no artifact" >&2
  echo "  records. ('make db-down' stops alloca-pg; 'make obs-down' stops a postgres exporter" >&2
  echo "  left behind by a larger rung.)" >&2
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
