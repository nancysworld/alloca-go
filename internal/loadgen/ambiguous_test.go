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

// A raw idempotency key is unique only within its scope, so the register cannot key on it.
//
// The normative scope is (user organisation, user id, operation, key) — domain.ScopeKey,
// what the service records a mutation under. Two users may legitimately both send "k-1",
// and one user may send "k-1" for a reserve and again for a confirm. Deduplicating on the
// key alone collapses those into one entry, so every mutation after the first is dropped
// from the register: never replayed, never resolved, and invisible in the post-run control
// that exists to catch exactly that.
func TestTheRegisterKeysOnTheFullIdempotencyScopeNotTheRawKey(t *testing.T) {
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
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	// Four genuinely distinct logical mutations, every one of them under the raw key "k-1":
	// two users in one organisation, a third in another, and one repeat of the first user's
	// key under a different operation.
	userOne := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	userTwo := loadgen.User{OrganisationID: "org-a", UserID: "u-2"}
	userThree := loadgen.User{OrganisationID: "org-b", UserID: "u-1"}

	c.Reserve(ctx, userOne, slot, "k-1")
	c.Reserve(ctx, userTwo, slot, "k-1")
	c.Reserve(ctx, userThree, slot, "k-1")
	c.Confirm(ctx, userOne, "res-1", "k-1")

	pending := c.Ambiguous()
	if len(pending) != 4 {
		t.Fatalf("register holds %d entries, want 4: four distinct scopes shared one raw key, "+
			"and every one of them is a mutation that must be replayed", len(pending))
	}

	// And the same scope twice really is one entry — the property the dedup exists for.
	c.Reserve(ctx, userOne, slot, "k-1")
	if n := len(c.Ambiguous()); n != 4 {
		t.Errorf("register holds %d entries after repeating one scope, want 4", n)
	}
}

// A settled mutation is no longer outstanding work.
//
// Leaving it registered compounds: the next pass replays a mutation already known to have
// committed — the amplification the register exists to prevent — and Ambiguous() never
// empties, so a fully resolved run can never report itself reconcilable.
func TestResolvedEntriesAreRetiredAndUnresolvedOnesAreNot(t *testing.T) {
	// "k-stuck" stays ambiguous while its authority is down; every other key resolves on
	// replay. Flipping authorityBack is the authority coming back.
	var requests atomic.Int64
	var authorityBack atomic.Bool
	seenKeys := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if (key == "k-stuck" && !authorityBack.Load()) || !seenKeys[key] {
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
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	ctx := context.Background()
	user := loadgen.User{OrganisationID: "org-a", UserID: "u-1"}
	slot := loadgen.Slot{OrganisationID: "org-a", SlotID: "slot-1"}

	c.Reserve(ctx, user, slot, "k-ok")
	c.Reserve(ctx, user, slot, "k-stuck")
	if n := len(c.Ambiguous()); n != 2 {
		t.Fatalf("register holds %d entries, want 2", n)
	}

	// Pass 1: one resolves, one does not. Only the unresolved entry may remain.
	first := c.ResolveAmbiguous(ctx)
	if len(first) != 2 {
		t.Fatalf("pass 1 resolved %d entries, want 2", len(first))
	}
	if loadgen.Unresolved(first) != 1 {
		t.Fatalf("pass 1 reported %d unresolved, want 1", loadgen.Unresolved(first))
	}
	pending := c.Ambiguous()
	if len(pending) != 1 {
		t.Fatalf("register holds %d entries after pass 1, want 1: the settled mutation was "+
			"not retired", len(pending))
	}
	if pending[0].Key != "k-stuck" {
		t.Errorf("the entry left pending is %q, want k-stuck", pending[0].Key)
	}

	// Pass 2 replays only the entry still outstanding — not the one already settled.
	before := requests.Load()
	second := c.ResolveAmbiguous(ctx)
	if len(second) != 1 {
		t.Fatalf("pass 2 resolved %d entries, want 1: a settled mutation was replayed again",
			len(second))
	}
	if issued := requests.Load() - before; issued != 1 {
		t.Errorf("pass 2 issued %d requests, want 1", issued)
	}

	// Once everything resolves, the register empties and a further pass is a no-op.
	authorityBack.Store(true)
	c.ResolveAmbiguous(ctx)
	if n := len(c.Ambiguous()); n != 0 {
		t.Fatalf("register holds %d entries after everything resolved, want 0: a fully "+
			"resolved run could never report itself reconcilable", n)
	}
	before = requests.Load()
	if final := c.ResolveAmbiguous(ctx); len(final) != 0 {
		t.Errorf("a pass over an empty register resolved %d entries, want 0", len(final))
	}
	if issued := requests.Load() - before; issued != 0 {
		t.Errorf("a pass over an empty register issued %d requests, want 0", issued)
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
