#!/usr/bin/env bash
#
# Discriminating tests for itc-topology-check.sh's container-set logic.
#
# That check decides whether the topology about to be measured is the one selected. It is what
# stands between an operator and a G1 point measured while three units from a previous G4 still
# hold pools, page caches and expiry loops inside the same envelope — a run nothing downstream
# would question, because `alloca-load` reads `/meta` only from the units it addresses.
#
# **These tests stub `docker` rather than calling it.** The property under test is set arithmetic
# over container names: which belong to this rung, which are leftovers from a larger one, which
# belong to another Compose project. That is decided before any daemon is involved. Stubbing makes
# every case reachable — including ones a real daemon could only produce by actually raising a
# wrong topology — and lets the check run in CI, where there is nothing to raise
# (`../../docs/design/project-structure.md` §1: a check earns a merge gate when it needs nothing
# an operator would have to provide).
#
# **What that deliberately does not cover:** whether the `docker ps` invocations are correct
# against a real daemon. The stub answers whatever filter it is handed, so a malformed filter
# would pass here. What it does gate is the classification, which is where the defect was —
# `--filter name=alloca-` matched the observability stack and `alloca-pg` as though they were
# topology units, so Iteration C, the first configuration to raise monitoring alongside the
# topology, was refused because of it (ag-sept-pr4.md §3).
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

CHECK=./test/scripts/itc-topology-check.sh

STUB="$(mktemp -d)"
trap 'rm -rf "$STUB"' EXIT
mkdir -p "$STUB/bin"

# The stub tells the two `docker ps` calls apart — one scoped to the topology's Compose project,
# one over every `alloca-` name — and answers each from its own file. `docker logs` returns a
# start line carrying the routing version under test.
cat > "$STUB/bin/docker" <<'STUBEOF'
#!/usr/bin/env bash
case "$1" in
  ps)
    if printf '%s\n' "$@" | grep -q 'com.docker.compose.project=alloca-topology'; then
      cat "$STUB_DIR/topology.txt"
    else
      cat "$STUB_DIR/all.txt"
    fi
    ;;
  logs)
    printf '{"msg":"starting alloca-go","routing_version":"%s"}\n' "$STUB_ROUTING"
    ;;
  *)
    exit 1
    ;;
esac
STUBEOF
chmod +x "$STUB/bin/docker"
export STUB_DIR="$STUB"

passed=0; failed=0

ok()  { printf '  ok    %s\n' "$1"; passed=$((passed + 1)); }
bad() { printf '  FAIL  %s\n     %s\n' "$1" "$2"; failed=$((failed + 1)); }

# case_ <name> <groups> <topology-project> <all-alloca-names> <routing> <pass|expected-substring>
#
# A refusal must name what is wrong: one that does not sends the operator to the wrong place,
# which is how the original defect presented — it advised tearing down a topology that was
# correct.
case_() {
  local name="$1" groups="$2" topology="$3" all="$4" routing="$5" want="$6" out status

  # shellcheck disable=SC2086 # deliberately word-split into one container name per line
  printf '%s\n' $topology > "$STUB/topology.txt"
  # shellcheck disable=SC2086
  printf '%s\n' $all > "$STUB/all.txt"
  export STUB_ROUTING="$routing"

  out="$(PATH="$STUB/bin:$PATH" ITC_GROUPS="$groups" "$CHECK" 2>&1)"; status=$?

  if [ "$want" = pass ]; then
    if [ $status -eq 0 ]; then
      ok "$name"
    else
      bad "$name" "refused, but must be accepted: $(printf '%s' "$out" | tr '\n' ' ')"
    fi
    return
  fi

  if [ $status -eq 0 ]; then
    bad "$name" "accepted, but must be refused"
  elif ! printf '%s' "$out" | grep -qF "$want"; then
    bad "$name" "refused without naming the problem; no '$want' in: $(printf '%s' "$out" | tr '\n' ' ')"
  else
    ok "$name"
  fi
}

G4_UNITS="alloca-authority-1-db alloca-authority-2-db alloca-authority-3-db alloca-authority-4-db
          alloca-service-1 alloca-service-2 alloca-service-3 alloca-service-4"
