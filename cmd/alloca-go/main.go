// Command alloca-go runs the Alloca-Go HTTP service.
//
// AG-M0 scope: an operational skeleton (liveness, readiness, runtime metadata)
// that starts, serves, and shuts down cleanly under CI and the race detector. The
// transactional booking core is added in AG-M1.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("service exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run wires configuration, the server, and signal handling, then blocks until the
// process is asked to stop. It is separated from main so its error path is
// testable and so os.Exit is confined to main.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadFromOS()
	if err != nil {
		return err
	}

	startedAt := time.Now()
	metaSource := func() buildinfo.Info { return buildinfo.Collect(startedAt) }

	srv := httpapi.New(cfg, metaSource, nil)
	httpServer := srv.HTTPServer()

	// Stop the base context on the first interrupt/termination signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	info := metaSource()
	logger.Info("starting alloca-go",
		slog.String("addr", cfg.ListenAddr),
		slog.String("go_version", info.GoVersion),
		slog.Int("gomaxprocs", info.GOMAXPROCS),
		slog.Bool("gomaxprocs_explicit", info.GOMAXPROCSExplicit),
		slog.String("revision", info.Revision),
	)

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
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining", slog.Duration("grace", cfg.ShutdownGrace))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}
