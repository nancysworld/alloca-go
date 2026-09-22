# Dynamic placement and reclamation of booking state

**Status:** Exploratory idea — non-normative, unscheduled, and not implemented.  
**Recorded:** 2026-09-22 — Nancy Zhang.

## Purpose and boundary

Explore how one organisation could span multiple writable database authorities,
and how those authorities could reuse capacity as slots, memberships, and
schedule claims leave the active working set.

The proposal combines two ideas:

1. Decouple slot placement from user-schedule placement, coordinating each booking
   with its slot-home and user-home.
2. Allocate resources for new work and reclaim them when existing work can safely
   retire, rather than treating every entity as permanently active.

This extends [Cross-authority booking](cross-authority-booking.md). The accepted
architecture remains in [Horizontal database authority and ownership axes](../design/horizontal-database-authority.md);
[Transaction semantics](../design/transaction-semantics.md) owns current local
behaviour. This note proposes future changes to placement granularity and
lifecycle policy; it does not change those contracts or select a protocol.

## 1. Why organisation-affine placement eventually limits growth

A fixed organisation-to-authority mapping leaves two cases unresolved: an
organisation may already exceed one authority's capacity, or grow beyond it.

The proposed escape is to place smaller units independently. An organisation
remains a business grouping; its slots and users need not share one database home.

| State | Candidate placement unit | Responsibility retained at its home |
|---|---|---|
| Slot and its capacity-related lifecycle state | One slot or a group of slots | Capacity safety |
| User identity, schedule claims, and client idempotency | One user or a group of users | Schedule serialization, non-overlap, and stable replay |
| Ownership directory | Entity or partition assignment | Resolve the current writable owner |

Retain the existing logical identities:
`SlotRef = (slot_organisation_id, slot_id)` and
`UserRef = (user_organisation_id, user_id)`. Physical location is separate.

The three logical authorities still form two ownership axes: user-schedule
serialization and claim validity remain colocated at user-home; capacity belongs
to slot-home. A single-user, single-slot booking has at most two participant
database authorities. That does not bound network calls, recovery work, or all
control-plane dependencies to two.

User placement must also be finer than organisation granularity if a large
organisation's users could overload one home. Each individual user's complete
active schedule remains at one authoritative home.

This removes the fixed one-authority ceiling per organisation. It does not make
one hot slot or one hot user parallel, or promise unlimited scale.

## 2. Coordinate the booking before making placement dynamic

The motivating booking sketch is: lock the user's schedule, lock the slot,
validate the interval and capacity, then reserve capacity and create the schedule
claim as one successful operation.

Locks alone cannot make independent database commits atomic. A crash between the
commits can leave only one side durable, whichever side commits first.

Candidate approaches to investigate include:

- **Two-phase commit:** stage and prepare both participants, durably record one
  coordinator decision, then resolve both participants according to it. A durable
  commit decision must be completed after recovery, not reversed because the
  request timed out. Prepared state can retain locks while the decision is
  unavailable. PostgreSQL supplies participant primitives; coordinator durability,
  recovery, and operational handling still need design.
- **Durable tentative claims and an application workflow:** use short local
  transactions to reserve capacity and protect the interval, with explicit pending,
  completion, and release rules. Recovery must resolve partial progress.
  Compensation is not automatically equivalent to an atomic transaction and must
  never temporarily admit overselling or overlapping active commitments.

These are starting points, not an exhaustive menu or an implementation decision.
Two-phase commit is a short database protocol, not a transaction left prepared
for the duration of a user's reservation hold.

Any candidate must define:

- stable operation identity, idempotent participant actions, durable decisions,
  coordinator replacement, and same-key replay after an unknown outcome;
- reserve, confirm, cancel, and expiry, with schedule protection beginning at
  reservation time;
- client-visible state during partial completion: atomic outcome alone does not
  give arbitrary cross-database reads a shared snapshot;
- lock ordering across local and remote paths. The current local path locks slot
  before user; the motivating user-first sketch must not silently replace it;
- authoritative time across databases, expiry races, and protection against delayed
  messages reactivating released or superseded claims;
