# Design notes

Design notes capture focused investigations and recommendations that may later be promoted
into normative design documents or ADRs.

- [`authoritative-time-in-a-scaled-service.md`](authoritative-time-in-a-scaled-service.md) — recommends PostgreSQL transaction time for correctness-sensitive release, expiry, and slot-close decisions across horizontally scaled API instances.
- [`user-schedule-non-overlap.md`](user-schedule-non-overlap.md) — records the cross-resource invariant that one identity cannot hold overlapping booking claims, and the exclusion-constraint design that enforces it (accepted; implemented by AG-M1 PR4).
- [`horizontal-database-authority.md`](horizontal-database-authority.md) — defines the two-phase database-scaling strategy: organisation-home authorities and same-authority booking in AG-Sept, with a compatible path to cross-authority coordination later.
