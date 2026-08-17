#!/usr/bin/env bash
#
# Drive N identical rehearsal cells back to back, retaining every one as its own artifact.
#
# **Why a population, and not another cell.** Every statement this repository makes about the
# degraded regime rests on one or two runs. Two nominally identical G4 cells — same fixture, same
# concurrency, same service binary — came out at 3,115/s flat and 2,015/s with a 2x within-window
# excursion (ag-sept-pr4.md §3.12, §3.13). One reading cannot separate a regime from the tail of a
# distribution, and it cannot tell a rare fault from a noisy environment. A ladder is worse off
# still: PR4b selects an operating point by claiming a higher rung produced no more sustained
# Goodput, which is unanswerable while nominally identical rungs disagree by half.
#
# So this script produces the base rate. It changes nothing about how a cell is driven — every
# cell is one ordinary `itc-run.sh` invocation with its own output directory, and every gate,
# reseed and refusal behaves exactly as it does for a single cell.
#
#   ITC_GROUPS=4 REPEATS=10 ./test/scripts/itc-repeat.sh
#
# Raise the topology and monitoring first, the same way a single cell needs them:
#
#   make itc-rehearse ITC_GROUPS=4
#   make obs-rehearse ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-11
#   make itc-deployment ITC_GROUPS=4 > test/observed/deployment.json
#   go build -o bin/alloca-load ./cmd/alloca-load
#
# Every variable itc-run.sh reads is passed through untouched — WORKLOAD, CONCURRENCY, WINDOW,
# SLOTS, CAPACITY, REQUIRE, PROM_URL, the CPU sets. `OUT` is the one it does not honour, because
# the series owns the layout: cells land in one directory so they are read, promoted and deleted
# together.

set -uo pipefail

REPEATS="${REPEATS:-10}"
ITC_GROUPS="${ITC_GROUPS:-4}"
RESULTS_GROUP="${RESULTS_GROUP:-pr4a}"

# Between cells, not inside one. A gap lets the units settle and makes each cell's baseline scrape
# describe a quiet machine rather than the tail of the previous window — but it is deliberately
# short, because whatever accumulates across a session (checkpoints, autovacuum, page cache) is a
# live hypothesis for the excursion and a long gap would be an uncontrolled intervention on it.
GAP="${GAP:-10}"

SERIES="${SERIES:-test/results/$RESULTS_GROUP/repeat-$(date -u +%Y%m%dT%H%M%SZ)}"

log() { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }

[ -d .git ] || { echo "run this from the repository root" >&2; exit 1; }

mkdir -p "$SERIES"

# The series is self-describing, for the same reason a cell is: a directory of ten cells whose
# shared configuration lives only in someone's shell history cannot be read six weeks later.
cat > "$SERIES/series.txt" <<EOF
started=$(date -u +%Y-%m-%dT%H:%M:%SZ)
repeats=$REPEATS
gap_seconds=$GAP
ITC_GROUPS=$ITC_GROUPS
WORKLOAD=${WORKLOAD:-wl-mut-disp-4}
CONCURRENCY=${CONCURRENCY:-16}
WINDOW=${WINDOW:-60s}
SLOTS=${SLOTS:-3200}
CAPACITY=${CAPACITY:-20}
ITC_CPUS_GENERATOR=${ITC_CPUS_GENERATOR:-8-11}
analyze_after_seed=${ANALYZE_AFTER_SEED:-0}
revision=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
tree_clean=$([ -z "$(git status --porcelain 2>/dev/null)" ] && echo yes || echo no)
EOF

log "series -> $SERIES  ($REPEATS cells, gap ${GAP}s)"
echo

passed=0
failed=0
consecutive=0
series_start=$SECONDS

for i in $(seq 1 "$REPEATS"); do
  cell="$SERIES/cell-$(printf '%02d' "$i")"
  cell_start=$SECONDS

  # The runner's own transcript lives inside the cell, so a cell promoted into docs/measurements/
  # carries the record of how it was driven — including the preflight, which is where a refused
  # cell says why. `.txt`, not `.log`: .gitignore excludes `*.log`, so a promoted cell would cite
  # a file the repository does not carry.
  mkdir -p "$cell"

  log "cell $i/$REPEATS -> $cell"
  OUT="$cell" ITC_GROUPS="$ITC_GROUPS" ./test/scripts/itc-run.sh > "$cell/runner.txt" 2>&1
  status=$?

  elapsed=$((SECONDS - cell_start))

  if [ "$status" -eq 0 ]; then
    passed=$((passed + 1))
    consecutive=0
    admitted="$(python3 -c '
import json, sys
try:
    t = json.load(open(sys.argv[1]))["summary"]
    print("%d admitted in %.1fs" % (t["successful_mutation_goodput"], t["duration_seconds"]))
except Exception:
    print("unreadable")' "$cell/run.json" 2>/dev/null)"
    log "  ok (${elapsed}s): $admitted"
  else
    failed=$((failed + 1))
    consecutive=$((consecutive + 1))
    log "  FAILED (${elapsed}s), exit $status — see $cell/runner.txt"
    tail -3 "$cell/runner.txt" | sed 's/^/    /'

    # A first-cell failure is almost always environmental — a dirty tree, a stale topology, an
    # unpinned container, a Prometheus that is not scraping this rung. Every gate that produces it
    # runs before the fixture is touched, so nothing has been measured and there is nothing to
    # salvage by continuing; ten identical refusals would only cost the operator ten minutes to
    # learn the same thing once.
    if [ "$i" -eq 1 ]; then
      echo
      echo "!! the first cell failed, so the series is stopping. Every check that refuses a cell" >&2
      echo "   before its window runs is environmental rather than a flake — fix it and start" >&2
      echo "   again. $cell/runner.txt names which one." >&2
      exit 1
    fi

    # After that, an isolated failure is tolerated: the series is measuring variability, and a
    # cell lost to a transient is a gap in the sample rather than a reason to discard the sample.
    # Two in a row is not a transient.
    if [ "$consecutive" -ge 2 ]; then
      echo
      echo "!! two consecutive cells failed, which is an environment that has changed rather than" >&2
      echo "   a flake. Stopping with $passed cell(s) retained in $SERIES." >&2
      exit 1
    fi
  fi

  if [ "$i" -lt "$REPEATS" ] && [ "$GAP" -gt 0 ]; then
    sleep "$GAP"
  fi
done

echo
log "series complete: $passed passed, $failed failed, $(( (SECONDS - series_start) / 60 ))m total"
log "$SERIES"
echo

# The summary is the point of the series, so it is printed rather than left as a second command
# the operator has to know about. It is also runnable on its own over any set of cells, including
# the ones already retained under docs/measurements/.
if [ -x ./test/scripts/itc-classify.py ] || [ -f ./test/scripts/itc-classify.py ]; then
  python3 ./test/scripts/itc-classify.py "$SERIES"/cell-*/ || true
fi

[ "$failed" -eq 0 ]
