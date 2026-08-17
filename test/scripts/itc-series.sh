#!/usr/bin/env bash
#
# Drive a complete repeat series from whatever state the machine is in: tear down what is
# running, raise the named rung with its matching partition and scrape set, and hand off to
# itc-repeat.sh.
#
#   ./test/scripts/itc-series.sh 1 10      # G1, ten cells
#   ./test/scripts/itc-series.sh 4 10      # G4, ten cells
#   ./test/scripts/itc-series.sh 1         # G1, REPEATS default
#
# **The division of labour.** itc-run.sh owns one cell, itc-repeat.sh owns a population of cells,
# and this script owns the environment they run in. Nothing here changes how a cell is driven.
#
# **Why a wrapper rather than the eight commands.** The sequence has an order that is not
# self-evident and two couplings nothing else enforces. The monitoring stack must be raised after
# the topology, because obs-rehearse generates its scrape targets from the group count and a
# stack raised first describes units that do not exist yet. And ITC_CPUS_GENERATOR has to reach
# both obs-rehearse and the run: it confines Prometheus and Grafana in one and the generator in
# the other, and passing it to only one leaves half the measuring side on the units' own CPUs
# while every cpuset check, topology check and certification still passes (ag-sept-pr4.md §2.14).
# Typing the sequence by hand is how a series ends up comparing two different partitions.
#
# Everything itc-repeat.sh and itc-run.sh read still travels through the environment — WORKLOAD,
# CONCURRENCY, WINDOW, SLOTS, CAPACITY, REQUIRE, GAP, RESULTS_GROUP, PROM_URL:
#
#   WINDOW=120s ./test/scripts/itc-series.sh 1 10
#
# The group count and the cell count are arguments instead, because they are the two that must
# agree with the topology this script raises rather than being free knobs on top of it.

set -euo pipefail

cd "$(dirname "$0")/../.." || exit 1

log()  { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage: ./test/scripts/itc-series.sh <groups> [repeats]

  groups   1, 2 or 4 — the shard-group count (ag-sept-validation-plan.md §4.6)
  repeats  cells in the series (default 10)

Every other knob is an environment variable passed through to itc-repeat.sh and
itc-run.sh unchanged, e.g. WINDOW=120s GAP=30 RESULTS_GROUP=pr4b
EOF
  exit 2
}

ITC_GROUPS="${1:-}"
REPEATS="${2:-${REPEATS:-10}}"

[ -n "$ITC_GROUPS" ] || usage

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "groups must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$ITC_GROUPS'" >&2
     usage ;;
esac

case "$REPEATS" in
  ''|*[!0-9]*) echo "repeats must be a positive integer; got '$REPEATS'" >&2; usage ;;
esac
[ "$REPEATS" -ge 1 ] || { echo "repeats must be at least 1; got '$REPEATS'" >&2; usage; }

# Named here rather than left to each command's own default, so the one value reaches both halves
# of the measuring side. See the header.
ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"
export ITC_CPUS_GENERATOR

# The series directory is chosen here, not by itc-repeat.sh, because this script needs to know it
# in advance: the database logs are retained into it after the run completes.
RESULTS_GROUP="${RESULTS_GROUP:-pr4a}"
SERIES="${SERIES:-test/results/$RESULTS_GROUP/repeat-$(date -u +%Y%m%dT%H%M%SZ)}"
export RESULTS_GROUP SERIES

# `${PROM_URL-...}`, not `${PROM_URL:-...}`, so an explicitly empty PROM_URL disables monitoring
# here exactly as it does in itc-run.sh rather than being silently replaced by the default.
PROM_URL="${PROM_URL-http://localhost:9091}"
PROM_JOB="${PROM_JOB:-alloca-go}"
export PROM_URL PROM_JOB

# How long to let the scrape stack converge before giving up. Generous against the two intervals
# that bound it — a 10s file_sd refresh_interval and a 1s scrape_interval — because the cost of
# waiting is seconds and the cost of not waiting is the whole series.
PROM_SETTLE="${PROM_SETTLE:-60}"

