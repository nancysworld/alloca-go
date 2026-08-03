package loadgen_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// soundSummary is a summary that passes every soundness rule, so a level test exercises the
// manifest and nothing else. Certify checks soundness first and would otherwise mask an
// incomplete manifest behind LevelNone for a different reason.
func soundSummary() loadgen.Summary {
	return loadgen.Summary{Sound: true}
}

// localManifest carries what PR1 can establish without an operator: the generator's own
// identity, and everything the service reports about itself at /meta. The fields no endpoint
// reports — the database's version, the pool arithmetic, the topology — are left at their
// zero values, which is precisely the state PR1's own smoke run is in.
//
// The two revisions differ on purpose. They are distinct facts about distinct binaries, and a
// fixture that used one value for both could not fail if the code confused them.
func localManifest() loadgen.Manifest {
	return loadgen.Manifest{
		ServiceCommitSHA:    "aaaa111111111111111111111111111111111111",
		ServiceGoVersion:    "go1.26.5",
		ServerGOMAXPROCS:    4,
		TimeoutBudget:       "lock_timeout=2s statement_timeout=3s txn_budget=3.5s",
		ReservationTTL:      "2m0s",
		GeneratorCommitSHA:  "bbbb222222222222222222222222222222222222",
		GeneratorGoVersion:  "go1.26.5",
		Workload:            "dispersed",
		Concurrency:         8,
		Iterations:          60,
		WarmUp:              "0s",
		GeneratorLocation:   "local",
		GeneratorGOMAXPROCS: 10,
		GeneratorNumCPU:     10,
		Target:              "http://localhost:8080",
		Timestamp:           time.Now().UTC(),
	}
}

// capacityManifest adds the fields no endpoint reports, which PR2 and PR3 supply by hand.
func capacityManifest() loadgen.Manifest {
	m := localManifest()
	m.PostgresVersion = "17.2"
	m.PoolSizePerReplica = 25
	m.AggregatePoolSize = 25
	m.ReplicaCount = 1
	m.DeploymentTopology = "single-instance-local"
	m.Environment = "workstation"
	return m
}

// publishableManifest additionally moves the generator off the service host, which is the
// one §6.3 requirement a manifest can record.
func publishableManifest() loadgen.Manifest {
	m := capacityManifest()
	m.GeneratorLocation = "separate-host"
	return m
}

// TestCompleteManifestReachesEachLevel is the positive control for the table below. Without
// it, a Validate that rejected everything would make every negative test pass for the wrong
// reason, and the ladder would be untestable.
func TestCompleteManifestReachesEachLevel(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    loadgen.Manifest
		want loadgen.Level
	}{
		{"generator fields only", localManifest(), loadgen.LevelLocal},
		{"service shape and topology", capacityManifest(), loadgen.LevelCapacity},
		{"generator on separate compute", publishableManifest(), loadgen.LevelPublishable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := loadgen.Certify(tc.m, soundSummary()); got.Level != tc.want {
				t.Errorf("level = %q, want %q (blocked by: %s)",
					got.Level, tc.want, got.BlockedBecause)
			}
		})
	}
}

