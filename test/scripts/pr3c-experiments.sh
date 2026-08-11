#!/usr/bin/env bash
#
# Drive the AG-Sept PR3c experiments against the running two-authority topology.
#
# The cells are the minimum experiment matrix of
# `docs/test/validation-plan/ag-sept-validation-plan.md` §4.5, and each one is a *sequence*
# rather than a load run:
#
#   seed -> baseline scrape per unit -> load -> (fault) -> after scrape per unit -> verify
#
# Every step in it is there because of a failure this harness has already met:
#
#   * **each unit is scraped on its own**, before and after. Counters are cumulative, and
#     `alloca-verify` differences each unit's pair separately before summing them — summing
#     first would let one unit restarting mid-run vanish into the other's counters;
#   * **the after-scrape is taken once the generator has exited**, not when the workload ends.
#     `alloca-load` replays ambiguous mutations after the measured interval, and a scrape taken
#     before it exits misses exactly those requests;
#   * **the fixture is re-seeded per cell**, because a cell that starts on a used fixture
#     measures capacity exhaustion and passes every gate while doing it;
#   * **verification runs immediately**, because unconfirmed holds expire on the reservation
#     TTL and live reservations decay after the run.
#
# Hand-driving this is what the script exists to prevent, and the failure cell is the reason:
# its fault has to land inside the measured window, and its authority has to be back *before
# the run ends* or the resolution pass cannot settle anything.
#
# Usage:
#
#   test/scripts/pr3c-experiments.sh controls      # VAL-COR-2, VAL-COR-3, VAL-COR-5
#   test/scripts/pr3c-experiments.sh correctness   # VAL-COR-1, VAL-COR-2
#   test/scripts/pr3c-experiments.sh distribution  # one-hot organisation
#   test/scripts/pr3c-experiments.sh refusal       # VAL-COR-4, its own evidence class
#   test/scripts/pr3c-experiments.sh failure       # VAL-FAIL-1, VAL-COR-6 (needs docker)
#   test/scripts/pr3c-experiments.sh all
#
# It needs the topology up (`make topo-up`) and a deployment record
# (`make topo-deployment > test/results/deployment.json`).
set -euo pipefail

S1="${S1:-http://localhost:8081}"
S2="${S2:-http://localhost:8082}"
M1="${M1:-http://localhost:9091/metrics}"
M2="${M2:-http://localhost:9092/metrics}"
A1_DSN="${A1_DSN:-postgres://alloca:alloca@localhost:15433/alloca?sslmode=disable}"
A2_DSN="${A2_DSN:-postgres://alloca:alloca@localhost:15434/alloca?sslmode=disable}"
PLACEMENT="${PLACEMENT:-deploy/topology/placement.json}"
DEPLOYMENT="${DEPLOYMENT:-test/results/deployment.json}"
OUT="${OUT:-test/results/pr3c-$(date -u +%Y%m%dT%H%M%SZ)}"

SLOTS="${SLOTS:-1200}"
CONCURRENCY="${CONCURRENCY:-8}"
ITERATIONS="${ITERATIONS:-2000}"
WINDOW="${WINDOW:-40s}"
FAULT_AFTER="${FAULT_AFTER:-10}"        # seconds into the window before the authority stops
FAULT_FOR="${FAULT_FOR:-15}"            # seconds it stays down; must end inside the window
FAULT_CONTAINER="${FAULT_CONTAINER:-alloca-authority-2-db}"

LOAD=bin/alloca-load
VERIFY=bin/alloca-verify

