// Package reconcile implements the correctness self-check of ag-sept-plan §6.5.
//
// It is the step that decides whether a run may be quoted at all. A capacity number is a
// claim about a service that was doing the work correctly; if the client's totals, the
// server's totals and the persisted state disagree, the number describes something else.
// §6.5 is explicit: "a run with unreconciled client totals, server totals, or persisted
// state is not quotable."
//
// It runs in a separate binary from the generator on purpose. The generator must be able to
// run on compute separate from the service for publishable claims (§6.3), and giving it
// database credentials would defeat that. So the generator emits client totals over HTTP
// only, and this package joins them to the database from wherever the database is reachable.
package reconcile

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// Querier is the database access this package needs: read-only, one row or many.
// Declared here where it is consumed, so a test can supply a fake and *pgxpool.Pool
// satisfies it without this package importing the pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Check is one reconciliation rule and its verdict.
type Check struct {
	Name string `json:"name"`
	// Invariant names the AG-M1 invariant this check exercises, so a failure points at
	// the property that broke rather than only at the arithmetic. It is empty for a check
	// that exercises a measurement rule rather than a domain invariant — the client/server
	// comparison is a statement about instrumentation, and citing an invariant it does not
	// test would misdirect whoever reads the failure.
	Invariant string `json:"invariant"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
}

// Result is the full verdict. Quotability is the whole point of the package.
type Result struct {
	Checks []Check `json:"checks"`

	// Quotability is what the run may back once persisted state has had its say. It can only
	// be at or below the level the report itself certifies: reconciliation is a veto, never
	// a promotion — no amount of agreement between three counts can supply a manifest field
	// that nobody recorded.
	Quotability loadgen.Quotability `json:"quotability"`
}

// RunClientChecks executes only the rules that need no database — currently §6.5's fourth,
// which is a statement about the client's own totals.
//
// It exists so the outcome-closure rule can be exercised without a schema, and so a run can
// be triaged from its report alone before anyone opens a connection. It is never a
// substitute for Run: a verdict from this function alone says nothing about persisted
// state, so it does not certify a run as quotable against §6.5.
func RunClientChecks(ctx context.Context, r loadgen.Report) (Result, error) {
	var res Result
	c, err := outcomeClosureCheck(ctx, nil, "", r.Summary)
	if err != nil {
		return res, err
	}
	res.Checks = append(res.Checks, c)
	res.Quotability = verdict(r, res.Checks)
	return res, nil
}

// Run executes every check of §6.5 against the summary, the metrics scrape and the
// database — the three independent counts the rule requires to agree.
//
// server carries the scrape. Passing nil is permitted and produces a failing check rather
// than a skipped one: §6.5 is a three-way agreement, and a verdict that quietly certified a
// run on two of the three would be the weaker gate wearing the stronger gate's name.
//
// Every check runs even after one fails: an operator debugging a bad run wants the whole
// picture, and stopping at the first failure hides whether the cause is narrow or broad.
func Run(ctx context.Context, q Querier, org domain.OrganisationID, r loadgen.Report, server ServerTotals) (Result, error) {
	var res Result

	for _, check := range []func(context.Context, Querier, domain.OrganisationID, loadgen.Summary) (Check, error){
		capacityCheck,
		idempotencyCheck,
		claimsCheck,
		outcomeClosureCheck,
	} {
		c, err := check(ctx, q, org, r.Summary)
		if err != nil {
			return res, err
		}
		res.Checks = append(res.Checks, c)
	}
	res.Checks = append(res.Checks, serverTotalsCheck(r.Summary, server))
	res.Quotability = verdict(r, res.Checks)
	return res, nil
}

// verdict combines the report's own certification with the reconciliation checks.
//
// The report's level is carried forward rather than recomputed, and reconciliation can only
// lower it. A run whose responses failed validation cannot become quotable by reconciling,
// and one whose manifest is missing its topology cannot acquire that topology from the
// database — the two gates are independent, and both must pass at the level being claimed.
func verdict(r loadgen.Report, checks []Check) loadgen.Quotability {
	q := loadgen.Certify(r.Manifest, r.Summary)
	if q.Level == loadgen.LevelNone {
		return q
	}
	for _, c := range checks {
		if !c.OK {
			return loadgen.Quotability{
				Level:          loadgen.LevelNone,
				BlockedFrom:    loadgen.LevelLocal,
				BlockedBecause: "reconciliation failed: " + c.Name + " — " + c.Detail,
			}
		}
	}
	return q
}

// capacityCheck is §6.5's first rule: consumed slot capacity against admitted reservation
// mutations. It is the arithmetic behind INV-1.
//
// Consumed capacity is derived from rows rather than a counter, since no counter exists to
// drift (INV-2) — so this compares two independent derivations of the same quantity: what
// the client was told, and what the rows say.
func capacityCheck(ctx context.Context, q Querier, org domain.OrganisationID, s loadgen.Summary) (Check, error) {
	c := Check{Name: "consumed capacity vs admitted reserves", Invariant: "INV-1"}

	var live, overCapacity int
	err := q.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE r.state IN ('held', 'confirmed')),
		  COUNT(*) FILTER (WHERE per_slot.consumed > s.capacity)
		FROM reservations r
		JOIN slots s
		  ON s.slot_organisation_id = r.slot_organisation_id AND s.slot_id = r.slot_id
		LEFT JOIN LATERAL (
		  SELECT COUNT(*) AS consumed
		  FROM reservations r2
		  WHERE r2.slot_organisation_id = r.slot_organisation_id
		    AND r2.slot_id = r.slot_id
		    AND r2.state IN ('held', 'confirmed')
		) per_slot ON TRUE
		WHERE r.slot_organisation_id = $1`, string(org)).Scan(&live, &overCapacity)
	if err != nil {
		return c, fmt.Errorf("capacity check: %w", err)
	}

	if overCapacity > 0 {
		c.Detail = fmt.Sprintf("%d reservations sit on slots whose consumed capacity "+
			"exceeds capacity: INV-1 is violated, and no number from this run is usable",
			overCapacity)
		return c, nil
	}

	admitted := s.FreshAdmittedFor(string(domain.OpReserve))
	if live > admitted {
		c.Detail = fmt.Sprintf("%d live reservations persisted but only %d fresh reserves "+
			"were admitted: the database holds units the client was never told about",
			live, admitted)
		return c, nil
	}

	c.OK = true
	c.Detail = fmt.Sprintf("%d live reservations, %d fresh admitted reserves, no slot over capacity",
		live, admitted)
	return c, nil
}

