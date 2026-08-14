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
#   test/scripts/pr3c-experiments.sh controls      # VAL-COR-2, VAL-COR-3, VAL-COR-4 (replay), VAL-COR-5
#   test/scripts/pr3c-experiments.sh correctness   # VAL-COR-1, VAL-COR-2
#   test/scripts/pr3c-experiments.sh distribution  # one-hot organisation
#   test/scripts/pr3c-experiments.sh refusal       # VAL-COR-4, its own evidence class
#   test/scripts/pr3c-experiments.sh failure       # VAL-FAIL-1, VAL-COR-6 (needs docker)
#   test/scripts/pr3c-experiments.sh all
#
# It needs the topology up (`make topo-up`) and a deployment record
# (`make topo-deployment > test/fixtures/deployment.json`).
set -euo pipefail

S1="${S1:-http://localhost:8081}"
S2="${S2:-http://localhost:8082}"
M1="${M1:-http://localhost:9081/metrics}"
M2="${M2:-http://localhost:9082/metrics}"
A1_DSN="${A1_DSN:-postgres://alloca:alloca@localhost:15433/alloca?sslmode=disable}"
A2_DSN="${A2_DSN:-postgres://alloca:alloca@localhost:15434/alloca?sslmode=disable}"
PLACEMENT="${PLACEMENT:-deploy/topology/placement.json}"
DEPLOYMENT="${DEPLOYMENT:-test/fixtures/deployment.json}"
# See itc-run.sh for why runs nest under a group rather than sitting flat.
RESULTS_GROUP="${RESULTS_GROUP:-pr3c}"
OUT="${OUT:-test/results/$RESULTS_GROUP/$(date -u +%Y%m%dT%H%M%SZ)}"

SLOTS="${SLOTS:-1200}"
# The one-hot cell names the organisation that carries the whole load, and the default is not
# usable: `-org` defaults to `load-org`, which no placement in this topology mentions, so the
# run is refused before it starts rather than silently spreading.
HOT_ORG="${HOT_ORG:-org-a}"
CONCURRENCY="${CONCURRENCY:-8}"
ITERATIONS="${ITERATIONS:-2000}"
# Seconds, not a duration string, because the script has to *compare* the window against a clock
# and not merely pass it to the generator: the failure cell's claim is that the authority came
# back before the measured window closed, and that is arithmetic.
WINDOW_SECONDS="${WINDOW_SECONDS:-40}"
FAULT_AFTER="${FAULT_AFTER:-10}"        # seconds into the window before the authority stops
# Seconds the authority is held down *after* isolation has been asserted — not the whole outage,
# which also covers the assertion and so runs a second or two longer. The restoration must still
# land inside the window.
FAULT_FOR="${FAULT_FOR:-15}"
# How long the affected unit may take to report itself unready. Readiness is a live database
# probe, so `503` follows the fault by up to the unit's readiness timeout rather than instantly.
FAULT_ISOLATION_WAIT="${FAULT_ISOLATION_WAIT:-10}"
# Resolved from FAULT_CONTAINER by fault_roles, and used by every step that has to tell the
# failing authority from its healthy peer.
AFFECTED_URL=""; PEER_URL=""; AFFECTED_NAME=""; PEER_NAME=""
FAULT_CONTAINER="${FAULT_CONTAINER:-alloca-authority-2-db}"
# The level every cell must reach for its numbers to mean anything.
#
# `local` is the floor of the ladder, and a floor rather than a target: a run that describes
# enough to certify higher is not held back by it. It is **not** the ceiling for a co-resident
# generator — co-residency blocks `publishable` alone (`measurement-contract.md` §13), and these
# cells stop at `local` because they are correctness experiments whose manifests carry none of
# the operator-supplied fields a `capacity` claim needs.
REQUIRE="${REQUIRE:-local}"

LOAD=bin/alloca-load
VERIFY=bin/alloca-verify

