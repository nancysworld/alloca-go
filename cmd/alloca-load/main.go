// Command alloca-load is the external load generator for AG-Sept experiments.
//
// It runs on compute separate from the service and speaks only HTTP, so a run it produces
// can back a publishable capacity claim (ag-sept-plan §6.3). It holds no database
// credentials; reconciling client totals against persisted state is alloca-verify's job.
//
// Every run writes a report combining the §6.4 manifest with the run summary, so a number
// cannot be separated from the conditions that produced it.
//
// Usage:
//
//	alloca-load -target http://localhost:8080 -workload hot-slot -concurrency 50 -n 500
//
// The -validate=false flag disables response validation. It exists only to drive the
// negative control measurement-contract §5.5 requires; a run produced with it is marked
// not quotable in its own summary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "alloca-load:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		target       = flag.String("target", "http://localhost:8080", "service base URL")
		workloadName = flag.String("workload", "dispersed", "dispersed | hot-slot | hot-identity | replay")
		concurrency  = flag.Int("concurrency", 10, "concurrent workers (closed loop)")
		iterations   = flag.Int("n", 100, "logical units of work (mutually exclusive with -duration)")
		duration     = flag.Duration("duration", 0,
			"run for this long instead of a fixed -n; required for sweep cells, whose rates "+
				"are only comparable when every cell covers the same interval")
		warmUp   = flag.Duration("warm-up", 0, "discard responses completing inside this window")
		timeout  = flag.Duration("timeout", 10*time.Second, "per-request client timeout")
		validate = flag.Bool("validate", true, "validate responses; false drives the §5.5 control")
		org      = flag.String("org", "load-org", "organisation for generated identities")
		slots    = flag.Int("slots", 100, "slots in the dataset (dispersed, hot-identity)")
		slotID   = flag.String("slot", "slot-0", "the contended slot (hot-slot)")
		userID   = flag.String("user", "user-0", "the contended identity (hot-identity)")
		location = flag.String("generator-location", "local", "where the generator runs")
		out      = flag.String("out", "", "write the JSON report here (default stdout)")
		confirm  = flag.Bool("confirm", false, "dispersed: drive reserve→confirm")
		require  = flag.String("require", string(loadgen.LevelLocal),
			"fail unless the run reaches this level: local | capacity | publishable")
	)
	flag.Parse()

	if *concurrency < 1 {
		return fmt.Errorf("-concurrency must be at least 1")
	}

	// Both bounds set is rejected rather than resolved by precedence. -n has a default, so
	// "was it set?" cannot be answered from its value — flag.Visit is the only way to tell an
	// explicit -n from the default, and a run bounded by the one the operator did not mean
	// measures the wrong thing while looking entirely normal.
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	switch {
	case explicit["n"] && explicit["duration"]:
		return fmt.Errorf("-n and -duration are mutually exclusive: -n bounds the run by " +
			"logical units, -duration by wall clock, and a run cannot be bounded by both")
	case *duration < 0:
		return fmt.Errorf("-duration must not be negative")
	case *duration == 0 && *iterations < 1:
		return fmt.Errorf("-n must be at least 1")
	}
	want, err := loadgen.ParseLevel(*require)
	if err != nil {
		return err
	}

	workload, werr := buildWorkload(*workloadName, *org, *slotID, *userID, *slots, *confirm)
	if werr != nil {
		return werr
	}

	// A run is interruptible and still reports: a truncated run that says what it did
	// beats one that says nothing, and the summary records the iterations it actually
	// completed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts := loadgen.Options{
		Concurrency: *concurrency,
		Iterations:  *iterations,
		Duration:    *duration,
		WarmUp:      *warmUp,
	}
	if *duration > 0 {
		// Clear the unused bound so nothing downstream reads -n's default as a request.
		opts.Iterations = 0
	}
	// Read the service's own provenance before driving load. It identifies the binary that
	// is about to answer the requests, which is what §6.4 means by "commit SHA" — the
	// generator's own revision answers a different question and is recorded separately.
	//
	// A failure here does not stop the run. The manifest keeps an empty service identity, the
	// quotability gate refuses it with a reason, and the operator gets a report explaining
	// what could not be established. Aborting would leave no artifact at all.
	svc, metaErr := loadgen.FetchServiceMeta(ctx, *target, *timeout)
	if metaErr != nil {
		fmt.Fprintln(os.Stderr, "alloca-load: could not read service /meta:", metaErr)
	}

	client := loadgen.NewClient(*target, *timeout, *validate)
	summary := loadgen.NewRunner(client, opts).Run(ctx, workload)

	// Read /meta again and compare. A pre-run read establishes only "the service behind the
	// target when the run began" (DEBT-3); this is what turns that into a claim about the
	// whole sample. A restart, a rolling replacement or a config change mid-run all leave the
	// totals internally consistent while describing something other than one experiment.
	//
	// A failed post-run read is itself drift: the service that answered the workload is not
	// answering now, and a run that cannot confirm what it measured must not certify itself.
	after, afterErr := loadgen.FetchServiceMeta(ctx, *target, *timeout)
	drift := ""
	switch {
	case metaErr != nil:
		// The pre-run read already failed; the manifest has no identity to compare against
		// and the gate refuses on the empty fields rather than on drift.
	case afterErr != nil:
		drift = "the service did not answer /meta after the run: " + afterErr.Error()
	default:
		drift = after.DriftFrom(svc)
	}

	manifest := loadgen.NewManifest(*target, workload.Name(), opts, *location, svc)
	manifest.DatasetSlots = *slots
	manifest.ServiceIdentityDrift = drift

	report := loadgen.Report{Manifest: manifest, Summary: summary}
	report.Quotability = loadgen.Certify(manifest, summary)

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("creating report: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	if err := loadgen.WriteReport(w, report); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	// A run that did not reach the level the operator asked for exits non-zero, so a script
	// cannot collect numbers from it and carry on. The report is still written — the
	// operator needs to see why.
	//
	// The bar is declared at the call site rather than assumed here, because what a run must
	// satisfy depends on what its numbers are for: PR1's smoke runs want -require local,
	// and the scripts behind a published claim want -require publishable. A harness that
	// picked for them would be guessing at the claim.
	if q := report.Quotability; !q.Level.AtLeast(want) {
		return fmt.Errorf("run reached level %q, below the required %q: %s",
			q.Level, want, q.BlockedBecause)
	}
	return nil
}

