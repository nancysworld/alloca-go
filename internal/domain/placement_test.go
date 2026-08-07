package domain_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// twoAuthorities is the horizontal-database design's example map: four organisations across two
// authorities, with A and C deliberately colocated so the cross-organisation
// same-authority path has something to exercise.
const twoAuthorities = `{
	"version": "test-v1",
	"homes": {
		"org-a": "authority-1",
		"org-b": "authority-2",
		"org-c": "authority-1",
		"org-d": "authority-2"
	}
}`

func mustParse(t *testing.T, doc string) domain.Placement {
	t.Helper()
	p, err := domain.ParsePlacement([]byte(doc))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}
	return p
}

func TestPlacementRoutesEachOrganisationToItsAssignedAuthority(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	for org, want := range map[domain.OrganisationID]domain.AuthorityID{
		"org-a": "authority-1",
		"org-b": "authority-2",
		"org-c": "authority-1",
		"org-d": "authority-2",
	} {
		got, ok := p.AuthorityFor(org)
		if !ok {
			t.Errorf("organisation %q is unplaced", org)
			continue
		}
		if got != want {
			t.Errorf("organisation %q routed to %q, want %q", org, got, want)
		}
	}

	if v := p.Version(); v != "test-v1" {
		t.Errorf("version = %q, want test-v1", v)
	}
}

// An unplaced organisation must not resolve to anything. Defaulting it would write its
// rows to whichever authority the code happened to pick, which is how one
// organisation's source of truth ends up split across two writers.
func TestUnplacedOrganisationResolvesToNoAuthority(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	if got, ok := p.AuthorityFor("org-unknown"); ok {
		t.Fatalf("unplaced organisation resolved to %q, want no authority", got)
	}
}

// The Phase 1 support boundary is placement equality, not organisation-identifier
// equality. This is the discriminating case: two *different* organisations that share
// an authority are supported, and the same test would fail against an implementation
// that compared identifiers.
func TestColocationIsDecidedByAuthorityNotByOrganisationIdentity(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	tests := []struct {
		name          string
		slotOrg       domain.OrganisationID
		userOrg       domain.OrganisationID
		wantColocated bool
		wantPlaced    bool
	}{
		{"same organisation", "org-a", "org-a", true, true},
		{"different organisations, one authority", "org-c", "org-a", true, true},
		{"different organisations, different authorities", "org-b", "org-a", false, true},
		{"slot organisation unplaced", "org-unknown", "org-a", false, false},
		{"user organisation unplaced", "org-a", "org-unknown", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			colocated, placed := p.Colocated(tc.slotOrg, tc.userOrg)
			if colocated != tc.wantColocated || placed != tc.wantPlaced {
				t.Errorf("Colocated(%q, %q) = (%t, %t), want (%t, %t)",
					tc.slotOrg, tc.userOrg, colocated, placed, tc.wantColocated, tc.wantPlaced)
			}
		})
	}
}

func TestServesAnswersWhatOneUnitOwns(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	if !p.Serves("authority-1", "org-c") {
		t.Error("authority-1 should serve org-c")
	}
	if p.Serves("authority-1", "org-b") {
		t.Error("authority-1 must not serve org-b, which is homed on authority-2")
	}
	if p.Serves("authority-1", "org-unknown") {
		t.Error("authority-1 must not serve an unplaced organisation")
	}
}

func TestOrganisationsAndAuthoritiesAreSortedAndComplete(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	orgs := p.Organisations("authority-1")
	if len(orgs) != 2 || orgs[0] != "org-a" || orgs[1] != "org-c" {
		t.Errorf("Organisations(authority-1) = %v, want [org-a org-c]", orgs)
	}

	authorities := p.Authorities()
	if len(authorities) != 2 || authorities[0] != "authority-1" || authorities[1] != "authority-2" {
		t.Errorf("Authorities() = %v, want [authority-1 authority-2]", authorities)
	}
}

