package loadgen

import (
	"fmt"
	"strings"
)

// Level is what a run's numbers may back. The ladder and its rules are
// measurement-contract §13.2; this type enforces them.
//
// It replaces an earlier boolean `quotable`, which could not be answered honestly: a capacity
// result needs topology provenance and an externally presented claim additionally needs the
// generator on separate compute (§13.1) — so "is this quotable?" has no answer until the claim
// is named. An unqualified true invited a later reader to publish a co-resident run.
//
// **This is a soundness and provenance ladder, not a publication gate.** It says the run
// describes itself well enough to support a class of claim; whether the experiment supports the
// claim is measurement-contract §5's question and is answered separately.
//
// The levels are ordered, and a run sits at the highest one whose requirements its manifest
// and summary satisfy. They are deliberately named for the claim rather than for the PR that
// first reaches them: a report outlives the schedule, and "PR1" would oblige whoever reads
// docs/measurements/ in a year to reconstruct that PR's scope before knowing what the number
// is good for. Which milestone reaches which level is scheduling, recorded in the plan.
type Level string

const (
	// LevelNone is a run that backs nothing, for either of two independent reasons: the
	// measurement is **unsound** — validation off or failed, the run interrupted, warm-up rows
	// unreconcilable, reconciliation failed, or the units not describing one deployment — or it
	// is sound but does not reach LevelLocal, so it cannot say what it measured.
	//
	// LevelLocal is the floor of the ladder, not a rung above this one: Certify starts at
	// LevelNone, so failing the first rung's provenance leaves a run here. `blocked_because`
	// distinguishes the two cases, carrying the soundness reason or the missing fields
	// (measurement-contract §13.2).
	LevelNone Level = "none"

	// LevelLocal is a sound measurement that identifies itself: the service (revision, Go
	// version, and the shape /meta hands over — PostgreSQL version, GOMAXPROCS, timeout budget,
	// reservation TTL, pool size per replica), the generator (revision, location, resources), the
	// workload and run shape, and the target and timestamp.
	//
	// Everything /meta reports sits here rather than higher up, because a field that costs
	// nothing to record should not gate a higher tier than one that costs an operator's
	// attention. What is missing is the operator's account of the deployment, so the run is a
	// reproducible observation of this machine rather than a capacity result.
	LevelLocal Level = "local"

	// LevelCapacity adds the provenance no endpoint can report: aggregate pool size, replica
	// count, deployment topology, environment, and the routing version — plus, where the topology
	// makes them meaningful, placement for a multi-unit run and an image ID for a containerised
	// one. It may back a capacity result about that recorded topology.
	//
	// It may be reached with the generator co-resident: co-residency is a LevelPublishable bar,
	// not this one (measurement-contract §13.2).
	LevelCapacity Level = "capacity"

	// LevelPublishable adds measurement-contract §13.1: the generator ran on compute separate
	// from the service. It is the provenance an externally presented, project-level capacity
	// claim needs — not a statement that this repository is public.
	//
	// This checks the *declaration* in the manifest, which is all a manifest can do. **It does
	// not discharge the experiment's evidence gates.** The VAL-NEG-2 generator-headroom control
	// and the rest of measurement-contract §5 are evidence rather than provenance, so a run can
	// hold top-of-ladder provenance and still be inadmissible because its experiment was not
	// controlled. This ladder answers "does the run describe itself?", never "was the experiment
	// sound enough to quote?".
	LevelPublishable Level = "publishable"
)

// rank orders the levels. Unknown levels rank below none so a typo cannot promote a run.
func (l Level) rank() int {
	switch l {
	case LevelLocal:
		return 1
	case LevelCapacity:
		return 2
	case LevelPublishable:
		return 3
	case LevelNone:
		return 0
	default:
		return -1
	}
}

// AtLeast reports whether l meets the bar want sets.
func (l Level) AtLeast(want Level) bool { return l.rank() >= want.rank() && l.rank() >= 0 }

// ladder is the ascending order Certify walks. LevelNone is not in it: it is the floor.
var ladder = []Level{LevelLocal, LevelCapacity, LevelPublishable}

// ParseLevel converts an operator-supplied level name, rejecting anything outside the set so
// a misspelled -require cannot silently lower the bar it was meant to raise.
func ParseLevel(s string) (Level, error) {
	l := Level(strings.ToLower(strings.TrimSpace(s)))
	if l.rank() < 0 {
		return "", fmt.Errorf("unknown quotability level %q: want none, local, capacity or publishable", s)
	}
	return l, nil
}

