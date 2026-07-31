// Package metrics is the aggregated telemetry.Recorder AG-Sept measures with.
//
// It is an adapter leaf (project-structure §4), wired in by cmd and never consulted to
// make a decision. It exists because AG-M1's log-based recorder answers "what happened to
// this request", and a capacity experiment asks "what happened to ten million of them" —
// a question no log aggregation on the request path can answer cheaply.
//
// # Label discipline
//
// Two things could give this package an unbounded series count, and neither is prevented
// by the types alone.
//
// First, the context. telemetry.Recorder is handed the ctx, and ctx carries RequestID,
// which is unbounded by construction. The rule is that **labels are never derived from
// request context**; the methods below take `_ context.Context` so the compiler keeps it.
//
// Second, the observation's own strings. Operation is a plain string and Outcome and
// Reason are string-backed, so `Outcome("GET /slots/1")` compiles and telemetry's closed-set
// rule for them is documentation, not enforcement. A caller's discipline is the wrong place
// to hold a guarantee that protects publishable runs, so this package does not rely on it:
// every label is normalised through the closed-set predicates that own each vocabulary —
// telemetry.IsKnownOperation, domain.Outcome.IsKnown, domain.Reason.IsKnown — and anything
// outside collapses to LabelUnknown.
//
// # Cardinality
//
// The result is a bound this package enforces rather than inherits: the request counter
// cannot exceed (operations+1) × (outcomes+1) × (reasons+2) × 2 series regardless of what
// it is handed, and the duration histogram omits reason to keep its bucket count down.
// Every factor is a constant of the contract, none of the traffic.
//
// The bound is enforced here, so it is not a claim about callers and does not weaken when
// a new caller appears.
package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/domain"
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

// LabelUnknown is the single series every out-of-vocabulary label value collapses to.
//
// Collapsing rather than dropping is deliberate: a dropped observation would make the
// totals stop reconciling, and §6.5 makes an unreconciled run unquotable. An observation
// with a label this recorder does not recognise is still a completed request, so it is
// counted — under a value that says the vocabulary was violated and where to look.
const LabelUnknown = "unknown"

// RecordRequest aggregates one completed request.
//
// ctx is unused by design — see the package comment. Deriving a label from it is one of
// the two ways this package could produce an unbounded series; the other is trusting the
// observation's own strings, which normalise below.
func (r *Recorder) RecordRequest(_ context.Context, obs telemetry.RequestObservation) {
	operation := normaliseOperation(obs.Operation)
	outcome := normaliseOutcome(obs.Outcome)

	r.requests.WithLabelValues(
		operation,
		outcome,
		normaliseReason(obs.Reason),
		boolLabel(obs.Replay),
	).Inc()

	r.duration.WithLabelValues(operation, outcome).Observe(obs.Duration.Seconds())
}

// normaliseOperation admits the observation vocabulary — the three mutations plus the
// read route — and collapses everything else.
func normaliseOperation(op string) string {
	if telemetry.IsKnownOperation(op) {
		return op
	}
	return LabelUnknown
}

// normaliseOutcome admits the closed terminal-outcome set of measurement-contract §4.
func normaliseOutcome(o domain.Outcome) string {
	if o.IsKnown() {
		return string(o)
	}
	return LabelUnknown
}

// normaliseReason admits the closed refusal-reason set, plus the empty reason that a
// non-refusal legitimately carries. Empty stays empty rather than becoming "unknown":
// "this request was not a refusal" and "this reason is not in the vocabulary" are
// different facts, and collapsing them would hide the second behind the commonest value.
func normaliseReason(reason domain.Reason) string {
	if reason == "" || reason.IsKnown() {
		return string(reason)
	}
	return LabelUnknown
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
