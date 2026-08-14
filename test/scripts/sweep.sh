#!/usr/bin/env bash
#
# Run one bounded sweep of AG-Sept PR2 cells, one artifact directory each.
#
# A cell is not just "run the load generator". ag-sept-pr2.md §5.4 fixes the sequence,
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

# Grouped like the other runners (see itc-run.sh). This one defaults to its own name rather than
# to a milestone: a sweep is a general instrument, and PR2 was simply its first caller. Set
# RESULTS_GROUP when a sweep belongs to a specific milestone's evidence.
RESULTS_GROUP="${RESULTS_GROUP:-sweep}"
OUT="${OUT:-test/results/$RESULTS_GROUP/$(date -u +%Y%m%dT%H%M%SZ)}"
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

# Ready is not the same as scraping, and the difference is a whole sweep of empty evidence.
#
# `make obs-up` tolerates a failed obs-target.sh (`|| true`), so Prometheus can be up and
# healthy with no target for this job at all — the file_sd address is discovered at runtime and
# is reassigned whenever WSL restarts. Every query then *succeeds* and returns nothing: the
# exporter writes header-only CSVs, the snapshot is created, and alloca-verify reads the direct
# $METRICS scrapes rather than Prometheus, so every cell passes and the sweep exits zero having
# retained no time series whatsoever. A successful empty query is the worst shape of failure
# there is, because nothing anywhere reports an error.
PROM_JOB="${PROM_JOB:-alloca-go}"
assert_scraped() {
  local n
  n=$(curl -sfG "$PROM/api/v1/query" --data-urlencode "query=up{job=\"$PROM_JOB\"}==1" \
      | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["data"]["result"]))' 2>/dev/null) || n=0
  [ "${n:-0}" -ge 1 ] || return 1
}
assert_scraped || fail "prometheus is ready but is not scraping job '$PROM_JOB' (no up==1 series).
  The target address is discovered at runtime and 'make obs-up' tolerates a failed probe, so
  this is the state where every panel query succeeds and returns nothing. Run 'make obs-target'
  with the service running, then re-run this sweep."

mkdir -p "$OUT"
log "sweep -> $OUT"

# The port the service publishes metrics on, derived from METRICS rather than hardcoded.
metrics_port() { printf '%s' "${METRICS#*://}" | sed 's#/.*##' | awk -F: '{print $NF}'; }

SERVICE_PID=""

# Stop only the service *this script started*, by pid.
#
# Looking it up by port was wrong in two ways at once. The port was hardcoded to :9090 while
# METRICS_URL is an override, so with an override the lookup found nothing, the old service
# kept running, and the next cell's process failed to bind — silently, since the readiness
# probe then succeeded against the *old* service. The cell would measure a process the sweep
# believed it had replaced, with warm state and counters from the previous cell, and every
# correctness check would still pass. Killing by pid cannot make that mistake.
stop_service() {
  [ -n "$SERVICE_PID" ] || return 0
  kill "$SERVICE_PID" 2>/dev/null || true
  wait "$SERVICE_PID" 2>/dev/null || true
  SERVICE_PID=""
}
trap 'stop_service' EXIT

# The documented flow leaves `make dev-measured` running in another terminal, and this script
# needs that port for its own per-cell processes. Take it over once, and say so: silently
# killing a process this script did not start is exactly the kind of thing that should not
# happen quietly.
takeover_port() {
  local port existing
  port=$(metrics_port)
  existing=$(ss -ltnp 2>/dev/null | grep ":${port} " | grep -o 'pid=[0-9]*' | cut -d= -f2 | head -1 || true)
  [ -n "${existing:-}" ] || return 0
  log "stopping the service already listening on :${port} (pid $existing) — this sweep starts its own per cell"
  kill "$existing" 2>/dev/null || true
  sleep 1
}

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
      [ "$cells" -eq 1 ] && takeover_port
      DATABASE_URL="$dsn" "$SERVICE" > "$cell/service.log" 2>&1 &
      SERVICE_PID=$!
      for _ in $(seq 1 40); do curl -sf "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
      curl -sf "$BASE/healthz" >/dev/null || fail "service did not become ready; see $cell/service.log"
      # Readiness alone cannot tell this cell's service from a previous one that never died,
      # which is the failure the pid-based stop_service exists to prevent. Prove the process
      # answering is the one just started.
      kill -0 "$SERVICE_PID" 2>/dev/null \
        || fail "the service started for this cell is no longer running; see $cell/service.log"

      # The scrape target survives a service restart only if the address still resolves, and
      # each cell restarts the service. Re-checked per cell rather than once at preflight,
      # because a cell that stops being scraped half way through a sweep produces exactly the
      # empty-but-successful export the preflight exists to prevent.
      for _ in $(seq 1 20); do assert_scraped && break; sleep 1; done
      assert_scraped || fail "prometheus is not scraping this cell's service (job '$PROM_JOB');
  its panel CSVs would be header-only while every other check passed. See $cell/service.log"

      # 3. warm up. Its report is kept — a warm-up that failed explains a strange cell — and
      #    its failure now refuses the cell: a cell reported as warmed when its warm-up did not
      #    run has different pool, heap and runtime state from every other cell in the sweep,
      #    which is precisely the variable warm-up exists to hold constant.
      "$LOAD" -workload "$workload" -concurrency "$conc" -duration "$WARMUP" \
        -slots "$slots" -org "$ORG" -out "$cell/warmup.json" > "$cell/warmup.log" 2>&1 \
        && warmup_ok=yes || warmup_ok=no
      [ "$warmup_ok" = yes ] || log "  warm-up FAILED; see $cell/warmup.log"

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
      #
      # Export failure refuses the cell. It used to only log: a cell whose panel CSVs or TSDB
      # snapshot were missing still counted as passed, and the sweep still exited zero, so the
      # run advertised measurements whose supporting evidence had not been retained. That is
      # the one failure this whole directory structure exists to prevent.
      PROM_URL="$PROM" ./test/scripts/export-panels.sh "$cell" "$start" "$end" \
        > "$cell/export.log" 2>&1 && export_ok=yes || export_ok=no
      [ "$export_ok" = yes ] || log "  export FAILED; see $cell/export.log"

      DATABASE_URL="$dsn" go run ./cmd/alloca-verify \
        -run "$cell/run.json" -metrics "$cell/metrics.txt" \
        -metrics-baseline "$cell/metrics-baseline.txt" \
        -org "$ORG" -out "$cell/verdict.json" > "$cell/verify.log" 2>&1 \
        && verify_ok=yes || verify_ok=no

      # The plateau check of §5.5. A cell whose goodput equals the fixture's capacity spent
      # part of its window measuring refusals, and nothing else in the pipeline notices —
      # every correctness check passes, because a refusal is a valid domain answer.
      python3 - "$cell" "$slots" "$CAPACITY" "$load_ok" "$verify_ok" "$export_ok" "$warmup_ok" <<'PY'
