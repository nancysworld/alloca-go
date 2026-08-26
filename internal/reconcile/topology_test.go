package reconcile_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

// The single most important property, and the one the plan calls out by name: a correct
// two-authority run must reconcile. Looping the organisation-scoped entry point against the
// unchanged global report would compare one organisation's rows with every organisation's
// totals, which fails this exact case — and would then be "fixed" by loosening the
// comparison until it stopped failing.
func TestATwoAuthorityRunReconcilesAgainstGlobalClientTotals(t *testing.T) {
	// Ten admitted reserves, five landing on each authority.
	report := reportWith(10)

	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		// Two organisations each, counts stated per organisation: 5 per authority, 10 in all.
		scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
		scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
	}, report)
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}

	if !res.ChecksOK() {
		for name, checks := range res.PerAuthority {
			for _, c := range checks {
				if !c.OK {
					t.Errorf("per-authority %s: %s", name, c.Detail)
				}
			}
		}
		for _, c := range res.Aggregate {
			if !c.OK {
				t.Errorf("aggregate: %s", c.Detail)
			}
		}
		t.Fatal("a correct two-authority run did not reconcile")
	}

	if len(res.Authorities) != 2 {
		t.Errorf("verdict names %v, want both authorities", res.Authorities)
	}
	if len(res.PerAuthority["authority-1"]) == 0 || len(res.PerAuthority["authority-2"]) == 0 {
		t.Error("each authority must carry its own local safety checks")
	}
}

// A safety violation on one authority must not be diluted by the other being clean. Local
// invariants are properties of one authority's rows and mean nothing averaged.
func TestASafetyViolationOnOneAuthorityFailsTheVerdict(t *testing.T) {
	report := reportWith(10)

	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-1", []domain.OrganisationID{"org-a"}, counts{live: 5, claims: 5, records: 5, overCapacity: 1}, 5),
		scope(t, "authority-2", []domain.OrganisationID{"org-b"}, counts{live: 5, claims: 5, records: 5}, 5),
	}, report)
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}

	if res.ChecksOK() {
		t.Fatal("an over-capacity slot on one authority produced a clean verdict")
	}
	var named bool
	for _, c := range res.PerAuthority["authority-1"] {
		if !c.OK && strings.Contains(c.Detail, "authority-1") {
			named = true
		}
	}
	if !named {
		t.Error("the failure must name the authority it happened on")
	}
	for _, c := range res.PerAuthority["authority-2"] {
		if !c.OK {
			t.Errorf("the healthy authority was failed too: %s", c.Detail)
		}
	}
}

// Aggregate arithmetic is over the topology, not per authority: five reservations on each of
// two authorities is ten, and ten admitted reserves reconcile with it.
func TestAggregateComparisonUsesTheWholeTopology(t *testing.T) {
	// Twelve persisted across two authorities, but only ten admitted.
	report := reportWith(10)

	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-1", []domain.OrganisationID{"org-a"}, counts{live: 6, claims: 6, records: 5}, 5),
		scope(t, "authority-2", []domain.OrganisationID{"org-b"}, counts{live: 6, claims: 6, records: 5}, 5),
	}, report)
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}

	if res.ChecksOK() {
		t.Fatal("12 persisted reservations against 10 admitted reserves reconciled")
	}
	var found bool
	for _, c := range res.Aggregate {
		if !c.OK && strings.Contains(c.Detail, "12") {
			found = true
		}
	}
	if !found {
		t.Errorf("the aggregate check should report the summed count; got %+v", res.Aggregate)
	}
}

// A verdict that cannot say what it examined cannot be trusted to have examined everything.
func TestVerdictNamesEveryAuthorityItRead(t *testing.T) {
	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-2", []domain.OrganisationID{"org-b"}, counts{live: 5, claims: 5, records: 5}, 5),
		scope(t, "authority-1", []domain.OrganisationID{"org-a"}, counts{live: 5, claims: 5, records: 5}, 5),
	}, reportWith(10))
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}
	if len(res.Authorities) != 2 || res.Authorities[0] != "authority-1" {
		t.Errorf("authorities = %v, want both, sorted", res.Authorities)
	}
}

// An authority with no organisations means the placement map and the verifier disagree about
// the topology. Verifying it anyway would produce a verdict over a subset while reporting on
// the whole.
func TestAnAuthorityWithNoOrganisationsIsASetupError(t *testing.T) {
	_, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-1", nil, counts{}, 0),
	}, reportWith(0))
	if err == nil {
		t.Fatal("an authority owning no organisations was verified")
	}
	if !strings.Contains(err.Error(), "disagree about the topology") {
		t.Errorf("error %q should say the map and the verifier disagree", err)
	}
}

