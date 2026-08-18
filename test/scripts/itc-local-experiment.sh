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

  build                 rebuild the load generator from the current tree and prove its stamp is
                        clean, so a later stage cannot discover after the run that it certifies
                        at no level

  preflight             report whether this machine can drive the experiment at all: host
                        reachability, Docker, the live topology, tree state and generator
                        provenance. Read-only — it raises, seeds and mutates nothing

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
# Mount points the agent sandbox leaves in the repository root.
#
# **What they are.** A sandboxed process sees `/dev/null` bind-mounted over each of these paths,
# and a bind mount needs the path to exist, so the harness creates a zero-byte file first. Those
# files used to be cleaned up; they are now left behind, and they land in exactly the place that
# breaks the experiment: `git status --porcelain` is non-empty, so `go build` stamps
# `vcs.modified=true`, `make image-provenance` refuses, and a long run dies at a gate that is
# working perfectly (ag-sept-pr4.md §3.6).
#
# **Why the cleanup is here and not in preflight.** Any sandboxed command recreates all ten,
# measured directly: delete them, run one unrelated command, and they are back. So a cleanup that
# runs and then hands control back is useless — the next command undoes it. It has to run inside
# the process that will reach the gate, which is this one, because nothing between here and the
# gate is sandboxed.
#
# **This is an environment workaround, not a repository concern**, and it should be deleted the
# day the harness stops leaving them. It is scoped as narrowly as that allows: named files only,
# and it refuses rather than deletes anything that is tracked, non-empty, or a directory — because
# every one of these names could one day be a real file. `.mcp.json` is the likeliest: a
# project-level MCP configuration would live at exactly this path.
SANDBOX_PLACEHOLDERS=".bash_profile .bashrc .gitconfig .gitmodules .idea .mcp.json .profile .ripgreprc .zprofile .zshrc"

clear_sandbox_placeholders() {
  local removed=0 name
  for name in $SANDBOX_PLACEHOLDERS; do
    [ -e "$name" ] || continue

    if git ls-files --error-unmatch "$name" >/dev/null 2>&1; then
      fail "$name is tracked by this repository, so it is not a sandbox mount point. Refusing to
  remove it. If the experiment cannot run with it present, that is a decision for a person."
    fi
    if [ -d "$name" ]; then
      fail "$name is a directory, so it is not a sandbox mount point — a real .idea would be one.
  Refusing to remove it."
    fi
    if [ -s "$name" ]; then
      fail "$name has content, so it is not an empty sandbox mount point. Refusing to remove it:
  a project-level .mcp.json would look exactly like this and is not the harness's."
    fi

    rm -f "$name" && removed=$((removed + 1))
  done

  if [ "$removed" -gt 0 ]; then
    log "cleared $removed empty sandbox mount point(s) from the repository root, so the"
    log "clean-tree gates below judge the repository rather than the harness"
  fi
}

# build_generator rebuilds the load generator from the current tree.
#
# **The stages that drive load call this rather than trusting whatever is in bin/.** A generator
# binary is not interchangeable with the tree it sits beside: it carries its own commit and
# `vcs.modified` stamp, and the manifest records them as the identity of the thing that produced
# the numbers. A stale binary therefore reports a revision that did not generate the load, and one
# built from an unclean tree certifies at no level — both discovered after the run, which on a
# long cell is the expensive place to discover anything (ag-sept-pr4.md §3.6).
#
# It is also the only way a build can happen where the tree is genuinely clean, since the stamp is
# a function of the tree state at build time and nothing else.
build_generator() {
  clear_sandbox_placeholders
  go build -o bin/alloca-load ./cmd/alloca-load \
    || fail "could not build the generator"
  local revision modified
  revision="$(go version -m bin/alloca-load | grep 'vcs.revision' | awk '{print $NF}')"
  modified="$(go version -m bin/alloca-load | grep -c 'vcs.modified=true' || true)"
  if [ "$modified" -ne 0 ]; then
    fail "the generator built from this tree is stamped vcs.modified=true, so the run would
  certify at no level. Commit or stash first: $revision"
  fi
  log "generator built clean at ${revision#vcs.revision=}"
}

CONDITIONING_SLOTS="${CONDITIONING_SLOTS:-200}"
CONDITIONING_TARGET="${CONDITIONING_TARGET:-4000}"
SLOTS="${SLOTS:-3200}"
CAPACITY="${CAPACITY:-20}"
WINDOW="${WINDOW:-60s}"
ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"
export CONDITIONING_SLOTS CONDITIONING_TARGET SLOTS CAPACITY WINDOW ITC_CPUS_GENERATOR

