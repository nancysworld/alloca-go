# Release shaping — evidence and experiment hypothesis

**Status:** Planned evidence note — to be exercised in AG-M2/AG-M5
**Scope:** record an external workload-policy observation, connect it to prior
RuntimeIQ-Alloca evidence, and define the hypotheses Alloca-Go will test. This note does
not claim access to any organisation's internal architecture, traffic, implementation,
or measured results.

## 1. External real-world observation

In July 2026, a consumer fitness-booking service announced a change from one daily
fixed booking-opening time to **per-class rolling release**: each class opens for
booking at its own scheduled start time, nine days in advance. The stated purpose was
to spread booking activity throughout the day and reduce slow-loading periods caused
by many users entering the app together.

A manual inspection of one representative daily timetable found approximately 40
classes, with some coincident start times:

- one cluster of five classes;
- one cluster of three classes;
- approximately five clusters of two classes;
- the remaining classes opening individually.

This is an observed product-policy and timetable shape only. It is **not** evidence of
the service's backend design, actual concurrency, traffic distribution, latency
improvement, bottleneck, or capacity.

## 2. Prior prototype evidence

`[PRIOR-UNREPRODUCED]` RuntimeIQ-Alloca experiments found that booking latency grew
approximately linearly with concurrent users over the measured range, and that arrival
spread materially changed outcomes. Those experiments established a coarse relationship
between arrival concentration and observed latency, but did **not** isolate the underlying
bottleneck. Candidate causes included application concurrency, database transactions,
connection-pool pressure, lock contention, shared-resource saturation, timeout behaviour,
and retry amplification.

These findings remain prior evidence until reproduced in Alloca-Go. They do not imply
that the external service has the same implementation or bottleneck.

## 3. First-order workload model

Let:

- `D` be the total booking demand that previously arrived at one synchronized release;
- `d_i` be expected competing demand for class `i`;
- `C_t` be the set of classes released at time `t`.

The scheduled release load at time `t` is modelled as:

```text
release_load(t) = sum(d_i for i in C_t)
```

Under the simplifying assumptions that all classes have equal demand and capacity, the
largest observed cluster changes the scheduled peak from approximately 40 classes to 5:

```text
new_scheduled_peak / old_scheduled_peak ~= 5 / 40 = 1 / 8
```

This `1/8` value is `[DERIVED]` from the observed timetable counts under an equal-demand
model. It is **not** a measured latency reduction. Fixed request cost, unequal class
popularity, clustered user behaviour, retries, and the system's actual saturation point
can make the realised improvement smaller or larger.

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

### H3 — demand-weighted clustering matters more than class count

`[HYPOTHESIS]` A cluster of several quiet classes can create less load than one unusually
popular class. Release planning should therefore be evaluated using expected competing
demand per release time, not only the number of classes in each cluster.

### H4 — isolating a super-hot slot protects shared resources

`[HYPOTHESIS]` Moving an unusually hot slot out of a multi-class release cluster does not
remove that slot's own authority-serialization ceiling, but it reduces competition with
unrelated slots for shared API, connection-pool, CPU, I/O, WAL, and telemetry capacity.
It should improve failure isolation and protect ordinary slots from the hot slot's burst.

### H5 — the bottleneck moves rather than disappears

`[HYPOTHESIS]` After the application boundary moves to Go and synchronized demand is
reduced, the limiting resource will become easier to identify and may shift among the
load generator, stateless API, PostgreSQL pool acquisition, transaction execution,
slot-row serialization, database CPU/I/O/WAL, or retry/timeout policy.

## 5. Experiment design

Alloca-Go should compare at least these arrival shapes with the same total users, slot
capacities, class-demand distribution, client deadlines, and retry policy:

1. **Global synchronized release** — all slots open at one instant.
2. **Uniform rolling release** — slots spread evenly across release times.
3. **Observed-cluster shape** — cluster sizes approximately `5, 3, 2, 2, 2, 2, 2`, with
   remaining slots released individually.
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

The external policy change is evidence that synchronized release is a real product-level
operational concern and that workload shaping is a plausible intervention. Only
Alloca-Go repository-local experiments can establish the size, mechanism, bottleneck,
and limits of the effect for this system.
