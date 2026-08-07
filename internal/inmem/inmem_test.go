package inmem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// testSlotOrg owns every slot these tests seed. A slot's identity is the pair
// (slot_organisation_id, slot_id) (domain.SlotRef), so seeding with an empty organisation
// would let the double's map lookups pass without ever exercising the pair.
const testSlotOrg = domain.OrganisationID("org-1")

// ref builds the SlotRef for a slot owned by testSlotOrg.
func ref(id domain.SlotID) domain.SlotRef {
	return domain.SlotRef{OrganisationID: testSlotOrg, SlotID: id}
}

func TestLockSlotFound(t *testing.T) {
	s := New(SystemClock{})
	s.SeedSlot(domain.Slot{ID: "slot-1", OrganisationID: testSlotOrg, Capacity: 3})
	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		slot, err := tx.LockSlot(ctx, ref("slot-1"))
		if err != nil {
			return err
		}
		if slot.Capacity != 3 {
			t.Errorf("Capacity = %d, want 3", slot.Capacity)
		}
		if _, err := tx.LockSlot(ctx, ref("missing")); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing slot: err = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

func TestReservationAndBookingRoundTrip(t *testing.T) {
	s := New(SystemClock{})
	ctx := context.Background()
	err := s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		res := domain.Reservation{ID: "res-1", SlotRef: ref("slot-1"), State: domain.ReservationHeld, ExpiresAt: time.Unix(100, 0)}
		if err := tx.PutReservation(ctx, res); err != nil {
			return err
		}
		if _, _, err := tx.ReservationTarget(ctx, "res-1"); err != nil {
			return err
		}
		held, err := tx.HeldReservations(ctx, ref("slot-1"))
		if err != nil {
			return err
		}
		if len(held) != 1 {
			t.Errorf("held = %d, want 1", len(held))
		}

		bk := domain.Booking{ID: "bk-1", ReservationID: "res-1", SlotRef: ref("slot-1"), State: domain.BookingActive}
		if err := tx.PutBooking(ctx, bk); err != nil {
			return err
		}
		got, err := tx.BookingForReservation(ctx, "res-1")
		if err != nil {
			return err
		}
		if got.ID != "bk-1" {
			t.Errorf("booking ID = %s, want bk-1", got.ID)
		}
		n, err := tx.ActiveBookingCount(ctx, ref("slot-1"))
		if err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("active bookings = %d, want 1", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

func TestNotFoundErrors(t *testing.T) {
	s := New(SystemClock{})
	ctx := context.Background()
	_ = s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.Reservation(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Reservation: %v, want ErrNotFound", err)
		}
		if _, _, err := tx.ReservationTarget(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("SlotIDForReservation: %v, want ErrNotFound", err)
		}
		if _, err := tx.BookingForReservation(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("BookingForReservation: %v, want ErrNotFound", err)
		}
		if _, err := tx.FindRecord(ctx, domain.ScopeKey{Key: "nope"}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindRecord: %v, want ErrNotFound", err)
		}
		return nil
	})
}

func TestInsertRecordConflict(t *testing.T) {
	s := New(SystemClock{})
	ctx := context.Background()
	rec := domain.IdempotencyRecord{UserRef: domain.UserRef{OrganisationID: "org-1", UserID: "user-1"}, Operation: domain.OpReserve, Key: "k", RequestHash: "h"}
	_ = s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		if err := tx.InsertRecord(ctx, rec); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if err := tx.InsertRecord(ctx, rec); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("second insert: %v, want ErrConflict", err)
		}
		return nil
	})
}

func TestWithinTxCancelledContextDoesNotRunFn(t *testing.T) {
	s := New(SystemClock{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := false
	err := s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithinTx: err = %v, want context.Canceled", err)
	}
	if ran {
		t.Error("fn ran under a cancelled context; an abandoned request must not mutate")
	}
}

// A transaction that cannot acquire the store lock before its deadline must fail
// rather than block indefinitely — the reference-double stand-in for a PostgreSQL
// lock wait exceeding lock_timeout (transaction-semantics §6).
func TestWithinTxDeadlineDuringLockWait(t *testing.T) {
	s := New(SystemClock{})
	held := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
			close(held)
			<-time.After(50 * time.Millisecond)
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	ran := false
	err := s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WithinTx: err = %v, want context.DeadlineExceeded", err)
	}
	if ran {
		t.Error("fn ran after the deadline expired during the lock wait")
	}
	<-done
}

// --- authoritative time contract --------------------------------------------
//
// These pin the contract the PostgreSQL adapter implements against clock_timestamp():
// a lock establishes the attempt's timestamp, and nothing else may invent one. The
// double must obey the same rules, or tests written above it would prove properties
// the real adapter does not have.

// stepClock returns a new value on each read, so a test can tell "the memoised value
// was returned" apart from "the clock was read again".
type stepClock struct {
	base time.Time
	step time.Duration
	n    int
}

func (c *stepClock) Now() time.Time {
	c.n++
	return c.base.Add(time.Duration(c.n) * c.step)
}

