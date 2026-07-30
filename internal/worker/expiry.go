// Package worker holds Alloca-Go's background processing. AG-M1 has exactly one worker:
// expiry, which settles holds whose TTL has lapsed so their capacity returns to the slot.
//
// It is not what makes expiry correct. Every operation already settles the slot it locks
// before evaluating preconditions (transaction-semantics §2.1), so a request never sees
// stale state whether or not this worker has run; the worker exists to return capacity
// that *nobody is asking for*. That is why it needs no leader election and no
// exactly-once machinery: each iteration settles under the same slot lock as every
// request, so two workers, or a worker racing a request, produce the same state as one.
// Running two is wasteful, never wrong.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// SlotSource lists slots that may hold elapsed reservations. Declared here, where it is
// consumed; *postgres.Repo satisfies it.
type SlotSource interface {
	ElapsedHoldSlots(ctx context.Context, limit int) ([]domain.SlotRef, error)
}

// Settler expires a slot's elapsed holds under its lock. *service.Service satisfies it.
//
// The worker deliberately cannot reach the repository directly: deciding that a hold has
// elapsed is a domain decision, and routing through the service is what keeps the worker
// and the request path applying identical rules.
type Settler interface {
	SettleSlot(ctx context.Context, ref domain.SlotRef) (expired int, err error)
}

// Defaults for a zero-valued Config.
const (
	// DefaultInterval is how often the worker looks for work. It is not a correctness
	// parameter — no request depends on the worker having run — so it is tuned for
	// modest load rather than for promptness.
	DefaultInterval = 5 * time.Second
	// DefaultBatch bounds one iteration's slots, so a large backlog is drained over
	// several ticks instead of one long transaction sequence holding connections.
	DefaultBatch = 50
)

// Config tunes the worker. The zero value is usable and uses the defaults above.
type Config struct {
	Interval time.Duration
	Batch    int
}

// Expiry is the expiry worker.
type Expiry struct {
	slots    SlotSource
	settler  Settler
	recorder telemetry.Recorder
	logger   *slog.Logger
	cfg      Config
}

// NewExpiry constructs the worker. A nil recorder discards observations; a nil logger
// uses slog.Default.
func NewExpiry(slots SlotSource, settler Settler, recorder telemetry.Recorder, logger *slog.Logger, cfg Config) *Expiry {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	if recorder == nil {
		recorder = telemetry.Nop{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Expiry{slots: slots, settler: settler, recorder: recorder, logger: logger, cfg: cfg}
}

// RunOnce performs exactly one iteration and reports what it did. It is exported so a
// test can drive an iteration with no ticker and no sleeping, which leaves Run thin
// enough to have nothing left to get wrong.
//
// An error from settling one slot ends the iteration rather than skipping to the next:
// the realistic cause is the database being unreachable or overloaded, which the next
// slot would hit too, and the following tick retries from a freshly derived candidate
// list. The observation still reports what was settled before the failure, so that work
// is not lost from the telemetry.
func (e *Expiry) RunOnce(ctx context.Context) (telemetry.ExpiryObservation, error) {
	start := time.Now()
	obs := telemetry.ExpiryObservation{}

	refs, err := e.slots.ElapsedHoldSlots(ctx, e.cfg.Batch)
	if err != nil {
		obs.Failed = true
		obs.Duration = time.Since(start)
		return obs, err
	}
	obs.Slots = len(refs)

	for _, ref := range refs {
		expired, err := e.settler.SettleSlot(ctx, ref)
		obs.Expired += expired
		if err != nil {
			obs.Failed = true
			obs.Duration = time.Since(start)
			return obs, err
		}
	}
	obs.Duration = time.Since(start)
	return obs, nil
}

// Run polls until ctx is cancelled, then returns.
//
// A failed iteration is reported and the loop continues: expiry is opportunistic, and a
// transient database failure must not take down a process that is still serving requests
// perfectly well from its own settlement path. Cancellation is not a failure — during
// shutdown the in-flight iteration's context is already done, and logging that as an
// error would make every clean shutdown look like an incident.
func (e *Expiry) Run(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			obs, err := e.RunOnce(ctx)
			e.recorder.RecordExpiry(ctx, obs)
			if err != nil && ctx.Err() == nil {
				// The error is diagnostic context for a human, never a telemetry
				// dimension: the observation above already carries the fact of failure
				// in a form that can be counted (internal/telemetry).
				e.logger.ErrorContext(ctx, "expiry iteration failed", slog.Any("error", err))
			}
		}
	}
}
