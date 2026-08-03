package metrics_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// TestRequestIDNeverBecomesALabel is the discriminating test for this package's one real
// risk. telemetry hands the recorder the context precisely so a recorder *could* read it,
// and ctx carries RequestID, which is unbounded by construction.
//
// A thousand requests differing only by request identifier must produce exactly one
// series. Add request_id (or anything else ctx-derived) as a label and this fails with a
// thousand — which is the failure an experiment would otherwise discover as a dead
// Prometheus, hours into a run.
func TestRequestIDNeverBecomesALabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	const requests = 1000
	for i := range requests {
		ctx := telemetry.WithRequestID(context.Background(), "req-"+strconv.Itoa(i))
		rec.RecordRequest(ctx, telemetry.RequestObservation{
			Operation:  string(domain.OpReserve),
			Outcome:    domain.OutcomeAdmittedSuccess,
			HTTPStatus: 201,
			Duration:   5 * time.Millisecond,
		})
	}

	if got := countSeries(t, reg, "alloca_requests_total"); got != 1 {
		t.Fatalf("1000 requests differing only by request ID produced %d series, want 1: "+
			"a label is being derived from request context", got)
	}
	if got := counterValue(t, reg, "alloca_requests_total"); got != requests {
		t.Fatalf("counter = %v, want %d", got, requests)
	}
}

// TestOutOfVocabularyLabelsCollapseToUnknown is the discriminating test for the label
// firewall. telemetry documents operation/outcome/reason as closed sets, but Operation is
// a plain string and Outcome and Reason are string-backed, so a caller can hand this
// recorder a URL path or an error string and the compiler will not object.
//
// Three hundred observations, each with a distinct invalid value in all three positions,
// must produce exactly one series. Remove any of the three normalise* calls and this fails
// with 300 — which is what a benchmark substrate must not do to a Prometheus mid-run.
func TestOutOfVocabularyLabelsCollapseToUnknown(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	const bogus = 300
	for i := range bogus {
		n := strconv.Itoa(i)
		rec.RecordRequest(context.Background(), telemetry.RequestObservation{
			Operation: "GET /slots/" + n,                      // a path, not an operation
			Outcome:   domain.Outcome("boom-" + n),            // not in the closed set
			Reason:    domain.Reason("connection reset " + n), // error text, not a reason
			Duration:  time.Millisecond,
		})
	}

	if got := countSeries(t, reg, "alloca_requests_total"); got != 1 {
		t.Fatalf("%d observations with distinct invalid labels produced %d series, want 1: "+
			"the label firewall is not normalising", bogus, got)
	}

	body := gather(t, reg)
	for _, want := range []string{
		`operation="` + metrics.LabelUnknown + `"`,
		`outcome="` + metrics.LabelUnknown + `"`,
		`reason="` + metrics.LabelUnknown + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition missing %s\n%s", want, body)
		}
	}

	// Collapsed, not dropped: an unreconciled total makes a run unquotable (§6.5).
	if got := counterValue(t, reg, "alloca_requests_total"); got != bogus {
		t.Fatalf("counter = %v, want %d: observations were dropped rather than collapsed",
			got, bogus)
	}
}

// TestKnownVocabularySurvivesNormalisation guards the other direction: a firewall that
// collapsed everything would pass the test above while destroying the measurement.
func TestKnownVocabularySurvivesNormalisation(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	for _, op := range []string{
		string(domain.OpReserve), string(domain.OpConfirm), string(domain.OpCancel),
		telemetry.OperationListSlots,
	} {
		rec.RecordRequest(context.Background(), telemetry.RequestObservation{
			Operation: op,
			Outcome:   domain.OutcomeAdmittedSuccess,
			Duration:  time.Millisecond,
		})
	}

	body := gather(t, reg)
	for _, op := range []string{"reserve", "confirm", "cancel", "list_slots"} {
		if !strings.Contains(body, `operation="`+op+`"`) {
			t.Errorf("known operation %q was collapsed\n%s", op, body)
		}
	}
	if strings.Contains(body, metrics.LabelUnknown) {
		t.Errorf("known vocabulary produced an %q series\n%s", metrics.LabelUnknown, body)
	}
}

// TestNonRefusalReasonStaysEmpty pins the distinction normaliseReason exists to keep:
// "not a refusal" is the empty reason, not the unknown one.
func TestNonRefusalReasonStaysEmpty(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	rec.RecordRequest(context.Background(), telemetry.RequestObservation{
		Operation: string(domain.OpReserve),
		Outcome:   domain.OutcomeAdmittedSuccess,
		Duration:  time.Millisecond,
	})

	body := gather(t, reg)
	if !strings.Contains(body, `reason=""`) {
		t.Errorf("non-refusal reason should stay empty\n%s", body)
	}
	if strings.Contains(body, `reason="`+metrics.LabelUnknown+`"`) {
		t.Errorf("non-refusal reason collapsed to %q, hiding real violations\n%s",
			metrics.LabelUnknown, body)
	}
}

