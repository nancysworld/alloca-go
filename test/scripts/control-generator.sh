#!/usr/bin/env bash
#
# VAL-NEG-2 (ag-sept-validation-plan.md §8), mandatory: deliberately constrain the generator
# and show how the apparent frontier changes.
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
REQUIRE="${REQUIRE:-local}"        # minimum quotability level every control run must reach

mkdir -p "$OUT"
echo "generator control -> $OUT  ($WORKLOAD c=$CONCURRENCY window=$WINDOW require=$REQUIRE)"

# A control that tolerates its own failures cannot rule anything out. Every requested run must
# complete and be admissible, or the control has not been performed — and this one is the
# foundation the whole frontier is read from, so "mostly ran" is not a state it may be in.
#
# `|| true` used to swallow a failed run here, and the verdict was then derived from whatever
# run.json files happened to exist. A generator that crashed under constraint — exactly the
# symptom the control is looking for — left no row and was silently excluded from the answer.
for procs in $PROCS; do
  label="$procs"; [ "$procs" = 0 ] && label="all"
  cell="$OUT/gomaxprocs-$label"
  mkdir -p "$cell"

  go run ./cmd/alloca-seed -reset -slots "$SLOTS" -capacity "$CAPACITY" > "$cell/seed.log" 2>&1 \
    || { printf '\n!! seed failed for gomaxprocs=%s; see %s/seed.log\n' "$label" "$cell" >&2; exit 1; }

  if [ "$procs" = 0 ]; then
    ./bin/alloca-load -workload "$WORKLOAD" -concurrency "$CONCURRENCY" -duration "$WINDOW" \
      -slots "$SLOTS" -org "$ORG" -require "$REQUIRE" -out "$cell/run.json" \
      > "$cell/load.log" 2>&1 \
      || { printf '\n!! control run gomaxprocs=%s failed or was refused at level %s; see %s/load.log\n' \
             "$label" "$REQUIRE" "$cell" >&2; exit 1; }
  else
    GOMAXPROCS="$procs" ./bin/alloca-load -workload "$WORKLOAD" -concurrency "$CONCURRENCY" \
      -duration "$WINDOW" -slots "$SLOTS" -org "$ORG" -require "$REQUIRE" \
      -out "$cell/run.json" > "$cell/load.log" 2>&1 \
      || { printf '\n!! control run gomaxprocs=%s failed or was refused at level %s; see %s/load.log\n' \
             "$label" "$REQUIRE" "$cell" >&2; exit 1; }
  fi
  printf '  gomaxprocs=%-3s done\n' "$label"
done

python3 - "$OUT" "$PROCS" "$REQUIRE" <<'PY'
import glob, json, os, sys

out, requested, require = sys.argv[1], sys.argv[2].split(), sys.argv[3]
LEVELS = ["none", "local", "capacity", "publishable"]

rows, refusals = [], []

# Every requested rung must be present. Deriving the verdict from whatever files exist is how a
# crashed run — the very symptom being tested for — disappears from the answer instead of
# failing it.
for procs in requested:
    label = "all" if procs == "0" else procs
    f = f"{out}/gomaxprocs-{label}/run.json"
    if not os.path.exists(f):
        refusals.append(f"gomaxprocs={label}: no run.json, so the rung was never measured")
        continue
    d = json.load(open(f))
    s, g, m = d["summary"], d["summary"]["generator"], d["manifest"]

    # Soundness, admissibility, and identity are each checked, because a control that averaged
    # over an unsound run would rule out the generator using evidence it should have refused.
    if not s.get("measurement_sound", False):
        refusals.append(f"gomaxprocs={label}: measurement is not sound")
    if s.get("invalid_responses", 0):
        refusals.append(f"gomaxprocs={label}: {s['invalid_responses']} invalid responses")
    if not s.get("response_validation_enabled", False):
        refusals.append(f"gomaxprocs={label}: response validation was disabled")
    lvl = d["quotability"]["level"]
    if LEVELS.index(lvl) < LEVELS.index(require):
        refusals.append(f"gomaxprocs={label}: quotability {lvl} is below the required {require}")
    for t in s.get("totals", []):
        if t["outcome"] in ("timeout_before_commit", "timeout_after_commit", "unknown_replayable",
                            "internal_failure"):
            refusals.append(f"gomaxprocs={label}: {t['count']} x {t['outcome']}")

    rows.append({
        "gomaxprocs": g["gomaxprocs"],
        "throughput_per_s": round(s["completed_requests"] / s["duration_seconds"], 1),
        "goodput_per_s": round(s["successful_mutation_goodput"] / s["duration_seconds"], 1),
        "p99_ms": s["latency_ms"]["p99"],
        "generator_cpu_per_core": round(g["cpu_utilisation_per_core"], 4),
        "level": lvl,
        "_identity": (m["service_commit_sha"], m["service_source_modified"],
                      m["pool_size_per_replica"], m["server_gomaxprocs"], m["telemetry_mode"]),
    })
