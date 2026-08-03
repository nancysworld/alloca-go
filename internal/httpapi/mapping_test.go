package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// contractOutcomes is every terminal outcome the measurement contract declares (§4),
// including the ones AG-M1 never produces. It is the list the mapping must be total
// over; adding an Outcome to the domain means adding it here, and the totality test
// below is what makes that visible rather than silent.
var contractOutcomes = []domain.Outcome{
	domain.OutcomeAdmittedSuccess,
	domain.OutcomeBusinessRefusal,
	domain.OutcomeInvalidRequest,
	domain.OutcomeRetryAfter,
	domain.OutcomeAdmissionRejected,
	domain.OutcomeQueuePosition,
	domain.OutcomeUnknownReplayable,
	domain.OutcomeTimeoutClient,
	domain.OutcomeTimeoutServer,
	domain.OutcomeTimeoutDB,
	domain.OutcomeTimeoutLB,
	domain.OutcomeInternalFailure,
}

// The mapping must answer for every outcome the contract declares. An unmapped outcome
// would otherwise render as a plausible 500 and nobody would learn that a new outcome
// had reached the transport unclassified.
func TestStatusForOutcomeIsTotal(t *testing.T) {
	for _, outcome := range contractOutcomes {
		if _, known := StatusForOutcome(outcome, ""); !known {
			t.Errorf("outcome %q has no HTTP mapping", outcome)
		}
	}
}

// The negative control for the test above: if the mapping claimed to know every outcome
// it was handed, the totality test would pass vacuously and prove nothing.
func TestStatusForOutcomeRejectsAnUnknownOutcome(t *testing.T) {
	status, known := StatusForOutcome(domain.Outcome("not_a_real_outcome"), "")
	if known {
		t.Error("mapping reported an invented outcome as known, so the totality test above proves nothing")
	}
	if status != http.StatusInternalServerError {
		t.Errorf("unknown outcome rendered %d, want 500 as the safe fallback", status)
	}
}

func TestStatusForOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome domain.Outcome
		reason  domain.Reason
		want    int
	}{
		{"success", domain.OutcomeAdmittedSuccess, "", http.StatusOK},
		// A replay is the same outcome with Replay=true, never an outcome of its own,
		// so it maps identically. The client distinguishes them by the body's flag.
		{"no capacity is a conflict", domain.OutcomeBusinessRefusal, domain.ReasonNoCapacity, http.StatusConflict},
		{"schedule conflict is a conflict", domain.OutcomeBusinessRefusal, domain.ReasonScheduleConflict, http.StatusConflict},
		{"slot closed is a conflict", domain.OutcomeBusinessRefusal, domain.ReasonSlotClosed, http.StatusConflict},
		{"idempotency conflict is a conflict", domain.OutcomeBusinessRefusal, domain.ReasonIdempotencyConflict, http.StatusConflict},
		// The one refusal that is not 409: the named target does not exist.
		{"unknown target is not found", domain.OutcomeBusinessRefusal, domain.ReasonUnknownTarget, http.StatusNotFound},
		{"invalid request", domain.OutcomeInvalidRequest, "", http.StatusBadRequest},
		{"unknown replayable", domain.OutcomeUnknownReplayable, "", http.StatusInternalServerError},
		{"client timeout", domain.OutcomeTimeoutClient, "", http.StatusRequestTimeout},
		{"server timeout", domain.OutcomeTimeoutServer, "", http.StatusGatewayTimeout},
		{"db timeout", domain.OutcomeTimeoutDB, "", http.StatusGatewayTimeout},
		{"internal failure", domain.OutcomeInternalFailure, "", http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, known := StatusForOutcome(tc.outcome, tc.reason)
			if !known {
				t.Fatalf("outcome %q/%q is unmapped", tc.outcome, tc.reason)
			}
			if got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

// Faults arrive as errors, not as a Result, so the transport must classify them through
// the domain's own mapping. This is the second half of the mapping's totality: a fault
// the edge failed to classify would be indistinguishable from a successful response
// with an empty body.
func TestResponseForFault(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantOutcome domain.Outcome
	}{
		{"commit unknown", fmt.Errorf("wrapped: %w", domain.ErrCommitUnknown), http.StatusInternalServerError, domain.OutcomeUnknownReplayable},
		{"database timeout", fmt.Errorf("wrapped: %w", domain.ErrDBTimeout), http.StatusGatewayTimeout, domain.OutcomeTimeoutDB},
		{"caller cancelled", fmt.Errorf("wrapped: %w", context.Canceled), http.StatusRequestTimeout, domain.OutcomeTimeoutClient},
		{"server deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, domain.OutcomeTimeoutServer},
		{"anything else", errors.New("boom"), http.StatusInternalServerError, domain.OutcomeInternalFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body := responseForFault(tc.err)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if body.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", body.Outcome, tc.wantOutcome)
			}
		})
	}
}

// A fault's response must never carry the underlying error text: it can contain SQL,
// connection strings, or invariant detail, none of which belongs in a client response.
func TestResponseForFaultDoesNotLeakTheError(t *testing.T) {
	const secret = "pgx: connection to host=db.internal user=alloca failed"
	_, body := responseForFault(errors.New(secret))
	if body.Message == secret {
		t.Fatal("the underlying error text was returned to the client verbatim")
	}
	if body.Message != msgInternal {
		t.Errorf("message = %q, want the fixed internal-failure message", body.Message)
	}
}

// The ambiguous commit is the one outcome whose message a client must act on, and the
// action is counter-intuitive: reuse the key that just appeared to fail. Getting this
// wrong duplicates bookings, so it is asserted rather than assumed.
func TestUnknownReplayableTellsTheClientToReuseTheKey(t *testing.T) {
	_, body := responseForFault(domain.ErrCommitUnknown)
	if body.Outcome != domain.OutcomeUnknownReplayable {
		t.Fatalf("outcome = %q, want unknown_replayable", body.Outcome)
	}
	for _, want := range []string{"SAME Idempotency-Key", "Do not retry with a new key"} {
		if !strings.Contains(body.Message, want) {
			t.Errorf("message does not mention %q: %q", want, body.Message)
		}
	}
}

func TestResponseForResultCarriesTheClassification(t *testing.T) {
	status, body := responseForResult(domain.Result{
		Outcome:       domain.OutcomeAdmittedSuccess,
		Replay:        true,
		ReservationID: "res_1",
	})
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if !body.Replay {
		t.Error("replay flag lost: a client cannot tell a replay from a fresh mutation")
	}
	if body.ReservationID != "res_1" {
		t.Errorf("reservation_id = %q, want res_1", body.ReservationID)
	}
}
