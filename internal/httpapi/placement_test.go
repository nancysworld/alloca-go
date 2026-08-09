package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/service"
)

// shardedPlacement homes org-a and org-c on authority-1, and org-b on authority-2, so a
// unit bound to authority-1 owns two organisations and must refuse the third.
func shardedPlacement(t *testing.T) domain.Placement {
	t.Helper()
	p, err := domain.ParsePlacement([]byte(
		`{"version":"routing-v1","homes":{"org-a":"authority-1","org-b":"authority-2","org-c":"authority-1"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}
	return p
}

// recordingService fails the test if it is ever reached: a misrouted request must be
// refused at the edge, before anything can write to the wrong authority.
type recordingService struct{ called bool }

func (s *recordingService) Reserve(context.Context, service.ReserveCommand) (domain.Result, error) {
	s.called = true
	return domain.Result{Outcome: domain.OutcomeAdmittedSuccess}, nil
}

func (s *recordingService) Confirm(context.Context, service.ConfirmCommand) (domain.Result, error) {
	s.called = true
	return domain.Result{Outcome: domain.OutcomeAdmittedSuccess}, nil
}

func (s *recordingService) Cancel(context.Context, service.CancelCommand) (domain.Result, error) {
	s.called = true
	return domain.Result{Outcome: domain.OutcomeAdmittedSuccess}, nil
}

type recordingLister struct{ called bool }

func (l *recordingLister) SlotsByOrganisation(context.Context, domain.OrganisationID, int) ([]domain.Slot, error) {
	l.called = true
	return nil, nil
}

func shardedServer(t *testing.T, svc BookingService, slots SlotLister, misroutes *int) http.Handler {
	t.Helper()
	return New(config.Default(), func() buildinfo.Info { return buildinfo.Info{} }, Options{
		Service:   svc,
		Slots:     slots,
		Placement: shardedPlacement(t),
		Authority: "authority-1",
		OnMisroute: func(context.Context, domain.Operation) {
			if misroutes != nil {
				*misroutes++
			}
		},
	}).Handler()
}

func mutationRequest(t *testing.T, method, path, org string) *http.Request {
	t.Helper()
	body := `{"user_organisation_id":"` + org + `","user_id":"u-1"}`
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "k-1")
	return req
}

// This is the ag-sept-plan §12.5 misrouting control. Without it, "no supported request reached
// the wrong authority" would be a property of whatever routed the traffic rather than of
// Alloca: the generator holds the same map, so a passing run would prove only that the
// generator's routing table is correct.
func TestMisroutedMutationIsRefusedBeforeReachingTheService(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"reserve", http.MethodPost, "/v1/slots/org-b/slot-1/reservations"},
		{"confirm", http.MethodPost, "/v1/reservations/res-1/confirm"},
		{"cancel", http.MethodPost, "/v1/reservations/res-1/cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &recordingService{}
			misroutes := 0
			h := shardedServer(t, svc, nil, &misroutes)

			rec := httptest.NewRecorder()
			// org-b is homed on authority-2; this unit is authority-1.
			h.ServeHTTP(rec, mutationRequest(t, tc.method, tc.path, "org-b"))

			if svc.called {
				t.Fatal("a misrouted request reached the service; it must be refused at the edge")
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var body response
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if body.Outcome != domain.OutcomeInvalidRequest {
				t.Errorf("outcome = %q, want %q", body.Outcome, domain.OutcomeInvalidRequest)
			}
			// A misroute must never be recorded as the user's durable domain outcome on
			// the wrong authority, which is why it is invalid_request — the one outcome
			// INV-7 exempts from the recording rule — and never a business_refusal.
			if body.Reason != "" {
				t.Errorf("reason = %q; a misroute is not a domain refusal and carries none", body.Reason)
			}
			if misroutes != 1 {
				t.Errorf("misroute counter = %d, want 1: a deployment fault must not hide among client errors", misroutes)
			}
			for _, want := range []string{"authority-1", "org-b", "routing-v1"} {
				if !strings.Contains(body.Message, want) {
					t.Errorf("message %q does not mention %q", body.Message, want)
				}
			}
		})
	}
}

// The organisations this unit *does* own must pass the guard untouched — including
// org-c, which is a different organisation sharing authority-1. A guard that refused it
// would have broken colocated cross-organisation booking.
func TestOwnedOrganisationsPassTheGuard(t *testing.T) {
	for _, org := range []string{"org-a", "org-c"} {
		t.Run(org, func(t *testing.T) {
			svc := &recordingService{}
			h := shardedServer(t, svc, nil, nil)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, mutationRequest(t, http.MethodPost, "/v1/slots/org-a/slot-1/reservations", org))

			if !svc.called {
				t.Fatalf("a request for %s, which authority-1 owns, was refused at the edge", org)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

// Reads route by the slot organisation, which is the authority holding the rows they
// return. A unit answering for an organisation it does not own would be reporting an
// empty catalogue as fact.
func TestMisroutedListSlotsIsRefusedBeforeReachingTheRepository(t *testing.T) {
	lister := &recordingLister{}
	misroutes := 0
	h := shardedServer(t, nil, lister, &misroutes)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/slots?slot_organisation_id=org-b", nil))

	if lister.called {
		t.Fatal("a misrouted read reached the repository")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if misroutes != 1 {
		t.Errorf("misroute counter = %d, want 1", misroutes)
	}
}

// The pre-PR3a deployment: no placement supplied, so the unit is unsharded and refuses
// nothing for placement reasons. This is what keeps every existing test — and every
// existing deployment — behaving exactly as it did.
func TestUnshardedServerRefusesNothingForPlacement(t *testing.T) {
	svc := &recordingService{}
	h := New(config.Default(), func() buildinfo.Info { return buildinfo.Info{} }, Options{
		Service:   svc,
		Placement: domain.Unsharded("authority-1"),
		Authority: "authority-1",
	}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mutationRequest(t, http.MethodPost, "/v1/slots/org-x/slot-1/reservations", "org-whatever"))

	if !svc.called {
		t.Fatal("an unsharded unit refused a request for placement reasons")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMetaReportsThisUnitsPlacement(t *testing.T) {
	h := shardedServer(t, &recordingService{}, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/meta", nil))

	var meta metaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decoding /meta: %v", err)
	}
	if meta.Placement.AuthorityID != "authority-1" {
		t.Errorf("authority_id = %q, want authority-1", meta.Placement.AuthorityID)
	}
	if meta.Placement.RoutingVersion != "routing-v1" {
		t.Errorf("routing_version = %q, want routing-v1", meta.Placement.RoutingVersion)
	}
	if !meta.Placement.Sharded {
		t.Error("sharded = false for a unit serving part of a placement map")
	}
	if len(meta.Placement.Organisations) != 2 {
		t.Errorf("organisations = %v, want the two org-a owns", meta.Placement.Organisations)
	}
}

// An unsharded unit still reports its routing, because "there was one authority" is an
// answer a result should carry rather than a field a reader infers from absence.
func TestMetaReportsUnshardedRouting(t *testing.T) {
	h := New(config.Default(), func() buildinfo.Info { return buildinfo.Info{} }, Options{
		Placement: domain.Unsharded("authority-1"),
		Authority: "authority-1",
	}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/meta", nil))

	var meta metaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decoding /meta: %v", err)
	}
	if meta.Placement.RoutingVersion != domain.UnshardedVersion {
		t.Errorf("routing_version = %q, want %q", meta.Placement.RoutingVersion, domain.UnshardedVersion)
	}
	if meta.Placement.Sharded {
		t.Error("sharded = true for a single-authority deployment")
	}
	if len(meta.Placement.Organisations) != 0 {
		t.Errorf("organisations = %v; an unsharded unit serves every organisation, which is not a list",
			meta.Placement.Organisations)
	}
}
