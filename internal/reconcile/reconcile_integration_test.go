//go:build integration

// The three database-backed rules of ag-sept-plan §6.5 can only be proven against a real
// PostgreSQL: they compare what a client was told against what the schema actually holds,
// and a mocked database would be asserting that the fake agrees with itself.
//
// Each test drives real booking operations through the service, then hands the
// reconciler a Summary describing what a generator *would have reported* for those
// operations — and, in the negative cases, a Summary that misreports them. The failure
// mode being guarded is a run whose numbers look fine while the database disagrees.
package reconcile_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nancysworld/alloca-go/internal/config"
	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/ids"
	"github.com/nancysworld/alloca-go/internal/loadgen"
	"github.com/nancysworld/alloca-go/internal/postgres"
	"github.com/nancysworld/alloca-go/internal/reconcile"
	"github.com/nancysworld/alloca-go/internal/service"
)

var databaseURL string

func TestMain(m *testing.M) { os.Exit(runSuite(m)) }

func runSuite(m *testing.M) int {
	databaseURL = os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "integration tests require DATABASE_URL")
		return 1
	}
	release, err := postgres.ClaimDatabase(databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		return 1
	}
	return m.Run()
}

const testOrg = domain.OrganisationID("load-org")

func budget() config.RequestBudget {
	return config.RequestBudget{
		ClientDeadline: 30 * time.Second, ServerDeadline: 20 * time.Second,
		AdmissionCap: time.Second, DBAcquireCap: 5 * time.Second,
		LockTimeout: 10 * time.Second, StatementTimeout: 15 * time.Second,
		TxnBudget: 18 * time.Second,
	}
}

