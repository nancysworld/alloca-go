package loadgen

import "time"

// SummariseForTest exposes the summariser to the external test package so a folding rule —
// notably that a read is not booking goodput — can be asserted on a fixed slice of
// responses rather than through a live server that would make the assertion depend on
// timing.
func SummariseForTest(responses []Response) Summary {
	return summarise("test", responses, time.Second, 0, Options{}, true)
}
