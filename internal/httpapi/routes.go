package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
)

// Route paths. Kept as constants so tests and future handlers share one source of
// truth.
const (
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
	pathMeta    = "/meta"
)

// registerRoutes wires the operational endpoints onto mux.
func registerRoutes(mux *http.ServeMux, metaSource func() buildinfo.Info, budget config.RequestBudget, ready ReadinessFunc) {
	mux.HandleFunc(pathHealthz, handleHealthz)
	mux.HandleFunc(pathReadyz, handleReadyz(ready))
	mux.HandleFunc(pathMeta, handleMeta(metaSource, budget))
}
