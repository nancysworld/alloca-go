package loadgen_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// mutDisp4Slots is the per-organisation dataset every topology in these tests is seeded with.
// Fixed once and reused, exactly as the catalog requires of a real comparison.
//
// It is a **multiple of the organisation count**, and that is load-bearing for the dataset
// coverage assertion below rather than incidental. Indexing the slots by seq instead of by the
// organisation's own counter gives a stride of four through the population; whether that stride
// collapses coverage or merely reorders it depends entirely on gcd(4, len(slots)). At five slots
// the two are coprime, the stride still visits all five, and the defect is invisible — the first
// version of this fixture used five and the assertion passed against the broken workload.
const mutDisp4Slots = 8

// servedRequest is one request as the unit that answered it saw it: the full pair, the
// identity, the key, and which authority received it.
//
// The pair alone would not catch a workload that changed *which* authority a supported pair
// was sent to, and the key would not catch a workload that changed the pair. A scale
// comparison needs all of it to be invariant, so the whole tuple is what gets compared.
type servedRequest struct {
	Authority domain.AuthorityID
	UserOrg   string
	UserID    string
	SlotOrg   string
	SlotID    string
	Key       string
}

// mutDisp4Units records every request each authority is asked to serve, in arrival order per
// authority.
type mutDisp4Units struct {
	mu   sync.Mutex
	seen []servedRequest
}

