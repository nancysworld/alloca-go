package loadgen_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// unit builds a unit's self-description with the four fields certification compares.
func unit(target, revision, authority, routing string, schema int64) loadgen.UnitMeta {
	var m loadgen.ServiceMeta
	m.Revision = revision
	m.Database.SchemaVersion = schema
	m.Placement.AuthorityID = authority
	m.Placement.RoutingVersion = routing
	return loadgen.UnitMeta{Target: target, Meta: m}
}

func TestAgreeingUnitsDescribeOneDeployment(t *testing.T) {
	topology := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
		unit("http://unit-2", "abc123", "authority-2", "pr3b-v1", 1),
	}}
	if got := topology.Disagreement(); got != "" {
		t.Fatalf("units that agree were reported as disagreeing: %s", got)
	}
	if got := topology.Authorities(); len(got) != 2 {
		t.Errorf("authorities = %v, want both", got)
	}
	if got := topology.RoutingVersion(); got != "pr3b-v1" {
		t.Errorf("routing version = %q", got)
	}
}

// Each of these produces a well-formed report whose numbers describe something other than
// one deployment, and nothing in the request totals would reveal any of them.
func TestUnitsThatDoNotDescribeOneDeploymentAreNamed(t *testing.T) {
	tests := []struct {
		name  string
		units []loadgen.UnitMeta
		want  string
	}{
		{
			name: "different code",
			units: []loadgen.UnitMeta{
				unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
				unit("http://unit-2", "def456", "authority-2", "pr3b-v1", 1),
			},
			want: "different code",
		},
		{
			name: "different routing — the split-brain case",
			units: []loadgen.UnitMeta{
				unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
				unit("http://unit-2", "abc123", "authority-2", "pr3b-v2", 1),
			},
			want: "disagree about routing",
		},
		{
			name: "different schema versions",
			units: []loadgen.UnitMeta{
				unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
				unit("http://unit-2", "abc123", "authority-2", "pr3b-v1", 2),
			},
			want: "different schema versions",
		},
		{
			name: "the same unit reached twice",
			units: []loadgen.UnitMeta{
				unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
				unit("http://unit-2", "abc123", "authority-1", "pr3b-v1", 1),
			},
			want: "both report authority",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := loadgen.TopologyMeta{Units: tc.units}.Disagreement()
			if got == "" {
				t.Fatal("a topology that is not one deployment was accepted")
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reason %q does not mention %q", got, tc.want)
			}
			// The operator has to know which unit to look at.
			if !strings.Contains(got, "unit-1") || !strings.Contains(got, "unit-2") {
				t.Errorf("reason %q should name both targets", got)
			}
		})
	}
}

// Two *later* units sharing an authority is the case a first-unit-only comparison misses.
//
// The topology below is a real misconfiguration, not a contrived one: unit-3's endpoint
// points at unit-2's service, so authority-3 is never reached at all. Every unit agrees on
// revision, routing version and schema version, and none of them collides with unit-1 — so a
// check that compares each unit against the first returns no disagreement, and the run
// certifies as a healthy three-authority deployment while holding two.
func TestDuplicateAuthorityAmongLaterUnitsIsCaught(t *testing.T) {
	topology := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1),
		unit("http://unit-2", "abc123", "authority-2", "pr3b-v1", 1),
		unit("http://unit-3", "abc123", "authority-2", "pr3b-v1", 1),
	}}

	got := topology.Disagreement()
	if got == "" {
		t.Fatal("a topology that reached one authority twice and missed another was accepted " +
			"as three authorities")
	}
	if !strings.Contains(got, "authority-2") {
		t.Errorf("reason %q does not name the repeated authority", got)
	}
	// Both ends of the clash, so the operator knows which endpoint to correct. Naming only
	// the newcomer leaves them checking a unit that is configured correctly.
	if !strings.Contains(got, "unit-2") || !strings.Contains(got, "unit-3") {
		t.Errorf("reason %q should name both units claiming the authority", got)
	}
}

// Authority and organisations are *supposed* to differ between units — that is what makes
// them separate authorities. Comparing them would refuse every correct topology.
func TestLegitimateDifferencesBetweenUnitsAreNotDisagreement(t *testing.T) {
	a := unit("http://unit-1", "abc123", "authority-1", "pr3b-v1", 1)
	a.Meta.Placement.Organisations = []string{"org-a", "org-c"}
	b := unit("http://unit-2", "abc123", "authority-2", "pr3b-v1", 1)
	b.Meta.Placement.Organisations = []string{"org-b", "org-d"}

	topology := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{a, b}}
	if got := topology.Disagreement(); got != "" {
		t.Fatalf("a correct two-authority topology was refused: %s", got)
	}

	assignment := topology.Assignment()
	if len(assignment["authority-1"]) != 2 || assignment["authority-2"][0] != "org-b" {
		t.Errorf("assignment = %v, want each authority's own organisations", assignment)
	}
}

// A single-unit run has nothing to disagree with, and must not be refused for it.
func TestOneUnitCannotDisagreeWithItself(t *testing.T) {
	topology := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		unit("http://unit-1", "abc123", "authority-1", "unsharded", 1),
	}}
	if got := topology.Disagreement(); got != "" {
		t.Errorf("a single-unit topology was reported as disagreeing: %s", got)
	}
}

// A disagreeing topology is unsound, not merely under-documented: its totals are two
// services averaged together, so it drops to none rather than stopping partway up.
func TestCertificationRefusesADisagreeingTopology(t *testing.T) {
	m := loadgen.Manifest{TopologyDisagreement: "units are running different code: ..."}
	got := loadgen.Certify(m, loadgen.Summary{Sound: true})

	if got.Level != loadgen.LevelNone {
		t.Errorf("level = %q, want %q", got.Level, loadgen.LevelNone)
	}
	if !strings.Contains(got.BlockedBecause, "one deployment") {
		t.Errorf("blocked_because = %q; it should say the topology was not one deployment", got.BlockedBecause)
	}
}
