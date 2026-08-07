package loadgen_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
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

// The manifest records one value per run-shaping field, projected from the first unit. That
// projection is only honest if the units agree, so each of these must be a disagreement.
//
// None of them would show up in any total. A run against one unit with telemetry off and one
// with it on produces a perfectly well-formed report whose observation cost is described by
// whichever unit happened to be read first.
func TestUnitsRunningDifferentConfigurationsAreNotOneDeployment(t *testing.T) {
	tests := []struct {
		name   string
		differ func(*loadgen.ServiceMeta)
		want   string
	}{
		{"source-modified", func(m *loadgen.ServiceMeta) { m.Modified = true }, "source-modified"},
		{"Go version", func(m *loadgen.ServiceMeta) { m.GoVersion = "go1.25.0" }, "Go version"},
		{"GOMAXPROCS", func(m *loadgen.ServiceMeta) { m.GOMAXPROCS = 2 }, "GOMAXPROCS"},
		{"telemetry mode", func(m *loadgen.ServiceMeta) { m.TelemetryMode = "off" }, "telemetry mode"},
		{"reservation TTL", func(m *loadgen.ServiceMeta) { m.ReservationTTL = "5m0s" }, "reservation TTL"},
		{"timeout budget", func(m *loadgen.ServiceMeta) {
			m.RequestBudget = map[string]string{"reserve": "9s"}
		}, "timeout budget"},
		{"PostgreSQL version", func(m *loadgen.ServiceMeta) { m.Database.Version = "15.1" }, "PostgreSQL version"},
		{"pool ceiling", func(m *loadgen.ServiceMeta) { m.Database.PoolMaxConns = 80 }, "pool ceiling"},
	}

	shaped := func(target, authority string) loadgen.UnitMeta {
		u := unit(target, "abc123", authority, "pr3b-v1", 1)
		u.Meta.GoVersion = "go1.26.5"
		u.Meta.GOMAXPROCS = 8
		u.Meta.TelemetryMode = "full"
		u.Meta.ReservationTTL = "2m0s"
		u.Meta.RequestBudget = map[string]string{"reserve": "5s"}
		u.Meta.Database.Version = "16.4"
		u.Meta.Database.PoolMaxConns = 20
		return u
	}

	// The positive control: identical shape is not a disagreement.
	agreeing := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		shaped("http://unit-1", "authority-1"), shaped("http://unit-2", "authority-2"),
	}}
	if got := agreeing.Disagreement(); got != "" {
		t.Fatalf("units running the same configuration were refused: %s", got)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			one := shaped("http://unit-1", "authority-1")
			two := shaped("http://unit-2", "authority-2")
			tc.differ(&two.Meta)

			got := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{one, two}}.Disagreement()
			if got == "" {
				t.Fatalf("units differing in %s certified: the manifest would record one value "+
					"for a run that had two", tc.name)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reason %q does not name the field %q", got, tc.want)
			}
		})
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

// serving builds a unit that reports the organisations it serves, so the observed assignment
// can be compared with the map the generator routed by.
func serving(target, authority, routing string, orgs ...string) loadgen.UnitMeta {
	u := unit(target, "abc123", authority, routing, 1)
	u.Meta.Placement.Organisations = orgs
	u.Meta.Placement.Sharded = true
	return u
}

func placement(t *testing.T, document string) domain.Placement {
	t.Helper()
	p, err := domain.ParsePlacement([]byte(document))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}
	return p
}

const routedV1 = `{"version":"pr3b-v1","homes":{
  "org-a":"authority-1","org-b":"authority-2",
  "org-c":"authority-1","org-d":"authority-2"}}`

// Equal routing-version labels do not establish equal routing content.
//
// Every topology below passes Disagreement — same revision, same schema version, same version
// *string*, distinct authorities — while the generator is routing by a different map from the
// one the units are enforcing. A run in that state produces refusals that look like a service
// defect, or admissions the placement never sanctioned, and the version label says everything
// is fine.
func TestCertificationComparesPlacementContentNotItsLabel(t *testing.T) {
	routed := placement(t, routedV1)

	tests := []struct {
		name  string
		units []loadgen.UnitMeta
		want  string
	}{
		{
			name: "an authority serves organisations the generator does not route to it",
			units: []loadgen.UnitMeta{
				serving("http://unit-1", "authority-1", "pr3b-v1", "org-a"),
				serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-c", "org-d"),
			},
			want: "different placements",
		},
		{
			name: "an organisation is served by nobody",
			units: []loadgen.UnitMeta{
				serving("http://unit-1", "authority-1", "pr3b-v1", "org-a"),
				serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d"),
			},
			want: "different placements",
		},
		{
			name: "an authority the generator never routes to is serving",
			units: []loadgen.UnitMeta{
				serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c"),
				serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d"),
				serving("http://unit-3", "authority-3", "pr3b-v1", "org-e"),
			},
			want: "routes nothing to it",
		},
		{
			name: "an authority the generator routes to is absent",
			units: []loadgen.UnitMeta{
				serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c"),
				serving("http://unit-3", "authority-3", "pr3b-v1", "org-b", "org-d"),
			},
			want: "no unit in this run reported serving it",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			topology := loadgen.TopologyMeta{Units: tc.units}
			if got := topology.Disagreement(); got != "" {
				t.Fatalf("precondition: these units must agree on labels, but Disagreement said %q", got)
			}
			got := topology.DisagreementWith(routed)
			if got == "" {
				t.Fatal("the generator and the services were routing by different maps, and the " +
					"run certified")
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reason %q does not mention %q", got, tc.want)
			}
		})
	}
}

