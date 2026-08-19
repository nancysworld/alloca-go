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

  sustained             the final PR4a qualification: one conditioned 600 s G1 run and one
                        conditioned 600 s G4 run on the ordinary measurement path, with the
                        fixture sized from the measured rate. Qualification evidence only —
                        it is not a capacity result and no E4_local follows from it

  recon                 adaptive workers_per_group reconnaissance (§4.6.4): short probes that
                        bracket saturation per topology and propose S and H. Not capacity
                        evidence, and nothing it produces may be quoted as a rate

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

# exclude_sandbox_placeholders makes their presence harmless, which deleting them cannot.
#
# **Deleting is necessary but not sufficient, and the first sustained attempt proved it.** The
# stage cleared them at 16:07:09 and `make image-provenance` refused at 16:07:18, because a
# monitoring command — an ordinary `tail -f`, sandboxed like anything else — recreated all ten at
# 16:07:16. Anything at all touching this repository during a run reopens the window, so a
# cleanup that depends on nothing happening for the next few seconds is not a fix for a
# twenty-five minute experiment.
#
# `.git/info/exclude` closes it: per-clone, never committed, never shared, and exactly the
# mechanism git provides for files in a working tree that are not the project's. The repository's
# own `.gitignore` stays untouched, so nothing about this leaks into what the project publishes.
#
# **What it costs.** A real file at one of these paths would no longer appear in `git status`.
# That is a genuine loss and the reason clear_sandbox_placeholders still refuses to delete
# anything tracked, non-empty, or a directory: a driving stage then fails loudly and names the
# file, which is how a real `.mcp.json` gets noticed once git has stopped mentioning it.
exclude_sandbox_placeholders() {
  local exclude=".git/info/exclude" name added=0
  [ -d .git ] || return 0
  [ -f "$exclude" ] || : > "$exclude"

  for name in $SANDBOX_PLACEHOLDERS; do
    # Anchored with a leading slash so it applies to the repository root only, never to a file
    # of the same name somewhere inside the tree.
    grep -qxF "/$name" "$exclude" 2>/dev/null && continue
    if [ "$added" -eq 0 ]; then
      printf '\n# Agent sandbox mount points (test/scripts/itc-local-experiment.sh). Local only:\n' >> "$exclude"
      printf '# this file is never committed. Remove these lines when the harness stops leaving\n' >> "$exclude"
      printf '# zero-byte files at these paths.\n' >> "$exclude"
    fi
    printf '/%s\n' "$name" >> "$exclude"
    added=$((added + 1))
  done

  if [ "$added" -gt 0 ]; then
    log "excluded $added sandbox mount point(s) in .git/info/exclude (local, never committed),"
    log "so a command run during a long experiment cannot dirty the tree under a gate"
  fi
}

