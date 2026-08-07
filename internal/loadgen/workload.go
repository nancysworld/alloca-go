package loadgen

import (
	"context"
	"fmt"
	"strconv"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// User and Slot are the two identities a workload addresses, each a pair scoped to an
// organisation (transaction-semantics §1.1, §1.2). Both are aliases for the domain's own
// refs, not copies of their shape.
//
// They are in-memory inputs used to *build* a request, not the wire format: what crosses
// the boundary is the JSON that `do` marshals and the JSON the service answers with, and
// those shapes stay local to this package. An identity, by contrast, is the same fact on
// both sides — the pair is what the service locks and scopes by — so a second declaration
// of it here would be a copy that nothing keeps in step.
//
// Note the target of each alias. `Slot` is `domain.SlotRef`, the *reference*, not
// `domain.Slot`, the aggregate the service owns; the harness names a slot and never holds
// one. The names stay short because a workload reads better for it.
type (
	User = domain.UserRef
	Slot = domain.SlotRef
)

// Workload is one controlled shape from ag-sept-plan §5. Each isolates a single mechanism,
// which is why they are separate types rather than one parameterised workload: a composite
// changes several variables at once and is harder to attribute (§3.1).
type Workload interface {
	// Name is the workload's stable identifier in the run summary and manifest.
	Name() string
	// Do performs one logical unit of work and returns every response it produced. seq is
	// unique across the run, so a workload can derive distinct identities and idempotency
	// keys from it without coordinating with other workers.
	Do(ctx context.Context, c *Client, seq int) []Response
}

// key builds an idempotency key that is unique per logical request. Uniqueness per
// *logical* request is the contract: a retry of the same logical request must reuse the
// key, and PR1's workloads do not retry, so one key per (workload, seq, step) is correct.
func key(workload string, seq int, step string) string {
	return workload + "-" + strconv.Itoa(seq) + "-" + step
}

// Dispersed spreads requests across many slots and many users so contention on any one
// authority is low (§5.1). It is the control for horizontal composition: what the service
// does when transactions can mostly proceed independently.
type Dispersed struct {
	Org   domain.OrganisationID
	Slots []Slot
	// Confirm drives reserve→confirm rather than reserve alone. It matters for more than
	// realism: a confirmed claim has expires_at IS NULL and is never expiry-reaped, so a
	// confirming workload leaves permanent rows and needs the clean-start discipline of
	// §5.3 just as much as the hot-identity control does.
	Confirm bool
}

func (Dispersed) Name() string { return "dispersed" }

func (d Dispersed) Do(ctx context.Context, c *Client, seq int) []Response {
	slot := d.Slots[seq%len(d.Slots)]
	user := User{OrganisationID: d.Org, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}

	res := c.Reserve(ctx, user, slot, key(d.Name(), seq, "reserve"))
	out := []Response{res}
	if !d.Confirm || res.ReservationID == "" {
		return out
	}
	return append(out, c.Confirm(ctx, user, res.ReservationID, key(d.Name(), seq, "confirm")))
}

// HotSlot points many distinct users at one slot (§5.2). It exposes the serialization
// frontier of the row that owns capacity, and is the workload whose scale efficiency is
// expected to approach 1/N — the correct answer, not a failure (§8).
//
// Every user is distinct, so the only contended authority is the slot. Reusing one user
// would add identity serialization and make the result un-attributable.
type HotSlot struct {
	Org  domain.OrganisationID
	Slot Slot
}

func (HotSlot) Name() string { return "hot_slot" }

func (h HotSlot) Do(ctx context.Context, c *Client, seq int) []Response {
	user := User{OrganisationID: h.Org, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}
	return []Response{c.Reserve(ctx, user, h.Slot, key(h.Name(), seq, "reserve"))}
}

// HotIdentity points one identity at many overlapping slots (§5.3). It measures the cost
// and containment of the user-identity serialization authority, and expects exactly one
// admitted reservation with the rest refused as schedule_conflict.
//
// That expectation only holds from a clean start. A confirmed claim is never reaped, so a
// rerun against a contaminated fixture yields zero admitted and all conflicts — a result
// that passes every correctness gate and means nothing (§5.3). Enforcing the clean start is
// the verifier's job, not this type's, but the trap belongs in the comment nearest the
// workload that springs it.
type HotIdentity struct {
	User  User
	Slots []Slot
}

func (HotIdentity) Name() string { return "hot_identity" }

func (h HotIdentity) Do(ctx context.Context, c *Client, seq int) []Response {
	slot := h.Slots[seq%len(h.Slots)]
	return []Response{c.Reserve(ctx, h.User, slot, key(h.Name(), seq, "reserve"))}
}

// Replay is the control for the disposition dimension: it issues each reserve twice under
// the *same* idempotency key and checks that the second is served from the record.
//
// It exists because goodput now excludes replays (see Summary.Goodput), and an excluded
// population that nothing exercises is an untested assumption wearing the word "correct".
// The other workloads derive a unique key per logical request, so a clean run produces no
// replays at all — which means without this control the replay path is measured by every
// AG-Sept run and verified by none of them.
//
// What it checks is measurement-contract §4.2's normative sentence: "A replay must return
// exactly the recorded outcome; it must never re-run the mutation or resolve to a different
// terminal outcome." So a second response is invalid unless it is flagged `replay=true` and
// carries the first's outcome and reason. Marking it invalid rather than counting it wrong
// puts the failure through the machinery that already exists: an invalid response makes the
// run unsound, and an unsound run is LevelNone whatever else it achieved.
//
// The persisted half of the same claim is reconciliation's: a replay must create no row, so
// live claims stay equal to fresh admitted reserves and idempotency records stay equal to
// fresh mutations however many replays ran.
type Replay struct {
	Org   domain.OrganisationID
	Slots []Slot
}

func (Replay) Name() string { return "replay" }

func (r Replay) Do(ctx context.Context, c *Client, seq int) []Response {
	slot := r.Slots[seq%len(r.Slots)]
	user := User{OrganisationID: r.Org, UserID: domain.UserID(fmt.Sprintf("u-%d", seq))}
	k := key(r.Name(), seq, "reserve")

	first := c.Reserve(ctx, user, slot, k)
	second := c.Reserve(ctx, user, slot, k)
	second.Invalid = replayDefect(first, second)
	return []Response{first, second}
}

// replayDefect names how a second response under a reused key departs from §4.2, or returns
// "" when it conforms. Separated from Do so the rule reads as one list rather than as control
// flow, and so a test can drive every branch without a server.
func replayDefect(first, second Response) string {
	switch {
	case first.Invalid != "" || !first.Outcome.IsKnown():
		// The first attempt never established a recorded outcome, so the second is not
		// evidence about the idempotency path at all. Judging it here would blame that path
		// for an unrelated fault and send an operator to the wrong subsystem.
		return second.Invalid
	case second.Invalid != "":
		// Already failed status/outcome validation; keep the original reason rather than
		// replacing it with a replay complaint about a response that was malformed anyway.
		return second.Invalid
	case !second.Replay:
		return fmt.Sprintf("second request under idempotency key returned replay=false: "+
			"the recorded %s outcome was re-run rather than replayed", first.Outcome)
	case second.Outcome != first.Outcome:
		return fmt.Sprintf("replay returned outcome %q but the recorded outcome was %q",
			second.Outcome, first.Outcome)
	case second.Reason != first.Reason:
		return fmt.Sprintf("replay returned refusal reason %q but the recorded reason was %q",
			second.Reason, first.Reason)
	case first.Outcome == domain.OutcomeAdmittedSuccess &&
		second.ReservationID != first.ReservationID:
		// A replay that admits a *different* reservation has performed a second mutation
		// while reporting the first's outcome, which is the exact failure §4.2 forbids and
		// the one a count of outcomes cannot see.
		return fmt.Sprintf("replay returned reservation %q but the recorded reservation was %q",
			second.ReservationID, first.ReservationID)
	default:
		return ""
	}
}

// Interface assertions.
var (
	_ Workload = Dispersed{}
	_ Workload = HotSlot{}
	_ Workload = HotIdentity{}
	_ Workload = Replay{}
)