# Every line the run prints is also retained beside the artifacts it describes. The controls
# are the reason: their evidence *is* the sequence of assertions, and a summary line saying
# they passed is not something a later reader can check anything against.
RUNLOG=""
log()  {
  printf '%s  %s\n' "$(date -u +%H:%M:%S)" "$*"
  [ -n "$RUNLOG" ] && printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*" >> "$RUNLOG"
  return 0
}
fail() {
  printf '\n!! %s\n' "$*" >&2
  [ -n "$RUNLOG" ] && printf 'FAILED: %s\n' "$*" >> "$RUNLOG"
  exit 1
}

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

  # Both binaries, for different reasons. The generator's stamp is *carried into the report*,
  # so a modified one is refused at `local` by certification. The verifier's is not carried
  # anywhere — nothing downstream would notice — and it is the binary that decides whether every
  # cell reconciled, so a stale or locally modified verifier can certify the whole matrix while
  # this script reports success. That asymmetry is exactly why it needs the check *here*.
  go version -m "$LOAD" | grep -q 'vcs.modified=false' \
    || fail "$LOAD is stamped vcs.modified=true: every run would be refused at 'local' for generator_source_modified"
  go version -m "$VERIFY" | grep -q 'vcs.modified=false' \
    || fail "$VERIFY is stamped vcs.modified=true: its verdicts would be produced by a binary this repository cannot identify, and nothing downstream would report that"

  local unit
  for unit in "$S1" "$S2"; do
    curl -fsS -m 5 "$unit/readyz" >/dev/null || fail "$unit is not ready"
    curl -fsS -m 5 "$unit/meta" | grep -q '"modified":false' \
      || fail "$unit reports a modified build: its image cannot certify a run (container-topology.md §2)"
  done

  mkdir -p "$OUT"
  # .txt, not .log: `.gitignore` excludes `*.log` so a 34 MB per-cell service log can never be
  # committed by accident, and these two transcripts are *evidence* — the control assertions
  # exist nowhere else, and a report citing an artifact the repository does not carry is an
  # assertion.
  RUNLOG="$OUT/experiments.txt"
  log "artifacts -> $OUT"
  # Every binary and artifact the evidence depends on, named in the transcript. The verifier
  # belongs here as much as the generator: a reader asking "which binary certified this?" must
  # not have to take the report's word for it.
  log "generator   $(revision_of "$LOAD")"
  log "verifier    $(revision_of "$VERIFY")"
  log "image       $(grep -o '"image_tag": "[^"]*"' "$DEPLOYMENT" | cut -d'"' -f4) $(grep -o '"image_id": "[^"]*"' "$DEPLOYMENT" | head -1 | cut -d'"' -f4)"
  log "require     $REQUIRE"
}

# revision_of reports a binary's VCS stamp as `<revision> (clean)`, which is what makes the
# transcript checkable rather than reassuring.
revision_of() {
  go version -m "$1" | awk '
    /vcs.revision/ {rev=$2}
    /vcs.modified/ {mod=$2}
    END {sub(/vcs.revision=/, "", rev); print rev, (mod == "vcs.modified=false" ? "(clean)" : "(MODIFIED)")}'
}

# --- fixture -----------------------------------------------------------------------------
#
# -reset truncates the whole authority, so it goes on the FIRST organisation of each one and
# nowhere else. Passing it on every call deletes the previous organisation's slots and leaves a
# run whose missing half comes back as ordinary-looking `unknown_target` refusals.
seed() {
  log "seeding $SLOTS slots per organisation"
  seed_one -reset "$A1_DSN" org-a
  seed_one ""     "$A1_DSN" org-c
  seed_one -reset "$A2_DSN" org-b
  seed_one ""     "$A2_DSN" org-d
}

# seed_one retries once, because `-reset` deadlocks with the running service often enough to
# lose a matrix at the last cell.
#
# TRUNCATE takes ACCESS EXCLUSIVE on every booking table while the expiry worker is sweeping
# the same tables on its own schedule, so the two can deadlock (40P01) — observed once in ten
# seeds against this topology. The retry is logged rather than silent: a *second* failure is
# not a race and the run stops, and a fixture that needed a retry is something the operator
# should see next to the results.
seed_one() {
  local reset="$1" dsn="$2" org="$3"
  # shellcheck disable=SC2086 # $reset is a bare flag or empty, deliberately unquoted
  if go run ./cmd/alloca-seed $reset -database-url "$dsn" -org "$org" -slots "$SLOTS" >/dev/null 2>&1; then
    return 0
  fi
  log "seed of $org failed (likely a TRUNCATE deadlock with the expiry worker); retrying once"
  sleep 2
  # shellcheck disable=SC2086 # $reset is a bare flag or empty, deliberately unquoted
  go run ./cmd/alloca-seed $reset -database-url "$dsn" -org "$org" -slots "$SLOTS" >/dev/null \
    || fail "seeding $org failed twice: the fixture is not in a known state, so no cell below it means anything"
}

scrape() { curl -fsS -m 10 "$1" -o "$2"; }

# verify reconciles one cell against both authorities and keeps the verdict beside its run.
#
# **Passing checks are not a usable cell, and `-require none` let that difference through.**
# Reconciliation compares the numbers a run produced; certification decides whether those
# numbers may be quoted at all. A run whose response validation failed, whose ambiguity is
# unresolved, or whose units drifted mid-run reaches `quotability.level: none` with every
# reconciliation check still `ok: true` — so a floor of `none` exits zero and the cell reports
# itself passed while discharging nothing.
#
# The earlier version justified `none` as keeping the diagnostic artifact, which was simply
# wrong about the verifier: it writes the verdict *before* enforcing `-require`, so a real floor
# costs no diagnostics. Both failures are checked, separately, because they mean different
# things to whoever reads the message.
verify() {
  local dir="$1"
  if ! "$VERIFY" \
      -run "$dir/run.json" \
      -placement "$PLACEMENT" \
      -authority-db "authority-1=$A1_DSN" \
      -authority-db "authority-2=$A2_DSN" \
      -authority-metrics authority-1="$dir/s1-after.prom" \
      -authority-metrics authority-2="$dir/s2-after.prom" \
      -authority-metrics-baseline authority-1="$dir/s1-baseline.prom" \
      -authority-metrics-baseline authority-2="$dir/s2-baseline.prom" \
      -require "$REQUIRE" \
      -out "$dir/verdict.json"; then
    fail "$dir: the run did not reach '$REQUIRE'; the verdict says why, and it was written before the level was enforced: $dir/verdict.json"
  fi
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
    "$@" 2>&1 | tee "$dir/generator-output.txt"

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
#
# The cross-authority refusal is asserted twice, under one key: being refused and *staying*
# refused on replay are separate properties, and the load cells cannot distinguish them
# because they never repost a key (case 3b).
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

  # 3. cross-authority booking: policy, at the user's own unit (VAL-COR-4). The request is held
  #    in one place because case 3b has to repost *exactly* it; spelled out twice, an edit to
  #    one copy would quietly turn 3b into a different request, which cannot replay and would
  #    fail for a reason that looks like a defect in the service.
  local refusal_url="$S1/v1/slots/org-b/slot-0/reservations"
  local refusal_key="c3-$stamp"
  local refusal_body='{"user_organisation_id":"org-a","user_id":"c3"}'
  local refused replayed

  refused="$(post "$refusal_url" "$refusal_key" "$refusal_body")"
  expect "cross-authority refusal" "$refused" 409 cross_authority_unsupported
  expect "cross-authority refusal is decided, not replayed" "$refused" 409 '"replay":false'

  # 3b. the same key again — VAL-COR-4's *same-key replay* clause, which the refusal cell
  #     cannot show: it drives distinct keys throughout and reposts none of them, so a
  #     persisted record is all it establishes. The refusal is recorded on user-home through
  #     the ordinary idempotency scope before any slot work, so reposting must return the
  #     *recorded* answer rather than re-deciding the policy a second time.
  #
  #     Three assertions, because no two of them are enough. The reason without the flag is what
  #     a re-decision also produces; the flag without the reason would accept a replay of some
  #     other recorded outcome; and both of those on the repost, without the first post's
  #     `replay=false`, would still pass in a build that labelled *every* cross-authority
  #     refusal a replay. Replay is proven deterministically below the topology by
  #     TestCrossAuthorityRefusalIsReplayable (service) and TestRefusalIsRecordedAndReplayed
  #     (PostgreSQL adapter); this is the observation that those two compose on the deployed
  #     two-authority stack.
  replayed="$(post "$refusal_url" "$refusal_key" "$refusal_body")"
  expect "cross-authority refusal replays the recorded reason" "$replayed" 409 cross_authority_unsupported
  expect "cross-authority refusal is marked as a replay" "$replayed" 409 '"replay":true'

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

  log "controls: all assertions passed"
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
  # Whatever happens below, the authority comes back. A cell that fails its own assertions
  # would otherwise leave the topology mid-fault, and the next run's preflight refuses a unit
  # that is not ready — so one failed cell costs a manual repair before anything else can run.
  # Observed while proving those assertions fire.
  # EXIT rather than RETURN: `fail` exits, and an exit does not run a RETURN trap — so the one
  # path that matters is the one RETURN would miss.
  trap 'docker start "$FAULT_CONTAINER" >/dev/null 2>&1 || true' EXIT
  fault_roles
  seed

  scrape "$M1" "$dir/s1-baseline.prom"
  scrape "$M2" "$dir/s2-baseline.prom"

  # The fault must open *and close* inside the measured window, or the experiment is not the one
  # the report describes — recovery outside the window makes the outage look longer than stated
  # to everything downstream.
  [ $((FAULT_AFTER + FAULT_FOR)) -lt "$WINDOW_SECONDS" ] \
    || fail "the fault runs from +${FAULT_AFTER}s to +$((FAULT_AFTER + FAULT_FOR))s of a ${WINDOW_SECONDS}s window: it cannot be restored inside a window it outlives"

  log "failure: driving ${WINDOW_SECONDS}s of load; $FAULT_CONTAINER stops at +${FAULT_AFTER}s for ${FAULT_FOR}s"
  local window_opened window_closes
  window_opened="$(date +%s)"
  window_closes=$((window_opened + WINDOW_SECONDS))
  "$LOAD" \
    -placement "$PLACEMENT" \
    -endpoint "authority-1=$S1" -endpoint "authority-2=$S2" \
    -deployment "$DEPLOYMENT" \
    -workload multi-org-dispersed \
    -slots "$SLOTS" -concurrency "$CONCURRENCY" -duration "${WINDOW_SECONDS}s" \
    -require none -out "$dir/run.json" > "$dir/generator-output.txt" 2>&1 &
  local load_pid=$!

  sleep "$FAULT_AFTER"
  docker stop "$FAULT_CONTAINER" >/dev/null
  log "failure: $FAULT_CONTAINER stopped"
  assert_isolated "$dir"

  sleep "$FAULT_FOR"
  docker start "$FAULT_CONTAINER" >/dev/null
  log "failure: $FAULT_CONTAINER started; waiting for $AFFECTED_NAME to report ready"
  # **The deadline is the measured window, not a count of attempts.** An iteration bound is
  # unrelated to the claim being made: the loop could run past the end of the load run and then
  # log "inside the measured window" about a recovery that happened after it, leaving
  # verification to certify a longer outage than the report describes. That is most likely
  # exactly when nothing was ambiguous, because the generator then exits the moment the window
  # closes rather than lingering in a resolution pass.
  #
  # The deadline is tested on **both sides of every probe**, and each side closes a hole the
  # other leaves open:
  #
  #   - before, including the first probe. Testing only after a *miss* means a unit answering
  #     `200` on the first attempt skips the check entirely, so a recovery that landed after the
  #     window closed passed the cell with its margin printed as a negative number nobody read;
  #   - after, because a probe may take up to its own timeout. One issued while the window was
  #     still open can return once it has closed, and that answer describes a readiness observed
  #     outside the interval the run is reported against.
  #
  # Both were found by running this gate's own mutations rather than reasoning about them.
  #
  # It is deliberately conservative: the cell refuses when the window has closed by the time it
  # can look, rather than claiming a readiness it did not observe inside the interval.
  local restarted_at now ready
  restarted_at="$(date +%s)"
  while :; do
    require_window_open "$window_closes"
    ready="$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$AFFECTED_URL/readyz" || true)"
    # Again, after the probe. A probe may take up to its own timeout, so one issued while the
    # window was still open can *return* after it closed — and accepting that answer would
    # record a readiness observed outside the interval the run is reported against.
    require_window_open "$window_closes"
    [ "$ready" = "200" ] && break
    sleep 1
  done
  now="$(date +%s)"
  log "failure: $AFFECTED_NAME ready again $((now - restarted_at))s after restart, with $((window_closes - now))s of the measured window left"

  wait "$load_pid" || fail "the generator exited non-zero; read $dir/generator-output.txt"
  scrape "$M1" "$dir/s1-after.prom"
  scrape "$M2" "$dir/s2-after.prom"
  verify "$dir"
  partition_check "$dir"
  trap - EXIT
}

