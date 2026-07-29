package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// handleHealthz is the liveness probe: it returns 200 as long as the process can
// serve HTTP. It does not check dependencies — that is readiness' job.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is the readiness probe. It returns 200 when ready reports no error, and
// 503 otherwise, so an orchestrator can withhold traffic until dependencies are healthy.
//
// The check is bounded by probeTimeout, independently of the per-request server
// deadline. A readiness probe that can run for the whole of a request budget is useless
// to an orchestrator whose probe interval is a few seconds: it would report "still
// deciding" exactly when the answer matters. The bound comes from the budget's
// DBAcquireCap — the same limit the request path allows for obtaining a connection —
// because a pool that cannot hand one out within its own cap would fail real requests
// anyway, which is precisely what "not ready" should mean.
//
// The reason is not echoed to the caller — an infrastructure error can carry a DSN or
// driver detail, and this is an unauthenticated endpoint — so it is logged here instead.
// Removing the detail from the response and recording it are one decision, and keeping
// them in one function is what stops the reason being lost altogether.
func handleReadyz(ready ReadinessFunc, probeTimeout time.Duration, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
		defer cancel()

		if err := ready(ctx); err != nil {
			logger.WarnContext(ctx, "readiness check failed",
				slog.Any("error", err),
				slog.Duration("probe_timeout", probeTimeout),
			)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
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
