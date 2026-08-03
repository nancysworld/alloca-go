#!/usr/bin/env bash
#
# Exercise a RUNNING alloca-go over a real socket.
#
# This covers what the test suites deliberately do not. The integration tests drive the
# handler through httptest, so they never touch the configured http.Server, its timeouts,
# the listener, or the wiring in cmd/alloca-go. Everything below goes over the network to
# the running binary, which is exactly the strip where a bad timeout or a missed wiring
# step would hide.
#
# It is a smoke test, not a correctness suite: it proves the assembled service answers
# correctly on the happy path and on each refusal shape. Capacity safety under concurrency,
# the schedule invariant and idempotency under contention are properties of the
# transactional core and are proven in internal/postgres, where the contention is.
#
#   make dev      # in another terminal
#   make smoke
#
# Slots are control-plane in AG-M1 — there is no slot-creation endpoint — so the world is
# seeded with SQL and everything after that is HTTP.
#
# Every identifier is unique per run ($RUN below). That is not tidiness: a reservation
# leaves a schedule claim, and a leftover claim for the same user within the hold TTL would
# refuse the next run's reserve with schedule_conflict. Unique users make runs independent
# whether or not cleanup succeeds.
set -uo pipefail

BASE="${BASE:-http://localhost:8080}"
DSN="${DATABASE_URL:-postgres://alloca:alloca@localhost:15432/alloca?sslmode=disable}"

# How long each request waits for a reply. This is curl's own patience and is unrelated to
# the service's deadline budget: raising ALLOCA_SERVER_DEADLINE does nothing here, because
# the client gives up on its own schedule.
#
# Ten seconds suits an ordinary run. Raise it when the service is stopped at a breakpoint,
# or every request fails with exit 28 and an empty body while you are still reading the
# stack:
#
#   make smoke MAX_TIME=600
MAX_TIME="${MAX_TIME:-10}"

ORG="org-1"
RUN="smoke-$$"
SLOT="$RUN-1"
# A second slot whose window OVERLAPS the first, for the schedule-conflict check.
SLOT_OVERLAPPING="$RUN-2"
USER_A="$RUN-a"
USER_B="$RUN-b"

pass=0 fail=0

for tool in curl psql python3; do
  command -v "$tool" >/dev/null || { echo "smoke: $tool is required but not installed"; exit 2; }
done

# Removes only this run's rows, in foreign-key order: the child tables reference slots with
# NO ACTION, so deleting the slot first simply fails. Run from a trap so an early exit
# cannot leave the database dirty, and never silenced — a cleanup that fails quietly is how
# a smoke test starts interfering with the next run.
cleanup() {
  psql "$DSN" -q -v ON_ERROR_STOP=1 \
    -c "DELETE FROM user_time_claims WHERE slot_organisation_id='$ORG' AND slot_id LIKE '$RUN%'" \
    -c "DELETE FROM bookings         WHERE slot_organisation_id='$ORG' AND slot_id LIKE '$RUN%'" \
    -c "DELETE FROM reservations     WHERE slot_organisation_id='$ORG' AND slot_id LIKE '$RUN%'" \
    -c "DELETE FROM slots            WHERE slot_organisation_id='$ORG' AND slot_id LIKE '$RUN%'" \
    -c "DELETE FROM idempotency_records WHERE user_organisation_id='$ORG' AND user_id LIKE '$RUN%'" \
    || echo "smoke: WARNING — cleanup failed; rows for $RUN remain"
}
trap cleanup EXIT