rows.sort(key=lambda r: r["gomaxprocs"])

# One service, one configuration, across the whole ladder. Otherwise the rungs are not
# comparable and the flatness they show is not about the generator at all.
identities = {r["_identity"] for r in rows}
if len(identities) > 1:
    refusals.append(f"the ladder ran against {len(identities)} different service "
                    f"identities/configurations: {sorted(identities)}")
for r in rows:
    del r["_identity"]

verdict = []
if rows and not refusals:
    top = max(rows, key=lambda r: r["gomaxprocs"])
    unconstrained = top["throughput_per_s"]
    for r in rows:
        r["pct_of_unconstrained"] = round(100 * r["throughput_per_s"] / unconstrained, 1)

    # Every rung must sit in the band, not merely the lowest one. The old logic took the
    # smallest GOMAXPROCS within 5% and reported headroom "down to" it, so a ladder of
    # 101%, 100%, 33%, 100% printed a PASS: it could not tell "flat" from "flat with a hole
    # in it", and would have passed identically had the hole been a real generator limit.
    # Observed on 2026-08-04 at c=128/pool=80, where the 32.8% rung was an environmental
    # excursion — but the script had no way to know that, and said nothing.
    outliers = [r for r in rows if r["pct_of_unconstrained"] < 95]
    if outliers:
        verdict.append(
            "INCONCLUSIVE: throughput is not flat across the ladder — "
            + ", ".join(f"GOMAXPROCS={r['gomaxprocs']} at {r['pct_of_unconstrained']}%"
                        for r in outliers)
            + ". Either the generator binds at those levels, or the run met the environmental "
              "excursion the frontier report's §4 describes; the generator cannot be ruled out "
              "from this ladder until every rung is within 5%. Check each rung's generator CPU: "
              "a generator that is binding works harder, not less.")
    elif len(rows) > 1:
        least = min(rows, key=lambda r: r["gomaxprocs"])
        verdict.append(
            f"throughput is within 5% of unconstrained at EVERY level tested, down to "
            f"GOMAXPROCS={least['gomaxprocs']}, so at this operating point the generator had at "
            f"least {top['gomaxprocs'] // max(least['gomaxprocs'], 1)}x the compute it needed "
            f"and is not the binding constraint")
    else:
        verdict.append("only one rung was measured, so nothing is controlled for")
else:
    for r in rows:
        r["pct_of_unconstrained"] = None
    verdict.append("REFUSED: the control did not produce an admissible ladder; see refusals")

json.dump({"rows": rows, "requested": requested, "require": require,
           "refusals": refusals, "verdict": verdict},
          open(f"{out}/control.json", "w"), indent=2)

print(f"\n{'GOMAXPROCS':>10} {'thru/s':>9} {'good/s':>9} {'p99ms':>8} {'gen cpu':>9} {'% of max':>9}")
for r in rows:
    pct = r["pct_of_unconstrained"]
    print(f"{r['gomaxprocs']:>10} {r['throughput_per_s']:>9} {r['goodput_per_s']:>9} "
          f"{r['p99_ms']:>8.1f} {r['generator_cpu_per_core']:>9} "
          f"{'-' if pct is None else str(pct)+'%':>9}")
print()
for x in refusals:
    print(f"  !! {x}")
for v in verdict:
    print(f"  {v}")

sys.exit(1 if refusals or any(v.startswith(("REFUSED", "INCONCLUSIVE")) for v in verdict) else 0)
PY
