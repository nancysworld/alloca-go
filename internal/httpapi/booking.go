package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/service"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// BookingService is the transport's view of the application layer, declared here where it
// is consumed so the handlers can be tested against a fake without a database;
// *service.Service satisfies it. It is also the handlers' only route to the domain.
type BookingService interface {
	Reserve(ctx context.Context, cmd service.ReserveCommand) (domain.Result, error)
	Confirm(ctx context.Context, cmd service.ConfirmCommand) (domain.Result, error)
	Cancel(ctx context.Context, cmd service.CancelCommand) (domain.Result, error)
}

// bookingHandlers serves the three mutation endpoints.
type bookingHandlers struct {
	svc      BookingService
	recorder telemetry.Recorder
	budget   config.RequestBudget
	guard    placementGuard
}

// Every handler here leaves the command's Body field unset, and must keep doing so.
//
// Passing the raw request bytes would look like it made the hash cover "what the client
// sent", but it would make the hash depend on JSON *representation*: the same logical
// request with its two fields reordered would hash differently and a legitimate retry
// would be refused as idempotency_conflict. The hash is defined over semantically
// significant *typed* fields, and AG-M1 mutation bodies are empty apart from the identity
// the hash already covers (transaction-semantics §5.1). A future body's significant
// fields belong in the canonical struct, not in opaque bytes.

func (h *bookingHandlers) reserve(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), h.budget.ServerDeadline)
	defer cancel()

	key, detail := idempotencyKey(r)
	if detail != "" {
		h.invalid(ctx, w, domain.OpReserve, start, detail)
		return
	}
	user, detail := decodeIdentity(r, maxBodyBytes)
	if detail != "" {
		h.invalid(ctx, w, domain.OpReserve, start, detail)
		return
	}
	// Mutations route by the user's organisation: user-home owns the schedule claim and
	// the client idempotency scope, so it is the stable home for a mutation and for
	// every replay of it (horizontal-database-authority §4.1).
	if !h.guard.serves(user.OrganisationID) {
		h.invalid(ctx, w, domain.OpReserve, start, h.guard.refuse(ctx, domain.OpReserve, user.OrganisationID))
		return
	}

	// Both halves of the slot's identity come from the path (§1.2). ServeMux decodes
	// percent-escapes, so an identifier containing a separator round-trips intact.
	result, err := h.svc.Reserve(ctx, service.ReserveCommand{
		UserRef: user,
		SlotRef: domain.SlotRef{
			OrganisationID: domain.OrganisationID(r.PathValue(varSlotOrganisation)),
			SlotID:         domain.SlotID(r.PathValue(varSlotID)),
		},
		IdempotencyKey: key,
	})
	h.finish(ctx, w, domain.OpReserve, start, result, err)
}

func (h *bookingHandlers) confirm(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), h.budget.ServerDeadline)
	defer cancel()

	key, detail := idempotencyKey(r)
	if detail != "" {
		h.invalid(ctx, w, domain.OpConfirm, start, detail)
		return
	}
	user, detail := decodeIdentity(r, maxBodyBytes)
	if detail != "" {
		h.invalid(ctx, w, domain.OpConfirm, start, detail)
		return
	}
	// Mutations route by the user's organisation: user-home owns the schedule claim and
	// the client idempotency scope, so it is the stable home for a mutation and for
	// every replay of it (horizontal-database-authority §4.1).
	if !h.guard.serves(user.OrganisationID) {
		h.invalid(ctx, w, domain.OpConfirm, start, h.guard.refuse(ctx, domain.OpConfirm, user.OrganisationID))
		return
	}

	result, err := h.svc.Confirm(ctx, service.ConfirmCommand{
		UserRef:        user,
		ReservationID:  domain.ReservationID(r.PathValue(varReservationID)),
		IdempotencyKey: key,
	})
	h.finish(ctx, w, domain.OpConfirm, start, result, err)
}

func (h *bookingHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), h.budget.ServerDeadline)
	defer cancel()

	key, detail := idempotencyKey(r)
	if detail != "" {
		h.invalid(ctx, w, domain.OpCancel, start, detail)
		return
	}
	user, detail := decodeIdentity(r, maxBodyBytes)
	if detail != "" {
		h.invalid(ctx, w, domain.OpCancel, start, detail)
		return
	}
	// Mutations route by the user's organisation: user-home owns the schedule claim and
	// the client idempotency scope, so it is the stable home for a mutation and for
	// every replay of it (horizontal-database-authority §4.1).
	if !h.guard.serves(user.OrganisationID) {
		h.invalid(ctx, w, domain.OpCancel, start, h.guard.refuse(ctx, domain.OpCancel, user.OrganisationID))
		return
	}

	result, err := h.svc.Cancel(ctx, service.CancelCommand{
		UserRef:        user,
		ReservationID:  domain.ReservationID(r.PathValue(varReservationID)),
		IdempotencyKey: key,
	})
	h.finish(ctx, w, domain.OpCancel, start, result, err)
}

// finish renders the operation's answer and observes it.
//
// A fault and a domain answer are mutually exclusive: the service returns a Result or an
// error, never both, and the error channel is classified by the domain rather than
// re-interpreted here.
func (h *bookingHandlers) finish(
	ctx context.Context, w http.ResponseWriter, op domain.Operation,
	start time.Time, result domain.Result, err error,
) {
	status, body := responseForResult(result)
	if err != nil {
		status, body = responseForFault(err)
	}
	h.respond(ctx, w, op, start, status, body)
}

// invalid renders a request rejected before the domain path. It is observed like any
// other outcome — invalid_request is a member of the taxonomy, and dropping it would
// leave the totals short.
func (h *bookingHandlers) invalid(
	ctx context.Context, w http.ResponseWriter, op domain.Operation, start time.Time, detail string,
) {
	status, body := responseForInvalid(detail)
	h.respond(ctx, w, op, start, status, body)
}

// respond writes the response and records the observation. It is the only place a booking
// response is written, so every completed request carries exactly one terminal outcome and
// the telemetry cannot disagree with the status the client received
// (measurement-contract §4).
//
// The write is best-effort by design: when the caller has already disconnected the status
// reaches nobody, but the request still completed and is still observed, which is what
// keeps timeout_client visible instead of vanishing from the mix.
func (h *bookingHandlers) respond(
	ctx context.Context, w http.ResponseWriter, op domain.Operation,
	start time.Time, status int, body response,
) {
	writeJSONResponse(w, status, body)
	h.recorder.RecordRequest(ctx, telemetry.RequestObservation{
		Operation:  string(op),
		Outcome:    body.Outcome,
		Reason:     body.Reason,
		Replay:     body.Replay,
		HTTPStatus: status,
		Duration:   time.Since(start),
	})
}
