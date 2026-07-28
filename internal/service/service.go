// Package service is the application/use-case layer over the booking domain. It
// orchestrates the reserve, confirm, and cancel operations against the domain ports:
// taking the slot lock, reading the authoritative timestamp that lock established,
// settling elapsed holds, evaluating preconditions, mutating, and recording the
// idempotency outcome — all within one transaction. It implements
// docs/design/transaction-semantics.md §4–§5 and §7.
//
// The service owns no clock. Authoritative time belongs to the transaction and is
// established by domain.Tx.LockSlot after the row-lock wait, so a decision is made
// against the instant it actually serializes (transaction-semantics §1.5). Keeping a
// service-level clock alongside Tx.Now would put two competing sources of semantic
// time in one operation.
//
// Contract: each operation returns (domain.Result, error). The Result is the
// classified *domain* answer (admitted_success, business_refusal, invalid_request).
// A non-nil error is an infrastructure fault or deadline expiry; the transport edge
// (PR4) classifies it as internal_failure or timeout_* (measurement-contract §6).
// Faults are never returned as a business_refusal — the fault line of §4.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/idempotency"
)

// slotTarget renders a slot's identity as the request hash's target field
// (transaction-semantics §5.1). A bare slot_id is no longer a target: two organisations
// may each mint "slot-1", and hashing only that would let a request for one hash
// identically to a request for the other.
//
// Length-prefixed rather than delimiter-joined, because organisation and slot
// identifiers are arbitrary caller-supplied strings: any separator could occur inside
// one of them, so ("a/b", "c") and ("a", "b/c") would otherwise collide.
func slotTarget(ref domain.SlotRef) string {
	return fmt.Sprintf("%d:%s/%s", len(ref.OrganisationID), ref.OrganisationID, ref.SlotID)
}

// errRetry signals that an idempotency-record insert lost the unique-constraint race
// (domain.ErrConflict). The transaction is rolled back and the whole operation is
// re-run once; the re-run observes the winning record and replays or conflicts
// (transaction-semantics §5.3). It never escapes the package.
var errRetry = errors.New("service: idempotency race, retry")

// Service performs the booking operations.
type Service struct {
	repo domain.Repository
	ids  domain.IDGen
	ttl  time.Duration
}

// New constructs a Service. ttl is the service-owned reservation hold duration
// (transaction-semantics §1.6) and must be positive.
func New(repo domain.Repository, ids domain.IDGen, ttl time.Duration) *Service {
	if ttl <= 0 {
		panic("service: reservation ttl must be positive")
	}
	return &Service{repo: repo, ids: ids, ttl: ttl}
}

// ReserveCommand asks to hold one unit of a slot's capacity.
//
// It carries two organisations and they are not interchangeable
// (transaction-semantics §1.1, §1.2): UserRef.OrganisationID is where the caller's
// identity is issued, while SlotRef.OrganisationID is who owns the slot. They differ
// whenever a user books into another organisation, which is exactly the case the
// schedule invariant exists to cover.
type ReserveCommand struct {
	UserRef        domain.UserRef
	SlotRef        domain.SlotRef
	IdempotencyKey string
	Body           []byte
}

// ConfirmCommand turns a held reservation into a booking.
type ConfirmCommand struct {
	UserRef        domain.UserRef
	ReservationID  domain.ReservationID
	IdempotencyKey string
	Body           []byte
}

// CancelCommand releases a held reservation or an active booking.
type CancelCommand struct {
	UserRef        domain.UserRef
	ReservationID  domain.ReservationID
	IdempotencyKey string
	Body           []byte
}

