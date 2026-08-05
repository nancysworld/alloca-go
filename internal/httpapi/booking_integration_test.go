//go:build integration

// These tests drive the assembled service — HTTP handler over the real service, over the
// real repository, over a real PostgreSQL — and assert on the wire contract and on
// persisted rows. They are the only tests that prove the vertical path holds together.
//
// They live in the external test package httpapi_test rather than in httpapi, because
// wiring them needs postgres and project-structure §4 does not let httpapi import it. An
// external test package is not part of the package's dependency graph (`go list -deps
// ./internal/httpapi` shows no postgres), so the rule stays satisfied; and like cmd, a
// vertical test is a composition root, which is why it is allowed to know about everything.
//
// Deliberately NOT re-tested here: capacity safety under concurrency, the user-schedule
// non-overlap invariant, and idempotency under contention. Those are properties of the
// transactional core, proven in internal/postgres where the contention is, and repeating
// them through a handler would only make them slower to run and easier to misread.
//
// The response bodies are decoded into structs declared in this file rather than into
// httpapi's own types. That is on purpose: it asserts the JSON *contract* a client depends
// on, so renaming an internal field cannot silently pass.
package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/httpapi"
	"github.com/nancysworld/alloca-go/internal/ids"
	"github.com/nancysworld/alloca-go/internal/postgres"
	"github.com/nancysworld/alloca-go/internal/service"
	"github.com/nancysworld/alloca-go/internal/telemetry"
	"github.com/nancysworld/alloca-go/internal/worker"
)

var databaseURL string

// TestMain delegates so the setup can use defer: os.Exit skips deferred calls, and the
// database claim has to be released on every exit path.
func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	databaseURL = os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "integration tests require DATABASE_URL")
		return 1
	}

	// Claimed before the migration, which mutates the same shared schema. This suite and
	// internal/postgres' both truncate the whole database, and `go test ./...` runs their
	// binaries in parallel, so the claim is what stops them deleting each other's world
	// (postgres.ClaimDatabase).
	release, err := postgres.ClaimDatabase(databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		return 1
	}
	return m.Run()
}

const (
	testOrg   = domain.OrganisationID("org-1")
	otherOrg  = domain.OrganisationID("org-2")
	testSlot  = domain.SlotID("slot-1")
	otherSlot = domain.SlotID("slot-2")
)

// --- wire contract ----------------------------------------------------------

// bookingBody is the JSON every mutation endpoint returns.
type bookingBody struct {
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	Replay        bool   `json:"replay"`
	ReservationID string `json:"reservation_id"`
	BookingID     string `json:"booking_id"`
	Message       string `json:"message"`
}

// slotsBody is the JSON the read route returns.
type slotsBody struct {
	Slots []struct {
		SlotOrganisationID string    `json:"slot_organisation_id"`
		SlotID             string    `json:"slot_id"`
		ResourceID         string    `json:"resource_id"`
		Capacity           int       `json:"capacity"`
		StartsAt           time.Time `json:"starts_at"`
	} `json:"slots"`
	Truncated bool `json:"truncated"`
}

// --- harness ----------------------------------------------------------------

// vertical is the whole service assembled over one migrated, empty database: the same
// components cmd/alloca-go wires, in the same order.
type vertical struct {
	repo    *postgres.Repo
	pool    *pgxpool.Pool
	handler http.Handler
	expiry  *worker.Expiry
}

func newVertical(t *testing.T, ttl time.Duration) *vertical {
	t.Helper()
	cfg := config.Default()
	cfg.ReservationTTL = ttl
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config is invalid: %v", err)
	}

	pool, err := postgres.OpenPool(context.Background(), databaseURL, cfg.RequestBudget)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := postgres.New(pool, cfg.RequestBudget)
	if err := repo.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// The production ID generator, so the tests also see the identifier format clients
	// receive. Nothing here depends on identifiers being predictable.
	svc := service.New(repo, ids.Random{}, ttl, domain.Placement{})
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	recorder := telemetry.NewSlogRecorder(quiet)

	srv := httpapi.New(cfg, func() buildinfo.Info { return buildinfo.Collect(time.Now()) },
		httpapi.Options{
			Service:  svc,
			Slots:    repo,
			Recorder: recorder,
			Logger:   quiet,
			Ready:    repo.Ready,
		})

	return &vertical{
		repo:    repo,
		pool:    pool,
		handler: srv.Handler(),
		expiry:  worker.NewExpiry(repo, svc, recorder, quiet, worker.Config{}),
	}
}

