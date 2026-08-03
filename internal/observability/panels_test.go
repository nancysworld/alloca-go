// Package observability_test guards the one invariant the PR2 diagnostic stack has: the
// dashboard an operator watches and the queries the report quotes are the same queries.
//
// There is no non-test Go code here. The artifacts under deploy/observability are consumed by
// Prometheus, Grafana and a shell exporter rather than by this module, so a test is the only
// place in the build that can notice them drifting apart — and drift is silent by nature: a
// dashboard showing a stale expression looks exactly like one showing the right one.
package observability_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const (
	panelsPath    = "../../deploy/observability/panels.json"
	dashboardPath = "../../deploy/observability/grafana/dashboards/alloca-frontier.json"
)

type canonicalPanels struct {
	Panels []struct {
		Key   string `json:"key"`
		Title string `json:"title"`
		Expr  string `json:"expr"`
	} `json:"panels"`
}

type dashboard struct {
	UID    string `json:"uid"`
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
}

func loadJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return out
}

// TestDashboardMatchesCanonicalPanels is the drift guard. Every canonical expression must
// appear in the dashboard and every dashboard expression must be canonical — both directions,
// because each failure mode is different: a missing one is a panel nobody can see, and an
// extra one is a number on screen that no artifact can reproduce.
func TestDashboardMatchesCanonicalPanels(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)
	dash := loadJSON[dashboard](t, dashboardPath)

	if len(canonical.Panels) == 0 {
		t.Fatal("no canonical panels, so this test would pass vacuously")
	}

	wanted := map[string]string{} // expr -> key
	for _, p := range canonical.Panels {
		if p.Expr == "" {
			t.Errorf("panel %q has an empty expression", p.Key)
		}
		if prev, dup := wanted[p.Expr]; dup {
			t.Errorf("panels %q and %q share an expression; one of them is not measuring "+
				"what its title claims", prev, p.Key)
		}
		wanted[p.Expr] = p.Key
	}

	found := map[string]bool{}
	for _, p := range dash.Panels {
		for _, target := range p.Targets {
			if _, ok := wanted[target.Expr]; !ok {
				t.Errorf("dashboard panel %q queries an expression that is not canonical, so "+
					"the exporter will not reproduce it:\n  %s", p.Title, target.Expr)
				continue
			}
			found[target.Expr] = true
		}
	}

	var missing []string
	for expr, key := range wanted {
		if !found[expr] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("canonical panel %q appears in no dashboard target", key)
	}
}

// TestDashboardIsDeliberatelySmall pins the scope bound rather than a style preference.
// ag-sept-plan §14 excludes rich dashboards, alerting and plugins from PR2, and every one of
// those arrives by accretion — one more panel at a time, each individually reasonable.
func TestDashboardIsDeliberatelySmall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(dashboardPath))
	if err != nil {
		t.Fatalf("reading dashboard: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("parsing dashboard: %v", err)
	}

	for _, banned := range []string{"alerting", "annotations", "templating", "libraryPanels"} {
		if v, ok := generic[banned]; ok && v != nil {
			t.Errorf("dashboard declares %q, which PR2 excludes (ag-sept-plan §14)", banned)
		}
	}

	dash := loadJSON[dashboard](t, dashboardPath)
	if n := len(dash.Panels); n > 8 {
		t.Errorf("dashboard has %d panels; PR2's diagnostic view is meant to stay compact. "+
			"Adding one is a scope decision, not a tidy-up", n)
	}
	if dash.UID != "alloca-frontier" {
		t.Errorf("dashboard uid = %q; provisioning and any saved link depend on it", dash.UID)
	}

	// Exactly one dashboard file. The provisioner loads the whole directory, so a second one
	// would appear in Grafana without any review of whether PR2 should have it.
	entries, err := filepath.Glob(filepath.Dir(dashboardPath) + "/*.json")
	if err != nil {
		t.Fatalf("globbing dashboards: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("found %d dashboard files, want exactly 1: %v", len(entries), entries)
	}
}
