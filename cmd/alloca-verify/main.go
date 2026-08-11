// Command alloca-verify reconciles a load run against persisted state.
//
// It reads the report alloca-load wrote and queries PostgreSQL directly, running the four
// checks of measurement-contract §12. It is a separate binary from the generator so the
// generator can run on compute separate from the service without database credentials
// (measurement-contract §13.1); this one runs wherever the database is reachable.
//
// It exits non-zero when the run is not quotable, so a pipeline cannot collect numbers from
// a run whose totals do not reconcile.
//
// Usage, one authority:
//
//	alloca-verify -run run-42.json -database-url "$DATABASE_URL" -org load-org
//
// Usage, several writable authorities (PR3c). The placement document says which
// organisations each authority owns, and one DSN and one scrape are supplied per authority:
//
//	alloca-verify -run topo-run.json \
//	    -placement deploy/topology/placement.json \
//	    -authority-db authority-1="$AUTHORITY_1_URL" \
//	    -authority-db authority-2="$AUTHORITY_2_URL" \
//	    -authority-metrics authority-1=s1.prom \
//	    -authority-metrics authority-2=s2.prom
//
// The organisation set comes from the placement document rather than from the databases,
// deliberately: an authority asked to discover its own scope would silently absorb rows that
// are on the wrong authority, which is the failure placement exists to prevent
// (measurement-contract §12).
//
// A sweep cell keeps its service warm across the measured phase rather than restarting it, so
// its counters do not start at zero. Pass -metrics-baseline alongside -metrics there — or the
// per-authority pair — and the comparison becomes the delta across the measured phase.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

// authorityValues collects repeated `authority=value` flags — one DSN, one scrape path or one
// baseline path per writable authority.
//
// One flag per authority rather than a single comma-separated value, for the reason
// alloca-load routes the same way: this list decides which authorities the verdict covers,
// and a typo inside one long string is easier to make and harder to see than a wrong line.
type authorityValues map[domain.AuthorityID]string

func (a authorityValues) String() string {
	pairs := make([]string, 0, len(a))
	for authority, value := range a {
		pairs = append(pairs, fmt.Sprintf("%s=%s", authority, value))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func (a authorityValues) Set(value string) error {
	authority, v, found := strings.Cut(value, "=")
	if !found {
		return fmt.Errorf("want authority=value, got %q", value)
	}
	authority, v = strings.TrimSpace(authority), strings.TrimSpace(v)
	if authority == "" || v == "" {
		return fmt.Errorf("want authority=value, got %q", value)
	}
	// A repeat is refused rather than overwritten: keeping the last silently would verify one
	// authority against a database or a scrape the operator did not think was in use.
	if existing, repeated := a[domain.AuthorityID(authority)]; repeated {
		return fmt.Errorf("authority %q already has %s", authority, existing)
	}
	a[domain.AuthorityID(authority)] = v
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "alloca-verify:", err)
		os.Exit(1)
	}
}

