package loadgen_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// conditioningReport builds a retained conditioning artifact for the reader to check.
func conditioningReport(t *testing.T, mutate func(*loadgen.Report)) *bytes.Buffer {
	t.Helper()
	report := loadgen.Report{
		Manifest: loadgen.Manifest{
			RunID:    "run-cond-1",
			Workload: "wl-mut-disp-4",
			Phase:    loadgen.PhaseConditioning,
			Conditioning: &loadgen.ConditioningDeclaration{
				SlotsPerOrganisation:           50,
				TargetMutationsPerOrganisation: 100,
				Organisations:                  4,
			},
		},
		Summary: loadgen.Summary{Sound: true, Goodput: 400, Completed: 400},
	}
	if mutate != nil {
		mutate(&report)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(report); err != nil {
		t.Fatalf("encoding the conditioning report: %v", err)
	}
	return &buf
}

// A measured run takes its starting state from the conditioning artifact, so the two halves
// of one experiment can be rejoined and the population boundary reconstructed.
func TestReadConditioningCarriesTheMeasuredStartBaseline(t *testing.T) {
	declaration, err := loadgen.ReadConditioning(conditioningReport(t, nil), "wl-mut-disp-4", true)
	if err != nil {
		t.Fatalf("reading a sound conditioning report: %v", err)
	}

	switch {
	case declaration.RunID != "run-cond-1":
		t.Errorf("declaration names run %q; without the conditioning run's identity its "+
			"persisted records cannot be traced back to the artifact", declaration.RunID)
	case declaration.SlotsPerOrganisation != 50:
		t.Errorf("declaration carries %d conditioning slots, want the 50 the conditioning "+
			"phase declared: the measured range is derived from this", declaration.SlotsPerOrganisation)
	case declaration.Goodput != 400 || declaration.Completed != 400:
		t.Errorf("declaration carries %d mutations over %d requests, want the conditioning "+
			"phase's own totals as the baseline measured deltas are taken against",
			declaration.Goodput, declaration.Completed)
	case !declaration.PoolRecycled:
		t.Error("the declaration does not record the pool recycle, which is what removes " +
			"plans prepared against empty mutation tables from the measured connections")
	}
}

// Each refusal is a way for the measured interval to open against a state its artifact would
// then misdescribe.
func TestReadConditioningRefusesAStateItCannotVouchFor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*loadgen.Report)
		wants  string
	}{
		{
			name:   "a measured report, which conditions nothing",
			mutate: func(r *loadgen.Report) { r.Manifest.Phase = loadgen.PhaseMeasured },
			wants:  "measured",
		},
		{
			name: "an unsound conditioning run",
			mutate: func(r *loadgen.Report) {
				r.Summary.Sound = false
				r.Summary.NotSoundBecause = "run was interrupted"
			},
			wants: "not sound",
		},
		{
			name:   "a different workload, which populates different tables",
			mutate: func(r *loadgen.Report) { r.Manifest.Workload = "multi-org-dispersed" },
			wants:  "multi-org-dispersed",
		},
		{
			name:   "no conditioning block, so no declared boundary",
			mutate: func(r *loadgen.Report) { r.Manifest.Conditioning = nil },
			wants:  "no conditioning block",
		},
		{
			// The one that matters most: conditioning that ran but did not establish the
			// state it declared. Every other field looks healthy.
			name:   "a phase that fell short of its declared target",
			mutate: func(r *loadgen.Report) { r.Summary.Goodput = 399 },
			wants:  "different state than the one declared",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadgen.ReadConditioning(conditioningReport(t, tc.mutate), "wl-mut-disp-4", true)
			if err == nil {
				t.Fatal("the conditioning state was accepted; the measured interval would " +
					"have opened against a state the artifact cannot describe")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("refusal says %q, which does not name the reason (%q)", err, tc.wants)
			}
		})
	}
}

// The target is a floor, not an equality: conditioning that overshot still established at
// least the declared state, and refusing it would fail runs for being thorough.
func TestReadConditioningAcceptsOvershoot(t *testing.T) {
	report := conditioningReport(t, func(r *loadgen.Report) { r.Summary.Goodput = 500 })
	if _, err := loadgen.ReadConditioning(report, "wl-mut-disp-4", true); err != nil {
		t.Fatalf("conditioning that exceeded its target was refused: %v", err)
	}
}