clear_sandbox_placeholders() {
  local removed=0 name
  exclude_sandbox_placeholders
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

# **Captured before any default is applied.** A default set here is indistinguishable from an
# operator's choice by the time a stage reads it, so a stage's own default can never fire — which
# is exactly what happened: `sustained` asked for `${WINDOW:-600s}` and got the 60s set below,
# then ran a 60 s cell on a 3,200-slot fixture and reported it as a clean 600 s qualification.
# Stages read these instead, so "nothing was set" stays distinguishable from "the operator asked".
ENV_WINDOW="${WINDOW-}"
ENV_SLOTS="${SLOTS-}"

# **The frozen Iteration C pool policy: `pool_max_conns=8`** (maintainer decision, 2026-08-18).
#
# One value for every shard group at G1, G2 and G4 — the capacity unit is meant to be the same
# unit at each topology, and a per-topology pool would make the comparison one between two
# different units.
#
# Chosen from three retained 600 s G1 runs at 16 workers per group, not from a preference:
#
#   pool=4    944.4/s   acquire 9.8->13.9 ms, pool 4.00/4, PG backends 2.18, run queue 4.03
#   pool=8   1248.6/s   acquire 5.1->6.6 ms,  pool 8.00/8, PG backends 4.78, run queue 7.26
#   pool=16  1046.6/s   acquire ~0.04 ms,     pool 15.5/16, PG backends 7.42, run queue 14.33
#
# 4 was an admission ceiling: doubling it returned 32% more sustained Goodput at unchanged host
# CPU. 16 removed the admission queue *entirely* — no worker waits for a connection at all — and
# converted it into contention: 55% more active backends, 55% more wait events, double the run
# queue, p95 and p99 both 41% worse, and 16% *less* Goodput. So 8 is not an obviously removable
# ceiling; past it the binding constraint has already moved off admission and onto one
# authority's capacity to do concurrent work on two CPUs.
#
# Every run records what the service actually opened at /meta, so this variable is the request
# and `pool_size_per_replica` in the manifest is the fact.
ALLOCA_POOL_MAX_CONNS="${ALLOCA_POOL_MAX_CONNS:-8}"
export ALLOCA_POOL_MAX_CONNS

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

  sustained)
    # **The ordinary measurement path.** No plan probe, no auto_explain, no pg_stat_statements:
    # the diagnostics cost measurable overhead on the database under test, and this run has to
    # behave like the PR4b runs it is qualifying rather than like the diagnostic that preceded it.
    # They are unset explicitly rather than left to default, so an exported value from an earlier
    # shell cannot silently instrument a qualification run.
    unset PLAN_PROBE PG_AUTO_EXPLAIN PG_STAT_STATEMENTS PG_LOG_AUTOVACUUM PG_AUTO_EXPLAIN_SAMPLE

    export ITC_WORKERS_PER_GROUP="${ITC_WORKERS_PER_GROUP:-16}"
    export WINDOW="${ENV_WINDOW:-600s}"
    export CAPACITY="${CAPACITY:-20}"
    export REQUIRE="${REQUIRE:-capacity}"
    export RESULTS_GROUP="${RESULTS_GROUP:-pr4a-sustained}"

    # --- fixture sizing, derived and recorded ---------------------------------------------
    #
    # The gate this exists to satisfy: fixture exhaustion must not be able to become the
    # deciding event of a 600 s run (measurement-contract §5, useful-demand / fixture-headroom).
    # G4 is what sizes it — it consumes roughly four groups' worth — and the same per-organisation
    # size is then reused unchanged at G1, because the workload's fixture is fixed for a
    # comparison (workload-catalog.md, WL-MUT-DISP-4).
    #
    # Every input below is measured or declared, and the arithmetic is retained beside the runs
    # so the number can be checked rather than trusted.
    # The input is now a *measured G4 aggregate* at exactly this configuration rather than a G1
    # rate extrapolated by four. The extrapolation assumed perfect scaling and over-estimated by
    # more than two to one: 6,816/s predicted against 3,165/s observed. Measuring the topology
    # that sizes the fixture is strictly better evidence than reasoning about it.
    measured_aggregate="${ITC_MEASURED_G4_RATE:-3165}"       # G4, 16 workers/group, pool 4, 60 s
    sustained_allowance="${ITC_SUSTAINED_ALLOWANCE:-140}"    # % — a 600 s regime may exceed its first minute
    safety="${ITC_FIXTURE_SAFETY:-140}"                      # % explicit headroom on top
    seconds="${ITC_WINDOW_SECONDS:-600}"

    peak_aggregate=$(( measured_aggregate * sustained_allowance / 100 ))
    per_org=$(( peak_aggregate * seconds / 4 ))
    required_per_org=$(( (per_org + CONDITIONING_TARGET) * safety / 100 ))
    slots_per_org=$(( (required_per_org + CAPACITY - 1) / CAPACITY ))
    # Rounded up to a round number so the retained value is legible in an artifact.
    slots_per_org=$(( (slots_per_org / 5000 + 1) * 5000 ))
    export SLOTS="${ENV_SLOTS:-$slots_per_org}"

    sizing="test/results/$RESULTS_GROUP/fixture-sizing.txt"
    mkdir -p "test/results/$RESULTS_GROUP"
    cat > "$sizing" <<SIZING
