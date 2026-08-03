package loadgen_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// metaServer answers /meta with the given payload and 404s everything else, so a test that
// hits the wrong path fails rather than quietly reading nothing.
func metaServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/meta" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serviceMetaPayload mirrors what internal/httpapi actually serves. Kept as a literal rather
// than built from the httpapi types on purpose: loadgen reaches the service over HTTP and
// must survive the wire format, and importing the server's structs would make this test pass
// on a shape no service ever emits.
const serviceMetaPayload = `{
  "go_version": "go1.25.0",
  "gomaxprocs": 4,
  "num_cpu": 4,
  "gomaxprocs_explicit": false,
  "modified": false,
  "revision": "aaaa111111111111111111111111111111111111",
  "started_at": "2026-08-03T10:00:00Z",
  "request_budget": {
    "client_deadline": "6s",
    "server_deadline": "5s",
    "lock_timeout": "2s",
    "statement_timeout": "3s",
    "txn_budget": "3.5s"
  },
  "reservation_ttl": "2m0s",
  "readiness_timeout": "1s"
}`

// TestManifestRecordsTheServiceRevisionNotTheGeneratorsIsThe discriminating test for the
// provenance-identity finding: the service and the generator are built from different
// commits, and the manifest must name the *service's* as the code under test.
//
// This is the case that motivated splitting the field. A service left running from one commit
// while the harness is rebuilt from another is the ordinary state of a working session, and
// the old manifest recorded the generator's revision under a field documented as the identity
// of the code under test — a populated, clean-looking, wrong answer.
func TestManifestRecordsTheServiceRevisionNotTheGenerators(t *testing.T) {
	srv := metaServer(t, serviceMetaPayload)

	svc, err := loadgen.FetchServiceMeta(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("fetch /meta: %v", err)
	}

	m := loadgen.NewManifest(srv.URL, "dispersed",
		loadgen.Options{Concurrency: 8, Iterations: 60}, "local", svc)

	const serviceRevision = "aaaa111111111111111111111111111111111111"
	if m.ServiceCommitSHA != serviceRevision {
		t.Errorf("service_commit_sha = %q, want the service's own revision %q",
			m.ServiceCommitSHA, serviceRevision)
	}
	if m.GeneratorCommitSHA == serviceRevision {
		t.Error("generator_commit_sha carries the service's revision: the two provenances " +
			"have been confused, which is the defect this test exists for")
	}

	// The Go versions differ between the fixture service and this test binary, which is what
	// makes the second assertion meaningful rather than incidental.
	if m.ServiceGoVersion != "go1.25.0" {
		t.Errorf("service_go_version = %q, want the service's go1.25.0", m.ServiceGoVersion)
	}
	if m.GeneratorGoVersion == m.ServiceGoVersion {
		t.Error("generator_go_version equals the service's, so the two runtimes are being " +
			"conflated")
	}
	if m.ServerGOMAXPROCS != 4 {
		t.Errorf("server_gomaxprocs = %d, want the service's 4", m.ServerGOMAXPROCS)
	}
	if m.ServerGOMAXPROCS == m.GeneratorGOMAXPROCS && m.GeneratorGOMAXPROCS != 4 {
		t.Error("server_gomaxprocs took the generator's value")
	}
}

// TestServiceMetaPopulatesTheBudgetFields covers the fields the review noted could come from
// the same fetch, so an operator is never asked to retype a value the service already reports.
func TestServiceMetaPopulatesTheBudgetFields(t *testing.T) {
	srv := metaServer(t, serviceMetaPayload)
	svc, err := loadgen.FetchServiceMeta(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("fetch /meta: %v", err)
	}
	m := loadgen.NewManifest(srv.URL, "dispersed",
		loadgen.Options{Concurrency: 1, Iterations: 1}, "local", svc)

	if m.ReservationTTL != "2m0s" {
		t.Errorf("reservation_ttl = %q, want 2m0s", m.ReservationTTL)
	}
	for _, want := range []string{"lock_timeout=2s", "statement_timeout=3s", "txn_budget=3.5s"} {
		if !strings.Contains(m.TimeoutBudget, want) {
			t.Errorf("timeout_budget %q is missing %q", m.TimeoutBudget, want)
		}
	}
}

// TestTimeoutBudgetIsStable pins the rendering, because a map's iteration order would make
// two runs under an identical budget serialise differently and look like a changed condition.
func TestTimeoutBudgetIsStable(t *testing.T) {
	svc := loadgen.ServiceMeta{RequestBudget: map[string]string{
		"txn_budget": "3.5s", "lock_timeout": "2s", "statement_timeout": "3s",
	}}
	first := svc.TimeoutBudgetString()
	for range 20 {
		if got := svc.TimeoutBudgetString(); got != first {
			t.Fatalf("rendering is not stable: %q then %q", first, got)
		}
	}
	if want := "lock_timeout=2s statement_timeout=3s txn_budget=3.5s"; first != want {
		t.Errorf("timeout_budget = %q, want %q", first, want)
	}
}

// TestUnreachableMetaLeavesTheRunUncertifiable covers the operational case: the target is
// wrong, or the service is down. The generator must not invent an identity, and the run must
// not reach `local` — an unknown service binary is exactly what the level exists to refuse.
func TestUnreachableMetaLeavesTheRunUncertifiable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	svc, err := loadgen.FetchServiceMeta(context.Background(), srv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("a 503 from /meta was reported as success")
	}

	m := loadgen.NewManifest(srv.URL, "dispersed",
		loadgen.Options{Concurrency: 1, Iterations: 1}, "local", svc)
	if m.ServiceCommitSHA != "" {
		t.Errorf("service_commit_sha = %q, want empty when /meta could not be read",
			m.ServiceCommitSHA)
	}

	got := loadgen.Certify(m, loadgen.Summary{Sound: true})
	if got.Level != loadgen.LevelNone {
		t.Fatalf("level = %q, want none for a run with no service identity", got.Level)
	}
	if !strings.Contains(got.BlockedBecause, "service_commit_sha") {
		t.Errorf("reason = %q, want it to name the missing service identity",
			got.BlockedBecause)
	}
}

// TestServiceWithoutVCSStampIsRefused is the case Nancy's local run actually hit: the service
// answers /meta correctly but was started with `go run`, so it carries no revision at all.
// The manifest must not fall back to the generator's, and the run must not reach `local`.
func TestServiceWithoutVCSStampIsRefused(t *testing.T) {
	srv := metaServer(t, `{"go_version":"go1.26.5","gomaxprocs":10,"modified":false,
		"request_budget":{"lock_timeout":"2s"},"reservation_ttl":"2m0s"}`)

	svc, err := loadgen.FetchServiceMeta(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("fetch /meta: %v", err)
	}
	m := loadgen.NewManifest(srv.URL, "dispersed",
		loadgen.Options{Concurrency: 1, Iterations: 1}, "local", svc)

	if m.ServiceCommitSHA != "" {
		t.Errorf("service_commit_sha = %q, want empty — an unstamped service has no revision "+
			"and the generator's must not stand in for it", m.ServiceCommitSHA)
	}
	if got := loadgen.Certify(m, loadgen.Summary{Sound: true}); got.Level != loadgen.LevelNone {
		t.Fatalf("level = %q, want none: `go run` leaves the service unidentifiable", got.Level)
	}
}
