package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/config"
)

const placementDoc = `{
	"version": "test-v1",
	"homes": {"org-a": "authority-1", "org-b": "authority-2", "org-c": "authority-1"}
}`

// The behaviour every deployment before PR3a had, and must keep having: no placement
// configuration means one authority owns everything, so nothing is refused for
// placement reasons and cross-organisation booking still works. If this test ever
// fails, PR3a has silently sharded a deployment that never asked to be.
func TestNoPlacementConfiguredIsAnUnshardedDeployment(t *testing.T) {
	p, authority, err := resolvePlacement(config.Default().Placement)
	if err != nil {
		t.Fatalf("an unconfigured deployment failed to start: %v", err)
	}
	// The unit still reports an authority, so a figure from a one-database run says
	// which database produced it rather than carrying an empty label.
	if authority == "" {
		t.Error("an unsharded unit reported no authority")
	}
	if !p.IsUnsharded() {
		t.Fatal("an unconfigured deployment must be unsharded, not unrouted")
	}
	colocated, placed := p.Colocated("org-a", "org-b")
	if !colocated || !placed {
		t.Error("an unsharded deployment must treat every pair of organisations as colocated")
	}
}

func TestInlinePlacementIsParsedAndGated(t *testing.T) {
	p, authority, err := resolvePlacement(config.PlacementSource{
		AuthorityID: "authority-1",
		Document:    placementDoc,
	})
	if err != nil {
		t.Fatalf("resolving a valid placement: %v", err)
	}
	if authority != "authority-1" {
		t.Errorf("unit authority = %q, want authority-1", authority)
	}
	if p.IsUnsharded() {
		t.Fatal("a configured placement must not be unsharded")
	}
	if v := p.Version(); v != "test-v1" {
		t.Errorf("version = %q, want test-v1", v)
	}
	if got := p.Organisations("authority-1"); len(got) != 2 {
		t.Errorf("authority-1 serves %v, want two organisations", got)
	}
}

func TestPlacementFileIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "placement.json")
	if err := os.WriteFile(path, []byte(placementDoc), 0o600); err != nil {
		t.Fatalf("writing placement file: %v", err)
	}

	p, _, err := resolvePlacement(config.PlacementSource{
		AuthorityID:  "authority-2",
		DocumentPath: path,
	})
	if err != nil {
		t.Fatalf("resolving a placement from file: %v", err)
	}
	if got := p.Organisations("authority-2"); len(got) != 1 || got[0] != "org-b" {
		t.Errorf("authority-2 serves %v, want [org-b]", got)
	}
}

// The distinction the whole design turns on: a *broken* map is not the same as *no*
// map. Each of these must stop the process rather than fall back to serving everything
// from one database, which is how one organisation's rows reach two authorities.
func TestBrokenPlacementFailsStartupRatherThanServingEverything(t *testing.T) {
	tests := []struct {
		name string
		src  config.PlacementSource
		want string
	}{
		{
			name: "document does not parse",
			src:  config.PlacementSource{AuthorityID: "authority-1", Document: `{"version":`},
			want: "placement document",
		},
		{
			name: "document has no version",
			src:  config.PlacementSource{AuthorityID: "authority-1", Document: `{"homes":{"org-a":"authority-1"}}`},
			want: "no version",
		},
		{
			name: "this unit is not in the map",
			src:  config.PlacementSource{AuthorityID: "authority-9", Document: placementDoc},
			want: "authority-9",
		},
		{
			name: "file is missing",
			src:  config.PlacementSource{AuthorityID: "authority-1", DocumentPath: "/nonexistent/placement.json"},
			want: "reading placement document",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _, err := resolvePlacement(tc.src)
			if err == nil {
				t.Fatalf("broken placement started successfully as %+v", p)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			if !p.IsZero() {
				t.Error("a failed resolution must not hand back a usable placement")
			}
		})
	}
}

// A unit bound to an authority the map does not name would pass every health check and
// refuse every request. Failing at startup makes that a configuration error rather than
// a mystery to be diagnosed from traffic.
func TestUnitNotNamedByTheMapDoesNotStart(t *testing.T) {
	_, _, err := resolvePlacement(config.PlacementSource{
		AuthorityID: "authority-9",
		Document:    placementDoc,
	})
	if err == nil {
		t.Fatal("a unit absent from its own placement map started")
	}
	if !strings.Contains(err.Error(), "test-v1") {
		t.Errorf("error %q should name the routing version so the operator knows which map was loaded", err)
	}
}

func TestResolvedPlacementAnswersTheSupportBoundary(t *testing.T) {
	p, _, err := resolvePlacement(config.PlacementSource{AuthorityID: "authority-1", Document: placementDoc})
	if err != nil {
		t.Fatalf("resolving placement: %v", err)
	}

	// org-a and org-c are different organisations on one authority: supported.
	if colocated, placed := p.Colocated("org-c", "org-a"); !colocated || !placed {
		t.Error("org-a and org-c share authority-1 and must be colocated")
	}
	// org-a and org-b are on different authorities: the one case Phase 1 refuses.
	if colocated, _ := p.Colocated("org-b", "org-a"); colocated {
		t.Error("org-a and org-b are on different authorities and must not be colocated")
	}
}