// Reserve holds one unit of the slot for the caller (transaction-semantics §4).
func (s *Service) Reserve(ctx context.Context, cmd ReserveCommand) (domain.Result, error) {
	if cmd.UserRef.OrganisationID == "" || cmd.UserRef.UserID == "" ||
		cmd.SlotRef.OrganisationID == "" || cmd.SlotRef.SlotID == "" || cmd.IdempotencyKey == "" {
		return domain.Result{Outcome: domain.OutcomeInvalidRequest}, nil
	}
	scope := idempotency.Scope(cmd.UserRef, domain.OpReserve, cmd.IdempotencyKey)
	hash := idempotency.RequestHash(domain.ContractVersion, domain.OpReserve, cmd.UserRef, slotTarget(cmd.SlotRef), cmd.Body)

	return s.run(ctx, func(ctx context.Context, tx domain.Tx) (domain.Result, error) {
		slot, err := tx.LockSlot(ctx, cmd.SlotRef)
		if errors.Is(err, domain.ErrNotFound) {
			return s.unknownTarget(ctx, tx, scope, hash)
		}
		if err != nil {
			return domain.Result{}, err
		}
		// Established by the LockSlot above, so it describes the instant this attempt
		// serialized rather than when the request arrived (transaction-semantics §1.5).
		now, err := tx.Now(ctx)
		if err != nil {
			return domain.Result{}, err
		}
		if r, done, err := s.lookup(ctx, tx, scope, hash); done || err != nil {
			return r, err
		}

		consumed, err := s.settle(ctx, tx, slot.Ref(), now)
		if err != nil {
			return domain.Result{}, err
		}
		// User-scoped claim settlement, the analogue of the slot-scoped settlement above
		// (transaction-semantics §2.2). The requesting user's elapsed holds may be on
		// *other* slots, which this transaction never locks and settle() therefore never
		// sees, so without this an abandoned hold would block the user's schedule until
		// the expiry worker happened to run. Correctness must not depend on that.
		if err := tx.SettleClaims(ctx, cmd.UserRef, now); err != nil {
			return domain.Result{}, err
		}

		var result domain.Result
		var persist func(context.Context, domain.Tx) error
		// The attempt's decision instant. LockSlot established it after the row-lock
		// wait; a claim acquisition is the only thing that can supersede it, because it
		// is the only other authority this transaction waits for (§1.5).
		decisionNow := now
		switch {
		case !slot.Released(now):
			result = domain.Refusal(domain.ReasonSlotNotReleased)
		case slot.Closed(now):
			result = domain.Refusal(domain.ReasonSlotClosed)
		default:
			expiresAt := now.Add(s.ttl)
			switch {
			case expiresAt.After(slot.StartsAt):
				// The computed hold would outlive the slot start; refuse rather than
				// grant a silently shortened hold (transaction-semantics §1.6).
				result = domain.Refusal(domain.ReasonOutsideWindow)
			case consumed >= slot.Capacity:
				result = domain.Refusal(domain.ReasonNoCapacity)
			default:
				resID := s.ids.NewReservationID()
				// The claim is inserted here, during precondition evaluation, rather than
				// alongside the reservation in persist. The insert is the conflict check —
				// a preceding SELECT could always lose to a transaction committing between
				// the read and the write — so the answer has to be known before commit()
				// records a terminal outcome that cannot then be changed.
				//
				// Claim before reservation inverts the natural FK order; the claim's
				// reference to reservations is DEFERRABLE INITIALLY DEFERRED so it is
				// validated at COMMIT, by which point persist has written the row.
				//
				// Its expiry is provisional: the insert may wait on a conflicting
				// uncommitted claim, so the final value is computed from the instant the
				// authority was actually acquired (§1.5) and written by admitAfterClaim.
				claim := domain.ScheduleClaim{
					ReservationID: resID,
					UserRef:       cmd.UserRef,
					SlotRef:       slot.Ref(),
					StartsAt:      slot.StartsAt,
					EndsAt:        slot.EndsAt,
					ExpiresAt:     now.Add(s.ttl),
				}
				acquiredAt, err := tx.InsertClaim(ctx, claim)
				switch {
				case errors.Is(err, domain.ErrScheduleConflict):
					// The user's own time is already claimed. A business refusal, and
					// distinct from no_capacity: this slot may still have room.
					result = domain.Refusal(domain.ReasonScheduleConflict)
				case err != nil:
					return domain.Result{}, err
				default:
					// The claim authority was waited for, so its acquisition instant
					// supersedes the lock instant for every decision from here on.
					decisionNow = acquiredAt
					result, persist, err = s.admitAfterClaim(ctx, tx, slot, resID, cmd.UserRef, acquiredAt)
					if err != nil {
						return domain.Result{}, err
					}
				}
			}
		}
		return s.commit(ctx, tx, scope, hash, result, decisionNow, persist)
	})
}