# Opt-in diagnostic: log every autovacuum, however short.
#
#   PG_LOG_AUTOVACUUM=1 ./test/scripts/itc-series.sh 1 10
#
# PostgreSQL defaults log_autovacuum_min_duration to 10 minutes, so an ordinary autovacuum on
# this fixture leaves no trace whatever — which is why autovacuum has stayed unobserved through
# every series so far while checkpoints, on by default, were visible throughout.
#
# Off unless asked for, and turning it off again is not an operation: it is a container start-up
# argument, so it exists only for the life of the containers this script raises and tears down.
PG_LOG_AUTOVACUUM="${PG_LOG_AUTOVACUUM:-0}"
ALLOCA_PG_ARGS="${ALLOCA_PG_ARGS:-}"
if [ "$PG_LOG_AUTOVACUUM" = "1" ]; then
  ALLOCA_PG_ARGS="${ALLOCA_PG_ARGS:+$ALLOCA_PG_ARGS }-c log_autovacuum_min_duration=0"
fi

# Statement-level attribution (§3.17). `shared_preload_libraries` is why this belongs here rather
# than in itc-run.sh: it can only be set at server start, so the topology has to be raised with it
# already in place. The extension itself is created after the units are up, below.
#
# Diagnostic only. It costs a few percent on the database under test, so a canonical measurement
# must not carry it — the same rule as ANALYZE_AFTER_SEED, and for the same reason.
PG_STAT_STATEMENTS="${PG_STAT_STATEMENTS:-0}"
if [ "$PG_STAT_STATEMENTS" = "1" ]; then
  ALLOCA_PG_ARGS="${ALLOCA_PG_ARGS:+$ALLOCA_PG_ARGS }-c shared_preload_libraries=pg_stat_statements"
fi
export ALLOCA_PG_ARGS PG_STAT_STATEMENTS

DEPLOYMENT=test/observed/deployment.json

# ---------------------------------------------------------------------------
# Everything that can refuse the series runs before anything is destroyed.
#
# Both checks below also run later — itc-run.sh re-checks the tree, itc-rehearse re-checks the
# partition before it builds. Doing them here as well is not redundant: by the time those fire,
# this script has already torn down the topology and its volumes, so a refusal that could have
# cost nothing instead costs a rebuild and a reseed. Failing before the teardown is the whole
# reason they appear twice.
# ---------------------------------------------------------------------------

# Go stamps an *untracked* file as a modified build, so any stray file refuses every cell at
# level `none` for source_modified — after its window has been driven (ag-sept-pr4.md §3.6).
# `make image-provenance` refuses outright on a dirty tree, where modified=true is the correct
# answer and the control could prove nothing.
#
# This includes the first run of this script itself, while it is still untracked. That is the
# correct answer and not a bootstrapping problem: a measured series must be reproducible from a
# commit, and a wrapper that existed only in the working tree could not be one.
dirty="$(git status --porcelain)"
if [ -n "$dirty" ]; then
  printf '%s\n' "$dirty" >&2
  fail "the working tree is not clean, so every cell would be refused at level none.
  Commit or stash the files listed above — untracked ones count."
fi

log "checking the CPU partition for G$ITC_GROUPS before tearing anything down"
ITC_GROUPS="$ITC_GROUPS" ITC_CPUS_GENERATOR="$ITC_CPUS_GENERATOR" \
  ./test/scripts/itc-cpu-layout.sh \
  || fail "the CPU partition is not usable on this machine; fix it before starting a series"

# ---------------------------------------------------------------------------
# Teardown
# ---------------------------------------------------------------------------

