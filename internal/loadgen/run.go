package loadgen

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Options configure one run.
type Options struct {
	Concurrency int
	// Iterations bounds the run by logical units of work. Closed-loop by concurrency is
	// sufficient for PR1 (§6.3); open-loop rate control is a later, optional addition.
	Iterations int
	// WarmUp discards responses completed before this much of the run has elapsed, so
	// connection setup and JIT-warm effects do not land in the reported percentiles.
	WarmUp time.Duration
}

// Runner executes a workload against a service with a fixed number of concurrent workers.
//
// Starts are synchronized: every worker blocks on one barrier and is released together, so
// a hot-slot or hot-identity workload actually contends rather than arriving in a queue the
// generator itself created (§6.3).
type Runner struct {
	client *Client
	opts   Options
}

func NewRunner(c *Client, opts Options) *Runner { return &Runner{client: c, opts: opts} }

// Run executes the workload and returns the summary. It stops early if ctx is cancelled;
// responses already collected are still summarised, because a truncated run that reports
// what it did is more useful than one that reports nothing.
func (r *Runner) Run(ctx context.Context, w Workload) Summary {
	var (
		seq       atomic.Int64
		mu        sync.Mutex
		collected []Response
		barrier   = make(chan struct{})
		wg        sync.WaitGroup
	)

	// Generator utilisation is sampled around the run rather than derived afterwards:
	// §6.3 requires the harness to expose enough of its own behaviour to rule out
	// generator saturation, and a number computed from the run's own duration would beg
	// the question.
	startCPU := cpuSample()

	for range r.opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			for {
				n := int(seq.Add(1)) - 1
				if n >= r.opts.Iterations || ctx.Err() != nil {
					return
				}
				responses := w.Do(ctx, r.client, n)
				now := time.Now()
				mu.Lock()
				for i := range responses {
					responses[i].completedAt = now
					collected = append(collected, responses[i])
				}
				mu.Unlock()
			}
		}()
	}

	started := time.Now()
	close(barrier)
	wg.Wait()
	elapsed := time.Since(started)

	measured, discarded := applyWarmUp(collected, started, r.opts.WarmUp)
	s := summarise(w.Name(), measured, elapsed, cpuDelta(startCPU), r.opts, r.client.validate)
	s.WarmUpDiscarded = discarded
	s.WarmUpSeconds = r.opts.WarmUp.Seconds()
	return s
}

// Summary is the machine-readable run result: the totals a capacity claim is built from,
// and the evidence that the claim is admissible at all.
type Summary struct {
	Workload    string `json:"workload"`
	Concurrency int    `json:"concurrency"`
	Iterations  int    `json:"iterations"`

	DurationSeconds float64 `json:"duration_seconds"`
	WarmUpSeconds   float64 `json:"warm_up_seconds"`
	// WarmUpDiscarded counts responses excluded from the totals by the warm-up window.
	// Reported so a reader can see how much of the sample the window consumed.
	WarmUpDiscarded int `json:"warm_up_discarded"`

	// Totals is every completed request keyed by operation and outcome, with replay as an
	// orthogonal dimension rather than a peer outcome (measurement-contract §4).
	Totals []Total `json:"totals"`

	// Completed counts every request that reached a terminal classification. Goodput
	// counts only successful *mutations* — summed over the three mutation operations,
	// never over all requests, since a successful listing is not a booking
	// (observability §3.1).
	Completed int `json:"completed_requests"`
	Goodput   int `json:"successful_mutation_goodput"`

	LatencyMS Percentiles `json:"latency_ms"`

	// Generator reports the harness's own behaviour, so a client-limited plateau cannot be
	// reported as server capacity.
	Generator GeneratorStats `json:"generator"`

	// ValidationEnabled records whether responses were checked. Invalid counts the
	// responses that failed the check.
	ValidationEnabled bool     `json:"response_validation_enabled"`
	Invalid           int      `json:"invalid_responses"`
	InvalidSamples    []string `json:"invalid_samples,omitempty"`

	// Quotable is the harness's own verdict on whether this run may be quoted. It is false
	// whenever validation was disabled or any response failed validation — a run cannot
	// certify itself by staying silent about the checks it skipped
	// (measurement-contract §5.4).
	Quotable           bool   `json:"quotable"`
	NotQuotableBecause string `json:"not_quotable_because,omitempty"`
}

// FreshAdmittedFor counts admitted successes for one operation, excluding replays.
//
// Replays are excluded because a replay re-reports a mutation that already happened: it
// returns the originally recorded outcome and creates no new reservation, claim or record.
// Counting it would compare N+R client admissions against N persisted rows and fail a
// service behaving exactly as the idempotency contract requires — which is what an earlier
// version of this did, caught by the integration suite rather than by reading.
func (s Summary) FreshAdmittedFor(operation string) int {
	n := 0
	for _, t := range s.Totals {
		if t.Operation == operation && t.Outcome == domain.OutcomeAdmittedSuccess && !t.Replay {
			n += t.Count
		}
	}
	return n
}

