package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// The Iteration C matrix, exactly as ag-sept/milestone-validation.md §4.6 fixes it: the same four
// organisations at every topology, spread over one, two and four shard groups.
var itcMatrix = []struct {
	groups      int
	version     string
	authorities []domain.AuthorityID
	// orgsPerAuthority is 4/groups at every rung, which is what makes the capacity units
	// like-for-like. An uneven split would put more organisations — and so more data and more
	// demand — behind one authority than another, and the efficiency figure would carry that
	// imbalance rather than the architecture.
	orgsPerAuthority int
}{
	{1, "itc-g1", []domain.AuthorityID{"authority-1"}, 4},
	{2, "itc-g2", []domain.AuthorityID{"authority-1", "authority-2"}, 2},
	{4, "itc-g4", []domain.AuthorityID{
		"authority-1", "authority-2", "authority-3", "authority-4"}, 1},
}

func itcPlacement(t *testing.T, groups int) domain.Placement {
	t.Helper()
	path := filepath.Join("..", "..", "deploy", "topology",
		fmt.Sprintf("placement-itc-g%d.json", groups))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the shipped placement document: %v", err)
	}
	placement, err := domain.ParsePlacement(raw)
	if err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	return placement
}

// The shipped documents are the topology. `make itc-up` pairs each with the Compose profile
// that raises exactly the authorities it names, so a document naming more authorities than its
// profile raises produces the dangerous failure the target's comment describes: the extra
// organisations route to units that are not running.
//
// This is the assertion that stops the two drifting apart, and it is checked against the files
// the experiment will actually mount rather than against a fixture written here.
func TestShippedIterationCPlacementsDescribeTheFixedMatrix(t *testing.T) {
	for _, want := range itcMatrix {
		t.Run(fmt.Sprintf("G%d", want.groups), func(t *testing.T) {
			placement := itcPlacement(t, want.groups)

			if placement.Version() != want.version {
				t.Errorf("routing version is %q, want %q; a manifest could not tell the "+
					"capacity points apart", placement.Version(), want.version)
			}

			// ag-sept-pr4.md §2.9: G1 is a sharded one-authority placement, never Unsharded.
			// Unsharded makes
			// placement enforcement inert and reports routing_version "unsharded", so a G1 built
			// that way would run a different code path from G2 and G4 and carry provenance that
			// cannot be compared with theirs.
			if placement.IsUnsharded() {
				t.Error("placement is Unsharded; routing enforcement would be inert and this " +
					"capacity point would not be comparable with the others")
			}

			got := placement.Authorities()
			if len(got) != len(want.authorities) {
				t.Fatalf("names %d authorities %v, want %d %v",
					len(got), got, len(want.authorities), want.authorities)
			}
			for i, authority := range want.authorities {
				if got[i] != authority {
					t.Errorf("authority %d is %q, want %q", i, got[i], authority)
				}
			}

			for _, authority := range got {
				if owned := placement.Organisations(authority); len(owned) != want.orgsPerAuthority {
					t.Errorf("authority %q owns %d organisations %v, want %d; the capacity "+
						"units are not like-for-like", authority, len(owned), owned, want.orgsPerAuthority)
				}
			}
		})
	}
}

// Every topology must place the same four organisations. The workload is defined over A/B/C/D
// (workload-catalog.md), and a document that dropped or renamed one would change the workload
// between capacity points while every routing check still passed.
func TestShippedIterationCPlacementsCarryTheSameFourOrganisations(t *testing.T) {
	want := []domain.OrganisationID{"org-a", "org-b", "org-c", "org-d"}

	for _, rung := range itcMatrix {
		placement := itcPlacement(t, rung.groups)

		placed := map[domain.OrganisationID]bool{}
		for _, authority := range placement.Authorities() {
			for _, org := range placement.Organisations(authority) {
				if placed[org] {
					t.Errorf("G%d places %q on more than one authority; its source of truth "+
						"would be divided", rung.groups, org)
				}
				placed[org] = true
			}
		}

		if len(placed) != len(want) {
			t.Errorf("G%d places %d organisations, want %d", rung.groups, len(placed), len(want))
		}
		for _, org := range want {
			if !placed[org] {
				t.Errorf("G%d does not place %q", rung.groups, org)
			}
		}
	}
}

