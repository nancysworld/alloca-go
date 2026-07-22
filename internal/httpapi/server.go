// Package httpapi builds the Alloca-Go HTTP surface.
//
// AG-M0 intentionally exposes only operational endpoints — liveness, readiness,
// and runtime metadata. The transactional booking API arrives in AG-M1; the
// readiness hook here is the seam it will extend.
package httpapi

import (
	"net/http"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

// ReadinessFunc reports whether the service is ready to serve domain traffic. In
// AG-M0 it always reports ready; AG-M1 replaces it with a check of the database
// and any other hard dependencies.
type ReadinessFunc func() error

// Server bundles the HTTP handler and the configuration used to construct the
// underlying *http.Server.
type Server struct {
	cfg     config.Config
	handler http.Handler
}

// New constructs a Server. metaSource supplies runtime metadata for /meta; ready
// gates /readyz. A nil ready is treated as "always ready".
func New(cfg config.Config, metaSource func() buildinfo.Info, ready ReadinessFunc) *Server {
	if ready == nil {
		ready = func() error { return nil }
	}
	mux := http.NewServeMux()
	registerRoutes(mux, metaSource, cfg.RequestBudget, ready)
	return &Server{cfg: cfg, handler: mux}
}

// Handler exposes the router, primarily so tests can exercise it via httptest
// without binding a socket.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// HTTPServer builds the hardened *http.Server. The timeouts come from config and
// are the outer, server-side layer of the provisional timeout budget documented in
// docs/design/measurement-contract.md.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}
}

// ShutdownGrace returns the configured graceful-shutdown budget.
func (s *Server) ShutdownGrace() time.Duration {
	return s.cfg.ShutdownGrace
}
