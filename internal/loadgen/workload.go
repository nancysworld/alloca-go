package loadgen

import (
	"context"
	"fmt"
	"strconv"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// User and Slot are the harness's view of the two identities, each a pair scoped to an
// organisation (transaction-semantics §1.1, §1.2). They are the client's own types rather
// than the domain's refs so the generator stays a consumer of the HTTP contract.
type User struct {
	OrganisationID domain.OrganisationID
	UserID         domain.UserID
}

type Slot struct {
	OrganisationID domain.OrganisationID
	SlotID         domain.SlotID
}

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

// Interface assertions.
var (
	_ Workload = Dispersed{}
	_ Workload = HotSlot{}
	_ Workload = HotIdentity{}
)