// The generator must be able to build WL-MUT-DISP-4 from each shipped document, and get the
// same four populations every time.
//
// This drives the real construction path — the same orgPopulations the command calls — so it
// covers the join between the two halves PR4a delivers: a topology document that is well-formed
// but names organisations the workload is not defined over would pass every test above and fail
// at the first request of an expensive run.
func TestEveryShippedTopologyCanBuildTheCapacityWorkload(t *testing.T) {
	const slotsPerOrg = 40

	var baseline []domain.OrganisationID
	for _, rung := range itcMatrix {
		placement := itcPlacement(t, rung.groups)
		router, err := loadgen.NewRouter(placement, endpointsFor(placement))
		if err != nil {
			t.Fatalf("G%d: building router: %v", rung.groups, err)
		}

		populations, dataset, err := orgPopulations(workloadSpec{Router: router, Slots: slotsPerOrg})
		if err != nil {
			t.Fatalf("G%d: %v", rung.groups, err)
		}

		// The dataset a run reports is the whole fixture, not one organisation's share of it,
		// and it must not change with the topology — the catalog's "fixed for a comparison"
		// rule is what makes G1 and G4 comparable at all.
		if want := slotsPerOrg * 4; dataset != want {
			t.Errorf("G%d reports a dataset of %d slots, want %d", rung.groups, dataset, want)
		}

		ordered := make([]domain.OrganisationID, len(populations))
		for i, population := range populations {
			ordered[i] = population.Org
		}
		if baseline == nil {
			baseline = ordered
			continue
		}
		for i := range baseline {
			if ordered[i] != baseline[i] {
				t.Errorf("G%d assigns demand in order %v, but G%d assigns it %v; the same seq "+
					"would address a different organisation at each topology",
					rung.groups, ordered, itcMatrix[0].groups, baseline)
				break
			}
		}
	}
}

