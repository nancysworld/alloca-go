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
// probeTimeout is config.ReadinessTimeout, which is bounded against the request budget at
// startup rather than derived here; see that field for why the relationship matters.
//
// The reason is not echoed to the caller — an infrastructure error can carry a DSN or
// driver detail, and this is an unauthenticated endpoint — so it is logged here instead.
// Withholding the detail and recording it are one decision, and keeping them in one
// function is what stops the reason being lost altogether, as it was in this PR's first
// draft.
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