// TestIncompleteManifestCannotBeCertified is the finding this whole change exists for: a run
// whose provenance is missing must not certify itself at a level that provenance cannot
// support. Each case removes exactly one required field from an otherwise complete manifest,
// so a passing case proves that field is load-bearing rather than that the manifest happened
// to fail somewhere.
func TestIncompleteManifestCannotBeCertified(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*loadgen.Manifest)
		from    loadgen.Manifest
		want    loadgen.Level
		mention string
	}{
		{
			// The defect in PR1's committed evidence: `go run` does not stamp VCS data, so
			// the report carried commit_sha: "".
			name:    "empty service commit SHA cannot reach local",
			corrupt: func(m *loadgen.Manifest) { m.ServiceCommitSHA = "" },
			from:    localManifest(),
			want:    loadgen.LevelNone,
			mention: "service_commit_sha",
		},
		{
			// Worse than an empty SHA, because nothing about it looks wrong.
			name:    "dirty service tree cannot reach local",
			corrupt: func(m *loadgen.Manifest) { m.ServiceSourceModified = true },
			from:    localManifest(),
			want:    loadgen.LevelNone,
			mention: "service_source_modified",
		},
		{
			name:    "missing generator CPU count cannot reach local",
			corrupt: func(m *loadgen.Manifest) { m.GeneratorNumCPU = 0 },
			from:    localManifest(),
			want:    loadgen.LevelNone,
			mention: "generator_num_cpu",
		},
		{
			name:    "missing PostgreSQL version stops at local",
			corrupt: func(m *loadgen.Manifest) { m.PostgresVersion = "" },
			from:    capacityManifest(),
			want:    loadgen.LevelLocal,
			mention: "postgres_version",
		},
		{
			name:    "missing replica count stops at local",
			corrupt: func(m *loadgen.Manifest) { m.ReplicaCount = 0 },
			from:    capacityManifest(),
			want:    loadgen.LevelLocal,
			mention: "replica_count",
		},
		{
			name:    "missing environment stops at local",
			corrupt: func(m *loadgen.Manifest) { m.Environment = "" },
			from:    capacityManifest(),
			want:    loadgen.LevelLocal,
			mention: "environment",
		},
		{
			// An aggregate that contradicts its own factors means one of the three was
			// typed from memory, and a capacity claim rests on all three.
			name:    "inconsistent aggregate pool size stops at local",
			corrupt: func(m *loadgen.Manifest) { m.AggregatePoolSize = 99 },
			from:    capacityManifest(),
			want:    loadgen.LevelLocal,
			mention: "aggregate_pool_size",
		},
		{
			// §6.3: a co-resident generator cannot back a published number, however
			// complete the rest of the manifest is.
			name:    "co-resident generator stops at capacity",
			corrupt: func(m *loadgen.Manifest) { m.GeneratorLocation = "local" },
			from:    publishableManifest(),
			want:    loadgen.LevelCapacity,
			mention: "§6.3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.from
			tc.corrupt(&m)

			got := loadgen.Certify(m, soundSummary())
			if got.Level != tc.want {
				t.Fatalf("level = %q, want %q", got.Level, tc.want)
			}
			if !strings.Contains(got.BlockedBecause, tc.mention) {
				t.Errorf("reason = %q, want it to name %q", got.BlockedBecause, tc.mention)
			}
			if got.BlockedFrom == "" {
				t.Error("no blocked_from recorded, so the report does not say what it fell short of")
			}
		})
	}
}

// TestUnsoundRunIsLevelNoneDespiteCompleteProvenance fixes the order of the two gates.
// Soundness cannot be bought with provenance: a run whose responses went unvalidated
// describes nothing, and the most complete manifest in the world does not change that.
func TestUnsoundRunIsLevelNoneDespiteCompleteProvenance(t *testing.T) {
	s := loadgen.Summary{Sound: false, NotSoundBecause: "response validation was disabled"}

	got := loadgen.Certify(publishableManifest(), s)
	if got.Level != loadgen.LevelNone {
		t.Fatalf("level = %q, want none — a complete manifest rescued an unsound run", got.Level)
	}
	if !strings.Contains(got.BlockedBecause, "validation was disabled") {
		t.Errorf("reason = %q, want the soundness failure to survive into the verdict",
			got.BlockedBecause)
	}
}

// TestLevelOrdering pins the ladder. AtLeast is what both binaries' -require gate is built
// on, so an ordering mistake here would let a co-resident run satisfy -require publishable.
func TestLevelOrdering(t *testing.T) {
	ascending := []loadgen.Level{
		loadgen.LevelNone, loadgen.LevelLocal, loadgen.LevelCapacity, loadgen.LevelPublishable,
	}
	for i, low := range ascending {
		for j, high := range ascending {
			want := i >= j
			if got := low.AtLeast(high); got != want {
				t.Errorf("%q.AtLeast(%q) = %v, want %v", low, high, got, want)
			}
		}
	}
}

// TestUnknownLevelIsRejected covers the typo at the call site. An unrecognised -require must
// fail loudly: silently treating it as "none" would turn a raised bar into no bar at all,
// which is the failure mode a gate must never have.
func TestUnknownLevelIsRejected(t *testing.T) {
	if _, err := loadgen.ParseLevel("publishible"); err == nil {
		t.Fatal("a misspelled level was accepted")
	}
	if got := loadgen.Level("publishible").AtLeast(loadgen.LevelLocal); got {
		t.Error("an unknown level satisfied a local requirement")
	}

	for _, name := range []string{"none", "LOCAL", " capacity ", "publishable"} {
		if _, err := loadgen.ParseLevel(name); err != nil {
			t.Errorf("ParseLevel(%q) rejected a valid level: %v", name, err)
		}
	}
}
