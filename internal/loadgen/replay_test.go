package loadgen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// idempotentServer is a minimal stand-in for the service's idempotency behaviour: the first
// request under a key commits and records, and every later request under the same key returns
// the recorded outcome with replay=true.
//
// mutate lets a test break exactly one aspect of the replay contract while leaving the rest
// correct, which is what makes each case below discriminating rather than merely failing.
func idempotentServer(t *testing.T, mutate func(seen int, body map[string]any)) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	recorded := map[string]map[string]any{}
	counts := map[string]int{}
	next := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := r.Header.Get("Idempotency-Key")

		mu.Lock()
		counts[k]++
		seen := counts[k]
		body, ok := recorded[k]
		if !ok {
			next++
			body = map[string]any{
				"outcome":        "admitted_success",
				"replay":         false,
				"reservation_id": "res_" + strings.Repeat("a", 6) + string(rune('0'+next%10)),
			}
			recorded[k] = body
		}
		mu.Unlock()

		// Copy before mutating so one response's alteration cannot leak into the record.
		out := map[string]any{}
		for k, v := range body {
			out[k] = v
		}
		if seen > 1 {
			out["replay"] = true
		}
		if mutate != nil {
			mutate(seen, out)
		}

		// Status is derived from the outcome the body carries, so every response this
		// fixture emits is a *validly mapped* one. That is what makes the cases below
		// discriminating: a mismatched status would be caught by the existing status/outcome
		// validator, and the replay rule would never be the thing under test.
		status := http.StatusOK
		if out["outcome"] == "business_refusal" {
			status = http.StatusConflict
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func replayRun(t *testing.T, srv *httptest.Server, iterations int) loadgen.Summary {
	t.Helper()
	client := loadgen.NewClient(srv.URL, 5*time.Second, true)
	return loadgen.NewRunner(client, loadgen.Options{Concurrency: 4, Iterations: iterations}).
		Run(context.Background(), loadgen.Replay{Org: "load-org", Slots: dispersedSlots(4)})
}

func dispersedSlots(n int) []loadgen.Slot {
	out := make([]loadgen.Slot, n)
	for i := range out {
		out[i] = loadgen.Slot{OrganisationID: "load-org", SlotID: "slot-0"}
	}
	return out
}

// TestReplayControlAcceptsACorrectService is the positive control, and it has to come first:
// every negative case below would also pass against a check that rejected everything.
//
// It also pins the accounting the whole change rests on — the replays are counted, and they
// are counted *separately* from goodput rather than folded into it.
func TestReplayControlAcceptsACorrectService(t *testing.T) {
	const units = 20
	s := replayRun(t, idempotentServer(t, nil), units)

	if !s.Sound {
		t.Fatalf("a correct idempotent service was rejected: %s", s.NotSoundBecause)
	}
	if s.Completed != units*2 {
		t.Errorf("completed = %d, want %d (two requests per logical unit)", s.Completed, units*2)
	}
	if s.Goodput != units {
		t.Errorf("goodput = %d, want %d — only the fresh mutations count", s.Goodput, units)
	}
	if s.ReplayedMutations != units {
		t.Errorf("replayed_mutations = %d, want %d", s.ReplayedMutations, units)
	}
	if s.Goodput+s.ReplayedMutations != s.Completed {
		t.Errorf("goodput %d + replays %d does not account for %d completed requests",
			s.Goodput, s.ReplayedMutations, s.Completed)
	}
}

// TestReplayControlCatchesTheDefects drives each way a service can violate
// measurement-contract §4.2 while still answering with a well-formed, correctly-mapped
// response — which is precisely the class of failure a status/outcome validator cannot see.
func TestReplayControlCatchesTheDefects(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(seen int, body map[string]any)
		mention string
	}{
		{
			// The mutation ran twice. Both responses are valid admitted successes, the
			// totals are internally consistent, and only the flag reveals it.
			name:    "second request re-ran the mutation",
			mutate:  func(seen int, body map[string]any) { body["replay"] = false },
			mention: "replay=false",
		},
		{
			// A replay that reports a different terminal outcome than the one recorded.
			name: "replay returned a different outcome",
			mutate: func(seen int, body map[string]any) {
				if seen > 1 {
					body["outcome"] = "business_refusal"
					body["reason"] = "no_capacity"
				}
			},
			mention: "outcome",
		},
		{
			// The subtlest: correct outcome, correct flag, but a *different* reservation —
			// so a second unit of capacity was consumed while reporting the first's result.
			name: "replay admitted a different reservation",
			mutate: func(seen int, body map[string]any) {
				if seen > 1 {
					body["reservation_id"] = "res_bbbbbbb9"
				}
			},
			mention: "reservation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := replayRun(t, idempotentServer(t, tc.mutate), 10)

			if s.Sound {
				t.Fatalf("a service violating §4.2 produced a sound run: goodput=%d replays=%d",
					s.Goodput, s.ReplayedMutations)
			}
			if s.Invalid == 0 {
				t.Fatal("no response was marked invalid, so the run failed for some other reason")
			}
			if !strings.Contains(strings.Join(s.InvalidSamples, " "), tc.mention) {
				t.Errorf("no invalid sample names %q: %v", tc.mention, s.InvalidSamples)
			}
		})
	}
}

// TestReplayDefectIgnoresAnUnestablishedFirstOutcome guards the opposite failure. When the
// first request never established a recorded outcome, the second is not evidence about the
// idempotency path, and blaming it would send an operator to the wrong subsystem.
func TestReplayDefectIgnoresAnUnestablishedFirstOutcome(t *testing.T) {
	// A first response that already failed validation carries its own reason; the replay
	// check must not overwrite it with a complaint about replay=false.
	first := loadgen.Response{Invalid: "status 500 does not match outcome admitted_success"}
	second := loadgen.Response{Replay: false}

	if got := loadgen.ReplayDefectForTest(first, second); got != "" {
		t.Errorf("replay check fired on an unestablished first outcome: %q", got)
	}
}
