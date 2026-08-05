// Command alloca-go runs the Alloca-Go HTTP service.
//
// It wires the pieces and owns none of the behaviour: configuration, a PostgreSQL pool,
// the repository, the booking service, the expiry worker, telemetry, and the HTTP
// surface. Every design decision lives in the package that implements it; this file is
// the only place that knows they exist together (project-structure §4).
//
// It does not migrate. ADR-0002 keeps schema changes out of the serving path, so
// alloca-migrate runs once before a new version serves traffic and replicas never race
// the same DDL on startup.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/httpapi"
	"github.com/nancysworld/alloca-go/internal/ids"
	"github.com/nancysworld/alloca-go/internal/postgres"
	"github.com/nancysworld/alloca-go/internal/service"
	"github.com/nancysworld/alloca-go/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("service exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run wires the service and blocks until the process is asked to stop. It is separated
// from main so its error path is testable and so os.Exit is confined to main.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadFromOS()
	if err != nil {
		return err
	}
	dsn, ok := os.LookupEnv("DATABASE_URL")
	if !ok || dsn == "" {
		return fmt.Errorf("DATABASE_URL must be set")
	}

	// The routing decision for this unit's whole life, resolved before anything opens a
	// connection: a unit that cannot say which organisations it owns has no business
	// accepting a request for one (horizontal-database-authority §5.2).
	placement, authority, err := resolvePlacement(cfg.Placement)
	if err != nil {
		return err
	}

	// Stop the base context on the first interrupt/termination signal. Everything that
	// runs for the life of the process derives from it, so one signal stops all of them.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The pool verifies connectivity before returning, so a bad DSN or an unreachable
	// database fails here rather than on the first request.
	pool, err := postgres.OpenPool(ctx, dsn, cfg.RequestBudget)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The schema gate, before anything serves. ADR-0002 keeps migration out of the
	// serving path, so this process cannot repair what it finds — which is precisely why
	// it must refuse to start rather than meet the mismatch one failing query at a time,
	// under load. In a sharded deployment an authority left behind by a rollout would
	// serve its own organisations wrongly while its peers served theirs correctly.
	schemaVersion, err := postgres.CheckSchema(ctx, pool)
	if err != nil {
		return err
	}

	repo := postgres.New(pool, cfg.RequestBudget)
	svc := service.New(repo, ids.Random{}, cfg.ReservationTTL, placement)

	// A private registry rather than the default: the default is package-global, so a
	// duplicate registration anywhere in the process would panic at startup and a test
	// binary importing this package would inherit whatever else had registered.
	registry := prometheus.NewRegistry()
	mode, err := telemetryMode()
	if err != nil {
		return err
	}
	recorder, onMisroute := buildRecorder(registry, pool, logger, mode)
	dbMeta := databaseMeta(ctx, pool, schemaVersion, logger)
	shutdownMetrics := serveMetrics(metricsAddr(), registry, logger)

	startedAt := time.Now()
	metaSource := func() buildinfo.Info { return buildinfo.Collect(startedAt) }

	srv := httpapi.New(cfg, metaSource, httpapi.Options{
		Service:       svc,
		Slots:         repo,
		Recorder:      recorder,
		Logger:        logger,
		Database:      dbMeta,
		TelemetryMode: mode,
		// This unit serves the organisations its authority owns and refuses the rest,
		// so a routing mistake is refused here rather than written to the wrong database.
		Placement:  placement,
		Authority:  authority,
		OnMisroute: onMisroute,
		// Readiness is the database check: this service cannot answer a booking request
		// without it, so reporting ready while it is unreachable would just move the
		// failure from the probe to every request.
		Ready: repo.Ready,
	})
	httpServer := srv.HTTPServer()

	expiry := worker.NewExpiry(repo, svc, recorder, logger, worker.Config{})

	info := metaSource()
	logger.Info("starting alloca-go",
		slog.String("addr", cfg.ListenAddr),
		slog.Duration("reservation_ttl", cfg.ReservationTTL),
		slog.String("go_version", info.GoVersion),
		slog.Int("gomaxprocs", info.GOMAXPROCS),
		slog.Bool("gomaxprocs_explicit", info.GOMAXPROCSExplicit),
		slog.String("revision", info.Revision),
		slog.String("authority", string(authority)),
		slog.String("routing_version", placement.Version()),
		slog.Int64("schema_version", schemaVersion),
		slog.Bool("sharded", !placement.IsUnsharded()),
	)

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		expiry.Run(ctx)
	}()

	serveErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		// Serving failed on its own. Stop the worker before returning, or the process
		// would exit with it still mid-transaction.
		stop()
		_ = shutdownMetrics(context.Background())
		workers.Wait()
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining", slog.Duration("grace", cfg.ShutdownGrace))
	}

	// Shutdown order matters: stop accepting and drain in-flight requests first, then
	// wait for the worker. The worker's context is already cancelled by the signal, so
	// it stops at its next tick or returns from the iteration it is in.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	// The metrics listener drains last: a scrape taken during the drain is exactly the
	// observation an experiment wants of a shutting-down replica.
	_ = shutdownMetrics(shutdownCtx)
	workers.Wait()
	if shutdownErr != nil {
		return shutdownErr
	}
	logger.Info("shutdown complete")
	return nil
}