// seedSlot creates a slot owned by testOrg whose window opens an hour ago and starts at
// startsIn.
//
// It reads the host clock, unlike internal/postgres' helper. Nothing here turns on an
// exact interval boundary — the windows have an hour of slack — and the instant that does
// matter, the hold's expiry, is computed by the service from the database's own
// clock_timestamp() regardless of what this clock says.
func (v *vertical) seedSlot(t *testing.T, id domain.SlotID, capacity int, startsIn time.Duration) domain.Slot {
	t.Helper()
	return v.seedSlotIn(t, testOrg, id, capacity, startsIn)
}

func (v *vertical) seedSlotIn(
	t *testing.T, org domain.OrganisationID, id domain.SlotID, capacity int, startsIn time.Duration,
) domain.Slot {
	t.Helper()
	now := time.Now().UTC()
	slot := domain.Slot{
		ID:             id,
		OrganisationID: org,
		ResourceID:     "yoga",
		Capacity:       capacity,
		ReleaseAt:      now.Add(-time.Hour),
		StartsAt:       now.Add(startsIn),
		EndsAt:         now.Add(startsIn + time.Hour),
	}
	if err := v.repo.SeedSlot(context.Background(), slot); err != nil {
		t.Fatalf("seed slot %q: %v", id, err)
	}
	return slot
}

// post sends a raw body string rather than marshalling a struct, so a test can control the
// exact JSON text — which the replay test needs, since its whole point is sending the same
// logical request with its fields in the other order.
func (v *vertical) post(t *testing.T, path, key, body string) (int, bookingBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	v.handler.ServeHTTP(rec, req)

	var out bookingBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST %s: response was not JSON (status %d): %s", path, rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

func (v *vertical) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	v.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func identity(org domain.OrganisationID, user string) string {
	return fmt.Sprintf(`{"user_organisation_id":%q,"user_id":%q}`, org, user)
}

func reservePath(org domain.OrganisationID, slot domain.SlotID) string {
	return fmt.Sprintf("/v1/slots/%s/%s/reservations", org, slot)
}

func (v *vertical) assertPersisted(t *testing.T, ref domain.SlotRef, wantHeld, wantBookings int) {
	t.Helper()
	held, active, err := v.repo.SlotCounts(context.Background(), ref)
	if err != nil {
		t.Fatalf("slot counts: %v", err)
	}
	if held != wantHeld || active != wantBookings {
		t.Errorf("persisted state: held=%d bookings=%d, want held=%d bookings=%d",
			held, active, wantHeld, wantBookings)
	}
}

func (v *vertical) assertClaims(t *testing.T, want int) {
	t.Helper()
	got, err := v.repo.ClaimCount(context.Background())
	if err != nil {
		t.Fatalf("claim count: %v", err)
	}
	if got != want {
		t.Errorf("persisted claims = %d, want %d", got, want)
	}
}

// --- tests ------------------------------------------------------------------

// The vertical path: an HTTP request becomes a committed hold, and the response carries the
// classification the measurement contract reconciles on.
func TestReserveOverHTTPCommitsAHold(t *testing.T) {
	v := newVertical(t, 2*time.Minute)
	slot := v.seedSlot(t, testSlot, 1, time.Hour)

	status, body := v.post(t, reservePath(testOrg, testSlot), "key-1", identity(testOrg, "user-1"))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, body)
	}
	if body.Outcome != string(domain.OutcomeAdmittedSuccess) {
		t.Errorf("outcome = %q, want admitted_success", body.Outcome)
	}
	if body.Replay {
		t.Error("a first request was reported as a replay")
	}
	if body.Reason != "" {
		t.Errorf("reason = %q, want empty on success", body.Reason)
	}
	if !strings.HasPrefix(body.ReservationID, "res_") {
		t.Errorf("reservation_id = %q, want a server-minted res_ identifier", body.ReservationID)
	}

	// The response is not the evidence; the rows are.
	v.assertPersisted(t, slot.Ref(), 1, 0)
	v.assertClaims(t, 1)
}

