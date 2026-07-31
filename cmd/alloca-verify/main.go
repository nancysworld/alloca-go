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
		runPath = flag.String("run", "", "path to the alloca-load JSON report (required)")
		dsn     = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL DSN")
		org     = flag.String("org", "load-org", "organisation whose rows the run touched")
		timeout = flag.Duration("timeout", 30*time.Second, "overall verification timeout")
		out     = flag.String("out", "", "write the verdict JSON here (default stdout)")
	)
	flag.Parse()

	if *runPath == "" {
		return fmt.Errorf("-run is required")
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

	result, err := reconcile.Run(ctx, pool, domain.OrganisationID(*org), report.Summary)
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

	if !result.Quotable {
		return fmt.Errorf("run is not quotable: %s", result.Because)
	}
	return nil
}
