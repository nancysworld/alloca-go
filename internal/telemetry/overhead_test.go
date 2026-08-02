package telemetry_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// These benchmarks price one observation, which is what ag-sept-plan §6.2's *decision*
// turns on: measure first, and build the bounded asynchronous sink only if the measurement
// says it is needed. observability.md §5.1 deferred that sink on the argument that "one line
// to a local stderr, at AG-M1 load, is not a plausible stall"; these turn the argument into
// a number.
//
// They do not discharge §6.2 on their own, and PR1 does not claim they do. Showing that
// telemetry is not the request-path bottleneck *under load* needs a workload-level
// comparison — telemetry on versus off, same dataset and concurrency, reporting the
// throughput and p99 delta — which is scoped to PR2. A per-call cost bounds that comparison;
// it does not replace it.
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

// BenchmarkRecorderSlogFileSink writes to a real file, and exists because every other
// benchmark here writes to io.Discard.
//
// io.Discard measures formatting, allocation and fan-out with the sink removed, which is the
// right isolation for "is emission inherently expensive" and the wrong one for "is the
// service's actual write cheap". The running service writes to a descriptor — a file, a
// pipe, a container's stdout — and that write is a syscall this package's synchronous
// emission holds the request through. A temporary file is the bounded local stand-in: not
// the slowest sink a deployment might have, but a real one, and the difference between this
// row and the io.Discard row is the part io.Discard cannot show.
//
// It is still a microbenchmark. It bounds the per-call cost of a healthy sink; it does not
// establish the end-to-end §6.2 claim, which needs a workload-level comparison of throughput
// and p99 with telemetry on versus off. That comparison belongs with the sweeps in PR2.
func BenchmarkRecorderSlogFileSink(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "telemetry-*.log")
	if err != nil {
		b.Fatalf("creating sink: %v", err)
	}
	b.Cleanup(func() { _ = f.Close() })

	rec := telemetry.NewSlogRecorder(slog.New(slog.NewJSONHandler(f, nil)))
	ctx := context.Background()
	obs := observation()
	b.ReportAllocs()
	for b.Loop() {
		rec.RecordRequest(ctx, obs)
	}
}

// BenchmarkRecorderTeeFileSink is the service's own configuration against a real sink: both
// recorders, one observation, a descriptor at the end of it. Compared against
// BenchmarkRecorderTee it prices the write that io.Discard removes.
func BenchmarkRecorderTeeFileSink(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "telemetry-*.log")
	if err != nil {
		b.Fatalf("creating sink: %v", err)
	}
	b.Cleanup(func() { _ = f.Close() })

	rec := metrics.Tee{
		telemetry.NewSlogRecorder(slog.New(slog.NewJSONHandler(f, nil))),
		metrics.New(prometheus.NewRegistry()),
	}
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
