package domain

import "testing"

// Both halves of an identity must be present, and the reason is sharper than "reject
// empty input": a half-empty ref is a *different* identity, not a weaker one. Two refs
// sharing an empty half compare equal, so one user could replay another's idempotency
// record or occupy their schedule.
//
// Each half is checked independently. A test that only tried the fully-empty ref would
// pass against a check for either half alone.
func TestUserRefIsValid(t *testing.T) {
	cases := []struct {
		name string
		ref  UserRef
		want bool
	}{
		{"both halves present", UserRef{OrganisationID: "org-1", UserID: "user-1"}, true},
		{"missing organisation", UserRef{UserID: "user-1"}, false},
		{"missing user", UserRef{OrganisationID: "org-1"}, false},
		{"zero value", UserRef{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.ref.IsValid(); got != c.want {
				t.Errorf("IsValid() = %v, want %v", got, c.want)
			}
		})
	}
}

// Two users of different organisations whose organisation half is missing compare equal.
// That is what makes a half-empty identity dangerous rather than merely useless, and it
// is the property IsValid exists to keep out of the domain.
func TestHalfEmptyUserRefsCollide(t *testing.T) {
	a := UserRef{UserID: "user-1"} // org-a's user-1, organisation lost
	b := UserRef{UserID: "user-1"} // org-b's user-1, organisation lost
	if a != b {
		t.Fatal("half-empty refs did not compare equal; this test's premise no longer holds")
	}
	if a.IsValid() {
		t.Error("a ref that collides with another user's must not be valid")
	}
}