// Quotability is the verdict recorded with every report: what this run may back, and what
// stands between it and the next level up.
//
// BlockedBecause names the missing thing rather than only reporting a failure, because
// provenance is staged across a milestone's PRs, which makes "incomplete" the expected state
// for much of it (measurement-contract §13.2). An operator reading an early report needs to
// see that the gap is scheduled, not broken; which PR closes which gap is the plan's.
type Quotability struct {
	Level Level `json:"level"`

	// BlockedFrom is the level this run did not reach; BlockedBecause says what is missing.
	// Both are empty only when the run reached the top of the ladder.
	BlockedFrom    Level  `json:"blocked_from,omitempty"`
	BlockedBecause string `json:"blocked_because,omitempty"`
}

// unexpectedReplays refuses a capacity claim built on a fixture carried over from an earlier
// run, and names the level it refuses at.
//
// Idempotency keys are scoped to the run (Client.key), so a workload that does not drive
// replays deliberately cannot produce one within a single run: every key it mints is new. A
// replay in such a run therefore means the *service* had a record for a key this run invented,
// which happens when a previous run of the same shape reached the same seq — the reused-fixture
// case — or when the run identity failed to vary. Either way the replayed requests committed
// nothing, and goodput is short by exactly that population.
//
// **It gates at capacity, not at local.** A local claim is a reproducible observation of a
// machine and a fully-replayed run is still honestly that; it is the *comparison* between
// capacity points that the contamination corrupts, because a fixture reset before some rungs
// and not others shifts one number and not the others. The disposition control is exempt by its
// own declaration rather than by its name (Workload.IntendsReplays).
//
// alloca-seed's clean-start assertion remains required fixture preparation. This is the
// structural half of the same guard: procedure catches the operator who forgets to re-seed,
// this catches the run whose evidence would otherwise certify anyway (ag-sept-pr4.md §3.5).
func unexpectedReplays(level Level, s Summary) string {
	if level != LevelCapacity || s.ReplaysIntended || s.ReplayedMutations == 0 {
		return ""
	}
	return fmt.Sprintf(
		"%d of the run's mutations were replays, but workload %q does not drive replays: "+
			"its idempotency keys are scoped to this run, so the service answered from records "+
			"an earlier run wrote. Those requests committed nothing, and the reported goodput of "+
			"%d is short by that population. Re-seed the fixture (test/scripts/itc-seed.sh, or "+
			"alloca-seed -reset) and re-run before quoting a capacity point",
		s.ReplayedMutations, s.Workload, s.Goodput)
}

// Certify decides what a run may back, from its manifest and its summary.
//
// Soundness is checked first and cannot be traded against provenance: a run whose responses
// went unvalidated is not rescued by a complete manifest, and one that was interrupted
// describes a smaller experiment than its manifest claims. Both are LevelNone regardless of
// how much provenance they carry.
//
// The converse does not hold — soundness does not buy a level. `reached` starts at LevelNone
// and the first rung is LevelLocal, so a sound run whose manifest fails that rung stays at
// LevelNone rather than being promoted for having run cleanly.
//
// Above that floor the run climbs the ladder until a level's provenance is incomplete, and
// stops there carrying the reason.
func Certify(m Manifest, s Summary) Quotability {
	if !s.Sound {
		return Quotability{
			Level:          LevelNone,
			BlockedFrom:    LevelLocal,
			BlockedBecause: s.NotSoundBecause,
		}
	}

	// A topology whose units do not describe one deployment is unsound, not merely
	// under-documented. Its client totals are two services averaged together, and no amount
	// of manifest completeness makes that a measurement of anything — so it drops to none
	// rather than stopping partway up the ladder.
	if m.TopologyDisagreement != "" {
		return Quotability{
			Level:          LevelNone,
			BlockedFrom:    LevelLocal,
			BlockedBecause: "the topology did not describe one deployment: " + m.TopologyDisagreement,
		}
	}

	reached := LevelNone
	for _, want := range ladder {
		if missing := m.Validate(want); len(missing) > 0 {
			return Quotability{
				Level:       reached,
				BlockedFrom: want,
				BlockedBecause: fmt.Sprintf("manifest is incomplete for a %s claim: %s",
					want, strings.Join(missing, "; ")),
			}
		}
		if because := unexpectedReplays(want, s); because != "" {
			return Quotability{Level: reached, BlockedFrom: want, BlockedBecause: because}
		}
		reached = want
	}
	return Quotability{Level: reached}
}
