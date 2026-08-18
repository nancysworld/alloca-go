#!/usr/bin/env bash
#
# Asserts that a conditioned cell sequences its phases the way the measurement contract requires.
#
# The sequence is the property, and the sequence is where it goes wrong quietly. Every ordering
# below produces a cell that completes, certifies, and retains a full set of artifacts; what
# changes is whether those artifacts describe the experiment:
#
#   - conditioning after the baseline scrape would fold its traffic into the measured counters;
#   - the boundary scrape after the recycle would lose conditioning's server totals entirely,
#     because a restarted unit's counters begin at zero (measurement-contract §12.1);
#   - the baseline scrape before the recycle would bracket the measured window against counters
#     the restart then reset, so every per-unit delta would be wrong by the whole conditioning
#     phase;
#   - a reseed between conditioning and measurement would discard the state conditioning existed
#     to establish, restoring the empty-table planner regime the phase exists to remove.
#
# None of that is observable from a finished cell, so it is checked here by running the real
# script against stubs that record what it invoked and in what order.
#
# Needs no daemon and no topology: `docker`, `curl`, `taskset` and the generator are stubs, the
# preflight helpers are replaced, and the run is driven inside a clean clone so the tree gate
# passes. It is therefore a merge gate.
set -euo pipefail

cd "$(dirname "$0")/../.."
repo="$PWD"

pass=0
fail=0
ok()  { echo "  ok    $1"; pass=$((pass + 1)); }
bad() { echo "  FAIL  $1" >&2; fail=$((fail + 1)); }

echo "itc-conditioning-test: phase sequencing"

work="$(mktemp -d)"
# Kept on request, because the whole evidence of this test is a transcript of what the cell
# invoked and in what order: ITC_KEEP_TRACE=1 leaves it to be read.
if [ -n "${ITC_KEEP_TRACE:-}" ]; then
  trap 'echo "trace: $work/run.log" >&2' EXIT
else
  trap 'rm -rf "$work"' EXIT
fi

# A clone rather than the working tree: itc-run.sh refuses a dirty tree, and this test must not
# depend on the state of the checkout it is run from.
git clone -q --no-hardlinks "$repo" "$work/repo" 2>/dev/null || { echo "  FAIL  could not clone" >&2; exit 1; }
cd "$work/repo"

# The clone carries *committed* scripts, so the working tree's are laid over it. Without this the
# test silently exercises the last commit rather than the change under review — which it did on
# its first run, passing the cell through the unconditioned path and reporting the absence of
# every step it exists to check.
cp "$repo"/test/scripts/*.sh test/scripts/

# The run log is the transcript. The stubs report on stderr and the script logs on stdout, and
# the cell is driven with both into one file, so the order in it is the order things happened —
# rather than two files whose interleaving would have to be guessed at.
trace="$work/run.log"

# --- stubs --------------------------------------------------------------------------------
mkdir -p "$work/bin"

# Reported twice, deliberately. The transcript on stderr is what gives ordering, but a caller
# can redirect a stub's output away — and a mutation that restarted the databases with
# `>/dev/null 2>&1` did exactly that, passing the scope assertion below because the call it
# made was invisible. The file is appended by the stub itself, so no redirection at the call
# site can hide it.
cat > "$work/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >&2
echo "docker \$*" >> "$work/docker-calls.txt"
exit 0
EOF

# Every readiness poll succeeds immediately; scrapes write a plausible exposition so the cell's
# own file checks pass.
cat > "$work/bin/curl" <<EOF
#!/usr/bin/env bash
echo "curl \$*" >&2

# Prometheus is deliberately unreachable: this cell is driven without monitoring, and a stub
# that answered its readiness probe would send the script down the "monitoring is up but
# scraping nothing" path instead.
case "\$*" in
  */-/ready*|*api/v1/targets*) exit 7 ;;
esac

out=""
prev=""
for a in "\$@"; do
  if [ "\$prev" = "-o" ]; then out="\$a"; fi
  prev="\$a"
done
[ -n "\$out" ] && printf 'alloca_requests_total 1\n' > "\$out"
exit 0
EOF

cat > "$work/bin/taskset" <<EOF
#!/usr/bin/env bash
shift 2
exec "\$@"
EOF

cat > "$work/bin/nproc" <<'EOF'
#!/usr/bin/env bash
echo 16
EOF

chmod +x "$work/bin/"*

