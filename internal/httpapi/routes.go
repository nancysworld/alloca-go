package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/nancysworld/alloca-go/internal/buildinfo"
	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// Route paths, kept as constants so tests and future handlers share one source of truth.
//
// The operational surface keeps its AG-M0 names, which are the documented operational
// contract. The booking surface is versioned from the start because the request contract
// already has a version — v2 of the idempotency request hash (transaction-semantics §5.1)
// — and a URL that cannot express which contract it speaks is a trap the moment the second
// one exists.
const (
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
	pathMeta    = "/meta"

	pathListSlots = "GET /v1/slots"
	pathReserve   = "POST /v1/slots/{" + varSlotOrganisation + "}/{" + varSlotID + "}/reservations"
	pathConfirm   = "POST /v1/reservations/{" + varReservationID + "}/confirm"
	pathCancel    = "POST /v1/reservations/{" + varReservationID + "}/cancel"
)

// Path wildcard names. A slot is named by both halves of its identity: slot identifiers
// are unique only within the owning organisation (§1.2), so a URL naming only the slot
// could not address one unambiguously.
const (
	varSlotOrganisation = "slot_organisation_id"
	varSlotID           = "slot_id"
	varReservationID    = "reservation_id"
)

// registerRoutes wires the operational and booking endpoints onto mux. The booking
// endpoints are registered only when a service is supplied, so a test that only wants the
// probes still builds a server without a database behind it.
func registerRoutes(
	mux *http.ServeMux,
	metaSource func() buildinfo.Info,
	cfg config.Config,
	ready ReadinessFunc,
	logger *slog.Logger,
	svc BookingService,
	slots SlotLister,
	recorder telemetry.Recorder,
	db DatabaseMeta,
	telemetryMode string,
) {
	mux.HandleFunc(pathHealthz, handleHealthz)
	mux.HandleFunc(pathReadyz, handleReadyz(ready, cfg.ReadinessTimeout, logger))
	mux.HandleFunc(pathMeta, handleMeta(metaSource, cfg, db, telemetryMode))

	if svc != nil {
		h := &bookingHandlers{svc: svc, recorder: recorder, budget: cfg.RequestBudget}
		mux.HandleFunc(pathReserve, h.reserve)
		mux.HandleFunc(pathConfirm, h.confirm)
		mux.HandleFunc(pathCancel, h.cancel)
	}
	if slots != nil {
		h := &slotHandlers{slots: slots, recorder: recorder, budget: cfg.RequestBudget}
		mux.HandleFunc(pathListSlots, h.listSlots)
	}
}
