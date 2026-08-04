// Command alloca-verify reconciles a load run against persisted state.
//
// It reads the report alloca-load wrote and queries PostgreSQL directly, running the four
// checks of ag-sept-plan §6.5. It is a separate binary from the generator so the generator
// can run on compute separate from the service without database credentials (§6.3); this
// one runs wherever the database is reachable.
//
// It exits non-zero when the run is not quotable, so a pipeline cannot collect numbers from
// a run whose totals do not reconcile.
//
// Usage:
//
//	alloca-verify -run run-42.json -database-url "$DATABASE_URL" -org load-org
//
// A sweep cell keeps its service warm across the measured phase rather than restarting it, so
// its counters do not start at zero. Pass -metrics-baseline alongside -metrics there and the
// comparison becomes the delta across the measured phase.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/reconcile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "alloca-verify:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		runPath      = flag.String("run", "", "path to the alloca-load JSON report (required)")
		dsn          = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL DSN")
		org          = flag.String("org", "load-org", "organisation whose rows the run touched")
		metricsPath  = flag.String("metrics", "", "path to the /metrics scrape taken after the run (required to certify a run)")
		baselinePath = flag.String("metrics-baseline", "",
			"path to a /metrics scrape taken before the measured phase; supply it when the "+
				"service was not restarted immediately before the run, as a warmed sweep cell "+
				"is not")
		timeout = flag.Duration("timeout", 30*time.Second, "overall verification timeout")
		out     = flag.String("out", "", "write the verdict JSON here (default stdout)")
		require = flag.String("require", string(loadgen.LevelLocal),
			"fail unless the run reaches this level: local | capacity | publishable")
	)
	flag.Parse()

	if *runPath == "" {
		return fmt.Errorf("-run is required")
	}
	want, err := loadgen.ParseLevel(*require)
	if err != nil {
		return err
	}
	if *dsn == "" {
		return fmt.Errorf("-database-url or DATABASE_URL is required")
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

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	after, err := readServerTotals(*metricsPath)
	if err != nil {
		return err
	}
	baseline, err := readServerTotals(*baselinePath)
	if err != nil {
		return err
	}

	result, err := reconcile.Run(ctx, pool, domain.OrganisationID(*org), report,
		reconcile.Scrapes{Baseline: baseline, After: after})
	if err != nil {
		return fmt.Errorf("reconciling: %w", err)
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
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("writing verdict: %w", err)
	}

	if q := result.Quotability; !q.Level.AtLeast(want) {
		return fmt.Errorf("run reached level %q, below the required %q: %s",
			q.Level, want, q.BlockedBecause)
	}
	return nil
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
