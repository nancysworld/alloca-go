// Package httpapi builds the Alloca-Go HTTP surface: the operational endpoints
// (liveness, readiness, runtime metadata) and the booking endpoints over them.
//
// It makes no domain decisions. A handler turns a request into one service command and
// hands the answer to the mapping in mapping.go; whether a slot has capacity, whether a
// key is a replay, and whether an identity's schedule is free are all decided below this
// package (project-structure §4).
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// withRequestID attaches a short random identifier to every request's context, so log
// lines from one request can be tied together. It travels in the context, not on the
// observation types, which is what keeps it out of a future metric's labels
// (internal/telemetry).
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		// rand.Read never fails on the platforms this runs on; an unlikely failure
		// yields the zero identifier, which is worse for correlation but harmless.
		_, _ = rand.Read(b[:])
		ctx := telemetry.WithRequestID(r.Context(), hex.EncodeToString(b[:]))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ReadinessFunc reports whether the service is ready to serve domain traffic. It takes a
// context because an unbounded probe cannot distinguish "the database is slow" from "the
// database is gone", which is the only question readiness exists to answer. The caller
// supplies the bound (see handleReadyz).
type ReadinessFunc func(ctx context.Context) error

// Options carry the collaborators the booking surface needs. They are optional so the
// operational skeleton — liveness, readiness, metadata — can still be served without a
// database behind it, which is what AG-M0 shipped and what the probe tests still use.
type Options struct {
	// Service enables the booking endpoints. When nil, only the operational surface is
	// registered.
	Service BookingService
	// Slots enables the read route. When nil, it is not registered.
	Slots SlotLister
	// Logger records what the responses deliberately withhold — a readiness failure's
	// cause, for instance. When nil, slog.Default is used.
	Logger *slog.Logger
	// Recorder observes completed requests. When nil, observations are discarded.
	Recorder telemetry.Recorder
	// Ready gates /readyz. When nil, the service always reports ready.
	Ready ReadinessFunc
}

// Server bundles the HTTP handler and the configuration used to construct the
// underlying *http.Server.
type Server struct {
	cfg     config.Config
	handler http.Handler
}

// New constructs a Server. metaSource supplies runtime metadata for /meta; opts supply
// the booking service, telemetry, and readiness check, each of which may be absent.
func New(cfg config.Config, metaSource func() buildinfo.Info, opts Options) *Server {
	ready := opts.Ready
	if ready == nil {
		ready = func(context.Context) error { return nil }
	}
	recorder := opts.Recorder
	if recorder == nil {
		recorder = telemetry.Nop{}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	registerRoutes(mux, metaSource, cfg, ready, logger, opts.Service, opts.Slots, recorder)
	return &Server{cfg: cfg, handler: withRequestID(mux)}
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