// The shipped rehearsal declarations have to load, and there has to be one per capacity point.
//
// Every one of them is a document `itc-run.sh` selects by ITC_GROUPS and hands to a run that
// requires `capacity`. A missing field or a misspelled key is refused by LoadDeclaration — which
// runs *after* the topology is up and the fixture is seeded, so the cost of finding out is the
// setup rather than the typo. JSON validity is not the gate; LoadDeclaration is.
func TestShippedIterationCDeclarationsLoad(t *testing.T) {
	environments := map[int]string{}

	for _, rung := range itcMatrix {
		t.Run(fmt.Sprintf("G%d", rung.groups), func(t *testing.T) {
			path := filepath.Join("..", "..", "deploy", "topology",
				fmt.Sprintf("declaration-itc-g%d.json", rung.groups))

			declaration, err := loadgen.LoadDeclaration(path)
			if err != nil {
				t.Fatalf("%s is not a usable declaration: %v", path, err)
			}
			environments[rung.groups] = declaration.Environment

			// The rehearsal is not independently provisioned capacity, and the declaration is
			// the only place a later reader is told so — ag-sept-pr4.md §2.14 turns on that
			// distinction, and an environment string that omitted it would describe these runs
			// as something they cannot be.
			if !strings.Contains(declaration.Environment, "not independently provisioned") {
				t.Errorf("environment does not record that this is not independent capacity:\n  %q",
					declaration.Environment)
			}
			if !strings.Contains(declaration.Environment, "shared host/kernel/storage") {
				t.Errorf("environment does not record what the groups share:\n  %q",
					declaration.Environment)
			}

			// The generator's CPUs are deliberately absent. The headroom control widens them
			// from 8-11 to 8-15, so any static generator cpuset here would be false for half
			// the runs the document describes; the effective set is retained per run by
			// itc-run.sh instead (maintainer decision, 2026-08-13).
			for _, cpus := range []string{"8-11", "8-15"} {
				if strings.Contains(declaration.Environment, cpus) {
					t.Errorf("environment names the generator cpuset %q; the headroom control "+
						"changes it, so it belongs in the run's artifacts, not in a document "+
						"reused across runs:\n  %q", cpus, declaration.Environment)
				}
			}

			// The shape is what a later run is compared against, so each point must state its
			// own group count rather than inherit a neighbour's.
			if want := fmt.Sprintf("%d shard group", rung.groups); !strings.HasPrefix(
				declaration.DeploymentTopology, want) {
				t.Errorf("deployment_topology is %q, which does not begin %q; the capacity "+
					"points would not be distinguishable from their declarations",
					declaration.DeploymentTopology, want)
			}

			// Nothing hides replicas from the generator in this topology: it addresses each
			// service directly, so the count is observed rather than declared (§2.7). A fan-out
			// here would override an observable fact with a typed one.
			if declaration.ReplicaFanOut != nil {
				t.Error("declares a replica fan-out; the rehearsal addresses every unit " +
					"directly, so the replica count must stay derived from the units")
			}
		})
	}

	// One environment across every rung, by maintainer decision (2026-08-13). G1, G2 and G4 are
	// measured on the same machine under the same partition scheme, and the environment is what
	// makes their numbers comparable at all — a rung whose environment drifted would be
	// compared against the others as though it had not.
	//
	// Only the topology may differ, and that is asserted per rung above.
	baseline, ok := environments[itcMatrix[0].groups]
	if !ok {
		t.Fatal("G1's declaration did not load, so the environments cannot be compared")
	}
	for groups, environment := range environments {
		if environment != baseline {
			t.Errorf("G%d declares a different environment from G%d:\n  G%d: %q\n  G%d: %q",
				groups, itcMatrix[0].groups, groups, environment, itcMatrix[0].groups, baseline)
		}
	}
}

// endpointsFor gives every authority a distinct placeholder address. The router refuses a map
// with a missing or surplus endpoint, so this has to match the document exactly.
func endpointsFor(placement domain.Placement) map[domain.AuthorityID]string {
	endpoints := map[domain.AuthorityID]string{}
	for i, authority := range placement.Authorities() {
		endpoints[authority] = fmt.Sprintf("http://127.0.0.1:%d", 8081+i)
	}
	return endpoints
}

// Each shipped topology must yield exactly one independent demand stream per shard group.
//
// This is the coupling between the placement documents and the generator that nothing else
// checks. A G4 run whose streams collapsed to one would offer a quarter of the intended
// workers, drive four organisations from one pool, and report a `workers_per_group` its
// demand never had — while every other check in the run still passed, because the requests
// themselves would be identical (validation plan §4.6.1).
func TestEveryShippedTopologyBuildsOneDemandStreamPerShardGroup(t *testing.T) {
	for _, rung := range itcMatrix {
		placement := itcPlacement(t, rung.groups)
		router, err := loadgen.NewRouter(placement, endpointsFor(placement))
		if err != nil {
			t.Fatalf("G%d: building router: %v", rung.groups, err)
		}

		streams, err := buildStreams("wl-mut-disp-4", workloadSpec{Router: router, Slots: 40})
		if err != nil {
			t.Fatalf("G%d: building streams: %v", rung.groups, err)
		}
		if len(streams) != rung.groups {
			t.Errorf("G%d builds %d demand streams, want one per shard group",
				rung.groups, len(streams))
			continue
		}

		groups := map[string]bool{}
		for _, stream := range streams {
			if groups[stream.Group] {
				t.Errorf("G%d has two streams for group %q", rung.groups, stream.Group)
			}
			groups[stream.Group] = true
		}
		for _, authority := range rung.authorities {
			if !groups[string(authority)] {
				t.Errorf("G%d builds no demand stream for %q, so that capacity unit would "+
					"be measured with no offered demand", rung.groups, authority)
			}
		}
	}
}

