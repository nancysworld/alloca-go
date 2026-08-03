package reconcile

import "github.com/nancysworld/alloca-go/internal/loadgen"

// RunServerCheckForTest exposes the client/server comparison on its own.
//
// The other §6.5 rules need a database and are exercised by the integration suite; this one
// needs neither, and testing it through Run would make a comparison of two in-memory totals
// depend on a running PostgreSQL — which is how a cheap test becomes one nobody runs.
func RunServerCheckForTest(s loadgen.Summary, server ServerTotals) Check {
	return serverTotalsCheck(s, server)
}
