package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ServiceMeta is the provenance of the service under test, read from its own `/meta`.
//
// This is the identity §6.4 asks for when it says "commit SHA". The generator's own revision
// answers a different question — which harness produced the numbers — and the two are not
// interchangeable: a service started from one commit and a generator built from another is
// the ordinary state of a working session, not an exotic case.
//
// Reading it over HTTP keeps the §6.3 boundary intact. The generator gains no credentials and
// no shared state; it asks the service to describe itself, on the same contract it already
// uses to drive load. `internal/buildinfo` says this is what `/meta` is for.
type ServiceMeta struct {
	GoVersion      string            `json:"go_version"`
	GOMAXPROCS     int               `json:"gomaxprocs"`
	Revision       string            `json:"revision"`
	Modified       bool              `json:"modified"`
	RequestBudget  map[string]string `json:"request_budget"`
	ReservationTTL string            `json:"reservation_ttl"`

	// Database is the authority the service is bound to. Supplying these here rather than as
	// operator flags is the same argument PR1 settled for the runtime fields: a value the
	// service already knows should never be retyped, because a typo in a transcribed version
	// string is indistinguishable from a measurement.
	Database struct {
		Version      string `json:"version"`
		PoolMaxConns int    `json:"pool_max_conns"`
	} `json:"database"`

	// TelemetryMode names which recorder the service is running. Two runs under different
	// observation settings are not comparable, and nothing in the totals would say so — this
	// is what lets §6.2's comparison identify its own arms, and what makes an unobservable
	// run refuse itself rather than reconcile against a server count that does not exist.
	TelemetryMode string `json:"telemetry_mode"`
}

// TimeoutBudgetString renders the deadline chain as one deterministic line for the manifest.
//
// Keys are sorted rather than written in chain order, because the chain's order is a property
// of §8.1 that the manifest does not restate — what the manifest needs is a value two runs can
// be compared on, and a map's iteration order would make identical budgets serialise
// differently.
func (m ServiceMeta) TimeoutBudgetString() string {
	if len(m.RequestBudget) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m.RequestBudget))
	for k := range m.RequestBudget {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m.RequestBudget[k])
	}
	return strings.Join(parts, " ")
}

// FetchServiceMeta reads the target service's `/meta`.
//
// A failure is returned rather than swallowed, but the caller is expected to carry on and
// still write a report: a run that cannot establish what it measured is one an operator needs
// to see, and the quotability gate is where it is refused. Failing here instead would produce
// no artifact at all, which is the one outcome that teaches nobody anything.
func FetchServiceMeta(ctx context.Context, target string, timeout time.Duration) (ServiceMeta, error) {
	var meta ServiceMeta

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := strings.TrimRight(target, "/") + "/meta"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return meta, fmt.Errorf("building /meta request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return meta, fmt.Errorf("reading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return meta, fmt.Errorf("%s returned %d, want 200", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return meta, fmt.Errorf("decoding %s: %w", url, err)
	}
	return meta, nil
}
