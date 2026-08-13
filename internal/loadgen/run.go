package loadgen

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Options configure one run. Exactly one of Iterations or Duration bounds it.
type Options struct {
	Concurrency int
	// Iterations bounds the run by logical units of work. Closed-loop by concurrency is
	// sufficient for PR1 (§6.3); open-loop rate control is a later, optional addition.
	//
	// It is the wrong bound for a sweep, which is why Duration exists. A fixed iteration
	// count makes a cell's length vary *inversely* with throughput: the faster the service
	// goes the sooner the cell ends, so the fewest samples are collected at exactly the
	// operating points a frontier is read from, and no two cells cover the same interval.
	// PR1's 60-iteration smoke run completed in 0.06s, which no rate() window can resolve.
	Iterations int
	// Duration bounds the run by wall-clock instead: workers keep pulling units until the
	// window elapses. Every cell then covers the same interval whatever throughput it
	// reaches, which is what makes rates comparable across a sweep.
	//
	// Zero means bound by Iterations. Both set is a configuration error the caller rejects,
	// not something resolved silently here — a run bounded by the one the operator did not
	// mean is a measurement of the wrong thing.
	Duration time.Duration
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
		finished  atomic.Int64
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

	// In duration mode the window is a deadline the workers watch, not a context timeout.
	// A context deadline would cancel requests already in flight when it expired, turning
	// the tail of every cell into artificial timeouts — the run would report faults it
	// caused itself, at exactly the load where real faults matter. Instead each worker
	// finishes the unit it is on and then stops, so the cell ends cleanly and slightly
	// after its nominal window rather than mid-request.
	//
	// Written below, after `started`, and read only by workers that have already received
	// from the barrier. The channel close is what orders the write before every read, so
	// the deadline is measured from the same instant the run is, not from whenever the last
	// goroutine happened to be scheduled.
	var deadline time.Time

	for range r.opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			for {
				n := int(seq.Add(1)) - 1
				if ctx.Err() != nil {
					return
				}
				if deadline.IsZero() && n >= r.opts.Iterations {
					return
				}
				if !deadline.IsZero() && !time.Now().Before(deadline) {
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
				// Counted after Do returns, so this is *logical units finished*, not
				// units started. A workload that issues several requests per unit —
				// dispersed with -confirm — makes the two different numbers, and it is
				// this one that says whether the experiment ran to its stated size.
				finished.Add(1)
			}
		}()
	}

	started := time.Now()
	if r.opts.Duration > 0 {
		deadline = started.Add(r.opts.Duration)
	}
	close(barrier)
	wg.Wait()
	elapsed := time.Since(started)

	measured, discarded := applyWarmUp(collected, started, r.opts.WarmUp)
	return summarise(w.Name(), w.IntendsReplays(), measured, elapsed, cpuDelta(startCPU), r.opts, r.client.validate,
		runFacts{
			completedUnits:      int(finished.Load()),
			interrupted:         ctx.Err() != nil,
			warmUpDiscarded:     discarded,
			warmUpWindow:        r.opts.WarmUp,
			iterationsRequested: r.opts.Iterations,
			duration:            r.opts.Duration,
			elapsed:             elapsed,
		})
}

// runFacts are what Run observed about the run itself rather than about any response.
// They are grouped because they share one purpose: each is a way for a run to be shaped
// differently from the experiment it claims to be, and all three decide quotability.
type runFacts struct {
	completedUnits  int
	interrupted     bool
	warmUpDiscarded int
	warmUpWindow    time.Duration
	// iterationsRequested is the unit count asked for, meaningful only when duration is zero.
	iterationsRequested int
	// duration is the window the run was bounded by, zero when it was bounded by an
	// iteration count instead. The two modes fail differently, which is why it is here.
	duration time.Duration
	// elapsed is how long the run actually took.
	elapsed time.Duration
}

// truncated reports whether the run covered less of the experiment than it was asked to.
//
// The test differs by mode, and conflating them is the bug this method exists to prevent. An
// iteration-bounded run is truncated when it finished fewer units than requested. A
// duration-bounded run has no requested unit count — completing "few" units is a *result*, not
// a shortfall, and comparing against Options.Iterations there would refuse every sweep cell at
// the frontier, where units are largest and slowest.
//
// What truncates a duration run is the clock: the window did not elapse, which only happens if
// the context was cancelled.
func (f runFacts) truncated() bool {
	if f.duration > 0 {
		return f.interrupted || f.elapsed < f.duration
	}
	return f.interrupted || f.completedUnits < f.iterationsRequested
}

