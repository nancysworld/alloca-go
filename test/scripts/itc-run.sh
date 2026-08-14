#!/usr/bin/env bash
#
# Drive one bounded rehearsal cell across the Iteration C topology, with the generator confined
# to its own CPUs.
#
# **Confining the generator is the half of the partition Docker cannot do.** The rehearsal overlay
# pins each shard group to a cpuset, but the generator is a host process, not a container: left
# alone the scheduler will run it on the capacity units' CPUs and dissolve the partition the
# overlay was raised to create. The units would then be sharing their two CPUs with the thing
# measuring them, and the resulting Goodput would describe neither the units nor the generator.
#
# So the generator runs under `taskset -c "$ITC_CPUS_GENERATOR"`, and the layout is checked first
# — the same check `make itc-rehearse` runs, repeated here because this script is independently
# invocable and the generator set it is about to use is the one that check validates.
#
# **taskset also sets GOMAXPROCS, and that is intended.** Go reads the affinity mask at startup,
# so a generator confined to four CPUs schedules over four. That is the property the confinement
# is for: the generator gets a fixed share of the machine, not a fixed share that it may exceed
# whenever the units are idle.
#
#   make itc-rehearse ITC_GROUPS=4        # raise the partitioned topology first
#   ITC_GROUPS=4 ./test/scripts/itc-seed.sh
#   ITC_GROUPS=4 ./test/scripts/itc-run.sh
#
# **The generator-headroom control** (ag-sept-pr4.md §2.14) is this script rerun over the same
# fixture with the generator widened onto the CPUs the standard partition holds idle:
#
#   ITC_GROUPS=4 ITC_CPUS_GENERATOR=8-15 ./test/scripts/itc-run.sh
#
# If Goodput does not move, the generator was not the binding constraint at that operating point
# and the units' number stands. If it tracks the generator's size, the cell was measuring the
# harness. This is the cpuset analogue of VAL-NEG-2's GOMAXPROCS control
# (test/scripts/control-generator.sh); it constrains placement rather than parallelism, which is
# what a partitioned rehearsal can vary and a single-host sweep cannot.
#
# **What this script's numbers are.** Every group shares one workstation, one WSL kernel, one
# storage path and one page cache, so the partition bounds CPU and nothing else. Rehearsal
# Goodput is diagnostic evidence: it cannot discharge `VAL-SCALE-5`, cannot become a Tier-2
# result, and is never mixed with AWS points (ag-sept-validation-plan.md §4.6). What it *can*
# do is prove the machinery — placement, fixtures, declaration, provenance, certification —
# before any of it is exercised on metered infrastructure.

set -euo pipefail

ITC_GROUPS="${ITC_GROUPS:-4}"

case "$ITC_GROUPS" in
  1|2|4) ;;
  *) echo "ITC_GROUPS must be 1, 2 or 4 (ag-sept-validation-plan.md §4.6); got '$ITC_GROUPS'" >&2
     exit 1 ;;
esac

ITC_CPUS_GENERATOR="${ITC_CPUS_GENERATOR:-8-11}"

WORKLOAD="${WORKLOAD:-wl-mut-disp-4}"
CONCURRENCY="${CONCURRENCY:-16}"
WINDOW="${WINDOW:-60s}"
# No WARMUP. `-warm-up` is refused outright at every level (measurement-contract §12): it drops
# responses from the client totals while their rows stay in the database, which persisted-state
# reconciliation cannot square. Warming is a separate invocation followed by a reseed, the shape
# sweep.sh uses; it is not a flag on the measured run, and re-adding one here would refuse every
# cell at `none`.

