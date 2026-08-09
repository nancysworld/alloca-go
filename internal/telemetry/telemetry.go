// Package telemetry defines the semantic observations Alloca-Go emits about its own
// behaviour, and the one AG-M1 implementation of them: structured slog output.
//
// It is an adapter leaf (project-structure §4), wired in by cmd and never consulted to
// make a decision — an observation is a report, never an input to domain behaviour.
// Callers hand Recorder a value describing *what happened* rather than a formatted
// message, so adding Prometheus or OpenTelemetry is one new Recorder and a line in cmd.
//
// Every field on the two observation types is drawn from a closed set or is a
// measurement: observation values exclude high-cardinality diagnostic fields, so the
// obvious way to build a metrics recorder cannot produce an unbounded label set.
//
// That is a strong default, not a guarantee. A recorder is handed the context as well as
// the value, and the context carries RequestID — so the rule metrics implementations must
// follow is: **do not derive labels from request context.** The observation shape removes
// the accident; only that discipline removes the deliberate case.
package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// RequestObservation is the semantic record of one completed request.
//
// It is emitted even when the response never reached the caller: a client that
// disconnects mid-request is still classified (timeout_client) and still observed, since
// every completed request must carry exactly one terminal outcome for the totals to
// reconcile (measurement-contract §4).
type RequestObservation struct {
	// Operation names the request kind, never the URL path — which is unbounded once
	// identifiers appear in it.
	//
	// A plain string rather than a domain.Operation because domain.Operation is the set
	// of *mutations* the idempotency record accepts, and the read route is not one. The
	// values must stay a closed set of their own: use OperationListSlots or
	// string(domain.OpReserve) and its siblings, never anything caller-supplied.
	Operation string
	// Outcome is the single terminal classification (measurement-contract §4).
	Outcome domain.Outcome
	// Reason is the refusal's stable code, empty for non-refusals.
	Reason domain.Reason
	// Replay reports whether the outcome was served from a recorded idempotency record.
	// It is the orthogonal half of the classification, never an Outcome of its own.
	Replay bool
	// HTTPStatus is the status the handler wrote, or intended to write when the caller
	// had already gone away.
	HTTPStatus int
	// Duration measures the handler, including the transaction it drove.
	Duration time.Duration
}

// ExpiryObservation is the semantic record of one expiry worker iteration. Counts, not
// identifiers: which slots were settled is a question for the database, not a time series.
type ExpiryObservation struct {
	// Slots is how many candidate slots the iteration examined.
	Slots int
	// Expired is how many held reservations were transitioned to expired across them.
	Expired int
	// Failed reports whether the iteration ended early in an error. The error itself is
	// diagnostic context for a log, never a dimension.
	Failed bool
	// Duration measures the whole iteration.
	Duration time.Duration
}

// OperationListSlots names the read route in an observation. The mutation operations
// take their names from domain.Operation, so there is nothing to redeclare for them.
const OperationListSlots = "list_slots"

// IsKnownOperation reports whether op is a member of the observation vocabulary: the
// three domain mutations plus the read route.
//
// Operation is a plain string on RequestObservation, so a caller can pass a URL path.
// That is a documented rule rather than a compiler-enforced one, and a rule protecting
// an aggregate needs something that can *check* it — an aggregating Recorder normalises
// through this rather than trusting what it is handed (observability.md §2).
func IsKnownOperation(op string) bool {
	return op == OperationListSlots || domain.Operation(op).IsKnown()
}

// Recorder consumes observations. Implementations must be safe for concurrent use.
//
// They execute **on the request path** and must remain bounded; remote export must not
// occur directly there. "Must not block" would be a contract no implementation can
// honour — even an slog.Handler writing to a pipe can block if the reader stalls — so the
// obligation is stated as boundedness, and SlogRecorder documents the backpressure it
// does not remove.
type Recorder interface {
	RecordRequest(ctx context.Context, obs RequestObservation)
	RecordExpiry(ctx context.Context, obs ExpiryObservation)
}

