// Command alloca-load is the external load generator for AG-Sept experiments.
//
// It runs on compute separate from the service and speaks only HTTP, so a run it produces
// can back a publishable capacity claim (ag-sept-plan §6.3). It holds no database
// credentials; reconciling client totals against persisted state is alloca-verify's job.
//
// Every run writes a report combining the §6.4 manifest with the run summary, so a number
// cannot be separated from the conditions that produced it.
//
// Usage, single authority:
//
//	alloca-load -target http://localhost:8080 -workload hot-slot -concurrency 50 -n 500
//
// Usage, several writable authorities (PR3b). The run is routed by the same versioned
// placement document the services enforce, and one endpoint is supplied per authority it
// names:
//
//	alloca-load -placement deploy/topology/placement.json \
//	    -endpoint authority-1=http://localhost:8081 \
//	    -endpoint authority-2=http://localhost:8082 \
//	    -workload multi-org-dispersed -concurrency 50 -n 500
//
// The -validate=false flag disables response validation. It exists only to drive the
// negative control measurement-contract §5.5 requires; a run produced with it is marked
// not quotable in its own summary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// endpointMap collects repeated -endpoint authority=url flags.
//
// One flag per authority rather than a single comma-separated value: an endpoint list is
// the thing that decides which units a run believes it exercised, and a typo inside one
// long string is far easier to make and far harder to see than a wrong line.
type endpointMap map[domain.AuthorityID]string

func (e endpointMap) String() string {
	pairs := make([]string, 0, len(e))
	for authority, url := range e {
		pairs = append(pairs, fmt.Sprintf("%s=%s", authority, url))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func (e endpointMap) Set(value string) error {
	authority, url, found := strings.Cut(value, "=")
	if !found {
		return fmt.Errorf("want authority=url, got %q", value)
	}
	authority, url = strings.TrimSpace(authority), strings.TrimSpace(url)
	if authority == "" || url == "" {
		return fmt.Errorf("want authority=url, got %q", value)
	}
	// A repeat is refused rather than overwritten. Two -endpoint flags for one authority
	// means the operator believes both are being exercised, and silently keeping the last
	// would produce a run that reached one of them and said nothing about it.
	if existing, repeated := e[domain.AuthorityID(authority)]; repeated {
		return fmt.Errorf("authority %q already has endpoint %s", authority, existing)
	}
	e[domain.AuthorityID(authority)] = url
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "alloca-load:", err)
		os.Exit(1)
	}
}

