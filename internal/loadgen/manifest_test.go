package loadgen_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// TestManifestRedactsCredentials is the discriminating test for the last line of
// ag-sept-plan §6.4: "secrets and private endpoints must not be committed". A manifest is
// precisely the artifact that gets pasted into a report and committed, so a credential
// reaching it is a disclosure, not a cosmetic problem.
//
// Remove the redaction and this fails with the password in the manifest.
func TestManifestRedactsCredentials(t *testing.T) {
	m := loadgen.NewManifest(
		"https://alloca:hunter2@db.internal.example:8443/v1?token=abc123#frag",
		"hot_slot", loadgen.Options{Concurrency: 1, Iterations: 1}, "local",
		loadgen.ServiceMeta{},
	)

	var buf bytes.Buffer
	if err := loadgen.WriteReport(&buf, loadgen.Report{Manifest: m}); err != nil {
		t.Fatalf("writing report: %v", err)
	}
	body := buf.String()

	for _, secret := range []string{"hunter2", "alloca:hunter2", "token=abc123"} {
		if strings.Contains(body, secret) {
			t.Errorf("manifest leaked %q\n%s", secret, body)
		}
	}
	// The host must survive: a manifest that redacted the target entirely would be safe
	// and useless, and could not identify which service was measured.
	if !strings.Contains(body, "db.internal.example:8443") {
		t.Errorf("manifest lost the target host, so the run cannot be attributed\n%s", body)
	}
}

// TestManifestCarriesRequiredProvenance checks the fields §6.4 makes mandatory are present
// and *populated*, since an absent field is indistinguishable from an unrecorded one once
// the report is written.
//
// The earlier version of this test checked only that each JSON key existed, which is how
// `commit_sha: ""` reached the committed evidence in docs/measurements/: the key was there,
// so the test passed, and the field the run most needed to be reproducible was empty.
// Presence is not the property — population is.
func TestManifestCarriesRequiredProvenance(t *testing.T) {
	opts := loadgen.Options{Concurrency: 8, Iterations: 200, WarmUp: 2 * time.Second}
	m := loadgen.NewManifest("http://localhost:8080", "dispersed", opts, "local",
		loadgen.ServiceMeta{GoVersion: "go1.26.5", GOMAXPROCS: 4, ReservationTTL: "2m0s"})

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Neither revision is checked here, and for two different reasons. The generator's is a
	// property of how *this* binary was built, and `go test` does not stamp VCS data any more
	// than `go run` does — asserting it would fail for every correct build, and the obvious
	// way to green that again is to weaken the rule the gate rests on. The service's comes
	// from a live `/meta`, which this test has no server for.
	//
	// Both are held elsewhere against explicit values: TestIncompleteManifestCannotBeCertified
	// for the gate, and TestManifestRecordsTheServiceRevisionNotTheGenerators for the one that
	// matters most — that the two are never confused.
	for _, field := range []string{
		"generator_go_version", "workload", "concurrency", "iterations", "warm_up",
		"generator_location", "generator_gomaxprocs", "generator_num_cpu",
		"target", "timestamp",
	} {
		v, ok := got[field]
		if !ok {
			t.Errorf("manifest is missing required field %q", field)
			continue
		}
		switch v := v.(type) {
		case string:
			if v == "" {
				t.Errorf("required field %q is present but empty", field)
			}
		case float64:
			if v == 0 {
				t.Errorf("required field %q is present but zero", field)
			}
		}
	}

	if m.Concurrency != 8 || m.Iterations != 200 {
		t.Errorf("manifest does not reflect the options it was built from: %+v", m)
	}
	if m.WarmUp != "2s" {
		t.Errorf("warm-up = %q, want 2s", m.WarmUp)
	}
	if m.GeneratorNumCPU < 1 {
		t.Error("generator CPU count not captured, so generator saturation cannot be ruled out")
	}
}

// TestManifestRecordsWhetherEitherTreeWasDirty pins that both dirty-tree flags survive into
// the JSON. A dirty tree stamped with a clean-looking SHA is the provenance failure that
// cannot be spotted by reading the report, and it can happen on either side independently —
// the service and the generator are separate builds.
func TestManifestRecordsWhetherEitherTreeWasDirty(t *testing.T) {
	raw, err := json.Marshal(loadgen.NewManifest("http://localhost:8080", "dispersed",
		loadgen.Options{Concurrency: 1, Iterations: 1}, "local",
		loadgen.ServiceMeta{Modified: true}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"service_source_modified", "generator_source_modified"} {
		if _, ok := got[field]; !ok {
			t.Errorf("manifest does not record %q, so a commit SHA that does not describe "+
				"the binary cannot be detected", field)
		}
	}
	if got["service_source_modified"] != true {
		t.Error("the service's dirty-tree flag was not carried from /meta")
	}
}