// A topology where only some units were scraped must be refused for *that* reason.
//
// The server-side count is a sum across units, so an unscraped unit lowers it — which is
// arithmetically indistinguishable from a service that dropped requests, and is reported as
// though the units that were scraped had disagreed with the client. The operator is then
// looking for a defect in a service that behaved correctly.
func TestAPartiallyScrapedTopologyIsRefusedForTheMissingScrape(t *testing.T) {
	scraped := scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"},
		counts{live: 2, claims: 2, records: 2}, 4)
	unscraped := scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"},
		counts{live: 3, claims: 3, records: 3}, 6)
	unscraped.Scrapes = reconcile.Scrapes{}

	res, err := reconcile.RunTopology(context.Background(),
		[]reconcile.AuthorityScope{scraped, unscraped}, reportWith(10))
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}
	if res.ChecksOK() {
		t.Fatal("a topology missing one unit's scrape produced a clean verdict")
	}

	var detail string
	for _, c := range res.Aggregate {
		if !c.OK && c.Name == "server totals vs client totals" {
			detail = c.Detail
		}
	}
	if !strings.Contains(detail, "authority-2") {
		t.Errorf("the failing check does not name the unscraped unit: %q", detail)
	}
	if strings.Contains(detail, "server counted") {
		t.Errorf("the failure is reported as a count disagreement, which sends the operator "+
			"looking for a service defect: %q", detail)
	}
}

// The control for the case above: scraping *nothing* is a different and honest state. No
// server-side count participates, and the run is refused for lacking one of the three counts
// §12 requires rather than for a partial one.
func TestATopologyWithNoScrapesAtAllSaysSo(t *testing.T) {
	scopes := []reconcile.AuthorityScope{
		scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
		scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
	}
	for i := range scopes {
		scopes[i].Scrapes = reconcile.Scrapes{}
	}

	res, err := reconcile.RunTopology(context.Background(), scopes, reportWith(10))
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}
	var detail string
	for _, c := range res.Aggregate {
		if !c.OK && c.Name == "server totals vs client totals" {
			detail = c.Detail
		}
	}
	if !strings.Contains(detail, "no metrics scrape was supplied") {
		t.Errorf("an unscraped topology should say no scrape participated; got %q", detail)
	}
}

func TestNoAuthoritiesIsASetupError(t *testing.T) {
	if _, err := reconcile.RunTopology(context.Background(), nil, reportWith(0)); err == nil {
		t.Fatal("a run with no authorities produced a verdict")
	}
}

// --- fixtures ---------------------------------------------------------------------------
//
// countAuthority issues one row-returning query per organisation, so a fake Querier that
// answers with fixed counts is enough to exercise the aggregation and the per-authority
// split without a schema. The queries themselves are covered by the integration suite,
// which has one.

type counts struct {
	live, claims, records, overCapacity, overlaps int
}

type fakeRow struct {
	c   counts
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	values := []int{r.c.live, r.c.overCapacity, r.c.records, r.c.records, r.c.claims, r.c.overlaps}
	for i, v := range values {
		if i >= len(dest) {
			break
		}
		if p, ok := dest[i].(*int); ok {
			*p = v
		}
	}
	return nil
}

type fakeQuerier struct{ row fakeRow }

