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
}

// handleMeta returns runtime metadata and the resolved timing configuration as JSON.
func handleMeta(source func() buildinfo.Info, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, metaResponse{
			Info:             source(),
			RequestBudget:    cfg.RequestBudget,
			ReservationTTL:   cfg.ReservationTTL.String(),
			ReadinessTimeout: cfg.ReadinessTimeout.String(),
		})
	}
}