G2_UNITS="alloca-authority-1-db alloca-authority-2-db alloca-service-1 alloca-service-2"
G1_UNITS="alloca-authority-1-db alloca-service-1"
# The whole monitoring set, node_exporter included. It is listed here rather than only in the
# check because the two gates are in tension and the tension is invisible from either side:
# itc-run.sh REFUSES a cell that retained no host samples (§2.5), so the exporter has to be
# running — while this check refuses containers the rehearsal did not raise, so an unlisted
# exporter would have to be stopped. Shipping the exporter without adding it here made
# `make itc-rehearse` fail on a correctly configured machine.
MONITORING="alloca-prometheus alloca-grafana alloca-node-exporter"
# The postgres exporters are per-authority, so unlike the three above they cannot be one constant.
# Shipping them without extending the check repeated the node_exporter mistake described above,
# verbatim and in the same week: `make itc-rehearse` failed on a correctly configured machine
# because the check refused a container the rehearsal itself had just raised.
PGEXP_1="alloca-postgres-exporter-1"
PGEXP_4="alloca-postgres-exporter-1 alloca-postgres-exporter-2 alloca-postgres-exporter-3 alloca-postgres-exporter-4"

echo "itc-topology-check-test: container-set logic"

# The regression. Monitoring is a different Compose project and Iteration C raises it alongside
# the topology by design (§2.14), so a check that counts it as a topology unit refuses the
# rehearsal itself.
case_ "G4 with monitoring running is accepted" \
  4 "$G4_UNITS" "$G4_UNITS $MONITORING" itc-g4 pass
case_ "G2 with monitoring running is accepted" \
  2 "$G2_UNITS" "$G2_UNITS $MONITORING" itc-g2 pass
case_ "G1 with monitoring running is accepted" \
  1 "$G1_UNITS" "$G1_UNITS $MONITORING" itc-g1 pass

# Monitoring is optional: a rehearsal may run without a scrape stack. Only its misclassification
# was the defect.
case_ "a rung without monitoring is accepted" \
  4 "$G4_UNITS" "$G4_UNITS" itc-g4 pass

# The postgres exporters, one per authority (§3.16). Accepted at the rung that raised them.
case_ "G1 with its postgres exporter is accepted" \
  1 "$G1_UNITS" "$G1_UNITS $MONITORING $PGEXP_1" itc-g1 pass
case_ "G4 with all four postgres exporters is accepted" \
  4 "$G4_UNITS" "$G4_UNITS $MONITORING $PGEXP_4" itc-g4 pass

# Tolerated, never required: `make itc-up` runs this check before obs-rehearse raises anything, so
# demanding the exporters here would refuse the documented order itself.
case_ "a rung with no postgres exporter is accepted" \
  1 "$G1_UNITS" "$G1_UNITS $MONITORING" itc-g1 pass

# An exporter above the rung is a leftover from a larger one, querying a database that is no longer
# running. Same property as leftover units, one Compose project across — and the case that fails if
# the tolerated set is ever flattened back into a fixed regex.
case_ "a postgres exporter above the rung is refused" \
  1 "$G1_UNITS" "$G1_UNITS $MONITORING $PGEXP_4" itc-g1 "did not raise"

# The property the check exists for. Compose profiles decide what `up` starts and say nothing
# about what is already running, so G1 after G4 leaves three units alive inside the envelope the
# one-group point is measured in.
case_ "leftover units from a larger rung are refused" \
  1 "$G4_UNITS" "$G4_UNITS $MONITORING" itc-g1 "running but NOT part of G1"
case_ "a missing unit is refused" \
  4 "$G1_UNITS" "$G1_UNITS $MONITORING" itc-g4 "expected but NOT running"

# Scoping the equality check to the topology's own project would otherwise have lost this: a
# container the rehearsal never raised still consumes the envelope, and the partition does not
# pin it.
case_ "a container the rehearsal did not raise is refused" \
  4 "$G4_UNITS" "$G4_UNITS $MONITORING alloca-pg" itc-g4 "did not raise"
case_ "the refusal names the unaccounted container" \
  4 "$G4_UNITS" "$G4_UNITS $MONITORING alloca-pg" itc-g4 "alloca-pg"

# A unit that survived a topology change serves the *previous* placement document while looking
# healthy, and its boot line is where that shows.
case_ "a unit serving the wrong routing version is refused" \
  4 "$G4_UNITS" "$G4_UNITS $MONITORING" itc-g2 "routing"

echo
printf 'itc-topology-check-test: %d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
