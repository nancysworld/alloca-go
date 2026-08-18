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

# --- conditioning (ag-sept-validation-plan.md §4.6.2) ------------------------------------------
#
# `TRUNCATE -> immediate peak load` opens the measured window against empty mutation tables, so
# the pool's connections prepare their lookups against a relation with no pages and PostgreSQL
# caches a Seq Scan that outlives the planner's own correction by tens of seconds (§3.20). The
# canonical experiment therefore does not begin there: it drives an explicit conditioning phase to
# a declared state target, recycles the pool so no measured connection carries a plan made against
# an empty table, and only then opens the measured interval.
#
# **This is not `-warm-up`.** Conditioning's requests, outcomes and mutations are retained in their
# own artifact and stay visible to reconciliation; what they are not is measured performance
# (measurement-contract §5, §12.1). The distinction is the population boundary, not the name.
#
# **Off by default, and the cell says so.** The retained PR4a series is the untreated baseline the
# conditioned shape is compared against, and silently changing what every cell does would destroy
# that comparison rather than inform it — the same reasoning ANALYZE_AFTER_SEED carries below. A
# canonical §4.6.5 capacity run sets these; an unconditioned cell is diagnostic evidence and its
# artifacts record which it was.
CONDITIONING_TARGET="${CONDITIONING_TARGET:-0}"
# Slots per organisation the conditioning population claims. They are its own: conditioning spends
# slot capacity, and drawing from the measured population's range would arrive at the measured
# interval having already eaten the headroom the capacity point depends on.
CONDITIONING_SLOTS="${CONDITIONING_SLOTS:-0}"
PLACEMENT="${PLACEMENT:-deploy/topology/placement-itc-g${ITC_GROUPS}.json}"
DEPLOYMENT="${DEPLOYMENT:-test/observed/deployment.json}"
# One declaration per capacity point, not one per rehearsal: G1, G2 and G4 are different
# deployment shapes, and `deployment_topology` is the shape a later run is compared against. A
# single shared document would describe three topologies with one string, and the two it did not
# describe would certify against a false statement. Selected by ITC_GROUPS for the same reason
# the placement document is — so the two cannot drift apart.
DECLARATION="${DECLARATION:-deploy/topology/declaration-itc-g${ITC_GROUPS}.json}"
# Runs nest one level under the milestone that commissioned them, so `test/results/` stays
# navigable as cells accumulate — a flat directory of timestamps stops being readable at about
# thirty. `RESULTS_GROUP` is the knob: PR4b sets it to `pr4b` rather than editing this default,
# which keeps its cells from landing in PR4a's drawer.
RESULTS_GROUP="${RESULTS_GROUP:-pr4a}"
OUT="${OUT:-test/results/$RESULTS_GROUP/itc-g${ITC_GROUPS}-$(date -u +%Y%m%dT%H%M%SZ)}"

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

# The declaration is the operator's *assertion* about the environment, and certification trusts it
# — so the one machine-specific number in it has to be checked against the machine. It names the
# logical CPU count, the committed default says 16, and `itc-cpu-layout.sh` deliberately accepts a
# rescaled partition on a smaller host: without this, the same run on a 12-CPU machine reaches
# `capacity` carrying a false environment description, and the falsehood is in exactly the field a
# reader would use to judge whether the numbers transfer.
#
# Checked here, not generated: generating it would make the declared document a second observed
# one, and the split between what the operator asserts and what the harness observed is the thing
# §2.7 exists to preserve. A mismatch means edit the declaration — that is the operator saying
# what this environment is, which is its job.
declared_cpus="$(grep -o '[0-9]\+ logical CPUs' "$DECLARATION" | head -1 | grep -o '^[0-9]\+' || true)"
[ -n "$declared_cpus" ] \
  || fail "$DECLARATION does not state a logical CPU count.
  The environment field must name it — certification trusts this text, and a reader uses it to
  judge whether the numbers transfer. Expected a phrase like '$(nproc) logical CPUs exposed'."
