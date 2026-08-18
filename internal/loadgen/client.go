// Package loadgen is the external load harness AG-Sept measures with.
//
// It is a client of the service's HTTP contract and shares no state with it: it holds no
// database credentials, and it reaches the service only over HTTP. That is what lets it run
// on separate compute for publishable capacity claims (measurement-contract §13.1). Reconciliation
// against persisted state is a separate step with its own credentials — see
// cmd/alloca-verify.
//
// # Response validation
//
// Every response is validated on two dimensions before it is counted: the HTTP status and
// the domain outcome must agree, checked against httpapi.StatusForOutcome — the same
// mapping the server answers with. measurement-contract §5.4 is explicit that "a run
// without response validation is not a capacity run", because an unchecked 200 counted as
// goodput is indistinguishable from a service that has silently stopped doing the work.
//
// Validation can be disabled, and that is deliberate: §5.5 requires a control that *fails
// when validation is silently disabled*. A mode that cannot be turned off cannot be proven
// to be on. Disabling it marks the run invalid in its own summary, so a run produced that
// way announces itself rather than passing as ordinary.
package loadgen

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/httpapi"
)

// Response is one completed request as the client saw it: the two dimensions that must
// agree, the orthogonal replay flag, and the latency the client measured.
type Response struct {
	Operation domain.Operation
	Status    int
	Outcome   domain.Outcome
	Reason    domain.Reason
	Replay    bool
	// ReservationID and BookingID echo the server's identifiers so a workflow can drive
	// confirm and cancel, and so the verifier can join client claims to persisted rows.
	ReservationID string
	BookingID     string
	Latency       time.Duration
	// Invalid names the validation failure, empty when the response validated. It is a
	// client-side judgement about the response, never a server outcome.
	Invalid string

	// completedAt is when the run observed this response, used to apply the warm-up
	// window. Unexported: it is an internal coordinate of one run, meaningless in a
	// summary that already reports the run's duration.
	completedAt time.Time
	// group is the shard group whose demand stream issued this request, empty in a
	// single-pool run. Unexported and stamped by the runner rather than the client: which
	// group offered the work is a property of the stream that issued it, while the client
	// only knows which authority the placement routed it to.
	group string
}

// body is the JSON shape every booking endpoint answers with. It mirrors httpapi's
// unexported response rather than importing it: that type is the server's private
// rendering, and a client that could not be written against the documented JSON would be
// testing the struct rather than the contract.
type body struct {
	Outcome       domain.Outcome `json:"outcome"`
	Reason        domain.Reason  `json:"reason"`
	Replay        bool           `json:"replay"`
	ReservationID string         `json:"reservation_id"`
	BookingID     string         `json:"booking_id"`
}

// Client issues booking requests against the service unit that owns each request's
// organisation, as decided by its Router.
type Client struct {
	http   *http.Client
	router Router
	// validate reports whether responses are checked. False is the negative control's
	// mode and nothing else — see the package comment.
	validate bool
	// ambiguous collects mutations whose outcome the client could not settle, so they
	// can be replayed under their own keys once the authority is back (ambiguous.go).
	ambiguous ambiguityRegister
	// runID scopes every idempotency key this client mints to one run, so a rerun cannot be
	// served from the previous run's records. See Client.key. Empty means unscoped, which is
	// the historical behaviour and what a test that asserts on literal keys expects.
	runID string
}

