package loadgen_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// admittingServer answers every reserve with a fresh admitted reservation, after an optional
// delay. The delay is what lets a test interrupt a run mid-flight, or hold responses inside
// a warm-up window, without depending on machine speed.
func admittingServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"outcome":"admitted_success","replay":false,` +
			`"reservation_id":"res_` + strings.Repeat("a", 8) + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dispersedOver(slots int) loadgen.Workload {
	dataset := make([]loadgen.Slot, slots)
	for i := range dataset {
		dataset[i] = loadgen.Slot{OrganisationID: "load-org", SlotID: domain.SlotID("slot-0")}
	}
	return loadgen.Dispersed{Org: "load-org", Slots: dataset}
}

// TestInterruptedRunIsNotQuotable covers the truncated experiment: SIGINT arrives, the run
// reports what it did, and the report must not certify itself.
//
// The failure this prevents is quiet. A truncated run's totals are internally consistent —
// they are simply the totals of a smaller experiment — while the manifest still says how
// many iterations were *requested*, so every rate derived from it describes a run that never
// happened.
func TestInterruptedRunIsNotQuotable(t *testing.T) {
	srv := admittingServer(t, 20*time.Millisecond)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	s := loadgen.NewRunner(client, loadgen.Options{Concurrency: 2, Iterations: 500}).
		Run(ctx, dispersedOver(4))

	if s.Quotable {
		t.Fatalf("interrupted run certified itself: %d of %d iterations",
			s.CompletedIterations, s.Iterations)
	}
	if !strings.Contains(s.NotQuotableBecause, "interrupted") {
		t.Errorf("reason = %q, want it to name the interruption", s.NotQuotableBecause)
	}
	if s.CompletedIterations >= s.Iterations {
		t.Errorf("completed %d of %d — the run was not actually truncated, so this test "+
			"proved nothing", s.CompletedIterations, s.Iterations)
	}
	// The report is still written and still describes what happened: refusing to quote a
	// run is not the same as refusing to report it.
	if s.Completed == 0 {
		t.Error("interrupted run reported no completed requests at all")
	}
}

// TestCompleteRunReportsEveryIteration is the control for the test above: the same workload
// and server, no cancellation. Without it, a bug that always reported a shortfall would make
// the interruption test pass for the wrong reason.
func TestCompleteRunReportsEveryIteration(t *testing.T) {
	srv := admittingServer(t, 0)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	s := loadgen.NewRunner(client, loadgen.Options{Concurrency: 4, Iterations: 40}).
		Run(context.Background(), dispersedOver(4))

	if s.CompletedIterations != 40 {
		t.Errorf("completed_iterations = %d, want 40", s.CompletedIterations)
	}
	if !s.Quotable {
		t.Errorf("complete run rejected: %s", s.NotQuotableBecause)
	}
}

// TestWarmUpRunIsNotQuotable pins PR1's deliberate limitation: the discarded responses left
// rows in the database that the client totals no longer mention, so persisted-state
// reconciliation would compare unlike quantities and fail a correct service. PR1 refuses the
// run instead of reporting one that cannot be reconciled; PR2 owns making warm-up quotable.
func TestWarmUpRunIsNotQuotable(t *testing.T) {
	srv := admittingServer(t, 5*time.Millisecond)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	s := loadgen.NewRunner(client, loadgen.Options{
		Concurrency: 2, Iterations: 40, WarmUp: 30 * time.Millisecond,
	}).Run(context.Background(), dispersedOver(4))

	if s.WarmUpDiscarded == 0 {
		t.Fatal("no responses were discarded, so this test proved nothing about warm-up")
	}
	if s.Quotable {
		t.Fatalf("warm-up run certified itself after discarding %d responses",
			s.WarmUpDiscarded)
	}
	if !strings.Contains(s.NotQuotableBecause, "warm-up") {
		t.Errorf("reason = %q, want it to name the warm-up window", s.NotQuotableBecause)
	}
}

// TestValidationOutranksTheOtherRefusals fixes the order of the quotability rules. A run
// that disabled validation *and* was interrupted must report the validation failure: it is
// the mandatory control of measurement-contract §5.5, and a control that reports a different
// reason is one nobody can assert on.
func TestValidationOutranksTheOtherRefusals(t *testing.T) {
	srv := admittingServer(t, 20*time.Millisecond)
	client := loadgen.NewClient(srv.URL, 5*time.Second, false)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	s := loadgen.NewRunner(client, loadgen.Options{Concurrency: 2, Iterations: 500}).
		Run(ctx, dispersedOver(4))

	if s.Quotable {
		t.Fatal("run with validation disabled certified itself")
	}
	if !strings.Contains(s.NotQuotableBecause, "validation was disabled") {
		t.Errorf("reason = %q, want the validation control's reason to win",
			s.NotQuotableBecause)
	}
}
