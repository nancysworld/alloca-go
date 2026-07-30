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
DSN="${DATABASE_URL:-postgres://alloca:alloca@localhost:55432/alloca?sslmode=disable}"

ORG="org-1"
RUN="smoke-$$"
SLOT="$RUN"
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
    -c "DELETE FROM user_time_claims WHERE slot_organisation_id='$ORG' AND slot_id='$SLOT'" \
    -c "DELETE FROM bookings         WHERE slot_organisation_id='$ORG' AND slot_id='$SLOT'" \
    -c "DELETE FROM reservations     WHERE slot_organisation_id='$ORG' AND slot_id='$SLOT'" \
    -c "DELETE FROM slots            WHERE slot_organisation_id='$ORG' AND slot_id='$SLOT'" \
    -c "DELETE FROM idempotency_records WHERE user_organisation_id='$ORG' AND user_id LIKE '$RUN%'" \
    || echo "smoke: WARNING — cleanup failed; rows for $SLOT remain"
}
trap cleanup EXIT

# check <label> <want-status> <want-substring> <curl args...>
check() {
  local label="$1" want_status="$2" want_body="$3"; shift 3
  local out status body
  out=$(curl -sS -m 10 -w $'\n%{http_code}' "$@" 2>&1)
  status="${out##*$'\n'}"
  body="${out%$'\n'*}"

  if [[ "$status" == "$want_status" && "$body" == *"$want_body"* ]]; then
    printf '  \033[32mPASS\033[0m  %-42s %s\n' "$label" "$status"
    pass=$((pass + 1))
  else
    printf '  \033[31mFAIL\033[0m  %-42s got %s want %s\n' "$label" "$status" "$want_status"
    printf '        body: %s\n        want substring: %s\n' "$body" "$want_body"
    fail=$((fail + 1))
  fi
  LAST_BODY="$body"
}

json_field() { python3 -c "import sys,json;print(json.load(sys.stdin).get('$1',''))" <<<"$LAST_BODY"; }

echo "service:  $BASE"
echo "run id:   $RUN"
echo

curl -sS -m 5 -o /dev/null "$BASE/healthz" 2>/dev/null \
  || { echo "smoke: $BASE is not answering — start it with 'make dev'"; exit 2; }

echo "operational surface"
check "healthz is 200"                200 '"status":"ok"'    "$BASE/healthz"
check "readyz sees the database"      200 '"status":"ready"' "$BASE/readyz"
check "meta reports the hold TTL"     200 'reservation_ttl'  "$BASE/meta"
echo

echo "seeding a slot with capacity 1 (control plane, not an API)"
psql "$DSN" -q -v ON_ERROR_STOP=1 -c "INSERT INTO slots
  (slot_id, slot_organisation_id, resource_id, capacity, release_at, starts_at, ends_at)
  VALUES ('$SLOT','$ORG','yoga',1, now()-interval '1 hour', now()+interval '1 hour', now()+interval '2 hours')" \
  || { echo "smoke: seed failed — is the database up? (make db-up)"; exit 2; }
echo "  seeded $ORG/$SLOT"
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
