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
                        (ag-sept/milestone-validation.md §4.6.2)

  pool                  the bounded G1 pool-sensitivity preflight over the conditioned path,
                        at one worker level, so a single pool policy can be frozen (§4.6.3)

  sustained             the final PR4a qualification: one conditioned 600 s G1 run and one
                        conditioned 600 s G4 run on the ordinary measurement path, with the
                        fixture sized from the measured rate. Qualification evidence only —
                        it is not a capacity result and no E4_local follows from it

  recon                 adaptive workers_per_group reconnaissance (§4.6.4): short probes that
                        bracket saturation per topology and propose S and H. Not capacity
                        evidence, and nothing it produces may be quoted as a rate

  fixture               derive the one per-organisation fixture size the retained comparison uses
                        (§4.6.6), from the deepest bracket reconnaissance selected. Reads the
                        recon reports; writes the sizing and the value the capacity stage consumes

  capacity              the retained comparison (§4.6.5): per topology, four 600 s runs — the
                        selected point, the deciding higher point, and each again as an
                        independent confirmation — then the knee decision and E2_local/E4_local

  drift                 N identical 600 s runs at one worker level, to measure whether rate tracks
                        run position. Selects nothing and backs no capacity claim; it exists
                        because the comparison's fixed S/H order confounds position with role

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

# **"Materially", as one number for every stage that needs it** (ag-sept/milestone-validation.md §4.6.5,
# which owns the definition). It is a **preselected engineering materiality margin**: the smallest
# difference in sustained Goodput this experiment treats as a real difference in capacity, fixed in
# advance so a knee is not decided by a threshold chosen to fit the numbers.
#
# **It is not a noise floor, and the plan's original wording that called it one was corrected after
# review.** The 2.6% agreement of ten identical G4 cells informed the choice; it never licensed
# reading 5% as a bound on this environment's noise. PR4b measured four identical G1 runs spanning
# 25.1% and G4 reproducing to 1.4%, so no single noise figure describes the machine at all.
# Materiality asks whether a difference matters; reproducibility asks whether the environment can
# measure the point — they are separate gates and §4.6.5 keeps them separate.
#
# **Defined here rather than inside a stage, because more than one stage decides with it.** It began
# inside `recon`, and the capacity stage's plateau validation then read it out of scope: under
# `set -u` that is an unbound-variable abort, after the retained runs have been driven. One
# definition also means the bracket is judged by the standard it was chosen by.
recon_margin="${ITC_RECON_MARGIN:-5}"

# recon_level reads a selected level out of a reconnaissance report: $1 the report, $2 the label.
#
# **One reader, because two readers of the same line drifted.** Both `fixture` and `capacity` parsed
# these lines with `awk '{print $NF}'`. When the report gained a parenthetical after each level —
# "selected S 12 (lowest level on the discovered plateau)" — `capacity` was corrected and `fixture`
# was not, so `fixture` went looking for a probe rate for level "plateau)". Nothing tied the two
# together, and `make ci` had nothing to say about it: the coupling was two similar lines in one
# file with no shared definition.
recon_level() {
  sed -n "s/^  $2  *\([0-9][0-9]*\).*/\1/p" "$1"
}

# **Readers for the probe table, scoped to the table itself.** A report is not only its table: it
# opens with a configuration block whose lines look enough like probe rows to be read as one. The
# first version matched any line whose second field was numeric, which found
# `slots/organisation  15000 at capacity 20` and concluded the topology's best observed rate was
# 15,000/s — so a valid bracket was refused for sitting "materially below" a fixture parameter.
#
# The table is delimited by its own header and the blank line after it, so that is what these read.
# They are shared for the same reason recon_level is: two copies of a parse drifted once already.
# assert_cell_intent checks a retained cell against the run that was *asked for*:
#   $1 cell directory, $2 a label for messages, $3 the intended workers_per_group.
# Duration and fixture come from ITC_WINDOW_SECONDS and SLOTS, which the calling stage has fixed.
#
# **Every gate inside a cell judges the run that happened; none of them knows which run was
# requested.** PR4a's first sustained attempt ran 60 s cells on the wrong fixture and passed
# everything, because each gate was satisfied by the run in front of it.
#
# **One function, because the resume path skipped these checks entirely** (review finding,
# 2026-08-19). The checks lived inline after the run that produced the cell, so a cell already on
# disk was accepted on sight — and that is the case most likely to be wrong, since it exists only
# when an earlier invocation used a possibly different bracket or fixture. Returns non-zero and
# explains; the caller decides whether that is fatal.
assert_cell_intent() {
  local cell="$1" label="$2" want_level="$3" ran_seconds ran_workers ran_slots
  local want_seconds="${ITC_WINDOW_SECONDS:-600}"

  ran_seconds="$(python3 -c "
import json,sys
print(int(round(json.load(open(sys.argv[1]))['summary']['duration_seconds'])))" "$cell/run.json")"
  ran_workers="$(python3 -c "
import json,sys
print(json.load(open(sys.argv[1]))['summary'].get('workers_per_group'))" "$cell/run.json")"
  ran_slots="$(grep '^SLOTS=' "$cell/fixture.txt" 2>/dev/null | cut -d= -f2)"

  if [ "$ran_workers" != "$want_level" ]; then
    printf '\n!! %s: asked for %s workers/group, cell ran at %s. It is a sound run of a
  different point, which is the hardest kind of wrong to notice later.\n' \
      "$label" "$want_level" "$ran_workers" >&2
    return 1
  fi
  if [ "$ran_seconds" -lt $(( want_seconds * 95 / 100 )) ]; then
    printf '\n!! %s: measured %ss against %ss. §4.6.5'"'"'s comparison quantity is the full-600 s
  horizon average; a shorter run answers a different question.\n' \
      "$label" "$ran_seconds" "$want_seconds" >&2
    return 1
  fi
  if [ "$ran_slots" != "$SLOTS" ]; then
    printf '\n!! %s: ran on %s slots/org, not the %s this comparison fixed. Two arms seeded
  differently are not comparable however carefully their averages are computed (§4.6.5).\n' \
      "$label" "$ran_slots" "$SLOTS" >&2
    return 1
  fi
  log "$label ran ${ran_seconds}s at $ran_workers workers/group on $ran_slots slots/org"
  return 0
}

