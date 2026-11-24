# Distributed SQL as an alternative for cross-authority booking

**Status:** Exploratory idea — non-normative, unscheduled, and not implemented.  
**Recorded:** 2026-09-28 — Nancy Zhang.

## The question

Could one distributed SQL cluster keep Alloca's user-home and slot-home ownership
axes independently placed while making a booking across them one database
transaction?

This is an alternative to the cross-database protocol discussed in
[Cross-authority booking](cross-authority-booking.md) and
[Dynamic placement and reclamation](dynamic-placement-and-reclamation.md).
The accepted PostgreSQL architecture remains in
[Horizontal database authority and ownership axes](../design/horizontal-database-authority.md),
and [Transaction semantics](../design/transaction-semantics.md) defines current
behaviour. No database migration or Phase 2 protocol has been selected.

## Two possible transactional boundaries

| | Independent PostgreSQL authorities | One CockroachDB cluster |
|---|---|---|
| Placement | User-home and slot-home on separate PostgreSQL writers | User and slot data may occupy different ranges and nodes in the same cluster |
| Booking across homes | Two independent commit domains; application or external coordinator must define atomicity and recovery | One SQL transaction can span rows on multiple ranges and nodes; database coordinates the commit |
| Correctness ownership | Slot capacity at slot-home; schedule serialization and claim validity at user-home | The same logical decisions and ownership axes would still be required |
| Operational boundary | Authorities can be separate database deployments | All participants must be in the **same** transactional cluster for this benefit |

The CockroachDB option changes the meaning of *database authority* used in the
accepted PostgreSQL design. A range or node is not an independent PostgreSQL
commit domain. Splitting logical ownership across ranges would no longer imply
that the application has to commit two separate databases. Two separate
CockroachDB clusters would restore the cross-cluster coordination problem.

Conceptually, reserve could read and change the user's schedule and the slot's
capacity in one transaction, alongside its idempotency record. This does not
assert that the present PostgreSQL schema or Go transaction can be copied over.
The current local reserve path locks the **slot before the user identity**;
the earlier user-first sketch is not a change to that contract.

## What the database would and would not take over

CockroachDB provides atomic transactions across distributed rows, with
serializable isolation by default. Its transaction layer handles the distributed
commit and replication inside one cluster. The application would still own
booking rules, placement keys, scoped idempotency, whole-transaction retries,
ambiguous-result replay, time/expiry rules, and the reserve/confirm/cancel
lifecycle. Contention on one hot slot or one hot user remains.

In particular, Alloca's PostgreSQL `user_time_claims` table uses a GiST
exclusion constraint to enforce non-overlap as a durable backstop. We must
**verify an equivalent database-enforced invariant** in the candidate schema;
serializable isolation alone does not prove that every writer follows the
intended check-and-claim protocol. Check PostgreSQL feature compatibility,
including the range/exclusion constraint and `clock_timestamp()` semantics,
before describing this as a port.

The experiment must also account for distributed transaction latency, retries
under contention, and what happens if a node or client connection fails. One
cluster can simplify application coordination without guaranteeing better
throughput, lower latency, or independent failure domains for the two axes.

## Small comparison experiment

1. Model one `UserRef`, one `SlotRef`, schedule claims, capacity, and scoped
   idempotency with the existing reserve/confirm/cancel semantics. Prove the
   non-overlap constraint or an equivalent database-enforced design first.
2. Place the user and slot data on different ranges/nodes in one CockroachDB
   cluster; verify placement rather than assuming that separate tables imply it.
3. Exercise overlapping claims, the last unit of capacity, same-key replay,
   transaction retries, ambiguous responses, and node loss. Reconcile all
   committed state after each run.
4. Compare with the independent-PostgreSQL-authorities protocol, once designed,
   on correctness, application coordination/recovery code, operational burden,
   and latency/throughput under matched workloads.

This is a learning and architecture comparison, not a replacement for Phase 1.

## References

- [CockroachDB transactions](https://www.cockroachlabs.com/docs/stable/transactions)
- [CockroachDB transaction layer](https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer)
- [CockroachDB PostgreSQL compatibility](https://www.cockroachlabs.com/docs/stable/postgresql-compatibility)
