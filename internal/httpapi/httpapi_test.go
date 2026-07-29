package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

func testServer(t *testing.T, ready ReadinessFunc) *Server {
	t.Helper()
	meta := func() buildinfo.Info { return buildinfo.Collect(time.Now()) }
	return New(config.Default(), meta, Options{Ready: ready})
}

func do(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthzOK(t *testing.T) {
	rec := do(t, testServer(t, nil), pathHealthz)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", pathHealthz, rec.Code)
	}
}

func TestReadyzReadyByDefault(t *testing.T) {
	rec := do(t, testServer(t, nil), pathReadyz)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", pathReadyz, rec.Code)
	}
}

func TestReadyzUnavailableWhenNotReady(t *testing.T) {
	notReady := func(context.Context) error { return errors.New("database unreachable") }
	rec := do(t, testServer(t, notReady), pathReadyz)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET %s = %d, want 503", pathReadyz, rec.Code)
	}
	// The reason is deliberately NOT echoed. A readiness failure comes from
	// infrastructure, so its error text can carry a DSN or driver detail, and the probe
	// is an unauthenticated endpoint. The operator gets it from the logs instead.
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readyz body not JSON: %v", err)
	}
	if got, ok := body["reason"]; ok {
		t.Errorf("readyz leaked a failure reason to the caller: %q", got)
	}
	if body["status"] != "unavailable" {
		t.Errorf("status = %q, want %q", body["status"], "unavailable")
	}
}

func TestMetaReportsRuntime(t *testing.T) {
	rec := do(t, testServer(t, nil), pathMeta)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", pathMeta, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("meta body not JSON: %v", err)
	}
	if info.GoVersion == "" {
		t.Error("meta go_version is empty")
	}
	if info.GOMAXPROCS < 1 {
		t.Errorf("meta gomaxprocs = %d, want >= 1", info.GOMAXPROCS)
	}
}

func TestMetaIncludesRequestBudget(t *testing.T) {
	rec := do(t, testServer(t, nil), pathMeta)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", pathMeta, rec.Code)
	}
	var body struct {
		GoVersion     string `json:"go_version"`
		RequestBudget struct {
			ServerDeadline string `json:"server_deadline"`
			LockTimeout    string `json:"lock_timeout"`
		} `json:"request_budget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("meta body not JSON: %v", err)
	}
	// buildinfo provenance stays inlined at the top level.
	if body.GoVersion == "" {
		t.Error("meta go_version is empty")
	}
	// The resolved budget is exposed as human-readable duration strings; the defaults
	// come from config.Default().RequestBudget.
	if body.RequestBudget.ServerDeadline != "5s" {
		t.Errorf("request_budget.server_deadline = %q, want 5s", body.RequestBudget.ServerDeadline)
	}
	if body.RequestBudget.LockTimeout != "2s" {
		t.Errorf("request_budget.lock_timeout = %q, want 2s", body.RequestBudget.LockTimeout)
	}
}

func TestHTTPServerAppliesTimeouts(t *testing.T) {
	cfg := config.Default()
	s := New(cfg, func() buildinfo.Info { return buildinfo.Info{} }, Options{})
	hs := s.HTTPServer()
	if hs.ReadHeaderTimeout != cfg.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", hs.ReadHeaderTimeout, cfg.ReadHeaderTimeout)
	}
	if hs.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", hs.IdleTimeout, cfg.IdleTimeout)
	}
	if hs.Addr != cfg.ListenAddr {
		t.Errorf("Addr = %q, want %q", hs.Addr, cfg.ListenAddr)
	}
}
