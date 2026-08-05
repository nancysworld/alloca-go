package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// AuthorityID names one writable PostgreSQL authority. It is a logical name, not a
// DSN: business code asks which authority owns an organisation and never derives a
// connection string, which is the routing boundary the horizontal-database-authority
// note keeps stable for Phase 2 (design note §4.3, obligation 2).
//
// It is bounded by topology — a deployment has as many authorities as it has database
// instances — so unlike an organisation it is safe as a metric label
// (ag-sept-plan-new.md §6.1).
type AuthorityID string

// Placement is the versioned organisation-to-authority map: the deployment's answer to
// "which writable authority owns this organisation's rows?".
//
// It is immutable for the duration of a run. That is not a convenience: two components
// disagreeing about placement would write one organisation to two authorities and
// silently divide its source of truth, which is the split-brain risk the design note
// records as §7.3. Every artifact records Version so a result can be read against the
// routing that produced it.
//
// There are three states and they are deliberately distinct. A parsed map routes what
// it names. Unsharded routes everything to one authority, which is a single-authority
// deployment stating its topology. The zero Placement routes *nothing* and fails
// ValidateUnit — because a service that silently served every organisation from one
// authority after its map failed to load is exactly the failure this type exists to
// make impossible. Absent configuration is a choice; a broken document is not.
type Placement struct {
	version string
	// homes is never mutated after construction, and Placement is only ever handed
	// out by value, so a caller cannot reach in and re-home an organisation mid-run.
	homes map[OrganisationID]AuthorityID

	// sole is set for an unsharded deployment: one authority owns every organisation,
	// present and future, so there is nothing to enumerate. See Unsharded.
	sole    AuthorityID
	unshard bool
}

// UnshardedVersion is the routing version reported by a single-authority deployment.
// It is a real version rather than an empty string because every artifact must record
// the routing that produced it, and "there was one authority" is an answer.
const UnshardedVersion = "unsharded"

// Unsharded returns the placement of a single-authority deployment: one writable
// authority owns every organisation, so every pair of organisations is colocated and
// every supported booking keeps the local transaction it has always used.
//
// This is what makes the Phase 1 policy *inert* where it should be. A deployment that
// has not been sharded gets the behaviour it had before placement existed — including
// cross-organisation booking, which is shipped and tested (INV-13) — without the
// service needing an "if sharded" branch on the request path. The map answers
// "colocated?" the same way whatever the topology; only the answer changes.
//
// It is deliberately a distinct constructor rather than the zero value. Absent
// configuration is a choice a deployment can make; a placement document that failed to
// load is not, and the two must never collapse into the same behaviour.
func Unsharded(authority AuthorityID) Placement {
	return Placement{version: UnshardedVersion, sole: authority, unshard: true}
}

// IsUnsharded reports whether this is a single-authority deployment.
func (p Placement) IsUnsharded() bool { return p.unshard }

// placementDoc is the wire form parsed by ParsePlacement. It is deliberately explicit
// rather than a bare map: the version is not optional, and a document that cannot say
// which routing it describes cannot be recorded against a measurement.
//
// Homes stays raw so ParsePlacement can walk its keys itself. Decoding straight into a
// map would silently collapse a duplicate organisation — see decodeHomes.
type placementDoc struct {
	Version string          `json:"version"`
	Homes   json.RawMessage `json:"homes"`
}

// decodeHomes turns the raw homes object into the map, rejecting an organisation that
// appears twice.
//
// Go's decoder resolves duplicate object keys by letting the last one win, so decoding
// into a map cannot see the ambiguity: `{"org-a":"authority-1","org-a":"authority-2"}`
// becomes a perfectly well-formed one-entry map. That is the single most dangerous thing
// a placement document can do quietly — an operator reading the file sees one routing and
// the service uses another — and the design note requires setup to fail when an
// organisation is "assigned more than once" (§5.1). So the keys are walked as tokens.
func decodeHomes(raw json.RawMessage, version string) (map[OrganisationID]AuthorityID, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("domain: placement version %q has no homes object", version)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("domain: placement version %q: reading homes: %w", version, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("domain: placement version %q: homes must be an object", version)
	}

	homes := map[OrganisationID]AuthorityID{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("domain: placement version %q: reading an organisation: %w", version, err)
		}
		org := OrganisationID(keyTok.(string))

		var authority AuthorityID
		if err := dec.Decode(&authority); err != nil {
			return nil, fmt.Errorf("domain: placement version %q: reading the authority for %q: %w", version, org, err)
		}
		if _, dup := homes[org]; dup {
			return nil, fmt.Errorf("domain: placement version %q assigns organisation %q more than once; one organisation, one writable home authority", version, org)
		}
		homes[org] = authority
	}
	return homes, nil
}