[ "$declared_cpus" = "$(nproc)" ] \
  || fail "$DECLARATION declares $declared_cpus logical CPUs; this machine exposes $(nproc).
  The run would certify at capacity carrying a false environment description. Edit the
  declaration to describe this machine, or drive the rung on the machine it describes."

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
# **Identities AND count — they are complementary, and treating them as alternatives cost a
# gate.** The first version counted: `count(up{job=...} == 1) == ITC_GROUPS`, which the wrong
# *set* satisfies as easily as the right one — a G4 rehearsal missing `authority-3` but carrying
# a stray healthy target still counts four. The second version fixed that by comparing the
# authority set with the selector pinned to `topology="itc-gN"` — and in narrowing the selector it
# stopped being able to see anything outside the rung, which is precisely where contamination
# lives. Each version was blind to what the other caught.
#
# The stray target is not hypothetical and survives both halves separately. `obs-rehearse` runs
# `itc-obs-targets.sh`, which refuses when `targets/alloca-go.json` already exists — but then
# invokes `obs-up`, which always runs `obs-target.sh`, recreating that file *after* the refusal
# has passed if anything is listening on the host's metrics port. `OBS_HOST_TARGET=0` now stops
# the rehearsal path from probing at all, and this gate is the backstop for every other way an
# extra target arrives.
#
# What makes it worth refusing over is not the extra scrape, which is trivial. It is what the
# extra target implies: a host-run service reachable on :9090 is an **unpinned process in the same
# WSL environment**, free to contend with the rehearsal while the cpuset and topology checks all
# pass. That is the silent environment contamination this rehearsal exists to eliminate.
#
# So the query is deliberately unfiltered by topology: it asks for every healthy target in the
# job, and the comparison below rejects both a missing unit and an unexpected one.
#
# The later per-unit `/metrics` curls do not cover this: they prove the units answer *this
# script*, not what Prometheus is scraping.
#
# Skipped, loudly, when no Prometheus is reachable: a rehearsal is allowed to run without one, and
# the run's own totals do not depend on it. What must never happen is a run that believes it was
# observed when it was not.
if curl -sf -m 5 "$PROM_URL/-/ready" >/dev/null 2>&1; then
  scraped_authorities="$(curl -sfG -m 10 "$PROM_URL/api/v1/query" \
      --data-urlencode "query=up{job=\"$PROM_JOB\"} == 1" 2>/dev/null \
    | ITC_GROUPS="$ITC_GROUPS" python3 -c 'import json,os,sys
# Identify every healthy target by rung and authority, so an extra one cannot hide behind a
# correct count and a missing one cannot hide behind an extra.
try:
    rung = "itc-g" + os.environ["ITC_GROUPS"]
    names = []
    for s in json.load(sys.stdin)["data"]["result"]:
        m = s["metric"]
        if m.get("topology") == rung:
            names.append(m.get("authority", "unlabelled"))
        else:
            # Anything outside this rung is named by what it actually is, so the refusal below
            # can say which foreign target it found rather than only that the set differed.
            names.append("FOREIGN[topology=%s instance=%s]"
                         % (m.get("topology", "<none>"), m.get("instance", "?")))
    print(",".join(sorted(names)))
except Exception:
    print("")' 2>/dev/null)" || scraped_authorities=""

  expected_authorities="$(python3 -c "print(','.join(sorted('authority-%d' % n for n in range(1, $ITC_GROUPS + 1))))")"

  if [ "$scraped_authorities" != "$expected_authorities" ]; then
    fail "prometheus is up, but the scraped units are not exactly the ones this rung raises.
  expected: ${expected_authorities}
  scraped:  ${scraped_authorities:-<none>}

  A FOREIGN entry above is a healthy target in this job that does not belong to this rung. The
  extra scrape is trivial; what it implies is not. The PR2 host target only becomes reachable
  when a service is listening on the host's metrics port, and that process is unpinned — free to
  contend with the rehearsal while every cpuset and topology check still passes. Remove it and
  raise the stack with the rehearsal path, which does not probe for it:

      rm -f deploy/observability/targets/alloca-go.json
      make obs-rehearse ITC_CPUS_GENERATOR=$ITC_CPUS_GENERATOR

  A missing entry is the opposite failure: a successful query against an unscraped job returns
  nothing and reports no error, so the cell would complete and retain no series. Regenerate the
  target list and give Prometheus its refresh interval to pick it up:

      ITC_GROUPS=$ITC_GROUPS ./test/scripts/itc-obs-targets.sh
      curl -s $PROM_URL/api/v1/targets | grep -o '\"health\":\"[a-z]*\"'

  Set PROM_URL= to drive a cell deliberately without monitoring."
  fi
  log "prometheus scraping $ITC_GROUPS units: $scraped_authorities"
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

# --- worker population ------------------------------------------------------------------------
#
# `ITC_WORKERS_PER_GROUP` is Iteration C's experiment variable: workers offered to *each* shard
# group, so the run's total is that times the group count (validation plan §4.6.1). `CONCURRENCY`
# stays the run total, which is what the retained `c16`/`c32` cells mean — those were 16 and 32
# total, about 4 and 8 per group at G4, and they must not be read as the new variable.
#
# Both are refused together by alloca-load rather than resolved by precedence here, so a cell that
# meant one and set the other fails instead of silently offering four times the demand.
workers=(-concurrency "$CONCURRENCY")
if [ -n "${ITC_WORKERS_PER_GROUP:-}" ]; then
  workers=(-workers-per-group "$ITC_WORKERS_PER_GROUP")
fi

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

# The fixture as *split*, not merely as seeded. A conditioned cell's measured population owns
# only the slots conditioning did not claim, and every headroom judgement about the cell is made
# against that number rather than against the seeded total.
cat > "$OUT/fixture.txt" <<EOF
SLOTS=$SLOTS
CAPACITY=$CAPACITY
organisations=4
seeded_mutation_supply=$((SLOTS * CAPACITY * 4))
conditioning_slots_per_organisation=$CONDITIONING_SLOTS
conditioning_target_per_organisation=$CONDITIONING_TARGET
measured_mutation_supply=$(((SLOTS - CONDITIONING_SLOTS) * CAPACITY * 4))
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

# **Diagnostic treatment, off by default and deliberately not the canonical fixture behaviour**
# (ag-sept-pr4.md §3.17). The untreated 60-cell population is the baseline this is measured
# against, and silently changing what every cell does would destroy the comparison rather than
# inform it.
#
# **What it can and cannot reach, measured rather than assumed.** After the reseed's TRUNCATE,
# `reltuples` is -1 and `relpages` is 0 on every table, while the per-column `pg_statistic` rows
# from the *previous* cell survive untouched. ANALYZE here repopulates row counts for whatever is
# populated at this instant — which is `slots` and only `slots`. The four tables the workload
# actually grows during the window (reservations, user_identities, user_time_claims,
# idempotency_records) are still empty, so ANALYZE pins them at reltuples=0 and leaves their stale
# column distributions in place: verified directly against a live authority, where analysing the
# empty tables left 10, 8, 7, 2 and 7 stat columns standing.
#
# So a null result here does not clear stale planner state as a cause — it clears *`slots`* stats
# as the cause, and points at the tables that cannot be analysed usefully until they have filled.
ANALYZE_AFTER_SEED="${ANALYZE_AFTER_SEED:-0}"
if [ "$ANALYZE_AFTER_SEED" = "1" ]; then
  log "  ANALYZE after reseed (diagnostic treatment; baseline cells do not have this)"
  for n in $(seq 1 "$ITC_GROUPS"); do
    container="alloca-authority-${n}-db"
    docker exec "$container" psql -U alloca -d alloca -qc "ANALYZE;" >> "$OUT/seed.txt" 2>&1 \
      || fail "ANALYZE_AFTER_SEED=1 was requested but ANALYZE failed on $container; see
  $OUT/seed.txt. A cell that silently skipped the treatment would be recorded as treated."
  done
  # Retained per cell, so a treated cell is identifiable from its own artifacts rather than from
  # whoever remembers which series carried the flag.
  {
    printf 'analyze_after_seed=1\n'
    for n in $(seq 1 "$ITC_GROUPS"); do
      printf -- '--- alloca-authority-%s-db post-ANALYZE planner state ---\n' "$n"
      docker exec "alloca-authority-${n}-db" psql -U alloca -d alloca -tAc \
        "SELECT relname||' reltuples='||(SELECT reltuples::bigint FROM pg_class c WHERE c.oid=s.relid)
                ||' relpages='||(SELECT relpages FROM pg_class c WHERE c.oid=s.relid)
                ||' stat_cols='||(SELECT count(*) FROM pg_statistic st WHERE st.starelid=s.relid)
         FROM pg_stat_user_tables s ORDER BY relname" 2>&1
    done
  } > "$OUT/planner-stats.txt"
fi

# **The state-preserving recycle** (ag-sept-validation-plan.md §4.6.2 step 4).
#
# A restart of the service units, and nothing else. The databases keep running and nothing is
# reseeded or truncated, so the state conditioning established survives exactly as declared —
# what does not survive is the pool, which is the point: PostgreSQL caches a plan per connection,
# and table growth alone does not invalidate one. Only a new connection, made against the
# populated tables, plans the way the measured window needs (§3.20).
#
# Readiness is waited for rather than slept past. A measured window opened against a unit that is
# still starting would attribute its startup to the service under test.
recycle_units() {
  local n port waited
  for n in $(seq 1 "$ITC_GROUPS"); do
    docker restart "alloca-service-${n}" >/dev/null \
      || fail "could not restart alloca-service-${n}; the measured window would open on
  connections still carrying plans prepared against empty mutation tables"
  done

  for n in $(seq 1 "$ITC_GROUPS"); do
    case $n in
      1) port="${SERVICE_1_PORT:-8081}" ;;
      2) port="${SERVICE_2_PORT:-8082}" ;;
      3) port="${SERVICE_3_PORT:-8083}" ;;
      4) port="${SERVICE_4_PORT:-8084}" ;;
    esac
    waited=0
    until curl -fsS -m 2 "http://localhost:${port}/readyz" >/dev/null 2>&1; do
      waited=$((waited + 1))
      [ "$waited" -le "${RECYCLE_READY_TIMEOUT:-60}" ] \
        || fail "alloca-service-${n} did not become ready within ${RECYCLE_READY_TIMEOUT:-60}s
  of the pool recycle; the conditioned state is intact but the cell cannot open a measured
  window against a unit that is not serving"
      sleep 1
    done
  done
}

