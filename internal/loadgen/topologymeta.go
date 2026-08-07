package loadgen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// UnitMeta is one service unit's self-description, together with where the harness reached
// it. The target matters: when units disagree, the operator needs to know *which* unit to
// look at, and an error naming only a revision sends them to the wrong terminal.
type UnitMeta struct {
	Target string      `json:"target"`
	Meta   ServiceMeta `json:"meta"`
}

// TopologyMeta is every participating unit's `/meta`, read once.
//
// A multi-authority run is only interpretable if the units are one deployment rather than
// several that happen to be running. Nothing in the client totals would reveal otherwise: a
// run against two units on different commits produces a perfectly well-formed report whose
// numbers describe two different services averaged together.
type TopologyMeta struct {
	Units []UnitMeta `json:"units"`
}

// FetchTopologyMeta reads `/meta` from every target.
//
// A unit that cannot be read is recorded as an error rather than dropped. Dropping it would
// shrink the topology silently — a two-authority run would report as a healthy one-authority
// run, which is the most misleading artifact this harness could produce.
func FetchTopologyMeta(ctx context.Context, targets []string, timeout time.Duration) (TopologyMeta, error) {
	if len(targets) == 0 {
		return TopologyMeta{}, fmt.Errorf("loadgen: no targets to read /meta from")
	}

	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)

	var topology TopologyMeta
	var failures []string
	for _, target := range sorted {
		meta, err := FetchServiceMeta(ctx, target, timeout)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target, err))
			continue
		}
		topology.Units = append(topology.Units, UnitMeta{Target: target, Meta: meta})
	}
	if len(failures) > 0 {
		return topology, fmt.Errorf("loadgen: could not read /meta from %d of %d units: %s",
			len(failures), len(sorted), strings.Join(failures, "; "))
	}
	return topology, nil
}

