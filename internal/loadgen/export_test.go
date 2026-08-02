package loadgen

import "time"

// SummariseForTest exposes the summariser to the external test package so a folding rule —
// notably that a read is not booking goodput — can be asserted on a fixed slice of
// responses rather than through a live server that would make the assertion depend on
// timing.
func SummariseForTest(responses []Response) Summary {
	opts := Options{Iterations: len(responses)}
	return summarise("test", responses, time.Second, 0, opts, true,
		runFacts{completedUnits: opts.Iterations})
}

// PercentilesForTest exposes the quantile calculation, so the rank it selects can be
// asserted on a known sample instead of inferred from a run whose latencies vary.
func PercentilesForTest(v []float64) Percentiles {
	return percentiles(v)
}
