package reconcile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/reconcile"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// exposition scrapes reg through the same promhttp handler the service serves on :9090, so
// the parser is tested against the bytes an operator's curl actually receives rather than
// against a string this test invented.
func exposition(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("scrape returned %d", w.Code)
	}
	return w.Body.String()
}

// scrape is a trimmed exposition holding the two cells a hot-slot run produces, with the
// runtime collectors and a HELP/TYPE header around them so the parser is exercised against
// the shape it actually meets rather than a bare pair of lines.
const scrape = `# HELP alloca_requests_total Completed requests by terminal outcome.
# TYPE alloca_requests_total counter
alloca_requests_total{operation="reserve",outcome="admitted_success",reason="",replay="false"} 5
alloca_requests_total{operation="reserve",outcome="business_refusal",reason="no_capacity",replay="false"} 55
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 19
alloca_request_duration_seconds_count{operation="reserve",outcome="admitted_success"} 5
`

func hotSlotSummary() loadgen.Summary {
	return loadgen.Summary{
		Totals: []loadgen.Total{
			{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Count: 5},
			{
				Operation: "reserve", Outcome: domain.OutcomeBusinessRefusal,
				Reason: domain.ReasonNoCapacity, Count: 55,
			},
		},
		Completed: 60, Sound: true, ValidationEnabled: true,
	}
}

func TestParsesOnlyTheRequestCounter(t *testing.T) {
	got, err := reconcile.ParseServerTotals(strings.NewReader(scrape))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d cells, want 2: %+v", len(got), got)
	}
	if got.Sum() != 60 {
		t.Errorf("sum = %d, want 60", got.Sum())
	}
	if got[1].Reason != domain.ReasonNoCapacity || got[1].Count != 55 {
		t.Errorf("second cell = %+v, want no_capacity/55", got[1])
	}
	if got[0].Replay {
		t.Error("replay parsed as true from replay=\"false\"")
	}
}

// TestParsesWhatTheRecorderEmits is the drift guard. The parser hardcodes a metric name and
// a label set; this renders the real recorder through the real Prometheus encoder and parses
// that, so a rename or a label change in internal/metrics fails here rather than producing an
// empty scrape that reconciles against nothing and looks like a clean run.
func TestParsesWhatTheRecorderEmits(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)
	ctx := context.Background()
	for range 3 {
		rec.RecordRequest(ctx, telemetry.RequestObservation{
			Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess,
			HTTPStatus: 200,
		})
	}
	rec.RecordRequest(ctx, telemetry.RequestObservation{
		Operation: string(domain.OpReserve), Outcome: domain.OutcomeBusinessRefusal,
		Reason: domain.ReasonNoCapacity, HTTPStatus: 200, Replay: true,
	})

	got, err := reconcile.ParseServerTotals(strings.NewReader(exposition(t, reg)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Sum() != 4 {
		t.Fatalf("sum = %d, want 4 — the parser is not reading the counter the recorder "+
			"emits: %+v", got.Sum(), got)
	}
	var replayed int
	for _, c := range got {
		if c.Replay {
			replayed += c.Count
		}
	}
	if replayed != 1 {
		t.Errorf("replay cells total %d, want 1", replayed)
	}
}

func TestAgreeingTotalsAreQuotable(t *testing.T) {
	s := hotSlotSummary()
	server, err := reconcile.ParseServerTotals(strings.NewReader(scrape))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := reconcile.RunServerCheckForTest(s, server)
	if !res.OK {
		t.Fatalf("agreeing totals rejected: %s", res.Detail)
	}
}

// TestMissingScrapeIsNotQuotable is the reason the parameter is not optional: two of three
// counts agreeing is a weaker gate wearing the stronger gate's name.
func TestMissingScrapeIsNotQuotable(t *testing.T) {
	res := reconcile.RunServerCheckForTest(hotSlotSummary(), nil)
	if res.OK {
		t.Fatal("a run with no metrics scrape was certified")
	}
	if !strings.Contains(res.Detail, "no metrics scrape") {
		t.Errorf("detail does not name the missing scrape: %q", res.Detail)
	}
}

// TestDisagreementsAreCaught covers the failures this check exists for: observations
// dropped, duplicated, or filed under the wrong outcome. Each mutates one side only, so a
// pass would mean the comparison is not comparing.
func TestDisagreementsAreCaught(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(reconcile.ServerTotals) reconcile.ServerTotals
		expect string
	}{
		{
			name: "server dropped an observation",
			mutate: func(s reconcile.ServerTotals) reconcile.ServerTotals {
				s[0].Count--
				return s
			},
			expect: "disagree about how much work happened",
		},
		{
			name: "server double-counted",
			mutate: func(s reconcile.ServerTotals) reconcile.ServerTotals {
				s[0].Count *= 2
				return s
			},
			expect: "disagree about how much work happened",
		},
		{
			name: "server misclassified the outcome, totals still match",
			mutate: func(s reconcile.ServerTotals) reconcile.ServerTotals {
				s[0].Count--
				s[1].Count++
				return s
			},
			expect: "reported 5 by the client and 4 by the server",
		},
		{
			// The refusals the client saw as no_capacity are split by the server across
			// two reasons. The sum still matches, so only the per-cell comparison can
			// catch it — which is the reason this check compares cells at all rather than
			// just totals.
			name: "server split one refusal reason into two, totals still match",
			mutate: func(s reconcile.ServerTotals) reconcile.ServerTotals {
				s[1].Count -= 10
				return append(s, loadgen.Total{
					Operation: "reserve", Outcome: domain.OutcomeBusinessRefusal,
					Reason: domain.ReasonScheduleConflict, Count: 10,
				})
			},
			expect: "reported 55 by the client and 45 by the server",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := reconcile.ParseServerTotals(strings.NewReader(scrape))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			res := reconcile.RunServerCheckForTest(hotSlotSummary(), tc.mutate(server))
			if res.OK {
				t.Fatalf("disagreement not caught; detail: %s", res.Detail)
			}
			if !strings.Contains(res.Detail, tc.expect) {
				t.Errorf("detail = %q, want it to contain %q", res.Detail, tc.expect)
			}
		})
	}
}

