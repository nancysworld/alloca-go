#!/usr/bin/env bash
#
# ag-sept-plan §12.2, mandatory: deliberately constrain the generator and show how the
# apparent frontier changes.
#
# The control exists because a closed-loop harness cannot tell you, from its own numbers,
# whether a plateau is the service's limit or its own. Both look identical: throughput stops
# rising as concurrency increases. The only way to separate them is to change the *generator*
# and see whether the answer moves.
#
# So the same cell runs at several generator GOMAXPROCS values. If the apparent frontier is
# flat across them, the generator was not the binding constraint at that operating point and
# the service's number stands. If it tracks the constraint, the run was measuring the harness.
#
# This is a control, not a sweep: it does not seed or reconcile per cell, because what is under
# test is the harness rather than the service. Run it at the operating point a sweep identified.
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL}"

OUT="${OUT:-test/results/control-generator-$(date -u +%Y%m%dT%H%M%SZ)}"
WORKLOAD="${WORKLOAD:-dispersed}"
CONCURRENCY="${CONCURRENCY:-16}"
WINDOW="${WINDOW:-20s}"
PROCS="${PROCS:-1 2 4 0}"          # 0 = unconstrained (all cores)
SLOTS="${SLOTS:-8000}"
CAPACITY="${CAPACITY:-50}"
ORG="${ORG:-load-org}"
BASE="${BASE:-http://localhost:8080}"

mkdir -p "$OUT"
echo "generator control -> $OUT  ($WORKLOAD c=$CONCURRENCY window=$WINDOW)"

for procs in $PROCS; do
  label="$procs"; [ "$procs" = 0 ] && label="all"
  cell="$OUT/gomaxprocs-$label"
  mkdir -p "$cell"

  go run ./cmd/alloca-seed -reset -slots "$SLOTS" -capacity "$CAPACITY" > "$cell/seed.log" 2>&1

  if [ "$procs" = 0 ]; then
    ./bin/alloca-load -workload "$WORKLOAD" -concurrency "$CONCURRENCY" -duration "$WINDOW" \
      -slots "$SLOTS" -org "$ORG" -out "$cell/run.json" > "$cell/load.log" 2>&1 || true
  else
    GOMAXPROCS="$procs" ./bin/alloca-load -workload "$WORKLOAD" -concurrency "$CONCURRENCY" \
      -duration "$WINDOW" -slots "$SLOTS" -org "$ORG" -out "$cell/run.json" \
      > "$cell/load.log" 2>&1 || true
  fi
  printf '  gomaxprocs=%-3s done\n' "$label"
done

python3 - "$OUT" <<'PY'
import glob, json, sys

out = sys.argv[1]
rows = []
for f in sorted(glob.glob(f"{out}/gomaxprocs-*/run.json")):
    d = json.load(open(f))
    s, g = d["summary"], d["summary"]["generator"]
    rows.append({
        "gomaxprocs": g["gomaxprocs"],
        "throughput_per_s": round(s["completed_requests"] / s["duration_seconds"], 1),
        "goodput_per_s": round(s["successful_mutation_goodput"] / s["duration_seconds"], 1),
        "p99_ms": s["latency_ms"]["p99"],
        "generator_cpu_per_core": round(g["cpu_utilisation_per_core"], 4),
    })
rows.sort(key=lambda r: r["gomaxprocs"])

peak = max(r["throughput_per_s"] for r in rows)
top = max(rows, key=lambda r: r["gomaxprocs"])
# Headroom is judged against the *most constrained* run that still reaches the same answer.
# If halving the generator's compute does not move the number, the generator had at least 2x
# the capacity it needed at this operating point.
unconstrained = top["throughput_per_s"]
verdict = []
for r in rows:
    r["pct_of_unconstrained"] = round(100 * r["throughput_per_s"] / unconstrained, 1)
flat = [r for r in rows if r["pct_of_unconstrained"] >= 95]
if len(flat) > 1:
    least = min(flat, key=lambda r: r["gomaxprocs"])
    verdict.append(
        f"throughput is within 5% of unconstrained down to GOMAXPROCS={least['gomaxprocs']}, "
        f"so at this operating point the generator had at least "
        f"{top['gomaxprocs'] // max(least['gomaxprocs'], 1)}x the compute it needed and is not "
        f"the binding constraint")
else:
    verdict.append(
        "throughput tracks the generator's compute at every level tested, so this operating "
        "point is generator-limited and no service frontier may be read from it")

json.dump({"rows": rows, "verdict": verdict}, open(f"{out}/control.json", "w"), indent=2)

print(f"\n{'GOMAXPROCS':>10} {'thru/s':>9} {'good/s':>9} {'p99ms':>8} {'gen cpu':>9} {'% of max':>9}")
for r in rows:
    print(f"{r['gomaxprocs']:>10} {r['throughput_per_s']:>9} {r['goodput_per_s']:>9} "
          f"{r['p99_ms']:>8.1f} {r['generator_cpu_per_core']:>9} {r['pct_of_unconstrained']:>8}%")
print()
for v in verdict:
    print(f"  {v}")
PY
