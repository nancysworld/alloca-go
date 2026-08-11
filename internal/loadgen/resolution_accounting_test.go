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

// These cover VAL-COR-6's three states against measurement-contract §12's two populations.
// Each drives the real client through a real resolution pass, so they exercise the same path
// a failure-isolation run does rather than a hand-built Summary.
//
// The property under test is the **population boundary**, and VAL-COR-6 requires it to be
// discriminating in both directions:
//
//   - an implementation that folds resolution traffic into the *measured* population —
//     Completed, Goodput, Totals, ReplayedMutations, latency, duration — must fail, even if its
//     final persisted-row count is right;
//   - an implementation that omits resolution traffic from the *reconciliation* population —
//     the server-scrape comparison — must also fail.
//
// So every case below asserts measured fields are untouched *and* that the reconciliation view
// sees the extra attempts.

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

func totalIn(totals []loadgen.Total, outcome domain.Outcome, replay bool) int {
	n := 0
	for _, t := range totals {
		if t.Outcome == outcome && t.Replay == replay {
			n += t.Count
		}
	}
	return n
}

// countIn reads the *measured* cells specifically, so a test asserting measured fields cannot
// accidentally be satisfied by the reconciliation view.
func countIn(s loadgen.Summary, outcome domain.Outcome, replay bool) int {
	return totalIn(s.Totals, outcome, replay)
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
	measured := summaryOf(original)
	summary := measured.WithResolutions(c.ResolveAmbiguous(ctx))

	// Measured population: untouched. The client saw one request and no definite success
	// inside the interval, and a later discovery does not change what happened during it.
	if summary.Completed != 1 {
		t.Errorf("measured completed = %d, want 1 — the resolution happened after the interval", summary.Completed)
	}
	if summary.Goodput != 0 {
		t.Errorf("measured goodput = %d, want 0. The mutation committed, but not observably "+
			"inside the measured interval; crediting it here rewrites performance history", summary.Goodput)
	}
	if summary.ReplayedMutations != 0 {
		t.Errorf("measured replayed mutations = %d, want 0 — the replay was post-run", summary.ReplayedMutations)
	}
	if got := countIn(summary, domain.OutcomeUnknownReplayable, false); got != 1 {
		t.Errorf("unknown_replayable cells = %d, want 1: the original's outcome is what the client saw", got)
	}
	if got := countIn(summary, domain.OutcomeAdmittedSuccess, true); got != 0 {
		t.Errorf("measured Totals gained %d resolution cells, want 0", got)
	}

	// Reconciliation population: the resolution attempt is visible and counted.
	if got := summary.ReconciliationCompleted(); got != 2 {
		t.Errorf("reconciliation completed = %d, want 2 — a scrape taken after resolution "+
			"counts both attempts, and omitting it fails a correct service", got)
	}
	if got := totalIn(summary.ReconciliationTotals(), domain.OutcomeAdmittedSuccess, true); got != 1 {
		t.Errorf("reconciliation admitted_success replay=true cells = %d, want 1", got)
	}

	// Final logical state: exactly one mutation for the key.
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 1 {
		t.Errorf("FreshAdmittedFor = %d, want 1: the replay proves the original committed, "+
			"so the database holds one row", got)
	}
	if got := summary.FreshMutations(); got != 1 {
		t.Errorf("FreshMutations = %d, want 1: the original wrote an idempotency record", got)
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

	// Measured population: untouched, exactly as in the committed branch. The mutation
	// happened *after* the interval, so measured goodput is zero either way — this is the
	// case where folding it in is most tempting and most wrong.
	if summary.Completed != 1 {
		t.Errorf("measured completed = %d, want 1", summary.Completed)
	}
	if summary.Goodput != 0 {
		t.Errorf("measured goodput = %d, want 0 — the resolution performed the mutation "+
			"outside the measured interval", summary.Goodput)
	}
	if got := countIn(summary, domain.OutcomeAdmittedSuccess, false); got != 0 {
		t.Errorf("measured Totals gained %d resolution cells, want 0", got)
	}

	// Reconciliation population sees it.
	if got := summary.ReconciliationCompleted(); got != 2 {
		t.Errorf("reconciliation completed = %d, want 2", got)
	}
	if got := totalIn(summary.ReconciliationTotals(), domain.OutcomeAdmittedSuccess, false); got != 1 {
		t.Errorf("reconciliation admitted_success replay=false cells = %d, want 1 — the resolution performed it", got)
	}

	// Final logical state: one mutation, and exactly one. This branch is why the credit
	// cannot be conditioned on replay=true — the row exists here too, and the database
	// cannot tell the two branches apart.
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 1 {
		t.Errorf("FreshAdmittedFor = %d, want 1: the resolution performed the mutation, so "+
			"the database holds one row for this key", got)
	}
	if got := summary.FreshMutations(); got != 1 {
		t.Errorf("FreshMutations = %d, want 1", got)
	}
	if summary.ReplayedMutations != 0 {
		t.Errorf("measured replayed mutations = %d, want 0", summary.ReplayedMutations)
	}
	if !summary.Sound {
		t.Errorf("run is unsound after a successful resolution: %s", summary.NotSoundBecause)
	}
	if len(summary.Resolved) != 1 || summary.Resolved[0].Replay {
		t.Errorf("resolved record = %+v, want one entry with replay=false", summary.Resolved)
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
	// It is still an HTTP request the service completed, so the reconciliation population
	// must carry it even though it settles nothing.
	if got := summary.ReconciliationCompleted(); got != 2 {
		t.Errorf("reconciliation completed = %d, want 2: the failed resolution attempt still "+
			"reached the service and still appears in its counters", got)
	}
	if len(summary.Resolved) != 1 || !summary.Resolved[0].StillAmbiguous {
		t.Errorf("resolved record = %+v, want one entry flagged still-ambiguous", summary.Resolved)
	}
}

// State 3, the other way it arrives: the resolution pass ran while the authority was still
// down, so the replay never reached a domain answer at all.
//
// **This is the discriminating case for the settlement predicate.** Testing for
// `unknown_replayable` alone — which is what an earlier version did — makes a transport
// failure look like a resolution: the entry is retired, the run reports nothing outstanding,
// and a key whose commit state nobody established is certified. It is also irreversible,
// because a transport failure never re-enters the register: only a parsed `unknown_replayable`
// is recorded, so the mutation could never be replayed again.
//
// Resolving too early is the expected operator error, not an exotic one — the authority being
// down is precisely the condition that produced the ambiguity.
func TestResolutionThatNeverReachedTheAuthorityLeavesTheMutationAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeUnknownReplayable, "replay": false,
		})
	}))

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	original := c.Reserve(ctx, user, slot, "k-1")
	if original.Outcome != domain.OutcomeUnknownReplayable {
		t.Fatalf("setup: original outcome = %q, want unknown_replayable", original.Outcome)
	}

	// The authority goes away before the resolution pass, so the replay fails at the
	// transport rather than being answered.
	srv.Close()

	resolutions := c.ResolveAmbiguous(ctx)
	if n := loadgen.Unresolved(resolutions); n != 1 {
		t.Fatalf("Unresolved = %d, want 1: a replay that never reached the service settles "+
			"nothing about whether the original committed", n)
	}
	if pending := c.Ambiguous(); len(pending) != 1 {
		t.Fatalf("register holds %d entries, want 1: an unsettled mutation must stay "+
			"outstanding so a later pass can replay it once the authority is back", len(pending))
	}

	summary := summaryOf(original).WithResolutions(resolutions)
	if summary.Sound {
		t.Error("run is sound with a mutation whose commit state was never established")
	}
	if got := summary.FreshAdmittedFor(domain.OpReserve); got != 0 {
		t.Errorf("FreshAdmittedFor = %d, want 0", got)
	}
	if len(summary.Resolved) != 1 || !summary.Resolved[0].StillAmbiguous {
		t.Errorf("resolved record = %+v, want one entry flagged still-ambiguous", summary.Resolved)
	}
}

// A resolution response that contradicts the outcome contract makes the run unsound, exactly
// as it would inside the measured interval. Post-run traffic is not exempt from validation:
// the artifact is only worth reading if the service answered within the contract throughout.
func TestInvalidResolutionResponseMakesTheRunUnsound(t *testing.T) {
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
		// A definite outcome under a status the contract maps elsewhere: settled as a
		// logical mutation, and not a response this run may be read through.
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeAdmittedSuccess, "replay": true, "reservation_id": "res-1",
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
		t.Error("run is sound although the service answered the replay outside the contract")
	}
	if !strings.Contains(summary.NotSoundBecause, "validation") {
		t.Errorf("not_sound_because = %q, want it to name the failed validation", summary.NotSoundBecause)
	}
	if len(summary.Resolved) != 1 || summary.Resolved[0].Invalid == "" {
		t.Errorf("resolved record = %+v, want the validation complaint retained", summary.Resolved)
	}
	if summary.Invalid != 0 {
		t.Errorf("measured invalid_responses = %d, want 0: the resolution ran after the "+
			"measured interval and must not be added to its counts", summary.Invalid)
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
