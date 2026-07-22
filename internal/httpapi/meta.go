package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

// metaResponse is the /meta payload. It inlines buildinfo.Info (runtime and build
// provenance) and adds the resolved, validated per-request deadline budget. Every
// capacity result the project publishes must be traceable both to the environment it
// ran in (roadmap §4.4) and to the deadline chain it ran under (measurement-contract
// §8.1); this endpoint is the machine-readable source of both.
type metaResponse struct {
	buildinfo.Info
	RequestBudget config.RequestBudget `json:"request_budget"`
}

// handleMeta returns runtime metadata and the resolved request budget as JSON.
func handleMeta(source func() buildinfo.Info, budget config.RequestBudget) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, metaResponse{Info: source(), RequestBudget: budget})
	}
}
