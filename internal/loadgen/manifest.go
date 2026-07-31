package loadgen

import (
	"encoding/json"
	"io"
	"net/url"
	"runtime"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
)

// Manifest is the provenance block ag-sept-plan §6.4 requires of every quotable run:
// enough to reproduce or compare it.
//
// It is emitted with the summary rather than alongside it so a result cannot be separated
// from the conditions that produced it. A number without its manifest is not a result.
type Manifest struct {
	// Identity of the code under test.
	CommitSHA string `json:"commit_sha"`
	ImageTag  string `json:"image_tag,omitempty"`
	GoVersion string `json:"go_version"`

	// Topology.
	ReplicaCount       int    `json:"replica_count"`
	DeploymentTopology string `json:"deployment_topology"`
	Environment        string `json:"environment"`

	// Service-side shape. These are supplied by the operator running the experiment
	// rather than discovered, because the generator deliberately cannot see the service's
	// configuration — it is an HTTP client, not a peer.
	PostgresVersion    string `json:"postgres_version,omitempty"`
	PoolSizePerReplica int    `json:"pool_size_per_replica,omitempty"`
	AggregatePoolSize  int    `json:"aggregate_pool_size,omitempty"`
	ServerGOMAXPROCS   int    `json:"server_gomaxprocs,omitempty"`
	TimeoutBudget      string `json:"timeout_budget,omitempty"`
	ReservationTTL     string `json:"reservation_ttl,omitempty"`

	// Workload and dataset.
	Workload     string `json:"workload"`
	Concurrency  int    `json:"concurrency"`
	Iterations   int    `json:"iterations"`
	WarmUp       string `json:"warm_up"`
	DatasetSlots int    `json:"dataset_slots,omitempty"`
	DatasetUsers int    `json:"dataset_users,omitempty"`

	// Generator side.
	GeneratorLocation   string `json:"generator_location"`
	GeneratorGOMAXPROCS int    `json:"generator_gomaxprocs"`
	GeneratorNumCPU     int    `json:"generator_num_cpu"`

	// Target names the service the run addressed, with any credentials stripped.
	Target string `json:"target"`

	Timestamp time.Time `json:"timestamp"`
}

// Report is what a run writes out: the conditions and the result, together.
type Report struct {
	Manifest Manifest `json:"manifest"`
	Summary  Summary  `json:"summary"`
}

// NewManifest fills the fields the generator can determine for itself and leaves the
// service-side fields to the caller.
//
// GoVersion and GOMAXPROCS describe the *generator*; the server reports its own at /meta,
// and conflating them would misattribute a generator-side constraint to the service.
func NewManifest(target, workload string, opts Options, location string) Manifest {
	info := buildinfo.Collect(time.Now())
	return Manifest{
		CommitSHA:           info.Revision,
		GoVersion:           runtime.Version(),
		Workload:            workload,
		Concurrency:         opts.Concurrency,
		Iterations:          opts.Iterations,
		WarmUp:              opts.WarmUp.String(),
		GeneratorLocation:   location,
		GeneratorGOMAXPROCS: runtime.GOMAXPROCS(0),
		GeneratorNumCPU:     runtime.NumCPU(),
		Target:              redact(target),
		Timestamp:           time.Now().UTC(),
	}
}

// redact removes anything credential-shaped from a URL before it is committed.
//
// §6.4 ends with "secrets and private endpoints must not be committed", and a manifest is
// exactly the artifact that gets pasted into a report. Userinfo is the part of a URL that
// carries a password, so it is dropped rather than masked — a masked secret still records
// that there was one and how long it was.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// WriteReport emits the report as indented JSON, which is both machine-readable and
// reviewable in a diff.
func WriteReport(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
