package loadgen

import (
	"fmt"
	"sort"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Router decides which service unit a request goes to.
//
// The generator holds the *same* versioned placement map the services hold
// (deployment-architecture.md §4). That is deliberate and it is also the reason the service
// enforces placement itself: if the generator were the only router, "no supported request
// reached the wrong authority" would be a property of this file rather than of Alloca, and
// the VAL-COR-5 misrouting control exists precisely to prove the service does not
// trust it (REQ-ROUTE-1).
//
// **Mutations route by user organisation, reads by slot organisation.** User-home owns the
// schedule claim and the client idempotency scope, so it is the stable home for a mutation
// and for every replay of it; a read routes to whichever authority holds the rows it
// returns (horizontal-database-authority §4.1, §5.5).
type Router struct {
	placement domain.Placement
	// endpoints maps an authority to the base URL of the unit serving it. Empty for a
	// single-target router, which sends everything to one place.
	endpoints map[domain.AuthorityID]string
	single    string
}

// SingleTarget routes every request to one base URL.
//
// This is the unsharded case — one authority owning everything — and it is what every run
// before PR3b did. It is a distinct constructor rather than a one-entry map because a run
// against one service should not have to invent an authority name to describe itself.
func SingleTarget(baseURL string) Router {
	return Router{single: baseURL}
}

// NewRouter builds a placement-aware router and refuses a topology it could not route.
//
// Every authority the map names must have an endpoint: a map that places an organisation on
// an authority the generator cannot reach would fail at the first request for that
// organisation, deep in a run, rather than at setup. Extra endpoints are refused for the
// same reason in the other direction — an endpoint nothing routes to is a unit the run
// believes it is exercising and is not.
func NewRouter(placement domain.Placement, endpoints map[domain.AuthorityID]string) (Router, error) {
	if placement.IsZero() {
		return Router{}, fmt.Errorf("loadgen: router needs a placement; use SingleTarget for an unsharded run")
	}
	if len(endpoints) == 0 {
		return Router{}, fmt.Errorf("loadgen: routing version %q names %v but no endpoints were supplied",
			placement.Version(), placement.Authorities())
	}

	named := map[domain.AuthorityID]struct{}{}
	for _, authority := range placement.Authorities() {
		named[authority] = struct{}{}
		if endpoints[authority] == "" {
			return Router{}, fmt.Errorf("loadgen: routing version %q places organisations on authority %q, but no endpoint was supplied for it",
				placement.Version(), authority)
		}
	}
	for authority := range endpoints {
		if _, ok := named[authority]; !ok {
			return Router{}, fmt.Errorf("loadgen: an endpoint was supplied for authority %q, which routing version %q never names; the run would believe it was exercising a unit it never reaches",
				authority, placement.Version())
		}
	}

	copied := make(map[domain.AuthorityID]string, len(endpoints))
	for authority, url := range endpoints {
		copied[authority] = url
	}
	return Router{placement: placement, endpoints: copied}, nil
}

// For returns the base URL serving org.
//
// An unplaced organisation is an error rather than a default. Sending its traffic somewhere
// would produce a run whose requests were refused as misroutes and whose totals looked like
// a service defect.
func (r Router) For(org domain.OrganisationID) (string, error) {
	if r.single != "" {
		return r.single, nil
	}
	authority, ok := r.placement.AuthorityFor(org)
	if !ok {
		return "", fmt.Errorf("loadgen: organisation %q is not placed by routing version %q",
			org, r.placement.Version())
	}
	url, ok := r.endpoints[authority]
	if !ok {
		return "", fmt.Errorf("loadgen: no endpoint for authority %q", authority)
	}
	return url, nil
}

// RoutingVersion identifies the map this run used, for the manifest. A run whose artifacts
// cannot say which routing produced them cannot be compared with another.
func (r Router) RoutingVersion() string {
	if r.single != "" {
		return domain.UnshardedVersion
	}
	return r.placement.Version()
}

// Targets lists the base URLs this router can reach, sorted, so the harness can read every
// unit's /meta without being told separately where they are.
func (r Router) Targets() []string {
	if r.single != "" {
		return []string{r.single}
	}
	out := make([]string, 0, len(r.endpoints))
	for _, url := range r.endpoints {
		out = append(out, url)
	}
	sort.Strings(out)
	return out
}

// Placement is the map this router routes by, so the verifier and the manifest can record
// the assignment rather than be handed it a second time and risk disagreeing.
func (r Router) Placement() domain.Placement { return r.placement }

// Colocated reports whether two organisations share an authority under this routing — the
// Phase 1 support boundary. A single-target run colocates everything, which is what makes
// the supported workloads generate identical traffic against one service or several.
func (r Router) Colocated(a, b domain.OrganisationID) bool {
	if r.single != "" {
		return true
	}
	colocated, placed := r.placement.Colocated(a, b)
	return colocated && placed
}
