# Cross-authority booking: claim time at the user's home

**Status:** Exploratory idea; protocol design and validation remain open.  
**Recorded:** 2026-09-11 — Nancy Zhang.

## The idea

Keep the user's reserved time intervals under the user's schedule owner, even when
the slots they book belong to different database authorities.

> Claim the user's interval as part of the reservation protocol, before reporting
> confirmation. Coordinate with that user's schedule owner and the target slot's
> capacity owner.

This gives a single-user, single-slot booking two known participant owners,
regardless of how many slot authorities exist in the deployment. A booking does
not need to ask every slot authority whether the user already has an overlapping
reservation.

This note captures the reasoning for future work. The accepted ownership model
remains in [Horizontal database authority and ownership axes](../design/horizontal-database-authority.md),
especially sections 3 and 4.2. It already defines user-home and slot-home and defers
the cross-database protocol to Phase 2. This idea explains that boundary; it does
not introduce a new placement model or claim that Phase 2 is implemented.

## Ownership follows the invariant

| Participant owner | State and decision |
|---|---|
| Slot-home | The target slot's capacity state; whether capacity can be claimed without overselling |
| User-home | The user's authoritative schedule claims; whether the requested interval can be claimed without overlap |

Use the existing identities:

- `SlotRef = (slot_organisation_id, slot_id)`
- `UserRef = (user_organisation_id, user_id)`

The user's identity is independent of the organisation owning the target slot.
All authoritative claims for that same `UserRef` stay at user-home. This is a set
of booked or held intervals, not a single "last reservation time" or a timestamp
saying when confirmation occurred. The logical schedule need not be stored as
one large field on the user row.

The existing design distinguishes three logical authorities across these two
ownership axes: slot capacity, user-schedule serialization, and claim validity.
The latter two remain colocated at user-home. "Two owners" here means the two
participant homes for a booking, not a replacement for that logical-authority
model.

## Why this avoids querying every slot authority

Consider two concurrent requests for the same user:

| Request | Slot owner | Requested interval | Schedule owner |
|---|---|---|---|
| A | Slot-home A | 10:00–11:00 | The user's home |
| B | Slot-home B | 10:30–11:30 | The same user's home |

Both slots may have spare capacity. Nevertheless, the user's schedule owner must
prevent both overlapping interval claims from becoming active together.

Neither slot owner needs to discover all the user's bookings elsewhere. The
schedule claims provide the authoritative conflict decision at one known home.
This must be a coordinated check-and-claim operation: a read-only "is the user
free?" check followed later by an uncoordinated insert would leave a race.

User-home is not one central database for all users. Different user homes can be
placed on different database authorities under the existing placement model.
The stable property is one authoritative home for each user's complete schedule.

## What still needs coordination

The two-owner shape bounds the participants; it does not make two independent
database commits atomic.

If capacity is claimed first and the interval claim fails, capacity needs safe
release or recovery. If the interval is claimed first and capacity cannot be
obtained, the schedule claim needs equivalent handling. A crash or lost response
between the operations can leave the outcome uncertain in either order.

The future protocol must explain:

- How tentative claims on both owners become one durable successful result.
- Where the workflow decision lives and how a replacement coordinator recovers it.
- How retries identify the same operation and resolve unknown outcomes.
- How reserve, confirm, cancel, and expiry affect both claims.
- How delayed messages are prevented from confirming a claim after it was released
  or superseded.
- What clients can observe while only part of the operation has completed.

Alloca already distinguishes reserve from confirm. Schedule protection must begin
when an active reservation hold is established; waiting until the later confirm
operation would leave overlapping holds unprotected. Exact current local
semantics remain owned by [Transaction semantics](../design/transaction-semantics.md)
and explained in [User schedule non-overlap](../design-notes/user-schedule-non-overlap.md).

Two participant owners also does not imply two network calls or constant response
time. Coordination, retries, contention, and recovery still have costs. No
performance or correctness result is claimed for a cross-database protocol here.

## Next exploration

Start with one user, one slot, and two database authorities. Draw the smallest
candidate protocol and challenge it with simultaneous overlapping bookings and
a crash or lost response after each durable transition.

Retain the existing single-transaction path when both ownership axes resolve to
the same database authority. For the cross-database path, compare possible
protocols against the same invariants before selecting an implementation.

## Related exploration

[Dynamic placement and reclamation of booking state](dynamic-placement-and-reclamation.md)
extends this idea to placement below organisation granularity, allocation of new
slots and user homes, and retirement of inactive slots, memberships, and past
schedule claims. It remains exploratory and unscheduled.