recon_probe_rate() {
  awk -v want="$2" '
    /^  workers/ { inside = 1; next }
    /^ *$/       { inside = 0 }
    inside && $1 == want && $2 ~ /^[0-9.]+$/ { print $2; exit }' "$1"
}

recon_best_rate() {
  awk '
    /^  workers/ { inside = 1; next }
    /^ *$/       { inside = 0 }
    inside && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9.]+$/ { if ($2 + 0 > best) best = $2 + 0 }
    END { print best }' "$1"
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
    # retained measurement?** (ag-sept/milestone-validation.md §4.6.4.) It is explicitly *not* capacity
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

    recon_groups="${ITC_RECON_GROUPS:-1 2 4}"
    recon_only="${ITC_RECON_ONLY:-}"

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
probe fixture sizing for PR4b reconnaissance (ag-sept/milestone-validation.md §4.6.4)
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
    # Refuses an out-of-range index rather than returning empty. An empty level silently becomes an
    # unset array subscript two lines later, and the walk then dies on `unbound variable` instead of
    # on the ladder-end refusal that was supposed to explain itself — which is what removing that
    # refusal actually produced under mutation.
    ladder_at() {
      local value
      [ "$1" -ge 0 ] || fail "ladder index $1 is below the ladder; the walk should have refused at
  its bottom end before asking for this"
      value="$(printf '%s\n' "$ladder" | tr ' ' '\n' | sed -n "$(( $1 + 1 ))p")"
      [ -n "$value" ] || fail "ladder index $1 is past the top of [$ladder]; the walk should have
  refused at its top end before asking for this"
      printf '%s\n' "$value"
    }
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

      # **Probe-only mode: drive named levels and report their rates, selecting nothing.**
      # `ITC_RECON_ONLY='16'` exists for the diagnostic question the walk cannot ask — re-reading a
      # level already probed, to separate run-to-run noise from drift across a session. It writes
      # `probe-G<n>.txt` rather than `recon-G<n>.txt` precisely so it cannot be mistaken for a
      # bracket: `fixture` and `capacity` both glob the latter and will never read this.
      if [ -n "$recon_only" ]; then
        for lvl in $recon_only; do
          idx="$(ladder_index "$lvl")" \
            || fail "ITC_RECON_ONLY names $lvl, which is not on the ladder [$ladder]"
          probe_once "$groups" "$idx"
        done
        probe_report="test/results/$RESULTS_GROUP/probe-G${groups}.txt"
        {
          printf 'PR4b diagnostic probes, G%s — NO BRACKET IS SELECTED\n' "$groups"
          printf 'driven %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
          printf 'These are individual %ss probes at named levels, driven to answer a question about\n' "$recon_seconds"
          printf 'the measurement rather than to bracket saturation. No S, no H, no capacity claim.\n\n'
          printf '  %-8s %10s  %-7s %7s  %s\n' workers probe/s shape spread cell
          for lvl in $(tr ' ' '\n' <<< "$order" | grep -v '^$' | sort -n); do
            printf '  %-8s %10s  %-7s %7s  %s\n' \
              "$lvl" "${rate_at[$lvl]}" "${shape_at[$lvl]}" "${spread_at[$lvl]}" "${cell_at[$lvl]}"
          done
        } > "$probe_report"
        log ""
        sed 's/^/    /' "$probe_report"
        log "G$groups diagnostic probes -> $probe_report"
        unset rate_at cell_at shape_at spread_at
        continue
      fi

      start_idx="$(ladder_index "$recon_start")" \
        || fail "ITC_RECON_START=$recon_start is not on the ladder [$ladder]"
      last_idx=$(( $(ladder_size) - 1 ))

      log ""
      log "=== reconnaissance at G$groups: start $recon_start, margin ${recon_margin}%, ladder [$ladder]"

      # rate_of reads a probed level's rate by ladder index. The nested expansion it replaces was
      # correct and unreadable, and this walk is the part of the stage a person most needs to follow.
      rate_of() { printf '%s\n' "${rate_at[$(ladder_at "$1")]}"; }

      probe_once "$groups" "$start_idx"
      peak_idx="$start_idx"

      # **Climb while a higher level is materially better.** The direction is decided by one probe
      # rather than assumed: if the level above the start does not beat it by the margin, the start
      # is already at or above the frontier's throughput and there is nothing above to find.
      if [ "$start_idx" -lt "$last_idx" ]; then
        probe_once "$groups" $(( start_idx + 1 ))
        if better_than "$(rate_of $(( start_idx + 1 )))" "$(rate_of "$start_idx")"; then
          peak_idx=$(( start_idx + 1 ))
          while [ "$peak_idx" -lt "$last_idx" ]; do
            probe_once "$groups" $(( peak_idx + 1 ))
            better_than "$(rate_of $(( peak_idx + 1 )))" "$(rate_of "$peak_idx")" || break
            peak_idx=$(( peak_idx + 1 ))
          done
        fi
      fi

      # **Then descend through the plateau, because S is the lowest level that reaches the
      # frontier's throughput and not the level that happens to read highest** (maintainer decision,
      # 2026-08-19). Where throughput is flat across several levels, every one of them delivers the
      # same Goodput and the lowest does it with the least queueing, so quoting a higher one
      # attributes capacity to workers that bought nothing.
      #
      # This is what G2 and G4 turned out to need. Both were flat from 12 to 16 — 2.2% and 2.0%
      # apart, inside the margin — so the earlier rule, which descended only while a lower level was
      # materially *better*, stopped at 16 and then had to refuse its own candidate. G1 was not
      # flat there (16 beat 12 by 7.0%) and is unaffected: the descent breaks immediately.
      #
      # **The descent runs unconditionally, and that is equivalent to gating it on the climb having
      # found nothing — not broader.** Mutation testing established this rather than reasoning
      # asserting it: re-adding the gate changed no case. The reason is that climbing from one level
      # to the next requires the higher to be materially better, so after any climb the level below
      # the peak is already known to be materially worse and the descent breaks on its first test.
      # A plateau therefore cannot be traversed above the start, and the unconditional form is kept
      # only because it is one less condition to read, not because it reaches more cases.
      s_idx="$peak_idx"
      while [ "$s_idx" -gt 0 ]; do
        probe_once "$groups" $(( s_idx - 1 ))
        # Stop at the bottom of the plateau: the level below is materially worse, so it is off it.
        if better_than "$(rate_of "$s_idx")" "$(rate_of $(( s_idx - 1 )))"; then break; fi
        s_idx=$(( s_idx - 1 ))
      done

      S="$(ladder_at "$s_idx")"

      [ "$peak_idx" -lt "$last_idx" ] || fail "G$groups: the climb reached the top of the ladder at
  $(ladder_at "$peak_idx") workers/group and each level was still materially better than the one
  below it. §4.6.4 sets no maximum, so this is a bracket outside the starting range rather than a
  limit. Extend and re-run:
      ITC_RECON_LADDER='$ladder 192 256' ./test/scripts/itc-local-experiment.sh recon"
      [ "$s_idx" -gt 0 ] || fail "G$groups: the plateau reached the bottom of the ladder at $S
  workers/group, so no level below it was measured to be materially worse and the frontier is
  below the starting range. Extend downward and re-run:
      ITC_RECON_LADDER='1 $ladder' ./test/scripts/itc-local-experiment.sh recon"

      H="$(ladder_at $(( s_idx + 1 )))"
      L="$(ladder_at $(( s_idx - 1 )))"

      # **The lower-side condition, asserted rather than discovered.** §4.6.5's selection rule tests
      # only that H fails to beat S, which confirms S is not *below* the frontier and says nothing
      # about S sitting past it — and a too-high S would understate capacity at every topology while
      # passing that rule unchallenged. The plateau descent above is what prevents it, so by the
      # time control reaches here the condition already holds: the descent stops precisely when the
      # level below is materially worse, which is what this tests.
      #
      # It is kept, and labelled honestly, for two reasons. It states the property S must satisfy in
      # the retained report, where a reader can see it rather than having to reconstruct it from a
      # probe table. And it is the assertion that fails if a future change to the descent stops
      # holding the invariant — which is the same reason the probe below is kept: every path through
      # the walk has already measured `s_idx - 1`, so the call is idempotent today and exists so a
      # later walk cannot leave the condition reading a level nothing observed. Mutation testing
      # established both facts rather than reasoning asserting them.
      probe_once "$groups" $(( s_idx - 1 ))
      if better_than "${rate_at[$S]}" "${rate_at[$L]}"; then
        lower_side="pass — $S beats $L by more than ${recon_margin}%"
      else
        lower_side="FAIL — $S does not beat $L by ${recon_margin}%; the frontier is at or below $L"
      fi

      report="test/results/$RESULTS_GROUP/recon-G${groups}.txt"
      {
        printf 'PR4b reconnaissance, G%s (ag-sept/milestone-validation.md §4.6.4)\n' "$groups"
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
        printf '  margin              %s%%  (preselected materiality margin, not a noise floor:\n' "$recon_margin"
        printf '                      ag-sept/milestone-validation.md §4.6.5)\n'
        printf '  ladder              %s\n' "$ladder"
        printf '  start               %s\n\n' "$recon_start"
        printf '  %-8s %10s  %-7s %7s  %s\n' workers probe/s shape spread cell
        for lvl in $(tr ' ' '\n' <<< "$order" | grep -v '^$' | sort -n); do
          printf '  %-8s %10s  %-7s %7s  %s\n' \
            "$lvl" "${rate_at[$lvl]}" "${shape_at[$lvl]}" "${spread_at[$lvl]}" "${cell_at[$lvl]}"
        done
        printf '\n  selected S          %s   (lowest level on the discovered plateau)\n' "$S"
        printf '  deciding H          %s   (higher, and not materially better than S)\n' "$H"
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

  fixture)
    # **One fixture size for the whole comparison, derived from the deepest point it must survive**
    # (ag-sept/milestone-validation.md §4.6.6). The retained runs are S, H and both confirmations at
    # G1, G2 and G4, and the *same* per-organisation size is reused unchanged across all of them:
    # resizing per topology would change the workload as well as the topology, and the comparison
    # would no longer be between two arrangements of one experiment.
    #
    # It reads reconnaissance rather than re-deriving a rate, because §4.6.6 asks for an expected
    # maximum useful rate and reconnaissance has just measured one at every level it probed. The
    # deepest *intended* bracket is the highest H across the topologies — an H run is retained too,
    # so the fixture has to carry it.
    recon_group="${ITC_RECON_RESULTS_GROUP:-pr4b-recon}"
    out_group="${RESULTS_GROUP:-pr4b-capacity}"
    seconds="${ITC_WINDOW_SECONDS:-600}"
    safety="${ITC_FIXTURE_SAFETY:-140}"

    reports="$(ls -1 test/results/$recon_group/recon-G*.txt 2>/dev/null || true)"
    [ -n "$reports" ] || fail "no reconnaissance reports under test/results/$recon_group/. The
  order is recon -> fixture -> capacity and each consumes the previous answer: sizing the fixture
  before the bracket is known is how PR4a's first attempt sized one for a rate it never measured.
      ./test/scripts/itc-local-experiment.sh recon"

    # The deepest level and the fastest observed probe, taken across every topology's report. Both
    # are read out of the retained artifacts rather than passed in by hand, so the sizing can be
    # re-derived from the same files a reader has.
    deepest_level=0
    max_rate=0
    covered=""
    for report in $reports; do
      groups="$(basename "$report" | sed 's/^recon-G//; s/\.txt$//')"
      s_level="$(recon_level "$report" 'selected S')"
      h_level="$(recon_level "$report" 'deciding H')"
      [ -n "$s_level" ] && [ -n "$h_level" ] \
        || fail "$report names no selected S or deciding H, so it is not a completed
  reconnaissance. Re-run recon for G$groups before sizing the fixture."

      # The retained points are S and H only. A probe taken above H is not retained and must not
      # inflate the fixture; a probe below S is not retained either.
      for level in "$s_level" "$h_level"; do
        rate="$(recon_probe_rate "$report" "$level")"
        [ -n "$rate" ] || fail "$report selects level $level but retains no probe rate for it"
        max_rate="$(awk -v a="$max_rate" -v b="$rate" 'BEGIN{print (b>a)?b:a}')"
      done
      [ "$h_level" -le "$deepest_level" ] || deepest_level="$h_level"
      covered="$covered G$groups(S=$s_level,H=$h_level)"
    done

    # **A 120 s probe rate over-estimates 600 s consumption, and that is the direction to err in.**
    # Every PR4a run declined across its window, so the horizon average of a retained run is below
    # the early rate a probe reads. Sizing from the probe therefore buys headroom rather than
    # spending it, and the safety factor sits on top of that.
    peak_aggregate="$(printf '%.0f' "$max_rate")"
    per_org=$(( peak_aggregate * seconds / 4 ))
    required_per_org=$(( (per_org + CONDITIONING_TARGET) * safety / 100 ))
    slots_per_org=$(( (required_per_org + CAPACITY - 1) / CAPACITY ))
    slots_per_org=$(( (slots_per_org / 5000 + 1) * 5000 ))

    mkdir -p "test/results/$out_group"
    sizing="test/results/$out_group/fixture-sizing.txt"
    cat > "$sizing" <<SIZING
