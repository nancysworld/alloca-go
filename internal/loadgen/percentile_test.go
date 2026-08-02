package loadgen_test

import (
	"testing"

	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// sample returns 1..n as float64s, so the value at each rank *is* that rank. An assertion
// can then name the observation it expects rather than a latency, and an off-by-one shows
// up as an arithmetic error rather than as a plausible number.
func sample(n int) []float64 {
	v := make([]float64, n)
	for i := range v {
		v[i] = float64(i + 1)
	}
	return v
}

// TestPercentilesAreNearestRank pins the rank each quantile selects at the sample sizes a
// sweep actually produces.
//
// The sizes are not arbitrary: every one of them makes p·n/100 integral for at least one
// quantile, which is the only case where nearest-rank and the truncating form disagree. On
// n=100 the truncating form returned the 96th observation for p95 and the 100th for p99; on
// n=2 it returned the larger of the two for p50. Those are the cells that would silently
// overstate every latency figure in AG-Sept.
func TestPercentilesAreNearestRank(t *testing.T) {
	for _, tc := range []struct {
		n             int
		p50, p95, p99 float64
		name          string
		maxValue      float64
	}{
		{n: 1, p50: 1, p95: 1, p99: 1, maxValue: 1, name: "single sample is every quantile"},
		{n: 2, p50: 1, p95: 2, p99: 2, maxValue: 2, name: "p50 of two is the lower, not the upper"},
		{n: 20, p50: 10, p95: 19, p99: 20, maxValue: 20, name: "p50 and p95 both integral"},
		{n: 60, p50: 30, p95: 57, p99: 60, maxValue: 60, name: "p95 of 60 is the 57th"},
		{n: 100, p50: 50, p95: 95, p99: 99, maxValue: 100, name: "every quantile integral"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := loadgen.PercentilesForTest(sample(tc.n))
			if got.P50 != tc.p50 {
				t.Errorf("n=%d p50 = %v, want %v", tc.n, got.P50, tc.p50)
			}
			if got.P95 != tc.p95 {
				t.Errorf("n=%d p95 = %v, want %v", tc.n, got.P95, tc.p95)
			}
			if got.P99 != tc.p99 {
				t.Errorf("n=%d p99 = %v, want %v", tc.n, got.P99, tc.p99)
			}
			if got.Max != tc.maxValue {
				t.Errorf("n=%d max = %v, want %v", tc.n, got.Max, tc.maxValue)
			}
		})
	}
}

// TestPercentilesNeverExceedTheSample is the property behind the table: a nearest-rank
// quantile is always an observation, and never one past the end. The truncating form
// violated this only by selecting a *higher* observation than the rank calls for, which is
// why an ordering assertion catches it where a range assertion does not.
func TestPercentilesNeverExceedTheSample(t *testing.T) {
	for n := 1; n <= 200; n++ {
		got := loadgen.PercentilesForTest(sample(n))
		if got.P50 > got.P95 || got.P95 > got.P99 || got.P99 > got.Max {
			t.Fatalf("n=%d quantiles out of order: %+v", n, got)
		}
		if got.P99 > float64(n) {
			t.Fatalf("n=%d p99 = %v exceeds the sample", n, got.P99)
		}
	}
}

// TestPercentilesOfEmptySampleAreZero covers the run that recorded nothing — an interrupted
// run, or one whose responses were all discarded by the warm-up window.
func TestPercentilesOfEmptySampleAreZero(t *testing.T) {
	if got := loadgen.PercentilesForTest(nil); got != (loadgen.Percentiles{}) {
		t.Errorf("empty sample = %+v, want zero value", got)
	}
}