// Replay returns the original outcome, and the body's field *order* must not matter: the
// request hash covers semantically significant typed fields, not the JSON text. If the
// handlers ever passed raw bytes into the hash, this reordered retry would be refused as
// idempotency_conflict — which is exactly the mistake this test exists to catch.
func TestReserveReplayIgnoresBodyFieldOrder(t *testing.T) {
	v := newVertical(t, 2*time.Minute)
	slot := v.seedSlot(t, testSlot, 1, time.Hour)

	firstStatus, first := v.post(t, reservePath(testOrg, testSlot), "key-1",
		`{"user_organisation_id":"org-1","user_id":"user-1"}`)
	if firstStatus != http.StatusOK || first.Replay {
		t.Fatalf("first reserve: status=%d replay=%v, want 200 and replay=false", firstStatus, first.Replay)
	}

	replayStatus, replay := v.post(t, reservePath(testOrg, testSlot), "key-1",
		`{"user_id":"user-1","user_organisation_id":"org-1"}`)

	if replayStatus != http.StatusOK {
		t.Fatalf("replay status = %d, want 200: %+v", replayStatus, replay)
	}
	if !replay.Replay {
		t.Error("the second request with the same key was not reported as a replay")
	}
	if replay.Outcome != first.Outcome {
		t.Errorf("replay outcome = %q, want the original %q", replay.Outcome, first.Outcome)
	}
	if replay.ReservationID != first.ReservationID {
		t.Errorf("replay reservation_id = %q, want the original %q", replay.ReservationID, first.ReservationID)
	}

	// The decisive assertion: one logical mutation, not two.
	v.assertPersisted(t, slot.Ref(), 1, 0)
	v.assertClaims(t, 1)
}

// Refusals reach the client as the classified outcome with the right status. 409 for a
// refusal the state caused, 404 for a target that does not exist — the one status exception
// in the mapping, and the only one worth proving through a real database.
func TestRefusalsMapToTheirStatus(t *testing.T) {
	t.Run("no capacity is 409", func(t *testing.T) {
		v := newVertical(t, 2*time.Minute)
		slot := v.seedSlot(t, testSlot, 1, time.Hour)

		if status, body := v.post(t, reservePath(testOrg, testSlot), "key-1",
			identity(testOrg, "user-1")); status != http.StatusOK {
			t.Fatalf("first reserve: status = %d, want 200: %+v", status, body)
		}

		status, body := v.post(t, reservePath(testOrg, testSlot), "key-2", identity(testOrg, "user-2"))

		if status != http.StatusConflict {
			t.Errorf("status = %d, want 409", status)
		}
		if body.Outcome != string(domain.OutcomeBusinessRefusal) {
			t.Errorf("outcome = %q, want business_refusal", body.Outcome)
		}
		if body.Reason != string(domain.ReasonNoCapacity) {
			t.Errorf("reason = %q, want no_capacity", body.Reason)
		}
		// The refused request consumed nothing.
		v.assertPersisted(t, slot.Ref(), 1, 0)
	})

	t.Run("unknown target is 404", func(t *testing.T) {
		v := newVertical(t, 2*time.Minute)

		status, body := v.post(t, reservePath(testOrg, "no-such-slot"), "key-1", identity(testOrg, "user-1"))

		if status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
		if body.Outcome != string(domain.OutcomeBusinessRefusal) {
			t.Errorf("outcome = %q, want business_refusal", body.Outcome)
		}
		if body.Reason != string(domain.ReasonUnknownTarget) {
			t.Errorf("reason = %q, want unknown_target", body.Reason)
		}
	})
}