// SlogRecorder is the AG-M1 Recorder: one structured log line per observation. The
// attribute keys are the label names a metrics implementation would use, so a query
// written against these logs translates directly.
//
// Emission is synchronous. slog invokes its handler on the calling goroutine, so if the
// log sink stalls — a full pipe, a stopped reader on stderr — the handler is held *after*
// its transaction has already committed. Correctness is unaffected: the mutation is
// durable and the idempotency record makes the client's retry safe. Two things are
// affected, and both matter later rather than now:
//
//   - the client may time out and retry a request that in fact succeeded, which the
//     idempotency key resolves but which shows up in the outcome mix;
//   - measured latency includes log backpressure, so a slow sink perturbs the very
//     numbers AG-M2 exists to collect.
//
// AG-M1 accepts this: one line to a local stderr, at AG-M1 load, is not a plausible
// stall. A bounded asynchronous sink (drop-on-full, flushed at shutdown) is the fix if
// AG-M2 measurement or a remote sink makes it real, and belongs with the work that needs
// it rather than ahead of it.
type SlogRecorder struct {
	logger *slog.Logger
}

// NewSlogRecorder builds a Recorder over logger. A nil logger uses slog.Default, so a
// caller that has not configured logging still gets observations rather than silence.
func NewSlogRecorder(logger *slog.Logger) *SlogRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogRecorder{logger: logger}
}

// RecordRequest emits the request observation at info level. Duration is reported in
// milliseconds as a float so sub-millisecond requests do not all collapse to zero.
func (r *SlogRecorder) RecordRequest(ctx context.Context, obs RequestObservation) {
	r.logger.LogAttrs(ctx, slog.LevelInfo, "request",
		slog.String("operation", obs.Operation),
		slog.String("outcome", string(obs.Outcome)),
		slog.String("reason", string(obs.Reason)),
		slog.Bool("replay", obs.Replay),
		slog.Int("http_status", obs.HTTPStatus),
		slog.Float64("duration_ms", msFloat(obs.Duration)),
		requestIDAttr(ctx),
	)
}

// RecordExpiry emits the worker observation. A failed iteration is still info-level
// here: the worker logs the error itself separately with its diagnostic context, and
// duplicating it at error level would double-count the same event in a log-based alert.
func (r *SlogRecorder) RecordExpiry(ctx context.Context, obs ExpiryObservation) {
	r.logger.LogAttrs(ctx, slog.LevelInfo, "expiry_iteration",
		slog.Int("slots", obs.Slots),
		slog.Int("expired", obs.Expired),
		slog.Bool("failed", obs.Failed),
		slog.Float64("duration_ms", msFloat(obs.Duration)),
	)
}

func msFloat(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Nop is a Recorder that discards observations, for tests and for callers that have not
// wired telemetry.
type Nop struct{}

func (Nop) RecordRequest(context.Context, RequestObservation) {}
func (Nop) RecordExpiry(context.Context, ExpiryObservation)   {}

// requestIDKey types the context key so it cannot collide with another package's.
type requestIDKey struct{}

// WithRequestID attaches a request identifier for diagnostics. Context-carried rather
// than a field on RequestObservation on purpose: it is high-cardinality by construction,
// so it must be reachable by a log and unreachable by a metric label.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the identifier attached by WithRequestID, or "" if there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestIDAttr renders the request identifier, or an empty group when absent so the
// attribute simply does not appear rather than logging an empty string.
func requestIDAttr(ctx context.Context) slog.Attr {
	if id := RequestID(ctx); id != "" {
		return slog.String("request_id", id)
	}
	return slog.Attr{}
}

// Static assertions that both implementations satisfy the port.
var (
	_ Recorder = (*SlogRecorder)(nil)
	_ Recorder = Nop{}
)
