package loadgen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// write puts a declaration on disk and returns its path. Written as a file rather than
// decoded from a string because the loader's contract starts at the file: an operator hands it
// a path, and "the file is missing" is one of the outcomes it has to explain.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "declaration.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing declaration: %v", err)
	}
	return path
}

const wellFormed = `{
  "environment": "AWS EC2 eu-west-2, 4x c5.large capacity units, c5.xlarge generator",
  "deployment_topology": "4 shard groups, one service and one PostgreSQL authority per unit"
}`

func TestLoadDeclarationAcceptsTheDocumentedShape(t *testing.T) {
	declared, err := loadgen.LoadDeclaration(write(t, wellFormed))
	if err != nil {
		t.Fatalf("LoadDeclaration: %v", err)
	}
	if declared.Environment == "" || declared.DeploymentTopology == "" {
		t.Fatalf("fields did not survive the round trip: %+v", declared)
	}
	if declared.ReplicaFanOut != nil {
		t.Errorf("a document that declares no fan-out produced one: %+v", declared.ReplicaFanOut)
	}
}

// TestLoadDeclarationRefusesAnIncompleteDocument covers the fields `capacity` is gated on. Each
// case is a document that parses cleanly and describes a deployment nobody could interpret.
func TestLoadDeclarationRefusesAnIncompleteDocument(t *testing.T) {
	cases := []struct {
		name, body, wants string
	}{
		{"no environment",
			`{"deployment_topology": "4 shard groups"}`, "environment"},
		{"no topology",
			`{"environment": "AWS EC2 eu-west-2"}`, "deployment_topology"},
		{"fan-out without a count", `{
			"environment": "e", "deployment_topology": "t",
			"replica_fan_out": {"aggregate_pool_size": 80, "because": "an ALB"}}`,
			"replica_count"},
		{"fan-out without an aggregate pool", `{
			"environment": "e", "deployment_topology": "t",
			"replica_fan_out": {"replica_count": 8, "because": "an ALB"}}`,
			"aggregate_pool_size"},
		{"fan-out without a reason", `{
			"environment": "e", "deployment_topology": "t",
			"replica_fan_out": {"replica_count": 8, "aggregate_pool_size": 80}}`,
			"without saying why"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadgen.LoadDeclaration(write(t, c.body))
			if err == nil {
				t.Fatal("the document was accepted, so the run would have been driven and the " +
					"omission found at certification")
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Errorf("the refusal does not name what is missing:\n  got:  %v\n  want it "+
					"to mention %q", err, c.wants)
			}
		})
	}
}

// TestLoadDeclarationRefusesAnUnknownField guards the failure a hand-written document actually
// has. A misspelled key is silently absent under encoding/json's default, so the operator
// believes they supplied a field, the run is driven, and certification reports it missing.
func TestLoadDeclarationRefusesAnUnknownField(t *testing.T) {
	_, err := loadgen.LoadDeclaration(write(t, `{
		"environment": "e", "deployment_topology": "t", "enviroment": "typo"}`))
	if err == nil {
		t.Fatal("a misspelled key was ignored rather than refused")
	}
	if !strings.Contains(err.Error(), "enviroment") {
		t.Errorf("the refusal does not name the key that was not understood: %v", err)
	}
}

// TestReplicaCountIsObservedUnlessTheDeploymentHides is the point of the type: the two fields
// most likely to be typed from memory are read back from the units instead.
func TestReplicaCountIsObservedUnlessTheDeploymentHides(t *testing.T) {
	plain, err := loadgen.LoadDeclaration(write(t, wellFormed))
	if err != nil {
		t.Fatalf("LoadDeclaration: %v", err)
	}
	if got := plain.ReplicaCount(4); got != 4 {
		t.Errorf("replica count for a 4-unit run = %d, want it derived as 4", got)
	}

	fanned, err := loadgen.LoadDeclaration(write(t, `{
		"environment": "e", "deployment_topology": "t",
		"replica_fan_out": {"replica_count": 8, "aggregate_pool_size": 80,
			"because": "two replicas behind each unit's load balancer"}}`))
	if err != nil {
		t.Fatalf("LoadDeclaration: %v", err)
	}
	if got := fanned.ReplicaCount(4); got != 8 {
		t.Errorf("replica count for a declared fan-out = %d, want the declared 8", got)
	}
}

