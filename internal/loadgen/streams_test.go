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

// The fixture both halves of VAL-NEG-8 are driven with. Shared so the two designs are compared
// under one set of parameters rather than two that could drift.
const (
	negWindow  = 600 * time.Millisecond
	negWorkers = 4
	negSlow    = 40 * time.Millisecond
	negGroups  = 2
)

// demandIndependence is the VAL-NEG-8 predicate, applied to whatever design produced the counts.
//
// **It compares the two groups inside one run, never one run against another.** The first
// version of this test measured the healthy group beside a slow one and again alone, and
// required the two numbers to be within 25%. That is a comparison of two independently timed
// samples: on a shared CI runner it read 1569 against 2652 and failed, while the slow group had
// completed 60 requests either way — so demand independence plainly held and the assertion was
// measuring the machine. A within-run ratio cancels machine speed out, because both numbers come
// from the same run on the same hardware.
//
// Two independent signals, because they fail differently:
//
//   - the *ratio*. With independent pools the healthy group runs at its own speed while the slow
//     one is gated by its latency, so the ratio is ~90. Sharing the pool makes every worker
//     alternate, so both groups are gated by the slow one and the ratio collapses to ~1.0. Two
//     orders of magnitude separate them;
//   - an absolute floor *derived from the fixture*, not from this machine. A shared pool cannot
//     complete more than totalWorkers × window/delay logical units in the whole run, because each
//     unit costs the slow group's latency. The healthy group alone exceeding several times that
//     total is something no shared-pool design can produce at any clock speed.
func demandIndependence(fast, slow int) error {
	if slow == 0 {
		return fmt.Errorf("the slow group served nothing at all, so the fixture is not " +
			"exercising the divergence this control detects")
	}

	// The thresholds are set against measured values at both ends rather than chosen round
	// numbers. Independent pools: healthy ~4700-5400, slow 60, ratio 79-91 locally under -race,
	// and healthy 1569 on the CI runner that first failed this test. Shared pool: healthy 119,
	// slow 120, ratio 1.0, and healthy cannot exceed the ceiling below by construction. So any
	// threshold between the two discriminates; these sit roughly in the middle of that range,
	// leaving ~3x margin against the defect and ~4x against the slowest healthy run observed.
	//
	// The slow group's count is the stable half of the ratio: it is latency-bound at
	// workers × window/delay = 60 whatever the machine's speed, which is why the ratio moves
	// only with the healthy group.
	sharedPoolCeiling := negWorkers * negGroups * int(negWindow/negSlow)
	if fast <= 3*sharedPoolCeiling {
		return fmt.Errorf("the healthy group completed %d requests, within reach of the %d a "+
			"single shared pool could complete in this whole run: its workers are being gated "+
			"by the slow group's latency, which is the coupling VAL-NEG-8 forbids",
			fast, sharedPoolCeiling)
	}

	if ratio := float64(fast) / float64(slow); ratio < 10 {
		return fmt.Errorf("the healthy group completed %d requests against the slow group's %d "+
			"(ratio %.1f): with independent pools the healthy group runs at its own speed and "+
			"the ratio is large, and a ratio near 1 is what sharing workers produces",
			fast, slow, ratio)
	}
	return nil
}

// TestOneSlowGroupDoesNotThrottleAHealthyGroup is VAL-NEG-8.
//
// The property is not "both groups completed similar work" — they must not, since one authority
// is ten times slower — but that the healthy group keeps issuing at its own pace while its
// neighbour is stalled.
func TestOneSlowGroupDoesNotThrottleAHealthyGroup(t *testing.T) {
	const (
		window  = negWindow
		workers = negWorkers
		slow    = negSlow
	)

	client, slowUnit, fastUnit, streams := twoGroupFixture(t, slow)
	if len(streams) != negGroups {
		t.Fatalf("fixture built %d groups, want %d: the derived shared-pool ceiling assumes it",
			len(streams), negGroups)
	}
	runner := loadgen.NewRunner(client, loadgen.Options{WorkersPerGroup: workers, Duration: window})
	summary, err := runner.RunStreams(context.Background(), streams)
	if err != nil {
		t.Fatalf("independent run: %v", err)
	}

	if err := demandIndependence(fastUnit.served(), slowUnit.served()); err != nil {
		t.Error(err)
	}

	// Reported whether or not the assertions passed: a future failure on someone else's machine
	// is far easier to read against the numbers this one produced.
	t.Logf("independent pools: healthy=%d slow=%d ratio=%.1f",
		fastUnit.served(), slowUnit.served(),
		float64(fastUnit.served())/float64(slowUnit.served()))

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
	// Same total workers as the independent run — 2 groups × 4 — so the difference between the
	// two tests is the pool structure and nothing else.
	client, slowUnit, fastUnit, streams := twoGroupFixture(t, negSlow)
	shared := loadgen.NewRunner(client, loadgen.Options{
		Concurrency: negWorkers * len(streams),
		Duration:    negWindow,
	})
	shared.Run(context.Background(), sharedPoolRoundRobin{streams: streams})

	t.Logf("shared pool: healthy=%d slow=%d ratio=%.1f",
		fastUnit.served(), slowUnit.served(),
		float64(fastUnit.served())/float64(slowUnit.served()))

	// The *same* predicate, not a mirror of it. A second assertion written to be the opposite of
	// the first can drift from it, and then this test would be reporting that the old design
	// fails a check the new one is no longer held to.
	if err := demandIndependence(fastUnit.served(), slowUnit.served()); err == nil {
		t.Fatalf("the shared-pool design passed the VAL-NEG-8 predicate (healthy=%d slow=%d): "+
			"the control does not discriminate between the two designs, so the new one passing "+
			"it says nothing", fastUnit.served(), slowUnit.served())
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
