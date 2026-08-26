package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

// metaResponse is the /meta payload. It inlines buildinfo.Info (runtime and build
// provenance) and adds the resolved, validated timing configuration. Every capacity
// result the project publishes must be traceable both to the environment it ran in and to the
// timings it ran under (measurement-contract §11 and §8.1); this endpoint is the
// machine-readable source of both.
//
// The bar for inclusion is **whether the value can change a measurement**, not whether
// it is configurable. The deadline chain can, and so can the hold TTL: it decides how
// long each admitted reserve keeps a unit of capacity, which sets how much contention a
// given arrival rate produces, so two runs with different TTLs are not comparable.
// ReadinessTimeout is included because it is the other bound validated against the
// chain, and an operator diagnosing probe flapping should not have to infer it. This is
// deliberately not a general configuration dump — ListenAddr and ShutdownGrace cannot
// change a result, and are left out.
type metaResponse struct {
	buildinfo.Info
	RequestBudget config.RequestBudget `json:"request_budget"`
	// Duration strings, matching the convention RequestBudget.MarshalJSON establishes.
	ReservationTTL   string `json:"reservation_ttl"`
	ReadinessTimeout string `json:"readiness_timeout"`

	// Database is the authority this process is bound to. It is here for the same reason
	// the revision is: the load harness records the shape of what it measured, and a value
	// the service already knows is one an operator should never be asked to retype — a typo
	// in a transcribed PostgreSQL version is indistinguishable from a measurement.
	Database DatabaseMeta `json:"database"`

	// Telemetry names which recorder is wired, because two runs under different observation
	// settings are not comparable and nothing in the totals would say so. It is the field
	// that lets the VAL-NEG-3 telemetry comparison identify its own arms.
	Telemetry string `json:"telemetry_mode"`

	// Placement is which authority this unit is and which routing it is serving under.
	// A multi-authority run is only certifiable if every participating unit agrees on
	// the routing version and reports a compatible schema, and the harness cannot ask
	// an operator to transcribe either (measurement-contract §11).
	Placement PlacementMeta `json:"placement"`
}

// PlacementMeta is what a shard-affine service unit can say about its own routing.
//
// It is reported even when the deployment is unsharded, because "there was one
// authority" is an answer a result should carry rather than a field a reader has to
// infer from absence.
type PlacementMeta struct {
	// AuthorityID names the writable authority this unit is bound to.
	AuthorityID string `json:"authority_id"`
	// RoutingVersion is the placement map's version, or "unsharded".
	RoutingVersion string `json:"routing_version"`
	// Sharded distinguishes a unit serving part of a placement map from one serving a
	// single-authority deployment. Derivable from RoutingVersion, and stated anyway:
	// a boolean a reader can trust beats a string they have to know the convention for.
	Sharded bool `json:"sharded"`
	// Organisations are the organisations this unit serves, empty when unsharded
	// because "every organisation" is not a list. Bounded by the placement map, so
	// unlike a metric label it is safe here.
	Organisations []string `json:"organisations,omitempty"`
}

// DatabaseMeta is what the service can say about its own authority without asking the
// operator. Both fields can change a measurement — the server version decides planner
// behaviour, and the pool ceiling is one of the candidate limiting mechanisms
// ag-sept/milestone-validation.md §7 lists — which is the bar measurement-contract.md §11 sets for
// inclusion.
type DatabaseMeta struct {
	// Version is PostgreSQL's own `server_version`, empty when it could not be read.
	Version string `json:"version,omitempty"`
	// PoolMaxConns is this process's configured ceiling, not the deployment's aggregate:
	// the aggregate needs a replica count, which one process cannot know, so it stays
	// operator-supplied (measurement-contract §13.2).
	PoolMaxConns int32 `json:"pool_max_conns,omitempty"`
	// SchemaVersion is the migration version this authority is at, empty when it could
	// not be read. A multi-authority run is only admissible if every participating
	// authority is schema-compatible with the serving binary
	// (horizontal-database-authority §5.1), and this is what lets the harness check it
	// rather than assume it.
	SchemaVersion int64 `json:"schema_version,omitempty"`
}

// handleMeta returns runtime metadata and the resolved timing configuration as JSON.
func handleMeta(
	source func() buildinfo.Info,
	cfg config.Config,
	db DatabaseMeta,
	telemetry string,
	guard placementGuard,
) http.HandlerFunc {
	placement := PlacementMeta{
		AuthorityID:    string(guard.authority),
		RoutingVersion: guard.placement.Version(),
		Sharded:        !guard.placement.IsUnsharded(),
	}
	for _, org := range guard.placement.Organisations(guard.authority) {
		placement.Organisations = append(placement.Organisations, string(org))
	}

	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, metaResponse{
			Info:             source(),
			RequestBudget:    cfg.RequestBudget,
			ReservationTTL:   cfg.ReservationTTL.String(),
			ReadinessTimeout: cfg.ReadinessTimeout.String(),
			Database:         db,
			Telemetry:        telemetry,
			Placement:        placement,
		})
	}
}