// run parses its own arguments into a local flag set rather than the package-global one, so
// the whole path — routing, workload construction, the run, both /meta reads and the report —
// can be driven end to end from a test. A multi-authority path that is only ever exercised by
// hand is one whose wiring nothing checks.
func run(args []string) error {
	fs := flag.NewFlagSet("alloca-load", flag.ContinueOnError)

	endpoints := endpointMap{}
	fs.Var(endpoints, "endpoint",
		"authority=url for one unit of a multi-authority topology; repeat once per authority "+
			"(requires -placement)")

	var (
		target    = fs.String("target", "http://localhost:8080", "service base URL")
		placement = fs.String("placement", "",
			"path to the versioned placement document routing this run; without it the run is "+
				"single-authority and everything goes to -target")
		workloadName = fs.String("workload", "dispersed",
			"dispersed | hot-slot | hot-identity | replay | multi-org-dispersed | "+
				"hot-organisation | cross-authority-control")
		concurrency = fs.Int("concurrency", 10, "concurrent workers (closed loop)")
		iterations  = fs.Int("n", 100, "logical units of work (mutually exclusive with -duration)")
		duration    = fs.Duration("duration", 0,
			"run for this long instead of a fixed -n; required for sweep cells, whose rates "+
				"are only comparable when every cell covers the same interval")
		warmUp   = fs.Duration("warm-up", 0, "discard responses completing inside this window")
		timeout  = fs.Duration("timeout", 10*time.Second, "per-request client timeout")
		validate = fs.Bool("validate", true, "validate responses; false drives the §5.5 control")
		org      = fs.String("org", "load-org", "organisation for generated identities")
		slots    = fs.Int("slots", 100,
			"slots seeded per organisation (dispersed, hot-identity, and the multi-organisation "+
				"shapes, which seed this many for every organisation the placement names)")
		slotID   = fs.String("slot", "slot-0", "the contended slot (hot-slot)")
		userID   = fs.String("user", "user-0", "the contended identity (hot-identity)")
		location = fs.String("generator-location", "local", "where the generator runs")
		out      = fs.String("out", "", "write the JSON report here (default stdout)")
		confirm  = fs.Bool("confirm", false, "dispersed: drive reserve→confirm")
		require  = fs.String("require", string(loadgen.LevelLocal),
			"fail unless the run reaches this level: local | capacity | publishable")
	)
	// The flag package's own output is discarded so a bad flag is reported once, by main,
	// rather than twice with a usage dump wedged between the two copies. -h is not an error:
	// it prints usage on stdout and exits zero, which is what a caller piping it expects.
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}

	if *concurrency < 1 {
		return fmt.Errorf("-concurrency must be at least 1")
	}

	// Both bounds set is rejected rather than resolved by precedence. -n has a default, so
	// "was it set?" cannot be answered from its value — Visit is the only way to tell an
	// explicit -n from the default, and a run bounded by the one the operator did not mean
	// measures the wrong thing while looking entirely normal.
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
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

	// Routing is settled before anything else, because it decides both which workloads can be
	// built and which units the run has to read. NewRouter refuses an incomplete topology in
	// both directions — an authority with no endpoint, and an endpoint nothing routes to — so
	// a misconfigured topology fails here rather than at some request deep in the run.
	router, rerr := buildRouter(*placement, *target, endpoints, explicit["target"])
	if rerr != nil {
		return rerr
	}

	workload, datasetSlots, werr := buildWorkload(*workloadName, workloadSpec{
		Router:  router,
		Org:     domain.OrganisationID(*org),
		SlotID:  domain.SlotID(*slotID),
		UserID:  domain.UserID(*userID),
		Slots:   *slots,
		Confirm: *confirm,
	})
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
	//
	// Every unit is read, not just one. On a multi-authority run the units' agreement is
	// itself a certification input: nothing in the request totals would reveal two units on
	// different commits, or serving different placements, and a run against those describes
	// two services averaged together.
	targets := router.Targets()
	before, metaErr := loadgen.FetchTopologyMeta(ctx, targets, *timeout)
	if metaErr != nil {
		fmt.Fprintln(os.Stderr, "alloca-load: could not read /meta from every unit:", metaErr)
	}

	client := loadgen.NewRoutedClient(router, *timeout, *validate)
	summary := loadgen.NewRunner(client, opts).Run(ctx, workload)

	// Read /meta again and compare. A pre-run read establishes only "the service behind the
	// target when the run began" (DEBT-3); this is what turns that into a claim about the
	// whole sample. A restart, a rolling replacement or a config change mid-run all leave the
	// totals internally consistent while describing something other than one experiment.
	//
	// A failed post-run read is itself drift: the service that answered the workload is not
	// answering now, and a run that cannot confirm what it measured must not certify itself.
	after, afterErr := loadgen.FetchTopologyMeta(ctx, targets, *timeout)
	drift := ""
	switch {
	case metaErr != nil:
		// The pre-run read already failed; the manifest has no identity to compare against
		// and the gate refuses on the empty fields rather than on drift.
	case afterErr != nil:
		drift = "not every unit answered /meta after the run: " + afterErr.Error()
	default:
		drift = after.DriftFrom(before)
	}

	manifest := loadgen.NewTopologyManifest(targets, workload.Name(), opts, *location, before)
	manifest.DatasetSlots = datasetSlots
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

// buildRouter settles how the run reaches the service: one target, or one endpoint per
// authority named by a versioned placement document.
//
// -placement and an explicit -target are refused together rather than resolved by
// precedence. They answer the same question differently, and a run that silently ignored one
// of them would route by a map the operator did not think was in force — which is the one
// mistake the §12.5 misrouting control exists to make visible, arriving instead as a wall of
// refusals that look like a service defect.
func buildRouter(placementPath, target string, endpoints endpointMap, explicitTarget bool) (loadgen.Router, error) {
	if placementPath == "" {
		if len(endpoints) > 0 {
			return loadgen.Router{}, fmt.Errorf("-endpoint needs -placement: without a placement " +
				"document there is no routing to attach an endpoint to, and the run would send " +
				"everything to -target while reporting the endpoints as though it had used them")
		}
		return loadgen.SingleTarget(target), nil
	}

	if explicitTarget {
		return loadgen.Router{}, fmt.Errorf("-placement and -target are mutually exclusive: " +
			"-placement routes each organisation to its own authority's endpoint, and -target " +
			"names one service for everything")
	}

	document, err := os.ReadFile(placementPath)
	if err != nil {
		return loadgen.Router{}, fmt.Errorf("reading placement document: %w", err)
	}
	parsed, err := domain.ParsePlacement(document)
	if err != nil {
		return loadgen.Router{}, fmt.Errorf("parsing placement document %s: %w", placementPath, err)
	}
	return loadgen.NewRouter(parsed, endpoints)
}

