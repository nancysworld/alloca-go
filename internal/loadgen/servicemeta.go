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
		// SchemaVersion is the migration version this authority is at, validated by the
		// unit's own startup gate before it served anything (INV-27). Two authorities at
		// different versions are not interchangeable, which is what makes this a
		// certification input rather than a diagnostic.
		SchemaVersion int64 `json:"schema_version"`
	} `json:"database"`

	// Placement is which authority this unit is and which routing it is serving under. It
	// is the field that makes a multi-unit run checkable: nothing in the request totals
	// would reveal two units disagreeing about placement, and that disagreement is the
	// split-brain the design note records as §7.3.
	Placement struct {
		AuthorityID    string   `json:"authority_id"`
		RoutingVersion string   `json:"routing_version"`
		Sharded        bool     `json:"sharded"`
		Organisations  []string `json:"organisations"`
	} `json:"placement"`

	// TelemetryMode names which recorder the service is running. Two runs under different
	// observation settings are not comparable, and nothing in the totals would say so — this
	// is what lets §6.2's comparison identify its own arms, and what makes an unobservable
	// run refuse itself rather than reconcile against a server count that does not exist.
	TelemetryMode string `json:"telemetry_mode"`

	// StartedAt is when the process began, and it is the field that makes this struct a
	// *process* identity rather than a build identity. A service restarted onto the same
	// commit has an identical revision, so nothing else here would change — and a restart
	// mid-run is precisely DEBT-3's hazard.
	StartedAt string `json:"started_at"`
}

// DriftFrom names how the service changed between two reads of /meta, or returns "" when it
// did not. It is the check DEBT-3 asks for: a run's identity is claimed from a read taken
// before the workload, and nothing until now proved the same service was still behind the
// target when it ended.
//
// Every field is compared rather than the revision alone. A restart onto the same commit
// leaves the revision identical while discarding the warm state a cell depends on, and a
// configuration change — a different timeout budget, a different pool ceiling, telemetry
// switched — changes what was measured without changing what was built.
func (m ServiceMeta) DriftFrom(before ServiceMeta) string {
	switch {
	case m.StartedAt != before.StartedAt:
		return fmt.Sprintf("the service restarted during the run: it reported start time %s "+
			"before and %s after, so part of the workload ran against a process that is gone "+
			"and whatever warm state the measurement assumed went with it",
			before.StartedAt, m.StartedAt)
	case m.Revision != before.Revision:
		return fmt.Sprintf("the service revision changed during the run, from %s to %s: the "+
			"manifest can name only one, and neither describes the whole sample",
			before.Revision, m.Revision)
	case m.Modified != before.Modified:
		return "the service's source-modified flag changed during the run"
	case m.GoVersion != before.GoVersion:
		return fmt.Sprintf("the service's Go version changed during the run, from %s to %s",
			before.GoVersion, m.GoVersion)
	case m.GOMAXPROCS != before.GOMAXPROCS:
		return fmt.Sprintf("the service's GOMAXPROCS changed during the run, from %d to %d: "+
			"the compute available to it was not constant across the measurement",
			before.GOMAXPROCS, m.GOMAXPROCS)
	case m.TelemetryMode != before.TelemetryMode:
		return fmt.Sprintf("the service's telemetry mode changed during the run, from %q to "+
			"%q, so the observation cost was not constant", before.TelemetryMode, m.TelemetryMode)
	case m.ReservationTTL != before.ReservationTTL:
		return fmt.Sprintf("the reservation TTL changed during the run, from %s to %s: it "+
			"decides how long each admitted reserve holds capacity, so the contention the "+
			"workload produced was not constant", before.ReservationTTL, m.ReservationTTL)
	case m.TimeoutBudgetString() != before.TimeoutBudgetString():
		return "the service's timeout budget changed during the run"
	case m.Database.Version != before.Database.Version:
		return fmt.Sprintf("the database version changed during the run, from %q to %q",
			before.Database.Version, m.Database.Version)
	case m.Placement.RoutingVersion != before.Placement.RoutingVersion:
		return fmt.Sprintf("the unit's routing version changed during the run, from %q to %q: "+
			"part of the workload was routed by a placement the rest was not",
			before.Placement.RoutingVersion, m.Placement.RoutingVersion)
	case m.Placement.AuthorityID != before.Placement.AuthorityID:
		return fmt.Sprintf("the unit's authority changed during the run, from %q to %q",
			before.Placement.AuthorityID, m.Placement.AuthorityID)
	case m.Database.SchemaVersion != before.Database.SchemaVersion:
		return fmt.Sprintf("the authority's schema version changed during the run, from %d to %d",
			before.Database.SchemaVersion, m.Database.SchemaVersion)
	case m.Database.PoolMaxConns != before.Database.PoolMaxConns:
		return fmt.Sprintf("the pool ceiling changed during the run, from %d to %d: it is one "+
			"of the admission boundaries a frontier is read against",
			before.Database.PoolMaxConns, m.Database.PoolMaxConns)
	default:
		return ""
	}
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