# assert_isolated proves containment *while the authority is actually down*, which is the only
# window in which it can be proven at all.
#
# Recording the two status codes is not the same as checking them, and the difference is the
# whole claim: `printf` succeeds whatever curl returns, so a regression that leaves the affected
# unit ready — or takes both units down — produced an artifact that contradicted the containment
# claim while the cell passed on the strength of the run recovering afterwards.
#
# The affected unit is polled rather than sampled once. Readiness is a live database probe
# bounded by the unit's own readiness timeout, so `503` follows the fault by up to that bound;
# sampling immediately would make the assertion a race. The healthy peer is checked at the same
# moment and must be `200` — that is the containment half, and it is asserted every second the
# affected unit is still refusing.
assert_isolated() {
  local dir="$1" waited=0 affected peer

  : > "$dir/readiness-during-fault.txt"
  while :; do
    affected="$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$AFFECTED_URL/readyz" || true)"
    peer="$(curl -sS -o /dev/null -w '%{http_code}' -m 5 "$PEER_URL/readyz" || true)"
    printf 'after %2ds of fault (%s stopped): %s readyz %s, %s readyz %s\n' \
      "$waited" "$FAULT_CONTAINER" "$PEER_NAME" "$peer" "$AFFECTED_NAME" "$affected" \
      >> "$dir/readiness-during-fault.txt"

    [ "$peer" = "200" ] || fail "$PEER_NAME reported $peer while only $FAULT_CONTAINER was stopped: the fault reached the unaffected authority, which is what REQ-FAIL-1 exists to exclude"
    [ "$affected" = "503" ] && break

    waited=$((waited + 1))
    [ "$waited" -lt "$FAULT_ISOLATION_WAIT" ] || fail "$AFFECTED_NAME still reported $affected ${waited}s after $FAULT_CONTAINER stopped: an authority that cannot reach its database must not report itself ready"
    sleep 1
  done
  log "failure: isolation asserted — $PEER_NAME 200, $AFFECTED_NAME 503 after ${waited}s"
}