// run parses its own arguments into a local flag set rather than the package-global one, so
// the multi-authority path can be driven end to end from a test. A verifier whose wiring is
// only ever exercised by hand is one that discovers its own mistakes on the day the evidence
// is being produced.
func run(args []string) error {
	fs := flag.NewFlagSet("alloca-verify", flag.ContinueOnError)

	authorityDBs, authorityMetrics, authorityBaselines :=
		authorityValues{}, authorityValues{}, authorityValues{}
	fs.Var(authorityDBs, "authority-db",
		"authority=DSN for one writable authority; repeat once per authority (requires "+
			"-placement)")
	fs.Var(authorityMetrics, "authority-metrics",
		"authority=path to that unit's /metrics scrape taken after the run and after any "+
			"resolution pass")
	fs.Var(authorityBaselines, "authority-metrics-baseline",
		"authority=path to that unit's /metrics scrape taken before the measured phase")

	var (
		runPath       = fs.String("run", "", "path to the alloca-load JSON report (required)")
		placementPath = fs.String("placement", "",
			"path to the versioned placement document the run was routed by; supplying it "+
				"verifies a multi-authority run, and the organisations each authority owns are "+
				"read from it rather than discovered from the databases")
		dsn          = fs.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL DSN")
		org          = fs.String("org", "load-org", "organisation whose rows the run touched")
		metricsPath  = fs.String("metrics", "", "path to the /metrics scrape taken after the run (required to certify a run)")
		baselinePath = fs.String("metrics-baseline", "",
			"path to a /metrics scrape taken before the measured phase; supply it when the "+
				"service was not restarted immediately before the run, as a warmed sweep cell "+
				"is not")
		timeout = fs.Duration("timeout", 30*time.Second, "overall verification timeout")
		out     = fs.String("out", "", "write the verdict JSON here (default stdout)")
		require = fs.String("require", string(loadgen.LevelLocal),
			"fail unless the run reaches this level: local | capacity | publishable")
	)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		}
		return err
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	if *runPath == "" {
		return fmt.Errorf("-run is required")
	}
	want, err := loadgen.ParseLevel(*require)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(*runPath)
	if err != nil {
		return fmt.Errorf("reading run report: %w", err)
	}
	var report loadgen.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return fmt.Errorf("parsing run report: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var (
		verdict     any
		quotability loadgen.Quotability
	)
	if *placementPath != "" {
		verdict, quotability, err = verifyTopology(ctx, topologyInput{
			Placement: *placementPath,
			DSNs:      authorityDBs,
			Metrics:   authorityMetrics,
			Baselines: authorityBaselines,
			Report:    report,
			Explicit:  explicit,
		})
	} else {
		verdict, quotability, err = verifySingle(ctx, singleInput{
			DSN:      *dsn,
			Org:      domain.OrganisationID(*org),
			Metrics:  *metricsPath,
			Baseline: *baselinePath,
			Report:   report,
			Extra:    authorityDBs,
		})
	}
	if err != nil {
		return err
	}

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("creating verdict file: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(verdict); err != nil {
		return fmt.Errorf("writing verdict: %w", err)
	}

	if !quotability.Level.AtLeast(want) {
		return fmt.Errorf("run reached level %q, below the required %q: %s",
			quotability.Level, want, quotability.BlockedBecause)
	}
	return nil
}

type singleInput struct {
	DSN      string
	Org      domain.OrganisationID
	Metrics  string
	Baseline string
	Report   loadgen.Report
	// Extra carries any -authority-db flags, which have no meaning without -placement.
	Extra authorityValues
}

