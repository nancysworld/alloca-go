package metrics_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// cell is the classification the log line and the metric series must agree on: the four
// dimensions measurement-contract §4 makes a request's identity, and nothing else.
//
// Everything outside it is deliberately excluded. The log carries a request identifier and a
// duration that the metric must never label by; the metric carries a histogram the log has no
// equivalent of. Agreement is required on the shared vocabulary, not on the whole record.
type cell struct {
	operation string
	outcome   string
	reason    string
	replay    string
}

// TestTeeRecordersAgreeOnTheSemanticCell is why Tee exists.
//
// The aggregate answers "how many", the log answers "which ones" — and an anomalous cell in
// a capacity report is investigated by going to the log records behind it. That only works if
// both sides classify a request the same way. An earlier test proved every recorder is
// *called*; being called is not the property, since two recorders can each receive every
// observation and still disagree about what it was.
//
// So this drives the real SlogRecorder and the real metrics.Recorder through the real Tee,
// aggregates each side independently by (operation, outcome, reason, replay), and requires
// the two maps to be equal.
func TestTeeRecordersAgreeOnTheSemanticCell(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	reg := prometheus.NewRegistry()
	tee := metrics.Tee{
		telemetry.NewSlogRecorder(logger),
		metrics.New(reg),
	}

	// One representative of every shape a measured run produces: a committed mutation, a
	// replay of one, both refusal reasons the controlled workloads generate, the read route,
	// and a fault. Repeats differ so the comparison is of counts rather than of presence —
	// a bug that recorded one side once per cell would pass a set comparison.
	observations := []telemetry.RequestObservation{
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess, HTTPStatus: 200},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess, HTTPStatus: 200},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess, Replay: true, HTTPStatus: 200},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeBusinessRefusal,
			Reason: domain.ReasonNoCapacity, HTTPStatus: 409},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeBusinessRefusal,
			Reason: domain.ReasonNoCapacity, HTTPStatus: 409},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeBusinessRefusal,
			Reason: domain.ReasonScheduleConflict, HTTPStatus: 409},
		{Operation: string(domain.OpConfirm), Outcome: domain.OutcomeAdmittedSuccess, HTTPStatus: 200},
		{Operation: string(domain.OpCancel), Outcome: domain.OutcomeAdmittedSuccess, HTTPStatus: 200},
		{Operation: telemetry.OperationListSlots, Outcome: domain.OutcomeAdmittedSuccess, HTTPStatus: 200},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeInternalFailure, HTTPStatus: 500},
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeTimeoutServer, HTTPStatus: 503},
	}

	// A request identifier on the context, because that is how the service calls this: it
	// must reach the log and never the metric, and a comparison run without it would not
	// exercise the one asymmetry the two recorders are allowed.
	ctx := telemetry.WithRequestID(context.Background(), "req_0123456789abcdef")
	for i, obs := range observations {
		obs.Duration = time.Duration(i+1) * time.Millisecond
		tee.RecordRequest(ctx, obs)
	}

	fromLogs := cellsFromLogs(t, &logs)
	fromMetrics := cellsFromRegistry(t, reg)

	if len(fromLogs) == 0 {
		t.Fatal("no request log records parsed, so this test compared nothing")
	}
	assertSameCells(t, fromLogs, fromMetrics)

	// Guard the comparison itself: both sides must account for every observation, or two
	// equally incomplete maps would agree and prove nothing.
	if total := totalOf(fromLogs); total != len(observations) {
		t.Errorf("log cells sum to %d, want %d observations", total, len(observations))
	}
	if total := totalOf(fromMetrics); total != len(observations) {
		t.Errorf("metric cells sum to %d, want %d observations", total, len(observations))
	}
}

// TestLogsCarryTheRequestIDThatMetricsMustNot pins the one asymmetry the test above allows,
// so "they agree" cannot be achieved by making the log as label-poor as the metric.
func TestLogsCarryTheRequestIDThatMetricsMustNot(t *testing.T) {
	var logs bytes.Buffer
	reg := prometheus.NewRegistry()
	tee := metrics.Tee{
		telemetry.NewSlogRecorder(slog.New(slog.NewJSONHandler(&logs, nil))),
		metrics.New(reg),
	}

	const id = "req_0123456789abcdef"
	tee.RecordRequest(telemetry.WithRequestID(context.Background(), id),
		telemetry.RequestObservation{
			Operation:  string(domain.OpReserve),
			Outcome:    domain.OutcomeAdmittedSuccess,
			HTTPStatus: 200,
		})

	if !bytes.Contains(logs.Bytes(), []byte(id)) {
		t.Error("the log record dropped the request identifier, which is the field that " +
			"makes an anomalous metric cell investigable")
	}
	if got := gather(t, reg); bytes.Contains([]byte(got), []byte(id)) {
		t.Errorf("the request identifier reached a metric label:\n%s", got)
	}
}

