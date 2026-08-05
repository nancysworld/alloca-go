package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// AuthorityScope is one writable authority: how to read it, and which organisations it owns.
//
// Orgs comes from the placement map rather than from the database, so an organisation that
// somehow acquired rows on the wrong authority is *not* silently included in that authority's
// own counts — it goes missing from the aggregate instead, and the totals disagree. An
// authority-scoped query that discovered its own scope would hide exactly the failure
// placement exists to prevent.
type AuthorityScope struct {
	Authority domain.AuthorityID
	Querier   Querier
	Orgs      []domain.OrganisationID
	// Scrapes is this unit's own before/after pair. Each unit's pair is differenced
	// independently and only then summed: differencing the sums instead would let one unit
	// restarting mid-run vanish into another unit's counters, which is the one arithmetic
	// error this contract exists to prevent (ag-sept-plan-new.md §6.5).
	Scrapes Scrapes
}

// authorityCounts are the raw persisted facts one authority holds. They carry no comparison
// against the client's totals: those totals are a property of the *run*, not of any one
// authority, and comparing them per authority is the error the plan's §6.5 singles out.
type authorityCounts struct {
	LiveReservations   int
	OverCapacitySlots  int
	IdempotencyRecords int
	DistinctKeys       int
	LiveClaims         int
	OverlappingClaims  int
}

func (c *authorityCounts) add(o authorityCounts) {
	c.LiveReservations += o.LiveReservations
	c.OverCapacitySlots += o.OverCapacitySlots
	c.IdempotencyRecords += o.IdempotencyRecords
	c.DistinctKeys += o.DistinctKeys
	c.LiveClaims += o.LiveClaims
	c.OverlappingClaims += o.OverlappingClaims
}

// TopologyResult is one verdict for a multi-authority run, naming every authority it read.
type TopologyResult struct {
	// Authorities are the authorities this verdict covers, in the order they were read.
	// Recorded because a verdict that does not say what it examined cannot be trusted to
	// have examined everything — a run that lost an authority would otherwise produce a
	// clean result describing half a topology.
	Authorities []string `json:"authorities"`
	// PerAuthority holds each authority's local safety checks.
	PerAuthority map[string][]Check `json:"per_authority"`
	// Aggregate holds the checks taken once over the whole run: persisted and server totals
	// against the client's global totals.
	Aggregate []Check `json:"aggregate"`

	Quotability loadgen.Quotability `json:"quotability"`
}

// OK reports whether every check in the verdict passed.
func (r TopologyResult) OK() bool {
	for _, checks := range r.PerAuthority {
		for _, c := range checks {
			if !c.OK {
				return false
			}
		}
	}
	for _, c := range r.Aggregate {
		if !c.OK {
			return false
		}
	}
	return true
}

// RunTopology reconciles a run that spanned several writable authorities.
//
// The shape is the plan's §6.5 contract, and each step is there because the obvious
// alternative is wrong:
//
//  1. **Local safety invariants are checked independently on each authority.** Capacity,
//     schedule non-overlap and one-key-one-outcome are properties of the rows one authority
//     owns; they need no client totals and mean nothing averaged across authorities.
//  2. **Persisted and server totals are aggregated and compared once** with the run's global
//     client totals. Looping the single-authority entry point instead would compare one
//     organisation's rows against every organisation's totals — which fails a correct
//     two-authority run, and would be "fixed" by loosening the comparison until it stopped
//     failing.
//  3. **Each unit's scrape pair is differenced before the sum**, so a unit that restarted
//     mid-run is caught rather than absorbed.
//
// Reads are sequential across databases and are not one atomic snapshot. That is why the run
// must be quiesced first: with traffic stopped and settlement drained, sequential reads see a
// consistent picture, and this function does not pretend to a snapshot it cannot take.
func RunTopology(ctx context.Context, scopes []AuthorityScope, r loadgen.Report) (TopologyResult, error) {
	res := TopologyResult{PerAuthority: map[string][]Check{}}
	if len(scopes) == 0 {
		return res, fmt.Errorf("reconcile: no authorities to verify")
	}

	ordered := append([]AuthorityScope(nil), scopes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Authority < ordered[j].Authority })

	var total authorityCounts
	var serverTotals ServerTotals

	for _, scope := range ordered {
		if scope.Querier == nil {
			return res, fmt.Errorf("reconcile: authority %q has no querier", scope.Authority)
		}
		if len(scope.Orgs) == 0 {
			return res, fmt.Errorf("reconcile: authority %q owns no organisations; the placement "+
				"map and the verifier disagree about the topology", scope.Authority)
		}

		counts, err := countAuthority(ctx, scope.Querier, scope.Orgs)
		if err != nil {
			return res, fmt.Errorf("authority %q: %w", scope.Authority, err)
		}
		total.add(counts)

		name := string(scope.Authority)
		res.Authorities = append(res.Authorities, name)
		res.PerAuthority[name] = localSafetyChecks(scope, counts)

		// Differenced here, per unit, before anything is summed.
		measured, err := scope.Scrapes.measured()
		if err != nil {
			res.Aggregate = append(res.Aggregate, Check{
				Name:   "server totals vs client totals",
				Detail: fmt.Sprintf("authority %q: %v", scope.Authority, err),
			})
			return finish(res, r), nil
		}
		serverTotals = append(serverTotals, measured...)
	}

	res.Aggregate = aggregateChecks(total, serverTotals, r.Summary)
	return finish(res, r), nil
}