// Every rejection here is a routing document that cannot describe one unambiguous
// placement. Accepting any of them would let a run start against a map it could not
// record, or route by a default nobody chose.
func TestParsePlacementRejectsDocumentsThatCannotDescribeOneRouting(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "no version",
			doc:  `{"homes": {"org-a": "authority-1"}}`,
			want: "no version",
		},
		{
			name: "blank version",
			doc:  `{"version": "   ", "homes": {"org-a": "authority-1"}}`,
			want: "no version",
		},
		{
			name: "no organisations",
			doc:  `{"version": "v1", "homes": {}}`,
			want: "assigns no organisations",
		},
		{
			name: "organisation assigned to nothing",
			doc:  `{"version": "v1", "homes": {"org-a": ""}}`,
			want: "empty authority",
		},
		{
			name: "empty organisation identifier",
			doc:  `{"version": "v1", "homes": {"": "authority-1"}}`,
			want: "empty organisation identifier",
		},
		{
			// Go's decoder lets the last duplicate win, so a map-typed decode cannot see
			// this: an operator reads one routing out of the file and the service uses
			// another. The horizontal-database design requires setup to fail here (§5.1).
			name: "organisation assigned twice",
			doc:  `{"version": "v1", "homes": {"org-a": "authority-1", "org-a": "authority-2"}}`,
			want: "more than once",
		},
		{
			name: "organisation assigned twice with the same authority",
			doc:  `{"version": "v1", "homes": {"org-a": "authority-1", "org-a": "authority-1"}}`,
			want: "more than once",
		},
		{
			// The outer object has the same last-wins hazard its homes object had: two
			// homes objects would silently replace one routing with another before
			// anything downstream could see there had been two.
			name: "homes given twice",
			doc: `{"version": "v1",
				"homes": {"org-a": "authority-1"},
				"homes": {"org-a": "authority-2"}}`,
			want: `repeats the "homes" field`,
		},
		{
			name: "version given twice",
			doc:  `{"version": "v1", "version": "v2", "homes": {"org-a": "authority-1"}}`,
			want: `repeats the "version" field`,
		},
		{
			name: "trailing document",
			doc:  `{"version": "v1", "homes": {"org-a": "authority-1"}} {"version": "v2", "homes": {"org-a": "authority-2"}}`,
			want: "trailing content",
		},
		{
			name: "trailing garbage",
			doc:  `{"version": "v1", "homes": {"org-a": "authority-1"}} nonsense`,
			want: "trailing content",
		},
		{
			name: "homes is not an object",
			doc:  `{"version": "v1", "homes": ["org-a"]}`,
			want: "must be an object",
		},
		{
			name: "unknown field",
			doc:  `{"version": "v1", "home": {"org-a": "authority-1"}}`,
			want: "unknown field",
		},
		{
			name: "not an object",
			doc:  `["org-a"]`,
			want: "placement document",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.ParsePlacement([]byte(tc.doc))
			if err == nil {
				t.Fatalf("parsed %s without error; it cannot describe one routing", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A unit that starts against a map with nothing for it would pass every health check
// and refuse every request, which reads as a routing bug for however long it takes
// someone to inspect the map. The gate turns that into a startup error.
func TestValidateUnitRejectsAUnitWithNothingToServe(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	if err := p.ValidateUnit("authority-1"); err != nil {
		t.Fatalf("authority-1 serves org-a and org-c, but the gate rejected it: %v", err)
	}

	err := p.ValidateUnit("authority-3")
	if err == nil {
		t.Fatal("a unit whose authority is absent from the map started successfully")
	}
	if !strings.Contains(err.Error(), "authority-3") || !strings.Contains(err.Error(), "test-v1") {
		t.Errorf("error %q should name the unserved authority and the routing version", err)
	}

	if err := p.ValidateUnit(""); err == nil {
		t.Error("a unit with no authority identifier started successfully")
	}
}

// The zero value must not read as "no sharding configured, serve everything". A service
// that silently served every organisation from one authority because its map failed to
// load is the failure this type exists to prevent.
func TestZeroPlacementRoutesNothingAndFailsTheStartupGate(t *testing.T) {
	var p domain.Placement

	if !p.IsZero() {
		t.Error("the zero Placement should report IsZero")
	}
	if _, ok := p.AuthorityFor("org-a"); ok {
		t.Error("the zero Placement resolved an organisation")
	}
	if _, placed := p.Colocated("org-a", "org-a"); placed {
		t.Error("the zero Placement reported an organisation as placed")
	}
	if err := p.ValidateUnit("authority-1"); err == nil {
		t.Error("a unit started against the zero Placement")
	}
}

// An unsharded deployment is the state today's service is in, and the Phase 1 policy
// must be inert there: every organisation resolves, every pair is colocated, and
// cross-organisation booking keeps working exactly as INV-13 requires. This is the test
// that says PR3a does not change the single-authority deployment's behaviour.
func TestUnshardedPlacementColocatesEveryOrganisation(t *testing.T) {
	p := domain.Unsharded("authority-only")

	if !p.IsUnsharded() || p.IsZero() {
		t.Fatal("an unsharded placement routes everything and is not zero")
	}
	if v := p.Version(); v != domain.UnshardedVersion {
		t.Errorf("version = %q, want %q", v, domain.UnshardedVersion)
	}

	// Including organisations no map ever named: an unsharded deployment has no concept
	// of an unplaced organisation.
	for _, pair := range [][2]domain.OrganisationID{
		{"org-a", "org-a"},
		{"org-a", "org-b"},
		{"never-seen-before", "nor-this-one"},
	} {
		colocated, placed := p.Colocated(pair[0], pair[1])
		if !colocated || !placed {
			t.Errorf("Colocated(%q, %q) = (%t, %t), want (true, true)", pair[0], pair[1], colocated, placed)
		}
	}

	if !p.Serves("authority-only", "any-org") {
		t.Error("the sole authority must serve every organisation")
	}
	if p.Serves("authority-9", "any-org") {
		t.Error("an authority that is not the sole one must serve nothing")
	}
	if err := p.ValidateUnit("authority-only"); err != nil {
		t.Errorf("the sole authority failed its own startup gate: %v", err)
	}
	if err := p.ValidateUnit("authority-9"); err == nil {
		t.Error("a unit whose authority is not the unsharded one started successfully")
	}
}

// Immutability for the duration of a run (horizontal-database design §3, invariant 4) is carried by
// the type, not by this test: homes is unexported and Placement is handed out by value,
// so there is no API through which a caller could re-home an organisation. What a test
// *can* discriminate is the one way that could leak — a returned slice aliasing the
// map's own storage — so that is what this pins, as a guard against a future
// Organisations that caches.
func TestReturnedOrganisationsDoNotAliasTheMap(t *testing.T) {
	p := mustParse(t, twoAuthorities)

	orgs := p.Organisations("authority-1")
	orgs[0] = "org-tampered"

	if got, _ := p.AuthorityFor("org-a"); got != "authority-1" {
		t.Errorf("org-a re-homed to %q through a returned slice", got)
	}
	if again := p.Organisations("authority-1"); again[0] != "org-a" {
		t.Errorf("Organisations returned tampered data: %v", again)
	}
}
