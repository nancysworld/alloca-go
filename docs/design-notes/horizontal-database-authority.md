# Horizontal database authority — promoted to formal design

**Status:** Superseded as a design note by the formal design document.  
**Formal owner:** [`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md)

This note graduated into the formal design record after Phase 1's placement and booking
contracts were implemented and the architecture terminology was clarified.

The formal document now owns:

- the distinction between **logical authority**, **ownership axis**, and **database authority**;
- why Alloca's three logical authorities form two independent ownership axes;
- the same-database-authority and cross-database-authority booking definitions;
- organisation-home placement and user-home mutation routing;
- Phase 1's local-transaction support boundary and cross-authority refusal;
- Phase 2 compatibility obligations;
- database-authority-aware verification and the horizontal-scaling evidence contract.

Existing references to this path are intentionally preserved while the branch is under review,
so old links do not break. The formal document retained the major section numbering used by the
load-bearing references:

- old §4.1 → formal §4.1;
- old §5.1–§5.5 → formal §5.1–§5.5;
- old §6, §7, §8 and §10 → the same major sections in the formal document.

Use the formal document for every new architectural reference or change.