// Disagreement names how the units fail to describe one deployment, or returns "" when they
// agree. It is the certification gate for a multi-authority run.
//
// **Revision, schema version and routing version are the three that matter**, and each for a
// different reason:
//
//   - a **revision** difference means the units are running different code, so the run
//     measured two services and the manifest can name only one of them;
//   - a **schema version** difference means the authorities are not interchangeable, and a
//     per-authority correctness result cannot be compared with its peer's. Equal versions are
//     not by themselves *compatible* versions — that is each unit's own startup gate (INV-27)
//     — but unequal ones are definitely not one deployment;
//   - a **routing version** difference is the split-brain case the horizontal-database design records as
//     §7.3: two units disagreeing about placement would write one organisation to two
//     authorities and divide its source of truth. It is the most dangerous of the three and
//     the least visible in any total.
//
// **The run-shaping fields are compared too**, and for a reason that is about the manifest
// rather than about the deployment: the manifest records one Go version, one GOMAXPROCS, one
// telemetry mode, one timeout budget, one reservation TTL, one PostgreSQL version and one
// pool ceiling for the whole run, projected from the first unit. That projection is honest
// only if the units agree. Two units at different pool ceilings, or one with telemetry off,
// produce a report describing a deployment that does not exist — and every one of those
// fields changes what the numbers mean.
//
// Configuration that differs *legitimately* between units — the authority each is bound to,
// the organisations each serves, when each process started — is deliberately not compared.
// Those are supposed to differ; that is what makes them separate authorities.
//
// The three equality properties are compared against the first unit, which is sufficient
// because equality is transitive: if every unit matches the first, they all match each other.
// **Authority distinctness is not transitive that way**, so it is tracked separately, in a
// set covering every unit — see the loop.
func (t TopologyMeta) Disagreement() string {
	if len(t.Units) < 2 {
		return ""
	}

	// Which unit first claimed each authority, so a repeat can name both ends of the clash
	// rather than only the newcomer.
	claimedBy := make(map[string]string, len(t.Units))

	first := t.Units[0]
	for _, unit := range t.Units {
		switch {
		case unit.Meta.Revision != first.Meta.Revision:
			return fmt.Sprintf("units are running different code: %s reports revision %q and %s reports %q, "+
				"so the run measured two services and the manifest can name only one",
				first.Target, first.Meta.Revision, unit.Target, unit.Meta.Revision)
		case unit.Meta.Placement.RoutingVersion != first.Meta.Placement.RoutingVersion:
			return fmt.Sprintf("units disagree about routing: %s is serving version %q and %s version %q — "+
				"two placements in one run can write an organisation to two authorities and divide its "+
				"source of truth",
				first.Target, first.Meta.Placement.RoutingVersion, unit.Target, unit.Meta.Placement.RoutingVersion)
		case unit.Meta.Database.SchemaVersion != first.Meta.Database.SchemaVersion:
			return fmt.Sprintf("authorities are at different schema versions: %s at %d and %s at %d, "+
				"so a per-authority result cannot be compared with its peer's",
				first.Target, first.Meta.Database.SchemaVersion, unit.Target, unit.Meta.Database.SchemaVersion)
		}

		if field, mine, theirs := runShapeMismatch(first.Meta, unit.Meta); field != "" {
			return fmt.Sprintf("units were not running the same configuration: %s reports %s %s and "+
				"%s reports %s. The manifest records one value for the whole run, so a report built "+
				"from these describes a deployment that does not exist",
				first.Target, field, mine, unit.Target, theirs)
		}

		// Every unit is checked against every earlier one, not against the first alone. With
		// three or more units, two *later* units can share an authority the first does not
		// hold — one endpoint pointed at the wrong service, say — which leaves an authority
		// unreached while the run believes it covered the whole topology. Comparing only with
		// the first unit returns no disagreement for exactly that case.
		if id := unit.Meta.Placement.AuthorityID; id != "" {
			if earlier, repeated := claimedBy[id]; repeated {
				return fmt.Sprintf("%s and %s both report authority %q: either the topology is misconfigured or "+
					"the run reached one unit twice, and in both cases it is not the topology it claims",
					earlier, unit.Target, id)
			}
			claimedBy[id] = unit.Target
		}
	}
	return ""
}

// runShapeMismatch names the first field two units disagree on that the manifest records
// once for the whole run, or returns "" for field when they agree.
//
// These are the fields NewTopologyManifest projects from the first unit. Each of them
// changes what the numbers mean rather than merely describing the deployment: a pool ceiling
// is one of the admission boundaries a frontier is read against, a telemetry mode decides the
// observation cost, and a reservation TTL decides how long each admitted reserve holds
// capacity and therefore the contention the workload produced.
func runShapeMismatch(first, other ServiceMeta) (field, mine, theirs string) {
	switch {
	case other.Modified != first.Modified:
		return "source-modified", fmt.Sprintf("%t", first.Modified), fmt.Sprintf("%t", other.Modified)
	case other.GoVersion != first.GoVersion:
		return "Go version", first.GoVersion, other.GoVersion
	case other.GOMAXPROCS != first.GOMAXPROCS:
		return "GOMAXPROCS", fmt.Sprintf("%d", first.GOMAXPROCS), fmt.Sprintf("%d", other.GOMAXPROCS)
	case other.TelemetryMode != first.TelemetryMode:
		return "telemetry mode", first.TelemetryMode, other.TelemetryMode
	case other.TimeoutBudgetString() != first.TimeoutBudgetString():
		return "timeout budget", first.TimeoutBudgetString(), other.TimeoutBudgetString()
	case other.ReservationTTL != first.ReservationTTL:
		return "reservation TTL", first.ReservationTTL, other.ReservationTTL
	case other.Database.Version != first.Database.Version:
		return "PostgreSQL version", first.Database.Version, other.Database.Version
	case other.Database.PoolMaxConns != first.Database.PoolMaxConns:
		return "pool ceiling", fmt.Sprintf("%d", first.Database.PoolMaxConns),
			fmt.Sprintf("%d", other.Database.PoolMaxConns)
	default:
		return "", "", ""
	}
}