fixture sizing for the PR4a sustained qualification
derived $(date -u +%Y-%m-%dT%H:%M:%SZ)

  measured G4 aggregate rate         $measured_aggregate /s   (G4, 16 workers/group, pool 4, 60 s,
                                                     ordinary measurement path)
  allowance for a sustained regime   ${sustained_allowance}%
  peak aggregate assumed             $peak_aggregate /s
  measured duration                  ${seconds}s
  measured consumption per org       $per_org mutations
  conditioning per org               $CONDITIONING_TARGET mutations
  explicit safety headroom           ${safety}%
  required per org                   $required_per_org mutations
  capacity per slot                  $CAPACITY
  slots per organisation             $SLOTS   (rounded up)
  seeded supply                      $(( SLOTS * CAPACITY * 4 )) mutations across 4 organisations
  measured supply after conditioning $(( (SLOTS - CONDITIONING_SLOTS) * CAPACITY * 4 )) mutations

The same per-organisation size is used at G1 and G4: the workload's fixture is fixed for a
comparison, and resizing it per topology would change the workload as well as the topology.
SIZING
    log "fixture sizing -> $sizing"
    sed 's/^/    /' "$sizing"

    clear_sandbox_placeholders
    build_generator

    # Which topologies this stage drives. Both, for the qualification itself; a single one when a
    # later comparison has to hold every other variable fixed — running the *same* code path is
    # what makes "everything else identical" true by construction rather than by inspection.
    for groups in ${ITC_SUSTAINED_GROUPS:-1 4}; do
      log "sustained qualification: G$groups, ${WINDOW}, workers/group=$ITC_WORKERS_PER_GROUP,"
      log "  pool_max_conns=$ALLOCA_POOL_MAX_CONNS, conditioned, ordinary measurement path"
      ./test/scripts/itc-series.sh "$groups" 1         || fail "the sustained G$groups run did not complete. Stop here and diagnose this run
  rather than adding controls or shorter runs around it."

      series="$(ls -1dt test/results/$RESULTS_GROUP/*/ 2>/dev/null | head -1)"
      cell="$(ls -1dt "$series"cell-*/ 2>/dev/null | head -1)"
      [ -n "$cell" ] || fail "G$groups produced no cell directory under $series"

      # **The artifact is checked against the intent, not assumed to match it.** The first
      # attempt at this stage ran 60 s cells on a 3,200-slot fixture and reported them as clean:
      # every gate passed, because each gate judged the run that happened rather than the run
      # that was asked for. Nothing downstream could have noticed.
      ran_seconds="$(python3 -c "
