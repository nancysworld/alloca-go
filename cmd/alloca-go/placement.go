package main

import (
	"fmt"
	"os"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
)

// resolvePlacement turns the unparsed placement configuration into the routing decision
// this service unit will use for its whole life, applies the startup gate, and returns
// the authority this unit is bound to.
//
// The unit's own identity is an output rather than a straight copy of the configured
// value, because an unsharded deployment may not have named one and still needs a name
// to report.
//
// It lives in cmd because it is wiring: config depends on nothing below it and the
// domain owns the map, so the one place that knows both is the one place that already
// knows everything (project-structure §4).
//
// The three outcomes are deliberately not two:
//
//   - no placement configured — an unsharded deployment. One authority owns every
//     organisation, every booking stays local, and the Phase 1 policy refuses nothing.
//     This is what every deployment before PR3a was, and it stays that way.
//   - a placement configured and valid — a shard-affine unit bound to one authority,
//     serving only the organisations that authority owns.
//   - a placement configured and broken — a startup error. A unit that could not load
//     its map must not fall back to serving everything: that is precisely how one
//     organisation's rows end up written to two authorities.
func resolvePlacement(src config.PlacementSource) (domain.Placement, domain.AuthorityID, error) {
	if !src.Configured() {
		// An unsharded deployment still deserves an authority name: a figure from a
		// one-database run should say which database produced it rather than carry an
		// empty label that reads as "unknown".
		authority := src.AuthorityID
		if authority == "" {
			authority = defaultAuthorityID
		}
		return domain.Unsharded(domain.AuthorityID(authority)), domain.AuthorityID(authority), nil
	}

	authority := domain.AuthorityID(src.AuthorityID)

	doc, err := placementDocument(src)
	if err != nil {
		return domain.Placement{}, "", err
	}

	placement, err := domain.ParsePlacement(doc)
	if err != nil {
		return domain.Placement{}, "", err
	}
	if err := placement.ValidateUnit(authority); err != nil {
		return domain.Placement{}, "", err
	}
	return placement, authority, nil
}

// placementDocument reads the placement JSON from wherever it was configured. config
// has already rejected supplying both forms, so at most one branch applies.
func placementDocument(src config.PlacementSource) ([]byte, error) {
	if src.DocumentPath != "" {
		doc, err := os.ReadFile(src.DocumentPath)
		if err != nil {
			return nil, fmt.Errorf("reading placement document: %w", err)
		}
		return doc, nil
	}
	return []byte(src.Document), nil
}

// defaultAuthorityID names the sole authority of an unsharded deployment when the
// operator has not named it. A sharded deployment never reaches this: config requires
// an explicit authority identity whenever a placement map is configured.
const defaultAuthorityID = "authority-1"