// TestInvalidVocabularyDivergesDeliberately marks the boundary of the agreement above.
//
// The consistency guarantee is scoped to the *valid* vocabulary, and this is the case where
// the two sides are supposed to differ: the log keeps the raw value because it is the
// diagnostic record, and the metric collapses it to `unknown` because an out-of-vocabulary
// string in a label is exactly how a series count becomes unbounded.
//
// Without this test, the obvious way to make the consistency test "stronger" is to have the
// metric preserve what the log preserves — which would pass, and would destroy the
// cardinality bound TestRequestIDNeverBecomesALabel exists to protect. Read the two together.
func TestInvalidVocabularyDivergesDeliberately(t *testing.T) {
	var logs bytes.Buffer
	reg := prometheus.NewRegistry()
	tee := metrics.Tee{
		telemetry.NewSlogRecorder(slog.New(slog.NewJSONHandler(&logs, nil))),
		metrics.New(reg),
	}

	const bogus = "GET /v1/slots/slot-0"
	tee.RecordRequest(context.Background(), telemetry.RequestObservation{
		Operation:  bogus,
		Outcome:    domain.Outcome("probably_fine"),
		HTTPStatus: 200,
	})

	if !bytes.Contains(logs.Bytes(), []byte(bogus)) {
		t.Error("the log collapsed an out-of-vocabulary operation, losing the diagnostic " +
			"detail that makes the unknown series investigable")
	}

	fromMetrics := cellsFromRegistry(t, reg)
	want := cell{operation: metrics.LabelUnknown, outcome: metrics.LabelUnknown, replay: "false"}
	if got := fromMetrics[want]; got != 1 {
		t.Errorf("metrics did not collapse the invalid cell to %q: %+v",
			metrics.LabelUnknown, fromMetrics)
	}
	// Still counted, not dropped: a dropped observation stops the totals reconciling, and
	// §12 makes an unreconciled run unquotable.
	if total := totalOf(fromMetrics); total != 1 {
		t.Errorf("invalid observation was dropped rather than collapsed: %d cells counted", total)
	}
}

// cellsFromLogs parses the emitted JSON records and counts them by semantic cell.
//
// It reads the wire format rather than instrumenting SlogRecorder, because the log records
// are what an operator actually queries — a comparison against an in-process hook would pass
// while the serialised form disagreed.
func cellsFromLogs(t *testing.T, logs *bytes.Buffer) map[cell]int {
	t.Helper()
	counts := map[cell]int{}

	sc := bufio.NewScanner(bytes.NewReader(logs.Bytes()))
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec struct {
			Msg       string `json:"msg"`
			Operation string `json:"operation"`
			Outcome   string `json:"outcome"`
			Reason    string `json:"reason"`
			Replay    bool   `json:"replay"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log record is not JSON: %v\n%s", err, line)
		}
		// Only request observations participate; the expiry record has its own shape.
		if rec.Msg != "request" {
			continue
		}
		counts[cell{rec.Operation, rec.Outcome, rec.Reason, boolLabel(rec.Replay)}]++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading log output: %v", err)
	}
	return counts
}

// cellsFromRegistry reads alloca_requests_total out of a private registry and counts it by
// the same cell, so the two sides are built by independent code paths.
func cellsFromRegistry(t *testing.T, g prometheus.Gatherer) map[cell]int {
	t.Helper()
	counts := map[cell]int{}

	families, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != metrics.Namespace+"_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var c cell
			for _, l := range m.GetLabel() {
				switch l.GetName() {
				case "operation":
					c.operation = l.GetValue()
				case "outcome":
					c.outcome = l.GetValue()
				case "reason":
					c.reason = l.GetValue()
				case "replay":
					c.replay = l.GetValue()
				}
			}
			counts[c] += int(m.GetCounter().GetValue())
		}
	}
	return counts
}

// assertSameCells reports every difference rather than the first, since a classification
// mismatch usually affects a family of cells and one of them names the cause poorly.
func assertSameCells(t *testing.T, fromLogs, fromMetrics map[cell]int) {
	t.Helper()

	for c, want := range fromLogs {
		if got := fromMetrics[c]; got != want {
			t.Errorf("cell %+v: logs say %d, metrics say %d", c, want, got)
		}
	}
	for c, got := range fromMetrics {
		if _, ok := fromLogs[c]; !ok {
			t.Errorf("cell %+v: metrics say %d, logs record it not at all", c, got)
		}
	}
}

func totalOf(counts map[cell]int) int {
	n := 0
	for _, v := range counts {
		n += v
	}
	return n
}

// boolLabel mirrors the metrics package's own rendering, so the two sides are keyed
// identically. Duplicated rather than exported: making it public would widen the package's
// API for a test's convenience, and the value is two constants.
func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