// -workers-per-group is refused for every workload except the one whose groups it means.
//
// The other shapes are single-authority controls and Iteration B correctness coverage whose
// retained evidence means one shared pool. Silently giving them a per-group pool would
// multiply their offered demand by the group count and leave the artifacts comparable to
// their predecessors in every field that would show it.
func TestPerGroupWorkersAreRefusedForWorkloadsWithoutShardGroups(t *testing.T) {
	placement := itcPlacement(t, 4)
	router, err := loadgen.NewRouter(placement, endpointsFor(placement))
	if err != nil {
		t.Fatalf("building router: %v", err)
	}

	for _, workload := range []string{"dispersed", "hot-slot", "hot-identity", "replay",
		"multi-org-dispersed", "hot-organisation", "cross-authority-control"} {
		if _, err := buildStreams(workload, workloadSpec{Router: router, Slots: 40}); err == nil {
			t.Errorf("%s accepted -workers-per-group; only wl-mut-disp-4 has shard groups "+
				"for it to mean anything", workload)
		}
	}
}

// The two phases must claim disjoint slots at every topology, driven through the same path
// the CLI uses rather than through the splitter directly.
//
// This is the check that the flags, the spec and the split agree. Each of them is individually
// correct in a way that would still let the measured run book slots conditioning had already
// spent — and the resulting refusals would look like contention, which is exactly what the
// workload is designed to produce.
func TestConditioningAndMeasuredPhasesClaimDisjointSlots(t *testing.T) {
	const conditioningSlots = 10

	for _, rung := range itcMatrix {
		placement := itcPlacement(t, rung.groups)
		router, err := loadgen.NewRouter(placement, endpointsFor(placement))
		if err != nil {
			t.Fatalf("G%d: building router: %v", rung.groups, err)
		}

		// Keyed on the pair, not the slot id. Every organisation is seeded its own `slot-0`
		// upward, so a map keyed on the id alone reports four organisations' distinct slots
		// as one slot claimed four times — which is what this test did on its first run.
		claimed := map[loadgen.Slot]loadgen.Phase{}
		for _, phase := range []loadgen.Phase{loadgen.PhaseConditioning, loadgen.PhaseMeasured} {
			streams, err := buildStreams("wl-mut-disp-4", workloadSpec{
				Router:            router,
				Slots:             40,
				Phase:             phase,
				ConditioningSlots: conditioningSlots,
			})
			if err != nil {
				t.Fatalf("G%d %s: building streams: %v", rung.groups, phase, err)
			}

			var slots int
			for _, stream := range streams {
				workload, ok := stream.Workload.(loadgen.MutDisp4)
				if !ok {
					t.Fatalf("G%d: stream %q does not drive wl-mut-disp-4", rung.groups, stream.Group)
				}
				for _, population := range workload.Orgs {
					for _, slot := range population.Slots {
						if owner, taken := claimed[slot]; taken {
							t.Errorf("G%d: slot %v is claimed by both the %s and %s phases",
								rung.groups, slot, phaseName(owner), phaseName(phase))
						}
						claimed[slot] = phase
						slots++
					}
				}
			}

			want := conditioningSlots * 4
			if phase == loadgen.PhaseMeasured {
				want = (40 - conditioningSlots) * 4
			}
			if slots != want {
				t.Errorf("G%d %s phase drives %d slots, want %d", rung.groups, phase, slots, want)
			}
		}
	}
}

// phaseName renders the measured phase's empty zero value readably in a failure message.
func phaseName(p loadgen.Phase) string {
	if p == loadgen.PhaseMeasured {
		return "measured"
	}
	return string(p)
}