// FreshMutations counts non-replay mutation requests that reached a terminal domain
// answer, which is the population an idempotency record is written for.
//
// Replays are excluded because a replay returns a recorded outcome without writing a new
// record; counting them would demand one record per replay and fail a correct service.
// invalid_request is excluded too: it stops at the transport edge and may carry no key to
// scope a record by (transaction-semantics §5.5, INV-7).
func (s Summary) FreshMutations() int {
	n := 0
	for _, t := range s.Totals {
		switch {
		case t.Replay,
			!domain.Operation(t.Operation).IsKnown(),
			t.Outcome == domain.OutcomeInvalidRequest,
			!isDefiniteTerminal(t.Outcome):
			continue
		}
		n += t.Count
	}
	return n
}

// isDefiniteTerminal reports whether an outcome definitely committed one way or the other.
// unknown_replayable is by definition uncertain, and the timeout and failure outcomes may
// or may not have committed — none of them can be required to have a durable record
// (INV-7's named exclusions).
func isDefiniteTerminal(o domain.Outcome) bool {
	switch o {
	case domain.OutcomeAdmittedSuccess, domain.OutcomeBusinessRefusal:
		return true
	default:
		return false
	}
}

// Total is one (operation, outcome, reason, replay) cell.
type Total struct {
	Operation string         `json:"operation"`
	Outcome   domain.Outcome `json:"outcome"`
	Reason    domain.Reason  `json:"reason,omitempty"`
	Replay    bool           `json:"replay"`
	Count     int            `json:"count"`
}

// Percentiles are client-observed latencies in milliseconds.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// GeneratorStats exposes the harness's own utilisation.
type GeneratorStats struct {
	GOMAXPROCS int     `json:"gomaxprocs"`
	Goroutines int     `json:"goroutines"`
	CPUSeconds float64 `json:"cpu_seconds"`
	CPUPerCore float64 `json:"cpu_utilisation_per_core"`
	NumCPU     int     `json:"num_cpu"`
}

// summarise folds responses into the reported totals.
func summarise(
	workload string, responses []Response, elapsed time.Duration,
	cpuSeconds float64, opts Options, validated bool,
) Summary {
	s := Summary{
		Workload:          workload,
		Concurrency:       opts.Concurrency,
		Iterations:        opts.Iterations,
		DurationSeconds:   elapsed.Seconds(),
		ValidationEnabled: validated,
	}

	cells := map[Total]int{}
	latencies := make([]float64, 0, len(responses))
	for _, r := range responses {
		s.Completed++
		cells[Total{Operation: r.Operation, Outcome: r.Outcome, Reason: r.Reason, Replay: r.Replay}]++
		latencies = append(latencies, float64(r.Latency)/float64(time.Millisecond))

		if r.Outcome == domain.OutcomeAdmittedSuccess && isMutation(r.Operation) {
			s.Goodput++
		}
		if r.Invalid != "" {
			s.Invalid++
			if len(s.InvalidSamples) < 5 {
				s.InvalidSamples = append(s.InvalidSamples, r.Invalid)
			}
		}
	}

	for cell, n := range cells {
		cell.Count = n
		s.Totals = append(s.Totals, cell)
	}
	sort.Slice(s.Totals, func(i, j int) bool {
		a, b := s.Totals[i], s.Totals[j]
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		if a.Outcome != b.Outcome {
			return a.Outcome < b.Outcome
		}
		return a.Reason < b.Reason
	})

	s.LatencyMS = percentiles(latencies)
	s.Generator = GeneratorStats{
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		Goroutines: runtime.NumGoroutine(),
		CPUSeconds: cpuSeconds,
		NumCPU:     runtime.NumCPU(),
	}
	if elapsed > 0 {
		s.Generator.CPUPerCore = cpuSeconds / elapsed.Seconds() / float64(runtime.NumCPU())
	}

	switch {
	case !validated:
		s.NotQuotableBecause = "response validation was disabled: an unchecked 200 cannot " +
			"be counted as goodput (measurement-contract §5.4)"
	case s.Invalid > 0:
		s.NotQuotableBecause = "one or more responses failed status/outcome validation"
	default:
		s.Quotable = true
	}
	return s
}

// applyWarmUp drops responses that completed inside the warm-up window and reports how
// many were dropped.
//
// The count is reported rather than silently absorbed: a run whose warm-up swallowed most
// of its sample is a run whose percentiles rest on very little, and the only way a reader
// can notice that is if the number is on the page. A zero window discards nothing.
func applyWarmUp(responses []Response, started time.Time, warmUp time.Duration) ([]Response, int) {
	if warmUp <= 0 {
		return responses, 0
	}
	cutoff := started.Add(warmUp)
	kept := make([]Response, 0, len(responses))
	for _, r := range responses {
		if r.completedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, r)
	}
	return kept, len(responses) - len(kept)
}

// isMutation reports whether an operation counts toward booking goodput. The read route
// deliberately does not: a successful listing is admitted_success in the sense that the
// service answered correctly, but it is not a booking (observability §3.1).
func isMutation(op string) bool { return domain.Operation(op).IsKnown() }

// percentiles computes the reported quantiles by nearest-rank on the sorted sample.
func percentiles(v []float64) Percentiles {
	if len(v) == 0 {
		return Percentiles{}
	}
	sort.Float64s(v)
	return Percentiles{
		P50: quantile(v, 0.50),
		P95: quantile(v, 0.95),
		P99: quantile(v, 0.99),
		Max: v[len(v)-1],
	}
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(q * float64(len(sorted)))
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}
