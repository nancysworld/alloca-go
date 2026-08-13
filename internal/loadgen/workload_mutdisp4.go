package loadgen

import (
	"context"
	"fmt"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// mutDisp4Participants is the exact organisation set `WL-MUT-DISP-4` is defined over
// (workload-catalog.md, "WL-MUT-DISP-4 — Population").
//
// **The named set, not a count of four.** Name() goes into the manifest as the workload's
// identity, and a comparison across topologies is only meaningful between runs that drove the
// same participants. Checking the count alone would let a run over `org-w` through `org-z`, or
// over A/B/C/D with one renamed, report itself as the catalog workload — and nothing downstream
// re-derives the population from the artifact, so two such runs would be compared as though they
// were the same experiment.
//
// It is sorted, and NewOrgPopulations relies on that: this is the order demand is assigned in.
var mutDisp4Participants = []domain.OrganisationID{"org-a", "org-b", "org-c", "org-d"}

// OrgPopulation is one organisation's independent seeded population.
//
// It is deliberately *not* OrgGroup. That type groups organisations by the authority that
// owns them, which is the right shape for a workload whose supported pairs depend on
// colocation — and the wrong shape here, because grouping by authority makes the dataset a
// function of the topology under test.
type OrgPopulation struct {
	Org domain.OrganisationID
	// Slots are this organisation's own seeded slots. Every one of them carries Org; the
	// constructor refuses any that do not.
	Slots []Slot
}

// NewOrgPopulations builds `WL-MUT-DISP-4`'s populations from the slots seeded per
// organisation.
//
// **The order is the catalog's, never the placement's.** This is the whole reason the
// constructor exists. Demand is assigned round-robin over this slice, so the slice order decides
// which organisation each seq addresses — and if it were derived by walking authorities and
// flattening their organisations, that order would change with the topology whenever an authority
// does not own an alphabetically contiguous run of them. `G1` would then compare against a `G2`
// that had shuffled the demand-to-organisation mapping, and the difference would appear as scale
// efficiency. Walking mutDisp4Participants makes the mapping a property of the workload, which is
// what `workload-catalog.md` "WL-MUT-DISP-4 — Demand shape" requires it to be.
//
// It also refuses a population that is not this workload's. Four organisations of any name are
// not `WL-MUT-DISP-4`, and the identity matters beyond pedantry: `Name()` is what the manifest
// records, nothing downstream re-derives the participants from the artifact, and two runs over
// different populations would therefore be compared as though they were the same experiment.
//
// It refuses a population whose slots belong to another organisation. Same-organisation
// pairing is the workload's load-bearing property, and a mis-seeded map would produce
// exactly the cross-organisation bookings its Exclusions rule out — silently, because such a
// pair is *supported* behaviour when the two organisations happen to be colocated, so
// nothing downstream would report it.
func NewOrgPopulations(slotsByOrg map[domain.OrganisationID][]Slot) ([]OrgPopulation, error) {
	if len(slotsByOrg) != len(mutDisp4Participants) {
		return nil, fmt.Errorf("loadgen: wl-mut-disp-4 is defined over %v, but %d organisations were "+
			"seeded; a run reporting this workload with a different population is naming a catalog "+
			"shape it did not drive", mutDisp4Participants, len(slotsByOrg))
	}

	// Built by walking the named participants rather than the supplied map, so the result is in
	// the catalog's order by construction and a wrong name cannot be sorted into a plausible
	// position. The count check above is what makes a *missing* participant reportable as such
	// rather than as a surplus one.
	populations := make([]OrgPopulation, 0, len(mutDisp4Participants))
	for _, org := range mutDisp4Participants {
		slots, seeded := slotsByOrg[org]
		if !seeded {
			return nil, fmt.Errorf("loadgen: wl-mut-disp-4 is defined over %v and nothing was seeded "+
				"for %q; four organisations of any name are not this workload, and two runs over "+
				"different participants cannot be compared under one identity",
				mutDisp4Participants, org)
		}
		if len(slots) == 0 {
			return nil, fmt.Errorf("loadgen: organisation %q was seeded no slots; the run would divide by zero "+
				"at the first request that addressed it", org)
		}
		for _, slot := range slots {
			if slot.OrganisationID != org {
				return nil, fmt.Errorf("loadgen: organisation %q was seeded slot %q, which belongs to %q; "+
					"wl-mut-disp-4 pairs a user only with its own organisation's slots",
					org, slot.SlotID, slot.OrganisationID)
			}
		}
		populations = append(populations, OrgPopulation{Org: org, Slots: slots})
	}

	return populations, nil
}

// MutDisp4 is `WL-MUT-DISP-4` from workload-catalog.md: four equivalent organisations, equal
// demand share, mutation path only, and a user paired **only** with its own organisation's
// slots.
//
// It is the workload Iteration C compares `G1`, `G2` and `G4` with, and the reason it is a
// separate type from MultiOrgDispersed is that one property: MultiOrgDispersed deliberately
// mixes colocated cross-organisation bookings into the run, which is useful correctness
// coverage and fatal to a scale comparison. Its pair mix is a function of how many
// organisations share an authority, so moving from one shard group to four changes the
// *workload* as well as the topology, and the resulting efficiency figure would carry both
// changes with no way to separate them. The catalog says so directly: MultiOrgDispersed "is
// therefore **not** the implementation of `WL-MUT-DISP-4`".
//
// Nothing here consults the placement map. The pair is chosen from the organisation's own
// population, and routing it to the authority that owns the user is the Router's job — so
// for any seq this workload produces byte-identical requests at every topology, which is
// what makes `E2` and `E4` a measurement of the architecture rather than of the generator.
type MutDisp4 struct {
	// Orgs are the four populations, ordered by NewOrgPopulations.
	Orgs []OrgPopulation
	// Confirm drives reserve→confirm rather than reserve alone. A confirmed claim has
	// expires_at IS NULL and is never expiry-reaped, so a confirming run leaves permanent
	// rows and needs the clean-start discipline the hot-identity control documents.
	Confirm bool
}

func (MutDisp4) Name() string         { return "wl-mut-disp-4" }
func (MutDisp4) IntendsReplays() bool { return false }

func (m MutDisp4) Do(ctx context.Context, c *Client, seq int) []Response {
	// Round-robin over the organisations gives each an equal share. With an -n that is not a
	// multiple of four the final cycle is short, so one organisation may carry a single extra
	// request — the same organisation, by the same amount, at every topology, so it cannot
	// tilt a comparison between them.
	population := m.Orgs[seq%len(m.Orgs)]

	// Each organisation walks its own dataset on its own counter, rather than indexing by seq
	// directly. Indexing by seq would stride through the slots four at a time, and how much of
	// the population that reaches depends on gcd(4, len(Slots)): coprime sizes still visit every
	// slot in a different order, while any multiple of four collapses the organisation onto
	// len(Slots)/4 of its seeded rows. So the damage is invisible at some fixture sizes and
	// severe at others — a contention profile decided by an arithmetic coincidence rather than
	// by the dataset the run reports.
	requestsIntoOrg := seq / len(m.Orgs)
	slot := population.Slots[requestsIntoOrg%len(population.Slots)]

	// The user is minted in the slot's own organisation, which is what makes every pair
	// same-organisation by construction rather than by a check. seq keeps the identity
	// distinct per request, so the dispersed shape does not collapse into the hot-identity
	// control's serialization on a single user row.
	user := User{OrganisationID: population.Org, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}

	reserved := c.Reserve(ctx, user, slot, c.key(m.Name(), seq, "reserve"))
	out := []Response{reserved}
	if !m.Confirm || reserved.ReservationID == "" {
		return out
	}
	return append(out, c.Confirm(ctx, user, reserved.ReservationID, c.key(m.Name(), seq, "confirm")))
}

var _ Workload = MutDisp4{}