func finish(res TopologyResult, r loadgen.Report) TopologyResult {
	flat := make([]Check, 0, len(res.Aggregate))
	for _, name := range res.Authorities {
		flat = append(flat, res.PerAuthority[name]...)
	}
	flat = append(flat, res.Aggregate...)
	res.Quotability = verdict(r, flat)
	return res
}

// countAuthority reads one authority's persisted facts across the organisations it owns.
func countAuthority(ctx context.Context, q Querier, orgs []domain.OrganisationID) (authorityCounts, error) {
	var counts authorityCounts
	for _, org := range orgs {
		var one authorityCounts
		err := q.QueryRow(ctx, `
			WITH live AS (
			  SELECT slot_organisation_id, slot_id, COUNT(*) AS consumed
			  FROM reservations
			  WHERE slot_organisation_id = $1 AND state IN ('held', 'confirmed')
			  GROUP BY slot_organisation_id, slot_id
			)
			SELECT
			  COALESCE((SELECT SUM(live.consumed) FROM live
			              JOIN slots s ON s.slot_organisation_id = live.slot_organisation_id
			                          AND s.slot_id = live.slot_id), 0),
			  COALESCE((SELECT COUNT(*) FROM live
			              JOIN slots s ON s.slot_organisation_id = live.slot_organisation_id
			                          AND s.slot_id = live.slot_id
			             WHERE live.consumed > s.capacity), 0),
			  (SELECT COUNT(*) FROM idempotency_records WHERE user_organisation_id = $1),
			  (SELECT COUNT(DISTINCT (user_organisation_id, user_id, operation, key))
			     FROM idempotency_records WHERE user_organisation_id = $1),
			  (SELECT COUNT(*) FROM user_time_claims WHERE user_organisation_id = $1),
			  (SELECT COUNT(*)
			     FROM user_time_claims a
			     JOIN user_time_claims b
			       ON a.user_organisation_id = b.user_organisation_id
			      AND a.user_id = b.user_id
			      AND a.reservation_id < b.reservation_id
			      AND a.claim_range && b.claim_range
			    WHERE a.user_organisation_id = $1)`, string(org)).
			Scan(&one.LiveReservations, &one.OverCapacitySlots,
				&one.IdempotencyRecords, &one.DistinctKeys,
				&one.LiveClaims, &one.OverlappingClaims)
		if err != nil {
			return counts, fmt.Errorf("counting organisation %q: %w", org, err)
		}
		counts.add(one)
	}
	return counts, nil
}