# **Interim, not the PR4b fixture size** (maintainer decision, 2026-08-13). 3200 x 20 x 4
# organisations is 256,000 fresh mutations against the 200,693 requests the first G4 cell
# completed, so it holds even under the conservative assumption that every request admits.
#
# The final value must be derived from the *deepest rung the ladder will reach* and then kept
# identical across G1, G2 and G4 — a fixture sized for the selected point can be exhausted by the
# rung above it, and an exhausted higher rung invalidates the point below it (§3.10). The default
# is not 200 any more because that value is now known to exhaust in about five seconds.
SLOTS="${SLOTS:-3200}"
CAPACITY="${CAPACITY:-20}"
PLACEMENT="${PLACEMENT:-deploy/topology/placement-itc-g${ITC_GROUPS}.json}"
DEPLOYMENT="${DEPLOYMENT:-test/results/deployment.json}"
# One declaration per capacity point, not one per rehearsal: G1, G2 and G4 are different
# deployment shapes, and `deployment_topology` is the shape a later run is compared against. A
# single shared document would describe three topologies with one string, and the two it did not
# describe would certify against a false statement. Selected by ITC_GROUPS for the same reason
# the placement document is — so the two cannot drift apart.
DECLARATION="${DECLARATION:-deploy/topology/declaration-itc-g${ITC_GROUPS}.json}"
OUT="${OUT:-test/results/itc-g${ITC_GROUPS}-$(date -u +%Y%m%dT%H%M%SZ)}"

# `capacity`, not `local`, because certification is one of the things PR4a exists to rehearse:
# the declaration, the deployment record and the per-unit provenance all have to line up, and
# discovering on AWS that they do not costs the rung.
#
# `capacity` is also the ceiling here and not a conservative choice. The generator is co-resident
# with the units it drives, and co-residency blocks `publishable` outright
# (measurement-contract.md §13) however clean everything else is. Asking for it would refuse
# every rehearsal run for a reason the rehearsal cannot fix.
REQUIRE="${REQUIRE:-capacity}"

# Where the scrape gate looks. Settable to empty to drive a cell with no monitoring at all, which
# is a legitimate thing to do while shaking out the harness — but it has to be said out loud
# rather than being what happens when Prometheus is quietly absent.
PROM_URL="${PROM_URL-http://localhost:9091}"
PROM_JOB="${PROM_JOB:-alloca-go}"

LOAD=bin/alloca-load

log()  { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

# The cpusets read off the running containers, captured during preflight and retained beside the
# run. The declared partition and the applied one are different facts, and a reader comparing two
# cells — a standard run against its headroom control — needs the observed one.
OBSERVED_CPUSETS="$(mktemp)"
trap 'rm -f "$OBSERVED_CPUSETS"' EXIT

# --- preflight ---------------------------------------------------------------------------
#
# Checked before the fixture is touched rather than discovered by a `level: none` verdict after
# the window has been driven.

command -v taskset >/dev/null 2>&1 \
  || fail "taskset is not available, so the generator cannot be confined and the partition the
  rehearsal overlay creates would be dissolved by the generator itself (util-linux)"

[ -x "$LOAD" ] || fail "build the generator: go build -o $LOAD ./cmd/alloca-load"
[ -f "$PLACEMENT" ]   || fail "no placement document at $PLACEMENT"
[ -f "$DEPLOYMENT" ]  || fail "no deployment record at $DEPLOYMENT — run 'make itc-deployment ITC_GROUPS=$ITC_GROUPS > $DEPLOYMENT'"
[ -f "$DECLARATION" ] || fail "no declaration at $DECLARATION (ag-sept-pr4.md §2.7)"

# `git status --porcelain`, not `git diff`: Go stamps *untracked* files as a modified tree, so a
# scratch file anywhere refuses every run at level none for source_modified — after the window
# has been driven (ag-sept-pr4.md §3.6).
[ -z "$(git status --porcelain)" ] \
  || fail "working tree is not clean (git status --porcelain is non-empty, and Go stamps untracked
  files as a modified tree): the run would be refused at level none and back nothing"

# The generator set is validated by the layout check rather than here, so there is one definition
# of a legal partition. It also fails before the fixture is touched, which is the point.
ITC_GROUPS="$ITC_GROUPS" ITC_CPUS_GENERATOR="$ITC_CPUS_GENERATOR" \
  ./test/scripts/itc-cpu-layout.sh \
  || fail "the CPU partition is not usable; fix it before driving a cell"

