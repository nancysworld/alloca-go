#!/usr/bin/env bash
#
# The bounded G1 pool-sensitivity preflight (ag-sept/milestone-validation.md §4.6.3).
#
# **What it is for.** Before G1/G2/G4 are compared, one thing has to be ruled out: that the
# connection ceiling, rather than the shard group, is what the comparison measures. If the pool is
# an obviously removable admission bottleneck at the operating point, then every efficiency figure
# derived later carries it. So the ceiling is varied deliberately, once, at G1.
#
# **What it is not.** Not a pool tuning matrix, and not an optimisation. §4.6.3 asks for a
# reasonable fixed policy, not the best one — the purpose is to choose a value that is defensibly
# not a ceiling, freeze it, and keep it identical across G1, G2 and G4 for the whole comparison.
# Extending to 16 is conditional on 8 leaving the answer genuinely ambiguous, and that is a
# maintainer decision rather than something this script should sweep into.
#
# **Same worker pressure on both arms.** The arms differ in one variable. Driving arm two harder
# because it can take more would answer a different question — whether more connections plus more
# demand goes faster, which nobody doubts.
#
#   ITC_POOL_ARMS="4 8" ./test/scripts/itc-local-experiment.sh pool
#
# **What each arm resets, precisely.** The service unit is recreated with the new pool
# configuration — Compose replaces a container whose environment changed — so the arm's
# connections are new and the ceiling is the one the arm names. The logical fixture is then reset,
# reseeded and reconditioned by the cell itself, so both arms begin from the same declared
# database *state*.
#
# What is *not* torn down is PostgreSQL. The server process, its data directory, its shared
# buffers and the host page cache survive between arms, because nothing in the arm changes the
# database container's configuration and Compose therefore leaves it running. So the two arms are
# comparable in pool ceiling and logical state, and share whatever the previous arm left in
# physical storage and cache. That is a limitation to state when interpreting a small difference,
# not a defect: fully recreating the database between arms would replace it with a different
# incomparability — one arm reading a cold cache and the other a warm one.
set -euo pipefail

cd "$(dirname "$0")/../.."

ITC_POOL_ARMS="${ITC_POOL_ARMS:-4 8}"
# 16 workers per group by maintainer decision (2026-08-18): the arms have to be compared at a
# worker level that actually presses on the connection ceiling, or a null result would say only
# that neither pool was reached.
WORKERS="${ITC_WORKERS_PER_GROUP:-16}"
WINDOW="${WINDOW:-60s}"
SLOTS="${SLOTS:-3200}"
CAPACITY="${CAPACITY:-20}"
CONDITIONING_SLOTS="${CONDITIONING_SLOTS:-200}"
CONDITIONING_TARGET="${CONDITIONING_TARGET:-2000}"
RESULTS_GROUP="${RESULTS_GROUP:-pr4a-pool}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

log()  { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

# G1 alone. The question is whether the ceiling binds on one shard group's own authority, and
# adding groups would vary the thing the answer is supposed to be independent of.
groups=1

log "pool sensitivity at G$groups: arms [$ITC_POOL_ARMS], workers/group=$WORKERS, window=$WINDOW"
log "  this is configuration qualification, not a tuning matrix: the output is a policy to freeze"

for arm in $ITC_POOL_ARMS; do
  out="test/results/$RESULTS_GROUP/pool-$arm-$STAMP"
  log "arm: pool_max_conns=$arm -> $out"

  # Re-raised per arm so Compose recreates the service unit with the new ceiling. Reconfiguring in
  # place would not work: a pool ceiling is read when the pool is opened, so a running service
  # would keep the previous arm's ceiling while every artifact recorded the new one — the failure
  # that presents as a null result. The database container is unchanged by this and keeps running.
  ALLOCA_POOL_MAX_CONNS="$arm" ITC_GROUPS="$groups" \
    make --no-print-directory itc-rehearse \
    || fail "could not raise G$groups with pool_max_conns=$arm"

  ALLOCA_POOL_MAX_CONNS="$arm" ITC_GROUPS="$groups" SLOTS="$SLOTS" CAPACITY="$CAPACITY" \
  ITC_WORKERS_PER_GROUP="$WORKERS" WINDOW="$WINDOW" \
  CONDITIONING_SLOTS="$CONDITIONING_SLOTS" CONDITIONING_TARGET="$CONDITIONING_TARGET" \
  RESULTS_GROUP="$RESULTS_GROUP" OUT="$out" \
    ./test/scripts/itc-run.sh \
    || fail "the cell for pool_max_conns=$arm did not complete; see $out"

  # Read back from /meta rather than trusted from the variable. The variable is what was asked
  # for; this is what the service opened, and §4.6.3's claim is about the latter (the manifest
  # records it for the same reason).
  observed="$(python3 -c '
import json, sys
try:
    print(json.load(open(sys.argv[1]))["manifest"].get("pool_size_per_replica", "unknown"))
except Exception:
    print("unknown")' "$out/run.json")"
  [ "$observed" = "$arm" ] \
    || fail "arm asked for pool_max_conns=$arm but the unit opened $observed; the two arms would
  not differ in the variable the comparison names"

  log "  arm $arm complete: unit opened pool_max_conns=$observed"
done

# Deliberately no verdict computed here. Whether 8 differs materially from 4 — and whether that
# makes the ceiling a removable bottleneck or simply a bigger pool against the same database
# frontier — is an interpretation, and §4.6.3 leaves the policy choice with the maintainer. The
# script's job is to produce two comparable arms and prove they differed in one variable.
log "both arms retained under test/results/$RESULTS_GROUP/. Compare Goodput, pool acquire-wait"
log "and per-authority saturation before freezing a policy; then set ALLOCA_POOL_MAX_CONNS"
log "identically for G1, G2 and G4 (validation plan §4.6.3)."
