//go:build integration

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// schemaTables is every table the migrations create, in no particular order. Kept in one
// place so a test asserting the schema exists and a test asserting it was fully removed
// cannot disagree about what "the schema" is.
var schemaTables = []string{"slots", "reservations", "bookings", "idempotency_records", "user_time_claims"}

// Migrations are applied once by TestMain, so this asserts the properties that make
// the schema trustworthy rather than re-running Up.
//
// Re-applying is checked because a rollout runs alloca-migrate on every deploy,
// including deploys that change no schema: if Up were not idempotent, an ordinary
// release would fail at the migration step.
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("re-applying migrations: %v", err)
	}

	repo := newRepo(t, testBudget())
	for _, table := range schemaTables {
		if !tableExists(t, repo, table) {
			t.Errorf("table %s does not exist after migration", table)
		}
	}
}

// Every migration's Down block rolls the schema back, and Up then rebuilds it.
//
// An untested rollback is not a rollback: a Down block that has never run is a guess
// about what Up did, and the first time it matters is a failed deploy under time
// pressure. This rolls the whole schema back to nothing and rebuilds it, so each Down
// block is executed against the state its own Up produced — including the ordering
// constraint that user_time_claims must go before the reservations and slots it
// references.
//
// The rollback count comes from the embedded migration files rather than a literal, so
// adding a migration extends this test automatically instead of silently leaving the new
// Down block unexercised.
func TestMigrationsRollBackAndReapply(t *testing.T) {
	ctx := context.Background()

	// Every other test in the package expects a migrated database, and this one is
	// deliberately destructive. Restoring in Cleanup rather than at the end of the body
	// means an assertion failure below cannot cascade into unrelated failures.
	t.Cleanup(func() {
		if err := Migrate(ctx, databaseURL); err != nil {
			t.Errorf("restore schema after rollback test: %v", err)
		}
	})

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	repo := newRepo(t, testBudget())

	for i := range entries {
		if err := MigrateDown(ctx, databaseURL); err != nil {
			t.Fatalf("roll back migration %d of %d: %v", i+1, len(entries), err)
		}
	}
	for _, table := range schemaTables {
		if tableExists(t, repo, table) {
			t.Errorf("table %s survived a full rollback: the Down block does not undo its Up", table)
		}
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("re-apply migrations after rollback: %v", err)
	}
	for _, table := range schemaTables {
		if !tableExists(t, repo, table) {
			t.Errorf("table %s missing after re-applying migrations", table)
		}
	}

	// The rebuilt schema must carry its constraints, not merely its tables. The
	// exclusion constraint is the whole user schedule authority (§2.2), and it depends on
	// an extension that a rollback dropped — so a Down/Up cycle that restored the table
	// without it would leave the invariant silently unenforced.
	var constraint string
	err = repo.pool.QueryRow(ctx,
		`SELECT conname FROM pg_constraint WHERE conname = 'user_time_claims_no_overlap'`).Scan(&constraint)
	if err != nil {
		t.Errorf("exclusion constraint missing after rollback and re-apply: %v", err)
	}
}

