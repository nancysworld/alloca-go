package httpapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// placementGuard refuses requests that reached the wrong service unit.
//
// A shard-affine unit serves exactly the organisations its authority owns
// (horizontal-database-authority §5.2). A request for any other organisation is a
// routing or deployment fault — the generator's map disagreeing with the service's, a
// load balancer pointing at the wrong unit, or a caller asserting an organisation that
// lives elsewhere — and it must be refused before it can write anything.
//
// Without this guard, "no supported request reached the wrong authority" would be a
// property of whatever routed the traffic rather than of Alloca, and the placement
// invariant would be untested by construction. The §12.5 misrouting control is what
// proves the guard is live.
type placementGuard struct {
	placement domain.Placement
	authority domain.AuthorityID
	logger    *slog.Logger
	// onMisroute counts refusals for operators. It is separate from the request
	// observation because a misroute is a deployment fault wearing a client error's
	// clothes: the caller gets a 400, and an operator needs a signal that is not
	// buried among genuinely malformed requests. Nil when metrics are not wired.
	onMisroute func(context.Context, domain.Operation)
}

// serves reports whether this unit owns org.
func (g placementGuard) serves(org domain.OrganisationID) bool {
	return g.placement.Serves(g.authority, org)
}

// refuse records and describes a misrouted request. The returned detail becomes the
// invalid_request message, which is the one outcome whose message may describe what was
// wrong with the request (api-surface §2.3).
//
// invalid_request rather than internal_failure, deliberately, for two reasons. The
// design note forbids recording a misroute as the user's durable domain outcome on the
// wrong authority, and invalid_request is precisely the outcome INV-7 exempts from the
// recording rule — it is rejected at the transport edge and never reaches the domain
// path. And routing identity is caller-asserted until authentication exists (design
// note §7.6), so classifying it as internal_failure would let any client drive this
// service's internal-failure rate, which is an SLO-relevant signal.
func (g placementGuard) refuse(ctx context.Context, op domain.Operation, org domain.OrganisationID) string {
	if g.onMisroute != nil {
		g.onMisroute(ctx, op)
	}
	if g.logger != nil {
		// The organisation is safe here and forbidden as a metric label: a log line is
		// not a time series, and an operator diagnosing a misroute needs to know which
		// organisation went astray.
		g.logger.Warn("request refused: this unit does not serve the requested organisation",
			slog.String("operation", string(op)),
			slog.String("authority", string(g.authority)),
			slog.String("routing_version", g.placement.Version()),
			slog.String("organisation", string(org)),
		)
	}
	return fmt.Sprintf("this service unit serves authority %q and does not own organisation %q "+
		"under routing version %q", g.authority, org, g.placement.Version())
}
