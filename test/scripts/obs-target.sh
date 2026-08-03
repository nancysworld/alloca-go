#!/usr/bin/env bash
#
# Write the Prometheus scrape target for the service running on this host.
#
# Why this exists rather than a static target in prometheus.yml: the service runs on the host
# and Prometheus runs in a container, and under Docker Desktop on WSL2 those are two different
# WSL distros. `host.docker.internal` resolves to the *Windows* host and the compose bridge
# gateway belongs to the docker-desktop distro, so both answer "connection refused" — which is
# indistinguishable from a service that never started. The address that works is this distro's
# own eth0, and WSL reassigns it on restart.
#
# So the address is probed rather than assumed, and probed *from inside the container*, since
# that is the only vantage point whose answer matters. Candidates are tried in order of how
# portable they are, so a plain Linux docker host still gets the conventional answer.
set -euo pipefail

PORT="${METRICS_PORT:-9090}"
CONTAINER="${PROM_CONTAINER:-alloca-prometheus}"
OUT="${1:-deploy/observability/targets/alloca-go.json}"

if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  echo "obs-target: $CONTAINER is not running; start it with 'make obs-up'" >&2
  exit 1
fi

candidates=(
  "host.docker.internal"                                              # Docker Desktop, plain Linux with host-gateway
  "$(ip -4 addr show eth0 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)"  # this WSL distro
  "$(ip -4 route show default 2>/dev/null | awk '{print $3}' | head -1)"                  # default gateway
)

for host in "${candidates[@]}"; do
  [ -n "$host" ] || continue
  if docker exec "$CONTAINER" wget -q -O /dev/null -T 2 "http://${host}:${PORT}/metrics" 2>/dev/null; then
    mkdir -p "$(dirname "$OUT")"
    cat > "$OUT" <<JSON
[
  {
    "targets": ["${host}:${PORT}"],
    "labels": {
      "instance": "alloca-go-local"
    }
  }
]
JSON
    echo "obs-target: scraping ${host}:${PORT} (written to $OUT)"
    exit 0
  fi
done

echo "obs-target: no candidate address reached the service on :${PORT}." >&2
echo "  Tried: ${candidates[*]}" >&2
echo "  Is the service running? Start it with 'make dev-measured'." >&2
exit 1
