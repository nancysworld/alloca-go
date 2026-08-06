package loadgen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

func writeDeployment(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("writing deployment record: %v", err)
	}
	return path
}

// The property the whole record exists for: units running different images.
//
// This is the failure the commit SHA cannot see. The SHA is stamped into the binary, so the
// same code served from a stale `:dev` tag left pointing at an older build — or rebuilt on a
// newer base layer — carries an identical revision on every unit. Nothing in the request
// totals would reveal it either, so a run across them would record one image identity for a
// deployment that had two.
func TestUnitsOnDifferentImagesCannotSupportOneIdentity(t *testing.T) {
	path := writeDeployment(t, `{
	  "units": {
	    "alloca-service-1": "sha256:1111111111111111",
	    "alloca-service-2": "sha256:9999999999999999"
	  }
	}`)

	_, err := loadgen.LoadDeployment(path)
	if err == nil {
		t.Fatal("a deployment whose units were running different images was accepted")
	}
	for _, want := range []string{"alloca-service-1", "alloca-service-2", "111111111111", "999999999999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q — an operator needs to know which unit to "+
				"go and look at", err, want)
		}
	}
}

// A record that agrees with itself is the positive control, without which every case above
// could pass for the wrong reason.
func TestAConsistentDeploymentRecordIsAccepted(t *testing.T) {
	path := writeDeployment(t, `{
	  "image_id": "sha256:1111111111111111",
	  "image_tag": "alloca-go:6e2f7ac",
	  "units": {
	    "alloca-service-1": "sha256:1111111111111111",
	    "alloca-service-2": "sha256:1111111111111111"
	  }
	}`)

	d, err := loadgen.LoadDeployment(path)
	if err != nil {
		t.Fatalf("a consistent deployment record was refused: %v", err)
	}
	if d.ImageID != "sha256:1111111111111111" {
		t.Errorf("image id = %q", d.ImageID)
	}
	if d.ImageTag != "alloca-go:6e2f7ac" {
		t.Errorf("image tag = %q", d.ImageTag)
	}
}

// Each of these describes a deployment the record cannot vouch for, so none may certify.
func TestDeploymentRecordsThatCannotSupportAClaimAreRefused(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{
			name:     "no units at all",
			document: `{"image_id": "sha256:1111"}`,
			want:     "names no units",
		},
		{
			name:     "a unit with no image id",
			document: `{"units": {"alloca-service-1": ""}}`,
			want:     "no image id",
		},
		{
			// The record claims one image while its own observations say another — so one of
			// the two was written rather than read, and neither can be trusted after that.
			name: "a claimed image its units contradict",
			document: `{
			  "image_id": "sha256:deadbeefdeadbeef",
			  "units": {"alloca-service-1": "sha256:1111111111111111"}
			}`,
			want: "claims image",
		},
		{
			name:     "not JSON at all",
			document: `this is not a deployment record`,
			want:     "decoding",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadgen.LoadDeployment(writeDeployment(t, tc.document))
			if err == nil {
				t.Fatal("a record that cannot describe the deployment was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A single-unit record is legitimate — an unsharded containerised deployment — and the image
// id may be left to the observation rather than stated twice.
func TestASingleUnitRecordTakesItsImageFromTheUnit(t *testing.T) {
	path := writeDeployment(t, `{"units": {"alloca-service-1": "sha256:1111111111111111"}}`)

	d, err := loadgen.LoadDeployment(path)
	if err != nil {
		t.Fatalf("a single-unit deployment was refused: %v", err)
	}
	if d.ImageID != "sha256:1111111111111111" {
		t.Errorf("image id = %q, want the unit's own", d.ImageID)
	}
}
