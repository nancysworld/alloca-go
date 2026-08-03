#!/usr/bin/env bash
#
# Run one bounded sweep of AG-Sept PR2 cells, one artifact directory each.
#
# A cell is not just "run the load generator". ag-sept-pr2-scope.md §5.4 fixes the sequence,
# and every step in it exists because of a failure the harness has already met:
#
#   seed -> restart service -> warm up -> reset fixture (service keeps running)
#        -> baseline scrape -> measured load -> after scrape -> export -> verify
#
#   * the service restarts *between* cells, not within one, so each cell starts from a known
#     process; the fixture reset in the middle is what keeps warm-up traffic out of
#     reconciliation without discarding the warm pool a restart would;
#   * the two scrapes bracket the measured phase, because counters are cumulative and the
#     service is deliberately still warm — alloca-verify compares the delta (§5.4);
#   * seeding happens twice per cell, which is cheap (150k units in ~2.3s) and is the only
#     thing standing between a warmed cell and a reconciliation failure on a correct service.
#
# Hand-driving this is what the script exists to prevent: PR1's operator guide records how one
# missed restart produced a scrape that looked like a service fault, and a sweep repeats every
# opportunity for that dozens of times.
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL}"

OUT="${OUT:-test/results/sweep-$(date -u +%Y%m%dT%H%M%SZ)}"
WORKLOADS="${WORKLOADS:-dispersed hot-slot hot-identity}"
CONCURRENCIES="${CONCURRENCIES:-4 8 16 32}"
POOLS="${POOLS:-}"                       # empty = whatever DATABASE_URL already says
WINDOW="${WINDOW:-60s}"
WARMUP="${WARMUP:-20s}"
CAPACITY="${CAPACITY:-50}"
ORG="${ORG:-load-org}"
PROM="${PROM_URL:-http://localhost:9091}"
METRICS="${METRICS_URL:-http://localhost:9090/metrics}"
BASE="${BASE:-http://localhost:8080}"

LOAD=bin/alloca-load
SERVICE=bin/alloca-go

log() { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

# --- preflight -------------------------------------------------------------------------
# Checked up front rather than discovered on cell 17 of 24, an hour in.
[ -x "$LOAD" ] && [ -x "$SERVICE" ] || fail "build first: go build -o bin/alloca-load ./cmd/alloca-load && go build -o bin/alloca-go ./cmd/alloca-go"
# `git status --porcelain`, not `git diff`, because Go's vcs.modified stamp counts *untracked*
# files as a modified tree — verified, not assumed. A `git diff`-based check passes preflight
# and then every cell is refused at level none for source_modified, an hour into the sweep.
[ -z "$(git status --porcelain)" ] || fail "working tree is not clean (git status --porcelain is non-empty, and Go stamps untracked files as a modified tree): every cell would be refused at level none, so the sweep would produce nothing quotable"
curl -sf "$PROM/-/ready" >/dev/null || fail "prometheus is not ready at $PROM — run 'make obs-up'"

mkdir -p "$OUT"
log "sweep -> $OUT"

stop_service() {
  local pid
  pid=$(ss -ltnp 2>/dev/null | grep ':9090' | grep -o 'pid=[0-9]*' | cut -d= -f2 | head -1) || true
  [ -n "${pid:-}" ] && kill "$pid" 2>/dev/null && sleep 1 || true
}
trap 'stop_service' EXIT

# slots_for sizes the fixture to the cell rather than to a constant.
#
# This is the trap §5.5 records: a duration-bounded cell exhausts any fixture not sized to
# window x throughput, and once it does, the rest of the window measures refusal throughput
# while every correctness check still passes. Over-provision generously — seeding is cheap and
# a too-small fixture silently changes what the cell measured.
slots_for() {
  local conc=$1 window_s=$2
  # Assume up to ~400 admitted units per concurrent worker per second, then double it. The
  # figure only has to be an over-estimate; the plateau check below is what actually catches
  # a fixture that ran out.
  local units=$(( conc * 400 * window_s * 2 ))
  echo $(( units / CAPACITY + 100 ))
}

secs() { python3 -c "import sys,re;s=sys.argv[1];m=re.fullmatch(r'(\d+)(ms|s|m)',s);v=int(m.group(1));print({'ms':v//1000,'s':v,'m':v*60}[m.group(2)])" "$1"; }

cells=0; passed=0; refused=0
WINDOW_S=$(secs "$WINDOW")

for workload in $WORKLOADS; do
  for conc in $CONCURRENCIES; do
    for pool in ${POOLS:-default}; do
      cells=$((cells+1))
      cell="$OUT/${workload}-c${conc}-pool${pool}"
      mkdir -p "$cell"

      dsn="$DATABASE_URL"
      [ "$pool" != "default" ] && dsn="${DATABASE_URL}&pool_max_conns=${pool}"

      slots=$(slots_for "$conc" "$WINDOW_S")
      log "cell $cells: $workload c=$conc pool=$pool slots=$slots"

      # 1. fixture, and 2. a fresh process so the cell starts from a known service.
      DATABASE_URL="$dsn" go run ./cmd/alloca-seed -reset -slots "$slots" -capacity "$CAPACITY" \
        > "$cell/seed.log" 2>&1 || fail "seed failed; see $cell/seed.log"
      stop_service
      DATABASE_URL="$dsn" "$SERVICE" > "$cell/service.log" 2>&1 &
      for _ in $(seq 1 40); do curl -sf "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
      curl -sf "$BASE/healthz" >/dev/null || fail "service did not become ready; see $cell/service.log"

      # 3. warm up. Its report is kept — a warm-up that failed explains a strange cell.
      "$LOAD" -workload "$workload" -concurrency "$conc" -duration "$WARMUP" \
        -slots "$slots" -org "$ORG" -out "$cell/warmup.json" > "$cell/warmup.log" 2>&1 || true

      # 4. reset the fixture, leaving the service running: the pool and heap stay warm, and
      #    the rows warm-up created never reach reconciliation.
      DATABASE_URL="$dsn" go run ./cmd/alloca-seed -reset -slots "$slots" -capacity "$CAPACITY" \
        > "$cell/reseed.log" 2>&1 || fail "re-seed failed; see $cell/reseed.log"

      # 5. bracket the measured phase.
      curl -sf "$METRICS" > "$cell/metrics-baseline.txt" || fail "baseline scrape failed"
      start=$(date -u +%Y-%m-%dT%H:%M:%SZ)
      "$LOAD" -workload "$workload" -concurrency "$conc" -duration "$WINDOW" \
        -slots "$slots" -org "$ORG" -out "$cell/run.json" > "$cell/load.log" 2>&1 \
        && load_ok=yes || load_ok=no
      end=$(date -u +%Y-%m-%dT%H:%M:%SZ)
      curl -sf "$METRICS" > "$cell/metrics.txt" || fail "after scrape failed"

      # 6. evidence, then the verdict.
      PROM_URL="$PROM" ./test/scripts/export-panels.sh "$cell" "$start" "$end" \
        > "$cell/export.log" 2>&1 || log "  export failed; see $cell/export.log"

      DATABASE_URL="$dsn" go run ./cmd/alloca-verify \
        -run "$cell/run.json" -metrics "$cell/metrics.txt" \
        -metrics-baseline "$cell/metrics-baseline.txt" \
        -org "$ORG" -out "$cell/verdict.json" > "$cell/verify.log" 2>&1 \
        && verify_ok=yes || verify_ok=no

      # The plateau check of §5.5. A cell whose goodput equals the fixture's capacity spent
      # part of its window measuring refusals, and nothing else in the pipeline notices —
      # every correctness check passes, because a refusal is a valid domain answer.
      python3 - "$cell" "$slots" "$CAPACITY" "$load_ok" "$verify_ok" <<'PY'
import json, sys
cell, slots, capacity, load_ok, verify_ok = sys.argv[1:6]
r = json.load(open(f"{cell}/run.json"))
s = r["summary"]
goodput, window = s["successful_mutation_goodput"], s["duration_seconds"]
fixture = int(slots) * int(capacity)
notes = []
if goodput >= fixture:
    notes.append(f"EXHAUSTED FIXTURE: goodput {goodput} reached capacity {fixture}; part of "
                 f"the window measured refusals, not bookings")
json.dump({
    "workload": s["workload"], "concurrency": s["concurrency"],
    "window_seconds": window,
    "throughput_per_s": round(s["completed_requests"] / window, 1),
    "goodput_per_s": round(goodput / window, 1),
    "latency_ms": s["latency_ms"],
    "generator_cpu_per_core": s["generator"]["cpu_utilisation_per_core"],
    "level": r["quotability"]["level"],
    "load_ok": load_ok == "yes", "verify_ok": verify_ok == "yes",
    "fixture_units": fixture, "notes": notes,
}, open(f"{cell}/cell.json", "w"), indent=2)
for n in notes:
    print(f"  !! {n}")
PY

      if [ "$load_ok" = yes ] && [ "$verify_ok" = yes ]; then
        passed=$((passed+1)); log "  ok"
      else
        refused=$((refused+1)); log "  REFUSED (load=$load_ok verify=$verify_ok)"
      fi
    done
  done
done

python3 - "$OUT" <<'PY'
import glob, json, sys
out = sys.argv[1]
rows = [json.load(open(f)) for f in sorted(glob.glob(f"{out}/*/cell.json"))]
json.dump(rows, open(f"{out}/sweep.json", "w"), indent=2)
print(f"\n{'workload':14} {'conc':>5} {'thru/s':>9} {'good/s':>9} {'p99ms':>8} {'gen cpu':>8}  level")
for r in rows:
    print(f"{r['workload']:14} {r['concurrency']:5} {r['throughput_per_s']:9} "
          f"{r['goodput_per_s']:9} {r['latency_ms']['p99']:8.1f} "
          f"{r['generator_cpu_per_core']:8.3f}  {r['level']}"
          + ("  <- " + r["notes"][0].split(":")[0] if r["notes"] else ""))
PY

log "sweep complete: $cells cells, $passed passed, $refused refused -> $OUT"
[ "$refused" -eq 0 ] || exit 1
