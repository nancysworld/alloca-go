package loadgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Declaration carries the facts about a deployment that nothing the generator can reach will
// tell it, and that `measurement-contract.md` §13.2 gates the `capacity` level on.
//
// **It is deliberately a separate document from Deployment.** That record is *observed* —
// written from the running containers, because a process cannot see which image wraps it
// (ADR-0003). This one is *declared*: an operator states it, and nothing can check it against
// the world. Merging the two would put both provenance classes in one artifact, and the weaker
// one would inherit the stronger one's credibility. A reader must be able to tell which fields
// were read off a deployment and which were typed.
//
// **It is a file rather than flags** for the same reason the deployment record is: the fields
// describe one coherent run shape, they are parsed and checked together, and a capacity point's
// declaration is written once and reused by every rung of that point's ladder rather than
// retyped per run. A mistyped flag is discovered when a run that has already been driven fails
// to certify, which is the expensive moment on metered infrastructure.
//
// **Only what cannot be derived belongs here.** Replica count and aggregate pool capacity are
// observable from the units the run addressed and are therefore not declared; see ReplicaFanOut
// for the one topology where the first of those stops being true.
type Declaration struct {
	// Environment names where the run was measured, in whatever terms make the result
	// interpretable later — instance shapes, region, or the workstation. Free text because a
	// closed vocabulary would be wrong within one milestone.
	Environment string `json:"environment"`

	// DeploymentTopology names the shape that was served, as the operator understands it:
	// "4 shard groups, one service and one PostgreSQL authority per EC2 capacity unit".
	DeploymentTopology string `json:"deployment_topology"`

	// ReplicaFanOut is set only by a deployment where the generator cannot see every replica.
	//
	// The replica count is otherwise the number of units the run addressed, which is exact
	// while the generator reaches each service directly — the case for every topology this
	// repository has run, and for Iteration C's one-service-per-capacity-unit design. Put a
	// load balancer in front of a unit and the two diverge, with no way for an HTTP client to
	// notice: the run would report one replica per endpoint for a deployment that had four.
	//
	// So the divergence is declarable but never silent, and never merely a number.
	ReplicaFanOut *ReplicaFanOut `json:"replica_fan_out,omitempty"`
}

// ReplicaFanOut declares that more service replicas serve the run than it has endpoints.
type ReplicaFanOut struct {
	// ReplicaCount is the total across every endpoint, not the count behind one of them.
	ReplicaCount int `json:"replica_count"`
	// AggregatePoolSize is the deployment's total connection ceiling.
	//
	// Declared here rather than summed from the units, because whatever hides the replicas
	// hides their pools with them: the generator can read a ceiling from each endpoint that
	// answers, and a fanned-out deployment has replicas that none of them speaks for. Summing
	// what answered would under-report the deployment's real concurrency against the database
	// — the number a frontier is read against.
	AggregatePoolSize int `json:"aggregate_pool_size"`
	// Because states what stands between the generator and the replicas. It is required, so
	// overriding a derived fact costs a sentence rather than a keystroke — an unexplained
	// override is the shape this field exists to prevent.
	Because string `json:"because"`
}

// LoadDeclaration reads a declaration and refuses one that cannot support a capacity claim.
//
// Every check here is local: nothing is fetched, so a malformed declaration costs no
// experiment time. The checks that need the units are in ReconcileWith.
func LoadDeclaration(path string) (Declaration, error) {
	var d Declaration

	raw, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("reading declaration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Unknown fields are refused rather than ignored: a misspelled key in a hand-written
	// document is silently absent otherwise, and the run fails certification for a field the
	// operator believes they supplied.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, fmt.Errorf("decoding declaration %s: %w", path, err)
	}

	if d.Environment == "" {
		return d, fmt.Errorf("declaration %s sets no environment: a capacity result that does "+
			"not say where it was measured is not interpretable, even unpublished", path)
	}
	if d.DeploymentTopology == "" {
		return d, fmt.Errorf("declaration %s sets no deployment_topology: the shape served is "+
			"what a later run is compared against", path)
	}
	if f := d.ReplicaFanOut; f != nil {
		if f.ReplicaCount < 1 {
			return d, fmt.Errorf("declaration %s declares a replica fan-out with replica_count "+
				"%d", path, f.ReplicaCount)
		}
		if f.AggregatePoolSize < 1 {
			return d, fmt.Errorf("declaration %s declares a replica fan-out but no "+
				"aggregate_pool_size: whatever hides the replicas hides their connection "+
				"ceilings too, so the total cannot be summed from the endpoints that answered",
				path)
		}
		if f.Because == "" {
			return d, fmt.Errorf("declaration %s declares a replica fan-out of %d without "+
				"saying why: the replica count is otherwise the number of units the run "+
				"addressed, and overriding an observed fact with an unexplained number is how "+
				"a deployment comes to be described by what someone expected it to be",
				path, f.ReplicaCount)
		}
	}
	return d, nil
}

// ReplicaCount is the number of service replicas that served a run across the given units.
//
// Derived from the units unless the deployment declared that it fans out behind them, because
// the generator addressed each of them and can count.
func (d Declaration) ReplicaCount(unitCount int) int {
	if d.ReplicaFanOut != nil {
		return d.ReplicaFanOut.ReplicaCount
	}
	return unitCount
}

// ReconcileWith checks the declaration against what the units actually reported, before any
// measured request is driven.
//
// The alternative is certification, which runs after the ladder has been driven. That is a
// tolerable place to discover a wrong number on a workstation and the wrong one on metered
// infrastructure, where the cost of finding out late is a rung nobody can quote.
func (d Declaration) ReconcileWith(topology TopologyMeta) error {
	if disagreement := topology.Disagreement(); disagreement != "" {
		return fmt.Errorf("the units do not describe one deployment: %s", disagreement)
	}
	if f := d.ReplicaFanOut; f != nil && f.ReplicaCount < len(topology.Units) {
		return fmt.Errorf("the declaration says %d replicas serve this run (%s) but it "+
			"addressed %d units: a fan-out adds replicas behind an endpoint, so it cannot "+
			"describe fewer of them than the run reached",
			f.ReplicaCount, f.Because, len(topology.Units))
	}
	return nil
}

// AggregatePoolSize is the deployment's total connection ceiling: summed from what each unit
// reports, or taken from the declaration when a fan-out means the units cannot speak for the
// whole deployment.
//
// Summed rather than multiplied out from one unit's value. The product form restates an
// invariant as arithmetic, and the invariant is separately enforced — runShapeMismatch
// compares the pool ceiling across units, and a deployment whose units disagree cannot
// certify. Reading each unit's own answer keeps the field a description of what served the
// run rather than a calculation about it.
func (d Declaration) AggregatePoolSize(topology TopologyMeta) int {
	if d.ReplicaFanOut != nil {
		return d.ReplicaFanOut.AggregatePoolSize
	}
	total := 0
	for _, unit := range topology.Units {
		total += unit.Meta.Database.PoolMaxConns
	}
	return total
}
