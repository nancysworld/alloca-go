// Package metrics is the aggregated telemetry.Recorder AG-Sept measures with.
//
// It is an adapter leaf (project-structure §4), wired in by cmd and never consulted to
// make a decision. It exists because AG-M1's log-based recorder answers "what happened to
// this request", and a capacity experiment asks "what happened to ten million of them" —
// a question no log aggregation on the request path can answer cheaply.
//
// # Label discipline
//
// telemetry's observation types are drawn from closed sets by construction, which removes
// the *accidental* unbounded label. The deliberate one is removed by a rule that package
// states and this one obeys: **labels are never derived from request context.** The ctx
// arguments below are accepted to satisfy the interface and are deliberately unused —
// ctx carries RequestID, which is unbounded and belongs in a log, never in a label
// (ag-sept-plan §6.1).
//
// Every label value here is either a closed set (operation, outcome, reason) or a boolean.
// Reason is closed by domain.Reason and empty for non-refusals, which is itself a fixed
// value rather than an open one.
//
// # Cardinality
//
// The request counter's series count is bounded by
// operations × outcomes × reasons × 2, and the duration histogram omits reason to keep its
// bucket count down. Both are constants of the domain, not of the traffic.
package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// Namespace prefixes every metric this package exports.
const Namespace = "alloca"

// Recorder implements telemetry.Recorder by aggregating observations into Prometheus
// collectors. It is safe for concurrent use: every collector it holds is.
//
// Recording is bounded work — a map lookup and an atomic add — with no I/O on the request
// path, which is what telemetry.Recorder requires of an implementation running there.
// Exposition happens on a scrape, on a different goroutine.
type Recorder struct {
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	expiryRuns  *prometheus.CounterVec
	expirySlots prometheus.Counter
	expired     prometheus.Counter
	expiryTime  prometheus.Histogram
}

// New builds a Recorder and registers its collectors with reg.
//
// It registers rather than accepting pre-registered collectors so that a duplicate
// registration is a startup panic in cmd, not a silently dropped metric discovered when a
// dashboard is empty halfway through an experiment.
func New(reg prometheus.Registerer) *Recorder {
	r := &Recorder{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "requests_total",
			Help: "Completed requests by terminal outcome. replay is the orthogonal " +
				"disposition flag (measurement-contract §4), never an outcome of its own.",
		}, []string{"operation", "outcome", "reason", "replay"}),

		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "request_duration_seconds",
			Help: "Handler duration including the transaction it drove. Excludes reason " +
				"to bound series count; the counter carries that dimension.",
			Buckets: requestBuckets,
		}, []string{"operation", "outcome"}),

		expiryRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "expiry_iterations_total",
			Help:      "Expiry worker iterations, by whether the iteration ended in an error.",
		}, []string{"failed"}),

		expirySlots: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "expiry_slots_examined_total",
			Help:      "Candidate slots examined by the expiry worker.",
		}),

		expired: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "expiry_reservations_expired_total",
			Help:      "Held reservations transitioned to expired by the worker.",
		}),

		expiryTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "expiry_iteration_duration_seconds",
			Help:      "Duration of one expiry worker iteration.",
			Buckets:   expiryBuckets,
		}),
	}

	reg.MustRegister(
		r.requests, r.duration,
		r.expiryRuns, r.expirySlots, r.expired, r.expiryTime,
	)
	return r
}

// requestBuckets span sub-millisecond to ten seconds. The upper end matters: a request
// that rides its whole timeout budget is the observation an overload experiment is looking
// for, and a histogram that tops out below the budget reports it as +Inf and loses the
// shape exactly where it is most informative.
var requestBuckets = []float64{
	0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// expiryBuckets are coarser and reach further: the worker is a background sweep, so its
// interesting failure is a long iteration rather than a slow one.
var expiryBuckets = []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 60}

// RecordRequest aggregates one completed request.
//
// ctx is unused by design — see the package comment. Deriving a label from it is the one
// way this package could produce an unbounded series.
func (r *Recorder) RecordRequest(_ context.Context, obs telemetry.RequestObservation) {
	outcome := string(obs.Outcome)
	r.requests.WithLabelValues(
		obs.Operation,
		outcome,
		string(obs.Reason),
		boolLabel(obs.Replay),
	).Inc()

	r.duration.WithLabelValues(obs.Operation, outcome).Observe(obs.Duration.Seconds())
}

// RecordExpiry aggregates one expiry worker iteration.
func (r *Recorder) RecordExpiry(_ context.Context, obs telemetry.ExpiryObservation) {
	r.expiryRuns.WithLabelValues(boolLabel(obs.Failed)).Inc()
	r.expirySlots.Add(float64(obs.Slots))
	r.expired.Add(float64(obs.Expired))
	r.expiryTime.Observe(obs.Duration.Seconds())
}

// boolLabel renders a bool as a stable label value. strconv.FormatBool would do, but
// naming it keeps the exposed vocabulary in one place.
func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Tee fans one observation out to several recorders in order, so a run can carry both the
// per-request log line AG-M1 emits and the aggregate series AG-Sept measures.
//
// It is sequential and synchronous: an observation is not recorded until every recorder
// has seen it. That keeps the request path's cost the sum of its recorders rather than
// hiding it behind a goroutine whose queue is the next thing to overflow — the same
// reasoning telemetry.SlogRecorder gives for staying synchronous.
type Tee []telemetry.Recorder

func (t Tee) RecordRequest(ctx context.Context, obs telemetry.RequestObservation) {
	for _, r := range t {
		r.RecordRequest(ctx, obs)
	}
}

func (t Tee) RecordExpiry(ctx context.Context, obs telemetry.ExpiryObservation) {
	for _, r := range t {
		r.RecordExpiry(ctx, obs)
	}
}

// Interface assertions.
var (
	_ telemetry.Recorder = (*Recorder)(nil)
	_ telemetry.Recorder = Tee(nil)
)
