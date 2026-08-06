package loadgen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// recordingUnits stands in for the topology: one httptest server per authority, each
// recording the (user organisation, slot organisation) pairs it was asked to serve. That is
// what makes routing observable — a workload that sent everything to one unit would look
// identical from the client side otherwise.
type recordingUnits struct {
	mu   sync.Mutex
	seen map[domain.AuthorityID][][2]string
}

func newRecordingUnits(t *testing.T, authorities ...domain.AuthorityID) (*recordingUnits, map[domain.AuthorityID]string) {
	t.Helper()
	units := &recordingUnits{seen: map[domain.AuthorityID][][2]string{}}
	endpoints := map[domain.AuthorityID]string{}

	for _, authority := range authorities {
		authority := authority
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				UserOrg string `json:"user_organisation_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			// /v1/slots/{slot_org}/{slot_id}/reservations
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			slotOrg := ""
			if len(parts) >= 3 {
				slotOrg = parts[2]
			}

			units.mu.Lock()
			units.seen[authority] = append(units.seen[authority], [2]string{body.UserOrg, slotOrg})
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

func (u *recordingUnits) pairs(authority domain.AuthorityID) [][2]string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([][2]string(nil), u.seen[authority]...)
}

func multiOrgFixture(t *testing.T) (*loadgen.Client, *recordingUnits, []loadgen.OrgGroup) {
	t.Helper()
	placement := twoAuthorityPlacement(t)
	units, endpoints := newRecordingUnits(t, "authority-1", "authority-2")
	router, err := loadgen.NewRouter(placement, endpoints)
	if err != nil {
		t.Fatalf("building router: %v", err)
	}
	groups, err := loadgen.NewOrgGroups(placement, map[domain.OrganisationID][]loadgen.Slot{
		"org-a": {{OrganisationID: "org-a", SlotID: "a-1"}},
		"org-b": {{OrganisationID: "org-b", SlotID: "b-1"}},
		"org-c": {{OrganisationID: "org-c", SlotID: "c-1"}},
	})
	if err != nil {
		t.Fatalf("building groups: %v", err)
	}
	return loadgen.NewRoutedClient(router, 5*time.Second, true), units, groups
}

// Every booking the dispersed workload generates must satisfy the Phase 1 support
// boundary, and it must reach the unit that owns the *user*. This is the test that says the
// supported workload cannot accidentally generate traffic Phase 1 refuses.
func TestMultiOrgDispersedGeneratesOnlySupportedColocatedPairs(t *testing.T) {
	client, units, groups := multiOrgFixture(t)
	placement := twoAuthorityPlacement(t)
	workload := loadgen.MultiOrgDispersed{Groups: groups}

	for seq := range 24 {
		for _, resp := range workload.Do(context.Background(), client, seq) {
			if resp.Invalid != "" {
				t.Fatalf("seq %d: %s", seq, resp.Invalid)
			}
		}
	}

	var sameOrg, crossOrg int
	for _, authority := range []domain.AuthorityID{"authority-1", "authority-2"} {
		for _, pair := range units.pairs(authority) {
			userOrg, slotOrg := domain.OrganisationID(pair[0]), domain.OrganisationID(pair[1])

			// Reached the unit that owns the user.
			if home, _ := placement.AuthorityFor(userOrg); home != authority {
				t.Errorf("user organisation %q was served by %q, but is homed on %q", userOrg, authority, home)
			}
			// And the pair is supported.
			colocated, placed := placement.Colocated(slotOrg, userOrg)
			if !placed || !colocated {
				t.Errorf("generated an unsupported pair: user %q, slot %q", userOrg, slotOrg)
			}
			if userOrg == slotOrg {
				sameOrg++
			} else {
				crossOrg++
			}
		}
	}

	// Both supported shapes must appear. A workload that only ever paired an organisation
	// with itself would leave the colocated cross-organisation path — the case INV-13 exists
	// for — unexercised while appearing to cover the dispersed control.
	if sameOrg == 0 {
		t.Error("no same-organisation bookings were generated")
	}
	if crossOrg == 0 {
		t.Error("no colocated cross-organisation bookings were generated; INV-13's path is unexercised")
	}
}

// Every placed organisation must appear as a *user home*, not merely as a slot owner.
//
// This uses the shipped fixture's shape — two authorities, two organisations each, exactly
// `deploy/topology/placement.json` — because that is where the defect lived. The group and
// the user organisation were both selected with `seq % 2`, so the two indices moved in
// lockstep: authority 1 only ever drew its first organisation as the user and authority 2
// only ever its second. The other two appeared as slot owners throughout, which looks like
// coverage and is not.
//
// It matters because user-home is the routing that decides which authority owns the
// schedule claim and the idempotency scope. An organisation that is never a user home never
// has that path exercised, so a run reports having driven the dispersed control while half
// the placement's mutation routing was never touched.
func TestMultiOrgDispersedExercisesEveryOrganisationAsAUserHome(t *testing.T) {
	placement, err := domain.ParsePlacement([]byte(
		`{"version":"pr3b-v1","homes":{"org-a":"authority-1","org-b":"authority-2",
		  "org-c":"authority-1","org-d":"authority-2"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}

	units, endpoints := newRecordingUnits(t, "authority-1", "authority-2")
	router, err := loadgen.NewRouter(placement, endpoints)
	if err != nil {
		t.Fatalf("building router: %v", err)
	}
	groups, err := loadgen.NewOrgGroups(placement, map[domain.OrganisationID][]loadgen.Slot{
		"org-a": {{OrganisationID: "org-a", SlotID: "a-1"}, {OrganisationID: "org-a", SlotID: "a-2"}},
		"org-b": {{OrganisationID: "org-b", SlotID: "b-1"}, {OrganisationID: "org-b", SlotID: "b-2"}},
		"org-c": {{OrganisationID: "org-c", SlotID: "c-1"}, {OrganisationID: "org-c", SlotID: "c-2"}},
		"org-d": {{OrganisationID: "org-d", SlotID: "d-1"}, {OrganisationID: "org-d", SlotID: "d-2"}},
	})
	if err != nil {
		t.Fatalf("building groups: %v", err)
	}

	client := loadgen.NewRoutedClient(router, 5*time.Second, true)
	workload := loadgen.MultiOrgDispersed{Groups: groups}
	for seq := range 64 {
		for _, resp := range workload.Do(context.Background(), client, seq) {
			if resp.Invalid != "" {
				t.Fatalf("seq %d: %s", seq, resp.Invalid)
			}
		}
	}

	userHomes := map[string]int{}
	var sameOrg, crossOrg int
	for _, authority := range []domain.AuthorityID{"authority-1", "authority-2"} {
		for _, pair := range units.pairs(authority) {
			userHomes[pair[0]]++
			if pair[0] == pair[1] {
				sameOrg++
			} else {
				crossOrg++
			}
		}
	}

	for _, org := range []string{"org-a", "org-b", "org-c", "org-d"} {
		if userHomes[org] == 0 {
			t.Errorf("%q never appeared as a user organisation: its mutation routing — the "+
				"authority owning its schedule claim and idempotency scope — is unexercised, "+
				"while the run reports having driven the dispersed control. Saw %v",
				org, userHomes)
		}
	}

	// The properties the previous cadence did hold must survive the change.
	if sameOrg == 0 {
		t.Error("no same-organisation bookings were generated")
	}
	if crossOrg == 0 {
		t.Error("no colocated cross-organisation bookings were generated; INV-13's path is unexercised")
	}
}

// The refusal control must generate pairs Phase 1 refuses, routed to the user's own unit —
// so the request is understood and refused on policy, not bounced at the edge as a misroute.
func TestCrossAuthorityControlGeneratesOnlyUnsupportedPairsRoutedToUserHome(t *testing.T) {
	client, units, groups := multiOrgFixture(t)
	placement := twoAuthorityPlacement(t)
	workload := loadgen.CrossAuthorityControl{Groups: groups}

	for seq := range 12 {
		workload.Do(context.Background(), client, seq)
	}

	var total int
	for _, authority := range []domain.AuthorityID{"authority-1", "authority-2"} {
		for _, pair := range units.pairs(authority) {
			total++
			userOrg, slotOrg := domain.OrganisationID(pair[0]), domain.OrganisationID(pair[1])
			if home, _ := placement.AuthorityFor(userOrg); home != authority {
				t.Errorf("control routed user %q to %q, not to its home %q", userOrg, authority, home)
			}
			if colocated, _ := placement.Colocated(slotOrg, userOrg); colocated {
				t.Errorf("control generated a *supported* pair: user %q, slot %q", userOrg, slotOrg)
			}
		}
	}
	if total == 0 {
		t.Fatal("the control generated no requests")
	}
}

// With one authority there is nothing to refuse. Saying so is better than reporting a
// control that quietly exercised nothing.
func TestCrossAuthorityControlRefusesToPretendOnASingleAuthority(t *testing.T) {
	client, _, _ := multiOrgFixture(t)
	workload := loadgen.CrossAuthorityControl{Groups: []loadgen.OrgGroup{{Authority: "authority-1"}}}

	got := workload.Do(context.Background(), client, 0)
	if len(got) != 1 || got[0].Invalid == "" {
		t.Fatalf("single-authority control returned %+v, want one invalid response", got)
	}
}

func TestHotOrganisationSendsEverythingToOneAuthority(t *testing.T) {
	client, units, _ := multiOrgFixture(t)
	workload := loadgen.HotOrganisation{
		Org:   "org-b",
		Slots: []loadgen.Slot{{OrganisationID: "org-b", SlotID: "b-1"}},
	}

	for seq := range 10 {
		workload.Do(context.Background(), client, seq)
	}

	if got := len(units.pairs("authority-1")); got != 0 {
		t.Errorf("authority-1 saw %d requests; the hot organisation is on authority-2", got)
	}
	if got := len(units.pairs("authority-2")); got != 10 {
		t.Errorf("authority-2 saw %d requests, want 10", got)
	}
}

// A group with no seeded slots would divide by zero at some seq deep in a run — a harness
// crash reported as a service result. It is a setup error instead.
func TestOrgGroupsRefuseAnAuthorityWithNoSeededSlots(t *testing.T) {
	_, err := loadgen.NewOrgGroups(twoAuthorityPlacement(t), map[domain.OrganisationID][]loadgen.Slot{
		"org-a": {{OrganisationID: "org-a", SlotID: "a-1"}},
	})
	if err == nil {
		t.Fatal("an authority with no seeded slots was accepted")
	}
	if !strings.Contains(err.Error(), "authority-2") {
		t.Errorf("error %q should name the authority with nothing seeded", err)
	}
}