// buildWorkload constructs the named shape. Each is one mechanism from §5; there is
// deliberately no "all" mode, because a composite changes several variables at once and is
// harder to attribute (§3.1).
func buildWorkload(name, org, slotID, userID string, slots int, confirm bool) (loadgen.Workload, error) {
	orgID := domain.OrganisationID(org)

	dataset := make([]loadgen.Slot, slots)
	for i := range dataset {
		dataset[i] = loadgen.Slot{
			OrganisationID: orgID,
			SlotID:         domain.SlotID(fmt.Sprintf("slot-%d", i)),
		}
	}

	switch strings.ToLower(name) {
	case "dispersed":
		return loadgen.Dispersed{Org: orgID, Slots: dataset, Confirm: confirm}, nil
	case "hot-slot":
		return loadgen.HotSlot{
			Org:  orgID,
			Slot: loadgen.Slot{OrganisationID: orgID, SlotID: domain.SlotID(slotID)},
		}, nil
	case "hot-identity":
		return loadgen.HotIdentity{
			User:  loadgen.User{OrganisationID: orgID, UserID: domain.UserID(userID)},
			Slots: dataset,
		}, nil
	case "replay":
		// The disposition control (measurement-contract §4.2). It issues two requests per
		// logical unit, so -n counts logical units here as everywhere: a run of -n 60 sends
		// 120 requests and expects 60 of them to be replays.
		return loadgen.Replay{Org: orgID, Slots: dataset}, nil
	default:
		return nil, fmt.Errorf("unknown workload %q: want dispersed, hot-slot, hot-identity or replay", name)
	}
}
