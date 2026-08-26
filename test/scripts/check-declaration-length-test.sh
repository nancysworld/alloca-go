#!/usr/bin/env bash
#
# Discriminating tests for check-declaration-length.sh.
#
# The check exists to bound one class — a Go declaration written as a single long line — and
# to leave every other long line alone. Both halves need a case that fails when the property
# is removed, because the failure modes point in opposite directions: a check that stops
# firing lets `commit` at 205 columns back in, and a check that fires too widely reports the
# error strings and SQL that DEBT-5 concluded must not be split, which is how a style gate
# earns its way out of `make ci`.
#
# The fixtures are Go source written here rather than files from the tree. Asserting against
# real files would make the test restate the current contents of the repository, so it would
# start failing for edits that have nothing to do with the rule, and the wrapped-declaration
# case could not be exercised at all until someone happened to write one.
#
# The threshold is driven down to a small number through MAX_DECL_COLUMNS so the fixtures
# stay readable. That is the same code path the 130-column bound runs through; a separate
# copy of the rule at test scale would prove nothing about the real one.
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

CHECK=./test/scripts/check-declaration-length.sh

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

passed=0; failed=0

ok()  { printf '  ok    %s\n' "$1"; passed=$((passed + 1)); }
bad() { printf '  FAIL  %s\n     %s\n' "$1" "$2"; failed=$((failed + 1)); }

# case_ <name> <threshold> <fixture-content> <pass|expected-substring>
case_() {
  local name="$1" threshold="$2" content="$3" want="$4" out status

  printf '%s\n' "$content" > "$WORK/fixture.go"

  out=$(MAX_DECL_COLUMNS="$threshold" "$CHECK" "$WORK/fixture.go" 2>&1)
  status=$?

  if [ "$want" = pass ]; then
    if [ "$status" -ne 0 ]; then
      bad "$name" "expected a pass, got exit $status: $out"
    else
      ok "$name"
    fi
    return
  fi

  if [ "$status" -eq 0 ]; then
    bad "$name" "expected a refusal, but the check passed"
  elif ! printf '%s' "$out" | grep -qF "$want"; then
    bad "$name" "refused, but the message did not mention '$want': $out"
  else
    ok "$name"
  fi
}

echo "check-declaration-length-test: the bounded class"

# The reason the check exists. Without the length comparison this is the case that stops
# failing.
case_ "a single-line declaration over the bound is refused" 40 \
  'func Reserve(ctx context.Context, ref domain.SlotRef, now time.Time) (domain.Result, error) {' \
  "fixture.go:1"

# The fix the refusal advises must itself pass, or the check would refuse a signature with
# nowhere to go. This is the discriminating case for the "already wrapped" behaviour: the
# declaration below is far longer than the bound in total and legal because no *line* is.
case_ "the same declaration wrapped one parameter per line passes" 40 \
  'func Reserve(
	ctx context.Context,
	ref domain.SlotRef,
	now time.Time,
) (domain.Result, error) {' \
  pass

case_ "a short declaration passes" 40 \
  'func Close() error {' \
  pass

# A method carries a receiver and is indented inside no block, but an interface method or a
# nested closure is indented. Anchoring on the line start alone would miss those.
case_ "an indented declaration is still checked" 40 \
  '	func inner(ctx context.Context, ref domain.SlotRef, now time.Time) (domain.Result, error) {' \
  "fixture.go:1"

echo
echo "check-declaration-length-test: what must stay out of scope"

# DEBT-5's conclusion, as a test. A general line-length rule reports all three of these; this
# check must report none. Remove the `func` anchor from the script and every one of them
# fails.
case_ "a long error string is not a finding" 40 \
  '	return fmt.Errorf("config: ReadinessTimeout (%s) must be greater than the server write timeout (%s)", a, b)' \
  pass

# shellcheck disable=SC2016 # the backticks are Go raw-string delimiters in the fixture, not
# a command substitution: single quotes are what keeps them literal, which is the point.
case_ "a long SQL literal is not a finding" 40 \
  '	const truncate = `TRUNCATE user_time_claims, user_identities, idempotency_records RESTART IDENTITY CASCADE`' \
  pass

case_ "a long comment is not a finding" 40 \
  '// The term PR4b'"'"'s result turned on, and the one no panel exported until it did, measured across every topology.' \
  pass

echo
echo "check-declaration-length-test: reporting"

# A refusal that does not say which line is wrong sends the reader hunting, and the whole
# point of the gate is that nobody had to read every line to find these.
case_ "the refusal names the column count" 40 \
  'func Reserve(ctx context.Context, ref domain.SlotRef, now time.Time) (domain.Result, error) {' \
  "columns)"

case_ "the refusal says error strings are out of scope" 40 \
  'func Reserve(ctx context.Context, ref domain.SlotRef, now time.Time) (domain.Result, error) {' \
  "must not be split"

echo
printf '%d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