func TestNowBeforeLockIsAnError(t *testing.T) {
	s := New(SystemClock{})
	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		_, err := tx.Now(ctx)
		if !errors.Is(err, domain.ErrTimeNotEstablished) {
			t.Errorf("Now before LockSlot: err = %v, want ErrTimeNotEstablished", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

// A slot that does not exist must leave the attempt with no timestamp. If LockSlot
// established one on the not-found path, the unknown-target case could silently pick
// up time without ever declaring that it never locked anything.
func TestLockSlotNotFoundEstablishesNoTime(t *testing.T) {
	s := New(SystemClock{})
	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.LockSlot(ctx, ref("missing")); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("LockSlot(missing): err = %v, want ErrNotFound", err)
		}
		if _, err := tx.Now(ctx); !errors.Is(err, domain.ErrTimeNotEstablished) {
			t.Errorf("Now after failed LockSlot: err = %v, want ErrTimeNotEstablished", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

// One attempt, one instant: every decision in the attempt must see the same value
// however many times the transaction locks or asks.
func TestAttemptTimeIsResolvedOnceAndMemoised(t *testing.T) {
	clock := &stepClock{base: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC), step: time.Second}
	s := New(clock)
	s.SeedSlot(domain.Slot{ID: "slot-1", OrganisationID: testSlotOrg, Capacity: 1})

	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.LockSlot(ctx, ref("slot-1")); err != nil {
			return err
		}
		first, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		// A second lock, a second read, and the no-slot resolver must all agree.
		if _, err := tx.LockSlot(ctx, ref("slot-1")); err != nil {
			return err
		}
		second, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		third, err := tx.ResolveTimeWithoutSlot(ctx)
		if err != nil {
			return err
		}
		if !first.Equal(second) || !first.Equal(third) {
			t.Errorf("attempt time drifted: %v, %v, %v", first, second, third)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
	if clock.n != 1 {
		t.Errorf("clock read %d times in one attempt, want 1", clock.n)
	}
}

// Time is per attempt, not per repository: a second transaction resolves a new
// instant. This is what makes the service's insert-race re-run see a later time, as
// the real adapter would.
func TestEachAttemptResolvesItsOwnTime(t *testing.T) {
	clock := &stepClock{base: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC), step: time.Second}
	s := New(clock)
	s.SeedSlot(domain.Slot{ID: "slot-1", OrganisationID: testSlotOrg, Capacity: 1})

	read := func() time.Time {
		var got time.Time
		if err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
			if _, err := tx.LockSlot(ctx, ref("slot-1")); err != nil {
				return err
			}
			var err error
			got, err = tx.Now(ctx)
			return err
		}); err != nil {
			t.Fatalf("WithinTx: %v", err)
		}
		return got
	}

	first, second := read(), read()
	if !second.After(first) {
		t.Errorf("second attempt time %v is not after the first %v", second, first)
	}
}

// The unknown-target path has no slot to lock but still records an outcome, so it
// must be able to establish time explicitly.
func TestResolveTimeWithoutSlotEstablishesTime(t *testing.T) {
	s := New(SystemClock{})
	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		resolved, err := tx.ResolveTimeWithoutSlot(ctx)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			t.Fatalf("Now after ResolveTimeWithoutSlot: %v", err)
		}
		if !now.Equal(resolved) {
			t.Errorf("Now = %v, want the resolved %v", now, resolved)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

// --- backwards-clock negative control ---------------------------------------

// rewindClock steps backwards after its first read, standing in for a host clock
// correction.
type rewindClock struct {
	base   time.Time
	rewind time.Duration
	reads  int
}

func (c *rewindClock) Now() time.Time {
	c.reads++
	if c.reads == 1 {
		return c.base
	}
	return c.base.Add(-c.rewind)
}

// A backwards clock step must not reverse terminal state (authoritative-time design note §5, §6's third
// negative control). Centralising wall time in PostgreSQL makes decisions coherent
// across API nodes; it does not make the clock monotonic, so irreversibility has to be
// a property of the state machine rather than an assumption about time.
//
// This runs against the double because the database's clock cannot be stepped from a
// test. It therefore proves the domain half — an expired reservation stays expired,
// and consumed capacity does not resurrect — rather than the adapter half.
func TestBackwardsClockDoesNotReviveExpiredHold(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := &rewindClock{base: base.Add(time.Hour), rewind: 2 * time.Hour}
	s := New(clock)
	s.SeedSlot(domain.Slot{
		ID: "slot-1", OrganisationID: testSlotOrg, Capacity: 1,
		ReleaseAt: base.Add(-time.Hour), StartsAt: base.Add(4 * time.Hour),
	})

	// An elapsed hold, settled on the first attempt while the clock reads +1h.
	expired := domain.Reservation{
		ID: "res-1", SlotRef: ref("slot-1"), State: domain.ReservationHeld,
		CreatedAt: base.Add(-time.Minute), ExpiresAt: base,
	}
	if err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		return tx.PutReservation(ctx, expired)
	}); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	settle := func() {
		t.Helper()
		err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
			if _, err := tx.LockSlot(ctx, ref("slot-1")); err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			held, err := tx.HeldReservations(ctx, ref("slot-1"))
			if err != nil {
				return err
			}
			for _, r := range held {
				if r.Elapsed(now) {
					r.State = domain.ReservationExpired
					if err := tx.PutReservation(ctx, r); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("settle: %v", err)
		}
	}

	settle() // clock reads +1h: the hold has elapsed and is settled to expired.

	// The clock now steps back to before the hold's expires_at. Settlement runs again
	// on a timeline where the hold "has not yet expired" — and must change nothing,
	// because expiry acts only on held rows and expired is terminal.
	settle()

	var got domain.Reservation
	if err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		var err error
		got, err = tx.Reservation(ctx, "res-1")
		return err
	}); err != nil {
		t.Fatalf("read reservation: %v", err)
	}

	if got.State != domain.ReservationExpired {
		t.Errorf("state = %q after the clock stepped backwards, want %q to remain terminal",
			got.State, domain.ReservationExpired)
	}
	held, active := s.SlotCounts(ref("slot-1"))
	if held != 0 || active != 0 {
		t.Errorf("consumed capacity held=%d active=%d after a backwards clock step, want 0/0: "+
			"an expired unit must not become live again", held, active)
	}
}
