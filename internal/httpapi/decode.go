package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// headerIdempotencyKey carries the client's idempotency key. A header rather than a body
// field so the key is visible to proxies and logs without parsing the body, and so it
// reads the same for every operation.
const headerIdempotencyKey = "Idempotency-Key"

// maxBodyBytes bounds a request body. AG-M1 bodies carry two short identifiers, so this
// is generous by three orders of magnitude and exists only to stop an unbounded read.
const maxBodyBytes int64 = 4 << 10

// identityRequest is the body every booking endpoint takes: who the caller is. The user
// is a pair and both halves are required (transaction-semantics §1.1). AG-M1 has no
// authentication, so identity is asserted by the caller; when authentication arrives this
// struct is what it replaces, and nothing else moves.
//
// Note what is *not* here: nothing server-generated is accepted from a client — no
// timestamps, no reservation IDs on reserve, no TTL (§1.6).
type identityRequest struct {
	UserOrganisationID string `json:"user_organisation_id"`
	UserID             string `json:"user_id"`
}

// decodeIdentity reads and validates the request body, returning the caller's identity or
// a detail describing what was malformed. A non-empty detail means invalid_request: the
// request never reaches the mutation path (transaction-semantics §8), and the detail
// describes the *request*, never anything internal.
//
// Unknown fields are rejected. A caller who sends "userId" instead of "user_id" would
// otherwise get a confusing invalid_request about a field they believe they sent, and
// silently ignoring input is how a client comes to depend on a field the server never read.
func decodeIdentity(r *http.Request, maxBytes int64) (domain.UserRef, string) {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	dec.DisallowUnknownFields()

	var body identityRequest
	if err := dec.Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return domain.UserRef{}, "request body is empty: expected a JSON object with user_organisation_id and user_id"
		case errors.As(err, &maxErr):
			return domain.UserRef{}, "request body is too large"
		default:
			// The decoder's message names the offending field or type and contains
			// nothing internal, so it is safe and useful to pass on.
			return domain.UserRef{}, "request body is not valid JSON for this endpoint: " + err.Error()
		}
	}
	// A second value in the stream means the caller sent more than one JSON object;
	// accepting the first silently would hide that.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return domain.UserRef{}, "request body must contain exactly one JSON object"
	}

	switch {
	case body.UserOrganisationID == "":
		return domain.UserRef{}, "user_organisation_id is required"
	case body.UserID == "":
		return domain.UserRef{}, "user_id is required"
	}
	return domain.UserRef{
		OrganisationID: domain.OrganisationID(body.UserOrganisationID),
		UserID:         domain.UserID(body.UserID),
	}, ""
}

// idempotencyKey returns the request's key, or a detail when it is absent.
func idempotencyKey(r *http.Request) (string, string) {
	key := r.Header.Get(headerIdempotencyKey)
	if key == "" {
		return "", "the " + headerIdempotencyKey + " header is required"
	}
	return key, ""
}
