package inmem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
)

func TestLockSlotFound(t *testing.T) {
	s := New()
	s.SeedSlot(domain.Slot{ID: "slot-1", Capacity: 3})
	err := s.WithinTx(context.Background(), func(ctx context.Context, tx domain.Tx) error {
		slot, err := tx.LockSlot(ctx, "slot-1")
		if err != nil {
			return err
		}
		if slot.Capacity != 3 {
			t.Errorf("Capacity = %d, want 3", slot.Capacity)
		}
		if _, err := tx.LockSlot(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing slot: err = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
}

func TestReservationAndBookingRoundTrip(t *testing.T) {
	s := New()
	ctx := context.Background()
	err := s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		res := domain.Reservation{ID: "res-1", SlotID: "slot-1", State: domain.ReservationHeld, ExpiresAt: time.Unix(100, 0)}
		if err := tx.PutReservation(ctx, res); err != nil {
			return err
		}
		if _, err := tx.SlotIDForReservation(ctx, "res-1"); err != nil {
			return err
		}
		held, err := tx.HeldReservations(ctx, "slot-1")
		if err != nil {
			return err
		}
		if len(held) != 1 {
			t.Errorf("held = %d, want 1", len(held))
		}

		bk := domain.Booking{ID: "bk-1", ReservationID: "res-1", SlotID: "slot-1", State: domain.BookingActive}
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
		n, err := tx.ActiveBookingCount(ctx, "slot-1")
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
	s := New()
	ctx := context.Background()
	_ = s.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
		if _, err := tx.Reservation(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Reservation: %v, want ErrNotFound", err)
		}
		if _, err := tx.SlotIDForReservation(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
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
	s := New()
	ctx := context.Background()
	rec := domain.IdempotencyRecord{OrganisationID: "org-1", UserID: "user-1", Operation: domain.OpReserve, Key: "k", RequestHash: "h"}
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
	s := New()
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
	s := New()
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
