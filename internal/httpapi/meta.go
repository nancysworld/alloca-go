package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

// metaResponse is the /meta payload. It inlines buildinfo.Info (runtime and build
// provenance) and adds the resolved, validated timing configuration. Every capacity
// result the project publishes must be traceable both to the environment it ran in
// (roadmap §4.4) and to the timings it ran under (measurement-contract §8.1); this
// endpoint is the machine-readable source of both.
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
	// that lets ag-sept-plan §6.2's comparison identify its own arms.
	Telemetry string `json:"telemetry_mode"`
}

// DatabaseMeta is what the service can say about its own authority without asking the
// operator. Both fields can change a measurement — the server version decides planner
// behaviour, and the pool ceiling is one of the admission boundaries §11.2 lists as a
// candidate frontier — which is the bar §6.4 sets for inclusion.
type DatabaseMeta struct {
	// Version is PostgreSQL's own `server_version`, empty when it could not be read.
	Version string `json:"version,omitempty"`
	// PoolMaxConns is this process's configured ceiling, not the deployment's aggregate:
	// the aggregate needs a replica count, which one process cannot know (PR3).
	PoolMaxConns int32 `json:"pool_max_conns,omitempty"`
}

// handleMeta returns runtime metadata and the resolved timing configuration as JSON.
func handleMeta(source func() buildinfo.Info, cfg config.Config, db DatabaseMeta, telemetry string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, metaResponse{
			Info:             source(),
			RequestBudget:    cfg.RequestBudget,
			ReservationTTL:   cfg.ReservationTTL.String(),
			ReadinessTimeout: cfg.ReadinessTimeout.String(),
			Database:         db,
			Telemetry:        telemetry,
		})
	}
}
