package loadgen_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// TestDurationBoundedRunCoversItsWindow is the property the whole flag exists for: a sweep
// cell must cover the interval it says it covers, whatever throughput it reaches.
//
// The server here answers instantly, so an iteration-bounded run of any reasonable -n would
// finish in milliseconds — the failure mode that makes rate() unusable across a sweep. The
// duration bound must hold the window open regardless.
func TestDurationBoundedRunCoversItsWindow(t *testing.T) {
	srv := admittingServer(t, 0)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	const window = 300 * time.Millisecond
	start := time.Now()
	s := loadgen.NewRunner(client, loadgen.Options{
		Concurrency: 4, Duration: window,
	}).Run(context.Background(), dispersedOver(4))
	elapsed := time.Since(start)

	if elapsed < window {
		t.Errorf("run returned after %s, before its %s window elapsed", elapsed, window)
	}
	// Generous upper bound: this asserts the run *stops*, not how promptly. A worker finishes
	// the unit it is on before checking the clock, which is deliberate — see Runner.Run.
	if elapsed > window*4 {
		t.Errorf("run took %s for a %s window; the deadline is not being observed",
			elapsed, window)
	}
	if !s.Sound {
		t.Fatalf("a completed duration run was rejected: %s", s.NotSoundBecause)
	}
	if s.CompletedIterations == 0 {
		t.Error("no logical units completed, so the window was open but nothing ran")
	}
	if s.DurationRequestedSeconds != window.Seconds() {
		t.Errorf("duration_requested_seconds = %v, want %v",
			s.DurationRequestedSeconds, window.Seconds())
	}
}

// TestDurationRunIsNotJudgedAgainstAnIterationCount is the regression this refactor is most
// likely to reintroduce.
//
// The soundness rule was "completed < requested iterations = truncated". Applied to a
// duration-bounded run it would compare against Options.Iterations — which a sweep never sets
// — and refuse every cell, most severely at the frontier where units are slowest and fewest.
// A run that completes few units in its window is reporting a result, not a shortfall.
func TestDurationRunIsNotJudgedAgainstAnIterationCount(t *testing.T) {
	// 40ms per unit against a 200ms window: a handful of units at most, far below any
	// plausible iteration default.
	srv := admittingServer(t, 40*time.Millisecond)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	s := loadgen.NewRunner(client, loadgen.Options{
		Concurrency: 1, Duration: 200 * time.Millisecond,
	}).Run(context.Background(), dispersedOver(4))

	if s.CompletedIterations >= 100 {
		t.Fatalf("completed %d units, which is not the slow-workload case this test needs",
			s.CompletedIterations)
	}
	if !s.Sound {
		t.Fatalf("a slow duration run was called truncated: %s", s.NotSoundBecause)
	}
}

// TestInterruptedDurationRunIsNotSound is the other half: the clock is what truncates a
// duration run, and a cancelled one must still be refused.
func TestInterruptedDurationRunIsNotSound(t *testing.T) {
	srv := admittingServer(t, 20*time.Millisecond)
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	s := loadgen.NewRunner(client, loadgen.Options{
		Concurrency: 2, Duration: 10 * time.Second,
	}).Run(ctx, dispersedOver(4))

	if s.Sound {
		t.Fatal("a duration run cancelled well inside its window certified itself")
	}
	if !strings.Contains(s.NotSoundBecause, "window") {
		t.Errorf("reason = %q, want it to name the unfinished window", s.NotSoundBecause)
	}
}

// TestManifestRecordsWhichBoundApplied pins that the manifest cannot imply a request nobody
// made. -n carries a default, so a duration-bounded run that recorded it would describe an
// iteration count the operator never asked for — and Validate would then be checking the
// wrong field for the wrong mode.
func TestManifestRecordsWhichBoundApplied(t *testing.T) {
	svc := loadgen.ServiceMeta{
		Revision:  "aaaa111111111111111111111111111111111111",
		GoVersion: "go1.26.5", GOMAXPROCS: 4,
		RequestBudget: map[string]string{"lock_timeout": "2s"}, ReservationTTL: "2m0s",
	}

	byDuration := loadgen.NewManifest("http://localhost:8080", "dispersed",
		loadgen.Options{Concurrency: 8, Duration: 30 * time.Second}, "local", svc)
	if byDuration.Iterations != 0 {
		t.Errorf("duration-bounded manifest records iterations = %d, implying a request that "+
			"was never made", byDuration.Iterations)
	}
	if byDuration.Duration != "30s" {
		t.Errorf("duration = %q, want 30s", byDuration.Duration)
	}

	byIterations := loadgen.NewManifest("http://localhost:8080", "dispersed",
		loadgen.Options{Concurrency: 8, Iterations: 60}, "local", svc)
	if byIterations.Duration != "" {
		t.Errorf("iteration-bounded manifest records duration = %q", byIterations.Duration)
	}
	if byIterations.Iterations != 60 {
		t.Errorf("iterations = %d, want 60", byIterations.Iterations)
	}

	// Both bounds must pass the local gate on this axis; neither must not.
	for name, m := range map[string]loadgen.Manifest{
		"duration": byDuration, "iterations": byIterations,
	} {
		for _, missing := range m.Validate(loadgen.LevelLocal) {
			if strings.Contains(missing, "iterations") || strings.Contains(missing, "duration") {
				t.Errorf("%s-bounded manifest rejected on its own bound: %s", name, missing)
			}
		}
	}

	var neither loadgen.Manifest
	if got := neither.Validate(loadgen.LevelLocal); !containsSubstring(got, "states no size") {
		t.Errorf("a manifest with no bound was not rejected for it: %v", got)
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