# The topology actually running must be the one being measured. Raising G1 after G4 leaves three
# units alive competing for the envelope, and nothing downstream notices: alloca-load reads /meta
# only from the units it addresses (see itc-topology-check.sh).
ITC_GROUPS="$ITC_GROUPS" ./test/scripts/itc-topology-check.sh \
  || fail "the running topology is not G$ITC_GROUPS"

# The partition being legal is not the partition being applied. `make itc-up` and `make obs-up`
# raise the same containers unpinned, and nothing downstream would notice: the run addresses the
# right units, routing passes, certification is clean, and the numbers carry contention no
# artifact records.
ITC_GROUPS="$ITC_GROUPS" \
ITC_CPUS_A="${ITC_CPUS_A:-0-1}" ITC_CPUS_B="${ITC_CPUS_B:-2-3}" \
ITC_CPUS_C="${ITC_CPUS_C:-4-5}" ITC_CPUS_D="${ITC_CPUS_D:-6-7}" \
ITC_CPUS_GENERATOR="$ITC_CPUS_GENERATOR" \
  ./test/scripts/itc-cpuset-check.sh 2>&1 | tee "$OBSERVED_CPUSETS"
[ "${PIPESTATUS[0]}" -eq 0 ] \
  || fail "the containers are not pinned where the partition says; raise them with
  'make itc-rehearse' and 'make obs-rehearse' rather than 'make itc-up' and 'make obs-up'"

# Exactly the units this rung raises must be scraped, and be up.
#
# "Prometheus is running" is not the property. A successful query against a job with no targets
# returns an empty result and no error, so every panel renders, every CSV exports with headers,
# and the whole cell completes having retained no time series at all — the failure shape sweep.sh
# records as the worst there is, because nothing anywhere reports it. That is exactly what the
# first driven G4 cell hit: Prometheus was healthy and scraping a stale host address.
#
# Counting is what makes it a gate rather than a smoke test. `up == 1` for *some* target passes
# while three of four units are missing, and a G4 point measured with one unit unobserved is not
# a G4 point — it is the rung below it, wearing the wrong label.
#
# Skipped, loudly, when no Prometheus is reachable: a rehearsal is allowed to run without one, and
# the run's own totals do not depend on it. What must never happen is a run that believes it was
# observed when it was not.
if curl -sf -m 5 "$PROM_URL/-/ready" >/dev/null 2>&1; then
  scraped="$(curl -sfG -m 10 "$PROM_URL/api/v1/query" \
      --data-urlencode "query=count(up{job=\"$PROM_JOB\"} == 1)" 2>/dev/null \
    | python3 -c 'import json,sys
try:
    r = json.load(sys.stdin)["data"]["result"]
    print(int(float(r[0]["value"][1])) if r else 0)
except Exception:
    print(0)' 2>/dev/null)" || scraped=0

  if [ "${scraped:-0}" -ne "$ITC_GROUPS" ]; then
    fail "prometheus is up but ${scraped:-0} of $ITC_GROUPS units are being scraped.
  A successful query against an unscraped job returns nothing and reports no error, so the cell
  would complete and retain no series. Regenerate the target list and give Prometheus its
  refresh interval to pick it up:

      ITC_GROUPS=$ITC_GROUPS ./test/scripts/itc-obs-targets.sh
      curl -s $PROM_URL/api/v1/targets | grep -o '\"health\":\"[a-z]*\"'

  Set PROM_URL= to drive a cell deliberately without monitoring."
  fi
  log "prometheus scraping $scraped/$ITC_GROUPS units"
elif [ -n "$PROM_URL" ]; then
  fail "prometheus is not reachable at $PROM_URL (set PROM_URL= to run without monitoring)"
fi

mkdir -p "$OUT"

# --- endpoints ------------------------------------------------------------------------------
#
# Derived from ITC_GROUPS rather than listed, for the same reason itc-deployment derives its
# container set: a hand-maintained list that disagrees with the topology produces a run that
# addresses three of four units and reports a clean four-unit result.
endpoints=()
for n in $(seq 1 "$ITC_GROUPS"); do
  case $n in
    1) port="${SERVICE_1_PORT:-8081}" ;;
    2) port="${SERVICE_2_PORT:-8082}" ;;
    3) port="${SERVICE_3_PORT:-8083}" ;;
    4) port="${SERVICE_4_PORT:-8084}" ;;
  esac
  endpoints+=(-endpoint "authority-${n}=http://localhost:${port}")