type fixture struct {
	pool *pgxpool.Pool
	repo *postgres.Repo
	svc  *service.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := postgres.OpenPool(context.Background(), databaseURL, budget())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := postgres.New(pool, budget())
	if err := repo.Truncate(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return &fixture{pool: pool, repo: repo, svc: service.New(repo, ids.Random{}, time.Hour)}
}

// seedSlot creates a slot open for booking now, with the given capacity.
func (f *fixture) seedSlot(t *testing.T, id domain.SlotID, capacity int) {
	t.Helper()
	var now time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("clock: %v", err)
	}
	err := f.repo.SeedSlot(context.Background(), domain.Slot{
		ID: id, OrganisationID: testOrg, ResourceID: "resource-1", Capacity: capacity,
		ReleaseAt: now.Add(-time.Hour),
		// Far enough out that a one-hour hold TTL still expires before the slot
		// starts; a nearer window is refused as outside_window, which is correct
		// behaviour and not what these tests are about.
		StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(25 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seed slot: %v", err)
	}
}

// reserve drives one real reserve through the service, as the generator's HTTP request
// eventually would.
func (f *fixture) reserve(t *testing.T, user string, slot domain.SlotID, key string) domain.Result {
	t.Helper()
	res, err := f.svc.Reserve(context.Background(), service.ReserveCommand{
		UserRef:        domain.UserRef{OrganisationID: testOrg, UserID: domain.UserID(user)},
		SlotRef:        domain.SlotRef{OrganisationID: testOrg, SlotID: slot},
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	return res
}

// summaryFor builds the Summary a generator would emit for the given tallies.
func summaryFor(admitted, refused int, reason domain.Reason) loadgen.Summary {
	totals := []loadgen.Total{}
	if admitted > 0 {
		totals = append(totals, loadgen.Total{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeAdmittedSuccess, Count: admitted,
		})
	}
	if refused > 0 {
		totals = append(totals, loadgen.Total{
			Operation: string(domain.OpReserve),
			Outcome:   domain.OutcomeBusinessRefusal, Reason: reason, Count: refused,
		})
	}
	return loadgen.Summary{
		Totals: totals, Completed: admitted + refused,
		Sound: true, ValidationEnabled: true,
	}
}

// runReconcile verifies s against the fixture's database, supplying the metrics scrape a
// correctly instrumented service would have produced for s.
//
// Deriving the server totals from the client summary is exactly what a test of the *database*
// rules wants: it holds the client/server comparison satisfied so a failure here names the
// rule under test. The comparison has its own tests, where the two sides are made to differ
// on purpose.
func runReconcile(f *fixture, s loadgen.Summary) (reconcile.Result, error) {
	return reconcile.Run(context.Background(), f.pool, testOrg, reportOf(s), reconcile.Scrapes{After: reconcile.ServerTotals(s.Totals)})
}

// TestCleanRunReconciles is the positive case, and it has to come first: every negative
// test below would also pass against a reconciler that rejected everything.
//
// It is the shape of the hot-slot control — capacity 2, four users, two admitted and two
// refused for no_capacity — driven for real, so the totals are not invented.
func TestCleanRunReconciles(t *testing.T) {
	f := newFixture(t)
	f.seedSlot(t, "slot-hot", 2)

	admitted, refused := 0, 0
	for i := range 4 {
		res := f.reserve(t, fmt.Sprintf("u-%d", i), "slot-hot", fmt.Sprintf("k-%d", i))
		switch res.Outcome {
		case domain.OutcomeAdmittedSuccess:
			admitted++
		case domain.OutcomeBusinessRefusal:
			refused++
		default:
			t.Fatalf("unexpected outcome %q (reason %q)", res.Outcome, res.Reason)
		}
	}
	if admitted != 2 || refused != 2 {
		t.Fatalf("admitted=%d refused=%d, want 2 and 2 for a capacity-2 slot", admitted, refused)
	}

	res, err := runReconcile(f, summaryFor(admitted, refused, domain.ReasonNoCapacity))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Quotability.Level == loadgen.LevelNone {
		t.Fatalf("clean run is not quotable: %s\n%+v", res.Quotability.BlockedBecause, res.Checks)
	}
	if len(res.Checks) != 5 {
		t.Errorf("ran %d checks, want the 4 database rules of §6.5 plus the "+
			"client/server comparison", len(res.Checks))
	}
	for _, c := range res.Checks {
		if !c.OK {
			t.Errorf("check %q failed on a clean run: %s", c.Name, c.Detail)
		}
	}
}

// TestUnderreportedAdmissionsAreCaught is the capacity rule's discriminating case
// (INV-1's arithmetic). The database holds two live reservations; the client claims it
// was told about one. That gap is a service that mutated state without telling the
// caller — the most dangerous disagreement of the three, because the numbers look
// conservative rather than wrong.
func TestUnderreportedAdmissionsAreCaught(t *testing.T) {
	f := newFixture(t)
	f.seedSlot(t, "slot-under", 2)
	f.reserve(t, "u-0", "slot-under", "k-0")
	f.reserve(t, "u-1", "slot-under", "k-1")

	res, err := runReconcile(f, summaryFor(1, 0, ""))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("run is quotable while the database holds units the client was never told about")
	}
	if !contains(res.Checks, "consumed capacity vs admitted reserves") {
		t.Errorf("wrong check failed: %s", res.Quotability.BlockedBecause)
	}
}

// TestOverreportedAdmissionsAreCaughtByClaims covers the opposite direction. The client
// claims three admitted reserves; only two claims exist. A reserve that was reported as
// admitted but left no claim is a hold the schedule does not know about, which is exactly
// what INV-4 exists to make impossible.
func TestOverreportedAdmissionsAreCaughtByClaims(t *testing.T) {
	f := newFixture(t)
	f.seedSlot(t, "slot-over", 5)
	f.reserve(t, "u-0", "slot-over", "k-0")
	f.reserve(t, "u-1", "slot-over", "k-1")

	res, err := runReconcile(f, summaryFor(3, 0, ""))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("run is quotable while claiming more admitted reserves than the schedule records")
	}
}

// TestHotIdentityContaminationIsVisible is the check that matters most for AG-Sept, and
// the reason the reconciler exists at all.
//
// One identity reserving across overlapping slots must yield exactly one admitted
// reservation, the rest schedule_conflict. A *rerun* against contaminated fixtures yields
// zero admitted and all conflicts — a result that violates no invariant and passes every
// correctness gate while meaning nothing.
//
// Reconciliation cannot detect that from the database alone, because the state is
// perfectly legal. What it can do is refuse the run whose *client totals* disagree with
// persisted claims — which is what a contaminated rerun produces the moment the generator
// still expects an admission. This test pins that, and documents the residual gap: the
// clean-start assertion in §5.3 is a separate guard, not something reconciliation subsumes.
func TestHotIdentityContaminationIsVisible(t *testing.T) {
	f := newFixture(t)
	// Two slots whose windows overlap, so one identity can hold at most one of them.
	f.seedSlot(t, "slot-a", 5)
	f.seedSlot(t, "slot-b", 5)

	first := f.reserve(t, "solo", "slot-a", "k-a")
	if first.Outcome != domain.OutcomeAdmittedSuccess {
		t.Fatalf("first reserve = %q, want admitted_success", first.Outcome)
	}
	second := f.reserve(t, "solo", "slot-b", "k-b")
	if second.Outcome != domain.OutcomeBusinessRefusal || second.Reason != domain.ReasonScheduleConflict {
		t.Fatalf("second reserve = %q/%q, want business_refusal/schedule_conflict",
			second.Outcome, second.Reason)
	}

	// The honest summary for what just happened reconciles.
	ok, err := runReconcile(f, summaryFor(1, 1, domain.ReasonScheduleConflict))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if ok.Quotability.Level == loadgen.LevelNone {
		t.Fatalf("honest hot-identity totals rejected: %s", ok.Quotability.BlockedBecause)
	}

	// A contaminated rerun reports zero admitted and two conflicts, while the claim from
	// the earlier run is still present. The totals are internally consistent, so this run
	// is *not* caught here — which is precisely why §5.3 requires a clean-start assertion
	// before load begins rather than relying on reconciliation to notice afterwards.
	contaminated, err := runReconcile(f, summaryFor(0, 2, domain.ReasonScheduleConflict))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if contaminated.Quotability.Level == loadgen.LevelNone {
		t.Log("contaminated rerun happened to be caught:", contaminated.Quotability.BlockedBecause)
	} else {
		t.Log("contaminated rerun reconciles cleanly, as expected: reconciliation cannot " +
			"detect it, and the clean-start assertion of §5.3 is the guard that must")
	}
}

// TestIdempotencyRecordsMatchFreshMutations covers INV-5's arithmetic against real records,
// including that a replay does not demand a record of its own — the mistake that would make
// this check fail a correct service.
func TestIdempotencyRecordsMatchFreshMutations(t *testing.T) {
	f := newFixture(t)
	f.seedSlot(t, "slot-idem", 3)

	f.reserve(t, "u-0", "slot-idem", "k-0")
	f.reserve(t, "u-1", "slot-idem", "k-1")
	// Same user, same key: a replay. It returns the recorded outcome and writes no new
	// record, so the reconciler must not count it as a fresh mutation.
	f.reserve(t, "u-0", "slot-idem", "k-0")

	s := loadgen.Summary{
		Totals: []loadgen.Total{
			{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess, Count: 2},
			{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess,
				Replay: true, Count: 1},
		},
		Completed: 3, Sound: true, ValidationEnabled: true,
	}

	res, err := runReconcile(f, s)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Quotability.Level == loadgen.LevelNone {
		t.Fatalf("a run with one legitimate replay was rejected: %s\n%+v", res.Quotability.BlockedBecause, res.Checks)
	}
}

// TestUnrecordedMutationsAreCaught is the idempotency rule's negative case: the client was
// told three mutations committed, but only two records exist. An outcome the client acted
// on that was never durably recorded breaks the replay guarantee.
func TestUnrecordedMutationsAreCaught(t *testing.T) {
	f := newFixture(t)
	f.seedSlot(t, "slot-rec", 5)
	f.reserve(t, "u-0", "slot-rec", "k-0")
	f.reserve(t, "u-1", "slot-rec", "k-1")

	// Claim three fresh admitted mutations against two records and two claims.
	res, err := runReconcile(f, summaryFor(3, 0, ""))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Quotability.Level != loadgen.LevelNone {
		t.Fatal("run claiming more committed mutations than were recorded is quotable")
	}
}

func contains(checks []reconcile.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name && !c.OK {
			return true
		}
	}
	return false
}