case "$stage" in

  build)
    build_generator
    ;;

  preflight)
    # **Read-only, deliberately.** Every later stage tears down and rebuilds the topology, and the
    # cost of discovering an unusable machine is that teardown. This asks the same questions those
    # stages will ask, before anything is destroyed, and changes nothing itself.
    #
    # It also answers a question about *this* process rather than the machine: whether the
    # invocation has the host's network and the host's view of the filesystem at all. Those differ
    # inside a sandbox, and a stage that discovered it mid-run would have destroyed a topology to
    # find out.
    status=0
    printf '\n--- host reachability -------------------------------------------------------\n'
    if curl -fsS -m 3 -o /dev/null "${PROM_URL:-http://localhost:9091}/-/ready" 2>/dev/null; then
      printf '  ok    %s answered: this invocation has the host network\n' "${PROM_URL:-http://localhost:9091}"
    else
      printf '  --    %s did not answer. Either the monitoring stack is down, or this\n' "${PROM_URL:-http://localhost:9091}"
      printf '        invocation cannot reach host-published ports, in which case no stage\n'
      printf '        below can drive a cell.\n'
    fi

    printf '\n--- docker and the live topology --------------------------------------------\n'
    if docker ps --format '{{.Names}}' >/dev/null 2>&1; then
      units="$(docker ps --format '{{.Names}}' | grep -c '^alloca-service-' || true)"
      dbs="$(docker ps --format '{{.Names}}' | grep -c '^alloca-authority-.*-db$' || true)"
      printf '  ok    docker answers: %s service unit(s), %s authority database(s) running\n' "$units" "$dbs"
      case "$units" in
        0) printf '        no topology is live; a stage that needs one will raise it\n' ;;
        1|2|4) printf '        that is the shape of G%s\n' "$units" ;;
        *) printf '  !!    %s units is not a G1/G2/G4 shape\n' "$units"; status=1 ;;
      esac
    else
      printf '  !!    docker is not reachable; no stage can raise or drive a topology\n'
      status=1
    fi

    printf '\n--- tree state --------------------------------------------------------------\n'
    dirty="$(git status --porcelain | wc -l)"
    placeholders=0
    for name in $SANDBOX_PLACEHOLDERS; do
      [ -e "$name" ] && [ ! -s "$name" ] && [ ! -d "$name" ] && placeholders=$((placeholders + 1))
    done
    if [ "$dirty" -eq 0 ]; then
      printf '  ok    the tree is clean, so a build stamps vcs.modified=false and can certify\n'
    elif [ "$dirty" -eq "$placeholders" ]; then
      # Named rather than counted. "10 uncommitted paths" sends a reader looking for their own
      # unfinished work; this says whose they are and that a driving stage removes them.
      printf '  --    %s empty sandbox mount point(s) and nothing else. A driving stage clears\n' "$placeholders"
      printf '        these itself; they are the harness, not the repository.\n'
    else
      printf '  !!    %s uncommitted path(s): every clean-tree gate below will refuse, and a\n' "$dirty"
      printf '        binary built from this tree stamps vcs.modified=true and certifies at no\n'
      printf '        level. First three:\n'
      git status --porcelain | head -3 | sed 's/^/          /'
      status=1
    fi

    printf '\n--- generator ---------------------------------------------------------------\n'
    if [ -x bin/alloca-load ]; then
      modified="$(go version -m bin/alloca-load 2>/dev/null | grep -c 'vcs.modified=true' || true)"
      revision="$(go version -m bin/alloca-load 2>/dev/null | grep 'vcs.revision' | awk '{print $NF}')"
      if [ "$modified" -eq 0 ]; then
        printf '  ok    bin/alloca-load is stamped clean at %s\n' "${revision:-unknown}"
      else
        printf '  !!    bin/alloca-load is stamped vcs.modified=true (%s): it cannot certify.\n' "${revision:-unknown}"
        printf '        Rebuild it from a clean tree: go build -o bin/alloca-load ./cmd/alloca-load\n'
        status=1
      fi
    else
      printf '  !!    bin/alloca-load does not exist: go build -o bin/alloca-load ./cmd/alloca-load\n'
      status=1
    fi

    printf '\n'
    if [ "$status" -eq 0 ]; then
      log "preflight clean: this machine can drive the experiment"
    else
      log "preflight found blockers above; fix them before a stage that destroys the topology"
    fi
    exit "$status"
    ;;

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

    clear_sandbox_placeholders
    build_generator
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

    clear_sandbox_placeholders
    build_generator
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