// admitAfterClaim re-evaluates the slot window against the instant the claim authority
// was acquired, and either finalises the hold or withdraws the claim it provisionally
// inserted.
//
// Acquiring the claim is the attempt's second authority wait (domain.Tx.InsertClaim). If
// a conflicting transaction held the range and then rolled back, this transaction waited
// — up to lock_timeout — and every check made from the slot-lock instant is stale by that
// much. Two of them can flip: the slot can cross starts_at while we wait, and a TTL
// computed from the older instant can end after starts_at or, with a short enough TTL,
// already be in the past. Committing either would break §1.6's "never grant a silently
// shortened hold" and §1.2's closed-slot rule.
//
// So the window is re-checked and the TTL recomputed in full. On refusal the provisional
// claim is removed: it must not outlive the decision that created it, or it would block
// the user's own schedule for an interval they were never granted.
func (s *Service) admitAfterClaim(
	ctx context.Context, tx domain.Tx, slot domain.Slot,
	resID domain.ReservationID, user domain.UserRef, acquiredAt time.Time,
) (domain.Result, func(context.Context, domain.Tx) error, error) {
	withdraw := func(reason domain.Reason) (domain.Result, func(context.Context, domain.Tx) error, error) {
		if err := tx.DeleteClaim(ctx, resID); err != nil {
			return domain.Result{}, nil, err
		}
		return domain.Refusal(reason), nil, nil
	}

	expiresAt := acquiredAt.Add(s.ttl)
	switch {
	case slot.Closed(acquiredAt):
		// The slot started while the claim authority was contended.
		return withdraw(domain.ReasonSlotClosed)
	case expiresAt.After(slot.StartsAt):
		return withdraw(domain.ReasonOutsideWindow)
	}

	// The claim and the hold must agree on when the hold lapses, or settlement would
	// remove one while the other still consumes capacity.
	if err := tx.SetClaimExpiry(ctx, resID, expiresAt); err != nil {
		return domain.Result{}, nil, err
	}
	res := domain.Reservation{
		ID:        resID,
		SlotRef:   slot.Ref(),
		UserRef:   user,
		State:     domain.ReservationHeld,
		CreatedAt: acquiredAt,
		ExpiresAt: expiresAt,
	}
	persist := func(ctx context.Context, tx domain.Tx) error { return tx.PutReservation(ctx, res) }
	return domain.Result{Outcome: domain.OutcomeAdmittedSuccess, ReservationID: resID}, persist, nil
}

