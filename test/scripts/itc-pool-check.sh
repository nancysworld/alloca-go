#!/usr/bin/env bash
#
# Asserts that the pool ceiling is declared identically for every shard-group service, and that
# leaving it unset changes nothing.
#
# Four service blocks have to agree, and nothing in Compose makes them. Three of four carrying
# the override is the dangerous shape: the run raises, every unit is healthy, routing passes,
# certification is clean, and the pool-sensitivity comparison silently contains one authority
# still on the default ceiling — an experiment about connection admission in which a quarter of
# the topology did not receive the treatment (ag-sept-validation-plan.md §4.6.3).
#
# The inert-by-default half matters just as much. Every recipe committed before the override
# existed must render byte-identically without it, or the variable would have quietly changed
# what those runs measure.
#
# Renders through `docker compose config`, so it checks what Compose actually produces rather
# than what the YAML looks like. Needs the docker CLI but raises nothing.
set -euo pipefail

cd "$(dirname "$0")/../.."

COMPOSE="deploy/topology/docker-compose.yml"

pass=0
fail=0
ok()  { echo "  ok    $1"; pass=$((pass + 1)); }
bad() { echo "  FAIL  $1" >&2; fail=$((fail + 1)); }

echo "itc-pool-check: pool ceiling is declared once, for every unit"

# Refused rather than skipped when Compose is missing. A gate that quietly passes when its tool
# is absent is the shape this repository has already been bitten by: it reports success on every
# machine that cannot run it, which is exactly where the drift would go unnoticed. Only the
# `docker compose config` renderer is needed — no daemon, nothing raised — so the requirement is
# cheap. ITC_POOL_CHECK_SKIP=1 is the deliberate, visible escape.
if [ -n "${ITC_POOL_CHECK_SKIP:-}" ]; then
  echo "  SKIP  ITC_POOL_CHECK_SKIP is set; the four service blocks are NOT being checked" >&2
  exit 0
fi
docker compose version >/dev/null 2>&1 \
  || { echo "  FAIL  docker compose is unavailable and this check renders the topology through
        it. Install the Compose plugin, or set ITC_POOL_CHECK_SKIP=1 to skip it deliberately." >&2
       exit 1; }

render() { # ALLOCA_POOL_MAX_CONNS value ("" for unset)
  if [ -z "$1" ]; then
    docker compose -f "$COMPOSE" --profile g4 config 2>/dev/null
  else
    ALLOCA_POOL_MAX_CONNS="$1" docker compose -f "$COMPOSE" --profile g4 config 2>/dev/null
  fi
}

# --- unset is inert ---------------------------------------------------------------------------
if render "" | grep -q "pool_max_conns"; then
  bad "an unset ALLOCA_POOL_MAX_CONNS still put pool_max_conns in a DATABASE_URL; every recipe
        predating the override would silently change what it measures"
else
  ok "unset leaves every DATABASE_URL as it was"
fi

# --- set reaches every serving unit, and only those ---------------------------------------------
rendered="$(render 8)"

# Counted over the four *service* units. The migrate containers deliberately do not carry it:
# they run DDL once and exit, and a connection ceiling on them would describe nothing.
carrying="$(printf '%s\n' "$rendered" | grep -c "alloca?sslmode=disable&pool_max_conns=8" || true)"
if [ "$carrying" = "4" ]; then
  ok "all 4 shard-group services carry the declared ceiling"
else
  bad "$carrying of 4 shard-group services carry pool_max_conns=8; a sensitivity result would
        contain an authority that never received the treatment"
fi

migrators="$(printf '%s\n' "$rendered" | grep -c "alloca-migrate" || true)"
if [ "$migrators" -gt 0 ]; then
  ok "the migrate containers are present and unaffected by the ceiling"
fi

# --- the value is the one that was asked for ----------------------------------------------------
# A ceiling that rendered a different number than it was given would produce a comparison whose
# two arms are not the two arms the artifact names.
if printf '%s\n' "$(render 4)" | grep -q "pool_max_conns=4" \
   && ! printf '%s\n' "$(render 4)" | grep -q "pool_max_conns=8"; then
  ok "the rendered ceiling is the declared one"
else
  bad "rendering ALLOCA_POOL_MAX_CONNS=4 did not produce pool_max_conns=4 alone"
fi

# --- the evidence summary renders the ceiling it actually ran with ----------------------------
#
# It did not. The denominator was hard-coded to 4 -- the value every cell happened to use -- so
# the first cell driven at a different policy rendered "8.0/8" as "8.0/4": a pool reported as
# twice full. A reader checking whether the pool saturated would have drawn the opposite
# conclusion from the one the numbers support, and every gate would still have passed.
#
# Checked against a synthetic cell at a non-4 ceiling, because a check built from a pool-4 cell
# cannot distinguish a resolved denominator from the constant that used to be there.
synthetic="$(mktemp -d)"
trap 'rm -rf "$synthetic"' EXIT
mkdir -p "$synthetic/panels"
cat > "$synthetic/run.json" <<'JSON'
{"manifest":{"unit_count":1,"pool_size_per_replica":16,"workload":"wl-mut-disp-4"},
 "summary":{"successful_mutation_goodput":600,"duration_seconds":60.0,
   "latency_ms":{"p50":1,"p95":2,"p99":3,"max":4},
   "generator":{"cpu_utilisation_per_core":0.1},
   "totals":[{"operation":"reserve","outcome":"admitted_success","count":600}]},
 "quotability":{"level":"capacity"}}
JSON
{
  echo "timestamp,labels,value"
  for t in 10 20 30; do echo "$t,\"authority=authority-1,unit=1\",15.5"; done
} > "$synthetic/panels/pool_in_use.csv"
{
  echo "timestamp,labels,value"
  for t in 10 20 30; do echo "$t,\"authority=authority-1,unit=1\",16"; done
} > "$synthetic/panels/pool_total.csv"
# `throughput` is what establishes the cell's rate window, and every per-authority series is
# read inside it. Without this panel the classifier reports "endpoints only" and never reaches
# the occupancy column at all — which would make this check pass vacuously on a regression.
{
  echo "timestamp,labels,value"
  for t in 10 20 30; do echo "$t,,600"; done
} > "$synthetic/panels/throughput.csv"

rendered="$(./test/scripts/itc-classify.py "$synthetic" 2>/dev/null || true)"
if printf '%s' "$rendered" | grep -q '15.5/16'; then
  ok "the occupancy column renders the resolved ceiling (15.5/16)"
elif printf '%s' "$rendered" | grep -q '/4'; then
  bad "the occupancy column rendered a pool-16 cell against a denominator of 4; the constant is
        back and a saturated pool would read as impossible over-occupancy"
else
  bad "could not read an occupancy figure from the classifier for a pool-16 cell:
        $(printf '%s' "$rendered" | tail -2)"
fi

echo
echo "itc-pool-check: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
