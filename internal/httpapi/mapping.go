package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// This file is the whole transport mapping: domain answer or fault in, HTTP status and
// response body out. It is the only place in the service that knows an outcome has a
// status code, and it is deliberately total — every Outcome the contract declares has an
// entry, including the ones AG-M1 never produces, so adding a producer later cannot
// silently fall through to a default.
//
// It has two entry points because the service has two answer channels: a domain.Result,
// and a non-nil error classified by domain.ClassifyFault. Handlers must never invent an
// outcome of their own.

// response is the body every booking endpoint returns, success or refusal alike.
//
// No *internal* error text appears. For domain answers and faults, Message is chosen from
// the closed set below and never built from an error, so no SQL, pgx, or invariant detail
// can escape. The one exception is invalid_request, whose Message describes what was
// malformed about the request and may include the JSON decoder's own text — that names an
// offending field or type the caller supplied, and nothing internal. See responseForInvalid.
type response struct {
	Outcome       domain.Outcome `json:"outcome"`
	Reason        domain.Reason  `json:"reason,omitempty"`
	Replay        bool           `json:"replay"`
	ReservationID string         `json:"reservation_id,omitempty"`
	BookingID     string         `json:"booking_id,omitempty"`
	Message       string         `json:"message,omitempty"`
}

// statusForOutcome maps a terminal outcome to its HTTP status.
//
// Refusals are 409 Conflict rather than 422: the request is well-formed and understood,
// and it is the current state of the slot, the schedule, or the key that prevents it.
// unknown_target is the exception, because "the thing you named does not exist" is 404 in
// any HTTP vocabulary.
//
// known=false reports an outcome this function does not recognise, and is what lets
// totality be *proven* rather than assumed: a test asserts known for every outcome the
// contract declares, and discriminates because a fabricated outcome makes it false.
// Without the flag an unmapped outcome would fall through to a plausible 500 and nothing
// would ever notice.
func statusForOutcome(outcome domain.Outcome, reason domain.Reason) (status int, known bool) {
	switch outcome {
	case domain.OutcomeAdmittedSuccess:
		// 200 rather than 201 for reserve/confirm. The mapping is keyed by outcome, not
		// by operation, so that it stays total and table-testable; the created entity's
		// identifier is in the body either way.
		return http.StatusOK, true
	case domain.OutcomeBusinessRefusal:
		if reason == domain.ReasonUnknownTarget {
			return http.StatusNotFound, true
		}
		return http.StatusConflict, true
	case domain.OutcomeInvalidRequest:
		return http.StatusBadRequest, true
	case domain.OutcomeUnknownReplayable:
		return http.StatusInternalServerError, true
	case domain.OutcomeTimeoutClient:
		// The caller has already disconnected, so this status usually reaches nobody.
		// Delivery is best-effort and the telemetry is the record; 408 is the closest
		// standard code, and a non-standard 499 is deliberately not used.
		return http.StatusRequestTimeout, true
	case domain.OutcomeTimeoutServer, domain.OutcomeTimeoutDB, domain.OutcomeTimeoutLB:
		return http.StatusGatewayTimeout, true
	case domain.OutcomeRetryAfter, domain.OutcomeAdmissionRejected, domain.OutcomeQueuePosition:
		return http.StatusServiceUnavailable, true
	case domain.OutcomeInternalFailure:
		return http.StatusInternalServerError, true
	default:
		return http.StatusInternalServerError, false
	}
}

// Client-facing messages. They are constants, not formatted from errors, so nothing
// internal can leak through this path.
const (
	// msgUnknownReplayable is the one message a client must act on. The operation may
	// already have committed, so the safe recovery is replaying the *same* idempotency
	// key: the record either exists (it committed — the replay returns the original
	// outcome) or it does not (it never committed — the replay performs it). A new key
	// would be treated as a new request and could double-book
	// (transaction-semantics §5.4).
	msgUnknownReplayable = "The outcome of this request is unknown: it may already have " +
		"committed. Retry with the SAME Idempotency-Key to learn the outcome. Do not retry " +
		"with a new key — that would be a new request and could duplicate the booking."
	msgTimeout        = "The request exceeded its deadline and was not completed."
	msgInternal       = "The request could not be completed."
	msgUnavailable    = "The service is not accepting this request at the moment."
	msgClientTimedOut = "The client closed the connection before the request completed."
)

// messageForOutcome returns the guidance for an outcome, or "" where the classification
// already says everything a client needs.
func messageForOutcome(outcome domain.Outcome) string {
	switch outcome {
	case domain.OutcomeUnknownReplayable:
		return msgUnknownReplayable
	case domain.OutcomeTimeoutServer, domain.OutcomeTimeoutDB, domain.OutcomeTimeoutLB:
		return msgTimeout
	case domain.OutcomeTimeoutClient:
		return msgClientTimedOut
	case domain.OutcomeInternalFailure:
		return msgInternal
	case domain.OutcomeRetryAfter, domain.OutcomeAdmissionRejected, domain.OutcomeQueuePosition:
		return msgUnavailable
	default:
		return ""
	}
}

// responseForResult renders a domain answer.
func responseForResult(r domain.Result) (int, response) {
	status, _ := statusForOutcome(r.Outcome, r.Reason)
	return status, response{
		Outcome:       r.Outcome,
		Reason:        r.Reason,
		Replay:        r.Replay,
		ReservationID: string(r.ReservationID),
		BookingID:     string(r.BookingID),
		Message:       messageForOutcome(r.Outcome),
	}
}

// responseForFault renders an infrastructure fault, classifying it through the domain's
// own error→outcome mapping so the transport edge does not get a second opinion on what
// a fault means.
func responseForFault(err error) (int, response) {
	outcome := domain.ClassifyFault(err)
	status, _ := statusForOutcome(outcome, "")
	return status, response{
		Outcome: outcome,
		Message: messageForOutcome(outcome),
	}
}

// responseForInvalid renders a request rejected before the domain path
// (transaction-semantics §8). The detail describes what was malformed about the request
// itself — a missing field, an unparseable body — and never anything internal.
func responseForInvalid(detail string) (int, response) {
	return http.StatusBadRequest, response{
		Outcome: domain.OutcomeInvalidRequest,
		Message: detail,
	}
}