// Confirm turns a held, unexpired reservation on an open slot into a booking
// (transaction-semantics §4). It changes no capacity: a held unit becomes a
// confirmed unit.
func (s *Service) Confirm(ctx context.Context, cmd ConfirmCommand) (domain.Result, error) {
	if cmd.UserRef.OrganisationID == "" || cmd.UserRef.UserID == "" ||
		cmd.ReservationID == "" || cmd.IdempotencyKey == "" {
		return domain.Result{Outcome: domain.OutcomeInvalidRequest}, nil
	}
	scope := idempotency.Scope(cmd.UserRef, domain.OpConfirm, cmd.IdempotencyKey)
	hash := idempotency.RequestHash(domain.ContractVersion, domain.OpConfirm, cmd.UserRef, string(cmd.ReservationID), cmd.Body)

	return s.run(ctx, func(ctx context.Context, tx domain.Tx) (domain.Result, error) {
		slot, res, now, done, r, err := s.lockByReservation(ctx, tx, cmd.ReservationID, scope, hash)
		if done || err != nil {
			return r, err
		}

		var result domain.Result
		var persist func(context.Context, domain.Tx) error
		switch res.State {
		case domain.ReservationHeld:
			if slot.Closed(now) {
				// Normally unreachable: a held hold has expires_at <= starts_at, so at
				// or after the start it is settled to expired above and handled below.
				// Kept as a defensive, explicit branch.
				result = domain.Refusal(domain.ReasonSlotClosed)
			} else {
				res.State = domain.ReservationConfirmed
				bkID := s.ids.NewBookingID()
				bk := domain.Booking{
					ID:            bkID,
					ReservationID: res.ID,
					SlotRef:       slot.Ref(),
					UserRef:       res.UserRef,
					State:         domain.BookingActive,
					CreatedAt:     now,
				}
				result = domain.Result{Outcome: domain.OutcomeAdmittedSuccess, BookingID: bkID}
				persist = func(ctx context.Context, tx domain.Tx) error {
					if err := tx.PutReservation(ctx, res); err != nil {
						return err
					}
					if err := tx.PutBooking(ctx, bk); err != nil {
						return err
					}
					// The hold's claim becomes the booking's claim: one logical claim
					// throughout, so confirming can neither drop the identity's protection
					// for an instant nor create a second claim to conflict with the first
					// (transaction-semantics §2.2).
					return tx.ConfirmClaim(ctx, res.ID)
				}
			}
		case domain.ReservationExpired:
			result = domain.Refusal(domain.ReasonReservationExpired)
		default: // confirmed or cancelled
			result = domain.Refusal(domain.ReasonInvalidState)
		}
		return s.commit(ctx, tx, scope, hash, result, now, persist)
	})
}

// Cancel releases the live unit for a reservation — the held reservation itself or
// its active booking — but only while the slot is open (transaction-semantics §3.3,
// §4).
func (s *Service) Cancel(ctx context.Context, cmd CancelCommand) (domain.Result, error) {
	if cmd.UserRef.OrganisationID == "" || cmd.UserRef.UserID == "" ||
		cmd.ReservationID == "" || cmd.IdempotencyKey == "" {
		return domain.Result{Outcome: domain.OutcomeInvalidRequest}, nil
	}
	scope := idempotency.Scope(cmd.UserRef, domain.OpCancel, cmd.IdempotencyKey)
	hash := idempotency.RequestHash(domain.ContractVersion, domain.OpCancel, cmd.UserRef, string(cmd.ReservationID), cmd.Body)

	return s.run(ctx, func(ctx context.Context, tx domain.Tx) (domain.Result, error) {
		slot, res, now, done, r, err := s.lockByReservation(ctx, tx, cmd.ReservationID, scope, hash)
		if done || err != nil {
			return r, err
		}

		var result domain.Result
		var persist func(context.Context, domain.Tx) error
		switch res.State {
		case domain.ReservationHeld:
			if slot.Closed(now) {
				result = domain.Refusal(domain.ReasonSlotClosed)
			} else {
				res.State = domain.ReservationCancelled
				result = domain.Result{Outcome: domain.OutcomeAdmittedSuccess, ReservationID: res.ID}
				persist = func(ctx context.Context, tx domain.Tx) error {
					if err := tx.PutReservation(ctx, res); err != nil {
						return err
					}
					// Atomic with the lifecycle transition: the user's time is freed by the
					// same commit that releases the slot unit.
					return tx.DeleteClaim(ctx, res.ID)
				}
			}
		case domain.ReservationConfirmed:
			bk, err := tx.BookingForReservation(ctx, res.ID)
			if err != nil {
				// A confirmed reservation without a booking is an invariant violation,
				// not a domain refusal (the fault line, §4).
				return domain.Result{}, err
			}
			switch {
			case bk.State != domain.BookingActive:
				result = domain.Refusal(domain.ReasonInvalidState)
			case slot.Closed(now):
				result = domain.Refusal(domain.ReasonSlotClosed)
			default:
				bk.State = domain.BookingCancelled
				result = domain.Result{Outcome: domain.OutcomeAdmittedSuccess, ReservationID: res.ID}
				persist = func(ctx context.Context, tx domain.Tx) error {
					if err := tx.PutBooking(ctx, bk); err != nil {
						return err
					}
					return tx.DeleteClaim(ctx, res.ID)
				}
			}
		default: // expired or cancelled: nothing live to release
			result = domain.Refusal(domain.ReasonInvalidState)
		}
		return s.commit(ctx, tx, scope, hash, result, now, persist)
	})
}