retained fixture sizing for the PR4b capacity comparison
(ag-sept/milestone-validation.md §4.6.6)
derived $(date -u +%Y-%m-%dT%H:%M:%SZ)

  reconnaissance read               test/results/$recon_group/
  brackets covered                 $covered
  deepest retained level            $deepest_level workers/group
  fastest retained probe            $max_rate /s aggregate, over ${ITC_RECON_SECONDS:-120}s

  A 120 s probe reads the early, high part of a trajectory that PR4a measured declining to ~0.75x
  by 600 s, so this rate is above the 600 s horizon average the retained runs will report. Sizing
  from it buys headroom rather than spending it.

  measured duration                 ${seconds}s
  measured consumption per org      $per_org mutations
  conditioning per org              $CONDITIONING_TARGET mutations
  explicit safety headroom          ${safety}%
  required per org                  $required_per_org mutations
  capacity per slot                 $CAPACITY
  slots per organisation            $slots_per_org   (rounded up)
  seeded supply                     $(( slots_per_org * CAPACITY * 4 )) mutations across 4 organisations
  measured supply after conditioning $(( (slots_per_org - CONDITIONING_SLOTS) * CAPACITY * 4 )) mutations
  worst-case consumption            $(awk -v c="$per_org" -v s="$(( (slots_per_org - CONDITIONING_SLOTS) * CAPACITY ))" \
                                        'BEGIN{printf "%.1f%%", 100*c/s}') of measured supply per org