func (f runFacts) truncationReason(s Summary) string {
	if f.duration > 0 {
		return fmt.Sprintf("run was interrupted after %s of a %s window, so the measured "+
			"interval is shorter than the one the manifest describes and every rate derived "+
			"from it is wrong", f.elapsed.Round(time.Millisecond), f.duration)
	}
	return fmt.Sprintf("run was interrupted: %d of %d logical iterations completed, so the "+
		"manifest describes a larger experiment than the one that ran",
		s.CompletedIterations, f.iterationsRequested)
}

// Summary is the machine-readable run result: the totals a capacity claim is built from,
// and the evidence that the claim is admissible at all.
type Summary struct {
	Workload string `json:"workload"`
	// ReplaysIntended records whether this workload drives replays deliberately, so a reader
	// — and Certify — can tell a measured disposition control from a run served out of a
	// previous run's idempotency records. Both report replays; only one of them meant to.
	ReplaysIntended bool `json:"replays_intended"`
	Concurrency     int  `json:"concurrency"`
	Iterations      int  `json:"iterations"`
	// DurationRequestedSeconds is the window a duration-bounded run was asked for, zero when
	// the run was bounded by Iterations instead. Reported so a reader can tell which bound
	// applied without inferring it: `iterations: 100` on a duration run is the flag default,
	// not a request, and reading it as one would misdescribe the experiment.
	DurationRequestedSeconds float64 `json:"duration_requested_seconds,omitempty"`
	// CompletedIterations counts logical units of work that finished. It is reported
	// separately from Completed because Completed counts *requests*: a workload issuing
	// several requests per unit makes the two different numbers, and only this one can
	// show that an interrupted run did less work than its manifest claims.
	CompletedIterations int `json:"completed_iterations"`

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
	// (observability §3.1) — and only *fresh* ones.
	//
	// Replays are excluded because measurement-contract §3 defines goodput as useful domain
	// operations completed, and §4.2 defines a replay as one that "returned the originally
	// recorded terminal outcome rather than performing a new mutation". A replay is a
	// correct response that commits nothing; counting it would both overstate the work done
	// and count one logical booking twice, since its original was already counted. It is
	// also the definition the database agrees with — reconciliation compares persisted rows
	// against FreshAdmittedFor, which has always excluded replays.
	//
	// ReplayedMutations is the same population that goodput now leaves out, reported rather
	// than dropped. An excluded quantity that appears nowhere is indistinguishable from one
	// that never happened, and a replay is a correctness claim in its own right (INV-5): the
	// service must return the recorded outcome without re-running the mutation. The `replay`
	// workload exists to exercise and check that claim.
	Completed         int `json:"completed_requests"`
	Goodput           int `json:"successful_mutation_goodput"`
	ReplayedMutations int `json:"replayed_mutations"`

	LatencyMS Percentiles `json:"latency_ms"`

	// Generator reports the harness's own behaviour, so a client-limited plateau cannot be
	// reported as server capacity.
	Generator GeneratorStats `json:"generator"`

	// ValidationEnabled records whether responses were checked. Invalid counts the
	// responses that failed the check.
	ValidationEnabled bool     `json:"response_validation_enabled"`
	Invalid           int      `json:"invalid_responses"`
	InvalidSamples    []string `json:"invalid_samples,omitempty"`

	// Sound is the generator's verdict on whether this run measured the experiment its
	// manifest describes. It is false whenever validation was disabled or any response
	// failed validation — a run cannot certify itself by staying silent about the checks it
	// skipped (measurement-contract §5.4) — and also when the run was interrupted or
	// discarded warm-up responses.
	//
	// It is deliberately not called "quotable". Soundness is necessary for quoting a run and
	// nowhere near sufficient: what a sound run may *back* depends on provenance the summary
	// cannot see, since the manifest is assembled beside it. Certify joins the two, and its
	// Level is the field to read. An unsound run is LevelNone whatever its manifest says.
	Sound           bool   `json:"measurement_sound"`
	NotSoundBecause string `json:"not_sound_because,omitempty"`

	// Resolved records the ambiguity-resolution pass, empty for a run that produced no
	// `unknown_replayable`. It is what makes the two accountings measurement-contract §12
	// distinguishes both derivable from one artifact: Totals already carries every HTTP
	// attempt including the resolution requests, while this says which logical mutation each
	// key turned out to hold.
	//
	// Kept as the resolution's *outcome* rather than as a recomputed count, so a reader can
	// audit the arithmetic rather than trust it.
	Resolved []ResolvedMutation `json:"ambiguity_resolutions,omitempty"`
}

