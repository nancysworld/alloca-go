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
// and populated, since an absent field is indistinguishable from an unrecorded one once
// the report is written.
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

	for _, field := range []string{
		"go_version", "workload", "concurrency", "iterations", "warm_up",
		"generator_location", "generator_gomaxprocs", "generator_num_cpu",
		"target", "timestamp",
	} {
		if _, ok := got[field]; !ok {
			t.Errorf("manifest is missing required field %q", field)
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
