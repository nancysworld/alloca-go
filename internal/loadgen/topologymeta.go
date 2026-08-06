package loadgen

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
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
//   - a **routing version** difference is the split-brain case the design note records as
//     §7.3: two units disagreeing about placement would write one organisation to two
//     authorities and divide its source of truth. It is the most dangerous of the three and
//     the least visible in any total.
//
// Configuration that differs *legitimately* between units — the authority each is bound to,
// the organisations each serves — is deliberately not compared. Those are supposed to differ;
// that is what makes them separate authorities.
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
