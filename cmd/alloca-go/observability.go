package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/httpapi"
	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// Telemetry modes, selected by ALLOCA_TELEMETRY.
//
// Three rather than two, because the two questions an experiment asks are different. §6.2
// asks what *emission* costs on the request path, and PR1 measured that almost all of it is
// the synchronous log write (1793ns for the Tee against a real file; 136ns for the Prometheus
// recorder alone) — so TelemetryMetricsOnly is the arm that isolates the cost that matters
// while leaving the run reconcilable. TelemetryOff answers the blunter question of what the
// service does with no observation at all, and pays for it: with no aggregate series there is
// no server-side count, so §6.5's three-way agreement cannot be reached and the run is not
// quotable. That refusal is deliberate and is enforced in the manifest, not left to be
// noticed.
const (
	TelemetryFull        = "full"
	TelemetryMetricsOnly = "metrics_only"
	TelemetryOff         = "off"
)

// telemetryMode reads the requested mode, defaulting to full.
//
// Env-driven rather than part of config.Config, for the same reason METRICS_ADDR is: this is
// an experiment-time concern, and config.Config is the service's operational contract — a
// field there would imply every deployment must answer for it.
func telemetryMode() (string, error) {
	switch mode := os.Getenv("ALLOCA_TELEMETRY"); mode {
	case "", TelemetryFull:
		return TelemetryFull, nil
	case TelemetryMetricsOnly, TelemetryOff:
		return mode, nil
	default:
		return "", fmt.Errorf("ALLOCA_TELEMETRY=%q: want %s, %s or %s",
			mode, TelemetryFull, TelemetryMetricsOnly, TelemetryOff)
	}
}

// buildRecorder assembles the observation recorder the service runs with.
//
// In full mode that is AG-M1's structured log line and AG-Sept's aggregate series, both fed
// from the same observation — the log answers "what happened to this request" and the metric
// answers "what happened to ten million of them", and an experiment needs to drop from the
// second to the first when a total looks wrong.
//
// The process-level collectors stay registered in every mode. They are not request-path
// telemetry, and keeping them means an `off` run still shows its own CPU and memory, which is
// most of the reason to run one.
// buildRecorder returns the telemetry recorder and, separately, the misroute hook.
//
// The hook is not part of telemetry.Recorder because a misroute is a deployment fault
// rather than a completed request's outcome (internal/metrics). It is nil when metrics
// are off, for the same reason every other signal is: "off" has to mean off, or the
// §6.2 telemetry comparison is measuring two different services.
func buildRecorder(reg *prometheus.Registry, pool *pgxpool.Pool, logger *slog.Logger, mode string) (telemetry.Recorder, func(context.Context, domain.Operation)) {
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		newPoolCollector(pool),
	)

	switch mode {
	case TelemetryOff:
		return telemetry.Nop{}, nil
	case TelemetryMetricsOnly:
		m := metrics.New(reg)
		return m, m.RecordMisroute
	default:
		m := metrics.New(reg)
		return metrics.Tee{
			telemetry.NewSlogRecorder(logger),
			m,
		}, m.RecordMisroute
	}
}

// databaseMeta reads what the service can say about its own authority.
//
// The version is queried once at startup rather than per request: it cannot change under a
// live connection pool, and a /meta that issued a database round trip would make the endpoint
// fail exactly when the database is the thing under pressure.
//
// A failed query yields an empty version rather than a startup failure. The service can serve
// without knowing its server version; what it cannot do is claim a capacity result, and the
// manifest gate is what refuses that — one place, not two.
func databaseMeta(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) httpapi.DatabaseMeta {
	meta := httpapi.DatabaseMeta{PoolMaxConns: pool.Config().MaxConns}

	var version string
	if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		logger.Warn("could not read server_version; /meta will omit it and no run against "+
			"this service can reach a capacity claim", slog.Any("error", err))
		return meta
	}
	meta.Version = version

	// The schema version comes from goose's own bookkeeping table. A multi-authority run
	// is admissible only if every participating authority is schema-compatible with the
	// serving binary (horizontal-database-authority §5.1), and the harness can only check
	// that if each unit reports what it is running against.
	//
	// Absent or unreadable is not a startup failure, for the same reason the server
	// version is not: the service can serve without knowing it, and the manifest gate is
	// what refuses to quote a run that does not.
	var schema int64
	if err := pool.QueryRow(ctx,
		"SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&schema); err != nil {
		logger.Warn("could not read the schema version; /meta will omit it and a "+
			"multi-authority run cannot check schema compatibility", slog.Any("error", err))
		return meta
	}
	meta.SchemaVersion = schema
	return meta
}

// serveMetrics exposes the registry on its own listener.
//
// A separate port from the service, deliberately. The scrape must stay reachable when the
// booking port is saturated — an experiment's most valuable measurement is the one taken
// while the service is at its frontier, and sharing the listener would make the metrics
// disappear at exactly the load that matters. It also keeps /metrics off the public
// surface without needing a route guard.
func serveMetrics(addr string, reg *prometheus.Registry, logger *slog.Logger) func(context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		// A scrape that fails should say so rather than serve a half-page that reads as
		// missing series.
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("metrics listener starting", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// A failed metrics listener must not take the service down: booking traffic is
			// the reason the process exists, and losing observability is a degraded
			// experiment rather than a failed service.
			logger.Error("metrics listener stopped", slog.Any("error", err))
		}
	}()

	return srv.Shutdown
}

// poolCollector reports pgxpool state as gauges.
//
// Pool acquisition is one of the boundaries ag-sept-plan §11.2 lists as a candidate
// frontier, and it is invisible from the request path alone: a request waiting on a
// connection looks exactly like a slow query until this is on the page.
type poolCollector struct {
	pool *pgxpool.Pool

	acquired    *prometheus.Desc
	idle        *prometheus.Desc
	total       *prometheus.Desc
	max         *prometheus.Desc
	acquireWait *prometheus.Desc
	acquires    *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	n := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(metrics.Namespace+"_db_pool_"+name, help, nil, nil)
	}
	return &poolCollector{
		pool:        pool,
		acquired:    n("acquired_connections", "Connections currently in use."),
		idle:        n("idle_connections", "Connections idle in the pool."),
		total:       n("total_connections", "Connections the pool currently holds."),
		max:         n("max_connections", "Configured maximum pool size."),
		acquireWait: n("acquire_wait_seconds_total", "Cumulative time spent waiting to acquire."),
		acquires:    n("acquires_total", "Cumulative successful acquires."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquired
	ch <- c.idle
	ch <- c.total
	ch <- c.max
	ch <- c.acquireWait
	ch <- c.acquires
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	gauge := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
	}

	gauge(c.acquired, float64(s.AcquiredConns()))
	gauge(c.idle, float64(s.IdleConns()))
	gauge(c.total, float64(s.TotalConns()))
	gauge(c.max, float64(s.MaxConns()))
	counter(c.acquireWait, s.AcquireDuration().Seconds())
	counter(c.acquires, float64(s.AcquireCount()))
}

var _ prometheus.Collector = (*poolCollector)(nil)

// metricsAddr is where the scrape listener binds. It is env-driven rather than part of
// config.Config because it is an experiment-time concern: config.Config is the service's
// operational contract, and adding a field there would imply every deployment must answer
// for it.
func metricsAddr() string {
	if addr := os.Getenv("METRICS_ADDR"); addr != "" {
		return addr
	}
	return ":9090"
}