// verifySingle reconciles a run against one authority: the shape every run before PR3b had,
// and still the right one for a single-authority deployment.
func verifySingle(ctx context.Context, in singleInput) (any, loadgen.Quotability, error) {
	var none loadgen.Quotability
	if len(in.Extra) > 0 {
		return nil, none, fmt.Errorf("-authority-db needs -placement: without a placement " +
			"document there is nothing that says which organisations that authority owns, and " +
			"the run would be verified against -database-url while reporting the authorities " +
			"as though they had been read")
	}
	if in.DSN == "" {
		return nil, none, fmt.Errorf("-database-url or DATABASE_URL is required")
	}

	after, err := readServerTotals(in.Metrics)
	if err != nil {
		return nil, none, err
	}
	baseline, err := readServerTotals(in.Baseline)
	if err != nil {
		return nil, none, err
	}

	pool, err := pgxpool.New(ctx, in.DSN)
	if err != nil {
		return nil, none, fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	result, err := reconcile.Run(ctx, pool, in.Org, in.Report,
		reconcile.Scrapes{Baseline: baseline, After: after})
	if err != nil {
		return nil, none, fmt.Errorf("reconciling: %w", err)
	}
	return result, result.Quotability, nil
}

type topologyInput struct {
	Placement string
	DSNs      authorityValues
	Metrics   authorityValues
	Baselines authorityValues
	Report    loadgen.Report
	Explicit  map[string]bool
}

// verifyTopology reconciles a run that spanned several writable authorities.
//
// The scopes are built from the placement document and then checked against the report's own
// placement assignment by RunTopology, so a verdict cannot describe a partition the run did
// not use — an omitted authority, a duplicated one, or an organisation moved between them.
func verifyTopology(ctx context.Context, in topologyInput) (any, loadgen.Quotability, error) {
	var none loadgen.Quotability

	// The single-authority flags are refused rather than ignored, for the reason -placement
	// and -target are mutually exclusive in the generator: they answer the same question
	// differently, and a verdict computed from the one the operator did not mean would look
	// exactly like a verdict computed from the one they did.
	if in.Explicit["database-url"] || in.Explicit["org"] {
		return nil, none, fmt.Errorf("-placement is mutually exclusive with -database-url and " +
			"-org: a placement document names every authority and the organisations it owns, " +
			"while those two name one database and one organisation")
	}
	if in.Explicit["metrics"] || in.Explicit["metrics-baseline"] {
		return nil, none, fmt.Errorf("-metrics and -metrics-baseline are single-authority " +
			"flags: a multi-authority run has one scrape per unit, supplied as " +
			"-authority-metrics authority=path, because each unit's pair must be differenced " +
			"before the sum is taken (measurement-contract §12)")
	}

	document, err := os.ReadFile(in.Placement)
	if err != nil {
		return nil, none, fmt.Errorf("reading placement document: %w", err)
	}
	placement, err := domain.ParsePlacement(document)
	if err != nil {
		return nil, none, fmt.Errorf("parsing placement document %s: %w", in.Placement, err)
	}
	authorities := placement.Authorities()
	named := map[domain.AuthorityID]bool{}
	for _, authority := range authorities {
		named[authority] = true
	}
	// Both directions, as the generator's router does. A missing DSN would verify a topology
	// with one authority's rows never read — and an unread authority is exactly where an
	// unnoticed write would sit. A DSN for an authority the map never names means the operator
	// and the placement disagree about the topology, and the verdict would cover a database
	// this run never wrote to.
	for _, supplied := range []struct {
		flag   string
		values authorityValues
	}{
		{"-authority-db", in.DSNs},
		{"-authority-metrics", in.Metrics},
		{"-authority-metrics-baseline", in.Baselines},
	} {
		for authority := range supplied.values {
			if !named[authority] {
				return nil, none, fmt.Errorf("%s names authority %q, which routing version %q "+
					"never mentions", supplied.flag, authority, placement.Version())
			}
		}
	}

	scopes := make([]reconcile.AuthorityScope, 0, len(authorities))
	var pools []*pgxpool.Pool
	defer func() {
		for _, pool := range pools {
			pool.Close()
		}
	}()

	for _, authority := range authorities {
		dsn := in.DSNs[authority]
		if dsn == "" {
			return nil, none, fmt.Errorf("routing version %q places organisations on authority "+
				"%q, but no -authority-db was supplied for it; its rows would never be read and "+
				"the run would be certified without them", placement.Version(), authority)
		}

		after, err := readServerTotals(in.Metrics[authority])
		if err != nil {
			return nil, none, fmt.Errorf("authority %q: %w", authority, err)
		}
		baseline, err := readServerTotals(in.Baselines[authority])
		if err != nil {
			return nil, none, fmt.Errorf("authority %q: %w", authority, err)
		}

		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, none, fmt.Errorf("connecting to authority %q: %w", authority, err)
		}
		pools = append(pools, pool)

		scopes = append(scopes, reconcile.AuthorityScope{
			Authority: authority,
			Querier:   pool,
			Orgs:      placement.Organisations(authority),
			Scrapes:   reconcile.Scrapes{Baseline: baseline, After: after},
		})
	}

	result, err := reconcile.RunTopology(ctx, scopes, in.Report)
	if err != nil {
		return nil, none, fmt.Errorf("reconciling: %w", err)
	}
	return result, result.Quotability, nil
}

// readServerTotals loads the metrics scrape, or returns nil when none was given.
//
// Nil is passed through rather than rejected here, so the reason a run cannot be certified
// appears as a failed check inside the verdict JSON alongside the others. Refusing at the
// flag would report the same fact as a usage error and leave no machine-readable record
// that the third count was the one missing.
func readServerTotals(path string) (reconcile.ServerTotals, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading metrics scrape: %w", err)
	}
	defer func() { _ = f.Close() }()

	totals, err := reconcile.ParseServerTotals(f)
	if err != nil {
		return nil, fmt.Errorf("parsing metrics scrape %s: %w", path, err)
	}
	return totals, nil
}