log()  { printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { printf '\n!! %s\n' "$*" >&2; exit 1; }

# --- preflight ---------------------------------------------------------------------------
#
# Checked up front rather than discovered by a verdict of `level: none` after the runs. The two
# stamp checks fail for unrelated reasons and both are silent until certification: the generator
# is stamped by the tree it was built from, the service by the build context its image was built
# from (see `docs/operations/container-topology.md` §2 for the client that gets that wrong).
preflight() {
  [ -x "$LOAD" ]   || fail "build the generator: go build -o $LOAD ./cmd/alloca-load"
  [ -x "$VERIFY" ] || fail "build the verifier: go build -o $VERIFY ./cmd/alloca-verify"
  [ -f "$PLACEMENT" ] || fail "no placement document at $PLACEMENT"
  [ -f "$DEPLOYMENT" ] || fail "no deployment record at $DEPLOYMENT — run 'make topo-deployment > $DEPLOYMENT'"

  go version -m "$LOAD" | grep -q 'vcs.modified=false' \
    || fail "$LOAD is stamped vcs.modified=true: every run would be refused at 'local' for generator_source_modified"

  local unit
  for unit in "$S1" "$S2"; do
    curl -fsS -m 5 "$unit/readyz" >/dev/null || fail "$unit is not ready"
    curl -fsS -m 5 "$unit/meta" | grep -q '"modified":false' \
      || fail "$unit reports a modified build: its image cannot certify a run (container-topology.md §2)"
  done

  mkdir -p "$OUT"
  log "artifacts -> $OUT"
}

# --- fixture -----------------------------------------------------------------------------
#
# -reset truncates the whole authority, so it goes on the FIRST organisation of each one and
# nowhere else. Passing it on every call deletes the previous organisation's slots and leaves a
# run whose missing half comes back as ordinary-looking `unknown_target` refusals.
seed() {
  log "seeding $SLOTS slots per organisation"
  go run ./cmd/alloca-seed -reset -database-url "$A1_DSN" -org org-a -slots "$SLOTS" >/dev/null
  go run ./cmd/alloca-seed        -database-url "$A1_DSN" -org org-c -slots "$SLOTS" >/dev/null
  go run ./cmd/alloca-seed -reset -database-url "$A2_DSN" -org org-b -slots "$SLOTS" >/dev/null
  go run ./cmd/alloca-seed        -database-url "$A2_DSN" -org org-d -slots "$SLOTS" >/dev/null
}

scrape() { curl -fsS -m 10 "$1" -o "$2"; }

# verify reconciles one cell against both authorities and keeps the verdict beside its run.
#
# -require none, deliberately: the level a run reaches belongs in its own artifact, and a cell
# that fails certification must still leave a verdict explaining why rather than exiting before
# it is written.
verify() {
  local dir="$1"
  "$VERIFY" \
    -run "$dir/run.json" \
    -placement "$PLACEMENT" \
    -authority-db "authority-1=$A1_DSN" \
    -authority-db "authority-2=$A2_DSN" \
    -authority-metrics authority-1="$dir/s1-after.prom" \
    -authority-metrics authority-2="$dir/s2-after.prom" \
    -authority-metrics-baseline authority-1="$dir/s1-baseline.prom" \
    -authority-metrics-baseline authority-2="$dir/s2-baseline.prom" \
    -require none \
    -out "$dir/verdict.json"
  if grep -q '"ok": false' "$dir/verdict.json"; then
    fail "$dir: a reconciliation check failed; read $dir/verdict.json"
  fi
  log "$dir: every check passed, level $(grep -o '"level": *"[^"]*"' "$dir/verdict.json" | head -1 | cut -d'"' -f4)"
}

# cell runs one bounded load cell end to end. $1 names it, the rest are generator arguments.
cell() {
  local name="$1"; shift
  local dir="$OUT/$name"
  mkdir -p "$dir"
  seed

  scrape "$M1" "$dir/s1-baseline.prom"
  scrape "$M2" "$dir/s2-baseline.prom"

  log "$name: driving load"
  "$LOAD" \
    -placement "$PLACEMENT" \
    -endpoint "authority-1=$S1" -endpoint "authority-2=$S2" \
    -deployment "$DEPLOYMENT" \
    -slots "$SLOTS" -concurrency "$CONCURRENCY" \
    -require none -out "$dir/run.json" \
    "$@" 2>&1 | tee "$dir/generator.log"

  scrape "$M1" "$dir/s1-after.prom"
  scrape "$M2" "$dir/s2-after.prom"
  verify "$dir"
}

# --- controls ------------------------------------------------------------------------------
#
# Four routing behaviours and the ownership check, asserted rather than eyeballed. Each is a
# property the topology exists to have, and three of them look alike from a distance: a
# cross-authority refusal is policy (409), a misroute is the edge (400), and an ownership
# mismatch is a domain answer about a reservation that exists (404).
post() { # post <url> <key> <body> -> "<status> <body>"
  local body="${TMPDIR:-/tmp}/pr3c-body.$$"
  curl -sS -o "$body" -w '%{http_code}' -X POST "$1" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $2" -d "$3"
  printf ' '; cat "$body"; rm -f "$body"
}

expect() { # expect <what> <got> <status> [<substring>]
  local what="$1" got="$2" status="$3" want="${4:-}"
  case "$got" in
    "$status"*) ;;
    *) fail "$what: got [$got], wanted status $status" ;;
  esac
  if [ -n "$want" ] && [[ "$got" != *"$want"* ]]; then
    fail "$what: got [$got], wanted it to carry $want"
  fi
  log "$what: ok ($status${want:+, $want})"
}

