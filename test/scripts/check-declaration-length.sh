#!/usr/bin/env bash
#
# A Go declaration must not be written as one excessively long line.
#
# gofmt normalises indentation, alignment and spacing and has no opinion on where a line
# wraps, by deliberate Go design, so nothing in the toolchain bounds this. The cost is
# reviewer attention: `commit` reached 205 columns carrying seven parameters, and a reader
# had to count commas to find the one they wanted. `lockByReservation` reached 258 before a
# human noticed. Both were found by eye, in review, instead of the logic being read.
#
# **Why declarations only, and not a general line-length linter.** Measured on the tree that
# resolved DEBT-5: at a 130-column bound, 7 declarations were over it and every one of them
# was worth wrapping. The other 22 lines over the same bound were error strings, metric help
# text, struct literals and SQL — text that wraps badly and reads no better split, which is
# why `lll` and `golines` were considered and rejected. A general rule would have reported
# 22 findings to fix 7 real ones, and the noise is how a style gate gets disabled. This
# check is the narrow guard for the class that actually hurt.
#
# The bound is 130 because that is where the measured distribution puts the extreme tail:
# p99.9 of every Go line in the repository was 129. It is a boundary read off the code, not
# a taste, and it leaves the 120-130 band legal.
#
# A declaration already wrapped across several lines has a short first line and passes
# without special handling — this only ever flags the single-line form.
#
# In `make ci` for the same reason build-context-check is: the defect arrives by ordinary
# editing, needs no daemon to detect, and otherwise surfaces only when a human happens to
# read the line.
set -euo pipefail

cd "$(dirname "$0")/../.."

# Overridable so the test sibling can drive the same code path at a threshold its fixtures
# can demonstrate, rather than asserting against a second copy of the rule.
MAX_DECL_COLUMNS="${MAX_DECL_COLUMNS:-130}"

# Files may be passed explicitly (the test sibling does this); otherwise check what git
# tracks, so generated or ignored trees are never a source of findings.
if [ "$#" -gt 0 ]; then
  files=("$@")
else
  mapfile -t files < <(git ls-files '*.go')
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "check-declaration-length: no Go files to check" >&2
  exit 0
fi

# A declaration line starts with `func` at the head of the line, optionally indented for a
# method inside a block. A comment line cannot match: it starts with `/`. A `func` literal
# inside a raw string could in principle match, and would be reported rather than silently
# skipped — this check reports where to look, it does not parse Go.
findings=$(
  awk -v max="$MAX_DECL_COLUMNS" '
    /^[[:space:]]*func[ (]/ && length > max {
      printf "  %s:%d  (%d columns)\n", FILENAME, FNR, length
      found = 1
    }
    END { exit(found ? 1 : 0) }
  ' "${files[@]}"
) && status=0 || status=$?

if [ "$status" -ne 0 ]; then
  echo "check-declaration-length: declaration(s) longer than ${MAX_DECL_COLUMNS} columns:" >&2
  echo >&2
  echo "$findings" >&2
  echo >&2
  echo "  Wrap the signature one parameter per line, closing with ') ret {' on its own" >&2
  echo "  line. gofmt accepts that form and will keep it." >&2
  echo >&2
  echo "  This bound is on declarations only. A long error string or SQL literal is not a" >&2
  echo "  finding here and must not be split to satisfy a number (tech-debts.md DEBT-5)." >&2
  exit 1
fi

echo "check-declaration-length: no declaration exceeds ${MAX_DECL_COLUMNS} columns (${#files[@]} files)"