// NewRunID mints an identity for one run, used only to scope its idempotency keys.
//
// Random rather than a timestamp or a counter: two runs started inside the same clock tick, or
// a rerun after a crash, must not be able to mint the same keys. It is short because it is a
// discriminator, not a secret — nothing authenticates on it, and a key that already carries
// workload, sequence and step needs only enough entropy that two runs do not collide.
//
// It is deliberately *not* derived from the manifest or the commit: reruns of the same
// experiment at the same revision are exactly the case that must produce different keys.
func NewRunID() (string, error) {
	var b [runIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("loadgen: reading randomness for a run id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// runIDBytes is 8 bytes = 64 bits. Across the tens of runs a sweep produces, collision
// probability is negligible, and the key stays short enough to read in a log line.
const runIDBytes = 8

// WithRunID returns the client scoped to a run, so its idempotency keys cannot collide with
// another run's.
//
// It mutates and returns the receiver rather than copying: a Client owns an http.Client and an
// ambiguity register, and a copy would leave the ambiguous mutations of one run being resolved
// against the other. Callers hold one client per run, which is the shape the runner already
// assumes.
func (c *Client) WithRunID(id string) *Client {
	c.runID = id
	return c
}

// NewClient builds a Client routing everything to one base URL — the unsharded case, and
// what every run before PR3b did. validate=false is the response-validation negative
// control and marks every run it produces invalid.
func NewClient(baseURL string, timeout time.Duration, validate bool) *Client {
	return NewRoutedClient(SingleTarget(baseURL), timeout, validate)
}

// NewRoutedClient builds a Client that routes by placement.
func NewRoutedClient(router Router, timeout time.Duration, validate bool) *Client {
	return &Client{
		http: &http.Client{
			Timeout: timeout,
			// A generator that opens a fresh connection per request measures the dialer,
			// not the service. These bounds are generous enough that connection setup is
			// not the frontier being reported.
			Transport: &http.Transport{
				MaxIdleConns:        4096,
				MaxIdleConnsPerHost: 4096,
				MaxConnsPerHost:     0,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		router:   router,
		validate: validate,
	}
}

// Router exposes the routing this client uses, so the manifest can record the version and
// the harness can reach every unit's /meta without being told where they are twice.
func (c *Client) Router() Router { return c.router }

// Reserve holds one unit of a slot for a user.
//
// Routed by the *user's* organisation, not the slot's: user-home owns the schedule claim
// and the client idempotency scope, so it is where this mutation and every replay of it
// belong (horizontal-database-authority §4.1). Whether the slot's organisation is colocated
// is the service's decision, not the router's — sending it there and being refused is the
// supported behaviour, and routing around the refusal would hide it.
func (c *Client) Reserve(ctx context.Context, u User, slot Slot, key string) Response {
	base, err := c.router.For(u.OrganisationID)
	if err != nil {
		return unroutable(domain.OpReserve, err)
	}
	path := fmt.Sprintf("%s/v1/slots/%s/%s/reservations", base, slot.OrganisationID, slot.SlotID)
	return c.do(ctx, domain.OpReserve, path, u, key)
}

// Confirm turns a held reservation into a booking.
func (c *Client) Confirm(ctx context.Context, u User, reservationID, key string) Response {
	base, err := c.router.For(u.OrganisationID)
	if err != nil {
		return unroutable(domain.OpConfirm, err)
	}
	path := fmt.Sprintf("%s/v1/reservations/%s/confirm", base, reservationID)
	return c.do(ctx, domain.OpConfirm, path, u, key)
}

// Cancel releases a held reservation or an active booking.
func (c *Client) Cancel(ctx context.Context, u User, reservationID, key string) Response {
	base, err := c.router.For(u.OrganisationID)
	if err != nil {
		return unroutable(domain.OpCancel, err)
	}
	path := fmt.Sprintf("%s/v1/reservations/%s/cancel", base, reservationID)
	return c.do(ctx, domain.OpCancel, path, u, key)
}

// do issues one mutation and classifies what came back.
//
// A transport failure is classified as a timeout or an internal failure rather than
// discarded: a request the client gave up on still happened, and dropping it is how a
// client-side stall gets reported as server capacity.
func (c *Client) do(ctx context.Context, op domain.Operation, url string, u User, key string) Response {
	payload, err := json.Marshal(map[string]string{
		"user_organisation_id": string(u.OrganisationID),
		"user_id":              string(u.UserID),
	})
	if err != nil {
		return Response{Operation: op, Outcome: domain.OutcomeInternalFailure,
			Invalid: "marshalling request body: " + err.Error()}
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Response{Operation: op, Outcome: domain.OutcomeInternalFailure,
			Latency: time.Since(start), Invalid: "building request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	resp, err := c.http.Do(req)
	if err != nil {
		return Response{
			Operation: op,
			Outcome:   transportOutcome(ctx, err),
			Latency:   time.Since(start),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	latency := time.Since(start)
	if err != nil {
		return Response{Operation: op, Status: resp.StatusCode,
			Outcome: domain.OutcomeInternalFailure, Latency: latency,
			Invalid: "reading response body: " + err.Error()}
	}

	var b body
	out := Response{Operation: op, Status: resp.StatusCode, Latency: latency}
	if err := json.Unmarshal(raw, &b); err != nil {
		out.Outcome = domain.OutcomeInternalFailure
		out.Invalid = c.invalidf("response body is not valid JSON: %v", err)
		return out
	}
	out.Outcome, out.Reason, out.Replay = b.Outcome, b.Reason, b.Replay
	out.ReservationID, out.BookingID = b.ReservationID, b.BookingID
	out.Invalid = c.check(out)
	c.recordIfAmbiguous(op, url, u, key, out)
	return out
}

// recordIfAmbiguous registers a mutation the client could not settle.
//
// It is called on the one path that produces a parsed response, deliberately: a transport
// failure is classified as a timeout above and *is* a definite client-side fact — the
// request may still have committed, but that is the timeout contract's problem, not this
// register's. What lands here is the server's own admission that its commit outcome is
// unknown.
func (c *Client) recordIfAmbiguous(op domain.Operation, url string, u User, key string, resp Response) {
	if resp.Outcome != domain.OutcomeUnknownReplayable {
		return
	}
	c.ambiguous.record(Ambiguous{Operation: op, User: u, Key: key, path: url})
}

// check validates the two dimensions §5.4 requires, and returns the first disagreement.
//
// It returns "" when validation is disabled — which is the whole point of the control: with
// checking off, a response that contradicts itself passes silently, and the negative control
// is what proves the checking was on for every other run.
func (c *Client) check(r Response) string {
	if !c.validate {
		return ""
	}
	if !r.Outcome.IsKnown() {
		return fmt.Sprintf("outcome %q is not in the closed terminal-outcome set", r.Outcome)
	}
	want, known := httpapi.StatusForOutcome(r.Outcome, r.Reason)
	if !known {
		return fmt.Sprintf("no status mapping for outcome %q", r.Outcome)
	}
	if want != r.Status {
		return fmt.Sprintf("status %d contradicts outcome %q (contract says %d)",
			r.Status, r.Outcome, want)
	}
	if r.Outcome == domain.OutcomeBusinessRefusal && !r.Reason.IsKnown() {
		return fmt.Sprintf("business_refusal carries reason %q, which is not in the closed set",
			r.Reason)
	}
	if r.Outcome != domain.OutcomeBusinessRefusal && r.Reason != "" {
		return fmt.Sprintf("outcome %q carries refusal reason %q", r.Outcome, r.Reason)
	}
	return ""
}

// invalidf formats a validation failure, or returns "" when validation is disabled.
func (c *Client) invalidf(format string, args ...any) string {
	if !c.validate {
		return ""
	}
	return fmt.Sprintf(format, args...)
}

// transportOutcome classifies a request that never produced a response. The client's own
// deadline and the caller's cancellation are different events and stay different outcomes.
func transportOutcome(ctx context.Context, err error) domain.Outcome {
	if ctx.Err() != nil {
		return domain.OutcomeTimeoutClient
	}
	if isTimeout(err) {
		return domain.OutcomeTimeoutClient
	}
	return domain.OutcomeInternalFailure
}

func isTimeout(err error) bool {
	var nerr net.Error
	return errors.As(err, &nerr) && nerr.Timeout()
}

// unroutable reports a request the generator could not address at all.
//
// It is marked Invalid rather than given an outcome the service might have produced: no
// request was sent, so there is no server behaviour to classify, and counting it as an
// internal_failure would attribute a harness error to the thing being measured. Invalid is
// the harness's own channel for "this run is not trustworthy" (measurement-contract §5).
func unroutable(op domain.Operation, err error) Response {
	return Response{
		Operation: op,
		Outcome:   domain.OutcomeInternalFailure,
		Invalid:   "unroutable request: " + err.Error(),
	}
}