The same per-organisation size is used at G1, G2 and G4. Resizing it per topology would change
the workload as well as the topology, and the comparison would not be between two arrangements
of one experiment.
SIZING

    # The machine-readable half, so the capacity stage consumes this answer rather than re-deriving
    # it from the same inputs and possibly disagreeing. A stage that recomputed would be a second
    # definition of the fixture, and nothing would notice the two drifting apart.
    printf 'SLOTS=%s\nDEEPEST_LEVEL=%s\nDERIVED_FROM=%s\n' \
      "$slots_per_org" "$deepest_level" "$sizing" \
      > "test/results/$out_group/retained-fixture.env"

    log "retained fixture sizing -> $sizing"
    sed 's/^/    /' "$sizing"
    log "capacity stage inputs -> test/results/$out_group/retained-fixture.env"
    log "drive the retained comparison next: ./test/scripts/itc-local-experiment.sh capacity"
    ;;

  drift)
    # **N identical runs at one worker level, to separate run position from S/H role.**
    #
    # The retained comparison drives S, H, S-confirm, H-confirm in that fixed order, so `S` always
    # occupies positions 1 and 3 and `H` always 2 and 4. On 2026-08-19 the local G2 series rose
    # monotonically with position — +0.0%, +2.8%, +8.5%, +9.7% — and G1's position-4 run was its
    # highest at +10.5%, which is enough on its own to produce the 9.1% "H beats S" that left G1's
    # knee unresolved. G4 showed no such effect (+1.4% at most).
    #
    # A drift that tracks position rather than worker level cannot be distinguished from a real
    # difference between S and H while every S runs early and every H runs late. Holding the level
    # fixed removes the role entirely: whatever remains is the environment's own trend across a
    # session, measured rather than argued about.
    #
    # **This is not capacity evidence and selects nothing.** Identical runs describe the measurement
    # environment. Its own results group and file name keep it away from anything that reads a
    # bracket or a retained point.
    unset PLAN_PROBE PG_AUTO_EXPLAIN PG_STAT_STATEMENTS PG_LOG_AUTOVACUUM PG_AUTO_EXPLAIN_SAMPLE

    drift_groups="${ITC_DRIFT_GROUPS:-1}"
    drift_level="${ITC_DRIFT_LEVEL:-12}"
    drift_runs="${ITC_DRIFT_RUNS:-4}"
    out_group="${RESULTS_GROUP:-pr4b-drift-g${drift_groups}}"
    export RESULTS_GROUP="$out_group"
    export WINDOW="${ENV_WINDOW:-600s}"
    export REQUIRE="${REQUIRE:-capacity}"
    seconds="${ITC_WINDOW_SECONDS:-600}"

    # **The same fixture the comparison used, not a fresh derivation.** The question is about the
    # configuration those twelve runs were driven under; a differently sized fixture would answer a
    # question nobody asked.
    fixture_env="${ITC_FIXTURE_ENV:-test/results/pr4b-capacity/retained-fixture.env}"
    [ -f "$fixture_env" ] || fail "$fixture_env does not exist. This stage reproduces the retained
  comparison's configuration at one worker level, so it needs that comparison's fixture size."
    # shellcheck disable=SC1090
    . "$fixture_env"
    export SLOTS="${ENV_SLOTS:-$SLOTS}"

    log "drift: $drift_runs identical runs at G$drift_groups, $drift_level workers/group, ${WINDOW},"
    log "  $SLOTS slots/org, pool_max_conns=$ALLOCA_POOL_MAX_CONNS (from $fixture_env)"

    position=1
    while [ "$position" -le "$drift_runs" ]; do
      run_dir="$(printf 'test/results/%s/run-%02d' "$out_group" "$position")"
      if [ -f "$run_dir/cell-01/run.json" ]; then
        log "position $position already retained at $run_dir — keeping it"
        position=$((position + 1))
        continue
      fi

      log ""
      log "position $position of $drift_runs: G$drift_groups at $drift_level workers/group"
      clear_sandbox_placeholders
      SERIES="$run_dir" ITC_WORKERS_PER_GROUP="$drift_level" \
        ./test/scripts/itc-series.sh "$drift_groups" 1 \
        || fail "the position-$position run did not complete. A drift series with a gap in it
  cannot say whether the trend is monotonic, which is the whole question."

      cell="$run_dir/cell-01"
      # The same check the capacity stage applies, for the same reason: identical runs are only
      # identical if each one actually ran at the level, duration and fixture asked for.
      assert_cell_intent "$cell" "position $position" "$drift_level" \
        || fail "the position-$position run does not match the rest of the series; see above. The
  runs must be identical or the series measures something other than position."
      ./test/scripts/itc-slices.py "$cell" | tee "$cell/slices.txt" > /dev/null
      position=$((position + 1))
    done

    # The table is written from the runs' own manifests so it is re-derivable, and it reports the
    # change against position 1 because that is the quantity the confound is made of.
    report="test/results/$out_group/drift-result.txt"
    python3 - "test/results/$out_group" "$drift_groups" "$drift_level" > "$report" <<'DRIFT'