# Bracketing scrapes. The service counters are cumulative and the units are deliberately left
# running between cells, so only the delta across the measured window describes this cell —
# a single scrape describes everything the process has ever done.
#
# A conditioned cell restarts its units between the `conditioned` and `baseline` scrapes, so the
# measured pair brackets counters that start at zero. That is why the conditioning boundary is
# scraped before the recycle rather than differenced out of the measured pair afterwards.
scrape() {
  local when="$1" n port
  # Announced, because a conditioned cell takes three of these at points whose *order* is the
  # contract — and an operator watching a ten-minute run otherwise sees a long silence between
  # the conditioning phase and the measured one.
  log "  scrape: $when"
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

# --- conditioning phase, boundary scrape, and pool recycle ------------------------------------
#
# The order here is the contract's, and each step is where it is for a reason that is easy to get
# wrong:
#
#  1. conditioning runs against its own slot/identity/key namespace, to a state target counted in
#     committed mutations rather than elapsed time, so G1 and G4 open their measured windows at
#     the same logical state instead of after the same number of seconds;
#  2. the boundary is scraped *before* the recycle. A service restart takes its counters with it,
#     so a scrape taken afterwards cannot describe what conditioning did, and §12.1 requires that
#     baseline to be retained rather than reconstructed by subtraction;
#  3. the recycle restarts the service units only. The databases keep running and nothing is
#     reseeded, so the conditioned state survives exactly as declared while every connection the
#     measured window uses is new and plans against populated tables;
#  4. the measured baseline scrape is taken *after* the restart, because the counters it brackets
#     start at zero there.
conditioned_by=()
conditioning_start=""
conditioning_end=""
recycle_end=""
if [ "$CONDITIONING_TARGET" -gt 0 ]; then
  conditioning_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  [ "$CONDITIONING_SLOTS" -gt 0 ] \
    || fail "CONDITIONING_TARGET=$CONDITIONING_TARGET needs CONDITIONING_SLOTS: the conditioning
  population has to own slots the measured population does not."

  log "  conditioning to $CONDITIONING_TARGET mutations/organisation over $CONDITIONING_SLOTS slots/organisation"
  taskset -c "$ITC_CPUS_GENERATOR" "$LOAD" \
    -placement "$PLACEMENT" \
    "${endpoints[@]}" \
    -deployment "$DEPLOYMENT" \
    -declaration "$DECLARATION" \
    -workload "$WORKLOAD" \
    "${workers[@]}" \
    -conditioning \
    -conditioning-slots "$CONDITIONING_SLOTS" \
    -conditioning-target "$CONDITIONING_TARGET" \
    -slots "$SLOTS" \
    -require "$REQUIRE" \
    -out "$OUT/conditioning.json" 2>&1 | tee "$OUT/conditioning-output.txt"
  status="${PIPESTATUS[0]}"
  [ "$status" -eq 0 ] || fail "conditioning failed or fell short of its declared state target;
  see $OUT/conditioning-output.txt. The measured interval would have opened against a state the
  experiment did not declare."

  conditioning_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  # Before the recycle. This is the only moment conditioning's server-side totals exist.
  scrape conditioned

  log "  recycling the service pool (restart, no reseed) so no measured connection carries a plan
  prepared against empty mutation tables"
  recycle_units
  recycle_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  conditioned_by=(-conditioned-by "$OUT/conditioning.json" -pool-recycled)
fi

scrape baseline

# **Statement-level attribution, diagnostic and off by default** (§3.17). The regime's discriminator
# is 8.8x more logical buffer accesses per request, and a separation that large is attributable to
# a statement rather than to "PostgreSQL". This resets the counters at the window's edge instead of
# taking a delta, so what is dumped afterwards describes this cell and nothing else — the same
# reason the .prom scrapes bracket the window rather than being read once.
#
# Never on for a canonical measurement: pg_stat_statements costs a few percent on the database
# under test, which is exactly the kind of unrecorded perturbation the partition exists to
# prevent. A treated cell records that it was treated, below.
PG_STAT_STATEMENTS="${PG_STAT_STATEMENTS:-0}"
if [ "$PG_STAT_STATEMENTS" = "1" ]; then
  for n in $(seq 1 "$ITC_GROUPS"); do
    docker exec "alloca-authority-${n}-db" psql -U alloca -d alloca -qtAc \
      "SELECT pg_stat_statements_reset() IS NOT NULL;" >/dev/null 2>&1 \
      || fail "PG_STAT_STATEMENTS=1 but pg_stat_statements_reset() failed on authority-$n.
  The extension has to be preloaded and created before the run:
      PG_STAT_STATEMENTS=1 ./test/scripts/itc-series.sh $ITC_GROUPS <cells>"
  done
fi

# **Planner-state probe** (§3.19): what access path the planner would choose for the statement the
# regime is attributed to, sampled through the window so a flip is visible against the throughput
# series rather than inferred from its endpoints.
#
# `EXPLAIN (GENERIC_PLAN)` — PostgreSQL 16 — plans the parameterised statement without values and
# without executing it, which is what makes it safe to run inside a measured window. It is the
# *idempotency lookup* because that is the statement with the largest measured separation
# (11.8 buffers per call healthy against 182.0 degraded, §3.18).
#
# **What it observes and what it does not.** It reports the plan a *fresh* plan would take, given
# the catalog and statistics at that instant, alongside the values that decide it. It does not show
# what the service's pooled connections are currently executing — auto_explain does that, and the
# two answer different halves. Offline probing established that a Seq Scan is chosen only while the
# relation genuinely has no pages, and that the choice reverts as soon as it has some even with
# reltuples still 0, so "the bad plan persists" cannot be assumed and has to be watched.
#
# The probe costs a psql process per sample inside the unit's own cpuset, so it is diagnostic-only
# and deliberately infrequent.
PLAN_PROBE="${PLAN_PROBE:-0}"
probe_pid=""
if [ "$PLAN_PROBE" = "1" ]; then
  : > "$OUT/plan-probe.txt"
  (
    while :; do
      for pn in $(seq 1 "$ITC_GROUPS"); do
        {
          printf '=== %s authority-%s ===\n' "$(date -u +%H:%M:%SZ)" "$pn"
          docker exec "alloca-authority-${pn}-db" psql -U alloca -d alloca -qXtA \
            -c "SELECT 'idempotency_records reltuples='||(SELECT reltuples::bigint FROM pg_class WHERE relname='idempotency_records')
                     ||' relpages='||(SELECT relpages FROM pg_class WHERE relname='idempotency_records')
                     ||' live='||(SELECT n_live_tup FROM pg_stat_user_tables WHERE relname='idempotency_records')
                     ||' dead='||(SELECT n_dead_tup FROM pg_stat_user_tables WHERE relname='idempotency_records')
                     ||' last_autoanalyze='||coalesce(to_char((SELECT last_autoanalyze FROM pg_stat_user_tables WHERE relname='idempotency_records'),'HH24:MI:SS'),'never')" \
            -c "EXPLAIN (GENERIC_PLAN, COSTS ON)
                SELECT user_organisation_id, user_id, operation, key, request_hash, outcome, reason,
                       reservation_id, booking_id, created_at
                FROM idempotency_records
                WHERE user_organisation_id = \$1 AND user_id = \$2 AND operation = \$3 AND key = \$4"
        } >> "$OUT/plan-probe.txt" 2>&1
      done
      sleep "${PLAN_PROBE_INTERVAL:-5}"
    done
  ) &
  probe_pid=$!
  log "  planner-state probe every ${PLAN_PROBE_INTERVAL:-5}s -> $OUT/plan-probe.txt"
fi

if [ -n "${ITC_WORKERS_PER_GROUP:-}" ]; then
  demand="workers/group=$ITC_WORKERS_PER_GROUP total=$((ITC_WORKERS_PER_GROUP * ITC_GROUPS))"
else
  demand="c=$CONCURRENCY total"
fi
if [ "$CONDITIONING_TARGET" -gt 0 ]; then
  phase="conditioned"
else
  phase="UNCONDITIONED — diagnostic only, not a §4.6.5 capacity point"
fi
log "  generator confined to CPUs $ITC_CPUS_GENERATOR ($WORKLOAD $demand window=$WINDOW require=$REQUIRE $phase)"

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
  "${workers[@]}" \
  "${conditioned_by[@]}" \
  -duration "$WINDOW" \
  -slots "$SLOTS" \
  -require "$REQUIRE" \
  -out "$OUT/run.json" 2>&1 | tee "$OUT/generator-output.txt"

# The generator's status, not tee's. `cmd | tee; echo $?` reports tee, so a refused run would be
# reported as a successful one and the whole point of -require would be lost.
status="${PIPESTATUS[0]}"
[ "$status" -eq 0 ] || fail "the run failed or was refused below $REQUIRE; see $OUT/generator-output.txt"

measured_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# **The phase boundaries are retained unconditionally.** They are what makes a server-side
# observation attributable: a plan logged by `auto_explain`, an autovacuum, a checkpoint, all
# carry a timestamp and nothing else says which phase of the cell they fell in. Until now these
# instants were written only when `PG_STAT_STATEMENTS=1` happened to be set, so a cell driven
# without that flag could not answer "was this executed before or after the pool recycle?" —
# which is the whole question conditioning exists to settle (ag-sept-validation-plan.md §4.6.2).
cat > "$OUT/phases.txt" <<EOF
conditioning_start=$conditioning_start
conditioning_end=$conditioning_end
recycle_end=$recycle_end
measured_start=$measured_start
measured_end=$measured_end
EOF

# By PID, never `pkill -f`: the pattern would match this script's own command line and take the
# run down with the probe.
if [ -n "$probe_pid" ]; then
  kill "$probe_pid" 2>/dev/null || true
  wait "$probe_pid" 2>/dev/null || true
fi

# After the run has exited, not before: alloca-load replays ambiguous mutations in a post-run
# pass, and a scrape taken while that is still in flight misses requests the report counts.
scrape after

# Ordered by shared buffer hits, because that is the quantity that separates the regimes — not by
# time, which is the conventional ordering and would rank the answer second. `calls` and `rows`
# travel with it so a statement doing more work per call is distinguishable from one simply called
# more often, which is the whole question.
if [ "$PG_STAT_STATEMENTS" = "1" ]; then
  {
    printf 'pg_stat_statements=1  window %s .. %s\n' "$measured_start" "$measured_end"
    for n in $(seq 1 "$ITC_GROUPS"); do
      printf -- '\n--- authority-%s: top statements by shared_blks_hit ---\n' "$n"
      docker exec "alloca-authority-${n}-db" psql -U alloca -d alloca -qXc \
        "SELECT calls, rows,
                shared_blks_hit  AS blks_hit,
                shared_blks_read AS blks_read,
                round(shared_blks_hit::numeric / GREATEST(calls,1), 1) AS hit_per_call,
                round(total_exec_time::numeric, 1) AS total_ms,
                round(mean_exec_time::numeric, 3)  AS mean_ms,
                left(regexp_replace(query, '\s+', ' ', 'g'), 90) AS statement
         FROM pg_stat_statements
         WHERE calls > 0
         ORDER BY shared_blks_hit DESC LIMIT 12;" 2>&1
    done
  } > "$OUT/pg-statements.txt"
  log "  statement attribution -> $OUT/pg-statements.txt"
fi

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

  # The populated-series gate, extended to the host panels (§2.5). A file existing is not evidence
  # existing: when a job is configured but not scraped every query succeeds and returns nothing,
  # and the exporter writes a header and no rows — which passes an existence check while retaining
  # no series at all.
  #
  # Host panels are named explicitly rather than "every panel", because some are legitimately
  # empty: `replay_rate` has no samples outside the replay control. VAL-NEG-7's host evidence is
  # not in that category — a cell that lost it has lost the thing the sensor was added for, and
  # §3.12 is the worked example of a diagnosis that could not be made without it.
  host_empty="$(python3 -c '
import json, sys
idx = json.load(open(sys.argv[1]))
points = {p["key"]: p["points"] for p in idx["panels"]}
required = ("host_cpu_busy", "host_cpu_steal", "host_runqueue", "host_memory_available")
print(",".join(k for k in required if points.get(k, 0) == 0))' "$OUT/panels/index.json" 2>/dev/null)" \
    || host_empty="index unreadable"

  [ -z "$host_empty" ] || fail "the cell retained no host samples for: $host_empty
  node_exporter is VAL-NEG-7's host sensor and the instrument the degraded-regime diagnosis needs
  (ag-sept-pr4.md §2.1, §3.12). A cell without it cannot separate a stall in the service from one
  in the machine under it, which is the whole question. Check the host job is scraping:

      curl -s $PROM_URL/api/v1/targets | grep -A2 '\"job\":\"node\"'

  Raise the stack with 'make obs-rehearse', which starts node-exporter beside Prometheus."
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

# The *measured* population's supply, not the whole fixture's. Conditioning claims its slots and
# spends their capacity, so a conditioned cell that used the seeded total here would credit itself
# with headroom that another phase already consumed — and the fixture-headroom gate exists
# precisely to catch a rung that ran out of state (measurement-contract §5).
supply=$(((SLOTS - CONDITIONING_SLOTS) * CAPACITY * 4))
if [ "${admitted:-0}" -ge "$supply" ] && [ "$supply" -gt 0 ]; then
  echo >&2
  echo "!! this cell exhausted its fixture: ${admitted} admitted against a supply of ${supply}." >&2
  echo "   It certifies, and it backs no capacity number — what it measured after exhaustion is" >&2
  echo "   refusal throughput (measurement-contract §5). Raise SLOTS and re-run before quoting" >&2
  echo "   anything, and remember an exhausted rung also invalidates the rung below it." >&2
fi

log "cell complete -> $OUT/run.json  (${admitted:-?} admitted of ${supply} supplied)"
