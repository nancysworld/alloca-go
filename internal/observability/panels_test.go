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
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	panelsPath    = "../../deploy/observability/panels.json"
	dashboardPath = "../../deploy/observability/grafana/dashboards/alloca-frontier.json"
)

type canonicalPanels struct {
	ExportRange string `json:"export_range"`
	Panels      []struct {
		Key   string `json:"key"`
		Title string `json:"title"`
		Expr  string `json:"expr"`
	} `json:"panels"`
}

// grafanaForm applies the substitution test/scripts/gen-dashboard.py applies, so the drift
// check compares like with like. Encoding the rule here rather than stripping ranges from both
// sides is deliberate: the substitution is itself part of the contract between the canonical
// queries and their two consumers, and a test that ignored it would pass while the generator
// emitted something else.
func grafanaForm(expr string) string {
	return strings.ReplaceAll(expr, rangeToken, "$__rate_interval")
}

// exporterForm is the other consumer's substitution: a literal duration, recorded per cell.
//
// The duration is bare — "15s", not "[15s]" — because canonical expressions already write the
// selector as [$RANGE]. Passing a bracketed value yields [[15s]], which Prometheus rejects with
// a 400 that only appears when the exporter runs against a live server.
func exporterForm(expr, duration string) string {
	return strings.ReplaceAll(expr, rangeToken, duration)
}

const rangeToken = "$RANGE"

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

	wanted := map[string]string{} // grafana-form expr -> key
	for _, p := range canonical.Panels {
		if p.Expr == "" {
			t.Errorf("panel %q has an empty expression", p.Key)
		}
		if strings.Contains(p.Expr, "$__rate_interval") {
			t.Errorf("panel %q hard-codes Grafana's $__rate_interval; canonical expressions "+
				"use $RANGE so the exporter can substitute a literal window", p.Key)
		}
		expr := grafanaForm(p.Expr)
		if prev, dup := wanted[expr]; dup {
			t.Errorf("panels %q and %q share an expression; one of them is not measuring "+
				"what its title claims", prev, p.Key)
		}
		wanted[expr] = p.Key
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

// TestRangeTokenSubstitutesToValidPromQL closes the gap the review named: panels.json is
// claimed as the single query source for both Grafana and the exporter, but a range selector
// has to become something concrete before the Prometheus HTTP API will accept it.
//
// This pins the rule rather than the parser — it asserts what each consumer's substitution
// produces, so a token that silently survives into an exported query fails here rather than at
// export time, when the run has already happened and the service is gone.
func TestRangeTokenSubstitutesToValidPromQL(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)

	if canonical.ExportRange == "" {
		t.Fatal("panels.json declares no export_range, so an exported CSV could not say what " +
			"window its rates were computed over")
	}
	// Must be a Prometheus duration. A bare number is the likely mistake and would produce
	// queries that fail only when the exporter runs.
	if !regexp.MustCompile(`^[0-9]+(ms|s|m|h)$`).MatchString(canonical.ExportRange) {
		t.Errorf("export_range = %q, which is not a Prometheus duration", canonical.ExportRange)
	}

	rangeSelector := regexp.MustCompile(`\[[^\]]*\]`)
	validSelector := regexp.MustCompile(`^\[[0-9]+(ms|s|m|h)\]$`)
	substituted := 0

	for _, p := range canonical.Panels {
		if !strings.Contains(p.Expr, rangeToken) {
			// Instant queries (gauges) legitimately have no range selector.
			if rangeSelector.MatchString(p.Expr) {
				t.Errorf("panel %q has a range selector that is not %s: %s",
					p.Key, rangeToken, p.Expr)
			}
			continue
		}
		substituted++

		for name, got := range map[string]string{
			"grafana":  grafanaForm(p.Expr),
			"exporter": exporterForm(p.Expr, canonical.ExportRange),
		} {
			if strings.Contains(got, rangeToken) {
				t.Errorf("panel %q still contains %s after the %s substitution: %s",
					p.Key, rangeToken, name, got)
			}
		}

		// The exporter's form must be literally parseable: a range selector holding exactly a
		// duration, with no macro and no stray bracket left in it.
		//
		// The selector pattern is anchored on both brackets rather than trimmed, because
		// strings.Trim takes a *cutset* and would strip both brackets of "[[15s]" — turning the
		// malformed double-bracket substitution into a value that passes. That bug was in this
		// test until a live query returned 400 on it.
		exported := exporterForm(p.Expr, canonical.ExportRange)
		for _, sel := range rangeSelector.FindAllString(exported, -1) {
			if !validSelector.MatchString(sel) {
				t.Errorf("panel %q exports range selector %q, which Prometheus cannot parse",
					p.Key, sel)
			}
		}
		if strings.Contains(exported, "$") {
			t.Errorf("panel %q exports a query still carrying a variable: %s", p.Key, exported)
		}
	}

	if substituted == 0 {
		t.Error("no canonical panel uses the range token, so this test proved nothing")
	}
}
