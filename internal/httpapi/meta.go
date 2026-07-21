package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
)

// handleMeta returns runtime and build metadata as JSON. Every capacity result the
// project publishes must be traceable to the environment it ran in; this endpoint
// is the machine-readable source of that provenance (roadmap §4.4).
func handleMeta(source func() buildinfo.Info) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, source())
	}
}
