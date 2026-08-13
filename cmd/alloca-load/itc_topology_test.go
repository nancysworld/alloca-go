package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// The Iteration C matrix, exactly as ag-sept-validation-plan.md §4.6 fixes it: the same four
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

// endpointsFor gives every authority a distinct placeholder address. The router refuses a map
// with a missing or surplus endpoint, so this has to match the document exactly.
func endpointsFor(placement domain.Placement) map[domain.AuthorityID]string {
	endpoints := map[domain.AuthorityID]string{}
	for i, authority := range placement.Authorities() {
		endpoints[authority] = fmt.Sprintf("http://127.0.0.1:%d", 8081+i)
	}
	return endpoints
}