// idempotencyCheck is §6.5's second rule: distinct logical idempotency keys against
// committed mutations and replays. It exercises INV-5 — one key records exactly one
// terminal outcome.
func idempotencyCheck(ctx context.Context, q Querier, org domain.OrganisationID, s loadgen.Summary) (Check, error) {
	c := Check{Name: "idempotency keys vs committed mutations", Invariant: "INV-5"}

	var records, distinctKeys int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT (user_organisation_id, user_id, operation, key))
		FROM idempotency_records
		WHERE user_organisation_id = $1`, string(org)).Scan(&records, &distinctKeys)
	if err != nil {
		return c, fmt.Errorf("idempotency check: %w", err)
	}

	if records != distinctKeys {
		c.Detail = fmt.Sprintf("%d records for %d distinct scoped keys: a key recorded "+
			"more than one outcome, violating INV-5", records, distinctKeys)
		return c, nil
	}

	// A replay must not create a record, so fresh (non-replay) mutations are what the record
	// count is compared against. Folding replays in would demand a record per replay and
	// fail a correct service.
	fresh := s.FreshMutations()
	if records < fresh {
		c.Detail = fmt.Sprintf("%d idempotency records for %d fresh mutations the client "+
			"was told committed: outcomes the client saw were not durably recorded",
			records, fresh)
		return c, nil
	}

	// The other direction, which used to pass silently. A record the client never asked for
	// means either a contaminated fixture or a replay that wrote one — and the second is the
	// failure this check exists to catch, since a replay that records its own outcome has
	// performed the mutation §4.2 says it must not.
	//
	// Equality is only assertable because alloca-seed refuses to start against a fixture
	// holding records (§5.3). Before that assertion existed, a leftover record was the
	// commoner explanation and this comparison would have failed correct services.
	if records > fresh {
		c.Detail = fmt.Sprintf("%d idempotency records for %d fresh mutations: %d record(s) "+
			"the client never committed. Either the fixture was not re-seeded, or a replay "+
			"wrote a record instead of returning the recorded outcome (INV-5, §4.2)",
			records, fresh, records-fresh)
		return c, nil
	}

	c.OK = true
	c.Detail = fmt.Sprintf("%d records, all keys distinct, %d fresh mutations reported, "+
		"%d replays wrote none", records, fresh, s.ReplayedMutations)
	return c, nil
}

// claimsCheck is §6.5's third rule: live claims against admitted reservations per identity
// and interval. It exercises INV-4, the non-overlap invariant, and is the check that would
// catch the hot-identity workload silently producing a meaningless result.
func claimsCheck(ctx context.Context, q Querier, org domain.OrganisationID, s loadgen.Summary) (Check, error) {
	c := Check{Name: "live claims vs admitted reserves per identity", Invariant: "INV-4"}

	var claims, overlaps int
	err := q.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM user_time_claims WHERE user_organisation_id = $1),
		  (SELECT COUNT(*)
		     FROM user_time_claims a
		     JOIN user_time_claims b
		       ON a.user_organisation_id = b.user_organisation_id
		      AND a.user_id = b.user_id
		      AND a.reservation_id < b.reservation_id
		      AND a.claim_range && b.claim_range
		    WHERE a.user_organisation_id = $1)`, string(org)).Scan(&claims, &overlaps)
	if err != nil {
		return c, fmt.Errorf("claims check: %w", err)
	}

	if overlaps > 0 {
		c.Detail = fmt.Sprintf("%d overlapping claim pairs for one identity: INV-4 is "+
			"violated and the exclusion constraint did not hold", overlaps)
		return c, nil
	}

	admitted := s.FreshAdmittedFor(string(domain.OpReserve))
	if claims < admitted {
		c.Detail = fmt.Sprintf("%d live claims for %d fresh admitted reserves: the client "+
			"was told about holds the schedule does not record", claims, admitted)
		return c, nil
	}

	c.OK = true
	c.Detail = fmt.Sprintf("%d live claims, no overlapping pair, %d fresh admitted reserves",
		claims, admitted)
	return c, nil
}