// workloadSpec is what the shapes are built from: the routing in force, and the dataset
// parameters the single-organisation shapes have always taken.
type workloadSpec struct {
	Router  loadgen.Router
	Org     domain.OrganisationID
	SlotID  domain.SlotID
	UserID  domain.UserID
	Slots   int
	Confirm bool
}

// slotsFor generates the seeded slot references of one organisation.
func slotsFor(org domain.OrganisationID, slots int) []loadgen.Slot {
	dataset := make([]loadgen.Slot, slots)
	for i := range dataset {
		dataset[i] = loadgen.Slot{
			OrganisationID: org,
			SlotID:         domain.SlotID(fmt.Sprintf("slot-%d", i)),
		}
	}
	return dataset
}

// buildWorkload constructs the named shape. Each is one mechanism from §5; there is
// deliberately no "all" mode, because a composite changes several variables at once and is
// harder to attribute (§3.1).
//
// The multi-organisation shapes (§5.6) need the placement map, because what makes a pair of
// organisations supported is whether they share an authority. They are refused on a
// single-target run rather than quietly degraded to the single-organisation case: a run that
// reported "multi-org-dispersed" while exercising one authority would name a control it
// never drove.
// It also reports the size of the dataset the shape actually draws from, which is not -slots
// for the multi-organisation shapes: those seed -slots for *every* organisation the routing
// places, and a manifest recording the per-organisation figure would understate the
// contention the run was exposed to by exactly the number of authorities.
func buildWorkload(name string, spec workloadSpec) (loadgen.Workload, int, error) {
	dataset := slotsFor(spec.Org, spec.Slots)

	switch strings.ToLower(name) {
	case "dispersed":
		return loadgen.Dispersed{Org: spec.Org, Slots: dataset, Confirm: spec.Confirm}, len(dataset), nil
	case "hot-slot":
		return loadgen.HotSlot{
			Org:  spec.Org,
			Slot: loadgen.Slot{OrganisationID: spec.Org, SlotID: spec.SlotID},
		}, 1, nil
	case "hot-identity":
		return loadgen.HotIdentity{
			User:  loadgen.User{OrganisationID: spec.Org, UserID: spec.UserID},
			Slots: dataset,
		}, len(dataset), nil
	case "replay":
		// The disposition control (measurement-contract §4.2). It issues two requests per
		// logical unit, so -n counts logical units here as everywhere: a run of -n 60 sends
		// 120 requests and expects 60 of them to be replays.
		return loadgen.Replay{Org: spec.Org, Slots: dataset}, len(dataset), nil

	case "multi-org-dispersed":
		groups, size, err := orgGroups(spec)
		if err != nil {
			return nil, 0, err
		}
		return loadgen.MultiOrgDispersed{Groups: groups, Confirm: spec.Confirm}, size, nil
	case "hot-organisation":
		// -org rather than a group, because the point of this shape is that *one* named
		// organisation carries the load while its peers carry none.
		if _, err := spec.Router.For(spec.Org); err != nil {
			return nil, 0, fmt.Errorf("hot-organisation targets -org %q: %w", spec.Org, err)
		}
		return loadgen.HotOrganisation{Org: spec.Org, Slots: dataset}, len(dataset), nil
	case "cross-authority-control":
		groups, size, err := orgGroups(spec)
		if err != nil {
			return nil, 0, err
		}
		return loadgen.CrossAuthorityControl{Groups: groups}, size, nil

	default:
		return nil, 0, fmt.Errorf("unknown workload %q: want dispersed, hot-slot, hot-identity, "+
			"replay, multi-org-dispersed, hot-organisation or cross-authority-control", name)
	}
}

// orgGroups derives the per-authority groups the §5.6 shapes draw from, seeding the same
// number of slots for every organisation the routing places.
func orgGroups(spec workloadSpec) ([]loadgen.OrgGroup, int, error) {
	placement := spec.Router.Placement()
	if placement.IsZero() {
		return nil, 0, fmt.Errorf("the multi-organisation workloads need -placement: what makes a " +
			"pair of organisations supported is whether they share an authority, and a " +
			"single-target run has no map to answer that from")
	}

	slotsByOrg := map[domain.OrganisationID][]loadgen.Slot{}
	dataset := 0
	for _, authority := range placement.Authorities() {
		for _, org := range placement.Organisations(authority) {
			slotsByOrg[org] = slotsFor(org, spec.Slots)
			dataset += spec.Slots
		}
	}

	groups, err := loadgen.NewOrgGroups(placement, slotsByOrg)
	if err != nil {
		return nil, 0, err
	}
	return groups, dataset, nil
}