// End-to-end expiry: a hold taken over HTTP, abandoned, and settled by the worker without
// any request touching the slot. This is the path that returns capacity nobody is asking
// for, and the one place the repository's candidate query, the service's settlement and the
// worker's iteration are exercised together.
func TestAbandonedHoldIsSettledByTheWorker(t *testing.T) {
	const shortTTL = 100 * time.Millisecond
	v := newVertical(t, shortTTL)
	slot := v.seedSlot(t, testSlot, 1, time.Hour)

	status, body := v.post(t, reservePath(testOrg, testSlot), "key-1", identity(testOrg, "user-1"))
	if status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200: %+v", status, body)
	}
	v.assertPersisted(t, slot.Ref(), 1, 0)

	v.waitForElapsedHold(t, slot.Ref())

	obs, err := v.expiry.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("expiry iteration: %v", err)
	}
	if obs.Failed {
		t.Error("a successful iteration was observed as failed")
	}
	if obs.Slots != 1 {
		t.Errorf("observed slots = %d, want 1", obs.Slots)
	}
	if obs.Expired != 1 {
		t.Errorf("observed expired = %d, want 1", obs.Expired)
	}

	// Capacity is back, and no request was involved in returning it.
	v.assertPersisted(t, slot.Ref(), 0, 0)

	// PR4's rule at the vertical level: the worker must not reap claims. User-scoped
	// settlement is the sole claim reaper, so the claim outlives the hold and only the
	// owning identity's next reserve may remove it.
	v.assertClaims(t, 1)
}

// waitForElapsedHold blocks until the repository reports ref as holding an elapsed
// reservation, polling the database rather than sleeping for a multiple of the TTL:
// "elapsed" is decided by clock_timestamp() inside the query, so this waits on the clock
// that owns the decision instead of trusting the test host's.
func (v *vertical) waitForElapsedHold(t *testing.T, ref domain.SlotRef) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		refs, err := v.repo.ElapsedHoldSlots(context.Background(), 10)
		if err != nil {
			t.Fatalf("elapsed hold slots: %v", err)
		}
		if slices.Contains(refs, ref) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot %s never reported an elapsed hold; ElapsedHoldSlots returned %v", ref.SlotID, refs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Readiness answers about the real dependency: 200 while the database is reachable, 503
// once it is not, and never with the driver's error text, which can carry the host and user.
func TestReadinessTracksTheDatabase(t *testing.T) {
	v := newVertical(t, 2*time.Minute)

	rec := v.get(t, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d with a live database, want 200: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "ready") {
		t.Errorf("body = %s, want a ready status", rec.Body)
	}

	// Closing the pool is the cheapest faithful way to make the dependency unavailable:
	// the probe's Ping then fails exactly as it would against a dead server.
	v.pool.Close()

	rec = v.get(t, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d with the pool closed, want 503: %s", rec.Code, rec.Body)
	}
	for _, leak := range []string{"pgx", "pool", "host=", "user=", "closed"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("readiness body leaks driver detail %q: %s", leak, rec.Body)
		}
	}
}

// Liveness must not depend on the database: a process that is serving HTTP is alive, and
// reporting otherwise would have an orchestrator restart a healthy service during a
// database blip instead of just withholding traffic.
func TestLivenessIsIndependentOfTheDatabase(t *testing.T) {
	v := newVertical(t, 2*time.Minute)
	v.pool.Close()

	if rec := v.get(t, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("status = %d with the database gone, want 200: %s", rec.Code, rec.Body)
	}
}

// The read route over real SQL: scoped to the requested organisation, and chronological
// because the query says so. The slots are seeded out of order so insertion order cannot
// be what makes this pass.
func TestListSlotsIsScopedAndChronological(t *testing.T) {
	v := newVertical(t, 2*time.Minute)
	v.seedSlot(t, otherSlot, 3, 2*time.Hour)
	v.seedSlot(t, testSlot, 2, time.Hour)
	v.seedSlotIn(t, otherOrg, "slot-elsewhere", 9, time.Hour)

	rec := v.get(t, "/v1/slots?slot_organisation_id=org-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body slotsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}

	if len(body.Slots) != 2 {
		t.Fatalf("returned %d slots, want 2 (the other organisation's must not appear): %+v",
			len(body.Slots), body.Slots)
	}
	if body.Slots[0].SlotID != string(testSlot) || body.Slots[1].SlotID != string(otherSlot) {
		t.Errorf("order = %s, %s; want %s then %s (ascending starts_at)",
			body.Slots[0].SlotID, body.Slots[1].SlotID, testSlot, otherSlot)
	}
	if body.Truncated {
		t.Error("a two-slot list was reported as truncated")
	}
	if body.Slots[0].Capacity != 2 || body.Slots[0].ResourceID != "yoga" {
		t.Errorf("slot view = %+v, want the seeded capacity and resource", body.Slots[0])
	}
}