controls() {
  local dir="$OUT/controls"
  mkdir -p "$dir"
  seed
  local stamp; stamp="$(date -u +%s)"

  # 1. same organisation, own authority.
  expect "same-organisation booking" \
    "$(post "$S1/v1/slots/org-a/slot-0/reservations" "c1-$stamp" '{"user_organisation_id":"org-a","user_id":"c1"}')" \
    200 admitted_success

  # 2. colocated cross-organisation booking (VAL-COR-2). A *different* user than case 1: the
  #    seeded slot-0 of each organisation covers nearly the same hour, so reusing the identity
  #    returns a correct schedule_conflict that reads as a broken topology.
  expect "colocated cross-organisation booking" \
    "$(post "$S1/v1/slots/org-c/slot-0/reservations" "c2-$stamp" '{"user_organisation_id":"org-a","user_id":"c2"}')" \
    200 admitted_success

  # 3. cross-authority booking: policy, at the user's own unit (VAL-COR-4).
  expect "cross-authority refusal" \
    "$(post "$S1/v1/slots/org-b/slot-0/reservations" "c3-$stamp" '{"user_organisation_id":"org-a","user_id":"c3"}')" \
    409 cross_authority_unsupported

  # 4. misroute: the edge, at a unit that does not own the user's organisation (VAL-COR-5).
  local before after
  before="$(curl -fsS "$M1" | awk '/^alloca_placement_misrouted_requests_total/ {n+=$2} END {print n+0}')"
  expect "misrouted request" \
    "$(post "$S1/v1/slots/org-b/slot-0/reservations" "c4-$stamp" '{"user_organisation_id":"org-b","user_id":"c4"}')" \
    400 invalid_request
  after="$(curl -fsS "$M1" | awk '/^alloca_placement_misrouted_requests_total/ {n+=$2} END {print n+0}')"
  [ "$after" -gt "$before" ] || fail "the misroute counter did not move ($before -> $after): a 400 that is really a misconfiguration must not hide among client errors"
  log "misroute counter: $before -> $after"

  # 5. ownership on confirm and cancel (VAL-COR-3). The reservation exists and the caller is
  #    not its owner, which is a domain answer rather than a routing one.
  local reserved id
  reserved="$(post "$S1/v1/slots/org-a/slot-1/reservations" "c5-$stamp" '{"user_organisation_id":"org-a","user_id":"c5"}')"
  expect "reservation for the ownership control" "$reserved" 200 admitted_success
  id="$(printf '%s' "$reserved" | sed -n 's/.*"reservation_id":"\([^"]*\)".*/\1/p')"
  [ -n "$id" ] || fail "could not read a reservation_id out of [$reserved]"

  expect "confirm with a wrong UserRef" \
    "$(post "$S1/v1/reservations/$id/confirm" "c6-$stamp" '{"user_organisation_id":"org-a","user_id":"not-the-owner"}')" \
    404 unknown_target
  expect "cancel with a wrong UserRef" \
    "$(post "$S1/v1/reservations/$id/cancel" "c7-$stamp" '{"user_organisation_id":"org-a","user_id":"not-the-owner"}')" \
    404 unknown_target

  # The owner still holds it: the refusals above must not have consumed the reservation.
  expect "the owner can still confirm" \
    "$(post "$S1/v1/reservations/$id/confirm" "c8-$stamp" '{"user_organisation_id":"org-a","user_id":"c5"}')" \
    200 admitted_success

  log "controls: all assertions passed" | tee "$dir/controls.log"
}

# --- failure isolation ----------------------------------------------------------------------
#
# One authority is stopped inside the measured window and started again *inside the same
# window*. The second half is not politeness: `alloca-load` replays ambiguous mutations when
# the workload ends, and a replay that never reaches the service settles nothing — so a run
# restored afterwards is refused as unresolved, which is the correct answer to the wrong
# experiment.
#
# The fault is a *stopped container*: it drops packets rather than refusing them. A killed
# container or a database refusing connections classifies differently, and any report of this
# cell must name which one it injected (ag-sept-pr3.md §6b).
failure() {
  local dir="$OUT/failure-isolation"
  mkdir -p "$dir"
  seed

  scrape "$M1" "$dir/s1-baseline.prom"
  scrape "$M2" "$dir/s2-baseline.prom"

  log "failure: driving $WINDOW of load; $FAULT_CONTAINER stops at +${FAULT_AFTER}s for ${FAULT_FOR}s"
  "$LOAD" \
    -placement "$PLACEMENT" \
    -endpoint "authority-1=$S1" -endpoint "authority-2=$S2" \
    -deployment "$DEPLOYMENT" \
    -workload multi-org-dispersed \
    -slots "$SLOTS" -concurrency "$CONCURRENCY" -duration "$WINDOW" \
    -require none -out "$dir/run.json" > "$dir/generator.log" 2>&1 &
  local load_pid=$!

  sleep "$FAULT_AFTER"
  docker stop "$FAULT_CONTAINER" >/dev/null
  log "failure: $FAULT_CONTAINER stopped"
  # Isolation, observed while it is actually down: the affected unit must report itself
  # unready and its peer must not.
  {
    printf 'unit-1 readyz: %s\n' "$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$S1/readyz")"
    printf 'unit-2 readyz: %s\n' "$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$S2/readyz")"
  } | tee "$dir/readiness-during-fault.txt"

  sleep "$FAULT_FOR"
  docker start "$FAULT_CONTAINER" >/dev/null
  log "failure: $FAULT_CONTAINER started; waiting for its unit to report ready"
  local waited=0
  while [ "$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$S2/readyz")" != "200" ]; do
    waited=$((waited + 1))
    [ "$waited" -lt 60 ] || fail "$FAULT_CONTAINER came back but its unit never reported ready"
    sleep 1
  done

  wait "$load_pid" || fail "the generator exited non-zero; read $dir/generator.log"
  scrape "$M1" "$dir/s1-after.prom"
  scrape "$M2" "$dir/s2-after.prom"
  verify "$dir"
}

case "${1:-all}" in
  controls)     preflight; controls ;;
  correctness)  preflight; cell correctness  -workload multi-org-dispersed -n "$ITERATIONS" ;;
  distribution) preflight; cell distribution -workload hot-organisation    -n "$ITERATIONS" ;;
  refusal)      preflight; cell refusal      -workload cross-authority-control -n "$ITERATIONS" ;;
  failure)      preflight; failure ;;
  all)
    preflight
    controls
    cell correctness  -workload multi-org-dispersed      -n "$ITERATIONS"
    cell distribution -workload hot-organisation         -n "$ITERATIONS"
    cell refusal      -workload cross-authority-control  -n "$ITERATIONS"
    failure
    ;;
  *) fail "unknown cell ${1}: want controls | correctness | distribution | refusal | failure | all" ;;
esac

log "done; artifacts in $OUT"
