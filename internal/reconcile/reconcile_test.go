package reconcile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

// The outcome-closure check is the one rule of §6.5 that needs no database — it is a
// statement about the client's own totals — so it is unit-testable here. The three
// database-backed rules are exercised by the integration suite, which has a schema.

func summary(totals []loadgen.Total, completed int) loadgen.Summary {
	return loadgen.Summary{Totals: totals, Completed: completed, Quotable: true}
}

// TestOutcomeOutsideClosedSetIsNotQuotable covers the case where the service answered with
// something the contract does not define. Counting it would put a value into a capacity
// report that no consumer of the contract can interpret.
func TestOutcomeOutsideClosedSetIsNotQuotable(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.Outcome("probably_fine"), Count: 3},
	}, 3)

	res, err := reconcile.RunClientChecks(context.Background(), s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotable {
		t.Fatal("run with an undefined outcome is marked quotable")
	}
	if !strings.Contains(res.Because, "closed terminal-outcome set") {
		t.Errorf("verdict does not name the closed-set violation: %q", res.Because)
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

	res, err := reconcile.RunClientChecks(context.Background(), s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotable {
		t.Fatal("run whose totals do not sum to its completed count is marked quotable")
	}
	if !strings.Contains(res.Because, "double-counted") {
		t.Errorf("verdict does not name the double count: %q", res.Because)
	}
}

// TestRefusalReasonOnNonRefusalIsCaught covers the other malformed-total case: a reason
// belongs to a business_refusal and to nothing else.
func TestRefusalReasonOnNonRefusalIsCaught(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess,
			Reason: domain.ReasonNoCapacity, Count: 1},
	}, 1)

	res, err := reconcile.RunClientChecks(context.Background(), s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotable {
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

	res, err := reconcile.RunClientChecks(context.Background(), s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Quotable {
		t.Fatalf("clean totals rejected: %s", res.Because)
	}
}

// TestGeneratorVerdictIsNotOverridden pins the independence of the two gates: a run whose
// responses failed validation cannot become quotable by reconciling. Both must pass.
func TestGeneratorVerdictIsNotOverridden(t *testing.T) {
	s := summary([]loadgen.Total{
		{Operation: "reserve", Outcome: domain.OutcomeAdmittedSuccess, Count: 1},
	}, 1)
	s.Quotable = false
	s.NotQuotableBecause = "response validation was disabled"

	res, err := reconcile.RunClientChecks(context.Background(), s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Quotable {
		t.Fatal("reconciliation overrode the generator's refusal to certify the run")
	}
	if !strings.Contains(res.Because, "validation was disabled") {
		t.Errorf("verdict loses the generator's reason: %q", res.Because)
	}
}