# **Retain the databases' logs before removing them, because nothing else records what Postgres
# did.** The service exports its own metrics and Prometheus keeps them; Postgres, running on the
# same cpuset as the service it serves, writes only to its container log, and `itc-down -v`
# destroys it along with the volume. Those logs are what refuted checkpoints as the cause of the
# 2026-08-16 G4 excursions — evidence that survived only because the containers happened to be
# stopped rather than removed. A teardown step turns that piece of luck into a guarantee of loss,
# so the previous series' logs are salvaged into their own directory first.
#
# Best-effort throughout: there is usually nothing to salvage, and a fresh machine must not be
# blocked by the absence of a previous run.
salvage="test/results/$RESULTS_GROUP/salvaged-$(date -u +%Y%m%dT%H%M%SZ)"
salvaged=0
for n in 1 2 3 4; do
  container="alloca-authority-${n}-db"
  docker inspect "$container" >/dev/null 2>&1 || continue
  mkdir -p "$salvage"
  if docker logs "$container" > "$salvage/${container}.log" 2>&1 \
     && [ -s "$salvage/${container}.log" ]; then
    salvaged=$((salvaged + 1))
  else
    rm -f "$salvage/${container}.log"
  fi
done
if [ "$salvaged" -gt 0 ]; then
  log "salvaged $salvaged database log(s) from the previous topology -> $salvage"
else
  rm -rf "$salvage"
fi

# `itc-down` removes every profile's units and their volumes, so a G4 topology cannot leave units
# alive to compete with a G1 series — the failure the topology check exists to catch, caught here
# instead by not creating it.
log "tearing down the topology"
make itc-down

# `obs-down` deliberately keeps the Prometheus volume: a previous series' evidence is snapshotted
# out of it and tearing it down with the containers would discard a half-finished analysis. Do
# not "fix" this into `down -v`. A stale TSDB is harmless to a new series, which queries its own
# window.
log "tearing down the monitoring stack (its retained TSDB survives on purpose)"
make obs-down

# ---------------------------------------------------------------------------
# Raise
# ---------------------------------------------------------------------------

log "proving the image's provenance stamp"
make image-provenance

log "raising the G$ITC_GROUPS topology, pinned"
make itc-rehearse ITC_GROUPS="$ITC_GROUPS"

# **Record the settings the databases actually started with, and refuse if a requested diagnostic
# did not take.** Without this a null result is uninterpretable: "no autovacuum appears in the
# log" would mean either that none ran or that logging was never enabled, and those demand
# opposite conclusions. Reading it back from pg_settings rather than trusting the argument we
# passed is the whole point — the argument is what we asked for, pg_settings is what happened.
#
# Recorded unconditionally, not only under the diagnostic, because a series should say what
# configuration produced it without anyone having to remember.
mkdir -p "$SERIES"
pg_settings_file="$SERIES/postgres-settings.txt"
: > "$pg_settings_file"
for n in $(seq 1 "$ITC_GROUPS"); do
  container="alloca-authority-${n}-db"
  observed="$(docker exec "$container" psql -U alloca -d alloca -tAc \
    "SELECT name||'='||setting||coalesce(' '||unit,'') FROM pg_settings WHERE name IN
     ('autovacuum','autovacuum_naptime','log_autovacuum_min_duration',
      'checkpoint_timeout','checkpoint_completion_target','max_wal_size')
     ORDER BY name" 2>/dev/null)" || observed=""
  [ -n "$observed" ] \
    || fail "could not read pg_settings from $container, so the series would not be able to say
  what configuration produced it"
  printf '%s\n%s\n\n' "$container" "$observed" >> "$pg_settings_file"

  if [ "$PG_LOG_AUTOVACUUM" = "1" ] \
     && ! printf '%s\n' "$observed" | grep -q '^log_autovacuum_min_duration=0'; then
    fail "PG_LOG_AUTOVACUUM=1 was requested but $container reports
  $(printf '%s\n' "$observed" | grep '^log_autovacuum_min_duration')
  The run would produce a log with no autovacuum lines in it and no way to tell that from a
  database that never vacuumed. Check that ALLOCA_PG_ARGS reached compose."
  fi