// TestReplayIsALabelNotAnOutcome pins measurement-contract §4: a replay carries the
// originally recorded terminal outcome and is distinguished by an orthogonal flag. If
// replay were folded into the outcome, both requests below would land on different
// outcomes and the admitted_success total would be 1 rather than 2.
func TestReplayIsALabelNotAnOutcome(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	for _, replay := range []bool{false, true} {
		rec.RecordRequest(context.Background(), telemetry.RequestObservation{
			Operation:  string(domain.OpReserve),
			Outcome:    domain.OutcomeAdmittedSuccess,
			Replay:     replay,
			HTTPStatus: 201,
			Duration:   time.Millisecond,
		})
	}

	body := gather(t, reg)
	for _, want := range []string{
		`outcome="admitted_success"`,
		`replay="false"`,
		`replay="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition missing %s\n%s", want, body)
		}
	}
	if got := countSeries(t, reg, "alloca_requests_total"); got != 2 {
		t.Fatalf("got %d series, want 2 (one per replay disposition)", got)
	}
}

// TestRefusalReasonIsRecorded checks the closed-set reason dimension survives, since
// business-refusal counts by reason are what separate a sold-out run from a broken one.
func TestRefusalReasonIsRecorded(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	rec.RecordRequest(context.Background(), telemetry.RequestObservation{
		Operation:  string(domain.OpReserve),
		Outcome:    domain.OutcomeBusinessRefusal,
		Reason:     domain.ReasonNoCapacity,
		HTTPStatus: 409,
		Duration:   time.Millisecond,
	})

	body := gather(t, reg)
	if !strings.Contains(body, `reason="`+string(domain.ReasonNoCapacity)+`"`) {
		t.Fatalf("refusal reason not exposed\n%s", body)
	}
}

// TestExpiryObservationsAggregate covers the worker's counters, including that a failed
// iteration is still counted as an iteration rather than dropped.
func TestExpiryObservationsAggregate(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec := metrics.New(reg)

	rec.RecordExpiry(context.Background(), telemetry.ExpiryObservation{
		Slots: 7, Expired: 3, Duration: 20 * time.Millisecond,
	})
	rec.RecordExpiry(context.Background(), telemetry.ExpiryObservation{
		Slots: 2, Expired: 0, Failed: true, Duration: 5 * time.Millisecond,
	})

	body := gather(t, reg)
	for _, want := range []string{
		"alloca_expiry_slots_examined_total 9",
		"alloca_expiry_reservations_expired_total 3",
		`alloca_expiry_iterations_total{failed="true"} 1`,
		`alloca_expiry_iterations_total{failed="false"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition missing %q\n%s", want, body)
		}
	}
}

// TestTeeFansOutToEveryRecorder covers the request and expiry paths together, since a Tee
// that forwarded only one of them would still satisfy the interface.
func TestTeeFansOutToEveryRecorder(t *testing.T) {
	a, b := &countingRecorder{}, &countingRecorder{}
	tee := metrics.Tee{a, b}

	tee.RecordRequest(context.Background(), telemetry.RequestObservation{})
	tee.RecordExpiry(context.Background(), telemetry.ExpiryObservation{})

	for i, r := range []*countingRecorder{a, b} {
		if r.requests != 1 || r.expiries != 1 {
			t.Errorf("recorder %d saw requests=%d expiries=%d, want 1 and 1",
				i, r.requests, r.expiries)
		}
	}
}

type countingRecorder struct{ requests, expiries int }

func (c *countingRecorder) RecordRequest(context.Context, telemetry.RequestObservation) {
	c.requests++
}
func (c *countingRecorder) RecordExpiry(context.Context, telemetry.ExpiryObservation) {
	c.expiries++
}

// gather renders the registry in the Prometheus text format the scrape would see, so the
// assertions above test the exposed surface rather than internal state.
func gather(t *testing.T, g prometheus.Gatherer) string {
	t.Helper()
	var sb strings.Builder
	families, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			sb.WriteString(mf.GetName())
			if labels := m.GetLabel(); len(labels) > 0 {
				sb.WriteString("{")
				for i, l := range labels {
					if i > 0 {
						sb.WriteString(",")
					}
					sb.WriteString(l.GetName())
					sb.WriteString(`="`)
					sb.WriteString(l.GetValue())
					sb.WriteString(`"`)
				}
				sb.WriteString("}")
			}
			switch {
			case m.GetCounter() != nil:
				sb.WriteString(" ")
				sb.WriteString(trimFloat(m.GetCounter().GetValue()))
			case m.GetHistogram() != nil:
				sb.WriteString(" ")
				sb.WriteString(strconv.FormatUint(m.GetHistogram().GetSampleCount(), 10))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// countSeries reports how many label combinations exist for one metric family — the
// cardinality number the discriminating test asserts on.
func countSeries(t *testing.T, g prometheus.Gatherer, name string) int {
	t.Helper()
	families, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() == name {
			return len(mf.GetMetric())
		}
	}
	t.Fatalf("metric family %q not found", name)
	return 0
}

// counterValue sums a counter family across its series, read back through the exposition
// path rather than from a struct field.
func counterValue(t *testing.T, g prometheus.Gatherer, name string) float64 {
	t.Helper()
	families, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		var total float64
		for _, m := range mf.GetMetric() {
			total += m.GetCounter().GetValue()
		}
		return total
	}
	t.Fatalf("metric family %q not found", name)
	return 0
}