func (q fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

func (q fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not used by the topology verifier")
}

// scope builds one authority's scope. Counts are stated **per organisation**, so an
// authority owning two organisations reports twice them — the aggregation under test, done
// in the open rather than inside the helper.
//
// served is this unit's own server-side count, supplied as an After scrape with no baseline:
// measurement-contract §12 is a three-way agreement, and a topology test that omitted the
// server's third would be exercising the weaker gate under the stronger gate's name.
func scope(
	t *testing.T,
	authority domain.AuthorityID,
	orgs []domain.OrganisationID,
	perOrg counts,
	served int,
) reconcile.AuthorityScope {
	t.Helper()
	return reconcile.AuthorityScope{
		Authority: authority,
		Orgs:      orgs,
		Querier:   fakeQuerier{row: fakeRow{c: perOrg}},
		Scrapes: reconcile.Scrapes{After: reconcile.ServerTotals{{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeAdmittedSuccess,
			Count:     served,
		}}},
	}
}

// reportWith builds a report claiming `admitted` fresh reserves and `mutations` fresh
// mutations, with a manifest complete enough that a verdict below LevelLocal means
// reconciliation refused it rather than provenance.
// reportReaching is a report whose manifest records the topology the run actually reached,
// which is what the scopes are checked against.
func reportReaching(admitted int, assignment map[string][]string) loadgen.Report {
	r := reportWith(admitted)
	r.Manifest.UnitCount = len(assignment)
	r.Manifest.AuthorityCount = len(assignment)
	r.Manifest.RoutingVersion = "pr3b-v1"
	r.Manifest.PlacementAssignment = assignment
	return r
}

var twoAuthorityRun = map[string][]string{
	"authority-1": {"org-a", "org-c"},
	"authority-2": {"org-b", "org-d"},
}

// Verifying whichever scopes the caller passes says nothing about whether they are the run's.
//
// Each case below produces a clean verdict over a topology the report does not describe, and
// nothing in the numbers would reveal it: the omitted authority is never read, so its rows
// cannot contradict anything, and the duplicated one contributes its rows twice to totals
// that are then compared against themselves.
func TestScopesThatDoNotMatchTheCertifiedTopologyAreRefused(t *testing.T) {
	tests := []struct {
		name   string
		scopes func(*testing.T) []reconcile.AuthorityScope
		want   string
	}{
		{
			name: "an authority omitted",
			scopes: func(t *testing.T) []reconcile.AuthorityScope {
				return []reconcile.AuthorityScope{
					scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 5, claims: 5, records: 5}, 10),
				}
			},
			want: "no scope was supplied for it",
		},
		{
			name: "an authority supplied twice",
			scopes: func(t *testing.T) []reconcile.AuthorityScope {
				return []reconcile.AuthorityScope{
					scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
					scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
					scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
				}
			},
			want: "supplied twice",
		},
		{
			name: "an organisation omitted from an authority's scope",
			scopes: func(t *testing.T) []reconcile.AuthorityScope {
				return []reconcile.AuthorityScope{
					scope(t, "authority-1", []domain.OrganisationID{"org-a"}, counts{live: 2, claims: 2, records: 2}, 4),
					scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
				}
			},
			want: "different partition",
		},
		{
			name: "an organisation attributed to the wrong authority",
			scopes: func(t *testing.T) []reconcile.AuthorityScope {
				return []reconcile.AuthorityScope{
					scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-b"}, counts{live: 2, claims: 2, records: 2}, 4),
					scope(t, "authority-2", []domain.OrganisationID{"org-c", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
				}
			},
			want: "different partition",
		},
		{
			name: "an authority this run never reached",
			scopes: func(t *testing.T) []reconcile.AuthorityScope {
				return []reconcile.AuthorityScope{
					scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
					scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
					scope(t, "authority-3", []domain.OrganisationID{"org-e"}, counts{}, 0),
				}
			},
			want: "never reached",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reconcile.RunTopology(context.Background(), tc.scopes(t),
				reportReaching(10, twoAuthorityRun))
			if err == nil {
				t.Fatal("a verdict was produced over a topology the report does not describe")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The positive control: scopes that do match the certified topology must still verify, or
// every case above would pass for the wrong reason.
func TestScopesMatchingTheCertifiedTopologyStillVerify(t *testing.T) {
	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
		scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
	}, reportReaching(10, twoAuthorityRun))
	if err != nil {
		t.Fatalf("scopes matching the report were refused: %v", err)
	}
	if !res.ChecksOK() {
		t.Error("a correct two-authority run did not reconcile")
	}
}

// measurement-contract §12's fourth rule was absent from the topology path: RunTopology
// checked local safety, reservations, claims, idempotency records and server totals, and never
// asked whether the client's own outcomes were inside the closed set.
//
// Both halves are exercised because they fail differently: an outcome the contract does not
// define, and totals that do not sum to the completed count — the shape a replay counted as
// its own outcome produces.
func TestTheAggregateVerdictChecksOutcomeClosure(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(*loadgen.Summary)
		want    string
	}{
		{
			name: "an outcome outside the closed set",
			corrupt: func(s *loadgen.Summary) {
				s.Totals[0].Outcome = domain.Outcome("teapot")
			},
			want: "closed terminal-outcome set",
		},
		{
			name: "totals that do not sum to the completed count",
			corrupt: func(s *loadgen.Summary) {
				s.Completed = 11
			},
			want: "double-counted or missing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := reportReaching(10, twoAuthorityRun)
			tc.corrupt(&report.Summary)

			res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
				scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
				scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
			}, report)
			if err != nil {
				t.Fatalf("RunTopology: %v", err)
			}
			if res.ChecksOK() {
				t.Fatal("a run whose client outcomes break measurement-contract §12's closure rule reconciled cleanly")
			}

			var named bool
			for _, c := range res.Aggregate {
				if !c.OK && strings.Contains(c.Detail, tc.want) {
					named = true
				}
			}
			if !named {
				t.Errorf("no aggregate check reported %q; the closure rule is not being applied "+
					"once globally", tc.want)
			}
		})
	}
}

