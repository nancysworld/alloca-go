package loadgen

import (
	"context"
	"fmt"
	"sort"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// OrgGroup is the organisations one authority owns, with the slots seeded for each.
//
// Grouping by authority rather than listing organisations flat is what lets a workload pick
// a *supported* pair without consulting the placement map on every request: any two
// organisations inside one group are colocated by construction, and any two across groups
// are not.
type OrgGroup struct {
	Authority domain.AuthorityID
	// Slots are the seeded slots of every organisation this authority owns. A slot carries
	// its own organisation, so a workload picks the pair (user org, slot) rather than
	// tracking which slots belong to whom.
	Slots []Slot
	// Orgs are the organisations this authority owns, sorted, used to mint user identities.
	Orgs []domain.OrganisationID
}

// NewOrgGroups derives the groups from a placement map and the slots seeded for each
// organisation.
//
// It refuses a group with no slots or no organisations: a workload that drew from it would
// divide by zero at some seq deep in a run, which is a harness crash reported as a service
// result.
func NewOrgGroups(placement domain.Placement, slotsByOrg map[domain.OrganisationID][]Slot) ([]OrgGroup, error) {
	if placement.IsZero() {
		return nil, fmt.Errorf("loadgen: org groups need a placement")
	}

	var groups []OrgGroup
	for _, authority := range placement.Authorities() {
		group := OrgGroup{Authority: authority, Orgs: placement.Organisations(authority)}
		for _, org := range group.Orgs {
			group.Slots = append(group.Slots, slotsByOrg[org]...)
		}
		if len(group.Slots) == 0 {
			return nil, fmt.Errorf("loadgen: authority %q owns %v but no slots were seeded for any of them",
				authority, group.Orgs)
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("loadgen: routing version %q names no authorities", placement.Version())
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Authority < groups[j].Authority })
	return groups, nil
}

// MultiOrgDispersed is the §5.6 dispersed control: traffic spread across several
// organisations placed on different authorities, with every booking satisfying the Phase 1
// support boundary.
//
// It deliberately generates **colocated cross-organisation** bookings as well as
// same-organisation ones. A user of one organisation booking a slot owned by another, both
// on one authority, is supported behaviour and the case INV-13 exists for; a workload that
// only ever paired an organisation with itself would leave the most interesting supported
// path unexercised while appearing to cover the dispersed case.
type MultiOrgDispersed struct {
	Groups []OrgGroup
	// Confirm drives reserve→confirm rather than reserve alone. A confirmed claim has
	// expires_at IS NULL and is never expiry-reaped, so a confirming workload leaves
	// permanent rows and needs the same clean-start discipline as the hot-identity control.
	Confirm bool
}

func (MultiOrgDispersed) Name() string         { return "multi-org-dispersed" }
func (MultiOrgDispersed) IntendsReplays() bool { return false }

func (m MultiOrgDispersed) Do(ctx context.Context, c *Client, seq int) []Response {
	group := m.Groups[seq%len(m.Groups)]

	// requestsIntoGroup is this group's own request counter, so each authority walks its
	// dataset independently of how many authorities there are.
	//
	// **The user's organisation must not advance on the same cadence as the group.** It
	// selected with `seq % len(Orgs)` before, which is the *same* expression that picks the
	// group whenever the counts are equal — and with the committed fixture, two authorities
	// owning two organisations each, they are. Authority 1 then only ever drew its first
	// organisation as the user and authority 2 only ever its second, so half the placed
	// organisations appeared as slot owners and never exercised mutation routing as a user
	// home at all — the routing that decides which authority owns the schedule claim and the
	// idempotency scope. Deriving it from the quotient breaks the lock.
	requestsIntoGroup := seq / len(m.Groups)
	userOrg := group.Orgs[requestsIntoGroup%len(group.Orgs)]

	// The slot then advances once the user's organisation has walked its own cycle, so
	// consecutive requests alternate between same-organisation and colocated
	// cross-organisation pairs. Both are supported; both must be exercised, and INV-13 is
	// about the second.
	slot := group.Slots[(requestsIntoGroup/len(group.Orgs))%len(group.Slots)]

	user := User{OrganisationID: userOrg, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}

	reserved := c.Reserve(ctx, user, slot, c.key(m.Name(), seq, "reserve"))
	out := []Response{reserved}
	if !m.Confirm || reserved.ReservationID == "" {
		return out
	}
	return append(out, c.Confirm(ctx, user, reserved.ReservationID, c.key(m.Name(), seq, "confirm")))
}

// HotOrganisation concentrates every request on one organisation, so one authority carries
// the whole load while the others carry none.
//
// It is the §5.6 variant the failure-isolation experiment needs: it makes "one saturated or
// unavailable authority bounds its own organisations and no others" a thing that can be
// observed rather than asserted, because the unaffected authorities have traffic of their
// own to keep serving.
type HotOrganisation struct {
	Org   domain.OrganisationID
	Slots []Slot
}

func (HotOrganisation) Name() string         { return "hot-organisation" }
func (HotOrganisation) IntendsReplays() bool { return false }

func (h HotOrganisation) Do(ctx context.Context, c *Client, seq int) []Response {
	slot := h.Slots[seq%len(h.Slots)]
	user := User{OrganisationID: h.Org, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}
	return []Response{c.Reserve(ctx, user, slot, c.key(h.Name(), seq, "reserve"))}
}

// CrossAuthorityControl issues reserves whose two organisations resolve to *different*
// authorities, which Phase 1 refuses.
//
// It is a **bounded control reported as its own evidence class**, never a share of the
// supported workload (ag-sept/milestone-validation.md §3.5). A refusal is decided before any slot work
// and is therefore far cheaper than a real booking; mixing it into the dispersed run would
// flatter both goodput and latency by exactly the proportion of refusals it contained.
//
// It routes to the *user's* authority, which is the correct route for the mutation. That is
// the point: the request reaches the unit that owns the caller, is understood, and is
// refused on policy — as opposed to a misrouted request, which never reaches the domain at
// all and is refused at the edge as invalid_request.
type CrossAuthorityControl struct {
	Groups []OrgGroup
}

func (CrossAuthorityControl) Name() string         { return "cross-authority-control" }
func (CrossAuthorityControl) IntendsReplays() bool { return false }

func (x CrossAuthorityControl) Do(ctx context.Context, c *Client, seq int) []Response {
	if len(x.Groups) < 2 {
		// Nothing to refuse: with one authority every pair is colocated. Returning an
		// invalid response rather than a misleading success keeps a single-authority run
		// from reporting that it exercised a control it could not.
		return []Response{{
			Operation: domain.OpReserve,
			Invalid:   "cross-authority control needs at least two authorities; this topology has one",
		}}
	}

	userGroup := x.Groups[seq%len(x.Groups)]
	slotGroup := x.Groups[(seq+1)%len(x.Groups)]

	userOrg := userGroup.Orgs[seq%len(userGroup.Orgs)]
	slot := slotGroup.Slots[seq%len(slotGroup.Slots)]
	user := User{OrganisationID: userOrg, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}

	return []Response{c.Reserve(ctx, user, slot, c.key(x.Name(), seq, "reserve"))}
}

var (
	_ Workload = MultiOrgDispersed{}
	_ Workload = HotOrganisation{}
	_ Workload = CrossAuthorityControl{}
)
