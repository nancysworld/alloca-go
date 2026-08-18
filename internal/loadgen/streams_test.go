package loadgen_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// pacedUnit is one authority that answers every mutation successfully after a fixed delay,
// counting what it was asked to serve and which idempotency keys arrived.
//
// The delay is the whole experiment: VAL-NEG-8's defect appears only when one group's
// response time diverges, so a control built from healthy units could not detect it.
type pacedUnit struct {
	delay time.Duration

	mu       sync.Mutex
	requests int
	keys     []string
	users    []string
}

func newPacedUnit(t *testing.T, delay time.Duration) (*pacedUnit, string) {
	t.Helper()
	unit := &pacedUnit{delay: delay}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(unit.delay)

		var body struct {
			UserID string `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		unit.mu.Lock()
		unit.requests++
		unit.keys = append(unit.keys, r.Header.Get("Idempotency-Key"))
		unit.users = append(unit.users, body.UserID)
		unit.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": domain.OutcomeAdmittedSuccess, "replay": false, "reservation_id": "res-1",
		})
	}))
	t.Cleanup(srv.Close)
	return unit, srv.URL
}

func (u *pacedUnit) served() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests
}

func (u *pacedUnit) mintedKeys() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.keys...)
}

func (u *pacedUnit) mintedUsers() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.users...)
}

// twoGroupFixture is the G2 placement: `org-a`/`org-b` on authority-1, `org-c`/`org-d` on
// authority-2, with authority-1 deliberately slow.
func twoGroupFixture(t *testing.T, slow time.Duration) (*loadgen.Client, *pacedUnit, *pacedUnit, []loadgen.Stream) {
	t.Helper()

	placement, err := domain.ParsePlacement([]byte(
		`{"version":"itc-g2","homes":{"org-a":"authority-1","org-b":"authority-1",` +
			`"org-c":"authority-2","org-d":"authority-2"}}`))
	if err != nil {
		t.Fatalf("parsing placement: %v", err)
	}

	slowUnit, slowURL := newPacedUnit(t, slow)
	fastUnit, fastURL := newPacedUnit(t, 0)
	router, err := loadgen.NewRouter(placement, map[domain.AuthorityID]string{
		"authority-1": slowURL,
		"authority-2": fastURL,
	})
	if err != nil {
		t.Fatalf("building router: %v", err)
	}

	slotsByOrg := map[domain.OrganisationID][]loadgen.Slot{}
	for _, org := range []domain.OrganisationID{"org-a", "org-b", "org-c", "org-d"} {
		slotsByOrg[org] = []loadgen.Slot{
			{OrganisationID: org, SlotID: domain.SlotID(string(org) + "-1")},
			{OrganisationID: org, SlotID: domain.SlotID(string(org) + "-2")},
		}
	}
	populations, err := loadgen.NewOrgPopulations(slotsByOrg)
	if err != nil {
		t.Fatalf("building populations: %v", err)
	}
	groups, err := loadgen.NewOrgGroups(placement, slotsByOrg)
	if err != nil {
		t.Fatalf("building groups: %v", err)
	}
	streams, err := loadgen.NewMutDisp4Streams(populations, groups, false, loadgen.PhaseMeasured)
	if err != nil {
		t.Fatalf("building streams: %v", err)
	}

	return loadgen.NewRoutedClient(router, 5*time.Second, true), slowUnit, fastUnit, streams
}

// sharedPoolRoundRobin is the design independent streams replaced: one worker pool, one
// sequence, groups taken in turn.
//
// It lives in the test rather than the package because its only remaining purpose is to be
// failed. VAL-NEG-8 requires the control to fail against the old implementation, and a
// mutation that is described but never run is evidence about the description.
type sharedPoolRoundRobin struct{ streams []loadgen.Stream }

func (sharedPoolRoundRobin) Name() string         { return "shared-pool-round-robin" }
func (sharedPoolRoundRobin) IntendsReplays() bool { return false }

func (s sharedPoolRoundRobin) Do(ctx context.Context, c *loadgen.Client, seq int) []loadgen.Response {
	return s.streams[seq%len(s.streams)].Workload.Do(ctx, c, seq)
}

// TestOneSlowGroupDoesNotThrottleAHealthyGroup is VAL-NEG-8.
//
// The property is not "both groups completed similar work" — they must not, since one
// authority is ten times slower — but that the healthy group's *offered demand* is what it
// would have been alone. The reference run establishes that number rather than assuming it,
// because a hard-coded expectation would encode this machine's speed.
func TestOneSlowGroupDoesNotThrottleAHealthyGroup(t *testing.T) {
	const (
		window  = 600 * time.Millisecond
		workers = 4
		slow    = 40 * time.Millisecond
	)

	// Reference: the healthy group alone, no slow neighbour in the run at all.
	client, _, fastAlone, streams := twoGroupFixture(t, slow)
	runner := loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: workers, Duration: window})
	if _, err := runner.RunStreams(context.Background(), streams[1:]); err != nil {
		t.Fatalf("reference run: %v", err)
	}
	alone := fastAlone.served()
	if alone == 0 {
		t.Fatal("the reference run served nothing, so it cannot bound anything")
	}

	// The measurement: both groups, independent pools.
	client, slowUnit, fastUnit, streams := twoGroupFixture(t, slow)
	runner = loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: workers, Duration: window})
	summary, err := runner.RunStreams(context.Background(), streams)
	if err != nil {
		t.Fatalf("independent run: %v", err)
	}

	// The slow group must actually have been slow, or the control proved nothing.
	if slowUnit.served() == 0 {
		t.Fatal("the slow group served nothing at all; the fixture is not exercising the case")
	}
	if ratio := float64(fastUnit.served()) / float64(slowUnit.served()); ratio < 4 {
		t.Fatalf("the two groups completed comparable work (%d fast vs %d slow); the delay is "+
			"not diverging their response times and the control is not discriminating",
			fastUnit.served(), slowUnit.served())
	}

	// The healthy group kept its own demand. The margin is wide because this is a timing
	// test: the defect it detects costs an order of magnitude, not a few percent.
	if floor := alone * 3 / 4; fastUnit.served() < floor {
		t.Errorf("the healthy group completed %d requests beside a slow group but %d alone: "+
			"its offered demand fell with another group's latency, which is the coupling "+
			"VAL-NEG-8 forbids", fastUnit.served(), alone)
	}

	// Per-group accounting must be present and attributable, since the capacity comparison
	// reads it: an aggregate alone cannot show one saturated group and three starved ones.
	if len(summary.Groups) != 2 {
		t.Fatalf("summary carries %d group rows, want one per active shard group", len(summary.Groups))
	}
	var completed int
	for _, group := range summary.Groups {
		if group.Workers != workers {
			t.Errorf("group %q reports %d workers, want the configured %d",
				group.Group, group.Workers, workers)
		}
		completed += group.Completed
	}
	if completed != summary.Completed {
		t.Errorf("group rows sum to %d completed requests, aggregate reports %d",
			completed, summary.Completed)
	}
	if summary.WorkersPerGroup != workers || summary.Concurrency != workers*len(streams) {
		t.Errorf("summary reports workers_per_group=%d concurrency=%d, want %d and %d: an "+
			"artifact must carry both, since neither can be derived without the topology",
			summary.WorkersPerGroup, summary.Concurrency, workers, workers*len(streams))
	}
}

// TestSharedPoolRoundRobinFailsTheDemandIndependenceControl is the mutation VAL-NEG-8 must
// fail against, executed rather than asserted.
//
// It runs the *same* scenario and the same total worker population through the old design.
// If this test ever stops seeing the collapse, the control above has stopped discriminating
// and its passing says nothing.
func TestSharedPoolRoundRobinFailsTheDemandIndependenceControl(t *testing.T) {
	const (
		window  = 600 * time.Millisecond
		workers = 4
		slow    = 40 * time.Millisecond
	)

	client, _, fastAlone, streams := twoGroupFixture(t, slow)
	runner := loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: workers, Duration: window})
	if _, err := runner.RunStreams(context.Background(), streams[1:]); err != nil {
		t.Fatalf("reference run: %v", err)
	}
	alone := fastAlone.served()

	// Same total workers as the independent run — 2 groups × 4 — so the difference is the
	// pool structure and not the demand.
	client, slowUnit, fastUnit, streams := twoGroupFixture(t, slow)
	shared := loadgen.NewRunner(client, loadgen.Options{Concurrency: workers * len(streams), Duration: window})
	shared.Run(context.Background(), sharedPoolRoundRobin{streams: streams})

	if slowUnit.served() == 0 {
		t.Fatal("the slow group served nothing at all; the fixture is not exercising the case")
	}
	if floor := alone * 3 / 4; fastUnit.served() >= floor {
		t.Fatalf("the shared-pool design served %d healthy requests against %d alone, which "+
			"passes the VAL-NEG-8 assertion: the control does not discriminate between the "+
			"two designs and proves nothing about the new one", fastUnit.served(), alone)
	}
}

// TestStreamsMintDisjointIdempotencyKeys covers the other half of independence. Each stream
// numbers its own units from zero, so without a group in the key two of them mint the same
// logical key — which no authority would report, because each sees it once.
func TestStreamsMintDisjointIdempotencyKeys(t *testing.T) {
	client, slowUnit, fastUnit, streams := twoGroupFixture(t, 0)
	runner := loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: 2, Iterations: 8})
	if _, err := runner.RunStreams(context.Background(), streams); err != nil {
		t.Fatalf("run: %v", err)
	}

	seen := map[string]bool{}
	for _, key := range append(slowUnit.mintedKeys(), fastUnit.mintedKeys()...) {
		if key == "" {
			t.Fatal("a request carried no idempotency key")
		}
		if seen[key] {
			t.Errorf("idempotency key %q was minted by two streams; a retained key must name "+
				"the stream that issued it", key)
		}
		seen[key] = true
	}
	if len(seen) == 0 {
		t.Fatal("no keys were observed, so nothing was checked")
	}
}

func TestRunStreamsRefusesAStreamSetItCannotAttribute(t *testing.T) {
	client, _, _, streams := twoGroupFixture(t, 0)
	runner := loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: 1, Iterations: 1})

	for _, tc := range []struct {
		name    string
		streams []loadgen.Stream
	}{
		{"no streams", nil},
		{"repeated group", []loadgen.Stream{streams[0], streams[0]}},
		{"unnamed group in a multi-group run", []loadgen.Stream{
			{Workload: streams[0].Workload}, streams[1],
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runner.RunStreams(context.Background(), tc.streams); err == nil {
				t.Fatal("the run was accepted; a stream set whose requests cannot be attributed " +
					"to a group is a configuration error, not a run with a caveat")
			}
		})
	}
}

// TestConditioningAndMeasuredPopulationsShareNothing is the conditioning gate's disjointness
// clause (measurement-contract §5).
//
// Conditioning has to leave representative rows in the same physical tables, so it cannot be
// separated by pointing it at a different fixture. What must be disjoint is what it *claims*:
// the slots whose capacity it spends, the identities whose schedules it occupies, and the
// idempotency keys it mints. Overlap in any of the three would let the conditioning phase
// consume the measured population's headroom, which invalidates the capacity point while
// presenting as ordinary contention.
func TestConditioningAndMeasuredPopulationsShareNothing(t *testing.T) {
	slotsByOrg := map[domain.OrganisationID][]loadgen.Slot{}
	for _, org := range []domain.OrganisationID{"org-a", "org-b", "org-c", "org-d"} {
		var slots []loadgen.Slot
		for i := range 6 {
			slots = append(slots, loadgen.Slot{
				OrganisationID: org,
				SlotID:         domain.SlotID(fmt.Sprintf("%s-%d", org, i)),
			})
		}
		slotsByOrg[org] = slots
	}
	populations, err := loadgen.NewOrgPopulations(slotsByOrg)
	if err != nil {
		t.Fatalf("building populations: %v", err)
	}

	conditioning, measured, err := loadgen.SplitPopulationsForConditioning(populations, 2)
	if err != nil {
		t.Fatalf("splitting populations: %v", err)
	}

	// Every organisation survives the split on both sides. A phase that silently dropped one
	// would condition three organisations and measure four, and the unconditioned authority
	// would carry the cold-plan regime into the measured interval alone.
	if len(conditioning) != len(populations) || len(measured) != len(populations) {
		t.Fatalf("split produced %d conditioning and %d measured populations, want %d each",
			len(conditioning), len(measured), len(populations))
	}

	claimed := map[domain.SlotID]string{}
	for _, population := range conditioning {
		for _, slot := range population.Slots {
			claimed[slot.SlotID] = "conditioning"
		}
	}
	for _, population := range measured {
		for _, slot := range population.Slots {
			if phase, taken := claimed[slot.SlotID]; taken {
				t.Errorf("slot %q is in both the %s and measured populations; conditioning "+
					"would spend fixture the capacity point depends on", slot.SlotID, phase)
			}
		}
	}

	// The whole fixture is used: a split that quietly dropped slots would shrink the measured
	// population without saying so, and the fixture-headroom calculation would be wrong in
	// the dangerous direction.
	var total int
	for i := range conditioning {
		total += len(conditioning[i].Slots) + len(measured[i].Slots)
	}
	if want := len(populations) * 6; total != want {
		t.Errorf("the split accounts for %d slots, want the whole seeded %d", total, want)
	}
}

// The two phases must also mint disjoint identities and idempotency keys against the same
// authority, which the slot split alone does not give: a user's schedule is its own
// serialization authority, and a shared key namespace would make conditioning's records
// replayable by measured requests.
func TestConditioningMintsADisjointIdentityAndKeyNamespace(t *testing.T) {
	unit, url := newPacedUnit(t, 0)
	client := loadgen.NewRoutedClient(loadgen.SingleTarget(url), 5*time.Second, true).WithRunID("run-1")

	population := []loadgen.OrgPopulation{{
		Org:   "org-a",
		Slots: []loadgen.Slot{{OrganisationID: "org-a", SlotID: "a-1"}},
	}}
	usersByPhase := map[loadgen.Phase][]string{}
	for _, phase := range []loadgen.Phase{loadgen.PhaseConditioning, loadgen.PhaseMeasured} {
		before := len(unit.mintedUsers())
		workload := loadgen.MutDisp4{Orgs: population, Group: "authority-1", Phase: phase}
		loadgen.NewRunner(client, loadgen.Options{Concurrency: 1, Iterations: 3}).
			Run(context.Background(), workload)
		usersByPhase[phase] = unit.mintedUsers()[before:]
	}

	// The identities, not only the keys. A user's schedule is its own serialization
	// authority, so reusing an identity would let conditioning's claims contend with the
	// measured population's on rows it is supposed to have to itself — and that contention
	// would present as ordinary latency rather than as a fixture error.
	conditioningUsers := map[string]bool{}
	for _, user := range usersByPhase[loadgen.PhaseConditioning] {
		conditioningUsers[user] = true
	}
	if len(conditioningUsers) == 0 {
		t.Fatal("the conditioning phase minted no identities, so nothing was checked")
	}
	for _, user := range usersByPhase[loadgen.PhaseMeasured] {
		if conditioningUsers[user] {
			t.Errorf("identity %q is used by both phases; conditioning's claims would contend "+
				"with the measured population on that user's schedule", user)
		}
	}

	keys := unit.mintedKeys()
	if len(keys) == 0 {
		t.Fatal("no requests were observed, so nothing was checked")
	}
	seen := map[string]bool{}
	var conditioningKeys int
	for _, key := range keys {
		if seen[key] {
			t.Errorf("key %q was minted by both phases; a measured request replaying a "+
				"conditioning record commits nothing and reports goodput short by that "+
				"population", key)
		}
		seen[key] = true
		if strings.Contains(key, string(loadgen.PhaseConditioning)) {
			conditioningKeys++
		}
	}
	if conditioningKeys == 0 {
		t.Error("no key names the conditioning phase, so the retained artifact cannot say " +
			"which population a record belongs to")
	}
}

// Conditioning may not be sized to leave the measured population nothing to book.
func TestSplitRefusesAConditioningPhaseThatWouldEatTheFixture(t *testing.T) {
	populations := []loadgen.OrgPopulation{{
		Org:   "org-a",
		Slots: []loadgen.Slot{{OrganisationID: "org-a", SlotID: "a-1"}, {OrganisationID: "org-a", SlotID: "a-2"}},
	}}
	for _, conditioningSlots := range []int{0, 2, 3} {
		if _, _, err := loadgen.SplitPopulationsForConditioning(populations, conditioningSlots); err == nil {
			t.Errorf("a %d-slot conditioning phase against a 2-slot organisation was accepted",
				conditioningSlots)
		}
	}
}
