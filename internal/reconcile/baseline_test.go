package reconcile_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

func totals(admitted, refused int) reconcile.ServerTotals {
	var out reconcile.ServerTotals
	if admitted > 0 {
		out = append(out, loadgen.Total{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeAdmittedSuccess, Count: admitted,
		})
	}
	if refused > 0 {
		out = append(out, loadgen.Total{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeBusinessRefusal,
			Reason:    domain.ReasonNoCapacity, Count: refused,
		})
	}
	return out
}

// TestBaselineIsolatesTheMeasuredPhase is the property warm-up depends on.
//
// A warmed cell leaves the service running across the fixture reset, so its counters carry the
// warm-up traffic. Without a baseline the check compares 140 server requests against the 100
// the client reports and fails a correct service — which is what would happen to every cell in
// every sweep.
func TestBaselineIsolatesTheMeasuredPhase(t *testing.T) {
	warmUp := totals(40, 0)           // what the warm-up phase left behind
	after := totals(40+80, 20)        // warm-up plus the measured phase
	s := summary(totals(80, 20), 100) // the client saw only the measured phase

	t.Run("without a baseline the correct service fails", func(t *testing.T) {
		got := reconcile.RunServerCheckForTest(s, after)
		if got.OK {
			t.Fatal("absolute counters reconciled against a measured-phase-only client " +
				"report, so this test is not exercising the problem baselines solve")
		}
	})

	t.Run("with the baseline it reconciles", func(t *testing.T) {
		got := reconcile.RunServerCheckWithBaselineForTest(s, warmUp, after)
		if !got.OK {
			t.Fatalf("warmed cell rejected: %s", got.Detail)
		}
	})
}

// TestCounterGoingBackwardsIsNamed covers the failure a delta introduces that an absolute
// count could not have: the service restarted between the two scrapes.
//
// It matters beyond arithmetic. A restart mid-cell discards the warm state the baseline was
// taken to preserve, and the binary that served the measured phase need not be the one the
// manifest names — which is DEBT-3's hazard arriving inside a single cell.
func TestCounterGoingBackwardsIsNamed(t *testing.T) {
	got := reconcile.RunServerCheckWithBaselineForTest(
		summary(totals(80, 0), 80),
		totals(500, 0), // baseline from before a restart
		totals(80, 0),  // counters reset, so "after" is lower
	)

	if got.OK {
		t.Fatal("a counter that went backwards was accepted, so a mid-cell restart would " +
			"be reported as a clean run")
	}
	for _, want := range []string{"backwards", "restarted"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail %q does not mention %q", got.Detail, want)
		}
	}
}

// TestBaselineStillCatchesRealDisagreement guards the obvious way to break this: a delta that
// subtracted whatever it needed to would reconcile everything.
func TestBaselineStillCatchesRealDisagreement(t *testing.T) {
	got := reconcile.RunServerCheckWithBaselineForTest(
		summary(totals(80, 20), 100),
		totals(40, 0),
		totals(40+85, 20), // server saw five more admitted than the client reported
	)
	if got.OK {
		t.Fatal("a five-request disagreement inside the measured phase was absorbed by the " +
			"baseline subtraction")
	}
	if !strings.Contains(got.Detail, "105") && !strings.Contains(got.Detail, "admitted") {
		t.Errorf("detail does not locate the disagreement: %q", got.Detail)
	}
}
