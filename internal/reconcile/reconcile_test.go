package reconcile_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

// The outcome-closure check is the one rule of §6.5 that needs no database — it is a
// statement about the client's own totals — so it is unit-testable here. The three
// database-backed rules are exercised by the integration suite, which has a schema.

func summary(totals []loadgen.Total, completed int) loadgen.Summary {
	return loadgen.Summary{Totals: totals, Completed: completed, Sound: true}
}

// reportOf wraps a summary in a manifest complete enough to reach LevelLocal, so a verdict
// that comes back below that level did so for the reason the test is about. A zero manifest
// would fail every case on missing provenance and prove nothing about reconciliation.
func reportOf(s loadgen.Summary) loadgen.Report {
	return loadgen.Report{Manifest: localManifest(), Summary: s}
}

func localManifest() loadgen.Manifest {
	return loadgen.Manifest{
		CommitSHA:           "91818cd7f3a0556371ff4619754323258488ea4a",
		GoVersion:           "go1.26.5",
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

// TestOutcomeOutsideClosedSetIsNotQuotable covers the case where the service answered with
// something the contract does not define. Counting it would put a value into a capacity
// report that no consumer of the contract can interpret.
func TestOutcomeOutsideClosedSetIsNotQuotable(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.Outcome("probably_fine"), Count: 3},
	}, 3)

	res, err := reconcile.RunClientChecks(context.Background(), reportOf(s))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("run with an undefined outcome is marked quotable")
	}
	if !strings.Contains(res.Quotability.BlockedBecause, "closed terminal-outcome set") {
		t.Errorf("verdict does not name the closed-set violation: %q", res.Quotability.BlockedBecause)
	}
}

// TestReplayCountedAsPeerOutcomeIsCaught is the discriminating case for §6.5's fourth rule.
//
// The totals are cells of (operation, outcome, reason, replay), so they must sum to the
// completed count exactly. If a replay were counted once under its recorded outcome and
// again as a disposition of its own, the sum would exceed the completed count — which is
// precisely the flat-enum modelling measurement-contract §4 calls a defect. This is the
// arithmetic that notices.
func TestReplayCountedAsPeerOutcomeIsCaught(t *testing.T) {
	// Two requests completed; the totals claim three, as they would if a replay were
	// counted both as admitted_success and as a peer "replay" outcome.
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Count: 2},
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Replay: true, Count: 1},
	}, 2)

	res, err := reconcile.RunClientChecks(context.Background(), reportOf(s))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("run whose totals do not sum to its completed count is marked quotable")
	}
	if !strings.Contains(res.Quotability.BlockedBecause, "double-counted") {
		t.Errorf("verdict does not name the double count: %q", res.Quotability.BlockedBecause)
	}
}

// TestRefusalReasonOnNonRefusalIsCaught covers the other malformed-total case: a reason
// belongs to a business_refusal and to nothing else.
func TestRefusalReasonOnNonRefusalIsCaught(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess,
			Reason: domain.ReasonNoCapacity, Count: 1},
	}, 1)

	res, err := reconcile.RunClientChecks(context.Background(), reportOf(s))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("admitted_success carrying a refusal reason is marked quotable")
	}
}

// TestCleanTotalsReconcile guards the opposite failure: a checker that rejected everything
// would pass every test above while making measurement impossible. Replays are legitimate
// here and must not be treated as an error.
func TestCleanTotalsReconcile(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Count: 8},
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Replay: true, Count: 1},
		{Operation: "reserve", Outcome: domain.OutcomeBusinessRefusal,
			Reason: domain.ReasonNoCapacity, Count: 3},
	}, 12)

	res, err := reconcile.RunClientChecks(context.Background(), reportOf(s))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotability.Level == loadgen.LevelNone {
		t.Fatalf("clean totals rejected: %s", res.Quotability.BlockedBecause)
	}
}

// TestGeneratorVerdictIsNotOverridden pins the independence of the two gates: a run whose
// responses failed validation cannot become quotable by reconciling. Both must pass.
func TestGeneratorVerdictIsNotOverridden(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Count: 1},
	}, 1)
	s.Sound = false
	s.NotSoundBecause = "response validation was disabled"

	res, err := reconcile.RunClientChecks(context.Background(), reportOf(s))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("reconciliation overrode the generator's refusal to certify the run")
	}
	if !strings.Contains(res.Quotability.BlockedBecause, "validation was disabled") {
		t.Errorf("verdict loses the generator's reason: %q", res.Quotability.BlockedBecause)
	}
}
