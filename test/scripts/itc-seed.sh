#!/usr/bin/env bash
#
# Seed the WL-MUT-DISP-4 fixture across an Iteration C topology.
#
# The workload is defined over four organisations whose per-organisation population is fixed
# for a comparison (workload-catalog.md, "WL-MUT-DISP-4 — Population"). What changes between
# G1, G2 and G4 is only which authority each organisation is homed on, so this reads the same
# placement document `make itc-up ITC_GROUPS=n` mounted and seeds each organisation into its own
# home. Deriving the mapping from the document rather than restating it here is what keeps the
# fixture and the routing from disagreeing — a disagreement that produces empty datasets on one
# authority and double-sized ones on another, both of which still serve traffic.
#
# **-reset runs once per authority, never once per organisation.** `Truncate` includes `slots`,
# so at G1 — where all four organisations share one database — resetting before each would
# leave only the last organisation's fixture standing, and the run would report three empty
# datasets as a scale result. The first organisation on each authority carries the reset; the
# rest are seeded on top of it. That is also why the loop is ordered by authority rather than by
# organisation.
#
# SLOTS is per organisation and must be identical at every capacity point being compared. It is
# sized once for the largest intended G4 run and reused unchanged; topology-specific resizing
# changes the workload and invalidates the comparison.
#
# **The default is interim, not the PR4b size** (ag-sept-pr4.md §3.10). 200 was exhausted about
# five seconds into the first measured G4 cell, which then spent the rest of its window measuring
# refusal throughput; 3200 clears the demand that cell actually produced. The value PR4b quotes
# against must be derived from the deepest rung its ladder reaches, because a fixture that runs
# out at a higher rung invalidates the point below it as well.
#
#   ITC_GROUPS=4 SLOTS=3200 ./test/scripts/itc-seed.sh
#
# Run it after `make itc-up ITC_GROUPS=n` and before the sweep. Locally that is the host, where
# the container ports are published. On EC2 each authority is a different machine, so point the
# script at them:
#
#   AUTHORITY_1_HOST=10.0.1.11 AUTHORITY_1_PGPORT=5432 \
#   AUTHORITY_2_HOST=10.0.1.12 AUTHORITY_2_PGPORT=5432 \
#   ... ITC_GROUPS=4 SLOTS=3200 ./test/scripts/itc-seed.sh

set -euo pipefail

ITC_GROUPS="${ITC_GROUPS:-4}"
SLOTS="${SLOTS:-3200}"
CAPACITY="${CAPACITY:-20}"

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$ITC_GROUPS'" >&2; exit 1 ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
placement="$repo_root/deploy/topology/placement-itc-g${ITC_GROUPS}.json"
[ -f "$placement" ] || { echo "no placement document at $placement" >&2; exit 1; }

# Where each authority's PostgreSQL answers.
#
# Host as well as port, because the three environments this has to serve disagree about both.
# Locally the authorities are containers publishing distinct ports on one host; inside the
# Compose network they are distinct hostnames all on 5432; on EC2 they are four separate hosts.
# Defaulting to localhost keeps the local recipe a single command while leaving the other two
# expressible without editing the script — which matters because editing it per environment is
# how the fixture and the routing start to disagree.
pgendpoint_for() {
  case "$1" in
    authority-1) echo "${AUTHORITY_1_HOST:-localhost}:${AUTHORITY_1_PGPORT:-15433}" ;;
    authority-2) echo "${AUTHORITY_2_HOST:-localhost}:${AUTHORITY_2_PGPORT:-15434}" ;;
    authority-3) echo "${AUTHORITY_3_HOST:-localhost}:${AUTHORITY_3_PGPORT:-15435}" ;;
    authority-4) echo "${AUTHORITY_4_HOST:-localhost}:${AUTHORITY_4_PGPORT:-15436}" ;;
    *) echo "unknown authority '$1'; deploy/topology defines authority-1..4 only" >&2
       return 1 ;;
  esac
}

# Emit "authority org" lines, authority-major and sorted, so the reset-once rule below can key
# on a change of authority.
mapping="$(python3 -c '
import json, sys
homes = json.load(open(sys.argv[1]))["homes"]
for org, authority in sorted(homes.items(), key=lambda kv: (kv[1], kv[0])):
    print(authority, org)
' "$placement")"

echo "seeding G${ITC_GROUPS} from $(basename "$placement"): ${SLOTS} slots per organisation, capacity ${CAPACITY}"

current_authority=""
while read -r authority org; do
  [ -n "$authority" ] || continue
  endpoint="$(pgendpoint_for "$authority")"
  dsn="postgres://${PGUSER:-alloca}:${PGPASSWORD:-alloca}@${endpoint}/${PGDATABASE:-alloca}?sslmode=${PGSSLMODE:-disable}"

  reset=""
  if [ "$authority" != "$current_authority" ]; then
    # First organisation on this authority: clear whatever the last run left, once.
    reset="-reset"
    current_authority="$authority"
  fi

  echo "  ${org} -> ${authority} (${endpoint})${reset:+ [reset]}"
  ( cd "$repo_root" && go run ./cmd/alloca-seed \
      -database-url "$dsn" -org "$org" -slots "$SLOTS" -capacity "$CAPACITY" $reset )
done <<< "$mapping"

echo "G${ITC_GROUPS} fixture seeded: 4 organisations x ${SLOTS} slots across ${ITC_GROUPS} authority/authorities"