# check <label> <want-status> <want-substring> <curl args...>
check() {
  local label="$1" want_status="$2" want_body="$3"; shift 3
  local out status body
  out=$(curl -sS -m "$MAX_TIME" -w $'\n%{http_code}' "$@" 2>&1)
  status="${out##*$'\n'}"
  body="${out%$'\n'*}"

  if [[ "$status" == "$want_status" && "$body" == *"$want_body"* ]]; then
    printf '  \033[32mPASS\033[0m  %-42s %s\n' "$label" "$status"
    pass=$((pass + 1))
  elif [[ "$status" != "$want_status" ]]; then
    printf '  \033[31mFAIL\033[0m  %-42s status %s, want %s\n' "$label" "$status" "$want_status"
    printf '        body: %s\n' "$body"
    fail=$((fail + 1))
  else
    # Status matched and only the body did. Reported separately, because printing
    # "got 409 want 409" for this case reads as a bug in the script.
    printf '  \033[31mFAIL\033[0m  %-42s status %s as expected, wrong body\n' "$label" "$status"
    printf '        body:           %s\n        want substring: %s\n' "$body" "$want_body"
    fail=$((fail + 1))
  fi
  LAST_BODY="$body"
}

# Returns "" for anything that is not a JSON object, rather than a traceback. The body is
# whatever the last request produced, which on a transport failure is a curl error string —
# and a stack trace there buries the message that actually explains the run.
json_field() {
  python3 -c "
import sys, json
try:
    v = json.load(sys.stdin)
except Exception:
    sys.exit(0)
print(v.get('$1', '') if isinstance(v, dict) else '')" <<<"$LAST_BODY"
}

echo "service:  $BASE"
echo "run id:   $RUN"
echo

# --connect-timeout, not -m: this asks "is anything listening", not "does it reply quickly".
# A refused connection fails immediately either way, while a service paused in a debugger
# still counts as up and is allowed the full MAX_TIME to answer.
curl -sS --connect-timeout 5 -m "$MAX_TIME" -o /dev/null "$BASE/healthz" 2>/dev/null \
  || { echo "smoke: $BASE is not answering — start it with 'make dev'"; exit 2; }

echo "operational surface"
check "healthz is 200"                200 '"status":"ok"'    "$BASE/healthz"
check "readyz sees the database"      200 '"status":"ready"' "$BASE/readyz"
check "meta reports the hold TTL"     200 'reservation_ttl'  "$BASE/meta"
echo

echo "seeding two slots with overlapping windows (control plane, not an API)"
# Released an hour ago so it is bookable now, but starting a day out.
#
# The start offset must stay comfortably longer than the service's configured hold TTL. A
# hold is valid only while expires_at <= starts_at (transaction-semantics §1.6), so a slot
# starting in an hour is refused outside_window the moment someone runs with a one-hour TTL
# — which reads as a broken service rather than a mis-sized fixture. A day of headroom means
# this script does not silently depend on how the service under test is configured.
# The second slot overlaps the first by half an hour and has room to spare. The spare
# capacity is what makes the schedule-conflict check below discriminating: with capacity 1 a
# refusal could be no_capacity, and the test would pass for the wrong reason.
psql "$DSN" -q -v ON_ERROR_STOP=1 -c "INSERT INTO slots
  (slot_id, slot_organisation_id, resource_id, capacity, release_at, starts_at, ends_at)
  VALUES
   ('$SLOT','$ORG','yoga',1, now()-interval '1 hour',
    now()+interval '24 hours', now()+interval '25 hours'),
   ('$SLOT_OVERLAPPING','$ORG','pilates',5, now()-interval '1 hour',
    now()+interval '24 hours 30 minutes', now()+interval '25 hours 30 minutes')" \
  || { echo "smoke: seed failed — is the database up? (make db-up)"; exit 2; }
echo "  seeded $ORG/$SLOT (capacity 1) and $ORG/$SLOT_OVERLAPPING (capacity 5, overlapping)"
echo

RES="$BASE/v1/slots/$ORG/$SLOT/reservations"
J='Content-Type: application/json'

echo "read route"
check "list includes the seeded slot"  200 "$SLOT" "$BASE/v1/slots?slot_organisation_id=$ORG"
check "list requires the organisation" 400 'invalid_request' "$BASE/v1/slots"
echo

echo "reserve"
check "reserve is admitted"            200 '"outcome":"admitted_success"' \
  -X POST "$RES" -H "$J" -H "Idempotency-Key: $RUN-k1" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
RESERVATION=$(json_field reservation_id)
echo "        reservation_id=$RESERVATION"

