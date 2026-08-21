package loadgen

import (
	"fmt"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Stream is one shard group's independent closed-loop demand: a fixed worker pool that
// offers work to exactly one group.
//
// Iteration C's capacity comparison rests on a property one shared pool cannot hold. With
// global workers taking groups in turn, a worker blocked on a slow group stops issuing to
// the healthy ones as well, so one group's saturation arrives in the artifact as a
// topology-wide throughput drop and the experiment attributes a local stall to the whole
// family. Independence is therefore structural here — a pool, a sequence and a collector per
// group — rather than a property the operator is trusted to preserve
// (ag-sept/milestone-validation.md §4.6.1, VAL-NEG-8).
type Stream struct {
	// Group is the stable shard-group identity. It labels this stream's accounting and
	// namespaces the idempotency keys its workload mints.
	Group string
	// Workload draws only on the organisations this group owns. Its per-group instance is
	// what makes the demand independent; a workload shared between streams would walk one
	// dataset from several pools.
	Workload Workload
}

// keyNamespacer is a workload whose idempotency keys can be scoped to one stream.
//
// Every stream starts its own sequence at zero, so two groups running the same workload
// would otherwise mint the same (workload, seq, step) key. That is not a collision the
// service would notice — the keys land on different authorities, each of which sees each key
// once — which is exactly why it has to be prevented here: nothing downstream would report
// it, and the retained artifact could not say which stream a key belonged to (VAL-NEG-8).
type keyNamespacer interface {
	keyNamespace() string
}

// NewMutDisp4Streams splits `WL-MUT-DISP-4`'s populations into one independent stream per
// shard group, following the placement that assigns organisations to authorities.
//
// The populations stay per-organisation and unchanged across topologies — that is the
// workload's identity (workload-catalog.md, "WL-MUT-DISP-4 — Population") — and only their
// grouping moves. G1 therefore yields one stream owning all four, G4 four streams owning one
// each, from the same fixture.
func NewMutDisp4Streams(populations []OrgPopulation, groups []OrgGroup, confirm bool, phase Phase) ([]Stream, error) {
	if len(groups) == 0 {
		return nil, fmt.Errorf("loadgen: wl-mut-disp-4 streams need at least one shard group")
	}

	byOrg := make(map[domain.OrganisationID]OrgPopulation, len(populations))
	for _, population := range populations {
		byOrg[population.Org] = population
	}

	placed := 0
	streams := make([]Stream, 0, len(groups))
	for _, group := range groups {
		// The group's own populations, in the catalog's order rather than the placement's,
		// for the reason NewOrgPopulations documents: demand-to-organisation mapping is a
		// property of the workload, and deriving it from the placement would let G2 shuffle
		// the mapping G1 used and report the difference as scale efficiency.
		var owned []OrgPopulation
		for _, population := range populations {
			for _, org := range group.Orgs {
				if population.Org == org {
					owned = append(owned, population)
					break
				}
			}
		}
		if len(owned) == 0 {
			return nil, fmt.Errorf("loadgen: shard group %q owns none of the wl-mut-disp-4 "+
				"organisations, so its workers would have nothing to offer", group.Authority)
		}
		placed += len(owned)

		streams = append(streams, Stream{
			Group: string(group.Authority),
			Workload: MutDisp4{
				Orgs:    owned,
				Confirm: confirm,
				Group:   string(group.Authority),
				Phase:   phase,
			},
		})
	}

	// Every organisation must be driven exactly once. A placement that dropped one would
	// quietly compare a three-organisation run against a four-organisation baseline, and a
	// placement that repeated one would drive its fixture from two pools at once — both of
	// which change the workload rather than the topology.
	if placed != len(populations) {
		return nil, fmt.Errorf("loadgen: the placement drives %d of the %d wl-mut-disp-4 "+
			"organisations; each must be owned by exactly one shard group", placed, len(populations))
	}

	return streams, nil
}

// validateStreams refuses a stream set that could not produce attributable accounting.
func validateStreams(streams []Stream) error {
	if len(streams) == 0 {
		return fmt.Errorf("loadgen: a run needs at least one demand stream")
	}

	groups := make(map[string]bool, len(streams))
	namespaces := make(map[string]bool, len(streams))
	for _, stream := range streams {
		if len(streams) > 1 && stream.Group == "" {
			return fmt.Errorf("loadgen: a multi-group run needs a group identity on every " +
				"stream, so per-group accounting and idempotency keys can be attributed")
		}
		if groups[stream.Group] {
			return fmt.Errorf("loadgen: shard group %q has two demand streams; one pool per "+
				"group is what makes the groups independent", stream.Group)
		}
		groups[stream.Group] = true

		// The key namespace is checked rather than assumed: a workload that ignores its
		// group would mint one key space from several streams, and the run would report
		// replays it did not intend.
		scoped, ok := stream.Workload.(keyNamespacer)
		if !ok {
			continue
		}
		if namespaces[scoped.keyNamespace()] {
			return fmt.Errorf("loadgen: two streams mint idempotency keys in namespace %q; "+
				"each stream starts its sequence at zero, so they would mint the same keys",
				scoped.keyNamespace())
		}
		namespaces[scoped.keyNamespace()] = true
	}
	return nil
}

// SplitPopulationsForConditioning divides each organisation's seeded slots into a
// conditioning population and a measured population.
//
// The two phases must not compete for the same rows. Conditioning exists to leave
// representative table state behind — rows in `idempotency_records`, `reservations` and the
// claim tables, so the pool's connections plan against a populated database rather than an
// empty one — and it consumes slot capacity to do it. If it drew from the measured
// population's slots it would arrive at the measured interval having already spent part of
// the fixture the capacity point depends on, and the resulting `business_refusal` population
// would invalidate the point while looking like contention (measurement-contract §5,
// useful-demand / fixture-headroom gate).
//
// The split is returned as a pair from one function rather than computed twice because that
// is the only way the two phases cannot disagree about where the boundary is. A caller that
// derived each side separately would have two expressions that must stay equal, with nothing
// tying them together.
func SplitPopulationsForConditioning(populations []OrgPopulation, conditioningSlotsPerOrg int) (conditioning, measured []OrgPopulation, err error) {
	if conditioningSlotsPerOrg <= 0 {
		return nil, nil, fmt.Errorf("loadgen: conditioning needs at least one slot per " +
			"organisation; a phase with no population establishes no state")
	}

	for _, population := range populations {
		if len(population.Slots) <= conditioningSlotsPerOrg {
			return nil, nil, fmt.Errorf("loadgen: organisation %q was seeded %d slots and "+
				"conditioning wants %d of them, leaving the measured population nothing to "+
				"book; the fixture must cover conditioning *and* the measured interval",
				population.Org, len(population.Slots), conditioningSlotsPerOrg)
		}

		// Conditioning takes the head of each organisation's slice and the measured
		// population takes the tail. Which end is arbitrary; that they are taken from one
		// slice in one place is not.
		conditioning = append(conditioning, OrgPopulation{
			Org:   population.Org,
			Slots: population.Slots[:conditioningSlotsPerOrg],
		})
		measured = append(measured, OrgPopulation{
			Org:   population.Org,
			Slots: population.Slots[conditioningSlotsPerOrg:],
		})
	}

	return conditioning, measured, nil
}