// lockByReservation resolves a reservation's slot, locks it, applies idempotency
// resolution, and settles elapsed holds, returning the locked slot, the attempt's
// authoritative timestamp, and the post-settlement reservation. The (done, result)
// pair short-circuits the operation on an unknown target, replay, or conflict.
//
// The reservation → slot lookup deliberately establishes no timestamp: it runs
// before the lock, so its instant is exactly the stale one the authoritative-time
// contract rejects. Only the LockSlot below fixes the attempt's now.
func (s *Service) lockByReservation(ctx context.Context, tx domain.Tx, id domain.ReservationID, scope domain.ScopeKey, hash string) (slot domain.Slot, res domain.Reservation, now time.Time, done bool, result domain.Result, err error) {
	ref, err := tx.SlotRefForReservation(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		result, err = s.unknownTarget(ctx, tx, scope, hash)
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, result, err
	}
	if err != nil {
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, domain.Result{}, err
	}
	slot, err = tx.LockSlot(ctx, ref)
	if err != nil {
		// A reservation pointing at a missing slot is an invariant violation.
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, domain.Result{}, err
	}
	now, err = tx.Now(ctx)
	if err != nil {
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, domain.Result{}, err
	}
	if r, resolved, lerr := s.lookup(ctx, tx, scope, hash); resolved || lerr != nil {
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, r, lerr
	}
	if _, err = s.settle(ctx, tx, slot.Ref(), now); err != nil {
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, domain.Result{}, err
	}
	res, err = tx.Reservation(ctx, id)
	if err != nil {
		return domain.Slot{}, domain.Reservation{}, time.Time{}, true, domain.Result{}, err
	}
	return slot, res, now, false, domain.Result{}, nil
}

// run executes fn inside a transaction, retrying once on the idempotency-insert race
// (transaction-semantics §5.3). Each attempt is a fresh transaction and therefore
// resolves its own authoritative timestamp; only the committed attempt's value
// becomes durable. Time is per attempt, not per request, precisely because the
// second attempt serializes later than the first.
func (s *Service) run(ctx context.Context, fn func(ctx context.Context, tx domain.Tx) (domain.Result, error)) (domain.Result, error) {
	const maxAttempts = 2
	var last error
	for range maxAttempts {
		var result domain.Result
		err := s.repo.WithinTx(ctx, func(ctx context.Context, tx domain.Tx) error {
			r, e := fn(ctx, tx)
			if e != nil {
				return e
			}
			result = r
			return nil
		})
		if errors.Is(err, errRetry) {
			last = err
			continue
		}
		if err != nil {
			return domain.Result{}, err
		}
		return result, nil
	}
	// Both attempts lost the race: the record exists but could not be read as a
	// resolution. This is not expected; surface it as a fault for the edge to classify.
	return domain.Result{}, last
}

// lookup applies the resolution algorithm's replay/conflict steps against an existing
// record (transaction-semantics §5.3 steps 2–3). done=false means no record was
// found and the operation should proceed.
func (s *Service) lookup(ctx context.Context, tx domain.Tx, scope domain.ScopeKey, hash string) (result domain.Result, done bool, err error) {
	rec, err := tx.FindRecord(ctx, scope)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Result{}, false, nil
	}
	if err != nil {
		return domain.Result{}, false, err
	}
	switch idempotency.Resolve(rec, hash) {
	case idempotency.Replay:
		return replayResult(rec), true, nil
	default:
		return domain.Refusal(domain.ReasonIdempotencyConflict), true, nil
	}
}