# fault_roles resolves which unit the injected fault belongs to, once, for every step that needs
# it.
#
# It is a function of its own because the mapping had **two** consumers and only one of them
# knew: the isolation assertion resolved the roles locally while the recovery wait below still
# polled unit 2 by name. With an inverted fault that loop watched the *healthy* peer, saw 200
# immediately, and let the cell continue without ever establishing that the restarted authority
# came back inside the measured window — so verification could wait for the database afterwards
# and certify a fault interval that was not the one the experiment described. One mapping, one
# place, and a third consumer inherits it rather than re-deriving it.
# require_window_open refuses once the measured window has closed. One definition, called on
# both sides of the readiness probe, so the two checks cannot drift into disagreeing about what
# "inside the window" means.
require_window_open() {
  local closes="$1" now
  now="$(date +%s)"
  if [ "$now" -ge "$closes" ]; then
    fail "$AFFECTED_NAME was not observed ready before the measured window closed $((now - closes))s ago: the outage outlasted the interval this run describes, so the fault is not the one the report would state"
  fi
}

fault_roles() {
  case "$FAULT_CONTAINER" in
    alloca-authority-1-db) AFFECTED_URL="$S1"; PEER_URL="$S2"; AFFECTED_NAME="unit-1"; PEER_NAME="unit-2" ;;
    alloca-authority-2-db) AFFECTED_URL="$S2"; PEER_URL="$S1"; AFFECTED_NAME="unit-2"; PEER_NAME="unit-1" ;;
    *) fail "FAULT_CONTAINER=$FAULT_CONTAINER: this cell knows which unit each authority's database belongs to, and that is not one of them" ;;
  esac
}