// ParsePlacement builds a Placement from its JSON document:
//
//	{"version": "v1", "homes": {"org-a": "authority-1", "org-b": "authority-2"}}
//
// It rejects a document that cannot describe one unambiguous routing: a missing or
// empty version, an empty map, an organisation with no authority, an authority with no
// name, an organisation assigned more than once, or anything at all after the document.
//
// Parsing is strict about unknown fields so a typo in a placement document fails at
// startup rather than silently routing by a default.
func ParsePlacement(data []byte) (Placement, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var doc placementDoc
	if err := dec.Decode(&doc); err != nil {
		return Placement{}, fmt.Errorf("domain: placement document: %w", err)
	}
	// One document, one routing decision. Decode stops at the end of the first JSON
	// value, so without this a file holding two placements would be accepted using only
	// the first — and the second, which someone wrote deliberately, would vanish.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Placement{}, fmt.Errorf("domain: placement document has trailing content after the routing map; one document, one routing decision")
	}

	if strings.TrimSpace(doc.Version) == "" {
		return Placement{}, fmt.Errorf("domain: placement document has no version; every artifact must record the routing it used")
	}
	homes, err := decodeHomes(doc.Homes, doc.Version)
	if err != nil {
		return Placement{}, err
	}
	if len(homes) == 0 {
		return Placement{}, fmt.Errorf("domain: placement version %q assigns no organisations", doc.Version)
	}

	for org, authority := range homes {
		if strings.TrimSpace(string(org)) == "" {
			return Placement{}, fmt.Errorf("domain: placement version %q has an empty organisation identifier", doc.Version)
		}
		if strings.TrimSpace(string(authority)) == "" {
			return Placement{}, fmt.Errorf("domain: placement version %q assigns organisation %q to an empty authority", doc.Version, org)
		}
	}

	return Placement{version: doc.Version, homes: homes}, nil
}

// Version is the routing version this map describes. It is recorded in operational
// metadata and in every run artifact so a measurement can be read against the placement
// that produced it.
func (p Placement) Version() string { return p.version }

// IsZero reports whether this Placement routes nothing. An unsharded placement routes
// everything and is never zero.
func (p Placement) IsZero() bool { return !p.unshard && len(p.homes) == 0 }

// AuthorityFor returns the writable authority that owns org, and whether org is placed
// at all.
//
// An unplaced organisation is deliberately not defaulted to any authority. Guessing
// would write its rows somewhere, and "somewhere" is how one organisation's source of
// truth ends up split across two writers.
func (p Placement) AuthorityFor(org OrganisationID) (AuthorityID, bool) {
	if p.unshard {
		// One authority owns every organisation, including ones no map has heard of.
		// An unsharded deployment has no concept of an unplaced organisation.
		return p.sole, true
	}
	a, ok := p.homes[org]
	return a, ok
}

// Colocated reports whether two organisations resolve to the same writable authority,
// and whether both are placed at all.
//
// This is the Phase 1 support boundary in one call (design note §4.1):
//
//	authority(slot_organisation_id) == authority(user_organisation_id)
//
// It is deliberately weaker than organisation-identifier equality. A user registered
// with one organisation booking a slot owned by another is supported whenever the two
// share an authority, which is what keeps the shipped cross-organisation booking
// behaviour — and INV-13 with it — exercised end to end.
func (p Placement) Colocated(a, b OrganisationID) (colocated, placed bool) {
	authorityA, okA := p.AuthorityFor(a)
	authorityB, okB := p.AuthorityFor(b)
	if !okA || !okB {
		return false, false
	}
	return authorityA == authorityB, true
}

// Organisations returns the organisations assigned to authority, sorted, so a service
// unit can state what it serves and a test can assert it without depending on map
// iteration order.
func (p Placement) Organisations(authority AuthorityID) []OrganisationID {
	if p.unshard {
		// Every organisation, which is not an enumerable set. Callers that need a list
		// are asking a sharded question; ValidateUnit handles the unsharded case itself.
		return nil
	}
	var orgs []OrganisationID
	for org, a := range p.homes {
		if a == authority {
			orgs = append(orgs, org)
		}
	}
	slices.Sort(orgs)
	return orgs
}

// Authorities returns every authority named by the map, sorted. Setup uses it to check
// that each participating authority is reachable and schema-compatible before traffic
// starts.
func (p Placement) Authorities() []AuthorityID {
	if p.unshard {
		return []AuthorityID{p.sole}
	}
	seen := make(map[AuthorityID]struct{}, len(p.homes))
	var out []AuthorityID
	for _, a := range p.homes {
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// Serves reports whether authority owns org. It is the question a shard-affine service
// unit asks about every request it receives, before doing any work.
func (p Placement) Serves(authority AuthorityID, org OrganisationID) bool {
	a, ok := p.AuthorityFor(org)
	return ok && a == authority
}

// ValidateUnit is the startup gate for one shard-affine service unit: the map must
// route something, must name this unit's authority, and must give that authority at
// least one organisation to serve.
//
// A unit that starts against a map naming no organisations for it would pass every
// health check and refuse every request, which reads as a routing bug for as long as it
// takes someone to check the map. Failing here makes it a startup error instead.
func (p Placement) ValidateUnit(authority AuthorityID) error {
	if p.unshard {
		if authority != p.sole {
			return fmt.Errorf("domain: unsharded placement names authority %q, but this unit is %q", p.sole, authority)
		}
		return nil
	}
	if p.IsZero() {
		return fmt.Errorf("domain: placement is empty; a shard-affine unit cannot start without a routing map")
	}
	if strings.TrimSpace(string(authority)) == "" {
		return fmt.Errorf("domain: placement version %q: this unit has no authority identifier", p.version)
	}
	if len(p.Organisations(authority)) == 0 {
		return fmt.Errorf("domain: placement version %q assigns no organisation to authority %q (it names %v)", p.version, authority, p.Authorities())
	}
	return nil
}
