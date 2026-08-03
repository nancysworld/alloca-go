package loadgen

import (
	"fmt"
	"strings"
)

// Level is what a run's numbers may back. It replaces an earlier boolean `quotable`, which
// could not be answered honestly: ag-sept-plan §14 gates a *capacity claim* on topology
// provenance (§14 line 481) and a *publishable* claim on the generator running on separate
// compute (§6.3, §14 line 505), so "is this quotable?" has no answer until the claim is
// named. An unqualified true invited a later reader to publish a co-resident run.
//
// The levels are ordered, and a run sits at the highest one whose requirements its manifest
// and summary satisfy. They are deliberately named for the claim rather than for the PR that
// first reaches them: a report outlives the schedule, and "PR1" would oblige whoever reads
// docs/measurements/ in a year to reconstruct that PR's scope before knowing what the number
// is good for. Which PR reaches which level is recorded in the plan, where it belongs.
type Level string

const (
	// LevelNone is a run whose measurement is unsound — validation off or failed, the run
	// interrupted, warm-up rows unreconcilable, or reconciliation failed. It describes no
	// experiment and backs nothing.
	LevelNone Level = "none"

	// LevelLocal is a sound measurement with every generator-determinable field of §6.4
	// populated. The service's shape is unrecorded and the generator may be co-resident, so
	// it is an observation about this machine, not a capacity claim. PR1 and PR2 live here.
	LevelLocal Level = "local"

	// LevelCapacity adds the service-side and topology provenance of §6.4: PostgreSQL
	// version, pool sizes, server GOMAXPROCS, timeout budget, reservation TTL, replica
	// count, deployment topology and environment. It may back a capacity claim about that
	// topology, but not a published one — §6.3 is not yet satisfied. PR2 supplies the
	// service shape, PR3 the topology.
	LevelCapacity Level = "capacity"

	// LevelPublishable adds §6.3: the generator ran on compute separate from the service.
	//
	// This checks the *declaration* in the manifest, which is all a manifest can do. It does
	// not stand in for §12.2's generator-headroom control, which is evidence rather than
	// provenance and arrives with PR4 — a run reaching this level has satisfied the
	// provenance gate, not the whole publication gate.
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
// BlockedBecause names the missing thing rather than only reporting a failure, because the
// staged manifest of §14 makes "incomplete" the *expected* state for most of AG-Sept. An
// operator reading a PR1 report needs to see that the gap is scheduled, not broken.
type Quotability struct {
	Level Level `json:"level"`

	// BlockedFrom is the level this run did not reach; BlockedBecause says what is missing.
	// Both are empty only when the run reached the top of the ladder.
	BlockedFrom    Level  `json:"blocked_from,omitempty"`
	BlockedBecause string `json:"blocked_because,omitempty"`
}

// Certify decides what a run may back, from its manifest and its summary.
//
// Soundness is checked first and cannot be traded against provenance: a run whose responses
// went unvalidated is not rescued by a complete manifest, and one that was interrupted
// describes a smaller experiment than its manifest claims. Both are LevelNone regardless of
// how much provenance they carry.
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
		reached = want
	}
	return Quotability{Level: reached}
}