// unknownTarget handles a well-formed request for a target that does not exist
// (transaction-semantics §5.5): no slot is locked; the refusal is recorded, guarded
// only by the unique constraint, so a retry replays it and the key cannot later be
// repurposed for a valid target.
//
// This is the one path with no lock to establish the attempt's timestamp, so it asks
// for one explicitly. Naming the no-slot case at the call site is the point: it
// cannot be reached by forgetting to lock a slot that does exist.
func (s *Service) unknownTarget(ctx context.Context, tx domain.Tx, scope domain.ScopeKey, hash string) (domain.Result, error) {
	if r, done, err := s.lookup(ctx, tx, scope, hash); done || err != nil {
		return r, err
	}
	now, err := tx.ResolveTimeWithoutSlot(ctx)
	if err != nil {
		return domain.Result{}, err
	}
	return s.commit(ctx, tx, scope, hash, domain.Refusal(domain.ReasonUnknownTarget), now, nil)
}

// settle transitions every elapsed held reservation on the slot to expired, then
// returns the slot's consumed capacity derived from settled state: held reservations
// plus active bookings (transaction-semantics §1.7, §2.1).
func (s *Service) settle(ctx context.Context, tx domain.Tx, ref domain.SlotRef, now time.Time) (consumed int, err error) {
	held, err := tx.HeldReservations(ctx, ref)
	if err != nil {
		return 0, err
	}
	stillHeld := 0
	for _, r := range held {
		if r.Elapsed(now) {
			r.State = domain.ReservationExpired
			if err := tx.PutReservation(ctx, r); err != nil {
				return 0, err
			}
			// Deliberately does not touch the claim. Removing an elapsed claim is
			// SettleClaims' sole job (transaction-semantics §2.2), so no transaction ever
			// locks a claim row belonging to a user other than the one it acts for —
			// which is what makes claim-row deadlock unreachable. The claim is harmless
			// meanwhile: it can only block its own user, whose next reserve settles it.
			continue
		}
		stillHeld++
	}
	active, err := tx.ActiveBookingCount(ctx, ref)
	if err != nil {
		return 0, err
	}
	return stillHeld + active, nil
}

// commit records the terminal outcome and then persists the mutation (if any). The
// record is inserted first so a unique-constraint race is detected before any entity
// mutation is persisted (transaction-semantics §5.2); on a race it returns errRetry
// so run re-executes.
//
// It first asserts the call-site invariant that ties the two arguments together: a
// mutation exists exactly when the outcome is admitted_success. Every operation sets
// result and persist as a pair, but nothing in the type system enforces it. Were the
// record inserted while its mutation was skipped (admitted_success with persist=nil),
// a replay would return a success referencing a reservation or booking that was never
// written — a durable, silent P1. A violation is a programming error on the fault
// line (§4), never a domain outcome, so it aborts the transaction.
func (s *Service) commit(ctx context.Context, tx domain.Tx, scope domain.ScopeKey, hash string, result domain.Result, now time.Time, persist func(context.Context, domain.Tx) error) (domain.Result, error) {
	if mutates := result.Outcome == domain.OutcomeAdmittedSuccess; mutates != (persist != nil) {
		return domain.Result{}, fmt.Errorf("service: commit invariant violated: outcome %q with persist!=nil==%t", result.Outcome, persist != nil)
	}
	rec := domain.IdempotencyRecord{
		UserRef:       scope.UserRef,
		Operation:     scope.Operation,
		Key:           scope.Key,
		RequestHash:   hash,
		Outcome:       result.Outcome,
		Reason:        result.Reason,
		ReservationID: result.ReservationID,
		BookingID:     result.BookingID,
		CreatedAt:     now,
	}
	err := tx.InsertRecord(ctx, rec)
	if errors.Is(err, domain.ErrConflict) {
		return domain.Result{}, errRetry
	}
	if err != nil {
		return domain.Result{}, err
	}
	if persist != nil {
		if err := persist(ctx, tx); err != nil {
			return domain.Result{}, err
		}
	}
	return result, nil
}

// replayResult reconstructs the recorded terminal outcome for a replay, with
// Replay=true (transaction-semantics §5.3).
func replayResult(rec domain.IdempotencyRecord) domain.Result {
	return domain.Result{
		Outcome:       rec.Outcome,
		Reason:        rec.Reason,
		Replay:        true,
		ReservationID: rec.ReservationID,
		BookingID:     rec.BookingID,
	}
}
