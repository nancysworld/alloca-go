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
	m := loadgen.NewManifest("http://localhost:8080", "dispersed", opts, "local")

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// commit_sha is checked by Validate rather than here, and deliberately so: VCS stamping
	// is a property of how the binary was *built*, and `go test` does not stamp it any more
	// than `go run` does. Asserting it in this test would fail for every correct build, and
	// the obvious way to make that green again is to weaken the rule the whole gate rests
	// on. TestIncompleteManifestCannotBeCertified holds that line against a manifest whose
	// SHA is set explicitly.
	for _, field := range []string{
		"go_version", "workload", "concurrency", "iterations", "warm_up",
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

// TestManifestRecordsWhetherTheTreeWasDirty pins that source_modified is carried from the
// build info rather than dropped. A dirty tree stamped with a clean-looking SHA is the
// provenance failure that cannot be spotted by reading the report.
func TestManifestRecordsWhetherTheTreeWasDirty(t *testing.T) {
	raw, err := json.Marshal(loadgen.NewManifest("http://localhost:8080", "dispersed",
		loadgen.Options{Concurrency: 1, Iterations: 1}, "local"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["source_modified"]; !ok {
		t.Error("manifest does not record whether the working tree was modified, so a " +
			"commit_sha that does not describe the binary cannot be detected")
	}
}
