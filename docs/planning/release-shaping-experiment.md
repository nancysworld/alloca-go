# Release shaping — evidence and experiment hypothesis

**Status:** Planned evidence note — to be exercised in AG-M2/AG-M5
**Scope:** connect a generic industry workload-shaping pattern to prior
RuntimeIQ-Alloca evidence and define the synthetic hypotheses Alloca-Go will test.
This note makes no claim about any external organisation's timetable, scale, traffic,
architecture, implementation, bottleneck, or measured results.

## 1. Industry context and synthetic workload model

Reservation products can replace one synchronized booking release with rolling
per-session releases so demand opens gradually instead of concentrating at one instant.
This public product-policy pattern motivates an Alloca-Go experiment; it is not evidence
about any external service's internal system or the size of the effect.

For a representative synthetic timetable, Alloca-Go models:

- `[HYPOTHESIS]` approximately 40 sessions in one release horizon;
- `[HYPOTHESIS]` occasional coincident releases containing two to five sessions;
- `[HYPOTHESIS]` a largest release cluster of five sessions;
- `[HYPOTHESIS]` one unusually hot session that can be placed alone or inside a cluster;
- `[HYPOTHESIS]` equal-demand and demand-skewed variants.

These are experiment parameters chosen to expose the difference between a global wave,
dispersed independent authorities, clustered releases, and one hot authority. They are
synthetic and are not presented as observations about an external timetable.

## 2. Prior prototype evidence

`[PRIOR-UNREPRODUCED]` RuntimeIQ-Alloca experiments found that booking latency grew
approximately linearly with concurrent users over the measured range, and that arrival
spread materially changed outcomes. Those experiments established a coarse relationship
between arrival concentration and observed latency, but did **not** isolate the underlying
bottleneck. Candidate causes included application concurrency, database transactions,
connection-pool pressure, lock contention, shared-resource saturation, timeout behaviour,
and retry amplification.

These findings remain prior evidence until reproduced in Alloca-Go. They do not imply
that any external service has the same implementation or bottleneck.

## 3. First-order workload model

Let:

- `D` be the total booking demand that arrives at one synchronized release;
- `d_i` be expected competing demand for session `i`;
- `C_t` be the set of sessions released at time `t`.

The scheduled release load at time `t` is modelled as:

```text
release_load(t) = sum(d_i for i in C_t)
```

Under the synthetic equal-demand model, changing the scheduled peak from all 40 sessions
to a largest cluster of five gives:

```text
new_scheduled_peak / old_scheduled_peak ~= 5 / 40 = 1 / 8
```

The `1/8` value is `[DERIVED]` from the explicitly synthetic `[HYPOTHESIS]` parameters
above. It estimates the ratio of scheduled concurrent demand under equal demand; it is
**not** a measured latency reduction. Fixed request cost, unequal popularity, clustered
user behaviour, retries, and the system's saturation point can make the realised
improvement smaller or larger.

## 4. Hypotheses to test in Alloca-Go

### H1 — release shaping reduces peak concurrency

`[HYPOTHESIS]` Replacing one synchronized release with rolling per-slot releases reduces
the maximum concurrent booking wave approximately in proportion to the demand-weighted
size of the largest release cluster, not simply the total number of daily slots.

### H2 — latency improvement is not necessarily proportional

`[HYPOTHESIS]` If latency can be decomposed as fixed cost plus a contention-dependent
component, release shaping reduces the contention-dependent portion but not fixed
network, parsing, authentication, application, and database costs.

```text
latency ~= fixed_cost + contention_cost(concurrency, arrival_rate, authority)
```

Near saturation, the improvement may be super-linear if lower initial concurrency also
prevents queue growth, timeouts, and retry amplification. Below saturation, the
improvement may be materially less than the concurrency ratio.

### H3 — demand-weighted clustering matters more than session count

`[HYPOTHESIS]` A cluster of several quiet sessions can create less load than one unusually
popular session. Release planning should therefore be evaluated using expected competing
demand per release time, not only the number of sessions in each cluster.

### H4 — isolating a super-hot slot protects shared resources

`[HYPOTHESIS]` Moving an unusually hot slot out of a multi-session release cluster does
not remove that slot's own authority-serialization ceiling, but it reduces competition
with unrelated slots for shared API, connection-pool, CPU, I/O, WAL, and telemetry
capacity. It should improve failure isolation and protect ordinary slots from the hot
slot's burst.

### H5 — the bottleneck moves rather than disappears

`[HYPOTHESIS]` After the application boundary moves to Go and synchronized demand is
reduced, the limiting resource will become easier to identify and may shift among the
load generator, stateless API, PostgreSQL pool acquisition, transaction execution,
slot-row serialization, database CPU/I/O/WAL, or retry/timeout policy.

## 5. Experiment design

Alloca-Go should compare at least these synthetic arrival shapes with the same total
users, slot capacities, demand distribution, client deadlines, and retry policy:

1. **Global synchronized release** — all slots open at one instant.
2. **Uniform rolling release** — slots spread evenly across release times.
3. **Representative clustered release** — synthetic clusters with sizes up to five,
   including a `5, 3, 2, 2, 2, 2, 2` comparison shape and individually released slots.
4. **Demand-weighted cluster shape** — quiet slots may cluster; predicted hot slots are
   separated.
5. **Hot-slot isolation control** — the same hot slot tested alone and inside the largest
   cluster.

For each shape, report:

- offered-load and actual arrival-time distributions;
- concurrent in-flight requests;
- goodput and business refusals;
- p50/p95/p99 latency;
- timeout, unknown-outcome, and replay rates;
- retry amplification;
- API and load-generator saturation;
- pool wait, transaction time, lock wait, and PostgreSQL CPU/I/O/WAL activity;
- per-slot and per-release-cluster outcomes;
- post-run database reconciliation.

The experiment must separate:

- **Layer B:** dispersed work across independent slot authorities;
- **Layer C:** one hot slot's serialization ceiling;
- shared PostgreSQL and fleet limits affecting both layers.

## 6. Interpretation rule

A result supports release shaping only when total demand and client policy are held
constant and the load generator has verified headroom. A lower peak accepted request
rate alone is not sufficient: the comparison must use the measurement contract's
correctness, outcome, SLO, reconciliation, and reproducibility gates.

The industry pattern shows that synchronized release is a plausible product-level
operational concern and workload shaping is a credible intervention to test. Only
Alloca-Go repository-local experiments can establish the size, mechanism, bottleneck,
and limits of the effect for this system.
