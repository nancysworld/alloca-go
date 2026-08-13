package loadgen

import "time"

// SummariseForTest exposes the summariser to the external test package so a folding rule —
// notably that a read is not booking goodput — can be asserted on a fixed slice of
// responses rather than through a live server that would make the assertion depend on
// timing.
func SummariseForTest(responses []Response) Summary {
	opts := Options{Iterations: len(responses)}
	return summarise("test", false, responses, time.Second, 0, opts, true,
		runFacts{completedUnits: opts.Iterations})
}

// ClientWithRunIDForTest builds a client whose keys are scoped to id, so the key scoping can
// be asserted directly rather than inferred from two runs against a live service.
func ClientWithRunIDForTest(id string) *Client { return (&Client{}).WithRunID(id) }

// KeyForTest exposes the idempotency key a client would mint.
func KeyForTest(c *Client, workload string, seq int, step string) string {
	return c.key(workload, seq, step)
}

// PercentilesForTest exposes the quantile calculation, so the rank it selects can be
// asserted on a known sample instead of inferred from a run whose latencies vary.
func PercentilesForTest(v []float64) Percentiles {
	return percentiles(v)
}

// ReplayDefectForTest exposes the §4.2 replay rule, so the branch that must *not* fire — a
// first attempt that established no recorded outcome — can be driven directly. Reaching it
// through a server would need one that fails the first request and replays the second, which
// is a fixture describing no real service.
func ReplayDefectForTest(first, second Response) string {
	return replayDefect(first, second)
}
