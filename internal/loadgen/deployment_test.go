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

// A record describing the two-unit topology, agreeing with itself. Used as the positive
// control wherever a refusal is being tested, so that a case cannot pass because the fixture
// was malformed for some unrelated reason.
const twoUnitRecord = `{
  "image_id": "sha256:1111111111111111",
  "image_tag": "alloca-go:6e2f7ac",
  "units": {
    "alloca-service-1": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8081"},
    "alloca-service-2": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8082"}
  }
}`

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
	    "alloca-service-1": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8081"},
	    "alloca-service-2": {"image_id": "sha256:9999999999999999", "target": "http://localhost:8082"}
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
	d, err := loadgen.LoadDeployment(writeDeployment(t, twoUnitRecord))
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
			document: `{"units": {"alloca-service-1": {"target": "http://localhost:8081"}}}`,
			want:     "no image id",
		},
		{
			// Without a target the record cannot be bound to the run, so it describes some
			// containers on the host rather than the units the measurement addressed.
			name:     "a unit with no target",
			document: `{"units": {"alloca-service-1": {"image_id": "sha256:1111111111111111"}}}`,
			want:     "no target",
		},
		{
			// Two containers cannot serve one address. One of the two observations is stale,
			// and the run cannot tell which unit it actually reached.
			name: "two units claiming one target",
			document: `{"units": {
			  "alloca-service-1": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8081"},
			  "alloca-service-2": {"image_id": "sha256:1111111111111111", "target": "http://127.0.0.1:8081"}
			}}`,
			want: "both serving",
		},
		{
			// The record claims one image while its own observations say another — so one of
			// the two was written rather than read, and neither can be trusted after that.
			name: "a claimed image its units contradict",
			document: `{
			  "image_id": "sha256:deadbeefdeadbeef",
			  "units": {"alloca-service-1": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8081"}}
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
	path := writeDeployment(t, `{"units": {
	  "alloca-service-1": {"image_id": "sha256:1111111111111111", "target": "http://localhost:8081"}
	}}`)

	d, err := loadgen.LoadDeployment(path)
	if err != nil {
		t.Fatalf("a single-unit deployment was refused: %v", err)
	}
	if d.ImageID != "sha256:1111111111111111" {
		t.Errorf("image id = %q, want the unit's own", d.ImageID)
	}
}

// Binding is what turns "some containers shared an image" into "the units this run addressed
// shared an image". Each direction of the correspondence fails for its own reason, and a
// record that satisfies the image check while describing the wrong topology is precisely the
// evidence this is here to refuse.
func TestBindingRequiresTheObservationToDescribeTheRoutedUnits(t *testing.T) {
	routed := []string{"http://localhost:8081", "http://localhost:8082"}

	tests := []struct {
		name    string
		targets []string
		want    string
	}{
		{
			// A unit the run drove whose artifact is unknown — the gap
			// measurement-contract §11 exists to close.
			name:    "a routed unit nothing was observed for",
			targets: append(routed, "http://localhost:8083"),
			want:    "does not cover every unit",
		},
		{
			// The record describes a topology other than the measured one, most often a file
			// recorded before the topology was raised again.
			name:    "an observed unit the run does not route to",
			targets: routed[:1],
			want:    "does not route to",
		},
		{
			name:    "neither set matches the other",
			targets: []string{"http://localhost:9001", "http://localhost:9002"},
			want:    "different topology",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := loadgen.LoadDeployment(writeDeployment(t, twoUnitRecord))
			if err != nil {
				t.Fatalf("fixture record was refused: %v", err)
			}
			if err := d.BindTo(tc.targets); err == nil {
				t.Fatal("a record that does not describe the routed topology was bound to it")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The positive control for the three refusals above. Without it they could all pass because
// BindTo rejects everything, which would make the check worthless in the other direction.
func TestBindingAcceptsAnExactCorrespondence(t *testing.T) {
	d, err := loadgen.LoadDeployment(writeDeployment(t, twoUnitRecord))
	if err != nil {
		t.Fatalf("fixture record was refused: %v", err)
	}
	if err := d.BindTo([]string{"http://localhost:8081", "http://localhost:8082"}); err != nil {
		t.Fatalf("the topology the record describes was refused: %v", err)
	}
}

// The loopback spellings an operator and a container runtime each prefer name one interface.
// Failing a run because the record said 127.0.0.1 where -endpoint said localhost would be a
// false refusal, and a false refusal teaches operators to bypass the check.
func TestBindingTreatsLoopbackSpellingsAsOneUnit(t *testing.T) {
	d, err := loadgen.LoadDeployment(writeDeployment(t, twoUnitRecord))
	if err != nil {
		t.Fatalf("fixture record was refused: %v", err)
	}
	if err := d.BindTo([]string{"http://127.0.0.1:8081", "http://localhost:8082/"}); err != nil {
		t.Fatalf("loopback spellings of the same units were refused: %v", err)
	}
}