# The generator stub records its phase and writes a report the script can read back. Goodput is
# reported as the declared target exactly, so a conditioning phase reaches its state target and
# the measured run's lineage check has something real to accept.
mkdir -p bin
cat > bin/alloca-load <<EOF
#!/usr/bin/env bash
phase=measured
out=""
target=0
slots=0
groups=0
prev=""
for a in "\$@"; do
  case "\$prev" in
    -out) out="\$a" ;;
    -conditioning-target) target="\$a" ;;
    -conditioning-slots) slots="\$a" ;;
  esac
  case "\$a" in
    -conditioning) phase=conditioning ;;
    -conditioned-by) phase=measured-conditioned ;;
  esac
  prev="\$a"
done
echo "alloca-load phase=\$phase args=\$*" >&2

if [ "\$phase" = conditioning ]; then
  cat > "\$out" <<JSON
{"manifest":{"run_id":"run-stub","workload":"wl-mut-disp-4","phase":"conditioning",
 "conditioning":{"slots_per_organisation":\$slots,"target_mutations_per_organisation":\$target,
 "organisations":4}},
 "summary":{"measurement_sound":true,"successful_mutation_goodput":\$((target * 4)),
 "completed_requests":\$((target * 4)),
 "totals":[{"operation":"reserve","outcome":"admitted_success","count":\$((target * 4))}]}}
JSON
else
  cat > "\$out" <<JSON
{"manifest":{"run_id":"run-stub","workload":"wl-mut-disp-4"},
 "summary":{"measurement_sound":true,"successful_mutation_goodput":10,"completed_requests":10,
 "totals":[{"operation":"reserve","outcome":"admitted_success","count":10}]}}
JSON
fi
exit 0
EOF
chmod +x bin/alloca-load

# The deployment record is observed, not tracked, so the clone has none. It is written here
# rather than stubbed away because the run has to carry an image identity to certify at all.
mkdir -p test/observed
cat > test/observed/deployment.json <<'EOF'
{
  "image_id": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
  "image_tag": "alloca-go:test",
  "units": {
    "alloca-service-1": {"image_id": "sha256:0000000000000000000000000000000000000000000000000000000000000000", "target": "http://localhost:8081"},
    "alloca-service-2": {"image_id": "sha256:0000000000000000000000000000000000000000000000000000000000000000", "target": "http://localhost:8082"}
  }
}
EOF

# Preflight helpers and the seeder are replaced: each needs a live topology, and none of them is
# what this test is about.
for helper in itc-cpu-layout.sh itc-topology-check.sh itc-cpuset-check.sh; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "test/scripts/$helper"
  chmod +x "test/scripts/$helper"
done
cat > test/scripts/itc-seed.sh <<EOF
#!/usr/bin/env bash
echo "reseed" >&2
exit 0
EOF
chmod +x test/scripts/itc-seed.sh

# The stubs make the clone dirty, and itc-run.sh refuses a dirty tree — correctly, since Go
# stamps an unclean tree as modified and the run would certify at nothing. Committing them inside
# the throwaway clone satisfies the gate without weakening it: the gate is still running, against
# a tree that is genuinely clean.
git -C . add -A >/dev/null 2>&1
git -C . -c user.email=test@example.com -c user.name=test commit -qm "stubs" >/dev/null 2>&1

# --- drive one conditioned cell -------------------------------------------------------------
set +e
PATH="$work/bin:$PATH" \
  ITC_GROUPS=2 PROM_URL= REQUIRE=local \
  SLOTS=100 CAPACITY=20 \
  CONDITIONING_SLOTS=10 CONDITIONING_TARGET=25 \
  ITC_WORKERS_PER_GROUP=4 WINDOW=1s \
  OUT="$work/cell" \
  ./test/scripts/itc-run.sh > "$trace" 2>&1
run_status=$?
set -e

if [ "$run_status" -ne 0 ]; then
  echo "  FAIL  the conditioned cell did not complete (exit $run_status)" >&2
  sed -n '1,40p' "$trace" >&2
  exit 1
fi
ok "a conditioned cell completes"

# --- the order ------------------------------------------------------------------------------
#
# Recorded positions rather than a diff of the whole trace, so the assertions name the property
# that failed instead of printing two transcripts and leaving the reader to compare them.
# `|| true` matters: with pipefail a step the cell never performed would abort this test
# instead of reporting which step was missing, which is the one thing it exists to say.
pos() { grep -n "$1" "$trace" 2>/dev/null | head -1 | cut -d: -f1 || true; }

