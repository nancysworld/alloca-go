# Design notes

Design notes capture focused investigations and recommendations that may later be promoted
into normative design documents or ADRs.

- [`authoritative-time-in-a-scaled-service.md`](authoritative-time-in-a-scaled-service.md) — recommends PostgreSQL transaction time for correctness-sensitive release, expiry, and slot-close decisions across horizontally scaled API instances.
- [`user-schedule-non-overlap.md`](user-schedule-non-overlap.md) — records the cross-resource invariant that one identity cannot hold overlapping booking claims, and the exclusion-constraint design that enforces it (accepted; implemented by AG-M1 PR4).
- **Horizontal database authority** — **promoted out of this directory.** The two-phase
  database-scaling strategy this note investigated is owned by
  [`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md), which
  retained the note's section numbering, so a citation of the form `§5.2` resolves against the
  formal design unchanged. Cite the formal design.
