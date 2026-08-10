//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestIdempotencyRecordsSurviveAnEmptyClaimTable is the evidence for the second half of the
// clean-start assertion (ag-sept-validation-plan.md §3.3), and the reason the first half is
// not enough.
//
// The trap it pins: a fixture can hold zero live claims — every hold cancelled, expired or
// never confirmed — while the idempotency records of the previous run remain, because a
// record deliberately outlives the entity it describes so a late retry still replays. PR1's
// workloads derive keys from workload name and sequence number with no per-run nonce, so the
// next run without -reset re-sends the same keys, is answered from those records, commits
// nothing, and reconciles cleanly: a run that reports goodput, breaks no invariant, and
// measures nothing.
//
// alloca-seed asserts both counts are zero. This test proves the counts can disagree, which
// is what makes the second assertion load-bearing rather than decorative.
func TestIdempotencyRecordsSurviveAnEmptyClaimTable(t *testing.T) {
	h := newHarness(t, testBudget(), time.Minute)
	ctx := context.Background()
	h.seedSlot(t, "slot-0", 2, -time.Hour, 24*time.Hour)

	res, err := h.reserve(ctx, "user-1", "dispersed-0-reserve", "slot-0")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := h.cancel(ctx, "user-1", "dispersed-0-cancel", res.ReservationID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Cancellation removes the claim, which is what makes a claim-only assertion pass here.
	claims, err := h.repo.ClaimCount(ctx)
	if err != nil {
		t.Fatalf("claim count: %v", err)
	}
	if claims != 0 {
		t.Fatalf("claims = %d, want 0 — cancellation should have removed it, and without "+
			"that this test is not exercising the gap it exists for", claims)
	}

	records, err := h.repo.IdempotencyRecordCount(ctx)
	if err != nil {
		t.Fatalf("record count: %v", err)
	}
	if records == 0 {
		t.Fatal("no idempotency records survived, so a claim-only clean-start assertion " +
			"would be sufficient and alloca-seed's second check would be dead weight")
	}

	// The replay itself, which is what the assertion is protecting the next run from: the
	// same key returns the recorded outcome and commits nothing new.
	replayed, err := h.reserve(ctx, "user-1", "dispersed-0-reserve", "slot-0")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayed.Replay {
		t.Errorf("re-sending a used key was not a replay: %+v", replayed)
	}
	if after, err := h.repo.ClaimCount(ctx); err != nil {
		t.Fatalf("claim count: %v", err)
	} else if after != 0 {
		t.Errorf("claims = %d after a replay, want 0: the replay committed something", after)
	}
}

// TestTruncateClearsBothCounts covers what -reset does, so the fix the assertion recommends
// is known to work rather than assumed to.
func TestTruncateClearsBothCounts(t *testing.T) {
	h := newHarness(t, testBudget(), time.Minute)
	ctx := context.Background()
	h.seedSlot(t, "slot-0", 2, -time.Hour, 24*time.Hour)

	if _, err := h.reserve(ctx, "user-1", "k-1", "slot-0"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := h.repo.Truncate(ctx); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	claims, err := h.repo.ClaimCount(ctx)
	if err != nil {
		t.Fatalf("claim count: %v", err)
	}
	records, err := h.repo.IdempotencyRecordCount(ctx)
	if err != nil {
		t.Fatalf("record count: %v", err)
	}
	if claims != 0 || records != 0 {
		t.Errorf("after truncate: %d claims, %d records, want 0 and 0", claims, records)
	}
}
