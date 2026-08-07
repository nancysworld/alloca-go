#!/usr/bin/env bash
#
# The build context must contain every tracked file, or the image is uncertifiable.
#
# `go build` decides the `vcs.modified` flag it stamps into the binary by running
# `git status --porcelain` inside the build context and testing whether the output is
# empty. The context includes `.git` on purpose (see .dockerignore), so excluding a
# *tracked* path does not hide it from that check: git reports the file as deleted, the
# context reads as a modified tree, and the binary is stamped `modified=true` even when the
# host checkout is spotless.
#
# That flag is not cosmetic. `Manifest.Validate` refuses `service_source_modified` at
# `local` — the floor of the quotability ladder — so a wrongly-stamped image makes every run
# against it unquotable at any level, and the failure presents as a harness bug rather than
# as a packaging one. An earlier .dockerignore excluded `docs/`, `test/`, `.github/` and
# `*.md`, which put 1,330 deleted paths into the context.
#
# This check runs without Docker, so it belongs in `make ci` rather than beside the image
# targets: the defect it catches is introduced by editing .dockerignore, which is a change
# anyone can make without ever building an image. `make image-provenance` proves the same
# property end to end against a real image, and needs a daemon to do it.
set -euo pipefail

cd "$(dirname "$0")/../.."

if [ ! -f .dockerignore ]; then
  echo "no .dockerignore: nothing to check" >&2
  exit 0
fi

# Read the exclusion patterns, dropping comments and blank lines.
patterns=()
while IFS= read -r line; do
  line="${line%%$'\r'}"
  case "$line" in
    ''|'#'*) continue ;;
  esac
  patterns+=("$line")
done < .dockerignore

# Negations and ** would need Docker's own matcher to evaluate honestly. Rather than
# approximate them and report a pass this script cannot justify, refuse the pattern and say
# so — an unchecked exclusion is exactly how the original defect got in.
for pattern in "${patterns[@]}"; do
  case "$pattern" in
    '!'*|*'**'*)
      echo "check-build-context: .dockerignore entry '$pattern' uses a form this check" >&2
      echo "  cannot evaluate. Either rewrite it in the simple forms (dir/, *.ext, path)" >&2
      echo "  or extend this script to match Docker's semantics for it." >&2
      exit 1
      ;;
  esac
done

# For each tracked file, report the first pattern that would exclude it from the context.
excluded=0
while IFS= read -r tracked; do
  for pattern in "${patterns[@]}"; do
    bare="${pattern%/}"
    # A directory entry excludes the directory itself and everything beneath it; a glob
    # entry is matched against the whole path and against the basename, which is how
    # Docker treats a pattern with no separator in it.
    # $bare is deliberately unquoted on the right of ==: these two comparisons are the glob
    # matches, and quoting would turn '*.log' into a literal filename.
    # shellcheck disable=SC2053
    if [[ "$tracked" == "$bare" || "$tracked" == "$bare"/* ]] ||
       [[ "$tracked" == $bare ]] ||
       { [[ "$bare" != */* ]] && [[ "${tracked##*/}" == $bare ]]; }; then
      echo "  $tracked  (excluded by '$pattern')" >&2
      excluded=$((excluded + 1))
      break
    fi
  done
done < <(git ls-files)

if [ "$excluded" -ne 0 ]; then
  echo >&2
  echo "check-build-context: .dockerignore excludes $excluded tracked file(s)." >&2
  echo "  Inside the build context git reports each of them as deleted, so \`go build\`" >&2
  echo "  stamps vcs.modified=true and the manifest refuses the run at 'local'." >&2
  echo "  .dockerignore may exclude only paths git already ignores." >&2
  exit 1
fi

echo "check-build-context: .dockerignore excludes no tracked file (${#patterns[@]} patterns, $(git ls-files | wc -l) tracked files)"