func newMutDisp4Units(t *testing.T, authorities ...domain.AuthorityID) (*mutDisp4Units, map[domain.AuthorityID]string) {
	t.Helper()
	units := &mutDisp4Units{}
	endpoints := map[domain.AuthorityID]string{}

	for _, authority := range authorities {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				UserOrg string `json:"user_organisation_id"`
				UserID  string `json:"user_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)

			// /v1/slots/{slot_org}/{slot_id}/reservations
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			var slotOrg, slotID string
			if len(parts) >= 4 {
				slotOrg, slotID = parts[2], parts[3]
			}

			units.mu.Lock()
			units.seen = append(units.seen, servedRequest{
				Authority: authority,
				UserOrg:   body.UserOrg,
				UserID:    body.UserID,
				SlotOrg:   slotOrg,
				SlotID:    slotID,
				Key:       r.Header.Get("Idempotency-Key"),
			})
			units.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"outcome": domain.OutcomeAdmittedSuccess, "replay": false, "reservation_id": "res-1",
			})
		}))
		t.Cleanup(srv.Close)
		endpoints[authority] = srv.URL
	}
	return units, endpoints
}

func (u *mutDisp4Units) requests() []servedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]servedRequest(nil), u.seen...)
}

// mutDisp4Fixture seeds the four organisations and drives `requests` sequential units of work
// against the given placement, returning what the units actually served.
//
// The populations are built from the placement's own organisations, the way the command does,
// so this exercises the construction path rather than a hand-ordered slice that would assume
// away the property under test.
func mutDisp4Fixture(t *testing.T, placementDoc string, requests int, authorities ...domain.AuthorityID) []servedRequest {
	t.Helper()

	placement, err := domain.ParsePlacement([]byte(placementDoc))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}
	units, endpoints := newMutDisp4Units(t, authorities...)
	router, err := loadgen.NewRouter(placement, endpoints)
	if err != nil {
		t.Fatalf("building router: %v", err)
	}

	slotsByOrg := map[domain.OrganisationID][]loadgen.Slot{}
	for _, authority := range placement.Authorities() {
		for _, org := range placement.Organisations(authority) {
			slots := make([]loadgen.Slot, mutDisp4Slots)
			for i := range slots {
				slots[i] = loadgen.Slot{OrganisationID: org, SlotID: domain.SlotID(fmt.Sprintf("slot-%d", i))}
			}
			slotsByOrg[org] = slots
		}
	}

	populations, err := loadgen.NewOrgPopulations(slotsByOrg)
	if err != nil {
		t.Fatalf("building populations: %v", err)
	}

	client := loadgen.NewRoutedClient(router, 5*time.Second, true)
	workload := loadgen.MutDisp4{Orgs: populations}
	for seq := range requests {
		for _, resp := range workload.Do(context.Background(), client, seq) {
			if resp.Invalid != "" {
				t.Fatalf("seq %d: %s", seq, resp.Invalid)
			}
		}
	}
	return units.requests()
}

// The three Iteration C topologies, as `ag-sept/milestone-validation.md` §4.6 fixes them.
//
// `G2` deliberately does **not** group the organisations alphabetically. That is the case
// that separates a workload whose demand mapping is its own property from one that inherits
// it from the placement: flattening authority-by-authority here yields C, D, A, B, so a
// dataset ordered by the placement walk would assign every seq to a different organisation
// than `G1` and `G4` do — and the resulting difference in goodput would be read as scale
// efficiency. An alphabetical `G2` would pass either way and prove nothing.
const (
	mutDisp4G1 = `{"version":"itc-g1","homes":{"org-a":"authority-1","org-b":"authority-1",` +
		`"org-c":"authority-1","org-d":"authority-1"}}`
	mutDisp4G2 = `{"version":"itc-g2","homes":{"org-c":"authority-1","org-d":"authority-1",` +
		`"org-a":"authority-2","org-b":"authority-2"}}`
	mutDisp4G4 = `{"version":"itc-g4","homes":{"org-a":"authority-1","org-b":"authority-2",` +
		`"org-c":"authority-3","org-d":"authority-4"}}`
)

// Proof that the request-pair semantics do not change with topology, which is the property
// `workload-catalog.md` requires of `WL-MUT-DISP-4` under "Demand shape": same-organisation
// user/slot pairing for every request, independent of how those organisations are placed onto
// database authorities.
//
// Without it, `E2 = G2 / (2 × G1)` silently measures two things at once — the architecture,
// and whatever the generator did differently at each topology — and nothing downstream could
// tell them apart. Every correctness gate would still pass, because each individual request
// is perfectly valid; the pairs would simply not be the *same* pairs.
//
// So it compares the whole served stream, ordered by seq, across `G1`, `G2` and `G4`. The
// authority differs by construction and is excluded from the comparison; everything a request
// consists of must be identical.
func TestMutDisp4IssuesIdenticalRequestsAtEveryTopology(t *testing.T) {
	const requests = 40

	byTopology := map[string][]servedRequest{
		"G1": mutDisp4Fixture(t, mutDisp4G1, requests, "authority-1"),
		"G2": mutDisp4Fixture(t, mutDisp4G2, requests, "authority-1", "authority-2"),
		"G4": mutDisp4Fixture(t, mutDisp4G4, requests,
			"authority-1", "authority-2", "authority-3", "authority-4"),
	}

	// Requests arrive interleaved across units, so compare them keyed by the idempotency key,
	// which identifies the logical request independently of which unit served it.
	baseline := map[string]servedRequest{}
	for _, req := range byTopology["G1"] {
		baseline[req.Key] = req
	}
	if len(baseline) != requests {
		t.Fatalf("G1 served %d distinct logical requests, want %d", len(baseline), requests)
	}

	for _, topology := range []string{"G2", "G4"} {
		served := byTopology[topology]
		if len(served) != requests {
			t.Errorf("%s served %d requests, want %d", topology, len(served), requests)
		}
		for _, req := range served {
			want, ok := baseline[req.Key]
			if !ok {
				t.Errorf("%s issued key %q, which G1 never issued", topology, req.Key)
				continue
			}
			// Authority is expected to differ — that is the variable under test.
			want.Authority, req.Authority = "", ""
			if want != req {
				t.Errorf("%s changed the request under key %q:\n  G1: %+v\n  %s: %+v",
					topology, req.Key, want, topology, req)
			}
		}
	}
}

// The workload's defining exclusion: no cross-organisation pairs, colocated or otherwise.
//
// It is checked at `G1`, where all four organisations share one authority and every
// cross-organisation pair would therefore be *supported* — so the service would admit it, the
// run would certify, and nothing but this assertion would notice that the workload had
// stopped being WL-MUT-DISP-4.
func TestMutDisp4PairsEveryUserWithItsOwnOrganisation(t *testing.T) {
	for _, req := range mutDisp4Fixture(t, mutDisp4G1, 40, "authority-1") {
		if req.UserOrg != req.SlotOrg {
			t.Errorf("cross-organisation pair: user %q booked a slot owned by %q", req.UserOrg, req.SlotOrg)
		}
	}
}

// Equal demand share per organisation, and each organisation walking its own dataset.
//
// The second half is what stops the round-robin from quietly degenerating: indexing the slots
// by seq rather than by the organisation's own counter gives a stride equal to the number of
// organisations, so each would touch only every fourth slot — a quarter of the seeded
// population carrying the whole run, at a contention level the reported fixture size does not
// describe.
func TestMutDisp4GivesEachOrganisationAnEqualShareOfItsOwnDataset(t *testing.T) {
	const requests = mutDisp4Slots * 4 * 2 // two full passes over every organisation's dataset

	perOrg := map[string]int{}
	slotsTouched := map[string]map[string]struct{}{}
	for _, req := range mutDisp4Fixture(t, mutDisp4G4, requests,
		"authority-1", "authority-2", "authority-3", "authority-4") {
		perOrg[req.UserOrg]++
		if slotsTouched[req.SlotOrg] == nil {
			slotsTouched[req.SlotOrg] = map[string]struct{}{}
		}
		slotsTouched[req.SlotOrg][req.SlotID] = struct{}{}
	}

	if len(perOrg) != 4 {
		t.Fatalf("addressed %d organisations, want 4", len(perOrg))
	}
	for org, count := range perOrg {
		if want := requests / 4; count != want {
			t.Errorf("organisation %q served %d requests, want an equal share of %d", org, count, want)
		}
		if touched := len(slotsTouched[org]); touched != mutDisp4Slots {
			t.Errorf("organisation %q touched %d of its %d seeded slots; the dataset the run "+
				"reports is not the dataset it drove", org, touched, mutDisp4Slots)
		}
	}
}

// Ordering is the workload's own property. Pinned as an explicit expected stream rather than
// as "is sorted", because the assertion has to fail for *any* re-ordering, including one that
// happens to stay sorted while changing which seq addresses which organisation.
func TestMutDisp4AssignsDemandToOrganisationsInAFixedOrder(t *testing.T) {
	want := []struct{ org, slot string }{
		{"org-a", "slot-0"}, {"org-b", "slot-0"}, {"org-c", "slot-0"}, {"org-d", "slot-0"},
		{"org-a", "slot-1"}, {"org-b", "slot-1"}, {"org-c", "slot-1"}, {"org-d", "slot-1"},
		{"org-a", "slot-2"}, {"org-b", "slot-2"}, {"org-c", "slot-2"}, {"org-d", "slot-2"},
	}

	// Keyed by seq via the idempotency key, because the units answer concurrently only in a
	// real run but the stream must be defined per seq regardless.
	bySeq := map[string]servedRequest{}
	for _, req := range mutDisp4Fixture(t, mutDisp4G2, len(want), "authority-1", "authority-2") {
		bySeq[req.Key] = req
	}

	for seq, expected := range want {
		req, ok := bySeq[fmt.Sprintf("wl-mut-disp-4-%d-reserve", seq)]
		if !ok {
			t.Fatalf("seq %d was never served", seq)
		}
		if req.UserOrg != expected.org || req.SlotID != expected.slot {
			t.Errorf("seq %d addressed %s/%s, want %s/%s",
				seq, req.UserOrg, req.SlotID, expected.org, expected.slot)
		}
	}
}

func TestNewOrgPopulationsRefusesAPopulationItCannotDrive(t *testing.T) {
	full := func() map[domain.OrganisationID][]loadgen.Slot {
		return map[domain.OrganisationID][]loadgen.Slot{
			"org-a": {{OrganisationID: "org-a", SlotID: "a-1"}},
			"org-b": {{OrganisationID: "org-b", SlotID: "b-1"}},
			"org-c": {{OrganisationID: "org-c", SlotID: "c-1"}},
			"org-d": {{OrganisationID: "org-d", SlotID: "d-1"}},
		}
	}

	for _, tc := range []struct {
		name    string
		corrupt func(map[domain.OrganisationID][]loadgen.Slot)
		mention string
	}{
		{
			name:    "three organisations is not the catalog workload",
			corrupt: func(m map[domain.OrganisationID][]loadgen.Slot) { delete(m, "org-d") },
			mention: "organisations",
		},
		{
			// The case a count check cannot see. Four organisations, correctly seeded, every
			// slot owned by its own organisation — and not this workload. `Name()` would still
			// report `wl-mut-disp-4`, and nothing downstream re-derives the population from the
			// artifact, so this run would be compared against a real one as an equal.
			name: "four organisations, but not the catalog's four",
			corrupt: func(m map[domain.OrganisationID][]loadgen.Slot) {
				delete(m, "org-d")
				m["org-e"] = []loadgen.Slot{{OrganisationID: "org-e", SlotID: "e-1"}}
			},
			// Asserted on the phrase unique to the participant-set branch. "org-d" alone also
			// appears in the empty-population message, so it would pass on a build that had
			// stopped checking the set and merely tripped over a nil slice.
			mention: "nothing was seeded for",
		},
		{
			name: "the right count with one participant renamed",
			corrupt: func(m map[domain.OrganisationID][]loadgen.Slot) {
				delete(m, "org-a")
				m["org-z"] = []loadgen.Slot{{OrganisationID: "org-z", SlotID: "z-1"}}
			},
			mention: "nothing was seeded for",
		},
		{
			name:    "an organisation seeded no slots",
			corrupt: func(m map[domain.OrganisationID][]loadgen.Slot) { m["org-c"] = nil },
			mention: "no slots",
		},
		{
			// The failure that would otherwise be invisible: at G1 these organisations are
			// colocated, so the pair is supported and every gate downstream passes.
			name: "a slot belonging to another organisation",
			corrupt: func(m map[domain.OrganisationID][]loadgen.Slot) {
				m["org-c"] = []loadgen.Slot{{OrganisationID: "org-a", SlotID: "a-9"}}
			},
			mention: "belongs to",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slotsByOrg := full()
			tc.corrupt(slotsByOrg)
			_, err := loadgen.NewOrgPopulations(slotsByOrg)
			if err == nil {
				t.Fatal("accepted a population the workload cannot be driven from")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not mention %q", err, tc.mention)
			}
		})
	}
}
