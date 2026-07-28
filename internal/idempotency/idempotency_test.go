package idempotency

import (
	"testing"

	"github.com/nancysworld/alloca-go/internal/domain"
)

func hash(op domain.Operation, org domain.OrganisationID, user domain.UserID, target string, body []byte) string {
	return RequestHash(domain.ContractVersion, op, domain.UserRef{OrganisationID: org, UserID: user}, target, body)
}

func TestRequestHashDeterministic(t *testing.T) {
	a := hash(domain.OpReserve, "org-1", "user-1", "slot-1", nil)
	b := hash(domain.OpReserve, "org-1", "user-1", "slot-1", nil)
	if a != b {
		t.Fatalf("same inputs produced different hashes: %s != %s", a, b)
	}
}

func TestRequestHashDistinguishesSemanticFields(t *testing.T) {
	base := hash(domain.OpReserve, "org-1", "user-1", "slot-1", nil)
	cases := map[string]string{
		"different target":       hash(domain.OpReserve, "org-1", "user-1", "slot-2", nil),
		"different operation":    hash(domain.OpConfirm, "org-1", "user-1", "slot-1", nil),
		"different organisation": hash(domain.OpReserve, "org-2", "user-1", "slot-1", nil),
		"different user":         hash(domain.OpReserve, "org-1", "user-2", "slot-1", nil),
		"different body":         hash(domain.OpReserve, "org-1", "user-1", "slot-1", []byte(`{"x":1}`)),
	}
	for name, h := range cases {
		if h == base {
			t.Errorf("%s did not change the request hash", name)
		}
	}
}

func TestRequestHashNilAndEmptyBodyEqual(t *testing.T) {
	if hash(domain.OpReserve, "org-1", "user-1", "slot-1", nil) !=
		hash(domain.OpReserve, "org-1", "user-1", "slot-1", []byte{}) {
		t.Fatal("nil and empty body must hash identically")
	}
}

func TestResolve(t *testing.T) {
	rec := domain.IdempotencyRecord{RequestHash: "abc"}
	if got := Resolve(rec, "abc"); got != Replay {
		t.Errorf("matching hash: got %v, want Replay", got)
	}
	if got := Resolve(rec, "xyz"); got != Conflict {
		t.Errorf("differing hash: got %v, want Conflict", got)
	}
}

func TestScopeExcludesTarget(t *testing.T) {
	got := Scope(domain.UserRef{OrganisationID: "org-1", UserID: "user-1"}, domain.OpReserve, "key-1")
	want := domain.ScopeKey{UserRef: domain.UserRef{OrganisationID: "org-1", UserID: "user-1"}, Operation: domain.OpReserve, Key: "key-1"}
	if got != want {
		t.Errorf("Scope = %+v, want %+v", got, want)
	}
}