# Everything below names this reservation in a URL. Without it the path becomes
# /v1/reservations//confirm, and ServeMux answers an empty path segment with a 307 from path
# cleaning — so the run would end in redirects that say nothing about the failure above.
if [[ -z "$RESERVATION" ]]; then
  echo
  echo "smoke: reserve returned no reservation_id, so the remaining checks cannot run."
  echo "       Fix the failure above first — the refusal reason names the cause."
  printf '\npassed %d, failed %d\n' "$pass" "$fail"
  exit 1
fi

# The same logical request with its fields in the other order. This is a replay, not a
# conflict: the request hash covers semantically significant typed fields, never the JSON
# text (transaction-semantics §5.1).
check "same key + reordered body replays" 200 '"replay":true' \
  -X POST "$RES" -H "$J" -H "Idempotency-Key: $RUN-k1" \
  -d "{\"user_id\":\"$USER_A\",\"user_organisation_id\":\"$ORG\"}"
if [[ "$(json_field reservation_id)" == "$RESERVATION" ]]; then
  echo "        replay returned the original reservation_id"
  pass=$((pass + 1))
else
  echo "        FAIL: replay returned a different reservation_id"
  fail=$((fail + 1))
fi
echo

echo "refusals and rejections"
check "second user gets no_capacity"   409 '"reason":"no_capacity"' \
  -X POST "$RES" -H "$J" -H "Idempotency-Key: $RUN-k2" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_B\"}"
# The milestone's flagship invariant: one identity cannot hold two claims covering the same
# instant, even on different slots that never contend for a lock (transaction-semantics §2.2).
#
# USER_A already holds the claim created above, and this slot overlaps it by half an hour.
# The refusal is decided by the INSERT into user_time_claims failing the exclusion
# constraint — not by a preceding read, which could always lose a race — and the surrounding
# transaction survives it via a savepoint, which is what makes this a clean 409 rather than a
# fault. The slot has spare capacity, so no_capacity cannot be the reason.
check "overlapping slot is schedule_conflict" 409 '"reason":"schedule_conflict"' \
  -X POST "$BASE/v1/slots/$ORG/$SLOT_OVERLAPPING/reservations" -H "$J" -H "Idempotency-Key: $RUN-k6" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
# A different identity is unaffected: it is the *user's* schedule that is full, not the slot.
check "another user may book the same slot" 200 '"outcome":"admitted_success"' \
  -X POST "$BASE/v1/slots/$ORG/$SLOT_OVERLAPPING/reservations" -H "$J" -H "Idempotency-Key: $RUN-k7" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$RUN-c\"}"
check "unknown slot is 404"            404 '"reason":"unknown_target"' \
  -X POST "$BASE/v1/slots/$ORG/no-such-slot/reservations" -H "$J" -H "Idempotency-Key: $RUN-k3" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
check "missing Idempotency-Key is 400" 400 'invalid_request' \
  -X POST "$RES" -H "$J" -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
check "unknown body field is 400"      400 'invalid_request' \
  -X POST "$RES" -H "$J" -H "Idempotency-Key: $RUN-k4" \
  -d "{\"user_organisation_id\":\"$ORG\",\"userId\":\"$USER_A\"}"
echo

echo "confirm and cancel"
check "confirm creates a booking"      200 'booking_id' \
  -X POST "$BASE/v1/reservations/$RESERVATION/confirm" -H "$J" -H "Idempotency-Key: $RUN-c1" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
check "cancel releases it"             200 '"outcome":"admitted_success"' \
  -X POST "$BASE/v1/reservations/$RESERVATION/cancel" -H "$J" -H "Idempotency-Key: $RUN-x1" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_A\"}"
check "capacity is reusable after cancel" 200 '"outcome":"admitted_success"' \
  -X POST "$RES" -H "$J" -H "Idempotency-Key: $RUN-k5" \
  -d "{\"user_organisation_id\":\"$ORG\",\"user_id\":\"$USER_B\"}"
echo

printf 'passed %d, failed %d\n' "$pass" "$fail"
[[ "$fail" -eq 0 ]] || exit 1