import json,sys
print(int(round(json.load(open(sys.argv[1]))['summary']['duration_seconds'])))" "${cell}run.json")"
      ran_slots="$(grep '^SLOTS=' "${cell}fixture.txt" | cut -d= -f2)"
      if [ "$ran_seconds" -lt $(( seconds * 95 / 100 )) ]; then
        fail "G$groups measured ${ran_seconds}s but the stage asked for ${seconds}s. The cell is
  sound and describes a different experiment; it is not this qualification."
      fi
      if [ "$ran_slots" != "$SLOTS" ]; then
        fail "G$groups ran on $ran_slots slots/organisation but the sizing derived $SLOTS. The
  fixture headroom this qualification depends on was not the one that was seeded."
      fi
      log "G$groups ran ${ran_seconds}s on $ran_slots slots/org, as intended"

      log "G$groups slices -> ${cell}slices.txt"
      ./test/scripts/itc-slices.py "$cell" | tee "${cell}slices.txt"
    done

    log "both sustained runs complete. They are qualification evidence only: no capacity result"
    log "and no E4_local follows from them, and 16 workers/group is not a selected S (§4.6.5)."
    ;;

  recon)
    # **Reconnaissance answers one question: which two worker levels deserve the expensive
    # retained measurement?** (ag-sept-validation-plan.md §4.6.4.) It is explicitly *not* capacity
    # evidence — no rate it produces enters E2/E4 or may be quoted as a result, and a level is not
    # promoted because a probe happened to look stable.
    #
    # The ordinary measurement path, for the same reason the sustained qualification uses it: a
    # probe instrumented differently from the runs it selects for would bracket a different system.
    unset PLAN_PROBE PG_AUTO_EXPLAIN PG_STAT_STATEMENTS PG_LOG_AUTOVACUUM PG_AUTO_EXPLAIN_SAMPLE

    export WINDOW="${ENV_WINDOW:-120s}"
    export REQUIRE="${REQUIRE:-capacity}"
    export RESULTS_GROUP="${RESULTS_GROUP:-pr4b-recon}"

    # **120 s, and the trade it makes is recorded rather than hidden.** A probe reads the early,
    # high part of a trajectory that PR4a measured declining to ~0.75x by 600 s
    # (ag-sept-pr4.md §3.21), so a bracket chosen here is chosen on a *different quantity* from the
    # 600 s horizon average that decides the retained comparison (§4.6.5). Long probes would close
    # the gap and cost the budget the retained runs need; the lower-side check below is what makes
    # the short probe safe enough to act on (maintainer decision, 2026-08-19).
    recon_seconds="${ITC_RECON_SECONDS:-120}"

    # **The ladder, and why the walk may not run off either end.** §4.6.4 sets no maximum, so this
    # is a starting range rather than a bound: reaching an end means the bracket is outside the
    # range prior evidence suggested, which is a fact worth a person seeing before another hour of
    # probing is spent. Re-run with an explicit ITC_RECON_LADDER to extend it.
    ladder="${ITC_RECON_LADDER:-2 4 8 12 16 24 32 48 64 96 128}"

    # 16 workers/group is where PR4a's six retained runs were taken, so it is the one level on this
    # machine whose 600 s behaviour is already known. Starting there means the first probe can be
    # read against something.
    recon_start="${ITC_RECON_START:-16}"

    # **The margin is derived, not chosen.** Ten identical G4 cells in the healthy regime agreed to
    # within 2.6% (docs/measurements/pr4a-rehearsal/repeats/), so 5% is roughly twice the
    # environment's own demonstrated reproducibility: a level must beat its neighbour by more than
    # the machine's noise before the walk treats it as better. That figure was measured on 60 s
    # cells rather than on 120 s probes, so it is a defensible transfer and not a measurement of
    # this probe shape — which is why the artifact records the margin it used.
    recon_margin="${ITC_RECON_MARGIN:-5}"

    recon_groups="${ITC_RECON_GROUPS:-1 2 4}"

    # **The prober is a seam, so the walk below can be tested without an hour of machine time.**
    # The search has real logic — a direction decision, two walks, two ladder-end refusals and the
    # lower-side check — and it runs unattended while driving real cells. A defect in it costs the
    # hour *and* can hand back the wrong bracket, which is the expensive kind of wrong.
    # `test/scripts/itc-recon-walk-test.sh` substitutes a prober that returns a synthetic curve and
    # exercises this exact code, rather than a copy of it that could drift from it.
    #
    # **It swaps where a rate comes from, so it is loud and it is fenced.** A synthetic invocation
    # drives no cell and measures nothing; it refuses to write into the real results group, says so
    # on every line, and stamps the report. Recon output is non-evidence by definition (§4.6.4), so
    # the worst this can produce is a fabricated *bracket* — and the retained 600 s runs are what
    # turn a bracket into a result.
    #
    # **The fence is here, above every write.** It was originally beside the prober call further
    # down, and the self-test caught what that meant: a synthetic invocation had already written its
    # fixture-sizing artifact into `pr4b-recon/` by the time the refusal fired. A refusal that
    # leaves a file behind in the real results group is not a fence.
    recon_synthetic="${ITC_RECON_PROBE_CMD:-}"
    if [ -n "$recon_synthetic" ]; then
      [ -x "$recon_synthetic" ] || fail "ITC_RECON_PROBE_CMD=$recon_synthetic is not executable"
      [ "$RESULTS_GROUP" != "pr4b-recon" ] || fail "a synthetic prober may not write into the
  real results group. Set RESULTS_GROUP to something a reader cannot mistake for a measurement."
    fi

    # --- probe fixture ---------------------------------------------------------------------
    #
    # Deliberately its own size, and not the one §4.6.6 fixes for the retained runs: that size is
    # derived *from* the bracket this stage has not found yet. It only has to keep fresh mutations
    # available for 120 s at the deepest probe, so it is sized from the measured G4 aggregate with
    # room for a probe to run twice as fast as PR4a's 16-worker point, and it is smaller than the
    # retained fixture because seeding time is the cost paid on every probe.
    #
    # **Why 2x is enough, stated rather than assumed.** With the safety factor on top the fixture
    # survives 2.8x the best measured G4 aggregate, and the pool is frozen at 8 per group: past 16
    # workers per group the binding constraint has already moved off admission onto one authority's
    # capacity to do concurrent work on two CPUs (§3.22), so a deeper probe queues rather than runs
    # faster. A probe that nonetheless spent its fixture would report an unexpected
    # `business_refusal` population, which invalidates that probe rather than quietly lowering it.
    measured_aggregate="${ITC_MEASURED_G4_RATE:-3436}"      # G4, 16 workers/group, pool 8, 600 s
    probe_speedup="${ITC_RECON_SPEEDUP:-200}"               # % — headroom for a deeper probe
    safety="${ITC_FIXTURE_SAFETY:-140}"                     # % explicit headroom on top

    peak_aggregate=$(( measured_aggregate * probe_speedup / 100 ))
    per_org=$(( peak_aggregate * recon_seconds / 4 ))
    required_per_org=$(( (per_org + CONDITIONING_TARGET) * safety / 100 ))
    slots_per_org=$(( (required_per_org + CAPACITY - 1) / CAPACITY ))
    slots_per_org=$(( (slots_per_org / 5000 + 1) * 5000 ))
    export SLOTS="${ENV_SLOTS:-$slots_per_org}"

    mkdir -p "test/results/$RESULTS_GROUP"
    sizing="test/results/$RESULTS_GROUP/probe-fixture-sizing.txt"
    cat > "$sizing" <<SIZING
