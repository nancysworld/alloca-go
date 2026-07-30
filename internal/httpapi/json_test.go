package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
)

func postWithBody(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/v1/slots/org-1/slot-1/reservations", strings.NewReader(body))
}

// Malformed input is rejected here, before the request becomes a domain operation, so
// invalid_request never inflates business_refusal (transaction-semantics §8).
func TestDecodeIdentity(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantUser   domain.UserRef
		wantDetail string // substring; empty means the request must be accepted
	}{
		{
			name:     "well formed",
			body:     `{"user_organisation_id":"org-1","user_id":"user-1"}`,
			wantUser: domain.UserRef{OrganisationID: "org-1", UserID: "user-1"},
		},
		{
			// Field order is not part of the contract, and neither is whitespace: the
			// request hash is computed over typed fields, not over the bytes we received
			// (see booking.go). The same logical request must decode identically however
			// the client chose to format it.
			name:     "field order and whitespace do not matter",
			body:     "{\n  \"user_id\" : \"user-1\" ,\n  \"user_organisation_id\":\"org-1\"\n}",
			wantUser: domain.UserRef{OrganisationID: "org-1", UserID: "user-1"},
		},
		{"empty body", ``, domain.UserRef{}, "body is empty"},
		{"not json", `not json at all`, domain.UserRef{}, "not valid JSON"},
		{"missing organisation", `{"user_id":"user-1"}`, domain.UserRef{}, "user_organisation_id is required"},
		{"missing user", `{"user_organisation_id":"org-1"}`, domain.UserRef{}, "user_id is required"},
		{"empty organisation", `{"user_organisation_id":"","user_id":"user-1"}`, domain.UserRef{}, "user_organisation_id is required"},
		{"empty user", `{"user_organisation_id":"org-1","user_id":""}`, domain.UserRef{}, "user_id is required"},
		{
			// A typo that is silently ignored becomes a client that believes it sent a
			// field the server never read.
			name:       "unknown field",
			body:       `{"user_organisation_id":"org-1","user_id":"user-1","userId":"user-2"}`,
			wantDetail: "not valid JSON",
		},
		{
			name:       "two objects",
			body:       `{"user_organisation_id":"org-1","user_id":"user-1"}{"user_id":"x"}`,
			wantDetail: "exactly one JSON object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, detail := decodeIdentity(postWithBody(tc.body), maxBodyBytes)
			if tc.wantDetail == "" {
				if detail != "" {
					t.Fatalf("rejected a valid body: %s", detail)
				}
				if user != tc.wantUser {
					t.Errorf("user = %+v, want %+v", user, tc.wantUser)
				}
				return
			}
			if detail == "" {
				t.Fatalf("accepted an invalid body, want a detail mentioning %q", tc.wantDetail)
			}
			if !strings.Contains(detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to mention %q", detail, tc.wantDetail)
			}
		})
	}
}

// An unbounded read is a denial-of-service vector on an endpoint that needs two short
// strings.
func TestDecodeIdentityRejectsAnOversizedBody(t *testing.T) {
	huge := `{"user_organisation_id":"` + strings.Repeat("a", int(maxBodyBytes)+1) + `","user_id":"u"}`
	_, detail := decodeIdentity(postWithBody(huge), maxBodyBytes)
	if !strings.Contains(detail, "too large") {
		t.Errorf("detail = %q, want it to report the body was too large", detail)
	}
}

func TestIdempotencyKeyRequired(t *testing.T) {
	r := postWithBody(`{}`)
	if _, detail := idempotencyKey(r); detail == "" {
		t.Error("accepted a request with no Idempotency-Key header")
	}

	r.Header.Set(headerIdempotencyKey, "key-1")
	key, detail := idempotencyKey(r)
	if detail != "" {
		t.Fatalf("rejected a request carrying a key: %s", detail)
	}
	if key != "key-1" {
		t.Errorf("key = %q, want key-1", key)
	}
}
