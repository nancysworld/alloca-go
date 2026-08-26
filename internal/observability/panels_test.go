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
	"time"
)

const (
	panelsPath    = "../../deploy/observability/panels.json"
	dashboardPath = "../../deploy/observability/grafana/dashboards/alloca-frontier.json"
	// The sweep runner names required panel keys in its own source; this package is where that
	// naming can be checked against the panels that actually exist.
	sweepPath = "../../test/scripts/sweep.sh"
	// Where each job's scrape cadence is declared; a rate() window has to outlive its own.
	promConfigPath = "../../deploy/observability/prometheus.yml"
	// The other consumer of panels.json: it retains every panel, plotted or not.
	exporterPath = "../../test/scripts/export-panels.sh"

	// Owns the measured window every ranged panel has to fit inside.
	runnerPath = "../../test/scripts/itc-run.sh"
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
		// Explicit rate window, for a series whose scrape cadence the datasource-wide macro
		// does not fit. Empty means $__rate_interval.
		Range string `json:"range"`
		// Pointer so absent and false are distinguishable: absent means "plot it", the default.
		// false means exported into the cell's CSVs but deliberately kept off the dashboard.
		Display *bool `json:"display"`
	} `json:"panels"`
}

// grafanaForm applies the substitution test/scripts/gen-dashboard.py applies, so the drift
// check compares like with like. Encoding the rule here rather than stripping ranges from both
// sides is deliberate: the substitution is itself part of the contract between the canonical
// queries and their two consumers, and a test that ignored it would pass while the generator
// emitted something else.
//
// A panel that declares its own range gets that literal instead of the macro. Applying a
// different rule here would report drift that does not exist and miss drift that does.
func grafanaForm(expr, declaredRange string) string {
	if declaredRange == "" {
		declaredRange = "$__rate_interval"
	}
	return strings.ReplaceAll(expr, rangeToken, declaredRange)
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
		Title string `json:"title"`
		// "row" is a section header, not a graph: it carries no query, no unit and no axis.
		// Every rule below is about graphs, so rows are skipped rather than special-cased.
		Type    string `json:"type"`
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
	exempt := map[string]bool{}   // exported but deliberately not plotted
	for _, p := range canonical.Panels {
		if p.Expr == "" {
			t.Errorf("panel %q has an empty expression", p.Key)
		}
		if strings.Contains(p.Expr, "$__rate_interval") {
			t.Errorf("panel %q hard-codes Grafana's $__rate_interval; canonical expressions "+
				"use $RANGE so the exporter can substitute a literal window", p.Key)
		}
		expr := grafanaForm(p.Expr, p.Range)
		if prev, dup := wanted[expr]; dup {
			t.Errorf("panels %q and %q share an expression; one of them is not measuring "+
				"what its title claims", prev, p.Key)
		}
		// A `display: false` panel is exported and deliberately not plotted, so it is exempt from
		// "must reach the dashboard" — but not from the other direction: a dashboard expression
		// that is not canonical is still a number no artifact reproduces.
		if p.Display != nil && !*p.Display {
			exempt[expr] = true
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
		if !found[expr] && !exempt[expr] {
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

	// 15 since AG-Sept PR4a, from 8, in three recorded steps on 2026-08-14.
	//
	// 8 -> 9: Iteration C made the pool the object of study rather than a background indicator.
	// Occupancy, lifecycle, acquire duration and *mean* acquire duration are four separate
	// questions, and the finding that mattered is legible only in the last — a per-acquire cost that rose
	// 16x, three orders of magnitude below the aggregate rate it would otherwise share an axis
	// with.
	//
	// 9 -> 13: node_exporter arrives as VAL-NEG-7's host sensor, and the host quantities that
	// answer the degraded-cell question are small next to the ones that do not. CPU steal against total busy is a
	// rounding error on a shared axis, and steal is the series that would say whether the host was
	// descheduled — the candidate PR2 named and could not test. Memory is bytes and may not share
	// an axis at all.
	//
	// 13 -> 15: one axis carries one unit. "Process CPU and Go runtime" plotted cores, a goroutine
	// count and a GC pause duration together; the count set the scale and the other two lay flat on
	// the bottom, unreadable. Splitting them is not decoration — two of that panel's three series
	// could not be read at all, which is the same defect the pool occupancy graph had.
	//
	// The panel set stays narrower than the collector set on purpose: diskstats, netdev and
	// filesystem are scraped and not plotted, because the snapshot retains everything scraped and
	// the pool-population question was answered from a retained snapshot without re-running a cell.
	//
	// The bound stays a bound. It exists so that adding a panel is a decision someone makes and
	// records, which is what this comment is.
	// Graphs, not rows: a section header carries no query and costs no scope.
	dash := loadJSON[dashboard](t, dashboardPath)
	graphs := 0
	for _, p := range dash.Panels {
		if p.Type != "row" {
			graphs++
		}
	}
	// Raised 15 -> 20 for the PostgreSQL section. **Maintainer decision,
	// 2026-08-17**, which is the form this bound is meant to take: it was first raised to 16 for
	// the wait-event graph alone, on the "collect broadly, panel narrowly" reading, and the
	// maintainer chose to plot the remaining four rather than leave them recoverable-in-principle
	// from the snapshot.
	//
	// The reasoning is specific to this diagnosis rather than general. Checkpoints and autovacuum
	// were both refuted with no candidate left, so the next degraded cell has to be read
	// without a hypothesis to test — and a series nobody plots is a series nobody looks at when
	// they do not yet know what they are looking for. The earlier precedent says an unplotted series
	// is *recoverable*; it does not say it is noticed. The bound stays a bound, and the next graph
	// after these is a decision someone makes and records here, exactly as this one was.
	//
	// Raised 20 -> 22 for the two host disk graphs. **Maintainer decision, 2026-08-19**, and it is
	// the same argument arriving a second time with the evidence to settle it. PR4b's local
	// capacity result turned on delivered write bandwidth; diskstats was collected throughout and
	// plotted nowhere, so no cell retained the series and the numbers behind the conclusion were
	// quoted from a live Prometheus query instead of from an artifact.
	//
	// Defining the panels fixes the retention half by itself — `display: false` would have been
	// enough for that, and was the state this landed in first. What it would not fix is the half
	// this comment already names: an unplotted series is recoverable, not noticed. The storage
	// path is now the leading candidate mechanism behind an unresolved local knee, and the next
	// degraded cell has to be read by someone who does not yet know that. Two graphs rather than
	// one because utilisation and queue depth must be read together — utilisation sat at ~1.0 at
	// every topology while the device was demonstrably not saturated, so utilisation alone
	// supports the wrong conclusion — and bandwidth carries a different unit and cannot share
	// their axis.
	if graphs > 22 {
		t.Errorf("dashboard has %d graphs; the diagnostic view is meant to stay compact. "+
			"Adding one is a scope decision, not a tidy-up", graphs)
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
			"grafana":  grafanaForm(p.Expr, p.Range),
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
// occupancy graph was a dozen of them, present and useless.
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
		perAuthority[grafanaForm(p.Expr, p.Range)] = p.PerAuthority
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

// TestPopulatedSeriesGateNamesPanelsThatExist closes a coupling that fails silently in the wrong
// direction.
//
// sweep.sh refuses a cell whose required panels retained no samples, and it names those panels by
// key in its own source. The lookup is `points.get(key, 0)`, so a key panels.json no longer
// defines reads as zero points and refuses **every** cell — a gate that looks like it is working
// while actually being unsatisfiable. PR4a hit exactly that: `pool_max` was named there and then
// dropped from panels.json.
//
// Failing closed is the right direction for a live run, and useless as a signal, because the
// refusal names a missing measurement rather than a missing panel definition.
func TestPopulatedSeriesGateNamesPanelsThatExist(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(sweepPath))
	if err != nil {
		t.Fatalf("reading %s: %v", sweepPath, err)
	}

	// The single line in sweep.sh that lists the required keys.
	m := regexp.MustCompile(`empty = \[k for k in \(([^)]*)\)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("sweep.sh no longer contains the populated-series key list this test guards; " +
			"find it and update the pattern rather than deleting the test")
	}

	canonical := loadJSON[canonicalPanels](t, panelsPath)
	defined := map[string]bool{}
	for _, p := range canonical.Panels {
		defined[p.Key] = true
	}

	keys := regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(string(m[1]), -1)
	if len(keys) == 0 {
		t.Fatal("parsed no panel keys out of sweep.sh's populated-series gate")
	}
	for _, k := range keys {
		if !defined[k[1]] {
			t.Errorf("sweep.sh requires panel %q to be populated, but panels.json does not define "+
				"it: every cell would be refused for retaining no samples of a panel that was "+
				"never exported", k[1])
		}
	}
}

// TestRatePanelsOutliveTheirScrapeInterval catches a panel that renders "No data" while its
// series is present, healthy and being scraped.
//
// rate() needs at least two samples inside its window. Grafana computes $__rate_interval from the
// datasource's single `timeInterval`, which is 1s here to match the service job — but the host job
// scrapes at 5s, so the macro resolves shorter than two host scrapes and the query returns nothing
// at all. Both host rate() panels shipped that way: the CSV export had data, because the
// exporter substitutes a literal window, and only the dashboard was blank.
//
// Nothing else notices. The query succeeds, the panel exists, the populated-series gate reads the
// CSV rather than the dashboard, and the failure is visible only to someone looking at Grafana.
func TestRatePanelsOutliveTheirScrapeInterval(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)

	// Scrape cadence per job, read from the Prometheus config so the two cannot drift.
	raw, err := os.ReadFile(filepath.Clean(promConfigPath))
	if err != nil {
		t.Fatalf("reading %s: %v", promConfigPath, err)
	}
	interval := scrapeIntervalsByJob(string(raw))
	if len(interval) == 0 {
		t.Fatal("parsed no scrape intervals from prometheus.yml")
	}

	checked := 0
	for _, p := range canonical.Panels {
		if !strings.Contains(p.Expr, "[$RANGE]") {
			continue
		}
		job := jobSelectorIn(p.Expr)
		if job == "" {
			continue // no job constraint: it rides the global interval, which the macro matches
		}
		scrape, ok := interval[job]
		if !ok {
			t.Errorf("panel %q selects job=%q, which prometheus.yml does not define", p.Key, job)
			continue
		}
		checked++

		// A panel on a job scraped more slowly than the datasource's timeInterval cannot rely on
		// $__rate_interval and must declare its own window, wide enough for two samples.
		if scrape <= interval["__global__"] {
			continue
		}
		if p.Range == "" {
			t.Errorf("panel %q queries job=%q, scraped every %v — slower than the datasource's "+
				"timeInterval — but declares no range, so $__rate_interval resolves shorter than "+
				"two scrapes and the panel renders empty. Declare \"range\".",
				p.Key, job, scrape)
			continue
		}
		declared, err := time.ParseDuration(p.Range)
		if err != nil {
			t.Errorf("panel %q has range %q, which is not a duration: %v", p.Key, p.Range, err)
			continue
		}
		if declared < 2*scrape {
			t.Errorf("panel %q declares range %v over a job scraped every %v: rate() needs two "+
				"samples in the window, so this can return nothing", p.Key, declared, scrape)
		}
	}
	if checked == 0 {
		t.Error("no job-scoped rate() panel was checked, so this test proved nothing")
	}
}

// scrapeIntervalsByJob reads prometheus.yml's global interval as "__global__" and each job's
// override under its own name. Deliberately textual: the file is small, the shape is fixed, and a
// YAML dependency for two fields would be the heavier answer.
func scrapeIntervalsByJob(cfg string) map[string]time.Duration {
	out := map[string]time.Duration{}

	global := regexp.MustCompile(`(?m)^global:(?:\n(?:[ \t]+.*|\s*)$)*`).FindString(cfg)
	if m := regexp.MustCompile(`scrape_interval:\s*(\S+)`).FindStringSubmatch(global); m != nil {
		if d, err := time.ParseDuration(m[1]); err == nil {
			out["__global__"] = d
		}
	}

	jobs := regexp.MustCompile(`(?m)^\s*-\s*job_name:\s*(\S+)`).FindAllStringSubmatchIndex(cfg, -1)
	for i, j := range jobs {
		name := cfg[j[2]:j[3]]
		end := len(cfg)
		if i+1 < len(jobs) {
			end = jobs[i+1][0]
		}
		body := cfg[j[1]:end]
		if m := regexp.MustCompile(`scrape_interval:\s*(\S+)`).FindStringSubmatch(body); m != nil {
			if d, err := time.ParseDuration(m[1]); err == nil {
				out[name] = d
				continue
			}
		}
		out[name] = out["__global__"]
	}
	return out
}

// jobSelectorIn returns the job a panel constrains itself to, or "" when it names none.
func jobSelectorIn(expr string) string {
	m := regexp.MustCompile(`job\s*=\s*"([^"]+)"`).FindStringSubmatch(expr)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestUnplottedPanelsAreStillExported pins the half of `display: false` that has no visible
// symptom.
//
// A panel dropped from the dashboard must keep producing a CSV, because the two sets answer
// different questions: the dashboard is what an operator reads at a glance, and the export is what
// a report is checked against. `pool_idle` is the case — redundant on screen, since it is exactly
// `total - acquired`, and load-bearing in the record, whose reading is "idle stayed at
// 6-9 while acquire cost rose".
//
// If the exporter ever learned to skip these, nothing would fail: the dashboard would look right,
// the cell would complete, and the series would simply not be in the CSV.
// A ranged panel whose range is not shorter than the measured window exports nothing: the panel
// exporter shifts the first evaluation by one range so no exported point carries pre-window
// samples, and when that shift lands at or past the window's end it refuses the whole cell.
//
// This is caught here because the alternative is where it was actually caught — 69 seconds into a
// measured cell, after the reseed and the full window, by a panel added with a 60s range against
// the 60s default. Everything before the export had already passed. The two files that must agree
// are panels.json and itc-run.sh's WINDOW default, and nothing tied them together.
func TestRangedPanelsFitInsideTheDefaultWindow(t *testing.T) {
	canonical := loadJSON[canonicalPanels](t, panelsPath)

	// Read from itc-run.sh rather than restated here, so this test cannot be the third place the
	// window is written down and the first to go stale.
	raw, err := os.ReadFile(filepath.Clean(runnerPath))
	if err != nil {
		t.Fatalf("reading %s: %v", runnerPath, err)
	}
	m := regexp.MustCompile(`WINDOW="\$\{WINDOW:-([0-9]+[a-z]+)\}"`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("could not find the WINDOW default in %s; if it moved, this test must follow it "+
			"rather than be deleted", runnerPath)
	}
	window, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatalf("WINDOW default %q in %s does not parse: %v", m[1], runnerPath, err)
	}

	checked := 0
	for _, p := range canonical.Panels {
		if !strings.Contains(p.Expr, "[$RANGE]") {
			continue // instant selectors carry no range and keep the full window
		}
		effective := p.Range
		if effective == "" {
			effective = canonical.ExportRange
		}
		declared, err := time.ParseDuration(effective)
		if err != nil {
			t.Errorf("panel %q declares range %q, which does not parse: %v", p.Key, effective, err)
			continue
		}
		checked++
		if declared >= window {
			t.Errorf("panel %q has range %v against a %v measured window, so the exporter's "+
				"one-range shift lands at or past the window end and refuses the cell after it "+
				"has been driven. Lower the panel's range, or make it an instant selector if the "+
				"quantity is a counter whose steps read better than its rate", p.Key, declared, window)
		}
	}
	if checked == 0 {
		t.Error("no ranged panels checked; the selector for [$RANGE] has stopped matching")
	}
}

func TestUnplottedPanelsAreStillExported(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(exporterPath))
	if err != nil {
		t.Fatalf("reading %s: %v", exporterPath, err)
	}
	if strings.Contains(string(raw), "display") {
		t.Error("export-panels.sh mentions `display`: the flag governs the dashboard only, and a " +
			"panel omitted from the screen must still be retained in the cell's CSVs")
	}

	canonical := loadJSON[canonicalPanels](t, panelsPath)
	unplotted := 0
	for _, p := range canonical.Panels {
		if p.Display != nil && !*p.Display {
			unplotted++
		}
	}
	if unplotted == 0 {
		t.Skip("no panel currently sets display:false")
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
		unitByExpr[grafanaForm(p.Expr, p.Range)] = p.Unit
	}

	for _, p := range dash.Panels {
		if len(p.Targets) < 2 {
			continue
		}
		units := map[string]bool{}
		for _, target := range p.Targets {
			units[unitByExpr[target.Expr]] = true
		}
		// One axis, one unit — the general rule, not just the bytes case that prompted it.
		//
		// Bytes were the first offender because they are the most extreme, but the defect is not
		// about magnitude: it is that an axis carrying two units cannot be labelled truthfully,
		// and the larger series sets the scale whatever the units are. "Process CPU and Go
		// runtime" plotted cores (~0.5), a goroutine count (~30) and a GC pause (~0.0001) on one
		// axis; the count won and the other two lay flat on the bottom, present and unreadable.
		if len(units) > 1 {
			names := make([]string, 0, len(units))
			for u := range units {
				names = append(names, u)
			}
			sort.Strings(names)
			t.Errorf("panel %q plots %v on one axis: the axis cannot be labelled for all of them, "+
				"and the largest series sets the scale for the rest", p.Title, names)
		}
	}
}

// TestEveryPanelDeclaresAGrafanaUnit stops an axis rendering raw numbers.
//
// A panel with no unit shows resident memory as "24000000" rather than "22.9 MiB", and a GC pause
// as "0.0001" rather than "100 µs". Grafana can scale and suffix both; it has to be told what the
// numbers are, and nothing fails if it is not.
func TestEveryPanelDeclaresAGrafanaUnit(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(dashboardPath))
	if err != nil {
		t.Fatalf("reading dashboard: %v", err)
	}
	var doc struct {
		Panels []struct {
			Title       string `json:"title"`
			Type        string `json:"type"`
			FieldConfig struct {
				Defaults struct {
					Unit string `json:"unit"`
				} `json:"defaults"`
			} `json:"fieldConfig"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing dashboard: %v", err)
	}

	for _, p := range doc.Panels {
		if p.Type == "row" {
			continue
		}
		if p.FieldConfig.Defaults.Unit == "" {
			t.Errorf("panel %q declares no Grafana unit, so its axis renders raw numbers", p.Title)
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
