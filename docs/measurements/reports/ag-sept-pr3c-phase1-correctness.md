# AG-Sept PR3c — Phase 1 correctness and failure isolation

> ## The conclusion
>
> **Two independent writable authorities compose without weakening any accepted transaction
> semantic, and one of them failing does not reach the other.**
>
> Across two passes of a five-cell matrix, every reconciliation check passed on both
> authorities and in aggregate — 11 of 11 per cell, per pass — with each authority's rows
> counted only over the organisations the placement map gives it. The supported booking path,
> the cross-authority refusal, the misrouting refusal and the confirm/cancel ownership check
> all behave as their contracts state.
>
> With one authority's database stopped mid-run: its unit reported `503` while its peer
> reported `200` and kept booking; the affected unit's requests took bounded infrastructure
> outcomes; **no request failed over to the surviving writer**, and no organisation holds a row
> on an authority that does not own it. Restoration needed no compensating write anywhere.
>
> In one further run of the same cell, the fault produced a genuinely ambiguous commit and the
> post-run resolution pass settled it: that run's two accounting populations differ by exactly
> the one replay — 36,794 measured requests against 36,795 counted by the two units — while the
> databases hold 35,982 rows for 35,982 final logical mutations. That is
> `measurement-contract.md` §12 on a live fault rather than only in tests. It is retained
> separately, as one observation and not a rate (§5.2).
>
> **No throughput multiplier is claimed, and none can be from this machine.** Both authorities,
> both units, the generator and the databases share one 10-vCPU WSL2 allocation. The runs here
> are correctness experiments at concurrency 8; the resource figures in §6 are retained for the
> Iteration B review, not offered as a capacity result.
>
> Evidence: [`pr3c-phase1/pass-1/`](../pr3c-phase1/pass-1/) ·
> [`pr3c-phase1/pass-2/`](../pr3c-phase1/pass-2/). Reproduce with
> [§8](#8-reproducing-this).

**Status:** the PR3c exit gate is discharged for correctness and failure isolation. What is
*not* discharged, and is not claimed: INV-21's acknowledgement-lost half (§7.3), and any
statement about capacity, scaling or the next frontier (§7.1).

All figures are `[MEASURED]` from the artifacts in
[`../pr3c-phase1/`](../pr3c-phase1/) unless labelled otherwise. Every number is derived from a
retained `run.json`, `verdict.json`, `.prom` scrape or row census; none is quoted from a
terminal. Both passes are reported wherever they differ, because a single load reading is
provisional under the measurement contract even when the property it supports is a gate.

**Certification.** Every run reached `quotability.level: local` — the floor of the ladder and
the highest level available here, since generator and service are co-resident
([`measurement-contract.md`](../../design/measurement-contract.md) §13). Both binaries and the
image are stamped from a clean tree at `2991940`, and the units were bound to the observed
image before any measured request:

| Provenance field | Value |
|---|---|
| `service_commit_sha` / `generator_commit_sha` | `2991940…` / `2991940…`, both `source_modified: false` |
| `image_id` | `sha256:3da62f4e2463853a…`, one image across both units |
| `unit_count` / `authority_count` | 2 / 2 |
| `routing_version` / `placement_digest` | `pr3b-v1` / `fb42ec8e5c1c27e2…` |
| `topology_disagreement` / `service_identity_drift` | empty / empty |
| `postgres_version` / `pool_max_conns` / `server_gomaxprocs` | 16.14 / 10 / 10 |

## 1. What was run

The topology is the PR3b two-authority stack
([`container-topology.md`](../../operations/container-topology.md)): `authority-1` owning
`org-a` and `org-c`, `authority-2` owning `org-b` and `org-d`, one placement document mounted
into both units. The fixture is 1,200 slots per organisation at capacity 20 — 96,000 units in
all — re-seeded before every cell, because a cell that starts on a used fixture measures
capacity exhaustion while passing every gate.

| Cell | Shape | Bound | Discharges |
|---|---|---|---|
| `controls` | five request-level assertions | — | VAL-COR-2, VAL-COR-3, VAL-COR-5 |
| `correctness` | `multi-org-dispersed` | 2,000 requests | VAL-COR-1, VAL-COR-2 |
| `distribution` | `hot-organisation`, `org-a` | 2,000 requests | organisation-to-authority distribution |
| `refusal` | `cross-authority-control` | 2,000 requests | VAL-COR-4 |
| `failure-isolation` | `multi-org-dispersed`, one authority stopped | 40 s | VAL-FAIL-1, VAL-COR-6 |

Each load cell is scraped per unit before and after, verified immediately, and its verdict
retained. The scrapes are taken once the generator has **exited**, not when the workload ends,
because the resolution pass sends requests after the measured interval closes (§5.3).

## 2. Correctness on independent authorities `[MEASURED]`

The `correctness` cell drives supported traffic across both authorities: same-organisation and
colocated cross-organisation bookings, routed by user organisation.

| | pass 1 | pass 2 |
|---|---:|---:|
| completed / goodput | 2,000 / 2,000 | 2,000 / 2,000 |
| requests served by `authority-1` / `authority-2` | 1,000 / 1,000 | 1,000 / 1,000 |
| reconciliation checks passed | 11 / 11 | 11 / 11 |
| p50 / p99 latency (ms) | 3.65 / 8.50 | 3.57 / 8.25 |

Every request was admitted; no refusal, no infrastructure outcome, no replay. The per-unit
split comes from the units' own scrapes rather than from the client, which carries no authority
dimension by design ([`ag-sept-pr3.md`](../../development/implementation/ag-sept-pr3.md) §3.2).

**The five request-level controls** ([`pass-1/experiments.txt`](../pr3c-phase1/pass-1/experiments.txt)),
each asserted rather than eyeballed, and each identical in both passes:

| Control | Result | Why it is not the one above it |
|---|---|---|
| same-organisation booking on its own authority | `200 admitted_success` | — |
| colocated cross-organisation booking (`org-a` user, `org-c` slot) | `200 admitted_success` | the Phase 1 policy compares *resolved authorities*, so this must keep working — INV-13 |
| cross-authority booking (`org-a` user, `org-b` slot) | `409 business_refusal / cross_authority_unsupported` | policy, applied by the unit that understood the request |
| misroute (`org-b` user sent to `authority-1`'s unit) | `400 invalid_request`, misroute counter +1 | the transport edge, refusing a request that reached the wrong unit |
| confirm and cancel with a wrong `UserRef` | `404 business_refusal / unknown_target` (both) | a domain answer about a reservation that exists |

The misroute control is the one that makes REQ-ROUTE-1 a property of the *service* rather than
of the generator's routing table, which is why it asserts the counter as well as the outcome: a
400 that is really a misconfiguration must not hide among client errors. The ownership control
then confirms the owner can still confirm afterwards (`200`), so the two refusals did not
consume the reservation.

## 3. The cross-authority refusal, as its own evidence class `[MEASURED]`

| | pass 1 | pass 2 |
|---|---:|---:|
| completed | 2,000 | 2,000 |
| `business_refusal / cross_authority_unsupported` | 2,000 | 2,000 |
| goodput | 0 | 0 |
| per-unit split | 1,000 / 1,000 | 1,000 / 1,000 |

Every request was refused, at the unit owning the *user's* organisation, and **no partial
booking state was created**: the aggregate persisted counts are unchanged by this cell and its
verdict reconciles at zero fresh mutations. It is reported separately and never mixed into the
supported workload — a refusal is a correct answer, not a failure, and averaging the two would
describe neither (VAL-COR-4).

## 4. Organisation-to-authority distribution `[MEASURED]`

The one-hot cell puts the whole load on `org-a`, which `pr3b-v1` places on `authority-1`:

| | pass 1 | pass 2 |
|---|---:|---:|
| requests served by `authority-1` | 2,000 | 2,000 |
| requests served by `authority-2` | **0** | **0** |
| `authority-2` CPU over the cell | 0.00 s | 0.00 s |

An idle peer is the point: placement decides where work lands, and an authority that owns none
of the loaded organisations does none of the work. The `correctness` cell is the companion
reading — 1,000/1,000 — so the two together show the distribution *following the map* rather
than following the generator's own balance.

## 5. Failure isolation `[MEASURED]`

**The fault is a stopped container**, `alloca-authority-2-db`, stopped 10 s into a 40 s window
and started again 15 s later, inside the same window. The failure mode is named because the
outcome depends on it: a stopped container drops packets rather than refusing them, and a
killed container or a database refusing connections classifies differently
([`ag-sept-pr3.md`](../../development/implementation/ag-sept-pr3.md) §6b).

| | pass 1 | pass 2 |
|---|---:|---:|
| completed / goodput | 32,543 / 31,867 | 33,016 / 32,230 |
| `internal_failure` | 372 | 482 |
| `timeout_server` | 304 | 304 |
| requests to `authority-1` / `authority-2` | 16,272 / 16,271 | 16,508 / 16,508 |
| infrastructure outcomes on `authority-1` | **0** | **0** |
| infrastructure outcomes on `authority-2` | 676 | 786 |
| reconciliation checks passed | 11 / 11 | 11 / 11 |

### 5.1 What isolation looks like from each side

**The unaffected authority did not notice.** `authority-1` served 16,272 requests in pass 1 and
16,508 in pass 2 with **zero** infrastructure outcomes and at most 0.01 s of pool acquire-wait.
Its readiness stayed `200` throughout while the affected unit reported `503`
([`readiness-during-fault.txt`](../pr3c-phase1/pass-1/failure-isolation/readiness-during-fault.txt)).

**The affected authority took bounded outcomes and recovered.** Every failed request landed in
the closed outcome set — `internal_failure` where the pool already held a broken connection,
`timeout_server` where a new connection could not be established — and the unit resumed serving
its own organisations after the database returned, with no operator action beyond starting the
container.

**No request failed over to the surviving writer.** The row census taken after each pass shows
each authority holding rows for its own organisations only
([`authority-1-rows-by-organisation.txt`](../pr3c-phase1/pass-1/failure-isolation/authority-1-rows-by-organisation.txt),
[`authority-2-rows-by-organisation.txt`](../pr3c-phase1/pass-1/failure-isolation/authority-2-rows-by-organisation.txt)):
`org-a` and `org-c` on `authority-1`, `org-b` and `org-d` on `authority-2`, nothing else on
either. The verdict already implies this — each authority is counted only over the
organisations the placement gives it, so a row written to the wrong writer would be excluded
from both scopes and the aggregate would come up short — but the census asks the databases
outright.

**Restoration required no compensating write on the unaffected authority.** Nothing was
replayed, reconciled or cleaned up on `authority-1`; the only post-restoration work anywhere
was the single same-key replay in §5.2, on the authority that had failed.

### 5.2 One real ambiguous commit, resolved — VAL-COR-6 on a live fault

**Neither retained pass produced an ambiguous commit.** Of nine runs of this cell driven on
2026-08-11, exactly one did, and that run is retained separately as
[`failure-with-ambiguity/`](../pr3c-phase1/failure-with-ambiguity/) — same script, same commit,
same image, same fault timing. It is reported as one observation of the contract holding on a
real fault, **not as a rate**, and VAL-COR-6's accounting does not depend on it: the validation
plan discharges that on deterministic end-to-end tests precisely so no experiment has to
produce an ambiguous commit to order.

That run's fault produced exactly one `unknown_replayable`. The generator replayed it under its
own idempotency key after the authority returned:

```json
{ "operation": "reserve", "user_organisation_id": "org-d", "user_id": "u-20623",
  "idempotency_key": "multi-org-dispersed-20623-reserve",
  "outcome": "admitted_success", "replay": false }
```

`replay: false` means the original attempt had **not** committed and the resolution performed
the mutation itself — VAL-COR-6's second state, arriving on its own rather than by
construction. The generator reported `replayed 1 ambiguous mutation(s), 0 still unresolved`.

The accounting is where this matters, and the verdict shows both populations in one place:

| Quantity | Value | What it is |
|---|---:|---|
| measured `completed_requests` | 36,794 | the measurement population: what the client saw inside the interval |
| measured goodput | 35,981 | **excludes** the resolved mutation — the client received no definite success for it inside the interval |
| server-counted requests | 36,795 | the reconciliation population: measured **+ 1** resolution attempt |
| fresh admitted reserves | 35,982 | 35,981 measured + 1 established by the resolution |
| live reservations / claims / idempotency records | 35,982 / 35,982 / 35,982 | what the two databases actually hold |

The two populations differ by exactly one request, in the direction the contract requires:
post-run resolution changed what is known about final logical state without rewriting a single
measured field. An implementation that folded the replay into measured goodput would report
35,982 there and reconcile just as cleanly — which is why the separation is a contract rather
than an implementation detail (`measurement-contract.md` §12).

In the two retained passes the resolution pass was correctly a no-op, and their verdicts show
the two populations coinciding, as they must when nothing was ambiguous: 32,543 completed
against 32,543 server-counted in pass 1, 33,016 against 33,016 in pass 2.

## 6. Resource evidence, retained for the Iteration B review `[MEASURED]`

Retention, not an experiment: these are figures the runs above already produced, kept so the
review can judge where the next limiting boundary probably sits without re-running anything.
Both passes, so the spread is visible rather than asserted.

| Cell | Unit | CPU | Pool acquires | Acquire-wait |
|---|---|---:|---:|---:|
| correctness (2,000 req) | `authority-1` | 0.70 / 0.68 s | 1,000 / 1,001 | 0.00 s |
| correctness | `authority-2` | 0.72 / 0.70 s | 1,000 / 1,001 | 0.00 s |
| failure isolation (40 s) | `authority-1` | 11.92 / 12.11 s | 16,281 / 16,517 | 0.01 / 0.00 s |
| failure isolation | `authority-2` | 11.46 / 11.74 s | 15,611 / 15,739 | **40.45 / 44.65 s** |

Read with care, and with §7.1:

- **at concurrency 8 the pool is not contended on a healthy authority** — acquire-wait is
  0.00–0.01 s across every healthy cell in both passes. PR2's frontier was found at far higher
  concurrency, where acquire-wait was the dominant term; these runs say nothing about that
  regime and were not driven into it;
- **service CPU is roughly 0.30 cores per unit** over the failure window (11.92 s of a 40 s
  window), against WSL2's 10-vCPU allocation shared by everything. Whether service compute is
  the next frontier is exactly the question the Iteration B review must answer, and this is an
  input to it rather than an answer;
- **the 40–45 s of acquire-wait on the affected authority is the outage**, accumulated by
  requests waiting on connections that could not be established. It is also the loosest figure
  here — an 11% spread across two faults of identical timing — and its shape is the open lead
  in §7.2.

## 7. What this does not establish

### 7.1 No capacity, throughput or scaling claim

Both authorities, both service units, both databases and the generator share one 10-vCPU WSL2
allocation ([`environment.md`](../environment.md)). Authority composition cannot be quoted as a
capacity multiplier from this machine, and nothing here attempts it: the cells are bounded at
2,000 requests or 40 seconds at concurrency 8, sized so that every request can succeed against
the fixture rather than to find a limit. PR2's unexplained ~2× excursions are still open, so
any single throughput reading from this workstation carries that caveat regardless.

### 7.2 Two reproducible observations that are not diagnoses

**`timeout_server` was exactly 304 in every script-driven run of this cell** — both retained
passes, the separately retained ambiguity run, and six further runs during harness validation
that are not retained. Across those runs `internal_failure` varied between 372 and 508 and
total volume by 13%, while 304 did not move once. The one run whose fault was hand-timed rather
than script-timed produced a different count, so the figure appears to be fixed by the fault's
*timing* rather than by the run's size. A quantity that is bit-identical across runs of
different volume is structural rather than incidental. It is not explained here, and it is
cheap to notice now and expensive to rediscover later.

**The worst client-observed latency was 503.1 ms and 502.7 ms** in the two passes, against a 5 s
server deadline and a 500 ms `db_acquire_cap`. That is consistent with the acquisition cap
bounding the wait when the pool is warm — and it points the *opposite* way from the single
observation recorded in `ag-sept-pr3.md` §6b, where an idle request against a stopped authority
took ~5 s. The plausible reconciliation is warm pool versus new connection, but that is a
hypothesis, not a diagnosis: it needs the investigation §6b asks for, and neither reading should
be used to change a timeout budget.

### 7.3 INV-21's acknowledgement-lost half is still unproven

PR3b proved the narrower fault — a `COMMIT` attempted on an already-terminated session
classifies conservatively. The remaining half, *commit landed but the acknowledgement was
lost*, needs something interposed between client and server that drops the reply, and a generic
authority shutdown does not produce it. The single ambiguous commit in §5.2 is **not** that
proof: it resolved `replay=false`, meaning the original had not committed at all. The invariant
register still says so.

## 8. Reproducing this

```sh
make image                                                  # clean tree required
make image-provenance                                       # want modified=false
make topo-up
make topo-deployment > test/results/deployment.json
go build -o bin/alloca-load ./cmd/alloca-load
go build -o bin/alloca-verify ./cmd/alloca-verify
test/scripts/pr3c-experiments.sh all                        # one pass of the matrix
make topo-down
```

The script seeds, scrapes, drives, injects the fault, resolves, verifies and writes the row
census; its preflight refuses a generator or an image stamped from a modified tree, which is
the failure that otherwise surfaces only as `quotability.level: none` after the runs. Cell
selectors (`controls`, `correctness`, `distribution`, `refusal`, `failure`) run one at a time.

Each cell directory holds:

| File | Why it is kept |
|---|---|
| `run.json` | client totals, the manifest, `ambiguity_resolutions`, and the run's own certification |
| `verdict.json` | the per-authority and aggregate reconciliation checks that make the run admissible |
| `s1-baseline.prom`, `s1-after.prom`, `s2-*.prom` | each unit's own scrape pair, differenced separately before summing |
| `authority-N-rows-by-organisation.txt` | the row census behind §5.1 (failure cell only) |
| `readiness-during-fault.txt` | both units' `/readyz` while the authority was down (failure cell only) |
| `generator-output.txt` | what the resolution pass reported |

with `experiments.txt` at the top of each pass recording the control assertions, the artifact
identity, and the fault timings.
