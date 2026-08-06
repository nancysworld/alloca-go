package loadgen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// ambiguousThenCommitted answers the first request for each key with unknown_replayable
// and every later one with the recorded success — the shape a real authority produces
// when a commit's acknowledgement is lost and the mutation did land.
func ambiguousThenCommitted(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var seen atomic.Int64
	seenKeys := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		seen.Add(1)
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
			"outcome": domain.OutcomeAdmittedSuccess, "replay": true, "reservation_id": "res-1",
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestAmbiguousMutationsAreRegisteredAndResolvedUnderTheirOwnKeys(t *testing.T) {
	srv, seen := ambiguousThenCommitted(t)
	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()

	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	first := c.Reserve(ctx, user, slot, "k-1")
	if first.Outcome != domain.OutcomeUnknownReplayable {
		t.Fatalf("outcome = %q, want %q", first.Outcome, domain.OutcomeUnknownReplayable)
	}

	pending := c.Ambiguous()
	if len(pending) != 1 {
		t.Fatalf("register holds %d entries, want 1", len(pending))
	}
	if pending[0].Key != "k-1" || pending[0].Operation != string(domain.OpReserve) {
		t.Errorf("registered %+v, want the reserve under k-1", pending[0])
	}

	before := seen.Load()
	resolutions := c.ResolveAmbiguous(ctx)
	if len(resolutions) != 1 {
		t.Fatalf("resolved %d entries, want 1", len(resolutions))
	}
	if got := seen.Load() - before; got != 1 {
		t.Errorf("resolution issued %d requests for one entry, want exactly 1: this pass must never amplify", got)
	}
	if loadgen.Unresolved(resolutions) != 0 {
		t.Error("the entry is still ambiguous after a successful resolution")
	}

	// Replay=true is the fact that matters: the original mutation *had* committed, and
	// the resolution returned its recorded outcome rather than performing a second one.
	got := resolutions[0].Response
	if !got.Replay || got.Outcome != domain.OutcomeAdmittedSuccess {
		t.Errorf("resolution returned %q replay=%t, want admitted_success replay=true",
			got.Outcome, got.Replay)
	}
}

// A resolution that is itself ambiguous must stay flagged. Reporting it as resolved would
// let a run be reconciled while a mutation's persisted state was still unknown.
func TestResolutionThatIsStillAmbiguousIsReportedUnresolved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeUnknownReplayable, "replay": false,
		})
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	c.Reserve(ctx, loadgen.User{OrganisationID: "org-a", UserID: "u-1"},
		loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}, "k-1")

	resolutions := c.ResolveAmbiguous(ctx)
	if n := loadgen.Unresolved(resolutions); n != 1 {
		t.Fatalf("unresolved = %d, want 1: the authority is still unavailable", n)
	}
}

// The register must not grow when resolution fails.
//
// Resolution replays through the ordinary request path, so a replay that is itself
// ambiguous comes back to the register under the original's key. If that appended, an
// authority that stays down would double the outstanding work on every pass: the second
// pass would replay one logical mutation twice, the third four times, and the post-run
// control would report mutations the run never issued. The expected operator behaviour —
// wait, retry the resolution, wait again — is exactly what triggers it.
func TestRepeatedFailedResolutionDoesNotGrowTheRegister(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeUnknownReplayable, "replay": false,
		})
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	c.Reserve(ctx, loadgen.User{OrganisationID: "org-a", UserID: "u-1"},
		loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}, "k-1")

	if n := len(c.Ambiguous()); n != 1 {
		t.Fatalf("register holds %d entries after one ambiguous reserve, want 1", n)
	}

	for pass := 1; pass <= 3; pass++ {
		before := requests.Load()
		resolutions := c.ResolveAmbiguous(ctx)

		if len(resolutions) != 1 {
			t.Fatalf("pass %d resolved %d entries, want 1: one logical mutation is outstanding",
				pass, len(resolutions))
		}
		if issued := requests.Load() - before; issued != 1 {
			t.Fatalf("pass %d issued %d requests for one outstanding mutation, want 1: "+
				"a resolution pass must never replay a mutation more than once", pass, issued)
		}
		if n := len(c.Ambiguous()); n != 1 {
			t.Fatalf("register holds %d entries after pass %d, want 1: the failed replay "+
				"registered itself as new work", n, pass)
		}
		if loadgen.Unresolved(resolutions) != 1 {
			t.Fatalf("pass %d reported the mutation resolved, but the authority is still down", pass)
		}
	}
}

// A healthy run registers nothing, so the resolution pass is a no-op rather than
// something a run has to remember not to do.
func TestHealthyRunRegistersNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeAdmittedSuccess, "replay": false, "reservation_id": "res-1",
		})
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	c.Reserve(ctx, loadgen.User{OrganisationID: "org-a", UserID: "u-1"},
		loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}, "k-1")

	if got := c.Ambiguous(); len(got) != 0 {
		t.Fatalf("healthy run registered %d ambiguous mutations, want 0", len(got))
	}
	if got := c.ResolveAmbiguous(ctx); len(got) != 0 {
		t.Errorf("resolution pass did %d replays on a healthy run, want 0", len(got))
	}
}

// Confirm and cancel are registered by the path they used, so a resolution reissues the
// request that was made rather than one reconstructed from remembered parts.
func TestConfirmAndCancelAreResolvedAgainstTheirOwnReservations(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeUnknownReplayable, "replay": false,
		})
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}

	c.Confirm(ctx, user, "res-7", "k-confirm")
	c.Cancel(ctx, user, "res-9", "k-cancel")

	paths = nil
	c.ResolveAmbiguous(ctx)

	if len(paths) != 2 {
		t.Fatalf("resolution issued %d requests, want 2", len(paths))
	}
	if paths[0] != "/v1/reservations/res-7/confirm" {
		t.Errorf("confirm resolved against %q", paths[0])
	}
	if paths[1] != "/v1/reservations/res-9/cancel" {
		t.Errorf("cancel resolved against %q", paths[1])
	}
}
