package loadgen_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

func stableMeta() loadgen.ServiceMeta {
	m := loadgen.ServiceMeta{
		GoVersion: "go1.26.5", GOMAXPROCS: 10,
		Revision:       "aaaa111111111111111111111111111111111111",
		RequestBudget:  map[string]string{"lock_timeout": "2s", "txn_budget": "3.5s"},
		ReservationTTL: "2m0s", TelemetryMode: "full",
		StartedAt: "2026-08-03T10:00:00Z",
	}
	m.Database.Version = "16.14"
	m.Database.PoolMaxConns = 10
	return m
}

// TestUnchangedServiceReportsNoDrift is the positive control. A check that reported drift on
// every run would refuse every cell, and would look like a working guard while making the
// sweep impossible.
func TestUnchangedServiceReportsNoDrift(t *testing.T) {
	before := stableMeta()
	after := stableMeta()
	if got := after.DriftFrom(before); got != "" {
		t.Errorf("identical /meta reads reported drift: %q", got)
	}
	// A re-serialised budget map must not read as a change: Go map ordering is not stable,
	// and comparing the raw maps by iteration would fail intermittently — the worst kind of
	// gate, one that refuses a correct run occasionally.
	after.RequestBudget = map[string]string{"txn_budget": "3.5s", "lock_timeout": "2s"}
	if got := after.DriftFrom(before); got != "" {
		t.Errorf("reordered budget map reported drift: %q", got)
	}
}

// TestDriftIsDetectedAndNamed covers each way a service can stop being the one the manifest
// describes. The restart case is the one DEBT-3 is actually about, and the one a revision
// comparison alone would miss entirely.
func TestDriftIsDetectedAndNamed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*loadgen.ServiceMeta)
		mention string
	}{
		{
			// Same binary, same commit, different process. Nothing but the start time
			// changes, and the warm state a cell depends on is gone.
			name:    "restart onto the same commit",
			change:  func(m *loadgen.ServiceMeta) { m.StartedAt = "2026-08-03T10:00:31Z" },
			mention: "restarted",
		},
		{
			name:    "rolling replacement onto a new commit",
			change:  func(m *loadgen.ServiceMeta) { m.Revision = "bbbb2222222222222222222222222222222222" },
			mention: "revision changed",
		},
		{
			name:    "compute changed under it",
			change:  func(m *loadgen.ServiceMeta) { m.GOMAXPROCS = 4 },
			mention: "GOMAXPROCS changed",
		},
		{
			name:    "observation cost changed",
			change:  func(m *loadgen.ServiceMeta) { m.TelemetryMode = "off" },
			mention: "telemetry mode changed",
		},
		{
			// Changes how long each admitted reserve holds capacity, so it changes the
			// contention the workload produced without changing the workload.
			name:    "reservation TTL changed",
			change:  func(m *loadgen.ServiceMeta) { m.ReservationTTL = "15m0s" },
			mention: "reservation TTL changed",
		},
		{
			name:    "timeout budget changed",
			change:  func(m *loadgen.ServiceMeta) { m.RequestBudget["lock_timeout"] = "9s" },
			mention: "timeout budget changed",
		},
		{
			name:    "pool ceiling changed",
			change:  func(m *loadgen.ServiceMeta) { m.Database.PoolMaxConns = 50 },
			mention: "pool ceiling changed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := stableMeta()
			after := stableMeta()
			tc.change(&after)

			got := after.DriftFrom(before)
			if got == "" {
				t.Fatal("no drift reported for a service that changed mid-run")
			}
			if !strings.Contains(got, tc.mention) {
				t.Errorf("reason %q does not name %q", got, tc.mention)
			}
		})
	}
}

// TestDriftRefusesTheRunAtTheGate closes the loop: detecting drift is only useful if it stops
// the number being quoted.
func TestDriftRefusesTheRunAtTheGate(t *testing.T) {
	m := localManifest()
	m.ServiceIdentityDrift = "the service restarted during the run"

	got := loadgen.Certify(m, loadgen.Summary{Sound: true})
	if got.Level != loadgen.LevelNone {
		t.Fatalf("level = %q, want none for a run whose service changed under it", got.Level)
	}
	if !strings.Contains(got.BlockedBecause, "did not stay the same") {
		t.Errorf("reason = %q, want it to name the identity change", got.BlockedBecause)
	}
}
