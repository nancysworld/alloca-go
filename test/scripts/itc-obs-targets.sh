#!/usr/bin/env bash
#
# Write the Prometheus file_sd target list for exactly the capacity units a rung raises.
#
# **file_sd stays the abstraction, deliberately.** The scrape *configuration* is version-controlled
# and says nothing about where the units are; only this generated file does. That is what lets the
# same prometheus.yml serve three different deployments without editing: locally the targets are
# Compose service names on the topology's network, and on EC2 the same file holds the units'
# private addresses. Hard-coding container names into prometheus.yml would work exactly once, on
# this machine.
#
# **Local targets are `service-N:9090`, not published host ports.** Prometheus joins
# `alloca-topology_default` (docker-compose.rehearsal.yml), so it resolves the units by their
# Compose service names and scrapes the container port directly. The published 908N ports are for
# an operator with curl; routing the scrape through them would reintroduce the discovered-host-
# address problem that had already left a stale `172.21.44.28` in this directory.
#
# **Every target carries `authority`, and that label is the stable per-unit identity.** The
# address is not: it is `service-1:9090` here and a private IP on EC2, so a panel or a report
# keyed on `instance` would not survive the move to the environment the whole experiment is for.
# ag-sept-pr4.md §2.6 wants both the per-authority series and the aggregate, and this label is
# what makes the per-authority half addressable.
#
# The units join the existing `alloca-go` job rather than a job of their own: §2.6 scopes the
# service panels to `job="alloca-go"` and reserves a second job for the host exporter, so a unit
# placed elsewhere would be missing from every panel that names the service.
#
#   ITC_GROUPS=4 ./test/scripts/itc-obs-targets.sh
#
# On EC2, give each unit its address and the same file is produced with no other change:
#
#   ITC_GROUPS=4 UNIT_1_ADDR=10.0.1.11:9090 UNIT_2_ADDR=10.0.1.12:9090 \
#     UNIT_3_ADDR=10.0.1.13:9090 UNIT_4_ADDR=10.0.1.14:9090 ./test/scripts/itc-obs-targets.sh

set -euo pipefail

cd "$(dirname "$0")/../.." || exit 1

ITC_GROUPS="${ITC_GROUPS:-4}"

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$ITC_GROUPS'" >&2
     exit 1 ;;
esac

TARGETS_DIR="${TARGETS_DIR:-deploy/observability/targets}"
OUT="${OUT:-$TARGETS_DIR/itc.json}"

mkdir -p "$TARGETS_DIR"

# `topology` is stamped here, on the targets, rather than by a job-wide relabel in
# prometheus.yml. It was job-wide once, fixed at `single-instance-local`, and every Iteration C
# sample inherited the PR2 experiment's identity — a static rule cannot know which topology the
# discovered targets belong to, and nothing reported the mismatch because the label was present
# and well-formed. It is a property of *which* targets these are, so it travels with them, and
# the rung is in the value: a G2 snapshot cannot be mistaken for a G4 one.
entries=""
for n in $(seq 1 "$ITC_GROUPS"); do
  var="UNIT_${n}_ADDR"
  addr="${!var:-service-${n}:9090}"
  [ -n "$entries" ] && entries="${entries},"
  entries="${entries}
  {
    \"targets\": [\"${addr}\"],
    \"labels\": {
      \"authority\": \"authority-${n}\",
      \"unit\": \"${n}\",
      \"topology\": \"itc-g${ITC_GROUPS}\"
    }
  }"
done

printf '[%s\n]\n' "$entries" > "$OUT"

echo "itc-obs-targets: wrote $ITC_GROUPS target(s) to $OUT"
for n in $(seq 1 "$ITC_GROUPS"); do
  var="UNIT_${n}_ADDR"
  printf '  authority-%s  %s\n' "$n" "${!var:-service-${n}:9090}"
done

# The host-run target from the PR2 path is a fifth member of the same job, and it is not reachable
# during a rehearsal — the service under test is in containers. Left in place it is a permanently
# down target: harmless to the aggregate, but it defeats "exactly N targets configured", and a
# host panel that must be non-empty *for every declared host* (§2.5) cannot tell a unit that was
# never scraped from one that is simply absent.
#
# Reported rather than deleted, because obs-target.sh owns that file and this script does not.
if [ -f "$TARGETS_DIR/alloca-go.json" ]; then
  echo >&2
  echo "itc-obs-targets: $TARGETS_DIR/alloca-go.json is also present." >&2
  echo "  That is the PR2 host-run target and nothing serves it during a rehearsal, so the job" >&2
  echo "  would carry a permanently down target. Remove it for the rehearsal and re-run" >&2
  echo "  test/scripts/obs-target.sh when you next measure a host-run service:" >&2
  echo >&2
  echo "      rm $TARGETS_DIR/alloca-go.json" >&2
  exit 1
fi
