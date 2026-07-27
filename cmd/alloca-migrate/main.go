// Command alloca-migrate applies the Alloca-Go database schema.
//
// It is a separate binary because ADR-0002 keeps migration out of the serving path:
// replicas never migrate on startup, so a rollout cannot have several replicas racing
// the same DDL, and a routine restart is never coupled to a schema change. Deployment
// runs this once, to completion, before the new version serves traffic.
//
// Usage:
//
//	alloca-migrate            # apply all pending migrations
//	alloca-migrate -down      # roll back the most recent migration
//
// The database URL comes from DATABASE_URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nancysworld/alloca-go/internal/postgres"
)

func main() {
	down := flag.Bool("down", false, "roll back the most recent migration instead of applying pending ones")
	flag.Parse()

	if err := run(*down); err != nil {
		fmt.Fprintf(os.Stderr, "alloca-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(down bool) error {
	dsn, ok := os.LookupEnv("DATABASE_URL")
	if !ok || dsn == "" {
		return fmt.Errorf("DATABASE_URL must be set")
	}

	// Interrupt cancels the context so a migration in progress is not left half-applied
	// by a hard kill; goose runs each migration in its own transaction, so cancelling
	// between migrations is safe.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if down {
		if err := postgres.MigrateDown(ctx, dsn); err != nil {
			return err
		}
		fmt.Println("alloca-migrate: rolled back one migration")
		return nil
	}
	if err := postgres.Migrate(ctx, dsn); err != nil {
		return err
	}
	fmt.Println("alloca-migrate: schema up to date")
	return nil
}
