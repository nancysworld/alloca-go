package loadgen

import (
	"encoding/json"
	"fmt"
	"io"
)

// ConditioningDeclaration is what a measured run records about the state it began from.
//
// It exists because a conditioned experiment's final persisted state contains mutations from
// more than one population, and reconciliation has to be able to take them apart. The
// contract's requirement is a starting-state baseline that is retained or reconstructible at
// the exact measured-start boundary, not merely a note that conditioning happened
// (measurement-contract.md §12.1).
//
// It is derived from the conditioning run's own retained artifact rather than declared again
// by the operator. A second declaration is a second thing to keep in step, and the failure it
// produces — a measured run believing conditioning stopped somewhere it did not — is invisible
// in every field that would otherwise show it.
type ConditioningDeclaration struct {
	// RunID identifies the conditioning run whose artifact this was read from, so the two
	// halves of one experiment can be rejoined after the fact.
	RunID string `json:"run_id"`
	// SlotsPerOrganisation is the conditioning population's share of each organisation's
	// seeded slots. The measured run derives its own range from this rather than being told
	// it separately, so the boundary is declared exactly once.
	SlotsPerOrganisation int `json:"slots_per_organisation"`
	// TargetMutationsPerOrganisation is the predeclared state target. It is per organisation
	// rather than per run so that a faster topology and a slower one begin measurement at the
	// same logical state instead of the same elapsed time (ag-sept-validation-plan.md §4.6.2).
	TargetMutationsPerOrganisation int `json:"target_mutations_per_organisation"`
	// Organisations is how many the target was applied to, so the run total can be checked
	// without re-deriving it from the placement.
	Organisations int `json:"organisations"`
	// Goodput is the fresh mutations conditioning actually committed. It is the state the
	// measured interval began against, and it is retained rather than assumed equal to the
	// target: a conditioning phase that fell short conditioned a different database.
	Goodput int `json:"achieved_mutations"`
	// Completed is every conditioning request, so its traffic stays visible to reconciliation
	// rather than disappearing because it happened before the performance clock started.
	Completed int `json:"conditioning_requests"`
	// PoolRecycled records the deterministic state-preserving transition between the two
	// phases. Without it the measured connections may still be carrying execution plans
	// prepared against empty mutation tables, which is the regime conditioning exists to
	// remove (ag-sept-validation-plan.md §4.6.2).
	PoolRecycled bool `json:"pool_recycled"`
}

// ReadConditioning loads a conditioning run's retained report and derives the declaration a
// measured run records.
//
// It refuses anything that is not the conditioning half of the same experiment. Each refusal
// is a way for the measured run to begin against a state it would then misdescribe:
//
//   - a measured report, which conditions nothing;
//   - an unsound conditioning run, whose own totals do not describe what it did;
//   - a different workload, which leaves different tables populated;
//   - a conditioning run that fell short of its declared target, which is a different
//     starting state than the one the experiment declared.
func ReadConditioning(r io.Reader, wantWorkload string, poolRecycled bool) (*ConditioningDeclaration, error) {
	var report Report
	if err := json.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("reading the conditioning report: %w", err)
	}

	if report.Manifest.Phase != PhaseConditioning {
		return nil, fmt.Errorf("that report is phase %q, not %q: a measured run cannot take "+
			"its starting state from another measured run",
			phaseName(report.Manifest.Phase), PhaseConditioning)
	}
	if report.Manifest.Workload != wantWorkload {
		return nil, fmt.Errorf("the conditioning run drove %q and this run drives %q; the "+
			"state one workload leaves behind is not the state the other begins from",
			report.Manifest.Workload, wantWorkload)
	}
	if !report.Summary.Sound {
		return nil, fmt.Errorf("the conditioning run is not sound (%s), so the state it "+
			"claims to have established is not established", report.Summary.NotSoundBecause)
	}

	if report.Manifest.Conditioning == nil {
		return nil, fmt.Errorf("that conditioning report declares no conditioning block, so " +
			"there is no state target or slot boundary to begin from")
	}

	declaration := *report.Manifest.Conditioning
	declaration.RunID = report.Manifest.RunID
	declaration.Goodput = report.Summary.Goodput
	declaration.Completed = report.Summary.Completed
	declaration.PoolRecycled = poolRecycled

	if err := declaration.Shortfall(); err != nil {
		return nil, err
	}
	return &declaration, nil
}

// Shortfall reports whether the conditioning phase reached its declared state target.
//
// The target is checked against what was *committed*, not against what was attempted. A
// conditioning phase whose reserves were refused left the tables in a state the experiment did
// not declare, and "conditioning ran" is not the property that matters — the state it
// established is.
//
// One implementation, two callers: the conditioning run fails on it immediately so the
// operator is not told at the far more expensive measured step, and the measured run checks it
// again because the artifact it reads may not be the one that was just produced.
func (c ConditioningDeclaration) Shortfall() error {
	want := c.TargetMutationsPerOrganisation * c.Organisations
	if c.Goodput >= want {
		return nil
	}
	return fmt.Errorf("conditioning committed %d fresh mutations against a declared target of "+
		"%d (%d per organisation × %d organisations): the measured interval would begin "+
		"against a different state than the one declared",
		c.Goodput, want, c.TargetMutationsPerOrganisation, c.Organisations)
}

// phaseName renders a phase for an error message, naming the measured phase rather than
// printing the empty string its zero value is.
func phaseName(p Phase) Phase {
	if p == PhaseMeasured {
		return "measured"
	}
	return p
}