reseed=$(pos 'reseed')
cond=$(pos 'alloca-load phase=conditioning')
boundary=$(pos 'scrape: conditioned')
restart=$(pos 'docker restart')
baseline=$(pos 'scrape: baseline')
measured=$(pos 'alloca-load phase=measured-conditioned')

missing=0
for step in reseed cond boundary restart baseline measured; do
  if [ -z "${!step}" ]; then
    bad "the cell never performed '$step'"
    missing=1
  fi
done
if [ "$missing" = 1 ]; then
  echo "--- trace ---" >&2
  cat "$trace" >&2
fi

ordered() { # description, earlier, later
  if [ -n "$2" ] && [ -n "$3" ] && [ "$2" -lt "$3" ]; then ok "$1"; else bad "$1 (positions $2, $3)"; fi
}

ordered "conditioning runs after the reseed" "$reseed" "$cond"
ordered "the boundary is scraped after conditioning" "$cond" "$boundary"
ordered "the boundary is scraped BEFORE the recycle, while those counters still exist" "$boundary" "$restart"
ordered "the measured baseline is scraped AFTER the recycle, when counters restart at zero" "$restart" "$baseline"
ordered "the measured run opens after the baseline scrape" "$baseline" "$measured"

# The recycle must not reseed. A reseed here would truncate away the state conditioning
# established, restoring exactly the empty-table planner regime the phase exists to remove.
# Counted from the script's own log line, not the seeder stub: itc-run.sh sends the seeder's
# output to seed.txt, so the stub's marker never reaches this transcript.
reseeds="$(grep -c 'reseeding:' "$trace" || true)"
if [ "$reseeds" = "1" ]; then
  ok "the fixture is seeded once, not again between conditioning and measurement"
else
  bad "the cell reseeded $reseeds times; a reseed after conditioning discards the state it established"
fi

# The recycle restarts the services and leaves the databases alone, or the conditioned state
# would not survive the transition it exists to survive.
if grep -q 'docker restart alloca-service-' "$work/docker-calls.txt" \
   && ! grep -q 'docker restart alloca-authority' "$work/docker-calls.txt"; then
  ok "the recycle restarts service units only, leaving the conditioned database untouched"
else
  bad "the recycle touched a database container; conditioned state must survive it"
fi

# --- lineage and demand reach the measured run ------------------------------------------------
measured_args="$(grep 'alloca-load phase=measured-conditioned' "$trace" | head -1)"
case "$measured_args" in
  *"-conditioned-by $work/cell/conditioning.json"*) ok "the measured run names the conditioning artifact it began from" ;;
  *) bad "the measured run does not carry -conditioned-by" ;;
esac
case "$measured_args" in
  *-pool-recycled*) ok "the measured run records that the pool was recycled" ;;
  *) bad "the measured run does not record the pool recycle" ;;
esac
case "$measured_args" in
  *"-workers-per-group 4"*) ok "the measured run carries workers-per-group, not a total" ;;
  *) bad "ITC_WORKERS_PER_GROUP did not reach the measured run" ;;
esac
case "$measured_args" in
  *-concurrency*) bad "the measured run carries both -concurrency and -workers-per-group" ;;
  *) ok "the measured run does not also pass a total-worker flag" ;;
esac

# --- the fixture the cell reports is the one the measured phase owned --------------------------
if grep -q '^measured_mutation_supply=7200$' "$work/cell/fixture.txt"; then
  ok "the retained fixture accounting excludes the slots conditioning claimed"
else
  bad "measured_mutation_supply is not (100-10)*20*4=7200: $(grep measured_mutation_supply "$work/cell/fixture.txt" || echo missing)"
fi

# --- a target without slots is refused --------------------------------------------------------
set +e
PATH="$work/bin:$PATH" \
  ITC_GROUPS=2 PROM_URL= REQUIRE=local SLOTS=100 CAPACITY=20 \
  CONDITIONING_TARGET=25 WINDOW=1s OUT="$work/cell2" \
  ./test/scripts/itc-run.sh > "$work/run2.log" 2>&1
refused=$?
set -e
if [ "$refused" -ne 0 ] && grep -q "CONDITIONING_SLOTS" "$work/run2.log"; then
  ok "a conditioning target with no slots of its own is refused"
else
  bad "a conditioning phase with no slot population was accepted (exit $refused)"
fi

echo
echo "itc-conditioning-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