import json, os, sys
cell, slots, capacity, load_ok, verify_ok, export_ok, warmup_ok = sys.argv[1:8]
r = json.load(open(f"{cell}/run.json"))
s = r["summary"]
goodput, window = s["successful_mutation_goodput"], s["duration_seconds"]
fixture = int(slots) * int(capacity)

# refusals are the reasons this cell may not be read as a measurement. Recorded in the cell's
# own artifact, not only in the terminal, so a directory inspected later still says why.
refusals = []
if load_ok != "yes":
    refusals.append("the load generator failed")
if verify_ok != "yes":
    refusals.append("reconciliation failed")
if warmup_ok != "yes":
    refusals.append("the warm-up run failed, so this cell is not comparable to a warmed one")
if export_ok != "yes":
    refusals.append("evidence export failed: panel CSVs or the TSDB snapshot are missing")

# Fixture exhaustion refuses the cell rather than annotating it. The surrounding comment always
# said a cell that ran out of fixture "measures refusal throughput while every correctness check
# still passes" — and then the pass condition read only load_ok and verify_ok, so the cell was
# counted as passed anyway and the sweep exited zero. A measurement identified as measuring the
# wrong thing is not a weaker result; it is not a result.
if goodput >= fixture:
    refusals.append(f"EXHAUSTED FIXTURE: goodput {goodput} reached capacity {fixture}; part of "
                    f"the window measured refusals, not bookings")

# The export can fail *partially* — a query returning nothing still writes its file. Check the
# panels the index claims, so a truncated export cannot pass as a complete one.
index = f"{cell}/panels/index.json"
if export_ok == "yes":
    if not os.path.exists(index):
        refusals.append("evidence export reported success but wrote no panels/index.json")
    else:
        idx = json.load(open(index))
        missing = [p["key"] for p in idx["panels"] if not os.path.exists(f"{cell}/{p['file']}")]
        if missing:
            refusals.append(f"evidence export is incomplete: no CSV for {sorted(missing)}")
        # A file existing is not evidence existing. When Prometheus is up but not scraping this
        # job, every query succeeds and returns nothing, and the exporter writes a header and
        # no rows — which passes an existence check while retaining no time series at all.
        # These three panels have data in any cell whose service was scraped, whatever the
        # workload did; replay_rate legitimately has none outside the replay control, so the
        # rule names the panels that must be populated rather than forbidding empty ones.
        points = {p["key"]: p["points"] for p in idx["panels"]}
        empty = [k for k in ("throughput", "pool_max", "process_cpu") if points.get(k, 0) == 0]
        if empty:
            refusals.append(f"evidence export retained no samples for {sorted(empty)}: "
                            f"Prometheus was almost certainly not scraping this cell's service")
    snapshot = f"{cell}/tsdb-snapshot"
    if not os.path.isdir(snapshot) or not os.listdir(snapshot):
        refusals.append("evidence export left no TSDB snapshot")

json.dump({
    "workload": s["workload"], "concurrency": s["concurrency"],
    "window_seconds": window,
    "throughput_per_s": round(s["completed_requests"] / window, 1),
    "goodput_per_s": round(goodput / window, 1),
    "latency_ms": s["latency_ms"],
    "generator_cpu_per_core": s["generator"]["cpu_utilisation_per_core"],
    "level": r["quotability"]["level"],
    "load_ok": load_ok == "yes", "verify_ok": verify_ok == "yes",
    "export_ok": export_ok == "yes",
    "fixture_units": fixture,
    "cell_ok": not refusals,
    "refusals": refusals,
    # notes is retained as the previous key name so older readers of a cell directory keep
    # working; it now carries exactly the refusal reasons.
    "notes": refusals,
}, open(f"{cell}/cell.json", "w"), indent=2)
for n in refusals:
    print(f"  !! {n}")
PY

      cell_ok=$(python3 -c 'import json,sys;print("yes" if json.load(open(sys.argv[1]))["cell_ok"] else "no")' "$cell/cell.json")
      if [ "$cell_ok" = yes ]; then
        passed=$((passed+1)); log "  ok"
      else
        refused=$((refused+1)); log "  REFUSED (load=$load_ok verify=$verify_ok export=$export_ok)"
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
