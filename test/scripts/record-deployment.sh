#!/usr/bin/env bash
#
# Record what the running topology is actually serving, by inspecting it.
#
# §6.4 asks every quotable run to identify the artifact it measured, and the commit SHA does
# not cover that. The SHA is stamped into the *binary*, so it binds the running code to a
# revision — but the same code served from a stale `:dev` tag, or rebuilt on a newer base
# layer, carries an identical SHA. Only the image identity separates them.
#
# **Why a host-side script rather than a field of /meta.** A process cannot see which image
# wraps it. Anything the service reported here would be an environment variable handed to it
# and repeated back — asserted provenance sitting beside a compiler-observed commit SHA under
# names that do not admit the difference, which is the shape of the PR1 defect where
# `commit_sha` named the generator rather than the service under test. Inspecting the
# containers is the one place the fact can actually be observed.
#
# **Why not from alloca-load.** Reading it needs the Docker socket, which is root on the
# host. §6.3 keeps the generator credential-free and speaking only HTTP precisely so it can
# move to separate compute later, and a generator holding root on the service host is the
# opposite of that. So the observation is taken here and travels as a file:
#
#   ./test/scripts/record-deployment.sh > test/results/deployment.json
#   alloca-load -deployment test/results/deployment.json ...
#
# The image ID, not a registry digest: a locally built image has an immutable content ID
# straight away, whereas a registry digest exists only after a push. That is what lets PR3b
# record identity at all without a registry.
set -euo pipefail

cd "$(dirname "$0")/../.."

# The serving units. The migration containers deliberately are not inspected: they have
# exited by the time a run starts, and what §6.4 asks about is the artifact that served the
# requests. `docker compose ps` is not used to discover them — a container that died would
# simply be absent from its output, and this must fail loudly rather than record a smaller
# deployment than the one under test.
CONTAINERS="${CONTAINERS:-alloca-service-1 alloca-service-2}"

if ! command -v docker >/dev/null 2>&1; then
  echo "record-deployment: docker is not available; this observation needs a daemon" >&2
  exit 1
fi

units_json=""
first_id=""
first_name=""

for name in $CONTAINERS; do
  if ! state="$(docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null)"; then
    echo "record-deployment: container '$name' does not exist." >&2
    echo "  Raise the topology first (make topo-up), or set CONTAINERS to name the units" >&2
    echo "  this run actually used." >&2
    exit 1
  fi
  if [ "$state" != "true" ]; then
    echo "record-deployment: container '$name' is not running." >&2
    echo "  A stopped unit cannot have served the run, and recording the image it would" >&2
    echo "  have used would describe a deployment that did not happen." >&2
    exit 1
  fi

  # .Image is the image ID the container was created from — the immutable content identity,
  # not the tag it was launched by. A tag can be repointed after the container starts; this
  # cannot.
  id="$(docker inspect --format '{{.Image}}' "$name")"
  if [ -z "$id" ]; then
    echo "record-deployment: could not read an image id for '$name'" >&2
    exit 1
  fi

  if [ -z "$first_id" ]; then
    first_id="$id"
    first_name="$name"
  elif [ "$id" != "$first_id" ]; then
    echo "record-deployment: the units are not running one image." >&2
    echo "  $first_name -> $first_id" >&2
    echo "  $name -> $id" >&2
    echo "  The commit SHA cannot see this: the same code from a stale tag, or rebuilt on a" >&2
    echo "  different base layer, carries the same revision. Rebuild and raise the topology" >&2
    echo "  again (make topo-down && make topo-up) before measuring anything." >&2
    exit 1
  fi

  [ -n "$units_json" ] && units_json="$units_json,"
  units_json="$units_json
    \"$name\": \"$id\""
done

# The tag is recorded as an alias only, and is deliberately taken from the image rather than
# from the Makefile: what matters is what the running container is on, not what a variable
# said it should be. An image carrying several tags, or none, is not an error — nothing rests
# on this value.
tag="$(docker inspect --format '{{if .RepoTags}}{{index .RepoTags 0}}{{end}}' "$first_id" 2>/dev/null || true)"

cat <<JSON
{
  "image_id": "$first_id",
  "image_tag": "$tag",
  "units": {$units_json
  }
}
JSON
