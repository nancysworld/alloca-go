package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// writeDeploymentFor records an observation of exactly the units a run addresses, so a
// multi-unit run can satisfy the artifact-identity preflight.
//
// A run across several units is the containerised topology, and measurement-contract §11 asks
// it to name the artifact it measured; the harness will not drive one that cannot. Tests whose
// subject is something else therefore need a record that matches their targets, and writing it
// here rather than inline keeps that fixture from being mistaken for part of what they assert.
func writeDeploymentFor(t *testing.T, targets ...string) string {
	t.Helper()
	units := make([]string, 0, len(targets))
	for i, target := range targets {
		units = append(units, fmt.Sprintf(
			`"alloca-service-%d": {"image_id": "sha256:1111111111111111", "target": %q}`,
			i+1, target))
	}
	document := fmt.Sprintf(`{"image_id": "sha256:1111111111111111", "units": {%s}}`,
		strings.Join(units, ","))

	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("writing deployment record: %v", err)
	}
	return path
}

// unit stands in for one shard-affine service unit: it answers /meta with its own authority
// and organisations, and admits every mutation, recording which organisation each request
// was routed to it for.
type unit struct {
	server    *httptest.Server
	authority string
	orgs      []string

	mu    sync.Mutex
	users []string
}

func newUnit(t *testing.T, authority string, orgs []string, routing string) *unit {
	t.Helper()
	u := &unit{authority: authority, orgs: orgs}

	mux := http.NewServeMux()
	mux.HandleFunc("/meta", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"go_version":      "go1.26.5",
			"gomaxprocs":      8,
			"revision":        "abc123",
			"modified":        false,
			"reservation_ttl": "30s",
			"telemetry_mode":  "full",
			"started_at":      "2026-08-06T00:00:00Z",
			"request_budget":  map[string]string{"reserve": "5s"},
			"database": map[string]any{
				"version": "16.4", "pool_max_conns": 20, "schema_version": 1,
			},
			"placement": map[string]any{
				"authority_id": authority, "routing_version": routing,
				"sharded": true, "organisations": orgs,
			},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			UserOrganisationID string `json:"user_organisation_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		u.mu.Lock()
		u.users = append(u.users, body.UserOrganisationID)
		u.mu.Unlock()

		// 200, not 201: the status/outcome mapping is keyed by outcome and says admitted
		// success is 200 for reserve and confirm alike (httpapi.StatusForOutcome).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeAdmittedSuccess, "replay": false,
			"reservation_id": "res-1",
		})
	})

	u.server = httptest.NewServer(mux)
	t.Cleanup(u.server.Close)
	return u
}

// routedUserOrgs is the organisations whose mutations reached this unit.
func (u *unit) routedUserOrgs() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.users...)
}

func writePlacement(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "placement.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("writing placement: %v", err)
	}
	return path
}

const twoAuthorities = `{
  "version": "test-v1",
  "homes": {"org-a": "authority-1", "org-b": "authority-2",
            "org-c": "authority-1", "org-d": "authority-2"}
}`

// The whole point of PR3b's generator work: a run driven from the command line reaches every
// unit, routes each organisation to the authority that owns it, and writes a report saying
// which topology it actually reached.
//
// Package APIs that no executable path constructs are not a harness. This is the test that
// says alloca-load can drive one.
func TestMultiAuthorityRunRoutesByPlacementAndRecordsTheTopology(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "multi-org-dispersed",
		"-concurrency", "4", "-n", "40", "-slots", "5",
		"-out", reportPath,
		// Every multi-unit run names its artifact, including this one: asking for no level
		// does not excuse the record, because -require does not cap what the report certifies.
		"-deployment", writeDeploymentFor(t, one.server.URL, two.server.URL),
		// A `go test` binary carries no VCS stamp, so the generator identity the local level
		// requires can never be present here. What this test is for is the wiring; the
		// quotability ladder has its own tests, against manifests built directly.
		"-require", "none",
	})
	if err != nil {
		t.Fatalf("multi-authority run failed: %v", err)
	}

	var report loadgen.Report
	raw, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("reading report: %v", readErr)
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decoding report: %v", err)
	}

	// Routing: every mutation went to the authority owning the *user's* organisation. A
	// generator that sent org-b's traffic to authority-1 would still produce a clean-looking
	// report, because both units admit everything.
	for _, unit := range []*unit{one, two} {
		owned := map[string]bool{}
		for _, org := range unit.orgs {
			owned[org] = true
		}
		routed := unit.routedUserOrgs()
		if len(routed) == 0 {
			t.Errorf("%s received no mutations: the run did not exercise it", unit.authority)
		}
		for _, org := range routed {
			if !owned[org] {
				t.Errorf("%s received a mutation for %q, which it does not own: the generator "+
					"routed by something other than the placement map", unit.authority, org)
			}
		}
	}

	// The report has to name the topology it reached, or the artifact describes a
	// single-authority run.
	m := report.Manifest
	if m.AuthorityCount != 2 {
		t.Errorf("authority_count = %d, want 2", m.AuthorityCount)
	}
	if m.RoutingVersion != "test-v1" {
		t.Errorf("routing_version = %q, want test-v1", m.RoutingVersion)
	}
	if m.TopologyDisagreement != "" {
		t.Errorf("a correct topology was reported as disagreeing: %s", m.TopologyDisagreement)
	}
	wantAssignment := map[string][]string{
		"authority-1": {"org-a", "org-c"},
		"authority-2": {"org-b", "org-d"},
	}
	if fmt.Sprint(m.PlacementAssignment) != fmt.Sprint(wantAssignment) {
		t.Errorf("placement_assignment = %v, want %v", m.PlacementAssignment, wantAssignment)
	}
	// Both endpoints, not one: a target naming a single unit reads as a single-authority run.
	for _, u := range []*unit{one, two} {
		if !strings.Contains(m.Target, u.server.URL) {
			t.Errorf("target %q does not name %s", m.Target, u.server.URL)
		}
	}
	// -slots is per organisation, and four organisations were placed.
	if m.DatasetSlots != 20 {
		t.Errorf("dataset_slots = %d, want 20 (5 slots × 4 organisations)", m.DatasetSlots)
	}
	if !report.Summary.Sound {
		t.Errorf("run reported unsound: %s", report.Summary.NotSoundBecause)
	}
}

// The cross-authority control is the only shape that can tell user-home routing from
// slot-home routing, and it is the reason the distinction is testable at all.
//
// MultiOrgDispersed draws the user's organisation and the slot from the *same* group, so
// both organisations sit on one authority and the two routing rules agree on every request —
// a generator that routed by the slot would pass the dispersed test unchanged. This shape
// pairs them across authorities deliberately, so the request must arrive at the unit owning
// the *user*: user-home owns the schedule claim and the idempotency scope, so it is where the
// mutation and every replay of it belong, and being refused there on policy is the supported
// behaviour. Routing to the slot's authority instead would bounce the request at the edge as
// a misroute and report a refusal the service never made.
func TestCrossAuthorityControlRoutesToTheUsersOwnAuthority(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "cross-authority-control",
		"-concurrency", "2", "-n", "20", "-slots", "4",
		"-out", filepath.Join(t.TempDir(), "report.json"),
		"-deployment", writeDeploymentFor(t, one.server.URL, two.server.URL),
		"-require", "none",
	})
	if err != nil {
		t.Fatalf("cross-authority control run failed: %v", err)
	}

	for _, unit := range []*unit{one, two} {
		owned := map[string]bool{}
		for _, org := range unit.orgs {
			owned[org] = true
		}
		routed := unit.routedUserOrgs()
		if len(routed) == 0 {
			t.Errorf("%s received no requests: the control did not exercise it", unit.authority)
		}
		for _, org := range routed {
			if !owned[org] {
				t.Errorf("%s received a request whose user is in %q, which it does not own: the "+
					"control routed by the slot's authority, so the refusal it records is an "+
					"edge misroute rather than the policy refusal it claims to measure",
					unit.authority, org)
			}
		}
	}
}

// A unit on a different routing version is the split-brain case, and it must reach the
// report as a refusal rather than as a well-formed set of totals.
func TestMultiAuthorityRunRefusesUnitsThatDisagree(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v2")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "multi-org-dispersed",
		"-concurrency", "2", "-n", "8", "-slots", "2",
		"-deployment", writeDeploymentFor(t, one.server.URL, two.server.URL),
		"-out", reportPath,
	})
	if err == nil {
		t.Fatal("a topology serving two routing versions certified at local")
	}

	raw, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("reading report: %v", readErr)
	}
	var report loadgen.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decoding report: %v", err)
	}
	if report.Quotability.Level != loadgen.LevelNone {
		t.Errorf("level = %q, want none: a split-brain topology is unsound, not merely "+
			"under-documented", report.Quotability.Level)
	}
	if !strings.Contains(report.Manifest.TopologyDisagreement, "routing") {
		t.Errorf("disagreement %q does not name the routing split",
			report.Manifest.TopologyDisagreement)
	}
}

// Forgetting the observation must not read as a lower-provenance run.
//
// The certification gate refuses a containerised run with no image id, but it is armed by
// `container_deployment`, which the record itself sets — so omitting the record also disarms
// the gate, and the run reports a clean `local` result whose missing artifact identity looks
// like a choice. Keying on the routed topology instead is what closes that: a run reaching
// several units is the containerised topology whatever the operator remembered to pass.
func TestAMultiUnitRunWithoutADeploymentRecordIsRefused(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "multi-org-dispersed",
		"-concurrency", "2", "-n", "8", "-slots", "2",
		"-out", filepath.Join(t.TempDir(), "report.json"),
	})
	if err == nil {
		t.Fatal("a two-unit run measured an artifact it could not name")
	}
	if !strings.Contains(err.Error(), "-deployment") {
		t.Errorf("error %q does not name the flag that fixes it", err)
	}
}

// The ordering property, and the one that fails if the check moves back after `Runner.Run`.
//
// A record checked afterwards can only annotate numbers that already exist: the operator
// learns the topology was not the one described once the measurement has been taken. Both
// refusals below must therefore land with no measured request having been sent — which is
// also why asserting on the error alone would not discriminate.
func TestTheDeploymentPreflightRefusesBeforeAnyMeasuredRequest(t *testing.T) {
	tests := []struct {
		name       string
		deployment func(t *testing.T, one, two *unit) []string
		want       string
	}{
		{
			name:       "no record at all",
			deployment: func(*testing.T, *unit, *unit) []string { return nil },
			want:       "-deployment",
		},
		{
			name: "a record that observed only one of the routed units",
			deployment: func(t *testing.T, one, _ *unit) []string {
				return []string{"-deployment", writeDeploymentFor(t, one.server.URL)}
			},
			want: "does not cover every unit",
		},
		{
			name: "a record describing units this run does not route to",
			deployment: func(t *testing.T, _, _ *unit) []string {
				return []string{"-deployment", writeDeploymentFor(t,
					"http://localhost:19001", "http://localhost:19002")}
			},
			want: "different topology",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
			two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")

			args := []string{
				"-placement", writePlacement(t, twoAuthorities),
				"-endpoint", "authority-1=" + one.server.URL,
				"-endpoint", "authority-2=" + two.server.URL,
				"-workload", "multi-org-dispersed",
				"-concurrency", "2", "-n", "8", "-slots", "2",
				"-out", filepath.Join(t.TempDir(), "report.json"),
			}
			err := run(append(args, tc.deployment(t, one, two)...))
			if err == nil {
				t.Fatal("a run whose artifact identity could not be established was driven anyway")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}

			for _, unit := range []*unit{one, two} {
				if sent := unit.routedUserOrgs(); len(sent) > 0 {
					t.Errorf("%s served %d measured requests before the run was refused: the "+
						"preflight ran after the workload, so the operator learns the topology "+
						"was wrong only once the measurement has been taken",
						unit.authority, len(sent))
				}
			}
		})
	}
}

// The positive control for both refusals above, without which they could pass because every
// multi-unit run is refused — which would make the check worthless in the other direction.
// It also pins what the manifest carries: the common identity, not the per-unit map.
func TestAMatchingDeploymentRecordCarriesTheArtifactIdentity(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "multi-org-dispersed",
		"-concurrency", "2", "-n", "8", "-slots", "2",
		"-deployment", writeDeploymentFor(t, one.server.URL, two.server.URL),
		// `go test` does not stamp VCS data, so the generator identity keeps this binary
		// below `local` for reasons that have nothing to do with the artifact record. The
		// preflight runs whenever a record is passed, whatever level is asked for.
		"-require", "none",
		"-out", reportPath,
	})
	if err != nil {
		t.Fatalf("a run whose record describes exactly its units was refused: %v", err)
	}

	raw, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("reading report: %v", readErr)
	}
	var report loadgen.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decoding report: %v", err)
	}
	if !report.Manifest.ContainerDeployment {
		t.Error("container_deployment is false on a run that carried a deployment observation")
	}
	if report.Manifest.ImageID != "sha256:1111111111111111" {
		t.Errorf("image_id = %q, want the observed identity", report.Manifest.ImageID)
	}
}

// Asking for no level must not buy an exemption, which is the failure the obvious reading of
// `-require` invites.
//
// `-require` is the floor a run must clear to exit zero, not a ceiling on what its report
// claims: `Certify` computes the level the manifest and summary actually reach whatever the
// flag says. So a VCS-stamped binary passing `-require none` would exit zero *and* write a
// report certified at `local` or above — a provenance-backed claim naming no artifact. That
// makes `-require none` the one phrasing under which an operator would reasonably expect the
// record to be optional, and the one that must still be refused.
func TestRequiringNoLevelDoesNotExcuseTheDeploymentRecord(t *testing.T) {
	one := newUnit(t, "authority-1", []string{"org-a", "org-c"}, "test-v1")
	two := newUnit(t, "authority-2", []string{"org-b", "org-d"}, "test-v1")

	err := run([]string{
		"-placement", writePlacement(t, twoAuthorities),
		"-endpoint", "authority-1=" + one.server.URL,
		"-endpoint", "authority-2=" + two.server.URL,
		"-workload", "multi-org-dispersed",
		"-concurrency", "2", "-n", "8", "-slots", "2",
		"-require", "none",
		"-out", filepath.Join(t.TempDir(), "report.json"),
	})
	if err == nil {
		t.Fatal("a two-unit run measured an artifact it could not name because it asked " +
			"for no level: -require does not cap what the report certifies")
	}
	if !strings.Contains(err.Error(), "-deployment") {
		t.Errorf("error %q does not name the flag that fixes it", err)
	}
}

// The flags that decide routing must not be silently combinable, and the multi-organisation
// shapes must not quietly degrade to a single-authority run.
func TestRoutingFlagsAreRefusedWhenTheyContradict(t *testing.T) {
	placementPath := writePlacement(t, twoAuthorities)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "endpoint without placement",
			args: []string{"-endpoint", "authority-1=http://localhost:8081"},
			want: "-endpoint needs -placement",
		},
		{
			name: "placement with an explicit target",
			args: []string{"-placement", placementPath, "-target", "http://localhost:8080",
				"-endpoint", "authority-1=http://localhost:8081"},
			want: "mutually exclusive",
		},
		{
			name: "an authority with no endpoint",
			args: []string{"-placement", placementPath,
				"-endpoint", "authority-1=http://localhost:8081"},
			want: "no endpoint was supplied",
		},
		{
			name: "an endpoint the routing never names",
			args: []string{"-placement", placementPath,
				"-endpoint", "authority-1=http://localhost:8081",
				"-endpoint", "authority-2=http://localhost:8082",
				"-endpoint", "authority-9=http://localhost:8089"},
			want: "never names",
		},
		{
			name: "the same authority twice",
			args: []string{"-placement", placementPath,
				"-endpoint", "authority-1=http://localhost:8081",
				"-endpoint", "authority-1=http://localhost:8082"},
			want: "already has endpoint",
		},
		{
			name: "a multi-organisation shape on a single-target run",
			args: []string{"-workload", "multi-org-dispersed"},
			want: "need -placement",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(append(tc.args, "-n", "1"))
			if err == nil {
				t.Fatal("a contradictory routing configuration was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