// ResolvedMutation is one ambiguous mutation's settled identity and answer.
//
// Key and User are recorded because the resolution is only meaningful as a statement about
// *that* idempotency scope: "one key contributed one fresh mutation" cannot be checked from
// an artifact that does not say which key.
type ResolvedMutation struct {
	Operation string         `json:"operation"`
	UserOrg   string         `json:"user_organisation_id"`
	UserID    string         `json:"user_id"`
	Key       string         `json:"idempotency_key"`
	Outcome   domain.Outcome `json:"outcome"`
	Reason    domain.Reason  `json:"reason,omitempty"`
	// Replay is the load-bearing field. True means the original attempt had committed and
	// this returned its record, so the fresh mutation belongs to the original; false means
	// the original had not committed and this request performed it.
	Replay bool `json:"replay"`
	// StillAmbiguous means the replay did not settle the key: it returned
	// `unknown_replayable` again, or failed at the transport, or was refused at the edge.
	// Any of these makes the run unreconcilable (measurement-contract §12).
	StillAmbiguous bool `json:"still_ambiguous,omitempty"`
	// Invalid carries the harness's validation complaint about the resolution response, or
	// is empty when it satisfied the contract. Recorded here rather than folded into the
	// measured Invalid count, which describes the measured interval alone.
	Invalid string `json:"invalid,omitempty"`
}

// WithResolutions records a post-run resolution pass, per measurement-contract §12.
//
// **It deliberately does not touch a single measured field.** Totals, Completed, Goodput,
// ReplayedMutations, latency and the duration stay exactly as the measured interval observed
// them, because post-run resolution may change what we know about final logical state but must
// not rewrite performance history. An original that returned `unknown_replayable` contributes
// one measured request and zero measured goodput, and goes on doing so even after a resolution
// proves its mutation committed — the client did not receive a definite successful outcome
// inside the interval, and no later discovery changes what happened during it.
//
// What the pass produces instead is Resolved: the retained, auditable record of the
// reconciliation population. Two derivations read it, and nothing else does —
// ReconciliationTotals/ReconciliationCompleted for the server-scrape comparison, and
// FreshAdmittedFor/FreshMutations for final logical state.
//
// A run carrying any still-ambiguous entry is marked unsound: no final logical-mutation count
// is established for that key, so no verdict over the run means anything. A resolution
// response that failed validation does the same, for the reason validation always does — the
// service answered something the contract does not define, and post-run traffic is no more
// exempt from that than measured traffic is.
func (s Summary) WithResolutions(resolutions []Resolution) Summary {
	if len(resolutions) == 0 {
		return s
	}

	resolved := make([]ResolvedMutation, 0, len(resolutions))
	var stillAmbiguous, invalid int

	for _, r := range resolutions {
		if r.StillAmbiguous {
			stillAmbiguous++
		}
		if r.Response.Invalid != "" {
			invalid++
		}
		resolved = append(resolved, ResolvedMutation{
			Operation:      string(r.Ambiguous.Operation),
			UserOrg:        string(r.Ambiguous.User.OrganisationID),
			UserID:         string(r.Ambiguous.User.UserID),
			Key:            r.Ambiguous.Key,
			Outcome:        r.Response.Outcome,
			Reason:         r.Response.Reason,
			Replay:         r.Response.Replay,
			StillAmbiguous: r.StillAmbiguous,
			Invalid:        r.Response.Invalid,
		})
	}
	s.Resolved = resolved

	var refusals []string
	if stillAmbiguous > 0 {
		refusals = append(refusals, fmt.Sprintf(
			"%d mutation(s) remain ambiguous after the resolution pass: their persisted state "+
				"and client record still disagree, so no correctness verdict over this run is "+
				"meaningful (measurement-contract §12)", stillAmbiguous))
	}
	if invalid > 0 {
		refusals = append(refusals, fmt.Sprintf(
			"%d resolution response(s) failed validation: the service answered the replay with "+
				"something the outcome contract does not define", invalid))
	}
	if len(refusals) > 0 && s.Sound {
		s.Sound = false
		s.NotSoundBecause = strings.Join(refusals, "; ")
	}
	return s
}