import json, pathlib, sys
root, groups, level = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
rows = []
for path in sorted(root.glob("run-*/cell-01/run.json")):
    s = json.loads(path.read_text())["summary"]
    phases = dict(l.split("=", 1) for l in (path.parent / "phases.txt").read_text().split())
    rows.append((path.parent.parent.name, phases["measured_start"][11:16],
                 s["successful_mutation_goodput"] / s["duration_seconds"]))
print(f"Run-position effect at G{groups}, {level} workers/group — IDENTICAL RUNS")
print("This selects nothing and is not a capacity result. It measures whether the environment")
print("trends across a session, which the retained comparison's fixed S/H order cannot separate")
print("from a real difference between the two levels (ag-sept/milestone-validation.md §4.6.5).\n")
print(f"  {'run':<10}{'start':<8}{'pos':>4}{'rate/s':>11}{'vs pos 1':>11}")
base = rows[0][2] if rows else 0
for position, (name, start, rate) in enumerate(rows, 1):
    print(f"  {name:<10}{start:<8}{position:>4}{rate:>11.1f}{100 * (rate / base - 1):>+10.1f}%")
if len(rows) >= 2:
    lo, hi = min(r[2] for r in rows), max(r[2] for r in rows)
    print(f"\n  spread {100 * (hi / lo - 1):.1f}% across {len(rows)} identical runs")
    print("  monotonic with position: "
          + ("yes" if all(rows[i][2] < rows[i + 1][2] for i in range(len(rows) - 1)) else "no"))
