package httpapi

import (
	"encoding/json"
	"net/http"
)

// handleHealthz is the liveness probe: it returns 200 as long as the process can
// serve HTTP. It does not check dependencies — that is readiness' job.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is the readiness probe. It returns 200 when ready reports no error,
// and 503 with the reason otherwise, so an orchestrator can withhold traffic until
// dependencies (added in AG-M1) are healthy.
func handleReadyz(ready ReadinessFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := ready(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"reason": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

// writeJSON writes v as a JSON response with the given status code. Encoding
// errors are ignored: the header is already committed and there is no useful
// recovery on a broken client connection.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
