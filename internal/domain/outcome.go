package domain

// Outcome is the single terminal classification every completed request carries
// (measurement-contract §4). It is one half of the two-dimensional classification;
// the orthogonal half is the Result.Replay flag. A replay returns the originally
// recorded Outcome with Replay=true — "replay" is never itself an Outcome.
type Outcome string

const (
	// OutcomeAdmittedSuccess: a correct domain mutation completed. Counts as goodput.
	OutcomeAdmittedSuccess Outcome = "admitted_success"
	// OutcomeBusinessRefusal: a valid domain answer to a well-formed request (sold out,
	// conflict, unknown target, …). Reported separately, never as failure. The specific
	// Reason distinguishes which refusal it is.
	OutcomeBusinessRefusal Outcome = "business_refusal"
	// OutcomeInvalidRequest: rejected before domain processing (malformed body, missing
	// required field, missing idempotency key). A transport/client error, neither
	// goodput nor failure. Normally produced at the httpapi edge (transaction-semantics
	// §8); the service also guards it so service-level totals stay complete.
	OutcomeInvalidRequest Outcome = "invalid_request"

	// The outcomes below are defined by the contract but NOT produced by AG-M1.
	// Admission outcomes arrive with AG-M2; timeout_lb with AG-M3. timeout_* /
	// unknown_replayable / internal_failure are produced by the adapter and transport
	// layers around the domain (from context and infrastructure errors), not by the
	// domain logic in this package — see the Service contract in internal/service.
	OutcomeRetryAfter        Outcome = "retry_after"
	OutcomeAdmissionRejected Outcome = "admission_rejected"
	OutcomeQueuePosition     Outcome = "queue_position"
	OutcomeUnknownReplayable Outcome = "unknown_replayable"
	OutcomeTimeoutClient     Outcome = "timeout_client"
	OutcomeTimeoutServer     Outcome = "timeout_server"
	OutcomeTimeoutDB         Outcome = "timeout_db"
	OutcomeTimeoutLB         Outcome = "timeout_lb"
	OutcomeInternalFailure   Outcome = "internal_failure"
)

// Reason is the stable reason code carried by a business_refusal so the
// separate-reporting requirement stays legible (transaction-semantics §4). It is
// empty for non-refusal outcomes.
//
// ReasonScheduleConflict means the user already holds an active claim overlapping the
// requested interval (§2.2). It is deliberately distinct from ReasonNoCapacity — the
// slot may still have room; it is the *user's* schedule that is full — and from
// timeout_db, which is what a wait on a conflicting uncommitted transaction maps to.
type Reason string

const (
	ReasonSlotNotReleased     Reason = "slot_not_released"
	ReasonSlotClosed          Reason = "slot_closed"
	ReasonNoCapacity          Reason = "no_capacity"
	ReasonScheduleConflict    Reason = "schedule_conflict"
	ReasonOutsideWindow       Reason = "outside_window"
	ReasonReservationExpired  Reason = "reservation_expired"
	ReasonInvalidState        Reason = "invalid_state"
	ReasonUnknownTarget       Reason = "unknown_target"
	ReasonIdempotencyConflict Reason = "idempotency_conflict"
)

// Result is a completed operation's classified answer: the terminal Outcome, the
// Reason when it is a refusal, whether it was served as an idempotent replay, and
// references to any entity the operation created (the idempotency record's
// result_ref, echoed on replay).
//
// Result carries only *domain* answers. Infrastructure faults and deadline expiries
// are signalled as a non-nil error from the Service and classified as internal_failure
// or timeout_* at the transport edge (measurement-contract §6); they are not encoded
// as a Result here.
type Result struct {
	Outcome Outcome
	Reason  Reason
	Replay  bool

	// ReservationID is set when a reserve created a hold (and echoed on its replay),
	// and identifies the target of a successful cancel.
	ReservationID ReservationID
	// BookingID is set when a confirm created a booking (and echoed on its replay).
	BookingID BookingID
}

// Refusal builds a business_refusal Result with the given reason.
func Refusal(reason Reason) Result {
	return Result{Outcome: OutcomeBusinessRefusal, Reason: reason}
}
