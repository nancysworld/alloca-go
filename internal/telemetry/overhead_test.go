package telemetry_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// These benchmarks discharge ag-sept-plan §6.2: before any performance claim, AG-Sept must
// show that observation is not the primary request bottleneck.
//
// The decision recorded in the PR1 scope note is to measure first and build the bounded
// asynchronous sink only if the measurement says it is needed — so this is the measurement
// that decides it, not a formality. observability.md §5.1 deferred the async sink on the
// argument that "one line to a local stderr, at AG-M1 load, is not a plausible stall";
// these benchmarks turn that argument into a number.
//
// Run with:
//
//	go test ./internal/telemetry/ -bench Recorder -benchmem -run '^$'
//
// The comparison that matters is BenchmarkRecorderNop against the others: the difference is
// what an observation costs the request path, and the request path's own budget is
// milliseconds. A cost measured in hundreds of nanoseconds is not the frontier.

func observation() telemetry.RequestObservation {
	return telemetry.RequestObservation{
		Operation:  string(domain.OpReserve),
		Outcome:    domain.OutcomeAdmittedSuccess,
		HTTPStatus: 200,
		Duration:   3 * time.Millisecond,
	}
}

// BenchmarkRecorderNop is the floor: the cost of the call itself with no recorder work.
func BenchmarkRecorderNop(b *testing.B) {
	rec := telemetry.Nop{}
	ctx := context.Background()
	obs := observation()
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordRequest(ctx, obs)
	}
}

// BenchmarkRecorderSlogBoundedSink measures AG-M1's structured line written to a bounded
// local sink — the treatment PR1 adopted. io.Discard stands in for a sink that never
// blocks, which isolates formatting and allocation cost from sink backpressure.
//
// Backpressure is the risk observability.md §5.1 actually names, and it is deliberately not
// measured here: a benchmark cannot show what a stalled reader costs, only what a healthy
// one does. That is why the plan requires the async sink *if the measurement shows it
// matters* rather than treating this number as the whole answer.
func BenchmarkRecorderSlogBoundedSink(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	rec := telemetry.NewSlogRecorder(logger)
	ctx := context.Background()
	obs := observation()
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordRequest(ctx, obs)
	}
}

// BenchmarkRecorderPrometheus measures the aggregate recorder alone.
func BenchmarkRecorderPrometheus(b *testing.B) {
	rec := metrics.New(prometheus.NewRegistry())
	ctx := context.Background()
	obs := observation()
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordRequest(ctx, obs)
	}
}

// BenchmarkRecorderTee measures what the service actually runs: both recorders fed from one
// observation. This is the number §6.2 is asking about.
func BenchmarkRecorderTee(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	rec := metrics.Tee{
		telemetry.NewSlogRecorder(logger),
		metrics.New(prometheus.NewRegistry()),
	}
	ctx := context.Background()
	obs := observation()
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordRequest(ctx, obs)
	}
}

// BenchmarkRecorderTeeParallel runs the same fan-out concurrently, because a recorder that
// is cheap serially and contended in parallel would be invisible above. Prometheus counters
// take a per-series lock, and a hot-slot workload drives every request onto one series —
// so the contended case is the realistic one, not the pessimistic one.
func BenchmarkRecorderTeeParallel(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	rec := metrics.Tee{
		telemetry.NewSlogRecorder(logger),
		metrics.New(prometheus.NewRegistry()),
	}
	obs := observation()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			rec.RecordRequest(ctx, obs)
		}
	})
}