DRIFT
    log ""
    sed 's/^/    /' "$report"
    log "drift result -> $report"
    ;;

  capacity)
    # **The retained comparison** (ag-sept/milestone-validation.md §4.6.5). Per topology, four 600 s runs
    # in this order: the selected point, the deciding higher point, then each again as an
    # independent confirmation. Each begins from its own reset/reseed/conditioning sequence, which
    # is what itc-run.sh does per cell — so a run is a cell, and the four are four cells rather than
    # one series analysed four ways.
    #
    # The order is §4.6.5's own and is not an implementation preference: S then H then the
    # confirmations means a bracket that turns out to be wrong is visible after two runs rather
    # than after four.
    #
    # **The ordinary measurement path**, unset explicitly so an exported value from an earlier
    # diagnostic shell cannot instrument the runs that carry the result.
    unset PLAN_PROBE PG_AUTO_EXPLAIN PG_STAT_STATEMENTS PG_LOG_AUTOVACUUM PG_AUTO_EXPLAIN_SAMPLE

    recon_group="${ITC_RECON_RESULTS_GROUP:-pr4b-recon}"
    out_group="${RESULTS_GROUP:-pr4b-capacity}"
    export RESULTS_GROUP="$out_group"
    export WINDOW="${ENV_WINDOW:-600s}"
    export REQUIRE="${REQUIRE:-capacity}"
    seconds="${ITC_WINDOW_SECONDS:-600}"

    fixture_env="test/results/$out_group/retained-fixture.env"
    [ -f "$fixture_env" ] || fail "$fixture_env does not exist, so the one fixture size §4.6.6
  fixes for this comparison has not been derived. Every arm must be seeded identically and the
  size must come from the deepest selected bracket:
      ./test/scripts/itc-local-experiment.sh fixture"
    # shellcheck disable=SC1090
    . "$fixture_env"
    export SLOTS="${ENV_SLOTS:-$SLOTS}"
    log "retained fixture: $SLOTS slots/organisation at capacity $CAPACITY (from $DERIVED_FROM)"

    for groups in ${ITC_CAPACITY_GROUPS:-1 2 4}; do
      report="test/results/$recon_group/recon-G${groups}.txt"
      [ -f "$report" ] || fail "no reconnaissance report at $report, so G$groups has no selected
  bracket. §4.6.4 discovers the bracket; this stage only measures it."
      S="$(recon_level "$report" 'selected S')"
      H="$(recon_level "$report" 'deciding H')"
      [ -n "$S" ] && [ -n "$H" ] || fail "$report names no selected S or deciding H"
      # A reconnaissance whose lower-side check failed proposes no bracket. Reading S out of it
      # anyway would spend four 600 s runs on a level its own report refused to stand behind.
      grep -q '^  lower-side check    pass' "$report" \
        || fail "$report did not pass the lower-side check, so its bracket is not a proposed
  selection. Re-run reconnaissance around the level below it before spending four retained runs."

      # **A common bracket across the arms, when the maintainer sets one** (decision, 2026-08-19).
      # Reconnaissance selects per topology, and on 2026-08-19 that returned S=12, S=8, S=12 — where
      # the dissenting 8 read within 0.8% of its own 12, well inside the ~6% variation the same
      # level shows between sessions. E2 and E4 compare topologies, so arms measured at different
      # demand-per-group would carry that difference into the efficiency; §2.3 of the validation plan
      # asks for like against like.
      #
      # **The override is validated against each topology's own probes rather than trusted.** A
      # hand-set level that no reconnaissance measured, or one that a topology measured materially
      # below its own best, is refused here — otherwise this variable would be a way to move the
      # operating point without evidence, which is the whole failure the stage sequence prevents.
      if [ -n "${ITC_CAPACITY_S:-}" ]; then
        common_s="$ITC_CAPACITY_S"
        common_h="${ITC_CAPACITY_H:?ITC_CAPACITY_S was set without ITC_CAPACITY_H; a selected point
  without its deciding point cannot resolve a knee (§4.6.5)}"

        s_rate="$(recon_probe_rate "$report" "$common_s")"
        h_rate="$(recon_probe_rate "$report" "$common_h")"
        [ -n "$s_rate" ] || fail "G$groups reconnaissance never probed $common_s workers/group, so
  ITC_CAPACITY_S=$common_s is a level this topology has no evidence about. Probe it first:
      ITC_RECON_ONLY='$common_s' ITC_RECON_GROUPS=$groups ./test/scripts/itc-local-experiment.sh recon"
        [ -n "$h_rate" ] || fail "G$groups reconnaissance never probed $common_h workers/group, so
  ITC_CAPACITY_H=$common_h is a level this topology has no evidence about."

        # **On this topology's plateau means: not materially worse than the best rate it probed.**
        #
        # This one comparison also enforces §4.6.4's requirement that `H` must not be materially
        # better than `S`, and a separate check for that was removed as unreachable. `best_rate` is
        # the maximum over every probed level and `H` is one of them, so `best_rate >= h_rate`
        # always; any `H` that materially beat `S` would therefore have already failed here. The
        # separate check could never fire, and a check that cannot fire is worse than none, because
        # it reads as protection. The self-test established this rather than inspection: a case
        # written to exercise it kept refusing one line earlier.
        best_rate="$(recon_best_rate "$report")"
        awk -v best="$best_rate" -v s="$s_rate" -v m="$recon_margin" \
          'BEGIN { exit !(best > s * (1 + m/100)) }' \
          && fail "G$groups measured $common_s workers/group at $s_rate/s against its own best of
  $best_rate/s, which is materially better at ${recon_margin}%. ITC_CAPACITY_S=$common_s is not on
  this topology's plateau, so running the arm there would measure it below its own frontier. When
  the better level is above $common_s this is also §4.6.4's 'bracket not found': move the bracket
  up rather than retaining four runs that will report the knee is elsewhere."

        # H must be higher than S. Ordering is the one part of §4.6.4's definition of H that the
        # rate comparison above cannot express.
        [ "$common_h" -gt "$common_s" ] || fail "ITC_CAPACITY_H=$common_h is not above
  ITC_CAPACITY_S=$common_s; H is by definition a higher level (§4.6.4)."

        log "G$groups: common bracket S=$common_s H=$common_h (reconnaissance selected S=$S H=$H;"
        log "  this topology probed $common_s at $s_rate/s against its own best $best_rate/s)"

        # **Retained, not just logged** (§4.6.4's third condition on a common bracket). A reader has
        # to be able to see that the arms ran at a substituted level rather than at each topology's
        # own selection, and a log line is not an artifact. Appended per topology so the file
        # records the whole comparison.
        record="test/results/$out_group/common-bracket.txt"
        if [ ! -f "$record" ]; then
          {
            printf 'Common S/H bracket across the arms (ag-sept/milestone-validation.md §4.6.4)\n'
            printf 'recorded %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
            printf 'These runs did NOT use each topology'"'"'s own reconnaissance selection. One\n'
            printf 'bracket was fixed for every arm so that E2 and E4 compare topologies at the\n'
            printf 'same demand-per-group (§2.3). Each level below was checked against that\n'
            printf 'topology'"'"'s own probe table before any run was driven.\n\n'
            printf '  %-6s %-14s %-14s %-12s %s\n' topology 'common S/H' 'recon S/H' 'S probed at' "topology's best"
          } > "$record"
        fi
        printf '  %-6s %-14s %-14s %-12s %s
