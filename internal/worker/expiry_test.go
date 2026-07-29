package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// These tests drive RunOnce directly, or cancel a context, so none of them sleeps or
// races a ticker. That is the point of RunOnce being exported: worker behaviour is
// decided by what one iteration does, and a test that waits for a tick would be timing
// against the clock rather than asserting on behaviour.

type fakeSlots struct {
	refs  []domain.SlotRef
	err   error
	calls int
	limit int
}

func (f *fakeSlots) ElapsedHoldSlots(_ context.Context, limit int) ([]domain.SlotRef, error) {
	f.calls++
	f.limit = limit
	return f.refs, f.err
}

type fakeSettler struct {
	mu       sync.Mutex
	perSlot  map[domain.SlotID]int
	failOn   domain.SlotID
	settled  []domain.SlotRef
	failWith error
}

func (f *fakeSettler) SettleSlot(_ context.Context, ref domain.SlotRef) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled = append(f.settled, ref)
	if ref.SlotID == f.failOn {
		return 0, f.failWith
	}
	return f.perSlot[ref.SlotID], nil
}

func ref(id domain.SlotID) domain.SlotRef {
	return domain.SlotRef{OrganisationID: "org-1", SlotID: id}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The ordinary case: every candidate slot is settled and the counts are reported.
func TestRunOnceSettlesEveryCandidateSlot(t *testing.T) {
	slots := &fakeSlots{refs: []domain.SlotRef{ref("a"), ref("b")}}
	settler := &fakeSettler{perSlot: map[domain.SlotID]int{"a": 2, "b": 3}}
	w := NewExpiry(slots, settler, nil, quietLogger(), Config{})

	obs, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(settler.settled) != 2 {
		t.Errorf("settled %d slots, want 2", len(settler.settled))
	}
	if obs.Slots != 2 {
		t.Errorf("observed slots = %d, want 2", obs.Slots)
	}
	if obs.Expired != 5 {
		t.Errorf("observed expired = %d, want 5 (2 + 3)", obs.Expired)
	}
	if obs.Failed {
		t.Error("a successful iteration was observed as failed")
	}
}

// Nothing to do is not a failure, and it must not look like one in the telemetry.
func TestRunOnceWithNoCandidatesIsNotAFailure(t *testing.T) {
	w := NewExpiry(&fakeSlots{}, &fakeSettler{}, nil, quietLogger(), Config{})

	obs, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if obs.Slots != 0 || obs.Expired != 0 || obs.Failed {
		t.Errorf("observation = %+v, want an empty successful iteration", obs)
	}
}

// A failure part-way through is reported, and the work already done is still counted:
// losing it would make the telemetry disagree with the database.
func TestRunOnceReportsAFailureAndKeepsTheWorkAlreadyDone(t *testing.T) {
	boom := errors.New("database unreachable")
	slots := &fakeSlots{refs: []domain.SlotRef{ref("a"), ref("b"), ref("c")}}
	settler := &fakeSettler{
		perSlot:  map[domain.SlotID]int{"a": 4},
		failOn:   "b",
		failWith: boom,
	}
	w := NewExpiry(slots, settler, nil, quietLogger(), Config{})

	obs, err := w.RunOnce(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("RunOnce error = %v, want the settler's error", err)
	}
	if !obs.Failed {
		t.Error("failed iteration not observed as failed")
	}
	if obs.Expired != 4 {
		t.Errorf("observed expired = %d, want 4: work completed before the failure was lost", obs.Expired)
	}
	// The iteration stops rather than continuing to the slot after the failure.
	if len(settler.settled) != 2 {
		t.Errorf("settled %d slots, want 2 (it must stop at the failure)", len(settler.settled))
	}
}

// A failure listing candidates is reported the same way, and never reaches the settler.
func TestRunOnceReportsAListingFailure(t *testing.T) {
	boom := errors.New("query failed")
	settler := &fakeSettler{}
	w := NewExpiry(&fakeSlots{err: boom}, settler, nil, quietLogger(), Config{})

	obs, err := w.RunOnce(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("RunOnce error = %v, want the listing error", err)
	}
	if !obs.Failed {
		t.Error("failed iteration not observed as failed")
	}
	if len(settler.settled) != 0 {
		t.Error("settled a slot despite failing to list candidates")
	}
}

// The batch bound is what keeps one iteration's work finite.
func TestRunOnceAppliesTheBatchBound(t *testing.T) {
	slots := &fakeSlots{}
	w := NewExpiry(slots, &fakeSettler{}, nil, quietLogger(), Config{Batch: 7})
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if slots.limit != 7 {
		t.Errorf("asked for %d slots, want the configured batch of 7", slots.limit)
	}
}

// Run must return when its context is cancelled — that is the whole of the shutdown
// contract, and a worker that ignores it would hold the process open past its grace
// period.
func TestRunReturnsOnCancellation(t *testing.T) {
	w := NewExpiry(&fakeSlots{}, &fakeSettler{}, nil, quietLogger(), Config{Interval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// A generous ceiling on a return that should be immediate: this asserts the
		// goroutine ended, and only fails the test if it never does.
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// A failing iteration must not stop the loop: expiry is opportunistic, and the process
// is still serving requests that settle their own slots correctly.
func TestRunSurvivesAFailingIteration(t *testing.T) {
	slots := &fakeSlots{err: errors.New("transient")}
	recorder := &countingRecorder{seen: make(chan telemetry.ExpiryObservation, 8)}
	w := NewExpiry(slots, &fakeSettler{}, recorder, quietLogger(), Config{Interval: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	// Two observed failures prove the loop kept going after the first, without
	// depending on how long a tick takes: the test waits for events, not for time.
	for i := range 2 {
		select {
		case obs := <-recorder.seen:
			if !obs.Failed {
				t.Fatalf("iteration %d observed as successful, want failed", i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d failing iterations were observed; the loop stopped after a failure", i)
		}
	}
	cancel()
	<-done
}

type countingRecorder struct {
	seen chan telemetry.ExpiryObservation
}

func (c *countingRecorder) RecordRequest(context.Context, telemetry.RequestObservation) {}

func (c *countingRecorder) RecordExpiry(_ context.Context, obs telemetry.ExpiryObservation) {
	select {
	case c.seen <- obs:
	default: // never block the worker on a slow test
	}
}