done
if [ "$PG_STAT_STATEMENTS" = "1" ]; then
  for n in $(seq 1 "$ITC_GROUPS"); do
    container="alloca-authority-${n}-db"
    docker exec "$container" psql -U alloca -d alloca -qc \
      "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;" >/dev/null 2>&1 \
      || fail "could not create pg_stat_statements on $container"
    # Created is not the same as loaded: without the preload the CREATE succeeds and every query
    # against the view then fails at run time, one cell into the series. Reading the view is the
    # only check that covers both halves.
    docker exec "$container" psql -U alloca -d alloca -qtAc \
      "SELECT count(*) FROM pg_stat_statements;" >/dev/null 2>&1 \
      || fail "pg_stat_statements exists on $container but cannot be read, which means the library
  was not preloaded. The topology must be raised with the flag, not have it added afterwards."
  done
  log "pg_stat_statements active on $ITC_GROUPS database(s) (diagnostic; costs a few percent)"
fi

log "database settings recorded -> $pg_settings_file"
# An `if`, not `[ ... ] && log ...`: under `set -e` a false test as the final command of a list
# is a non-zero status, which would abort every run that did not ask for the diagnostic — the
# default path.
if [ "$PG_LOG_AUTOVACUUM" = "1" ]; then
  log "autovacuum logging is ON for this series (every vacuum, regardless of duration)"
fi

log "raising the monitoring stack on CPUs $ITC_CPUS_GENERATOR"
make obs-rehearse ITC_GROUPS="$ITC_GROUPS" ITC_CPUS_GENERATOR="$ITC_CPUS_GENERATOR"

