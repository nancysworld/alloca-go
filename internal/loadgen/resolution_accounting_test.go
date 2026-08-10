package loadgen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// These cover VAL-COR-6's three states against measurement-contract §12's accounting rule.
// Each drives the real client through a real resolution pass, so they exercise the same path
// a failure-isolation run does rather than a hand-built Summary.
//
// The property under test is that the two accountings §12 distinguishes stay separate and
// both stay correct: every HTTP attempt appears in Totals, while one idempotency key
// contributes exactly one fresh logical mutation to the persisted-state comparison.

// ambiguousThenPerformed answers the first request for each key with unknown_replayable and
// every later one with a *fresh* success — the shape a real authority produces when the
// commit did not land, so the replay performs the mutation itself.
//
// It is the counterpart of ambiguousThenCommitted, which answers replay=true.
func ambiguousThenPerformed(t *testing.T) *httptest.Server {
	t.Helper()
	seenKeys := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")

		if !seenKeys[key] {
			seenKeys[key] = true
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"outcome": domain.OutcomeUnknownReplayable, "replay": false,
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeAdmittedSuccess, "replay": false, "reservation_id": "res-1",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// summaryOf builds the request-accounting summary for one original attempt, as Runner would.
func summaryOf(r loadgen.Response) loadgen.Summary {
	s := loadgen.Summary{Completed: 1, Sound: true, ValidationEnabled: true}
	s.Totals = []loadgen.Total{{
		Operation: string(r.Operation), Outcome: r.Outcome,
		Reason: r.Reason, Replay: r.Replay, Count: 1,
	}}
	if r.Outcome == domain.OutcomeAdmittedSuccess && r.Operation.IsKnown() && !r.Replay {
		s.Goodput = 1
	}
	return s
}

func countIn(s loadgen.Summary, outcome domain.Outcome, replay bool) int {
	n := 0
	for _, t := range s.Totals {
		if t.Outcome == outcome && t.Replay == replay {
			n += t.Count
		}
	}
	return n
}

// State 1: the original committed. The replay proves it, so the fresh logical mutation
// belongs to the original attempt even though no cell in Totals reports it as fresh.
//
// **This is the discriminating case.** Removing the credit in FreshAdmittedFor leaves it at
// zero while the database holds a row, which is exactly the defect ag-sept-pr3.md §6c named:
// "the mutation is real and the summary would say it never happened".
func TestResolvedOriginalCommittedCreditsOneFreshMutationToTheOriginal(t *testing.T) {
	srv, _ := ambiguousThenCommitted(t)
	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()

	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	original := c.Reserve(ctx, user, slot, "k-1")
	if original.Outcome != domain.OutcomeUnknownReplayable {
		t.Fatalf("setup: original outcome = %q, want unknown_replayable", original.Outcome)
	}
	summary := summaryOf(original).WithResolutions(c.ResolveAmbiguous(ctx))

	// Request accounting: both attempts present, as observed.
	if summary.Completed != 2 {
		t.Errorf("completed = %d, want 2 — the original and the resolution are both requests", summary.Completed)
	}
	if got := countIn(summary, domain.OutcomeUnknownReplayable, false); got != 1 {
		t.Errorf("unknown_replayable cells = %d, want 1: the original's own outcome is what the client saw and must not be rewritten", got)
	}
	if got := countIn(summary, domain.OutcomeAdmittedSuccess, true); got != 1 {
		t.Errorf("admitted_success replay=true cells = %d, want 1", got)
	}

	// Logical-mutation accounting: exactly one fresh mutation for the key.
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 1 {
		t.Errorf("FreshAdmittedFor = %d, want 1. Without the §12 credit this reads 0 while the "+
			"database holds one row, and reconciliation fails a correct service", got)
	}
	if got := summary.FreshMutations(); got != 1 {
		t.Errorf("FreshMutations = %d, want 1: the original wrote an idempotency record", got)
	}
	if summary.Goodput != 1 {
		t.Errorf("goodput = %d, want 1 — a booking that committed is a completed useful operation", summary.Goodput)
	}
	if summary.ReplayedMutations != 1 {
		t.Errorf("replayed mutations = %d, want 1", summary.ReplayedMutations)
	}
	if !summary.Sound {
		t.Errorf("run is unsound after a successful resolution: %s", summary.NotSoundBecause)
	}
	if len(summary.Resolved) != 1 || summary.Resolved[0].Key != "k-1" || !summary.Resolved[0].Replay {
		t.Errorf("resolved record = %+v, want one entry for k-1 with replay=true", summary.Resolved)
	}
}

// State 2: the original did not commit, so the resolution performs the mutation. The fresh
// count must come from the resolution's own cell and must NOT be credited a second time.
func TestResolvedOriginalNotCommittedCountsTheResolutionOnce(t *testing.T) {
	srv := ambiguousThenPerformed(t)
	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()

	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	original := c.Reserve(ctx, user, slot, "k-1")
	if original.Outcome != domain.OutcomeUnknownReplayable {
		t.Fatalf("setup: original outcome = %q, want unknown_replayable", original.Outcome)
	}
	summary := summaryOf(original).WithResolutions(c.ResolveAmbiguous(ctx))

	if summary.Completed != 2 {
		t.Errorf("completed = %d, want 2", summary.Completed)
	}
	if got := countIn(summary, domain.OutcomeAdmittedSuccess, false); got != 1 {
		t.Errorf("admitted_success replay=false cells = %d, want 1 — the resolution performed it", got)
	}

	// Exactly one, not two: the credit must not fire for a replay=false resolution, whose
	// own cell is already fresh.
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 1 {
		t.Errorf("FreshAdmittedFor = %d, want 1. Two would mean the resolution was counted "+
			"both as its own fresh cell and as a credit to the original", got)
	}
	if got := summary.FreshMutations(); got != 1 {
		t.Errorf("FreshMutations = %d, want 1", got)
	}
	if summary.Goodput != 1 {
		t.Errorf("goodput = %d, want 1", summary.Goodput)
	}
	if summary.ReplayedMutations != 0 {
		t.Errorf("replayed mutations = %d, want 0 — nothing was replayed, the mutation was performed", summary.ReplayedMutations)
	}
	if !summary.Sound {
		t.Errorf("run is unsound after a successful resolution: %s", summary.NotSoundBecause)
	}
}

// State 3: the replay was itself ambiguous. The run must be refused rather than producing a
// verdict, because persisted state and the client record still disagree.
func TestStillAmbiguousAfterResolutionMakesTheRunUnsound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeUnknownReplayable, "replay": false,
		})
	}))
	t.Cleanup(srv.Close)

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	original := c.Reserve(ctx, user, slot, "k-1")
	summary := summaryOf(original).WithResolutions(c.ResolveAmbiguous(ctx))

	if summary.Sound {
		t.Fatal("run is sound with a mutation still ambiguous: reconciliation over it would " +
			"compare a client record against persisted state that may or may not exist")
	}
	if !strings.Contains(summary.NotSoundBecause, "ambiguous") {
		t.Errorf("not_sound_because = %q, want it to name the unresolved ambiguity", summary.NotSoundBecause)
	}
	// A still-ambiguous entry establishes nothing, so it must not be credited either way.
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 0 {
		t.Errorf("FreshAdmittedFor = %d, want 0: an unresolved mutation proves nothing about persisted state", got)
	}
	if len(summary.Resolved) != 1 || !summary.Resolved[0].StillAmbiguous {
		t.Errorf("resolved record = %+v, want one entry flagged still-ambiguous", summary.Resolved)
	}
}

// A run that produced no ambiguity must be untouched by the fold — the common case, and the
// control that the credit cannot fire spontaneously.
func TestWithResolutionsIsANoOpWhenNothingWasAmbiguous(t *testing.T) {
	before := summaryOf(loadgen.Response{
		Operation: domain.OpReserve, Outcome: domain.OutcomeAdmittedSuccess,
	})
	after := before.WithResolutions(nil)

	if after.Completed != before.Completed || after.Goodput != before.Goodput ||
		len(after.Totals) != len(before.Totals) || after.Resolved != nil {
		t.Errorf("empty resolution pass changed the summary:\n before %+v\n after  %+v", before, after)
	}
	if got := after.FreshAdmittedFor(domain.OpReserve); got != 1 {
		t.Errorf("FreshAdmittedFor = %d, want 1", got)
	}
}