// TestAggregatePoolSizeSumsWhatEachUnitReports pins the arithmetic to the units rather than to
// one unit's value multiplied out. The heterogeneous case cannot certify — runShapeMismatch
// refuses it — but the sum must be the honest number regardless, because a function that
// happened to be right only for uniform deployments would be wrong the day one is not.
func TestAggregatePoolSizeSumsWhatEachUnitReports(t *testing.T) {
	var plain loadgen.Declaration
	if got := plain.AggregatePoolSize(topologyWithPools(10, 10, 10, 10)); got != 40 {
		t.Errorf("aggregate over four units of 10 = %d, want 40", got)
	}
	if got := plain.AggregatePoolSize(topologyWithPools(10, 20)); got != 30 {
		t.Errorf("aggregate over units of 10 and 20 = %d, want 30 — the sum, not a multiple "+
			"of the first unit's ceiling", got)
	}

	fanned := loadgen.Declaration{ReplicaFanOut: &loadgen.ReplicaFanOut{
		ReplicaCount: 8, AggregatePoolSize: 80, Because: "an ALB"}}
	if got := fanned.AggregatePoolSize(topologyWithPools(10, 10, 10, 10)); got != 80 {
		t.Errorf("aggregate for a fanned-out deployment = %d, want the declared 80: the "+
			"replicas the endpoints do not speak for hold connections too", got)
	}
}

func topologyWithPools(ceilings ...int) loadgen.TopologyMeta {
	var t loadgen.TopologyMeta
	for _, c := range ceilings {
		var meta loadgen.ServiceMeta
		meta.Database.PoolMaxConns = c
		t.Units = append(t.Units, loadgen.UnitMeta{Meta: meta})
	}
	return t
}

// The fan-out's pool arithmetic must be refused in preflight, not at certification.
//
// It is the only inconsistency a declaration can introduce: without a fan-out both the replica
// count and the aggregate pool are derived from the units and agree by construction, so a
// declared fan-out is the one way the three numbers can disagree. The manifest already refuses
// the same disagreement, but that runs after the ladder has been driven — and this document was
// made a file rather than four flags precisely so a mistyped number would not cost a rung.
//
// The passing cases are here too: a fan-out whose arithmetic holds, and a run with no fan-out at
// all, must not be refused by a rule that only exists for the declared case.
func TestFanOutPoolArithmeticIsRefusedBeforeTheRunNotAfterIt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fanOut   *loadgen.ReplicaFanOut
		topology loadgen.TopologyMeta
		wantErr  string
	}{
		{
			name: "declared aggregate disagrees with count times ceiling",
			fanOut: &loadgen.ReplicaFanOut{
				ReplicaCount: 8, AggregatePoolSize: 70, Because: "an ALB in front of each unit"},
			topology: topologyWithPools(10, 10, 10, 10),
			wantErr:  "typed from memory",
		},
		{
			name: "consistent fan-out is accepted",
			fanOut: &loadgen.ReplicaFanOut{
				ReplicaCount: 8, AggregatePoolSize: 80, Because: "an ALB in front of each unit"},
			topology: topologyWithPools(10, 10, 10, 10),
		},
		{
			name:     "no fan-out declared, nothing to check",
			topology: topologyWithPools(10, 10, 10, 10),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadgen.Declaration{ReplicaFanOut: tc.fanOut}.ReconcileWith(tc.topology)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ReconcileWith refused a consistent declaration: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ReconcileWith accepted a declaration the manifest would later refuse, " +
					"so the run would be driven before anyone found out")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}