// Equal content under different version labels is still a disagreement.
//
// The contract is the same *versioned* map, so content equality and version equality are
// separate gates. The manifest records the version the **units** report, so certifying this
// would produce an artifact naming `pr3b-v1` for a run the generator drove from a document
// labelled `pr3b-v2` — and two documents agreeing on content today is not evidence they are
// the same document tomorrow.
func TestGeneratorAndUnitsMustAgreeOnTheRoutingVersionNotOnlyItsContent(t *testing.T) {
	units := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c"),
		serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d"),
	}}
	// Byte-for-byte the same assignment as routedV1, under a different version label.
	relabelled := placement(t, `{"version":"pr3b-v2","homes":{
	  "org-a":"authority-1","org-b":"authority-2",
	  "org-c":"authority-1","org-d":"authority-2"}}`)

	got := units.DisagreementWith(relabelled)
	if got == "" {
		t.Fatal("a run whose generator and services carried different placement versions " +
			"certified, because their content happened to match")
	}
	if !strings.Contains(got, "pr3b-v1") || !strings.Contains(got, "pr3b-v2") {
		t.Errorf("reason %q should name both versions", got)
	}

	// The positive control: the matching version must still certify, or the check above
	// would pass for the wrong reason.
	if got := units.DisagreementWith(placement(t, routedV1)); got != "" {
		t.Fatalf("a run whose generator and services agreed on version and content was "+
			"refused: %s", got)
	}
}

// A unit that believes it is unsharded will not refuse the organisations it does not own, so
// its presence in a multi-unit run is a split-brain that no assignment comparison catches:
// its own list may still be correct.
func TestAUnitReportingItselfUnshardedInAMultiUnitRunIsRefused(t *testing.T) {
	one := serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c")
	two := serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d")
	two.Meta.Placement.Sharded = false

	got := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{one, two}}.DisagreementWith(placement(t, routedV1))
	if got == "" {
		t.Fatal("a unit serving every organisation was accepted as one shard of a topology")
	}
	if !strings.Contains(got, "unsharded") {
		t.Errorf("reason %q does not name the unsharded unit", got)
	}
}

// The correct topology must pass, or every test above would pass for the wrong reason.
func TestATopologyMatchingTheGeneratorsMapCertifies(t *testing.T) {
	topology := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c"),
		serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d"),
	}}
	if got := topology.DisagreementWith(placement(t, routedV1)); got != "" {
		t.Fatalf("a topology serving exactly the generator's map was refused: %s", got)
	}
	// A single-target run has no map to compare against and must not be refused for it.
	if got := topology.DisagreementWith(domain.Placement{}); got != "" {
		t.Errorf("a run with no placement to compare was refused: %s", got)
	}
}

// The digest exists because the version label is an operator's assertion: a map edited
// without a version bump produces two runs that look comparable and are not.
func TestPlacementDigestTracksContentRatherThanTheVersionLabel(t *testing.T) {
	original := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		serving("http://unit-1", "authority-1", "pr3b-v1", "org-a", "org-c"),
		serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-d"),
	}}
	// The same assignment, read in the other order and with each unit's list unsorted.
	reordered := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		serving("http://unit-2", "authority-2", "pr3b-v1", "org-d", "org-b"),
		serving("http://unit-1", "authority-1", "pr3b-v1", "org-c", "org-a"),
	}}
	// One organisation moved, version label untouched — the case the label cannot report.
	moved := loadgen.TopologyMeta{Units: []loadgen.UnitMeta{
		serving("http://unit-1", "authority-1", "pr3b-v1", "org-a"),
		serving("http://unit-2", "authority-2", "pr3b-v1", "org-b", "org-c", "org-d"),
	}}

	if original.PlacementDigest() != reordered.PlacementDigest() {
		t.Error("the digest changed when only the read order did: it must fingerprint the " +
			"assignment, not the order units happened to answer in")
	}
	if original.PlacementDigest() == moved.PlacementDigest() {
		t.Error("moving an organisation between authorities left the digest unchanged, so two " +
			"runs on different placements would compare as identical")
	}
	if original.PlacementDigest() == "" {
		t.Error("digest is empty")
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