probe fixture sizing for PR4b reconnaissance (ag-sept-validation-plan.md §4.6.4)
derived $(date -u +%Y-%m-%dT%H:%M:%SZ)

  measured G4 aggregate rate         $measured_aggregate /s   (G4, 16 workers/group, pool 8, 600 s,
                                                     docs/measurements/pr4a-sustained/g4-pool8)
  headroom for a deeper probe        ${probe_speedup}%
  peak aggregate assumed             $peak_aggregate /s
  probe duration                     ${recon_seconds}s
  probe consumption per org          $per_org mutations
  conditioning per org               $CONDITIONING_TARGET mutations
  explicit safety headroom           ${safety}%
  required per org                   $required_per_org mutations
  capacity per slot                  $CAPACITY
  slots per organisation             $SLOTS   (rounded up)
  seeded supply                      $(( SLOTS * CAPACITY * 4 )) mutations across 4 organisations
  measured supply after conditioning $(( (SLOTS - CONDITIONING_SLOTS) * CAPACITY * 4 )) mutations

This is the *probe* fixture and is not the retained one. §4.6.6 sizes the retained fixture from
the deepest bracket this stage selects, which is not known until it has run.
SIZING
    log "probe fixture sizing -> $sizing"
    sed 's/^/    /' "$sizing"

    if [ -n "$recon_synthetic" ]; then
      log "!! SYNTHETIC PROBER: $recon_synthetic"
      log "!! this invocation drives no cell and measures nothing. Every rate below is the test"
      log "!! harness's own, and no artifact it writes describes this machine."
    else
      clear_sandbox_placeholders
      build_generator
    fi

    # --- one probe -------------------------------------------------------------------------
    #
    # Results travel in globals rather than on stdout because log() writes to stdout too, and a
    # capture that swallowed the log lines would hide exactly the progress a long stage needs to
    # show. PROBE_RATE is the only value the walk reads; the rest are for the artifact.
    PROBE_RATE=""; PROBE_CELL=""; PROBE_SHAPE=""; PROBE_SPREAD=""
    recon_probe() {
      local groups="$1" level="$2" series cell ran_seconds ran_workers ran_slots

      log "probe: G$groups at $level workers/group, ${WINDOW}, pool_max_conns=$ALLOCA_POOL_MAX_CONNS"
      ITC_WORKERS_PER_GROUP="$level" ./test/scripts/itc-series.sh "$groups" 1 \
        || fail "the G$groups probe at $level workers/group did not complete. A probe that failed
  is not a probe that found a limit: diagnose it rather than reading its absence as saturation."

      series="$(ls -1dt test/results/$RESULTS_GROUP/*/ 2>/dev/null | head -1)"
      cell="$(ls -1dt "$series"cell-*/ 2>/dev/null | head -1)"
      [ -n "$cell" ] || fail "the G$groups probe at $level produced no cell directory under $series"

      # **Check the artifact against the intent.** The first attempt at the sustained stage ran
      # 60 s cells and reported them as a clean 600 s qualification, because every gate judged the
      # run that happened rather than the run that was asked for. A probe is cheaper to lose and
      # far easier to misread: a level silently probed at the wrong worker count would move the
      # bracket, and nothing downstream could notice.
      ran_seconds="$(python3 -c "
import json,sys
print(int(round(json.load(open(sys.argv[1]))['summary']['duration_seconds'])))" "${cell}run.json")"
      ran_workers="$(python3 -c "
import json,sys
print(json.load(open(sys.argv[1]))['summary'].get('workers_per_group'))" "${cell}run.json")"
      ran_slots="$(grep '^SLOTS=' "${cell}fixture.txt" | cut -d= -f2)"

      [ "$ran_workers" = "$level" ] \
        || fail "the G$groups probe was asked for $level workers/group and ran at $ran_workers.
  The walk would attribute this rate to the wrong level and bracket somewhere else entirely."
      [ "$ran_seconds" -ge $(( recon_seconds * 95 / 100 )) ] \
        || fail "the G$groups probe at $level measured ${ran_seconds}s against ${recon_seconds}s
  asked for. Probe rates are only comparable across levels at one duration."
      [ "$ran_slots" = "$SLOTS" ] \
        || fail "the G$groups probe at $level ran on $ran_slots slots/org, not the $SLOTS this
  stage sized. A probe short of fixture measures exhaustion, not the worker level."

      PROBE_CELL="$cell"
      PROBE_RATE="$(python3 -c "
import json,sys
s=json.load(open(sys.argv[1]))['summary']
print('%.1f' % (s['successful_mutation_goodput'] / s['duration_seconds']))" "${cell}run.json")"

      # Shape and spread are recorded, not gated on. The within-window spread discriminator was
      # calibrated on 60 s cells in the degraded regime (itc-classify.py), and that regime is the
      # one conditioning closed (§3.20) — so a threshold imported here would refuse probes on a
      # boundary no longer measured for this shape. It is retained so a reader can see whether a
      # level's reading looks unlike its neighbours'.
      PROBE_SHAPE="$(./test/scripts/itc-classify.py "$cell" 2>/dev/null | awk 'NR==3{print $3}')"
      PROBE_SPREAD="$(./test/scripts/itc-classify.py "$cell" 2>/dev/null | awk 'NR==3{print $4}')"
      [ -n "$PROBE_SHAPE" ] || { PROBE_SHAPE="-"; PROBE_SPREAD="-"; }

      log "  G$groups @ $level -> ${PROBE_RATE}/s  (shape $PROBE_SHAPE, spread $PROBE_SPREAD)"
    }

    # better_than tests strictly by the margin, in awk because the rates are not integers.
    better_than() { awk -v a="$1" -v b="$2" -v m="$recon_margin" \
      'BEGIN { exit !(a > b * (1 + m/100)) }'; }

    # `if`, not `[ ... ] && ...`. Under `set -e` a false test as the final command of a list is a
    # non-zero status, and the same shape has already aborted a run in this repository once.
    ladder_index() {
      local want="$1" i=0 lvl
      for lvl in $ladder; do
        if [ "$lvl" = "$want" ]; then printf '%s\n' "$i"; return 0; fi
        i=$((i + 1))
      done
      return 1
    }
    ladder_at() { printf '%s\n' "$ladder" | tr ' ' '\n' | sed -n "$(( $1 + 1 ))p"; }
    ladder_size() { printf '%s\n' "$ladder" | wc -w; }

    for groups in $recon_groups; do
      declare -A rate_at=() cell_at=() shape_at=() spread_at=()
      order=""

      # A level is probed at most once per topology. The walk revisits neighbours by design — the
      # lower-side check asks about a level the upward walk may already have measured — and
      # re-driving a cell would spend four minutes to get a second answer to a settled question.
      probe_once() {
        local groups="$1" idx="$2" level
        level="$(ladder_at "$idx")"
        if [ -n "${rate_at[$level]:-}" ]; then return 0; fi
        if [ -n "$recon_synthetic" ]; then
          PROBE_RATE="$("$recon_synthetic" "$groups" "$level")"
          PROBE_CELL="synthetic"; PROBE_SHAPE="-"; PROBE_SPREAD="-"
          log "  G$groups @ $level -> ${PROBE_RATE}/s  (synthetic)"
        else
          recon_probe "$groups" "$level"
        fi
        rate_at[$level]="$PROBE_RATE"
        cell_at[$level]="$PROBE_CELL"
        shape_at[$level]="$PROBE_SHAPE"
        spread_at[$level]="$PROBE_SPREAD"
        order="$order $level"
      }

      start_idx="$(ladder_index "$recon_start")" \
        || fail "ITC_RECON_START=$recon_start is not on the ladder [$ladder]"
      last_idx=$(( $(ladder_size) - 1 ))

      log ""
      log "=== reconnaissance at G$groups: start $recon_start, margin ${recon_margin}%, ladder [$ladder]"

      probe_once "$groups" "$start_idx"
      best_idx="$start_idx"

      # **Walk up first, because the question is where the frontier stops rising.** The direction
      # is decided by one probe rather than assumed: if the level above the start does not beat it
      # by the margin, the start is already at or past the frontier and the walk turns around.
      if [ "$start_idx" -lt "$last_idx" ]; then
        probe_once "$groups" $(( start_idx + 1 ))
        if better_than "${rate_at[$(ladder_at $(( start_idx + 1 )))]}" "${rate_at[$(ladder_at "$start_idx")]}"; then
          best_idx=$(( start_idx + 1 ))
          while [ "$best_idx" -lt "$last_idx" ]; do
            probe_once "$groups" $(( best_idx + 1 ))
            better_than "${rate_at[$(ladder_at $(( best_idx + 1 )))]}" "${rate_at[$(ladder_at "$best_idx")]}" || break
            best_idx=$(( best_idx + 1 ))
          done
        fi
      fi

      # Turn around only if going up bought nothing at all: the start is then a candidate whose
      # lower side has never been observed, which is exactly what the check below needs.
      if [ "$best_idx" -eq "$start_idx" ]; then
        while [ "$best_idx" -gt 0 ]; do
          probe_once "$groups" $(( best_idx - 1 ))
          better_than "${rate_at[$(ladder_at $(( best_idx - 1 )))]}" "${rate_at[$(ladder_at "$best_idx")]}" || break
          best_idx=$(( best_idx - 1 ))
        done
      fi

      S="$(ladder_at "$best_idx")"

      [ "$best_idx" -lt "$last_idx" ] || fail "G$groups: the walk reached the top of the ladder at
  $S workers/group and each level was still better than the one below it. §4.6.4 sets no maximum,
  so this is a bracket outside the starting range rather than a limit. Extend and re-run:
      ITC_RECON_LADDER='$ladder 192 256' ./test/scripts/itc-local-experiment.sh recon"
      [ "$best_idx" -gt 0 ] || fail "G$groups: the walk reached the bottom of the ladder at $S
  workers/group. The frontier is below the starting range; extend downward and re-run:
      ITC_RECON_LADDER='1 $ladder' ./test/scripts/itc-local-experiment.sh recon"

      H="$(ladder_at $(( best_idx + 1 )))"
      L="$(ladder_at $(( best_idx - 1 )))"

      # **The lower-side check** (maintainer decision, 2026-08-19). §4.6.5's selection rule tests
      # only that H fails to beat S, which confirms S is not *below* the frontier and says nothing
      # about S sitting past the peak — and a too-high S would understate capacity at every
      # topology while passing the rule unchallenged. Recon is where that costs one short probe
      # rather than four retained 600 s runs, so S is proposed only when it beats the level below
      # it by the same margin.
      #
      # **The probe below is a guard, not the source of the data, and mutation testing is how that
      # was established** rather than assumed. Every path through the walk above has already
      # measured `best_idx - 1`: the upward walk passed through it, the turnaround probed it to
      # decide it was not better, and a downward walk exits precisely when it fails to beat the
      # level above. So this call is idempotent in all three cases — deleting it changes no
      # current behaviour, and it is kept only so that a future change to the walk cannot leave the
      # check reading a level nothing observed.
      probe_once "$groups" $(( best_idx - 1 ))
      if better_than "${rate_at[$S]}" "${rate_at[$L]}"; then
        lower_side="pass — $S beats $L by more than ${recon_margin}%"
      else
        lower_side="FAIL — $S does not beat $L by ${recon_margin}%; the frontier is at or below $L"
      fi

      report="test/results/$RESULTS_GROUP/recon-G${groups}.txt"
      {
        printf 'PR4b reconnaissance, G%s (ag-sept-validation-plan.md §4.6.4)\n' "$groups"
        printf 'driven %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        if [ -n "$recon_synthetic" ]; then
          printf '*** SYNTHETIC PROBER (%s) ***\n' "$recon_synthetic"
          printf '*** No cell was driven. Every rate below is a test harness fixture and describes\n'
          printf '*** no machine. This file is a self-test transcript, not a probe record.\n\n'
        fi
        printf 'NOT CAPACITY EVIDENCE. These are short non-canonical probes. No rate below may be\n'
        printf 'quoted, entered into E2/E4, or compared with a 600 s horizon average: a probe reads\n'
        printf 'the early part of a trajectory that PR4a measured declining to ~0.75x by 600 s.\n'
        printf 'Their only output is the two levels named at the bottom.\n\n'
        printf '  probe duration      %ss\n' "$recon_seconds"
        printf '  pool_max_conns      %s\n' "$ALLOCA_POOL_MAX_CONNS"
        printf '  slots/organisation  %s at capacity %s\n' "$SLOTS" "$CAPACITY"
        printf '  conditioning        %s mutations/org\n' "$CONDITIONING_TARGET"
        printf '  margin              %s%%  (twice the 2.6%% agreement of ten identical healthy\n' "$recon_margin"
        printf '                      G4 cells, docs/measurements/pr4a-rehearsal/repeats/)\n'
        printf '  ladder              %s\n' "$ladder"
        printf '  start               %s\n\n' "$recon_start"
        printf '  %-8s %10s  %-7s %7s  %s\n' workers probe/s shape spread cell
        for lvl in $(tr ' ' '\n' <<< "$order" | grep -v '^$' | sort -n); do
          printf '  %-8s %10s  %-7s %7s  %s\n' \
            "$lvl" "${rate_at[$lvl]}" "${shape_at[$lvl]}" "${spread_at[$lvl]}" "${cell_at[$lvl]}"
        done
        printf '\n  selected S          %s\n' "$S"
        printf '  deciding H          %s\n' "$H"
        printf '  lower-side check    %s\n' "$lower_side"
      } > "$report"

      log ""
      sed 's/^/    /' "$report"
      log "G$groups reconnaissance -> $report"

      case "$lower_side" in
        FAIL*) fail "G$groups: the lower-side check failed. Proposing S=$S would risk selecting a
  level past the peak, which §4.6.5's rule cannot detect. Re-run reconnaissance around $L before
  spending four retained 600 s runs on this bracket." ;;
      esac

      unset rate_at cell_at shape_at spread_at
    done

    log ""
    log "reconnaissance complete for topologies [$recon_groups]. Size the retained fixture from the"
    log "deepest selected bracket next: ./test/scripts/itc-local-experiment.sh fixture"
    ;;

  fixture|capacity)
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