// Adding the schedule authority to a database that already holds bookings must bring
// those bookings under it.
//
// A claim table that started empty would exempt every pre-existing reservation: the
// overlap this migration forbids would stay admissible for them, and confirming an
// existing hold would find no claim to update — which the service treats as an invariant
// breach, not a domain answer. So the migration backfills, and this rolls the claims
// migration back, seeds the world an upgrade would find, and re-applies it.
func TestClaimBackfillCoversPreExistingReservations(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)

	// A slot far enough ahead that the holds below are unambiguously live or elapsed.
	slot := h.seedWindow(t, testOrg, "backfill-slot", 5, base, time.Hour, 2*time.Hour)

	// Roll back only the claims migration, then restore it however this test exits.
	if err := MigrateDown(ctx, databaseURL); err != nil {
		t.Fatalf("roll back claims migration: %v", err)
	}
	t.Cleanup(func() {
		if err := Migrate(ctx, databaseURL); err != nil {
			t.Errorf("restore schema: %v", err)
		}
	})

	// The world an upgrade finds: a live hold, a confirmed booking, an elapsed hold, and
	// a cancelled reservation. Only the first two are active claims (§1).
	seed := func(id, state string, expiresIn time.Duration) {
		t.Helper()
		_, err := h.repo.pool.Exec(ctx, `
			INSERT INTO reservations (reservation_id, slot_organisation_id, slot_id,
				user_organisation_id, user_id, state, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, string(slot.OrganisationID), string(slot.ID), string(testOrg), "user-"+id,
			state, base.Add(-time.Hour), base.Add(expiresIn))
		if err != nil {
			t.Fatalf("seed reservation %s: %v", id, err)
		}
	}
	seed("live-hold", "held", 30*time.Minute)
	seed("elapsed-hold", "held", -time.Minute)
	seed("cancelled", "cancelled", 30*time.Minute)
	seed("confirmed", "confirmed", 30*time.Minute)
	if _, err := h.repo.pool.Exec(ctx, `
		INSERT INTO bookings (booking_id, reservation_id, slot_organisation_id, slot_id,
			user_organisation_id, user_id, state, created_at)
		VALUES ('bk-1', 'confirmed', $1, $2, $3, 'user-confirmed', 'active', $4)`,
		string(slot.OrganisationID), string(slot.ID), string(testOrg), base); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("re-apply claims migration over existing data: %v", err)
	}

	// A confirmed claim is permanent, so its expiry is NULL; a hold's is not.
	rows, err := h.repo.pool.Query(ctx,
		`SELECT reservation_id, expires_at IS NULL FROM user_time_claims ORDER BY reservation_id`)
	if err != nil {
		t.Fatalf("read backfilled claims: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var id string
		var permanent bool
		if err := rows.Scan(&id, &permanent); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = permanent
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read backfilled claims: %v", err)
	}

	want := map[string]bool{"live-hold": false, "confirmed": true}
	if len(got) != len(want) {
		t.Fatalf("backfilled claims = %v, want %v", got, want)
	}
	for id, wantPermanent := range want {
		permanent, ok := got[id]
		if !ok {
			t.Errorf("no claim backfilled for %q: it would be exempt from the invariant", id)
			continue
		}
		if permanent != wantPermanent {
			t.Errorf("claim %q permanent = %t, want %t", id, permanent, wantPermanent)
		}
	}
}

// Pre-existing overlapping bookings must stop the migration rather than be skipped.
//
// Admitting such overlaps is the bug this migration fixes, so an upgraded database may
// well contain them. Backfilling only the non-conflicting rows would leave those users
// unprotected while the schema claimed otherwise — so the constraint is added last and
// is allowed to fail.
func TestClaimBackfillRefusesPreExistingOverlaps(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, testBudget(), 30*time.Second)
	base := h.dbNow(t)

	// Two slots with overlapping windows, and one user holding both.
	a := h.seedWindow(t, testOrg, "overlap-a", 5, base, time.Hour, 2*time.Hour)
	b := h.seedWindow(t, testOrg, "overlap-b", 5, base, 90*time.Minute, 150*time.Minute)

	if err := MigrateDown(ctx, databaseURL); err != nil {
		t.Fatalf("roll back claims migration: %v", err)
	}
	t.Cleanup(func() {
		// The overlapping rows must go before the migration can succeed again. Truncated
		// directly rather than through Repo.Truncate, which names user_time_claims — a
		// table that does not exist while this migration is rolled back.
		if _, err := h.repo.pool.Exec(ctx,
			`TRUNCATE idempotency_records, bookings, reservations, slots CASCADE`); err != nil {
			t.Errorf("truncate: %v", err)
		}
		if err := Migrate(ctx, databaseURL); err != nil {
			t.Errorf("restore schema: %v", err)
		}
	})

	for i, s := range []domain.Slot{a, b} {
		_, err := h.repo.pool.Exec(ctx, `
			INSERT INTO reservations (reservation_id, slot_organisation_id, slot_id,
				user_organisation_id, user_id, state, created_at, expires_at)
			VALUES ($1, $2, $3, $4, 'user-1', 'held', $5, $6)`,
			fmt.Sprintf("res-%d", i), string(s.OrganisationID), string(s.ID), string(testOrg),
			base.Add(-time.Hour), base.Add(30*time.Minute))
		if err != nil {
			t.Fatalf("seed reservation %d: %v", i, err)
		}
	}

	err := Migrate(ctx, databaseURL)
	if err == nil {
		t.Fatal("migration succeeded over pre-existing overlapping claims: " +
			"those users would be unprotected while the schema claimed otherwise")
	}
	if !strings.Contains(err.Error(), "user_time_claims_no_overlap") {
		t.Errorf("migration failed with %v, want the exclusion constraint to be the cause", err)
	}
}

func tableExists(t *testing.T, repo *Repo, table string) bool {
	t.Helper()
	var exists bool
	err := repo.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		table).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

// The scoped-key primary key is the concurrency backstop of transaction-semantics
// §5.3. It is asserted directly because the constraint's existence — not the Go code
// around it — is what makes a second insert for the same key fail rather than
// overwrite the first.
func TestIdempotencyScopeIsTheTableIdentity(t *testing.T) {
	repo := newRepo(t, testBudget())

	rows, err := repo.pool.Query(context.Background(), `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = 'idempotency_records'::regclass AND i.indisprimary
		ORDER BY a.attname`)
	if err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read primary key: %v", err)
	}

	want := []string{"key", "operation", "user_id", "user_organisation_id"}
	if len(columns) != len(want) {
		t.Fatalf("primary key columns = %v, want %v", columns, want)
	}
	for i := range want {
		if columns[i] != want[i] {
			t.Errorf("primary key columns = %v, want %v", columns, want)
			break
		}
	}
}
