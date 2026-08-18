package loadgen_test

import (
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// The property the whole guard rests on: two runs of the same workload, at the same sequence
// numbers, must not mint the same idempotency key.
//
// Without it a rerun against a fixture the previous run mutated is served entirely from that
// run's records. It commits nothing, creates no rows, reconciles cleanly and breaks no
// invariant — the failure has no symptom except a goodput that is wrong, which at a capacity
// point is indistinguishable from the topology being slower.
func TestKeysAreScopedToTheRunSoARerunCannotReplayTheLastOne(t *testing.T) {
	first := loadgen.ClientWithRunIDForTest("run-aaaa")
	second := loadgen.ClientWithRunIDForTest("run-bbbb")

	for seq := range 8 {
		a := loadgen.KeyForTest(first, "wl-mut-disp-4", seq, "reserve")
		b := loadgen.KeyForTest(second, "wl-mut-disp-4", seq, "reserve")
		if a == b {
			t.Fatalf("seq %d minted the same key in two runs: %q", seq, a)
		}
	}
}

// Within one run the key must still be a pure function of (workload, seq, step): the
// disposition control issues its second request under the *same* key deliberately, and a key
// that varied per call would turn that control into two fresh mutations and silently stop
// exercising the replay path it exists for.
func TestKeysAreStableWithinARun(t *testing.T) {
	c := loadgen.ClientWithRunIDForTest("run-aaaa")

	if a, b := loadgen.KeyForTest(c, "replay", 3, "reserve"), loadgen.KeyForTest(c, "replay", 3, "reserve"); a != b {
		t.Errorf("the same logical request minted two keys: %q and %q", a, b)
	}
	if a, b := loadgen.KeyForTest(c, "replay", 3, "reserve"), loadgen.KeyForTest(c, "replay", 4, "reserve"); a == b {
		t.Errorf("two sequence numbers minted the same key: %q", a)
	}
	if a, b := loadgen.KeyForTest(c, "replay", 3, "reserve"), loadgen.KeyForTest(c, "replay", 3, "confirm"); a == b {
		t.Errorf("two steps of one unit minted the same key: %q", a)
	}
}

func TestNewRunIDDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		id, err := loadgen.NewRunID()
		if err != nil {
			t.Fatalf("minting a run id: %v", err)
		}
		if seen[id] {
			t.Fatalf("minted the same run id twice: %q", id)
		}
		seen[id] = true
	}
}

// Only the disposition control may answer true. This is written as an exhaustive table rather
// than a spot check because the exemption is what the capacity gate keys on: a workload that
// answered true by mistake would be silently excused from it forever.
func TestOnlyTheDispositionControlIntendsReplays(t *testing.T) {
	for _, tc := range []struct {
		workload loadgen.Workload
		intends  bool
	}{
		{loadgen.Dispersed{}, false},
		{loadgen.HotSlot{}, false},
		{loadgen.HotIdentity{}, false},
		{loadgen.Replay{}, true},
		{loadgen.MultiOrgDispersed{}, false},
		{loadgen.HotOrganisation{}, false},
		{loadgen.CrossAuthorityControl{}, false},
		{loadgen.MutDisp4{}, false},
	} {
		if got := tc.workload.IntendsReplays(); got != tc.intends {
			t.Errorf("%s: IntendsReplays() = %v, want %v", tc.workload.Name(), got, tc.intends)
		}
	}
}

// The certification half of the guard: a capacity claim may not rest on replayed mutations,
// but a local observation still may, and the disposition control is exempt at every level.
//
// The three cases are one table because what separates them is exactly the two inputs the
// rule reads, and asserting them apart would let a change satisfy each in isolation while
// breaking the relationship between them.
func TestCapacityRefusesReplaysTheWorkloadDidNotIntend(t *testing.T) {
	for _, tc := range []struct {
		name      string
		workload  string
		intended  bool
		replays   int
		wantLevel loadgen.Level
	}{
		{
			name:      "a capacity workload serving replays is refused",
			workload:  "wl-mut-disp-4",
			intended:  false,
			replays:   400,
			wantLevel: loadgen.LevelLocal,
		},
		{
			name:      "the same run with a clean fixture is not",
			workload:  "wl-mut-disp-4",
			intended:  false,
			replays:   0,
			wantLevel: loadgen.LevelPublishable,
		},
		{
			// Exempt by its own declaration, not by its name: replays are what it measures.
			name:      "the disposition control is exempt",
			workload:  "replay",
			intended:  true,
			replays:   400,
			wantLevel: loadgen.LevelPublishable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := publishableManifest()
			m.Workload = tc.workload
			s := loadgen.Summary{
				Sound:             true,
				Workload:          tc.workload,
				ReplaysIntended:   tc.intended,
				ReplayedMutations: tc.replays,
				Goodput:           400 - tc.replays,
			}

			got := loadgen.Certify(m, s)
			if got.Level != tc.wantLevel {
				t.Fatalf("level = %q, want %q (blocked_because: %s)",
					got.Level, tc.wantLevel, got.BlockedBecause)
			}
			if tc.wantLevel == loadgen.LevelLocal {
				if got.BlockedFrom != loadgen.LevelCapacity {
					t.Errorf("blocked_from = %q, want %q", got.BlockedFrom, loadgen.LevelCapacity)
				}
				// The message has to send an operator to the fixture, not to the manifest.
				for _, want := range []string{"replays", "re-seed"} {
					if !strings.Contains(strings.ToLower(got.BlockedBecause), want) {
						t.Errorf("blocked_because does not mention %q: %s", want, got.BlockedBecause)
					}
				}
			}
		})
	}
}