done

# --- the cell ---------------------------------------------------------------------------------

# Retained beside the artifacts because the partition is part of what the run means, and it is
# the one environment fact that changes between a standard run and the headroom control. The
# declaration deliberately does *not* name the generator's CPUs — the control widens them, so a
# static string would be false for half the runs it describes. This file, and the observed
# cpusets beside it, are where the effective generator/monitor set is recorded per run.
cp "$OBSERVED_CPUSETS" "$OUT/observed-cpusets.txt"
cat > "$OUT/cpu-partition.txt" <<EOF
ITC_GROUPS=$ITC_GROUPS
ITC_CPUS_A=${ITC_CPUS_A:-0-1}
ITC_CPUS_B=${ITC_CPUS_B:-2-3}
ITC_CPUS_C=${ITC_CPUS_C:-4-5}
ITC_CPUS_D=${ITC_CPUS_D:-6-7}
ITC_CPUS_GENERATOR=$ITC_CPUS_GENERATOR
nproc=$(nproc)
EOF

cat > "$OUT/fixture.txt" <<EOF
SLOTS=$SLOTS
CAPACITY=$CAPACITY
organisations=4
fresh_mutation_supply=$((SLOTS * CAPACITY * 4))
EOF

log "G$ITC_GROUPS cell -> $OUT"

# **Reseed immediately before the measured window, every time** (maintainer decision, 2026-08-13).
#
# A ladder point that inherits the previous point's depleted fixture measures the fixture, not the
# service — and it does so while looking entirely healthy, because refusing a booking for a full
# slot is a correct answer that certifies. The first G4 rehearsal cell consumed its whole 16,000
# unit supply in roughly the first five seconds and spent the remaining ~55s measuring refusal
# throughput (§3.10).
#
# Owned by this script rather than left to the operator because "reseed between rungs" is exactly
# the step a ladder of a dozen cells drops once, silently, and every point after it is wrong.
# `alloca-seed -reset` also asserts a clean start, so this is where a contaminated fixture is
# caught rather than inferred later from an odd outcome mix.
log "  reseeding: $SLOTS slots x $CAPACITY capacity per organisation ($((SLOTS * CAPACITY * 4)) fresh mutations)"
# `.txt`, not `.log`: `.gitignore` excludes `*.log`, so a transcript written under that name
# cannot be retained when a cell is promoted into `docs/measurements/`, and a promoted cell would
# cite a file the repository does not carry.
ITC_GROUPS="$ITC_GROUPS" SLOTS="$SLOTS" CAPACITY="$CAPACITY" \
  ./test/scripts/itc-seed.sh > "$OUT/seed.txt" 2>&1 \
  || fail "reseeding failed; see $OUT/seed.txt"

# Bracketing scrapes. The service counters are cumulative and the units are deliberately left
# running between cells, so only the delta across the measured window describes this cell —
# a single scrape describes everything the process has ever done.
scrape() {
  local when="$1" n port
  for n in $(seq 1 "$ITC_GROUPS"); do
    case $n in
      1) port="${SERVICE_1_METRICS_PORT:-9081}" ;;
      2) port="${SERVICE_2_METRICS_PORT:-9082}" ;;
      3) port="${SERVICE_3_METRICS_PORT:-9083}" ;;
      4) port="${SERVICE_4_METRICS_PORT:-9084}" ;;
    esac
    curl -sS -m 10 "http://localhost:${port}/metrics" > "$OUT/s${n}-${when}.prom" \
      || fail "could not scrape unit $n on ${port} (${when}); the cell would have no counter
  delta for that unit, which is the evidence a report reads per authority"
  done
}