// TestWarmUpDiscardsAreAccountedFor covers the one legitimate asymmetry: the client drops
// warm-up responses from its totals, but the service completed and counted those requests.
func TestWarmUpDiscardsAreAccountedFor(t *testing.T) {
	s := hotSlotSummary()
	s.Completed = 50
	s.WarmUpDiscarded = 10 // the server still counted all 60

	server, err := reconcile.ParseServerTotals(strings.NewReader(scrape))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := reconcile.RunServerCheckForTest(s, server)
	if !res.OK {
		t.Fatalf("warm-up run rejected: %s", res.Detail)
	}
	if !strings.Contains(res.Detail, "per-cell comparison skipped") {
		t.Errorf("detail does not say per-cell comparison was skipped: %q", res.Detail)
	}

	// The sum must still be enforced, or the warm-up path becomes a way to pass anything.
	s.WarmUpDiscarded = 3
	if res := reconcile.RunServerCheckForTest(s, server); res.OK {
		t.Error("a warm-up run whose totals do not add up was certified")
	}
}

func TestMalformedScrapeIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unterminated labels", `alloca_requests_total{operation="reserve" 5`},
		{"fractional count", `alloca_requests_total{operation="reserve"} 5.5`},
		{"non-numeric count", `alloca_requests_total{operation="reserve"} five`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := reconcile.ParseServerTotals(strings.NewReader(tc.body)); err == nil {
				t.Error("malformed scrape parsed without error")
			}
		})
	}
}

// TestEmptyScrapeIsNotAMissingScrape separates "the service served nothing" from "nobody
// supplied a scrape". Collapsing them would let a run with no scrape look like a run against
// an idle service.
func TestEmptyScrapeIsNotAMissingScrape(t *testing.T) {
	got, err := reconcile.ParseServerTotals(strings.NewReader("# nothing here\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got == nil {
		t.Fatal("an empty scrape parsed to nil, which means 'not supplied'")
	}
	if res := reconcile.RunServerCheckForTest(hotSlotSummary(), got); res.OK {
		t.Error("60 client requests against an empty scrape was certified")
	}
}