- failure containment and bounded admission under stalled coordination.

Keep the existing local transaction when both ownership axes resolve to the same
database authority.

## 3. Place new slot occurrences and let existing work drain

Slots provide a useful natural turnover boundary. A synthetic weekly class may
last one hour and accept bookings for one week beforehand. Each occurrence is a
new opportunity to choose placement.

| Occurrence | Owner | Placement policy |
|---|---|---|
| Current occurrence | Authority A | Retain its existing owner |
| Next occurrence | Authority B | Place before booking opens, using available headroom |
| Later occurrence | Authority A or C | Reuse reclaimed capacity or add capacity as needed |

Prefer choosing placement when an occurrence is created, before it accepts
bookings. A future-dated slot that already has bookings is existing live state;
moving it is migration, not fresh allocation.

An overloaded authority can stop receiving new allocations while existing
workload drains. This does not immediately relieve its already active hotspots.

Load influences **placement**; requests for an existing entity follow **ownership**.
A request cannot use an arbitrary quieter writer or fall back to another authority
on failure. Load balancing among service replicas of the same database authority
remains a separate concern.

Future live migration would need safe state transfer, handling of in-flight
operations, versioned routing, and enforced fencing so stale owners cannot continue
writing. A version number without enforcement is insufficient.

Organisation-wide slot listing will also need a read strategy across partitions.
Any discovery index or cached view must leave booking validation with the owner.

## 4. Allocation includes reclamation

Resources are allocated for new work and become reusable when that work retires.
The memory-allocation analogy suggests headroom, fragmentation, reclamation, and
compaction as useful questions. Unlike ordinary memory, this state must also
survive crashes and satisfy recovery, history, and replay obligations.

Distinguish active state, retained history, and eventual deletion. Their policies
and retention periods need not match.

| Entity or state | Candidate retirement condition | What can remain afterward |
|---|---|---|
| Slot | Its mutable booking/event lifecycle and outstanding operations have finished | Booking history and retained outcomes |
| Membership | Membership ends and membership-specific obligations are settled | Other memberships and the user's outstanding commitments |
| Expired hold claim | Temporary protection has ended and no unresolved transition can revive it | Reservation history and replay/recovery records |
| Completed confirmed claim | Its booked interval has ended and no permitted operation can reactivate or extend it | Historical booking record |
| Idempotency or recovery record | Its explicit retention and recovery obligations have ended | Any required historical evidence |

An event ending is a retirement signal, not permission to delete everything
associated with it. Logical inactivity, safe reclamation, and physical resource
recovery are different stages.

A candidate archive process must preserve required history durably before removing
its active copy, recover safely after interruption, and coordinate with lifecycle
mutations. It must preserve references or reconstruction needed by retained
idempotent responses. Define retention before deleting replay records; cleanup
must not silently allow an old request to execute again.

Separating history into another relation can reduce the active relation's working
set. Moving history off the same authority can additionally reduce its storage and
maintenance burden. Row removal does not imply immediate return of physical disk
space. Storage organisation and maintenance policy remain open.

Reusing space within an authority is distinct from retiring that authority.
Scale-in requires all remaining ownership and recovery responsibilities to drain
or move safely.

## 5. User joining, leaving, and switching organisations

Joining happens over time, so newly created user homes can be assigned to
authorities with headroom. Existing spare capacity can be reused; new authorities
can be added when needed. Registration-time placement is a useful opportunity,
not a guarantee that the user's future workload is known.

Member count is only one input. Request rate, contention, active claim count,
storage, and correlated booking bursts can make equally sized populations very
different workloads.

Leaving an organisation may reclaim membership resources, but does not necessarily
retire the user's schedule. A departing member may still have an active booking
with another organisation.

Under the current identity contract, changing `user_organisation_id` creates a
different `UserRef`. Do not rewrite it to follow current membership and assume
schedule continuity. If membership switching must preserve identity, explicitly
model membership separately or design an identity-continuity mechanism. That
product/schema decision is open; a membership change need not move user-home.

Prefer stable homes for existing users, with selective rebalancing if justified.
Short-lived slots can exploit natural turnover much more aggressively than
long-lived user identities.

