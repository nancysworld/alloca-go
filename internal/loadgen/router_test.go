package loadgen_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

func twoAuthorityPlacement(t *testing.T) domain.Placement {
	t.Helper()
	p, err := domain.ParsePlacement([]byte(
		`{"version":"pr3b-v1","homes":{"org-a":"authority-1","org-b":"authority-2","org-c":"authority-1"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}
	return p
}

func TestRouterSendsEachOrganisationToItsOwnAuthority(t *testing.T) {
	r, err := loadgen.NewRouter(twoAuthorityPlacement(t), map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
		"authority-2": "http://unit-2:8080",
	})
	if err != nil {
		t.Fatalf("building router: %v", err)
	}

	for org, want := range map[domain.OrganisationID]string{
		"org-a": "http://unit-1:8080",
		"org-c": "http://unit-1:8080",
		"org-b": "http://unit-2:8080",
	} {
		got, err := r.For(org)
		if err != nil {
			t.Errorf("routing %q: %v", org, err)
			continue
		}
		if got != want {
			t.Errorf("%q routed to %q, want %q", org, got, want)
		}
	}

	if v := r.RoutingVersion(); v != "pr3b-v1" {
		t.Errorf("routing version = %q, want pr3b-v1", v)
	}
	if got := r.Targets(); len(got) != 2 {
		t.Errorf("targets = %v, want both units", got)
	}
}

// An unplaced organisation must not be sent anywhere. Guessing produces a run whose
// requests are refused as misroutes and whose totals read as a service defect.
func TestRouterRefusesAnUnplacedOrganisation(t *testing.T) {
	r, err := loadgen.NewRouter(twoAuthorityPlacement(t), map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
		"authority-2": "http://unit-2:8080",
	})
	if err != nil {
		t.Fatalf("building router: %v", err)
	}
	if _, err := r.For("org-unknown"); err == nil {
		t.Fatal("an unplaced organisation was routed somewhere")
	}
}

// Both directions of an incomplete topology are setup errors, not runtime surprises.
func TestRouterRefusesATopologyItCouldNotRoute(t *testing.T) {
	placement := twoAuthorityPlacement(t)

	if _, err := loadgen.NewRouter(placement, map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
	}); err == nil || !strings.Contains(err.Error(), "authority-2") {
		t.Errorf("a missing endpoint was accepted or misreported: %v", err)
	}

	if _, err := loadgen.NewRouter(placement, map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
		"authority-2": "http://unit-2:8080",
		"authority-9": "http://unit-9:8080",
	}); err == nil || !strings.Contains(err.Error(), "authority-9") {
		t.Errorf("an endpoint nothing routes to was accepted: %v", err)
	}

	if _, err := loadgen.NewRouter(domain.Placement{}, map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
	}); err == nil {
		t.Error("a zero placement built a router")
	}
}

// The unsharded case: one target, everything colocated, and a routing version that still
// says what it was, so a single-authority run's artifacts are comparable with a sharded
// run's rather than silently missing the field.
func TestSingleTargetRoutesEverythingAndColocatesEverything(t *testing.T) {
	r := loadgen.SingleTarget("http://localhost:8080")

	got, err := r.For("any-organisation")
	if err != nil || got != "http://localhost:8080" {
		t.Fatalf("For = (%q, %v), want the single target", got, err)
	}
	if !r.Colocated("org-a", "org-b") {
		t.Error("a single-target run must treat every pair as colocated")
	}
	if v := r.RoutingVersion(); v != domain.UnshardedVersion {
		t.Errorf("routing version = %q, want %q", v, domain.UnshardedVersion)
	}
}

func TestRouterColocationMatchesThePlacement(t *testing.T) {
	r, err := loadgen.NewRouter(twoAuthorityPlacement(t), map[domain.AuthorityID]string{
		"authority-1": "http://unit-1:8080",
		"authority-2": "http://unit-2:8080",
	})
	if err != nil {
		t.Fatalf("building router: %v", err)
	}
	if !r.Colocated("org-a", "org-c") {
		t.Error("org-a and org-c share authority-1 and must be colocated")
	}
	if r.Colocated("org-a", "org-b") {
		t.Error("org-a and org-b are on different authorities")
	}
	if r.Colocated("org-a", "org-unknown") {
		t.Error("an unplaced organisation shares an authority with nothing")
	}
}
