package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// SlotLister reads an organisation's slots. Declared here, where it is consumed;
// *postgres.Repo satisfies it and cmd wires the two together.
//
// It reaches the repository rather than the service, deliberately. The service
// orchestrates mutations — a transaction, a lock, an authoritative instant, an
// idempotency record, a terminal outcome — and a catalogue listing has none of those, so
// a service method would be a passthrough implying a transactional guarantee this
// endpoint does not have.
type SlotLister interface {
	SlotsByOrganisation(ctx context.Context, org domain.OrganisationID, limit int) ([]domain.Slot, error)
}

// listLimit caps one response. A safety bound, not pagination: the repository's
// deterministic ordering makes truncation predictable rather than arbitrary. A cursor
// contract is query-platform work and is deliberately out of scope.
const listLimit = 1000

// queryParamSlotOrganisation names the required filter. Required rather than optional
// because "every slot in the database" is not a meaningful request for a multi-tenant
// service, and an optional filter would quietly invite one.
const queryParamSlotOrganisation = "slot_organisation_id"

// slotView reports what a slot *is*: identity, resource, capacity, booking window.
//
// It must never report availability — no remaining count, no "bookable" flag. Any such
// number would be computed without the slot lock and stale before the response was
// written, which is exactly when it would matter; reserve under the slot lock is the sole
// authority on whether a unit can be held (transaction-semantics §2), and this endpoint
// must not read as a second opinion. Capacity is safe because it is the slot's configured
// size, not a measurement of what is left.
type slotView struct {
	SlotOrganisationID string    `json:"slot_organisation_id"`
	SlotID             string    `json:"slot_id"`
	ResourceID         string    `json:"resource_id"`
	Capacity           int       `json:"capacity"`
	ReleaseAt          time.Time `json:"release_at"`
	StartsAt           time.Time `json:"starts_at"`
	EndsAt             time.Time `json:"ends_at"`
}

// listResponse wraps the collection so the body can grow — a truncation flag now, a
// cursor later — without changing from array to object and breaking every client.
type listResponse struct {
	Slots     []slotView `json:"slots"`
	Truncated bool       `json:"truncated"`
}

// slotHandlers serves the read surface.
type slotHandlers struct {
	slots    SlotLister
	recorder telemetry.Recorder
	budget   config.RequestBudget
}

// listSlots serves GET /v1/slots?slot_organisation_id=…
func (h *slotHandlers) listSlots(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), h.budget.ServerDeadline)
	defer cancel()

	org := r.URL.Query().Get(queryParamSlotOrganisation)
	if org == "" {
		status, body := responseForInvalid(queryParamSlotOrganisation + " is required")
		h.write(ctx, w, start, status, body.Outcome, body)
		return
	}

	// One more than the cap, so a full page can be distinguished from a truncated one
	// without a second count query.
	slots, err := h.slots.SlotsByOrganisation(ctx, domain.OrganisationID(org), listLimit+1)
	if err != nil {
		status, body := responseForFault(err)
		h.write(ctx, w, start, status, body.Outcome, body)
		return
	}

	truncated := len(slots) > listLimit
	if truncated {
		slots = slots[:listLimit]
	}
	views := make([]slotView, 0, len(slots))
	for _, s := range slots {
		views = append(views, slotView{
			SlotOrganisationID: string(s.OrganisationID),
			SlotID:             string(s.ID),
			ResourceID:         s.ResourceID,
			Capacity:           s.Capacity,
			ReleaseAt:          s.ReleaseAt,
			StartsAt:           s.StartsAt,
			EndsAt:             s.EndsAt,
		})
	}
	h.write(ctx, w, start, http.StatusOK, domain.OutcomeAdmittedSuccess,
		listResponse{Slots: views, Truncated: truncated})
}

// write emits the response and observes the request.
//
// The observation carries operation=list_slots, which keeps read traffic countable and
// separable — so AG-M2 must compute booking goodput over the three mutation operations
// rather than over all requests: a successful listing means the service answered
// correctly, not that a booking happened.
func (h *slotHandlers) write(
	ctx context.Context, w http.ResponseWriter, start time.Time,
	status int, outcome domain.Outcome, body any,
) {
	writeJSONResponse(w, status, body)
	h.recorder.RecordRequest(ctx, telemetry.RequestObservation{
		Operation:  telemetry.OperationListSlots,
		Outcome:    outcome,
		HTTPStatus: status,
		Duration:   time.Since(start),
	})
}
