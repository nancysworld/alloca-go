#!/usr/bin/env bash
#
# The one host-executable entry point for the local Iteration C experiment.
#
# **Why a single entry point.** Everything below this line is a focused worker — itc-series.sh
# owns the environment, itc-run.sh owns one cell, itc-pool-sensitivity.sh owns one comparison —
# and each of them needs host resources the sandbox does not have. Rather than granting each
# script its own execution rule, one boundary is granted here and its children inherit it. New
# stages are added inside this script rather than as new top-level scripts, so the permitted
# surface stays exactly one line (maintainer decision, 2026-08-18).
#
# **It is an execution boundary, not a design change.** Nothing here alters the experiment the
# validation plan defines; the stages compose the same workers a person would run by hand, in the
# order §4.6 requires them.
#
#   ./test/scripts/itc-local-experiment.sh qualify-conditioning
#   ./test/scripts/itc-local-experiment.sh pool
#
# The dependency between the stages is not a preference — each consumes the previous one's answer:
#
#   qualify conditioning -> freeze pool -> find the worker saturation bracket -> size the fixture
#
# Running them out of order produces numbers that describe a method that had not yet been
# qualified, which is the failure the whole sequence exists to avoid.
set -euo pipefail

cd "$(dirname "$0")/../.."

log()  { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage: ./test/scripts/itc-local-experiment.sh <stage>

  qualify-conditioning  drive one conditioned G1 diagnostic cell with the executed-plan and
                        planner-state probes both on, and establish that the measured interval
                        no longer executes the Seq Scan cached against empty mutation tables
                        (ag-sept-validation-plan.md §4.6.2)

  pool                  the bounded G1 pool-sensitivity preflight over the conditioned path,
                        at one worker level, so a single pool policy can be frozen (§4.6.3)

  recon                 adaptive workers_per_group reconnaissance (§4.6.4)      [not yet built]
  fixture               fixture sizing for a 600 s retained bracket (§4.6.6)    [not yet built]
  capacity              the retained S/H + confirmations, PR4b (§4.6.5)         [not yet built]

Knobs travel through the environment to the workers unchanged, e.g.
  ITC_WORKERS_PER_GROUP=16 CONDITIONING_TARGET=4000 ./test/scripts/itc-local-experiment.sh pool
EOF
  exit 2
}

[ $# -ge 1 ] || usage
stage="$1"; shift || true

# --- shared conditioning parameters -----------------------------------------------------------
#
# Declared once, here, so the qualification stage and the pool stage condition to the *same*
# logical state. If they differed, the pool comparison would run against a database the
# qualification never examined, and the qualification's conclusion would not transfer to it.
#
# The target is a state, not a duration (§4.6.2). Its value is provisional and is exactly what
# the qualification stage exists to test: enough committed mutations that the tables the workload
# grows are no longer empty when the recycled pool opens its connections against them.
CONDITIONING_SLOTS="${CONDITIONING_SLOTS:-200}"
CONDITIONING_TARGET="${CONDITIONING_TARGET:-4000}"
SLOTS="${SLOTS:-3200}"
CAPACITY="${CAPACITY:-20}"
WINDOW="${WINDOW:-60s}"
ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"
export CONDITIONING_SLOTS CONDITIONING_TARGET SLOTS CAPACITY WINDOW ITC_CPUS_GENERATOR

case "$stage" in

  qualify-conditioning)
    # **Both probes, because neither answers the question alone** (§3.20). auto_explain records
    # what the pooled connections executed; the plan probe records what a fresh planner would
    # have chosen at the same moment. The original diagnosis was only possible as the *gap*
    # between them — the planner had corrected itself within five seconds while the pooled
    # connections went on executing the stale plan for another forty-six.
    export PG_AUTO_EXPLAIN=1 PLAN_PROBE=1
    # A higher sample rate than a capacity cell would carry. This run is a diagnostic whose whole
    # output is the plans, and the default 0.1% can retain none at all inside a short window —
    # which reads as "clean" when it means "not observed".
    export PG_AUTO_EXPLAIN_SAMPLE="${PG_AUTO_EXPLAIN_SAMPLE:-0.05}"
    export ITC_WORKERS_PER_GROUP="${ITC_WORKERS_PER_GROUP:-16}"
    export RESULTS_GROUP="${RESULTS_GROUP:-pr4a-conditioning}"
    export REQUIRE="${REQUIRE:-local}"

    log "qualifying conditioning at G1: one conditioned cell, executed-plan and planner-state"
    log "probes both on, workers/group=$ITC_WORKERS_PER_GROUP window=$WINDOW"
    log "  this is a diagnostic: the probes cost measurable overhead and it backs no capacity number"

    # itc-series.sh owns the environment lifecycle, and it is the only place the auto_explain
    # preload can be applied: shared_preload_libraries is settable only at server start, so the
    # topology has to be raised with it rather than have it added afterwards. One cell.
    ./test/scripts/itc-series.sh 1 1 \
      || fail "the conditioned diagnostic series did not complete"

    series="$(ls -1dt test/results/$RESULTS_GROUP/*/ 2>/dev/null | head -1)"
    [ -n "$series" ] || fail "no series directory under test/results/$RESULTS_GROUP/"
    cell="$(ls -1dt "$series"cell-*/ 2>/dev/null | head -1)"
    [ -n "$cell" ] || cell="$series"

    logs="$(ls -1 "$series"postgres/*.log 2>/dev/null || true)"
    [ -n "$logs" ] || fail "the series retained no PostgreSQL logs, so what the pooled connections
  executed cannot be read. Expected them under ${series}postgres/"

    log "classifying executed plans by phase -> ${cell}plan-evidence.txt"
    # shellcheck disable=SC2086
    if ./test/scripts/itc-plan-evidence.py "$cell" $logs | tee "${cell}plan-evidence.txt"; then
      log "conditioning qualified: the measured interval executed no sequential scan of a"
      log "mutation table. Freeze the pool policy next: ./test/scripts/itc-local-experiment.sh pool"
    else
      fail "conditioning is NOT qualified — see ${cell}plan-evidence.txt. Do not proceed to the
  pool comparison or the reconnaissance: every number after this point would be measured against
  a starting state the procedure was supposed to have removed."
    fi
    ;;

  pool)
    # Deliberately over the *conditioned* path, at one worker level. The comparison is only
    # meaningful if both arms begin from the same state, and that state is the one the
    # qualification stage examined.
    export ITC_WORKERS_PER_GROUP="${ITC_WORKERS_PER_GROUP:-16}"
    export ITC_POOL_ARMS="${ITC_POOL_ARMS:-4 8}"
    export RESULTS_GROUP="${RESULTS_GROUP:-pr4a-pool}"

    log "pool sensitivity at G1 over the conditioned path: arms [$ITC_POOL_ARMS] at"
    log "workers/group=$ITC_WORKERS_PER_GROUP"
    ./test/scripts/itc-pool-sensitivity.sh
    ;;

  recon|fixture|capacity)
    # Named and refused rather than absent. A stage that silently did nothing would look like a
    # stage that found nothing, and the dependency order is the point: each of these consumes the
    # answer the previous one produced.
    fail "the '$stage' stage is not built yet. The order is qualify-conditioning -> pool ->
  recon -> fixture -> capacity, and each consumes the previous answer (§4.6)."
    ;;

  *)
    usage
    ;;
esac