# partition_check reads each authority's rows *by organisation*, which is the direct form of
# "no request failed over to the surviving writer" (VAL-FAIL-1).
#
# The verdict already implies it: each authority is counted only over the organisations the
# placement gives it, so a row written to the wrong authority is excluded from both scopes and
# the aggregate comes up short against the client's totals. This asks the databases the
# question outright, because the implication takes a paragraph to explain and the row counts
# take a glance.
partition_check() {
  local dir="$1" authority
  for authority in 1 2; do
    docker exec "alloca-authority-${authority}-db" psql -U alloca -d alloca -tAc "
      select 'reservations ' || slot_organisation_id || '=' || n
        from (select slot_organisation_id, count(*) n from reservations group by 1) r
      union all
      select 'claims ' || user_organisation_id || '=' || n
        from (select user_organisation_id, count(*) n from user_time_claims group by 1) c
      union all
      select 'idempotency ' || user_organisation_id || '=' || n
        from (select user_organisation_id, count(*) n from idempotency_records group by 1) i
      order by 1" > "$dir/authority-${authority}-rows-by-organisation.txt"
  done

  # The census is a *gate*, not a keepsake. Writing it and leaving the reading to whoever
  # opens the file is how "no request failed over to the surviving writer" becomes something a
  # report author eyeballs — and reconciliation will not catch it either: `RunTopology` counts
  # each authority only over the organisations placement gives it, so a stray row on the wrong
  # writer sits outside every scoped count and the aggregate still balances whenever the correct
  # row also exists.
  #
  # The expected sets come from the run's own manifest rather than from constants here, so the
  # gate follows the placement the run actually reached instead of a second copy of it that can
  # drift.
  python3 - "$dir" <<'PY' || fail "$dir: rows exist on an authority that does not own them; a request reached the wrong writer (VAL-FAIL-1)"
import json, re, sys

directory = sys.argv[1]
assignment = json.load(open(f"{directory}/run.json"))["manifest"]["placement_assignment"]

violations = []
for authority, organisations in sorted(assignment.items()):
    number = authority.rsplit("-", 1)[1]
    for line in open(f"{directory}/authority-{number}-rows-by-organisation.txt"):
        if not line.strip():
            continue
        # "<relation> <organisation>=<count>"
        found = re.match(r"(\S+) (.+)=(\d+)$", line.strip())
        if not found:
            violations.append(f"{authority}: unparsable census line {line.strip()!r}")
            continue
        relation, organisation, count = found.groups()
        if organisation not in organisations:
            violations.append(
                f"{authority} holds {count} {relation} row(s) for {organisation}, which the "
                f"run's placement assigns elsewhere ({', '.join(organisations)})")

for v in violations:
    print(v, file=sys.stderr)
sys.exit(1 if violations else 0)
PY
  log "$dir: row census clean — every row sits on the authority its organisation is placed on"
}

case "${1:-all}" in
  controls)     preflight; controls ;;
  correctness)  preflight; cell correctness  -workload multi-org-dispersed -n "$ITERATIONS" ;;
  distribution) preflight; cell distribution -workload hot-organisation -org "$HOT_ORG" -n "$ITERATIONS" ;;
  refusal)      preflight; cell refusal      -workload cross-authority-control -n "$ITERATIONS" ;;
  failure)      preflight; failure ;;
  all)
    preflight
    controls
    cell correctness  -workload multi-org-dispersed      -n "$ITERATIONS"
    cell distribution -workload hot-organisation -org "$HOT_ORG" -n "$ITERATIONS"
    cell refusal      -workload cross-authority-control  -n "$ITERATIONS"
    failure
    ;;
  *) fail "unknown cell ${1}: want controls | correctness | distribution | refusal | failure | all" ;;
esac

log "done; artifacts in $OUT"