' \
          "G$groups" "$common_s/$common_h" "$S/$H" "$s_rate/s" "$best_rate/s" >> "$record"

        S="$common_s"
        H="$common_h"
      fi

      log ""
      log "=== G$groups retained comparison: S=$S, H=$H, ${WINDOW} each, four runs"

      for role in s h s-confirm h-confirm; do
        case "$role" in
          s|s-confirm) level="$S" ;;
          h|h-confirm) level="$H" ;;
        esac

        run_dir="test/results/$out_group/g${groups}-${role}"

        # **A kept cell faces the same gate as a driven one** (review finding, 2026-08-19). Resume
        # exists so a fourteen-run sequence interrupted at run nine does not restart from run one,
        # and it originally accepted any existing `cell-01/run.json` on sight. That is precisely
        # where a stale cell enters: change the bracket or the fixture and re-run, and the runs
        # already on disk are from the *previous* configuration — a 600 s S at the old worker level,
        # sound and `capacity`-certified, silently mixed with confirmations from the new one. Rates
        # close enough to pass the reproducibility test then produce a knee nobody measured.
        if [ -f "$run_dir/cell-01/run.json" ]; then
          assert_cell_intent "$run_dir/cell-01" "G$groups $role (retained earlier)" "$level" \
            || fail "the retained G$groups $role cell does not match this comparison. Delete it and
  re-drive that role, or point RESULTS_GROUP at a fresh group: resuming across a changed bracket or
  fixture is how a sound run of a different experiment enters a knee."
          log "G$groups $role already retained at $run_dir and matches this comparison — keeping it"
          continue
        fi

        log ""
        log "G$groups $role: $level workers/group, ${WINDOW}, pool_max_conns=$ALLOCA_POOL_MAX_CONNS"

        # **SERIES names the destination, so the cell lands where the result lives.** The
        # alternative was driving into a timestamped series and copying the cell afterwards, which
        # duplicates every retained panel export and creates a second copy that can drift from the
        # first.
        clear_sandbox_placeholders
        SERIES="$run_dir" ITC_WORKERS_PER_GROUP="$level" \
          ./test/scripts/itc-series.sh "$groups" 1 \
          || fail "the G$groups $role run did not complete. Stop here and diagnose this run rather
  than adding shorter runs or controls around it: §4.6.5 needs this exact point, and a bracket
  missing one of its four observations establishes nothing."

        cell="$run_dir/cell-01"
        [ -f "$cell/run.json" ] || fail "G$groups $role produced no cell at $cell"

        assert_cell_intent "$cell" "G$groups $role" "$level" \
          || fail "the G$groups $role run does not match what this stage asked for; see above."

        log "G$groups $role slices -> $cell/slices.txt"
        ./test/scripts/itc-slices.py "$cell" | tee "$cell/slices.txt"
      done
    done

    log ""
    log "retained runs complete. Deciding the knee and deriving the efficiencies:"
    log ""
    # **The status is read out of PIPESTATUS, not off the pipeline.** `cmd | tee` reports tee's
    # status, so `|| ...` on the pipeline would never fire and an unresolved knee would be
    # announced as a completed result — the failure mode this whole stage exists to avoid.
    set +e
    ./test/scripts/itc-capacity-result.py "test/results/$out_group" \
      | tee "test/results/$out_group/capacity-result.txt"
    result_status="${PIPESTATUS[0]}"
    set -e

    log ""
    log "result -> test/results/$out_group/capacity-result.txt"
    if [ "$result_status" -ne 0 ]; then
      log "!! the result is incomplete: a knee is unresolved or a topology is missing runs. That is"
      log "!! the absence of a capacity result, not a low one — read the output above before"
      log "!! quoting anything from it, and do not promote a withheld figure."
      exit "$result_status"
    fi
    ;;

  *)
    usage
    ;;
esac
