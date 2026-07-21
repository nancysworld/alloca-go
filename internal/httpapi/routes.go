package httpapi

import (
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
)

// Route paths. Kept as constants so tests and future handlers share one source of
// truth.
const (
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
	pathMeta    = "/meta"
)

// registerRoutes wires the AG-M0 operational endpoints onto mux.
func registerRoutes(mux *http.ServeMux, metaSource func() buildinfo.Info, ready ReadinessFunc) {
	mux.HandleFunc(pathHealthz, handleHealthz)
	mux.HandleFunc(pathReadyz, handleReadyz(ready))
	mux.HandleFunc(pathMeta, handleMeta(metaSource))
}