// Authorities lists the authorities the units reported, sorted. It is the manifest's record
// of what the run actually reached, as opposed to what the placement map said should exist.
func (t TopologyMeta) Authorities() []string {
	out := make([]string, 0, len(t.Units))
	for _, unit := range t.Units {
		if id := unit.Meta.Placement.AuthorityID; id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Assignment is the organisation-to-authority mapping the units themselves report, rendered
// deterministically for the manifest.
//
// It is read back from the units rather than copied from the placement file the harness
// loaded. Those are two different facts — what the operator intended and what is actually
// serving — and recording the intention as though it were the observation is how a
// misconfigured run comes to look correct in its own artifact.
func (t TopologyMeta) Assignment() map[string][]string {
	assignment := map[string][]string{}
	for _, unit := range t.Units {
		id := unit.Meta.Placement.AuthorityID
		if id == "" {
			continue
		}
		orgs := append([]string(nil), unit.Meta.Placement.Organisations...)
		sort.Strings(orgs)
		assignment[id] = orgs
	}
	return assignment
}

// RoutingVersion is the version every unit agrees on; empty when there are no units. Callers
// should check Disagreement first — this returns the first unit's answer and does not itself
// establish that the others share it.
func (t TopologyMeta) RoutingVersion() string {
	if len(t.Units) == 0 {
		return ""
	}
	return t.Units[0].Meta.Placement.RoutingVersion
}

// DisagreementWith is the full certification gate: the units describe one deployment, *and*
// that deployment is the one the generator routed by.
//
// Disagreement alone compares the units with each other, which leaves a gap that equal
// routing-version *labels* cannot close. A version string is an assertion about placement,
// not the placement itself, so units can agree on the label while serving a different map
// from the generator's — and a run in which the generator routes by one assignment and the
// services enforce another produces refusals that look like a service defect, or worse,
// admissions the placement never sanctioned.
//
// The comparison is therefore over content: the authority set, and the exact set of
// organisations each authority claims. A zero placement skips it — a single-target run has no
// map to compare against, and inventing one would refuse every run made before PR3b.
//
// **Content equality and version equality are separate gates, and both are required.** The
// contract is the same *versioned* map, so equal content under different version labels is
// still a disagreement: the manifest records the version the units report, and a generator
// routing by a document labelled differently makes that record name a map the run did not
// use. Two documents agreeing today is not evidence they are the same document, and the
// artifact has to say which one produced the numbers.
func (t TopologyMeta) DisagreementWith(routed domain.Placement) string {
	if disagreement := t.Disagreement(); disagreement != "" {
		return disagreement
	}
	if routed.IsZero() || routed.IsUnsharded() {
		return ""
	}

	if serving := t.RoutingVersion(); routed.Version() != serving {
		return fmt.Sprintf("the generator routed by placement version %q but the units are "+
			"serving %q: the manifest records the version the units report, so it would name "+
			"a map this run did not route by — and two documents agreeing on content today is "+
			"not evidence they are the same document",
			routed.Version(), serving)
	}

	// The generator's map, rendered the same way the units' answers are, so the two are
	// compared as like with like rather than through two different normalisations.
	intended := map[string][]string{}
	for _, authority := range routed.Authorities() {
		orgs := make([]string, 0)
		for _, org := range routed.Organisations(authority) {
			orgs = append(orgs, string(org))
		}
		sort.Strings(orgs)
		intended[string(authority)] = orgs
	}
	observed := t.Assignment()

	for _, authority := range sortedKeys(intended) {
		serving, reached := observed[authority]
		if !reached {
			return fmt.Sprintf("the generator routes organisations %v to authority %q under "+
				"version %q, but no unit in this run reported serving it: that organisation's "+
				"traffic went to a unit that does not own it",
				intended[authority], authority, routed.Version())
		}
		if !equalStrings(intended[authority], serving) {
			return fmt.Sprintf("authority %q serves %v but the generator routes %v to it under "+
				"version %q: the two are working from different placements while reporting the "+
				"same version",
				authority, serving, intended[authority], routed.Version())
		}
	}
	for _, authority := range sortedKeys(observed) {
		if _, routes := intended[authority]; !routes {
			return fmt.Sprintf("authority %q is serving in this run but version %q routes nothing "+
				"to it: the run reached a unit it never exercised, and whatever that unit owns "+
				"went somewhere else", authority, routed.Version())
		}
	}

	// Sharded mode last, because a topology whose assignment already matches is far more
	// likely to be misreporting this flag than to be genuinely unsharded.
	for _, unit := range t.Units {
		if len(t.Units) > 1 && !unit.Meta.Placement.Sharded {
			return fmt.Sprintf("%s reports itself unsharded, but the run reached %d units under "+
				"routing version %q: a unit that believes it owns every organisation will not "+
				"refuse the ones it does not", unit.Target, len(t.Units), routed.Version())
		}
	}
	return ""
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PlacementDigest is a stable fingerprint of the assignment the units actually reported.
//
// The routing version is a label an operator chooses, so two runs carrying the same version
// are not thereby known to have run the same placement — a map edited without a version bump
// is exactly the mistake that produces two incomparable runs which look comparable. This is
// derived from the content, so comparing two reports' digests answers the question the label
// only claims to.
// The canonical form is built first and hashed once, with separators that cannot occur in an
// identifier, so no pair of distinct assignments can render to the same bytes.
func (t TopologyMeta) PlacementDigest() string {
	assignment := t.Assignment()
	if len(assignment) == 0 {
		return ""
	}

	var canonical strings.Builder
	for _, authority := range sortedKeys(assignment) {
		canonical.WriteString(authority)
		canonical.WriteByte(0)
		for _, org := range assignment[authority] {
			canonical.WriteString(org)
			canonical.WriteByte(1)
		}
		canonical.WriteByte(2)
	}

	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

// DriftFrom names how the topology changed between two reads of every unit's /meta, or
// returns "" when it did not. It is ServiceMeta.DriftFrom raised to a multi-unit run: the
// pre-run read establishes only which units were behind the endpoints when the run began,
// and this is what turns that into a claim about the whole sample (DEBT-3).
//
// The unit *set* is compared before any unit's contents, because a topology that lost or
// gained a unit mid-run is a different failure from one whose units changed underneath it —
// and reporting the second when the first happened sends the operator to the wrong terminal.
// A unit that stopped answering /meta is drift by itself: the run cannot confirm what it
// measured, which is exactly the state that must not certify.
func (t TopologyMeta) DriftFrom(before TopologyMeta) string {
	seen := make(map[string]ServiceMeta, len(before.Units))
	for _, unit := range before.Units {
		seen[unit.Target] = unit.Meta
	}

	for _, unit := range t.Units {
		if _, ok := seen[unit.Target]; !ok {
			return fmt.Sprintf("%s answered /meta after the run but not before, so part of the "+
				"workload ran against a topology that did not include it", unit.Target)
		}
	}

	for _, unit := range t.Units {
		if drift := unit.Meta.DriftFrom(seen[unit.Target]); drift != "" {
			return fmt.Sprintf("%s: %s", unit.Target, drift)
		}
		delete(seen, unit.Target)
	}

	// Sorted, so a topology that lost several units names the same one every time. A drift
	// message that varies between identical runs is one an operator learns to distrust.
	missing := make([]string, 0, len(seen))
	for target := range seen {
		missing = append(missing, target)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Sprintf("%s answered /meta before the run but not after: the unit that served "+
			"part of the workload is no longer reachable, so the run cannot confirm what it measured",
			strings.Join(missing, ", "))
	}
	return ""
}
