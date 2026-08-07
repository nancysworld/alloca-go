#!/usr/bin/env bash
#
# The image's binary must carry the revision it was built from, and must not be stamped
# modified when the checkout is clean.
#
# This is the end-to-end half of the build-context control. `check-build-context.sh` proves
# the *rule* — .dockerignore excludes nothing tracked — without needing a daemon, and runs in
# `make ci`. This proves the *consequence* against a real image, and needs Docker, so it is
# run beside the image targets rather than in the gate.
#
# Why it reads the binary rather than curling /meta: /meta is served by a process that needs
# a reachable authority to start, so asking it this question would mean standing up a
# database to learn a fact that is already sealed into the binary at build time. `go version
# -m` reads the same `vcs.*` settings that `debug.ReadBuildInfo` hands to buildinfo.Collect,
# which is what /meta marshals — one step earlier in the same chain, with nothing between
# them that could differ.
#
# A dirty checkout is refused rather than tolerated: on a dirty tree `modified=true` is the
# *correct* answer, so the control could not tell a working image from the broken one it
# exists to catch.
set -euo pipefail

cd "$(dirname "$0")/../.."

IMAGE="${IMAGE:-alloca-go:provenance-check}"

if ! command -v docker >/dev/null 2>&1; then
  echo "check-image-provenance: docker is not available; this control needs a daemon" >&2
  exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "check-image-provenance: the working tree has uncommitted changes." >&2
  echo "  On a dirty tree the binary is *supposed* to be stamped modified=true, so this" >&2
  echo "  control cannot distinguish a correct image from a broken one. Commit or stash" >&2
  echo "  first, then re-run." >&2
  exit 1
fi

want_revision="$(git rev-parse HEAD)"

echo "building $IMAGE from $(git rev-parse --short HEAD)..."
docker build -q -t "$IMAGE" . >/dev/null

# The runtime layer is distroless: no shell, so the binary is copied out and read here.
container="$(docker create "$IMAGE")"
trap 'docker rm -f "$container" >/dev/null 2>&1 || true' EXIT

extracted="$(mktemp -d)"
trap 'docker rm -f "$container" >/dev/null 2>&1 || true; rm -rf "$extracted"' EXIT
docker cp "$container:/usr/local/bin/alloca-go" "$extracted/alloca-go" >/dev/null

settings="$(go version -m "$extracted/alloca-go")"

# `go version -m` prints one setting per line as `build<TAB>key=value`, so the key and value
# arrive in a single whitespace-delimited field and have to be split on the first `=`.
setting() {
  sed -n "s/^[[:space:]]*build[[:space:]]\{1,\}$1=\(.*\)\$/\1/p" <<<"$settings"
}

got_revision="$(setting 'vcs\.revision')"
got_modified="$(setting 'vcs\.modified')"

failed=0

if [ -z "$got_revision" ]; then
  echo "FAIL: the binary carries no vcs.revision." >&2
  echo "  The build context is missing .git, or the build ran with -buildvcs=false." >&2
  echo "  Every run against this image is uncertifiable (§6.4)." >&2
  failed=1
elif [ "$got_revision" != "$want_revision" ]; then
  echo "FAIL: the binary reports revision $got_revision, want $want_revision." >&2
  failed=1
fi

if [ "$got_modified" = "true" ]; then
  echo "FAIL: the binary is stamped vcs.modified=true, but the checkout is clean." >&2
  echo "  \`go build\` reads this from \`git status --porcelain\` inside the build context," >&2
  echo "  so the usual cause is .dockerignore excluding a tracked path: git then reports" >&2
  echo "  those files as deleted and the context reads as a modified tree." >&2
  echo "  Manifest.Validate refuses service_source_modified at 'local', the floor of the" >&2
  echo "  ladder, so this makes every run against this image unquotable at any level." >&2
  echo "  Run test/scripts/check-build-context.sh to see which paths are excluded." >&2
  failed=1
elif [ -z "$got_modified" ]; then
  echo "FAIL: the binary carries no vcs.modified setting." >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "check-image-provenance: $IMAGE reports revision $got_revision, modified=false"
