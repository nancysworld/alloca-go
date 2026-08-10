// Command alloca-seed prepares a deterministic fixture for one AG-Sept run, and asserts
// the clean start ag-sept-validation-plan.md §3.3 requires.
//
// The assertion is the part that matters. A confirmed claim has expires_at IS NULL and is
// never reaped by expiry, so a reserve→confirm workload leaves permanent rows. Rerunning
// the hot-identity control against those rows yields zero admitted and all
// schedule_conflict — a result that violates no invariant, passes every correctness gate,
// and means nothing. §5.3 therefore requires every run to begin from a deterministic reset
// or fresh seed and to assert zero live claims before load starts, and this is where that
// happens: before the generator sends a request, not afterwards when the damage is a
// plausible-looking number.
//
// It holds database credentials, which is why it is a separate binary from the generator
// (§6.3). It is a fixture tool for experiments and is never part of the serving path.
//
// Usage:
//
//	alloca-seed -database-url "$DATABASE_URL" -slots 100 -capacity 20 -reset
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "alloca-seed:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dsn      = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL DSN")
		org      = flag.String("org", "load-org", "organisation owning the seeded slots")
		slots    = flag.Int("slots", 100, "slots to create")
		capacity = flag.Int("capacity", 20, "capacity per slot")
		startsIn = flag.Duration("starts-in", 24*time.Hour, "how far ahead the slot window opens")
		window   = flag.Duration("window", time.Hour, "slot window length")
		reset    = flag.Bool("reset", false, "truncate booking state before seeding")
		timeout  = flag.Duration("timeout", 60*time.Second, "overall timeout")
	)
	flag.Parse()

	if *dsn == "" {
		return fmt.Errorf("-database-url or DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	budget := config.RequestBudget{
		ClientDeadline: 60 * time.Second, ServerDeadline: 50 * time.Second,
		AdmissionCap: time.Second, DBAcquireCap: 10 * time.Second,
		LockTimeout: 15 * time.Second, StatementTimeout: 30 * time.Second,
		TxnBudget: 40 * time.Second,
	}
	pool, err := postgres.OpenPool(ctx, *dsn, budget)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()
	repo := postgres.New(pool, budget)

	if *reset {
		if err := repo.Truncate(ctx); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
	}

	// The window is anchored to the database clock, never the host's: the service decides
	// release and expiry from PostgreSQL time (transaction-semantics §1.5), and a fixture
	// built from a drifting host clock would put the boundary somewhere the service does
	// not agree with.
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return fmt.Errorf("reading database clock: %w", err)
	}

	for i := range *slots {
		slot := domain.Slot{
			ID:             domain.SlotID(fmt.Sprintf("slot-%d", i)),
			OrganisationID: domain.OrganisationID(*org),
			ResourceID:     "resource-1",
			Capacity:       *capacity,
			ReleaseAt:      now.Add(-time.Hour),
			StartsAt:       now.Add(*startsIn),
			EndsAt:         now.Add(*startsIn + *window),
		}
		if err := repo.SeedSlot(ctx, slot); err != nil {
			return fmt.Errorf("seeding %s: %w", slot.ID, err)
		}
	}

	// The clean-start assertion. It runs after seeding rather than before, so it covers a
	// -reset that silently did nothing as well as a fixture inherited from an earlier run.
	claims, err := repo.ClaimCount(ctx)
	if err != nil {
		return fmt.Errorf("counting claims: %w", err)
	}
	if claims != 0 {
		return fmt.Errorf(
			"clean-start assertion failed: %d live claims already exist "+
				"(ag-sept-validation-plan.md §3.3). "+
				"A confirmed claim is never expiry-reaped, so a rerun against these rows "+
				"would report zero admitted and all schedule_conflict — a plausible result "+
				"that measures nothing. Re-run with -reset", claims)
	}

	// The second half of the same assertion, and the half that zero live claims does not
	// imply. Idempotency records outlive the entities they describe — that is their job —
	// so a fixture can hold no claims at all and still turn the next run into a replay of
	// the last one, because the workloads derive their keys from workload name and
	// sequence number with no per-run nonce. Such a run commits nothing, reconciles
	// cleanly (a replay creates no row and the reconciler correctly excludes it from fresh
	// counts), breaks no invariant, and measures nothing. It is the §5.3 trap wearing the
	// costume the claim count cannot see through.
	records, err := repo.IdempotencyRecordCount(ctx)
	if err != nil {
		return fmt.Errorf("counting idempotency records: %w", err)
	}
	if records != 0 {
		return fmt.Errorf(
			"clean-start assertion failed: %d idempotency records already exist "+
				"(ag-sept-validation-plan.md §3.3). The workloads reuse deterministic keys, so the next "+
				"run would replay these records rather than commit anything: it would "+
				"report goodput, reconcile cleanly and move no rows. Re-run with -reset",
			records)
	}

	fmt.Printf("seeded %d slots (capacity %d) for org %q; clean start asserted: "+
		"0 live claims, 0 idempotency records\n", *slots, *capacity, *org)
	return nil
}