// addTotal merges one observation into the cell it belongs to, appending a cell only when
// none matches. Totals is a set of (operation, outcome, reason, replay) cells, not a log.
func addTotal(totals []Total, add Total) []Total {
	for i := range totals {
		t := &totals[i]
		if t.Operation == add.Operation && t.Outcome == add.Outcome &&
			t.Reason == add.Reason && t.Replay == add.Replay {
			t.Count += add.Count
			return totals
		}
	}
	return append(totals, add)
}

// resolvedLogicalMutations counts the final logical mutations a resolution pass established
// for the given operation and outcome.
//
// **Both definitive branches count, and each counts once.** `replay=true` proves the original
// committed; `replay=false` means the resolution performed the mutation itself after the
// measured interval. Either way the key holds exactly one final logical mutation. This is not
// a distinction persisted state can see — the database holds one row in both cases — which is
// precisely why it must not be inferred from measured Totals, where only one of the two ever
// appears. A still-ambiguous entry establishes nothing and is excluded.
func (s Summary) resolvedLogicalMutations(operation domain.Operation, want domain.Outcome) int {
	n := 0
	for _, r := range s.Resolved {
		if !r.StillAmbiguous && r.Outcome == want &&
			(operation == "" || r.Operation == string(operation)) {
			n++
		}
	}
	return n
}

// ResolutionAttempts counts the post-run HTTP requests the resolution pass issued, including
// those that stayed ambiguous — the service completed them and counted them either way.
func (s Summary) ResolutionAttempts() int { return len(s.Resolved) }

// ReconciliationCompleted is the request count the reconciliation population covers: everything
// the measured interval saw, plus the post-run resolution attempts.
//
// This is the number to compare against a server scrape taken *after* resolution, which is what
// measurement-contract §12 requires. Measured `Completed` deliberately excludes the resolution
// traffic and must not be used for that comparison.
func (s Summary) ReconciliationCompleted() int { return s.Completed + s.ResolutionAttempts() }

// ReconciliationTotals is the measured cells plus one cell per resolution attempt, as observed.
//
// The measured Totals are returned unchanged when nothing was resolved, and are never mutated:
// the slice is copied before any resolution cell is merged in.
func (s Summary) ReconciliationTotals() []Total {
	if len(s.Resolved) == 0 {
		return s.Totals
	}
	totals := append([]Total(nil), s.Totals...)
	for _, r := range s.Resolved {
		totals = addTotal(totals, Total{
			Operation: r.Operation,
			Outcome:   r.Outcome,
			Reason:    r.Reason,
			Replay:    r.Replay,
			Count:     1,
		})
	}
	return totals
}

// FreshAdmittedFor counts admitted successes for one operation, excluding replays.
//
// Replays are excluded because a replay re-reports a mutation that already happened: it
// returns the originally recorded outcome and creates no new reservation, claim or record.
// Counting it would compare N+R client admissions against N persisted rows and fail a
// service behaving exactly as the idempotency contract requires — which is what an earlier
// version of this did, caught by the integration suite rather than by reading.
// **This is a reconciliation view, not a measured one.** It answers "how many rows should the
// database hold?", so it counts the final logical mutations a resolution pass established as
// well as the ones the measured interval saw definitively. Neither resolution branch appears in
// measured Totals — §12 keeps post-run traffic out of the measurement population — so both are
// added here rather than inferred. Measured performance is Goodput; the two are different
// questions and are deliberately allowed to differ.
func (s Summary) FreshAdmittedFor(operation domain.Operation) int {
	n := 0
	for _, t := range s.Totals {
		if t.Operation == string(operation) && t.Outcome == domain.OutcomeAdmittedSuccess && !t.Replay {
			n += t.Count
		}
	}
	return n + s.resolvedLogicalMutations(operation, domain.OutcomeAdmittedSuccess)
}

