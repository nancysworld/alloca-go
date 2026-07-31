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
		workloadName = flag.String("workload", "dispersed", "dispersed | hot-slot | hot-identity")
		concurrency  = flag.Int("concurrency", 10, "concurrent workers (closed loop)")
		iterations   = flag.Int("n", 100, "logical units of work")
		warmUp       = flag.Duration("warm-up", 0, "discard responses completing inside this window")
		timeout      = flag.Duration("timeout", 10*time.Second, "per-request client timeout")
		validate     = flag.Bool("validate", true, "validate responses; false drives the §5.5 control")
		org          = flag.String("org", "load-org", "organisation for generated identities")
		slots        = flag.Int("slots", 100, "slots in the dataset (dispersed, hot-identity)")
		slotID       = flag.String("slot", "slot-0", "the contended slot (hot-slot)")
		userID       = flag.String("user", "user-0", "the contended identity (hot-identity)")
		location     = flag.String("generator-location", "local", "where the generator runs")
		out          = flag.String("out", "", "write the JSON report here (default stdout)")
		confirm      = flag.Bool("confirm", false, "dispersed: drive reserve→confirm")
	)
	flag.Parse()

	if *concurrency < 1 || *iterations < 1 {
		return fmt.Errorf("concurrency and -n must both be at least 1")
	}

	workload, err := buildWorkload(*workloadName, *org, *slotID, *userID, *slots, *confirm)
	if err != nil {
		return err
	}

	// A run is interruptible and still reports: a truncated run that says what it did
	// beats one that says nothing, and the summary records the iterations it actually
	// completed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts := loadgen.Options{
		Concurrency: *concurrency,
		Iterations:  *iterations,
		WarmUp:      *warmUp,
	}
	client := loadgen.NewClient(*target, *timeout, *validate)
	summary := loadgen.NewRunner(client, opts).Run(ctx, workload)

	manifest := loadgen.NewManifest(*target, workload.Name(), opts, *location)
	manifest.DatasetSlots = *slots

	report := loadgen.Report{Manifest: manifest, Summary: summary}

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

	// A non-quotable run exits non-zero so a script cannot collect numbers from it and
	// carry on. The report is still written — the operator needs to see why.
	if !summary.Quotable {
		return fmt.Errorf("run is not quotable: %s", summary.NotQuotableBecause)
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
	default:
		return nil, fmt.Errorf("unknown workload %q: want dispersed, hot-slot or hot-identity", name)
	}
}