// localSafetyChecks are the invariants that hold or fail on one authority's own rows.
//
// None of them consults the client's totals. That is the point: a capacity violation is a
// violation whatever the client believed, and an authority whose safety depended on the
// run's global counts would not be an independent authority.
func localSafetyChecks(scope AuthorityScope, c authorityCounts) []Check {
	orgs := make([]string, 0, len(scope.Orgs))
	for _, org := range scope.Orgs {
		orgs = append(orgs, string(org))
	}
	where := fmt.Sprintf("authority %s (%s)", scope.Authority, strings.Join(orgs, ", "))

	capacity := Check{Name: "capacity within every slot's limit", Invariant: "INV-1"}
	if c.OverCapacitySlots > 0 {
		capacity.Detail = fmt.Sprintf("%s: %d slot(s) hold more live reservations than capacity "+
			"allows; INV-1 is violated and no number from this run is usable",
			where, c.OverCapacitySlots)
	} else {
		capacity.OK = true
		capacity.Detail = fmt.Sprintf("%s: %d live reservations, no slot over capacity",
			where, c.LiveReservations)
	}

	claims := Check{Name: "no identity holds overlapping claims", Invariant: "INV-4"}
	if c.OverlappingClaims > 0 {
		claims.Detail = fmt.Sprintf("%s: %d overlapping claim pair(s) for one identity; INV-4 is "+
			"violated and the exclusion constraint did not hold", where, c.OverlappingClaims)
	} else {
		claims.OK = true
		claims.Detail = fmt.Sprintf("%s: %d live claims, no overlapping pair", where, c.LiveClaims)
	}

	keys := Check{Name: "one scoped key, one recorded outcome", Invariant: "INV-5"}
	if c.IdempotencyRecords != c.DistinctKeys {
		keys.Detail = fmt.Sprintf("%s: %d records for %d distinct scoped keys; a key recorded "+
			"more than one outcome", where, c.IdempotencyRecords, c.DistinctKeys)
	} else {
		keys.OK = true
		keys.Detail = fmt.Sprintf("%s: %d records, all keys distinct", where, c.IdempotencyRecords)
	}

	return []Check{capacity, claims, keys}
}

// aggregateChecks compare the whole topology's persisted state, and the whole topology's
// server counters, against the run's global client totals — once each.
func aggregateChecks(total authorityCounts, server ServerTotals, s loadgen.Summary) []Check {
	admitted := s.FreshAdmittedFor(string(domain.OpReserve))
	fresh := s.FreshMutations()

	reservations := Check{Name: "persisted reservations vs admitted reserves (all authorities)", Invariant: "INV-1"}
	if total.LiveReservations > admitted {
		reservations.Detail = fmt.Sprintf("%d live reservations across the topology but only %d "+
			"fresh reserves admitted: the databases hold units the client was never told about",
			total.LiveReservations, admitted)
	} else {
		reservations.OK = true
		reservations.Detail = fmt.Sprintf("%d live reservations across the topology, %d fresh "+
			"admitted reserves", total.LiveReservations, admitted)
	}

	claims := Check{Name: "persisted claims vs admitted reserves (all authorities)", Invariant: "INV-4"}
	if total.LiveClaims < admitted {
		claims.Detail = fmt.Sprintf("%d live claims across the topology for %d fresh admitted "+
			"reserves: the client was told about holds no schedule records",
			total.LiveClaims, admitted)
	} else {
		claims.OK = true
		claims.Detail = fmt.Sprintf("%d live claims across the topology, %d fresh admitted reserves",
			total.LiveClaims, admitted)
	}

	records := Check{Name: "idempotency records vs fresh mutations (all authorities)", Invariant: "INV-5"}
	switch {
	case total.IdempotencyRecords < fresh:
		records.Detail = fmt.Sprintf("%d records across the topology for %d fresh mutations the "+
			"client was told committed: outcomes the client saw were not durably recorded",
			total.IdempotencyRecords, fresh)
	case total.IdempotencyRecords > fresh:
		records.Detail = fmt.Sprintf("%d records across the topology for %d fresh mutations: %d "+
			"record(s) the client never committed. Either a fixture was not re-seeded, or a "+
			"replay wrote a record instead of returning the recorded outcome",
			total.IdempotencyRecords, fresh, total.IdempotencyRecords-fresh)
	default:
		records.OK = true
		records.Detail = fmt.Sprintf("%d records across the topology, %d fresh mutations",
			total.IdempotencyRecords, fresh)
	}

	return []Check{reservations, claims, records, serverTotalsCheckFromMeasured(s, server)}
}
