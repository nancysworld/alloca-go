//go:build integration

// Package postgres' tests are integration tests: they prove the properties that only
// exist once a real PostgreSQL is serializing transactions, and there is no value in
// a mocked version of them. They are behind the `integration` build tag and require
// DATABASE_URL, so `go test ./...` stays fast and hermetic while `make test-integration`
// (and CI's Postgres service) runs the real thing.
//
// What is proven here, and nowhere else (implementation-plan §5):
//
//   - capacity is never exceeded under genuinely concurrent transactions;
//   - the attempt's authoritative timestamp is resolved *after* the row-lock wait;
//   - one idempotency key yields one logical mutation under contention;
//   - cancellation/confirmation races have exactly one winner;
//   - database timeouts map to distinct, classified outcomes.
package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/service"
)

// databaseURL is resolved once; every test shares one migrated database and truncates
// between runs.
var databaseURL string

func TestMain(m *testing.M) {
	databaseURL = os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "integration tests require DATABASE_URL")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := Migrate(ctx, databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// testBudget is the deadline chain used by most tests. LockTimeout is deliberately
// long enough that an intentional lock wait completes rather than tripping the
// timeout — the tests that want the timeout to fire set their own budget.
func testBudget() config.RequestBudget {
	return config.RequestBudget{
		ClientDeadline:   30 * time.Second,
		ServerDeadline:   20 * time.Second,
		AdmissionCap:     time.Second,
		DBAcquireCap:     5 * time.Second,
		LockTimeout:      10 * time.Second,
		StatementTimeout: 15 * time.Second,
		TxnBudget:        18 * time.Second,
	}
}

// harness bundles a migrated, empty database with a repo and service over it.
type harness struct {
	repo *Repo
	svc  *service.Service
	ids  *seqIDGen
}

func newHarness(t *testing.T, budget config.RequestBudget, ttl time.Duration) *harness {
	t.Helper()
	repo := newRepo(t, budget)
	if err := repo.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	ids := &seqIDGen{}
	return &harness{repo: repo, svc: service.New(repo, ids, ttl), ids: ids}
}

// newRepo builds a repo over its own pool, without truncating or constructing a
// service. Schema-level tests use it: they inspect the database rather than exercise
// the booking operations, so a reservation TTL would be meaningless to them.
func newRepo(t *testing.T, budget config.RequestBudget) *Repo {
	t.Helper()
	pool, err := OpenPool(context.Background(), databaseURL, budget)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool, budget)
}

// dbNow reads the database clock on its own connection, so it is never blocked by a
// transaction the test is deliberately holding.
func (h *harness) dbNow(t *testing.T) time.Time {
	t.Helper()
	var now time.Time
	if err := h.repo.pool.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("read database clock: %v", err)
	}
	return now.UTC()
}

// seedSlot creates a slot whose window is expressed relative to the database clock, so
// tests never depend on the test host's clock agreeing with the database's.
func (h *harness) seedSlot(t *testing.T, id domain.SlotID, capacity int, releaseIn, startsIn time.Duration) domain.Slot {
	t.Helper()
	now := h.dbNow(t)
	slot := domain.Slot{
		ID:             id,
		OrganisationID: testOrg,
		ResourceID:     "resource-1",
		Capacity:       capacity,
		ReleaseAt:      now.Add(releaseIn),
		StartsAt:       now.Add(startsIn),
		EndsAt:         now.Add(startsIn + time.Hour),
	}
	if err := h.repo.SeedSlot(context.Background(), slot); err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	return slot
}

// holdSlotLock takes and holds the slot's row lock for d, mirroring a competing
// transaction. It returns once the lock is actually held, so the caller can then
// contend for it deterministically, plus a wait function for the holder's completion
// and the database time at which the lock was released.
func (h *harness) holdSlotLock(t *testing.T, id domain.SlotID, d time.Duration) (release func() time.Time) {
	t.Helper()
	locked := make(chan struct{})
	releasedAt := make(chan time.Time, 1)

	go func() {
		defer close(releasedAt)
		err := h.repo.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
			if _, err := tx.LockSlot(ctx, id); err != nil {
				return err
			}
			close(locked)
			time.Sleep(d)
			// Read on a separate connection: this transaction still holds the lock, so
			// the value describes the last instant at which it did. Errors are reported
			// with Errorf, never Fatalf — this is not the test goroutine.
			var now time.Time
			if err := h.repo.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
				t.Errorf("lock holder: read database clock: %v", err)
				return nil
			}
			releasedAt <- now.UTC()
			return nil
		})
		if err != nil {
			t.Errorf("lock holder: %v", err)
			// Unblock a caller waiting on the lock that was never taken, so a failure
			// here surfaces as a test error rather than a hang.
			select {
			case <-locked:
			default:
				close(locked)
			}
		}
	}()

	<-locked
	// The returned function blocks until the holder has committed, so a caller can
	// order its assertions after the lock is definitely gone. It yields the zero time
	// if the holder failed; the accompanying Errorf has already failed the test.
	return func() time.Time { return <-releasedAt }
}

// seqIDGen mints deterministic sequential identifiers. Safe for concurrent use.
type seqIDGen struct {
	mu       sync.Mutex
	res, bok int
}

func (g *seqIDGen) NewReservationID() domain.ReservationID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.res++
	return domain.ReservationID(fmt.Sprintf("res-%d", g.res))
}

func (g *seqIDGen) NewBookingID() domain.BookingID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bok++
	return domain.BookingID(fmt.Sprintf("bk-%d", g.bok))
}

const (
	testOrg  = domain.OrganisationID("org-1")
	testSlot = domain.SlotID("slot-1")
)

// --- assertions -------------------------------------------------------------

func assertOutcome(t *testing.T, r domain.Result, want domain.Outcome, reason domain.Reason) {
	t.Helper()
	if r.Outcome != want {
		t.Errorf("outcome = %q (reason %q), want %q", r.Outcome, r.Reason, want)
	}
	if r.Reason != reason {
		t.Errorf("reason = %q, want %q", r.Reason, reason)
	}
}

// assertConsumed checks the capacity invariant against persisted rows rather than
// against what the service reported.
func assertConsumed(t *testing.T, h *harness, slotID domain.SlotID, wantHeld, wantBookings int) {
	t.Helper()
	held, active, err := h.repo.SlotCounts(context.Background(), slotID)
	if err != nil {
		t.Fatalf("slot counts: %v", err)
	}
	if held != wantHeld || active != wantBookings {
		t.Errorf("persisted state: held=%d active bookings=%d, want held=%d active=%d",
			held, active, wantHeld, wantBookings)
	}
}

func (h *harness) reserve(ctx context.Context, user, key string, slotID domain.SlotID) (domain.Result, error) {
	return h.svc.Reserve(ctx, service.ReserveCommand{
		OrganisationID: testOrg, UserID: domain.UserID(user),
		SlotID: slotID, IdempotencyKey: key,
	})
}

func (h *harness) confirm(ctx context.Context, user, key string, res domain.ReservationID) (domain.Result, error) {
	return h.svc.Confirm(ctx, service.ConfirmCommand{
		OrganisationID: testOrg, UserID: domain.UserID(user),
		ReservationID: res, IdempotencyKey: key,
	})
}

func (h *harness) cancel(ctx context.Context, user, key string, res domain.ReservationID) (domain.Result, error) {
	return h.svc.Cancel(ctx, service.CancelCommand{
		OrganisationID: testOrg, UserID: domain.UserID(user),
		ReservationID: res, IdempotencyKey: key,
	})
}
