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
		Unit  string `json:"unit"`
		Expr  string `json:"expr"`
		// Whether the query preserves one series per shard-group authority. Declared rather than
		// inferred from the PromQL: see TestPerAuthorityPanelsExposeAuthority.
		PerAuthority bool   `json:"per_authority"`
		Legend       string `json:"legend"`
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
			Expr         string `json:"expr"`
			LegendFormat string `json:"legendFormat"`
		} `json:"targets"`
	} `json:"panels"`
}

// aggregatedLabels returns the labels a `sum by (...)` / `by (a, b)` clause fans out over.
// Written against the canonical expressions this repository actually uses rather than as a
// general PromQL parser: a wrong answer here would be a test that lies, so it stays narrow.
func aggregatedLabels(expr string) []string {
	var out []string
	for rest := expr; ; {
		i := strings.Index(rest, " by (")
		if i < 0 {
			return out
		}
		rest = rest[i+len(" by ("):]
		j := strings.Index(rest, ")")
		if j < 0 {
			return out
		}
		for l := range strings.SplitSeq(rest[:j], ",") {
			if l = strings.TrimSpace(l); l != "" && l != "le" {
				out = append(out, l)
			}
		}
		rest = rest[j:]
	}
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
// measurement-contract §6 scopes the diagnostic view to the indicators a frontier argument
// rests on and excludes alerting, templating and shared-library machinery — and every one of
// those arrives by accretion, one more panel at a time, each individually reasonable.
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
			t.Errorf("dashboard declares %q, which measurement-contract §6 excludes", banned)
		}
	}

	// 9 since AG-Sept PR4a, raised from 8 by maintainer decision on 2026-08-14. Iteration C made
	// the pool the object of study rather than a background indicator: occupancy, lifecycle,
	// acquire duration and *mean* acquire duration are four separate questions, and §3.13.1's
	// finding is legible only in the last of them — a per-acquire cost that rose 16x while the
	// aggregate rate it shares an axis with would have hidden it three orders of magnitude down.
	//
	// The bound stays a bound. It exists so that adding a panel is a decision someone makes and
	// records, which is what this comment is.
	dash := loadJSON[dashboard](t, dashboardPath)
	if n := len(dash.Panels); n > 9 {
		t.Errorf("dashboard has %d panels; the diagnostic view is meant to stay compact. "+
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

// TestFannedOutPanelsCarryTheirLabelInTheLegend guards a property the drift test cannot see.
//
// TestDashboardMatchesCanonicalPanels compares *expressions*, so a panel can carry the right
// query and still be unreadable. `outcomes` is `sum by (outcome) (...)`: it returns one series
// per outcome, and the generator gave every one of them the same fixed legend, "Outcomes". On
// screen that is four or five indistinguishable lines — admitted success, business refusal,
// timeout — in the one panel whose entire purpose is telling them apart.
//
// The rule is general rather than a list of known panels, so the next `by (reason)` panel is
// covered the day it is added rather than the day someone notices the legend.
func TestFannedOutPanelsCarryTheirLabelInTheLegend(t *testing.T) {
	dash := loadJSON[dashboard](t, dashboardPath)

	checked := 0
	for _, p := range dash.Panels {
		for _, target := range p.Targets {
			labels := aggregatedLabels(target.Expr)
			if len(labels) == 0 {
				continue
			}
			checked++
			for _, l := range labels {
				if !strings.Contains(target.LegendFormat, "{{"+l+"}}") {
					t.Errorf("panel %q fans out over %q but its legend is %q: every series would "+
						"be labelled identically, hiding the distinction the panel exists to show. "+
						"Use {{%s}}.", p.Title, l, target.LegendFormat, l)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no dashboard panel aggregates by a label, so this test proved nothing")
	}
}

// TestPerAuthorityPanelsExposeAuthority is the dashboard's cardinality invariant:
//
//	if a query preserves per-authority series cardinality, its legend must expose {{authority}};
//	if it intentionally aggregates authorities, a fixed legend is correct.
//
// `sum by (outcome)` fans out *explicitly*, and the neighbouring test covers it. This covers the
// other way, which is invisible in the expression: a query that simply does not aggregate returns
// one series per scraped target. Under PR2's single target that never showed. Iteration C scrapes
// one per shard group, and every such panel became four identically-labelled lines — the pool
// occupancy graph was a dozen of them, present and useless (ag-sept-pr4.md §3).
//
// The property is read from each panel's `per_authority` flag rather than inferred from its
// PromQL. Inferring it needs a parser that is wrong at the edges — `histogram_quantile` over
// `sum by (le)` collapses, a bare selector does not, an arithmetic combination of two rates
// depends on both sides — and the flag is a statement of intent the panel author holds and a
// parser can only guess at. TestPerAuthorityMetadataMatchesTheQuery keeps the flag honest.
func TestPerAuthorityPanelsExposeAuthority(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)
	dash := loadJSON[dashboard](t, dashboardPath)

	perAuthority := map[string]bool{}
	for _, p := range canonical.Panels {
		perAuthority[grafanaForm(p.Expr)] = p.PerAuthority
	}

	checked := 0
	for _, p := range dash.Panels {
		for _, target := range p.Targets {
			if !perAuthority[target.Expr] {
				continue
			}
			checked++
			if !strings.Contains(target.LegendFormat, "{{authority}}") {
				t.Errorf("panel %q runs %q, which panels.json declares per_authority, but its "+
					"legend is %q: on a multi-unit rung every line would be labelled identically. "+
					"Use \"{{authority}} <what it is>\".",
					p.Title, target.Expr, target.LegendFormat)
			}
			// Authority first, so one unit reads the same in every panel and can be followed
			// across graphs without relying on colour.
			if !strings.HasPrefix(target.LegendFormat, "{{authority}} ") {
				t.Errorf("panel %q legend is %q: {{authority}} must come first, so the same unit "+
					"reads identically across panels", p.Title, target.LegendFormat)
			}
		}
	}
	if checked == 0 {
		t.Error("no dashboard panel is declared per_authority, so this test proved nothing")
	}
}

// TestPerAuthorityMetadataMatchesTheQuery stops the flag drifting from the expression it
// describes. The flag drives the legend, so a wrong flag is a wrong dashboard — and unlike a
// wrong legend, nothing on screen would look obviously broken.
//
// This is deliberately a narrow sanity check rather than a PromQL parser: it asserts only the one
// direction that is unambiguous. A query wrapped in a top-level `sum(...)` with no `by` clause
// collapses every series into one, so it cannot preserve per-authority cardinality, whatever the
// flag says.
func TestPerAuthorityMetadataMatchesTheQuery(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)

	for _, p := range canonical.Panels {
		if !p.PerAuthority {
			continue
		}
		if strings.HasPrefix(p.Expr, "sum(") && !strings.Contains(p.Expr, " by (") {
			t.Errorf("panel %q declares per_authority but its query is a bare sum(...), which "+
				"collapses every authority into one series: %s", p.Key, p.Expr)
		}
	}
}

// TestPanelsSharingAnAxisShareAScale keeps a readable panel readable.
//
// Resident memory (tens of millions of bytes) once shared an axis with CPU, goroutines and GC
// pause (all below ten). Grafana scales to the largest series, so the other three rendered as a
// flat line along the bottom: the panel was present, and useless. Bytes are the only unit in
// this set that can do that, so the rule is expressed as the unit rather than as a threshold.
func TestPanelsSharingAnAxisShareAScale(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)
	dash := loadJSON[dashboard](t, dashboardPath)

	unitByExpr := map[string]string{}
	for _, p := range canonical.Panels {
		unitByExpr[grafanaForm(p.Expr)] = p.Unit
	}

	for _, p := range dash.Panels {
		if len(p.Targets) < 2 {
			continue
		}
		units := map[string]bool{}
		for _, target := range p.Targets {
			units[unitByExpr[target.Expr]] = true
		}
		if units["bytes"] && len(units) > 1 {
			t.Errorf("panel %q plots bytes alongside %d other units on one axis; the byte series "+
				"will set the scale and flatten the rest", p.Title, len(units)-1)
		}
	}
}

// A metric selector: the name, and the label block constraining it when one is present. The
// block is consumed by the same match so label names inside it are not read as metric names.
var selectorRE = regexp.MustCompile(`([a-zA-Z_:][a-zA-Z0-9_:]*)\s*(\{[^}]*\})?`)

// Range selectors and grouping clauses are removed before scanning: `[$RANGE]` holds a token
// rather than a series, and `by (outcome)` holds label names.
var (
	rangeRE    = regexp.MustCompile(`\[[^\]]*\]`)
	groupingRE = regexp.MustCompile(`\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)`)
)

// promQLKeywords are identifiers that stand where a metric name could and are not one.
// Functions are excluded separately, by the parenthesis that follows them.
var promQLKeywords = map[string]bool{
	"and": true, "or": true, "unless": true, "offset": true, "bool": true,
}

// metricSelectors maps each metric name an expression selects to the label block that
// constrains it, which is empty when the selector carries none.
func metricSelectors(expr string) map[string]string {
	cleaned := groupingRE.ReplaceAllString(rangeRE.ReplaceAllString(expr, ""), " ")

	out := map[string]string{}
	for _, loc := range selectorRE.FindAllStringSubmatchIndex(cleaned, -1) {
		name := cleaned[loc[2]:loc[3]]
		if promQLKeywords[name] {
			continue
		}
		// A name followed by "(" is a function, not a series.
		if loc[1] < len(cleaned) && cleaned[loc[1]] == '(' {
			continue
		}
		var labels string
		if loc[4] >= 0 {
			labels = cleaned[loc[4]:loc[5]]
		}
		out[name] = labels
	}
	return out
}

// TestGenericMetricFamiliesAreScopedToTheirJob guards the meaning of a panel against the
// arrival of a second scrape job.
//
// `process_*` and `go_*` are exported by every Prometheus-instrumented Go process. An unscoped
// selector therefore means "the service under test" only while Prometheus scrapes exactly one
// job — true for PR2, and false the moment a host exporter is added beside it. The failure is
// silent in every direction that normally catches things: the query still succeeds, the CSV
// still has rows, the populated-series gate still passes, and the panel is three processes
// wearing the title of one.
//
// Metrics this repository owns need no scoping, because `alloca_` is a prefix no other
// exporter emits. The rule is written that way round so a new generic panel is covered on the
// day it is added rather than the day someone reads a strange number.
func TestGenericMetricFamiliesAreScopedToTheirJob(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)

	checked := 0
	for _, p := range canonical.Panels {
		for name, labels := range metricSelectors(p.Expr) {
			if strings.HasPrefix(name, "alloca_") {
				continue
			}
			checked++
			if !strings.Contains(labels, "job=") {
				t.Errorf("panel %q selects %s without a job matcher: that family is exported by "+
					"every instrumented Go process, so the panel widens to include the host "+
					"exporter and Prometheus itself once a second job is scraped\n  %s",
					p.Key, name, p.Expr)
			}
		}
	}
	if checked == 0 {
		t.Error("no canonical panel selects a metric family this repository does not own, so " +
			"this test proved nothing")
	}
}
