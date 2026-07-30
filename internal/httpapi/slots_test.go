package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
)

type fakeLister struct {
	slots []domain.Slot
	err   error
	org   domain.OrganisationID
	limit int
}

func (f *fakeLister) SlotsByOrganisation(_ context.Context, org domain.OrganisationID, limit int) ([]domain.Slot, error) {
	f.org, f.limit = org, limit
	return f.slots, f.err
}

func listServer(t *testing.T, lister SlotLister) *Server {
	t.Helper()
	meta := func() buildinfo.Info { return buildinfo.Collect(time.Now()) }
	return New(config.Default(), meta, Options{Slots: lister})
}

func getSlots(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/slots"+query, nil))
	return rec
}

func slot(id domain.SlotID, startsIn time.Duration) domain.Slot {
	now := time.Now().UTC()
	return domain.Slot{
		ID: id, OrganisationID: "org-1", ResourceID: "yoga", Capacity: 5,
		ReleaseAt: now, StartsAt: now.Add(startsIn), EndsAt: now.Add(startsIn + time.Hour),
	}
}

func TestListSlotsReturnsTheOrganisationsSlots(t *testing.T) {
	lister := &fakeLister{slots: []domain.Slot{slot("slot-a", time.Hour), slot("slot-b", 2*time.Hour)}}
	rec := getSlots(t, listServer(t, lister), "?slot_organisation_id=org-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if len(body.Slots) != 2 {
		t.Fatalf("returned %d slots, want 2", len(body.Slots))
	}
	if body.Truncated {
		t.Error("a short list was reported as truncated")
	}
	// The repository's ordering is preserved rather than re-sorted, so the endpoint's
	// determinism comes from one place.
	if body.Slots[0].SlotID != "slot-a" || body.Slots[1].SlotID != "slot-b" {
		t.Errorf("order = %s, %s; want the repository's order", body.Slots[0].SlotID, body.Slots[1].SlotID)
	}
	if lister.org != "org-1" {
		t.Errorf("queried organisation %q, want org-1", lister.org)
	}

	got := body.Slots[0]
	if got.SlotOrganisationID != "org-1" || got.ResourceID != "yoga" || got.Capacity != 5 {
		t.Errorf("slot view = %+v, want identity, resource and capacity populated", got)
	}
	if got.StartsAt.IsZero() || got.EndsAt.IsZero() || got.ReleaseAt.IsZero() {
		t.Errorf("slot view is missing window timestamps: %+v", got)
	}
}

// The listing is informational. If it ever grows a field that implies a unit is
// available, it becomes a second opinion on a decision only reserve under the slot lock
// can make — and clients will believe it.
func TestListSlotsMakesNoAvailabilityClaim(t *testing.T) {
	rec := getSlots(t, listServer(t, &fakeLister{slots: []domain.Slot{slot("slot-a", time.Hour)}}),
		"?slot_organisation_id=org-1")

	var raw struct {
		Slots []map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	for _, forbidden := range []string{"available", "remaining", "held", "free", "bookable", "consumed"} {
		if _, present := raw.Slots[0][forbidden]; present {
			t.Errorf("slot view exposes %q: the listing must not claim availability, which only "+
				"reserve under the slot lock can decide", forbidden)
		}
	}
}

// An organisation with no slots must serialise as an empty array, not null. The repository
// returns a nil slice for no rows, and a nil []slotView marshals to `null` — which is a
// different type to a client, and breaks anything that iterates the field without a nil
// check. The handler's make(..., 0, n) is what prevents it, and that is easy to "simplify"
// away, so it is asserted on the raw JSON rather than through a decode that would hide the
// difference.
func TestListSlotsReturnsAnEmptyArrayNotNull(t *testing.T) {
	rec := getSlots(t, listServer(t, &fakeLister{slots: nil}), "?slot_organisation_id=org-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"slots":[]`) {
		t.Errorf("body = %s, want an empty slots array", got)
	}
}

func TestListSlotsRequiresTheOrganisation(t *testing.T) {
	for _, query := range []string{"", "?slot_organisation_id="} {
		rec := getSlots(t, listServer(t, &fakeLister{}), query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", query, rec.Code)
		}
		var body response
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if body.Outcome != domain.OutcomeInvalidRequest {
			t.Errorf("query %q: outcome = %q, want invalid_request", query, body.Outcome)
		}
	}
}

// Truncation is reported rather than silent: a client that cannot tell it received a
// partial list will treat it as the whole catalogue.
func TestListSlotsReportsTruncation(t *testing.T) {
	many := make([]domain.Slot, listLimit+1)
	for i := range many {
		many[i] = slot(domain.SlotID("slot"), time.Hour)
	}
	lister := &fakeLister{slots: many}
	rec := getSlots(t, listServer(t, lister), "?slot_organisation_id=org-1")

	var body listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if len(body.Slots) != listLimit {
		t.Errorf("returned %d slots, want the cap of %d", len(body.Slots), listLimit)
	}
	if !body.Truncated {
		t.Error("a truncated list was not reported as truncated")
	}
	// One more than the cap is requested, which is how truncation is detected without a
	// second count query.
	if lister.limit != listLimit+1 {
		t.Errorf("asked for %d, want the cap plus one", lister.limit)
	}
}

func TestListSlotsMapsAFaultWithoutLeakingIt(t *testing.T) {
	const secret = "pgx: connect to host=db.internal failed"
	rec := getSlots(t, listServer(t, &fakeLister{err: errors.New(secret)}), "?slot_organisation_id=org-1")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db.internal") {
		t.Error("the underlying database error reached the client")
	}
}

// The readiness reason is withheld from the response, so it has to appear in the logs or
// it is lost entirely — which is what happened in the first draft of this PR.
func TestReadyzLogsTheFailureItWithholds(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, nil))
	meta := func() buildinfo.Info { return buildinfo.Collect(time.Now()) }
	srv := New(config.Default(), meta, Options{
		Logger: logger,
		Ready:  func(context.Context) error { return errors.New("database unreachable: host=db.internal") },
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pathReadyz, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db.internal") {
		t.Error("the readiness failure detail reached the caller")
	}
	if !strings.Contains(logged.String(), "db.internal") {
		t.Errorf("the readiness failure was withheld from the response and never logged: %s", logged.String())
	}
}