// FreshMutations counts non-replay mutation requests that reached a terminal domain
// answer, which is the population an idempotency record is written for.
//
// Replays are excluded because a replay returns a recorded outcome without writing a new
// record; counting them would demand one record per replay and fail a correct service.
// invalid_request is excluded too: it stops at the transport edge and may carry no key to
// scope a record by (transaction-semantics §5.5, INV-7).
//
// Like FreshAdmittedFor this is a reconciliation view. A resolved key holds an idempotency
// record whichever branch it took — written by the original when the replay proves it
// committed, or by the resolution when it performs the mutation — while the original's
// `unknown_replayable` cell is excluded from the sum below because INV-7 requires no record for
// it. Without the credit this would demand fewer records than the database holds.
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
	for _, r := range s.Resolved {
		if !r.StillAmbiguous &&
			domain.Operation(r.Operation).IsKnown() && isDefiniteTerminal(r.Outcome) {
			n++
		}
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
//
// Operation is a plain string, not domain.Operation, because this is the reporting
// boundary: the cell is serialised into the report and joined against the `operation`
// label on the server's own counters, which arrives from Prometheus as text.
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
	workload string, replaysIntended bool, responses []Response, elapsed time.Duration,
	cpuSeconds float64, opts Options, validated bool, facts runFacts,
) Summary {
	s := Summary{
		Workload:                 workload,
		ReplaysIntended:          replaysIntended,
		Concurrency:              opts.Concurrency,
		Iterations:               opts.Iterations,
		CompletedIterations:      facts.completedUnits,
		DurationSeconds:          elapsed.Seconds(),
		DurationRequestedSeconds: facts.duration.Seconds(),
		WarmUpSeconds:            facts.warmUpWindow.Seconds(),
		WarmUpDiscarded:          facts.warmUpDiscarded,
		ValidationEnabled:        validated,
	}

	cells := map[Total]int{}
	latencies := make([]float64, 0, len(responses))
	for _, r := range responses {
		s.Completed++
		cells[Total{Operation: string(r.Operation), Outcome: r.Outcome, Reason: r.Reason, Replay: r.Replay}]++
		latencies = append(latencies, float64(r.Latency)/float64(time.Millisecond))

		// IsKnown is the mutation test: the read route deliberately does not count toward
		// booking goodput. A successful listing is admitted_success in the sense that the
		// service answered correctly, but it is not a booking (observability §3.1).
		if r.Outcome == domain.OutcomeAdmittedSuccess && r.Operation.IsKnown() {
			if r.Replay {
				s.ReplayedMutations++
			} else {
				s.Goodput++
			}
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
		s.NotSoundBecause = "response validation was disabled: an unchecked 200 cannot " +
			"be counted as goodput (measurement-contract §5.4)"
	case s.Invalid > 0:
		s.NotSoundBecause = "one or more responses failed status/outcome validation"
	case facts.truncated():
		// A truncated run still reports — see Run — but it may not be quoted. Its
		// duration covers a smaller experiment than its manifest describes, so every
		// rate derived from it is wrong, and nothing downstream could detect that from
		// the totals alone: they are internally consistent, just for a different run.
		s.NotSoundBecause = facts.truncationReason(s)
	case s.WarmUpDiscarded > 0:
		// The discarded responses left reservations, claims and idempotency records in
		// the database, and the client totals no longer mention them. Persisted-state
		// reconciliation would compare all the rows against the post-warm-up totals and
		// fail a correct service — so PR1 refuses the run rather than reporting one that
		// cannot be reconciled. Making warm-up quotable needs a separate warm-up phase
		// with a reset between, or per-cell warm-up totals carried for the verifier;
		// both belong with the sweeps that need them, and are scheduled there.
		s.NotSoundBecause = fmt.Sprintf("-warm-up discarded %d responses from the "+
			"client totals while their rows remain in the database, which persisted-state "+
			"reconciliation cannot reconcile in PR1 (measurement-contract §12)", s.WarmUpDiscarded)
	default:
		s.Sound = true
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

// percentiles computes the reported quantiles by nearest-rank on the sorted sample.
func percentiles(v []float64) Percentiles {
	if len(v) == 0 {
		return Percentiles{}
	}
	sort.Float64s(v)
	return Percentiles{
		P50: quantile(v, 50),
		P95: quantile(v, 95),
		P99: quantile(v, 99),
		Max: v[len(v)-1],
	}
}

// quantile returns the nearest-rank value for the pth percentile: the smallest observation
// whose rank is at least ⌈p·n/100⌉, one-based, which is index ⌈p·n/100⌉-1 zero-based.
//
// p is an integer percentile rather than a float fraction, and the ceiling is computed in
// integer arithmetic, because the float form is wrong in a way that hides. `int(q*n)`
// truncates, which returns the *next* observation whenever q·n is integral — p95 of 60
// samples becomes the 58th rather than the 57th, and p50 of two becomes the larger value —
// so it overstates every reported percentile at exactly the round sample sizes a sweep
// tends to use. `ceil(q*n)-1` in floating point fixes those cases but only because 0.95 and
// 0.99 happen to be stored slightly below their decimal value; a quantile that landed
// slightly above would shift the rank up again. Integers cannot land either way.
func quantile(sorted []float64, p int) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := min(max((p*n+99)/100, 1), n) // ⌈p·n/100⌉, clamped into [1, n]
	return sorted[rank-1]
}
