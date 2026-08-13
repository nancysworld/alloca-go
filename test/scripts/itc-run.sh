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
WARMUP="${WARMUP:-20s}"
SLOTS="${SLOTS:-200}"
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

log "G$ITC_GROUPS cell -> $OUT"
log "  generator confined to CPUs $ITC_CPUS_GENERATOR ($WORKLOAD c=$CONCURRENCY window=$WINDOW require=$REQUIRE)"

taskset -c "$ITC_CPUS_GENERATOR" "$LOAD" \
  -placement "$PLACEMENT" \
  "${endpoints[@]}" \
  -deployment "$DEPLOYMENT" \
  -declaration "$DECLARATION" \
  -workload "$WORKLOAD" \
  -concurrency "$CONCURRENCY" \
  -duration "$WINDOW" \
  -warm-up "$WARMUP" \
  -slots "$SLOTS" \
  -require "$REQUIRE" \
  -out "$OUT/run.json" 2>&1 | tee "$OUT/generator-output.txt"

# The generator's status, not tee's. `cmd | tee; echo $?` reports tee, so a refused run would be
# reported as a successful one and the whole point of -require would be lost.
status="${PIPESTATUS[0]}"
[ "$status" -eq 0 ] || fail "the run failed or was refused below $REQUIRE; see $OUT/generator-output.txt"

log "cell complete -> $OUT/run.json"
