package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nancysworld/alloca-go/internal/metrics"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// buildRecorder assembles the observation recorder the service runs with: AG-M1's
// structured log line and AG-Sept's aggregate series, both fed from the same observation.
//
// Both, not one: the log answers "what happened to this request" and the metric answers
// "what happened to ten million of them", and an experiment needs to be able to drop from
// the second to the first when a total looks wrong.
func buildRecorder(reg *prometheus.Registry, pool *pgxpool.Pool, logger *slog.Logger) telemetry.Recorder {
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		newPoolCollector(pool),
	)
	return metrics.Tee{
		telemetry.NewSlogRecorder(logger),
		metrics.New(reg),
	}
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