# **Prometheus is not scraping by the time obs-rehearse returns, and the first cell paid for it.**
# The target file is written before the container starts, so Prometheus normally picks it up
# during start-up — but it still has to boot, load its configuration and complete a scrape, and
# file_sd only re-reads the directory on a 10s refresh_interval if it missed the file. A cell
# driven one second later is refused against an empty target set, and because a first-cell failure
# is treated as environmental rather than a flake, the entire series stops.
#
# It stayed invisible until an image build and a Go build were both warm: the interval between
# raising the stack and driving cell 1 collapsed from minutes to under a second, and only then was
# it shorter than Prometheus takes to answer. A fixed sleep would have hidden it again on the next
# machine, so this waits for the observable condition instead.
#
# **A liveness wait, not a gate.** It asks whether this rung is being scraped *yet*. itc-run.sh
# asks whether *exactly* this rung is being scraped — a different question, and one that stays
# that script's alone. In particular the FOREIGN-target refusal is deliberately not reimplemented
# here: duplicating it would create a second definition of a correctness rule that must not drift,
# and a contaminated scrape set should be reported by the gate that owns the explanation.
#
# **Both jobs, not just the service one.** The postgres exporters are raised with the same stack
# and discovered through the same file_sd refresh, so they are usually healthy at the same moment —
# usually is not a property. itc-run.sh's populated-series gate requires the *host* panels and says
# nothing about the database ones, so an exporter still starting when cell 1 opens produces a cell
# with empty PostgreSQL panels and nothing anywhere reports it. That is the §3.9 failure shape
# exactly, on the evidence path added to answer the question the whole series exists for.
healthy_targets() {
  # $1 is the job. Captured into a variable and emitted once, rather than letting the pipeline
  # write straight to stdout with `|| echo 0` appended.
  #
  # **That shape emits twice.** Until Prometheus is listening curl exits 7, but python has already
  # run on empty stdin and printed its own 0; `pipefail` then fails the pipeline and the fallback
  # prints a second one. The caller receives "0\n0" and dies on `[: integer expression expected`,
  # which under `set -e` aborts the run in exactly the situation this wait exists to survive. The
  # single-query version this replaced avoided it by assigning, and the bug came back the moment
  # the query became a function — so the fallback assigns here too.
  local answered
  answered="$(curl -sfG -m 5 "$PROM_URL/api/v1/query" \
      --data-urlencode "query=count(up{job=\"$1\",topology=\"itc-g${ITC_GROUPS}\"} == 1)" \
      2>/dev/null \
    | python3 -c 'import json,sys
# An unmatched count() returns an empty result rather than a zero, and a Prometheus still booting
# returns nothing at all. Both mean "not yet", so both print 0 rather than raising.
try:
    result = json.load(sys.stdin)["data"]["result"]
    print(int(float(result[0]["value"][1])) if result else 0)
except Exception:
    print(0)' 2>/dev/null)" || answered=0
  printf '%s\n' "${answered:-0}"
}

if [ -n "$PROM_URL" ]; then
  log "waiting up to ${PROM_SETTLE}s for prometheus to scrape G$ITC_GROUPS (service and database)"
  settled=0
  for _ in $(seq 1 "$PROM_SETTLE"); do
    healthy="$(healthy_targets "$PROM_JOB")"
    healthy_pg="$(healthy_targets postgres)"
    if [ "${healthy:-0}" -ge "$ITC_GROUPS" ] && [ "${healthy_pg:-0}" -ge "$ITC_GROUPS" ]; then
      settled=1
      break
    fi
    sleep 1
  done
  [ "$settled" -eq 1 ] || fail "prometheus did not report $ITC_GROUPS healthy target(s) on both
  jobs for topology itc-g$ITC_GROUPS within ${PROM_SETTLE}s (service: ${healthy:-0}, postgres:
  ${healthy_pg:-0}). Cell 1 would either be refused by the scrape gate or retain no database
  panels at all, and only the first of those reports itself:

      curl -s $PROM_URL/api/v1/targets | grep -o '\"health\":\"[a-z]*\"'
      ITC_GROUPS=$ITC_GROUPS ./test/scripts/itc-obs-targets.sh"
  log "prometheus is scraping $ITC_GROUPS unit(s)"
fi

# Written through a temporary file. `make itc-deployment > deployment.json` truncates the target
# before the recipe runs, so a failed recording leaves an empty file that looks exactly like a
# real one to the next run — and alloca-load reads it to check it is addressing the units the
# record describes.
log "recording what the containers are actually serving"
make itc-deployment ITC_GROUPS="$ITC_GROUPS" > "$DEPLOYMENT.tmp"
mv "$DEPLOYMENT.tmp" "$DEPLOYMENT"

# `go run` stamps no VCS data, so the generator is built rather than run from source.
log "building the generator"
go build -o bin/alloca-load ./cmd/alloca-load

# ---------------------------------------------------------------------------
# Drive
# ---------------------------------------------------------------------------

log "driving $REPEATS cell(s) at G$ITC_GROUPS -> $SERIES"
echo

# `set +e` around the series: itc-repeat.sh exits non-zero when any cell failed, and a partly
# failed series still has cells worth keeping and logs worth retaining. The status is carried to
# the end rather than dropped.
set +e
ITC_GROUPS="$ITC_GROUPS" REPEATS="$REPEATS" ./test/scripts/itc-repeat.sh
series_status=$?
set -e

# The counterpart to the salvage above: retain this series' database logs into the series itself,
# while its containers are still alive. Warned about rather than fatal — the cells are the
# result, and losing the supporting logs must not turn a completed series into a failed one.
mkdir -p "$SERIES/postgres"
retained=0
for n in $(seq 1 "$ITC_GROUPS"); do
  container="alloca-authority-${n}-db"
  if docker logs "$container" > "$SERIES/postgres/${container}.log" 2>&1 \
     && [ -s "$SERIES/postgres/${container}.log" ]; then
    retained=$((retained + 1))
  else
    rm -f "$SERIES/postgres/${container}.log"
  fi
done
if [ "$retained" -eq "$ITC_GROUPS" ]; then
  log "retained $retained database log(s) -> $SERIES/postgres/"
else
  printf '!! retained only %d of %d database logs into %s/postgres/ (docker ps -a)\n' \
    "$retained" "$ITC_GROUPS" "$SERIES" >&2
fi

echo
log "series directory: $SERIES"

# Left running on purpose. Reading a series means going back to the units — their logs, their
# metrics, the Grafana view over the window — and the next invocation of this script tears down
# whatever it finds anyway.
log "the topology and monitoring stack are still up; the next run of this script tears them down"

exit "$series_status"
