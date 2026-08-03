package loadgen

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
)

// Manifest is the provenance block ag-sept-plan §6.4 requires of every quotable run:
// enough to reproduce or compare it.
//
// It is emitted with the summary rather than alongside it so a result cannot be separated
// from the conditions that produced it. A number without its manifest is not a result.
type Manifest struct {
	// Identity of the code under test, read from the service's own /meta.
	//
	// This is the revision §6.4 means by "commit SHA". It is *not* the generator's: a
	// service left running from one commit while the harness is rebuilt from another is the
	// ordinary state of a working session, and recording the generator's revision here would
	// name a commit that was never measured. That is worse than recording nothing, because
	// the provenance looks trustworthy.
	//
	// SourceModified records whether the working tree carried uncommitted changes at build.
	// A dirty tree makes the SHA describe something the binary is not, so a run stamped with
	// a clean-looking commit that cannot be checked out and reproduced is worse provenance
	// than one with no commit at all — again, nothing about it looks wrong.
	ServiceCommitSHA      string `json:"service_commit_sha"`
	ServiceSourceModified bool   `json:"service_source_modified"`
	ServiceGoVersion      string `json:"service_go_version"`
	ImageTag              string `json:"image_tag,omitempty"`

	// Identity of the harness that produced the numbers. Kept because "which generator ran
	// this?" is a real question when a run looks anomalous — but kept under names that
	// cannot be mistaken for the service's.
	GeneratorCommitSHA      string `json:"generator_commit_sha"`
	GeneratorSourceModified bool   `json:"generator_source_modified"`
	GeneratorGoVersion      string `json:"generator_go_version"`

	// Topology.
	ReplicaCount       int    `json:"replica_count"`
	DeploymentTopology string `json:"deployment_topology"`
	Environment        string `json:"environment"`

	// Service-side shape the service publishes about itself at /meta. Discovered rather
	// than transcribed: a value the service already reports is one an operator should never
	// be asked to retype, since a typo there is indistinguishable from a measurement.
	ServerGOMAXPROCS int    `json:"server_gomaxprocs,omitempty"`
	TimeoutBudget    string `json:"timeout_budget,omitempty"`
	ReservationTTL   string `json:"reservation_ttl,omitempty"`

	// Service-side shape the service does not know about itself. These stay operator-
	// supplied and staged by ag-sept-plan §14 — the database's version and the pool
	// arithmetic are facts about the deployment, and no endpoint on the service reports them.
	PostgresVersion    string `json:"postgres_version,omitempty"`
	PoolSizePerReplica int    `json:"pool_size_per_replica,omitempty"`
	AggregatePoolSize  int    `json:"aggregate_pool_size,omitempty"`

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

// Report is what a run writes out: the conditions, the result, and the verdict on what the
// two together may back.
//
// Quotability sits on the Report rather than on either half because neither half can reach
// it alone — the summary knows whether the measurement is sound, the manifest knows what
// provenance was recorded, and the level is a function of both.
type Report struct {
	Manifest    Manifest    `json:"manifest"`
	Summary     Summary     `json:"summary"`
	Quotability Quotability `json:"quotability"`
}

// NewManifest joins the two provenances a run has: what the generator knows about itself,
// and what the service reported about itself at /meta.
//
// Every runtime fact is recorded twice under distinct names, never once under a shared one.
// GOMAXPROCS and the Go version differ between the two processes routinely, and a single
// field would silently attribute one side's constraint to the other — which is the same class
// of error as recording the generator's commit as the code under test.
//
// A zero ServiceMeta is accepted: the caller passes one when /meta could not be read, and the
// resulting empty service identity is what the quotability gate refuses. Refusing here would
// mean no report at all.
func NewManifest(target, workload string, opts Options, location string, svc ServiceMeta) Manifest {
	info := buildinfo.Collect(time.Now())
	return Manifest{
		ServiceCommitSHA:      svc.Revision,
		ServiceSourceModified: svc.Modified,
		ServiceGoVersion:      svc.GoVersion,
		ServerGOMAXPROCS:      svc.GOMAXPROCS,
		TimeoutBudget:         svc.TimeoutBudgetString(),
		ReservationTTL:        svc.ReservationTTL,

		GeneratorCommitSHA:      info.Revision,
		GeneratorSourceModified: info.Modified,
		GeneratorGoVersion:      runtime.Version(),

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

// Validate reports the §6.4 fields a claim at the given level requires and this manifest
// does not carry. An empty result means the provenance bar for that level is met.
//
// The split between levels is ag-sept-plan §14's staging, not a judgement made here. The
// generator is an HTTP client and cannot discover the service's shape (§14 line 481), so the
// service-side fields are supplied by the operator in the PR that first has something to say
// — service shape in PR2, topology and image identity in PR3, environment in PR4. What the
// staging does not excuse is a field the generator *can* determine: PR1's own exit criterion
// is that the manifest carries every one of those, and an empty commit SHA fails it.
//
// Each rule names the field as it appears in the JSON, so the reason travels with the report
// to someone holding only the artifact.
func (m Manifest) Validate(level Level) []string {
	var missing []string
	add := func(cond bool, msg string) {
		if cond {
			missing = append(missing, msg)
		}
	}

	if level.AtLeast(LevelLocal) {
		// Identity of the code under test. This is the one the level exists to protect: a
		// run that cannot say which service binary answered it measures an unknown.
		add(m.ServiceCommitSHA == "", "service_commit_sha is empty: the service reported no "+
			"revision at /meta, or /meta could not be read. A service started with `go run` "+
			"or `make dev` carries no VCS stamp — build it with `go build` and restart it")
		add(m.ServiceSourceModified, "service_source_modified is true: the service was built "+
			"from a tree with uncommitted changes, so service_commit_sha does not describe "+
			"the binary that answered the requests")
		add(m.ServiceGoVersion == "", "service_go_version is empty: /meta was not read")
		add(m.ServerGOMAXPROCS < 1, "server_gomaxprocs is not positive: /meta was not read")
		add(m.TimeoutBudget == "", "timeout_budget is empty: /meta was not read")
		add(m.ReservationTTL == "", "reservation_ttl is empty: /meta was not read")

		// Identity of the harness. Generator-determinable, so nothing here has an excuse to
		// be empty — see NewManifest.
		add(m.GeneratorCommitSHA == "", "generator_commit_sha is empty: the generator was "+
			"built without VCS stamping (`go run` does not stamp; build it with `go build`)")
		add(m.GeneratorSourceModified, "generator_source_modified is true: the generator was "+
			"built from a tree with uncommitted changes, so generator_commit_sha does not "+
			"describe the binary that ran")
		add(m.GeneratorGoVersion == "", "generator_go_version is empty")

		add(m.Workload == "", "workload is empty")
		add(m.Concurrency < 1, "concurrency is not positive")
		add(m.Iterations < 1, "iterations is not positive")
		add(m.WarmUp == "", "warm_up is empty")
		add(m.GeneratorLocation == "", "generator_location is empty")
		add(m.GeneratorGOMAXPROCS < 1, "generator_gomaxprocs is not positive")
		add(m.GeneratorNumCPU < 1, "generator_num_cpu is not positive: generator saturation "+
			"cannot be ruled out")
		add(m.Target == "", "target is empty")
		add(m.Timestamp.IsZero(), "timestamp is unset")
	}

	if level.AtLeast(LevelCapacity) {
		// Service shape the service cannot report about itself — PR2 supplies these. The
		// other three service-shape fields are checked at `local`, because /meta hands them
		// over for free and a field that costs nothing to record should not gate a
		// higher tier than a field that costs an operator's attention.
		add(m.PostgresVersion == "", "postgres_version is empty (operator-supplied, PR2)")
		add(m.PoolSizePerReplica < 1, "pool_size_per_replica is not positive (operator-supplied, PR2)")
		add(m.AggregatePoolSize < 1, "aggregate_pool_size is not positive (operator-supplied, PR2)")

		// Topology — PR3 supplies these.
		add(m.ReplicaCount < 1, "replica_count is not positive (operator-supplied, PR3)")
		add(m.DeploymentTopology == "", "deployment_topology is empty (operator-supplied, PR3)")

		// Aggregate pool capacity is a claim about the whole deployment, so it must be
		// consistent with the two fields it is derived from. An inconsistency here means one
		// of the three was typed from memory, and none of them can be trusted after that.
		if m.ReplicaCount > 0 && m.PoolSizePerReplica > 0 && m.AggregatePoolSize > 0 {
			add(m.AggregatePoolSize != m.ReplicaCount*m.PoolSizePerReplica, fmt.Sprintf(
				"aggregate_pool_size is %d but %d replicas × %d connections is %d",
				m.AggregatePoolSize, m.ReplicaCount, m.PoolSizePerReplica,
				m.ReplicaCount*m.PoolSizePerReplica))
		}

		// Environment — PR4 supplies this. It sits at this level rather than the next
		// because a capacity claim that does not say where it was measured is not
		// interpretable, even unpublished.
		add(m.Environment == "", "environment is empty (operator-supplied, PR4)")

		// image_tag is deliberately absent: §14 allows it to be conditional for a local
		// source build, provided the manifest identifies the deployment mode — which
		// deployment_topology, required above, is what does that.
	}

	if level.AtLeast(LevelPublishable) {
		// §6.3: the generator must run on compute separate from the service. This is a
		// declaration check — the manifest records where the operator says the generator
		// ran, and no HTTP client can verify that from the outside.
		add(isCoResident(m.GeneratorLocation), fmt.Sprintf(
			"generator_location is %q: §6.3 requires the generator on compute separate "+
				"from the service for a publishable claim", m.GeneratorLocation))
	}

	return missing
}

// isCoResident reports whether a generator location names the service's own host.
//
// It matches the values alloca-load's own default and documentation produce rather than
// attempting to classify arbitrary strings: an operator who writes something else is making
// a declaration this package takes at face value, which is the most a manifest can do.
func isCoResident(location string) bool {
	switch strings.ToLower(strings.TrimSpace(location)) {
	case "", "local", "localhost", "same-host", "co-resident":
		return true
	default:
		return false
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