## 6. Retire past schedule claims while keeping the user active

A user may remain a member for years without needing every historical interval in
the relation that enforces present and future non-overlap.

Two retirement conditions must remain distinct:

- A temporary hold can expire at `expires_at`, before the booked event begins.
- A confirmed claim currently has `expires_at = NULL`. Its candidate retirement
  boundary is the booked interval's `ends_at`, subject to lifecycle and recovery
  rules, not the hold-expiry field.

Current local semantics settle the requesting user's elapsed hold claims when
that user next reserves. Confirmed past intervals need a separate future
retirement policy. Exact current mechanics remain owned by
[Transaction semantics](../design/transaction-semantics.md), section 2.2; this note
does not authorise an age-only deletion path.

The proposed rule is:

> Keep claims needed for current and future non-overlap in the active schedule
> authority; keep completed history under a separate, bounded retention policy.

Retirement must respect concurrent confirmation, cancellation, and any future
rescheduling operation, as well as unresolved distributed transitions.
Background cleanup should reduce retained state, not become a prerequisite for
correct booking decisions.

**Performance hypothesis:** a smaller active claim relation and index could
improve cache efficiency and reduce maintenance cost. Indexed conflict checks do
not necessarily scan history, so a response-time gain must be measured rather
than assumed. Outstanding commitments, rather than account age alone, should
primarily determine the active schedule working set.

## 7. Allocation questions and remaining limits

| Question | What to investigate |
|---|---|
| Capacity estimation | CPU, I/O, storage, connections, contention, and expected request peaks |
| Headroom | Space for bursts, growth, recovery, and placement transitions |
| Fragmentation | Spare resources distributed across authorities but unsuitable for a particular workload |
| Locality | Balance user/slot colocation against skew and distributed-transaction cost |
| Stability | Avoid repeated placement changes as transient load fluctuates |
| Reclamation lag | Measure the delay from logical completion to usable capacity |
| Compaction and scale-in | Drain naturally where possible; migrate remaining long-lived responsibilities when justified |

Aggregate growth can be spread across many independent slots and users, but one
hot slot and one hot user retain their serialization limits. Coordination,
ownership lookup, historical queries, and maintenance introduce costs of their own.
No correctness or performance improvement is claimed as measured here.

## 8. Suggested restart sequence

These are candidate investigations for when work resumes, not scheduled scope.

1. **Fixed placement, two participants:** define the smallest booking protocol and
   compare candidates against existing invariants. Challenge crashes or lost
   responses after every durable transition, overlapping bookings for one user on
   different slot authorities, capacity contention, replay, and confirm/expiry races.
2. **Safe retirement:** define active/history boundaries, retention, and recoverable
   archival for slots, memberships, and schedule claims. Check interrupted archive
   operations, concurrent lifecycle changes, and replay after archival.
3. **Placement of new work:** allocate new slot occurrences and user homes using
   measured headroom, keep existing owners stable, and observe natural draining.
4. **Measure turnover:** use workloads that create and retire work continuously;
   measure active working set, reclamation lag, useful capacity, response time,
   coordination cost, and archive overhead on independently provisioned resources.
5. **Migration only where needed:** investigate moving existing hot partitions or
   consolidating sparse authorities when natural turnover is insufficient.

Before implementation, promote the selected questions through the existing
Goal/Problem, requirements, design, validation, and scheduling process.

## References

- [Cross-authority booking](cross-authority-booking.md) — the earlier two-home idea.
- [Horizontal database authority and ownership axes](../design/horizontal-database-authority.md)
  — accepted ownership and Phase 1/Phase 2 boundary.
- [Transaction semantics](../design/transaction-semantics.md) — current lifecycle,
  locks, identity, time, claims, and replay.
- [Exploration roadmap](../planning/alloca-go-roadmap.md) — unscheduled directions.
- [PostgreSQL: PREPARE TRANSACTION](https://www.postgresql.org/docs/current/sql-prepare-transaction.html)
  — participant primitives and operational cautions for a candidate 2PC approach.