scrape baseline

log "  generator confined to CPUs $ITC_CPUS_GENERATOR ($WORKLOAD c=$CONCURRENCY window=$WINDOW require=$REQUIRE)"

# Bracket the measured phase for the panel export. A cell's headline scalars come from run.json,
# but a scalar cannot show a *shape* — and this workload's rate is not flat within a window
# (§3.11), so the average alone actively misdescribes what happened. The series lives in
# Prometheus, whose retention will drop it, so a report can only quote it if the cell retained it.
measured_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# No `-warm-up`. The flag discards responses from the client totals while their rows stay in the
# database, which persisted-state reconciliation cannot reconcile, so it refuses the run at
# `none` (measurement-contract §12). Warming is a *separate* invocation followed by a reseed —
# the shape sweep.sh uses — and the reseed above is what makes that safe to add here later.
taskset -c "$ITC_CPUS_GENERATOR" "$LOAD" \
  -placement "$PLACEMENT" \
  "${endpoints[@]}" \
  -deployment "$DEPLOYMENT" \
  -declaration "$DECLARATION" \
  -workload "$WORKLOAD" \
  -concurrency "$CONCURRENCY" \
  -duration "$WINDOW" \
  -slots "$SLOTS" \
  -require "$REQUIRE" \
  -out "$OUT/run.json" 2>&1 | tee "$OUT/generator-output.txt"

# The generator's status, not tee's. `cmd | tee; echo $?` reports tee, so a refused run would be
# reported as a successful one and the whole point of -require would be lost.
status="${PIPESTATUS[0]}"
[ "$status" -eq 0 ] || fail "the run failed or was refused below $REQUIRE; see $OUT/generator-output.txt"

measured_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# After the run has exited, not before: alloca-load replays ambiguous mutations in a post-run
# pass, and a scrape taken while that is still in flight misses requests the report counts.
scrape after

# Retain the shape, not just the endpoints. export-panels.sh writes every canonical panel as a
# CSV over the measured phase and takes a TSDB snapshot beside them, which is what keeps the
# series checkable after Prometheus's retention window has dropped it.
#
# **Not tolerated on failure.** An export that only logged its failure is a defect this
# repository has already met: the cell completes, the run looks finished, and the evidence the
# report was going to quote silently does not exist. Skipped only when the cell was deliberately
# driven without monitoring.
if [ -n "$PROM_URL" ]; then
  PROM_URL="$PROM_URL" ./test/scripts/export-panels.sh "$OUT" "$measured_start" "$measured_end" \
    || fail "panel export failed; the cell has its scalars but no retained series, and the
  rate within this window is not flat (ag-sept-pr4.md §3.11) so the average alone does not
  describe it. Artifacts are in $OUT"
fi

# The useful-demand discriminator, reported rather than gated (measurement-contract §5). A cell
# that admitted its whole supply measured the fixture's headroom, not the service — and it stays
# `measurement_sound` and certifies at whatever its manifest earns, so nothing else will say so.
admitted="$(python3 -c '
import json, sys
try:
    totals = json.load(open(sys.argv[1]))["summary"]["totals"]
    print(sum(t["count"] for t in totals if t.get("outcome") == "admitted_success"
              and not t.get("replay")))
except Exception:
    print(-1)' "$OUT/run.json" 2>/dev/null)" || admitted=-1

supply=$((SLOTS * CAPACITY * 4))
if [ "${admitted:-0}" -ge "$supply" ] && [ "$supply" -gt 0 ]; then
  echo >&2
  echo "!! this cell exhausted its fixture: ${admitted} admitted against a supply of ${supply}." >&2
  echo "   It certifies, and it backs no capacity number — what it measured after exhaustion is" >&2
  echo "   refusal throughput (measurement-contract §5). Raise SLOTS and re-run before quoting" >&2
  echo "   anything, and remember an exhausted rung also invalidates the rung below it." >&2
fi

log "cell complete -> $OUT/run.json  (${admitted:-?} admitted of ${supply} supplied)"