// Passing checks are not a certified run, and a caller that conflates them would quote a run
// whose provenance the ladder refused.
func TestPassingChecksAreNotACertifiedRun(t *testing.T) {
	report := reportReaching(10, twoAuthorityRun)
	// Sound summary, correct rows, every check passes — but the manifest cannot support a
	// claim, because the units did not describe one deployment.
	report.Manifest.TopologyDisagreement = "units are running different code"

	res, err := reconcile.RunTopology(context.Background(), []reconcile.AuthorityScope{
		scope(t, "authority-1", []domain.OrganisationID{"org-a", "org-c"}, counts{live: 2, claims: 2, records: 2}, 4),
		scope(t, "authority-2", []domain.OrganisationID{"org-b", "org-d"}, counts{live: 3, claims: 3, records: 3}, 6),
	}, report)
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}

	if !res.ChecksOK() {
		t.Fatal("precondition: the checks themselves must pass for this test to mean anything")
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Errorf("level = %q, want none: the units did not describe one deployment",
			res.Quotability.Level)
	}
	if res.Certified() {
		t.Error("a run whose topology did not describe one deployment reported itself certified")
	}
}

func reportWith(admitted int) loadgen.Report {
	s := loadgen.Summary{
		Sound:     true,
		Completed: admitted,
		Totals: []loadgen.Total{{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeAdmittedSuccess,
			Replay:    false,
			Count:     admitted,
		}},
	}
	return loadgen.Report{Manifest: localManifest(), Summary: s}
}

// The arithmetic measurement-contract §12 singles out, made concrete.
//
// Unit 1 restarts mid-run: its counter resets, so its After (20) is below its Baseline (100).
// Unit 2 runs cleanly and counts 100 more. Difference each unit's own pair and unit 1's
// negative delta is caught. Sum first and difference after — (20+200) − (100+100) = 20 — and
// the restart vanishes into unit 2's increase, leaving a verdict that looks like a healthy
// 20-request run.
//
// This is why the contract says *differenced independently before the sum is taken*, and it
// is the test that fails if anyone reorders those two operations.
func TestOneUnitRestartingMidRunIsNotMaskedByAnother(t *testing.T) {
	cell := func(count int) reconcile.ServerTotals {
		return reconcile.ServerTotals{{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeAdmittedSuccess,
			Count:     count,
		}}
	}

	restarted := reconcile.AuthorityScope{
		Authority: "authority-1",
		Orgs:      []domain.OrganisationID{"org-a"},
		Querier:   fakeQuerier{row: fakeRow{c: counts{live: 10, claims: 10, records: 10}}},
		Scrapes:   reconcile.Scrapes{Baseline: cell(100), After: cell(20)},
	}
	healthy := reconcile.AuthorityScope{
		Authority: "authority-2",
		Orgs:      []domain.OrganisationID{"org-b"},
		Querier:   fakeQuerier{row: fakeRow{c: counts{live: 10, claims: 10, records: 10}}},
		Scrapes:   reconcile.Scrapes{Baseline: cell(100), After: cell(200)},
	}

	res, err := reconcile.RunTopology(context.Background(),
		[]reconcile.AuthorityScope{restarted, healthy}, reportWith(20))
	if err != nil {
		t.Fatalf("RunTopology: %v", err)
	}

	if res.ChecksOK() {
		t.Fatal("a unit that restarted mid-run produced a clean verdict: the restart was " +
			"masked by the other unit's increase, which is exactly what differencing the " +
			"sums instead of the pairs would do")
	}

	var named bool
	for _, c := range res.Aggregate {
		if !c.OK && strings.Contains(c.Detail, "authority-1") && strings.Contains(c.Detail, "backwards") {
			named = true
		}
	}
	if !named {
		t.Errorf("the failure must name the unit whose counter went backwards; got %+v", res.Aggregate)
	}
}