// outcomeClosureCheck is §6.5's fourth rule: every completed request against the closed
// terminal-outcome set, with replay folded in as an orthogonal flag rather than
// double-counted.
//
// It needs no database: it is a statement about the client's own totals, and it is the
// check that catches a service answering with something the contract does not define.
func outcomeClosureCheck(_ context.Context, _ Querier, _ domain.OrganisationID, s loadgen.Summary) (Check, error) {
	c := Check{Name: "outcomes within the closed set, replay orthogonal", Invariant: "INV-7"}

	var counted int
	for _, t := range s.Totals {
		if !t.Outcome.IsKnown() {
			c.Detail = fmt.Sprintf("outcome %q is outside the closed terminal-outcome set",
				t.Outcome)
			return c, nil
		}
		if t.Outcome != domain.OutcomeBusinessRefusal && t.Reason != "" {
			c.Detail = fmt.Sprintf("outcome %q carries refusal reason %q", t.Outcome, t.Reason)
			return c, nil
		}
		counted += t.Count
	}

	// The totals are cells of (operation, outcome, reason, replay), so summing them must
	// return the completed count exactly. A mismatch means a replay was counted as its own
	// outcome somewhere, which is the modelling measurement-contract §4 calls a defect.
	if counted != s.Completed {
		c.Detail = fmt.Sprintf("totals sum to %d but %d requests completed: a request is "+
			"double-counted or missing, most likely replay treated as a peer outcome",
			counted, s.Completed)
		return c, nil
	}

	c.OK = true
	c.Detail = fmt.Sprintf("%d completed requests, all outcomes in the closed set", counted)
	return c, nil
}
