#!/usr/bin/env python3
"""Generate the Grafana dashboard from deploy/observability/panels.json.

The dashboard is a build artifact, not a hand-edited file. Grafana's JSON is verbose enough
that a hand-maintained copy drifts from the canonical queries silently — which is the exact
failure TestDashboardMatchesCanonicalPanels exists to catch, and this script exists to prevent.

Run after editing panels.json:

    python3 test/scripts/gen-dashboard.py

$RANGE becomes Grafana's $__rate_interval here. The exporter substitutes a literal window
instead; see panels.json's comment for why the token is repository-owned rather than Grafana's
macro being the source form.
"""
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[2]
PANELS = ROOT / "deploy/observability/panels.json"
OUT = ROOT / "deploy/observability/grafana/dashboards/alloca-frontier.json"

# Which canonical panels share a graph. Grouped by what an operator reads together: the three
# latency quantiles on one axis, pool size against pool usage, and so on.
#
# Panels only share an axis when they share a *scale*. Resident memory used to sit beside CPU,
# goroutine count and GC pause: tens of millions against values below ten, so memory set the
# axis and flattened the other three into a line along the bottom — the panel existed but could
# not be read. It now has its own.
LAYOUT = [
    ("Throughput and goodput", ["throughput", "goodput", "replay_rate"]),
    ("Latency", ["latency_p50", "latency_p95", "latency_p99"]),
    ("Outcomes", ["outcomes"]),
    # Occupancy, lifecycle and acquire cost are three different questions, and the first version
    # of this put them on one graph with a redundant per-unit constant (`pool_max`) on top. On a
    # four-unit rung that rendered as a dozen identically-labelled lines plus four flat ones: the
    # panel existed and answered nothing.
    #
    # Occupancy says whether the pool is populated and saturated. Lifecycle says whether it is
    # churning underneath a steady population — which is where PostgreSQL enters, since
    # construction connects. Acquire cost says what an acquire is paying. §3.13.1's discriminator
    # reads across the last two, so they must be separable at a glance.
    ("Database pool occupancy", ["pool_in_use", "pool_idle", "pool_total"]),
    ("Database pool lifecycle", ["pool_constructing", "pool_new_conns", "pool_destroys"]),
    ("Database pool acquire duration", ["pool_acquire_wait", "pool_empty_acquire_wait"]),
    # Its own graph rather than sharing the one above: seconds-per-second and seconds-per-acquire
    # differ by three orders of magnitude here, so one axis would flatten the series that actually
    # discriminated the degraded cell (§3.13.1).
    ("Database pool mean acquire duration", ["pool_acquire_mean"]),
    ("Process CPU and Go runtime", ["process_cpu", "go_goroutines", "go_gc_pause"]),
    ("Process resident memory", ["process_memory"]),
    # VAL-NEG-7's host view (ag-sept-pr4.md §2.1, §2.5). Four graphs rather than one because the
    # quantities that answer §3.12 are small next to the ones that do not: CPU steal against total
    # busy is a rounding error on a shared axis, and it is the series that would say whether the
    # host was descheduled.
    #
    # The collector set is deliberately wider than these panels (diskstats, netdev, filesystem are
    # collected and not plotted). Collect broadly, panel narrowly: the snapshot retains everything
    # scraped, so a series nobody thought to plot is still recoverable — which is exactly how the
    # pool population question was answered without re-running a cell (§3.13.1).
    ("Host CPU", ["host_cpu_busy"]),
    ("Host CPU stolen and blocked", ["host_cpu_steal"]),
    ("Host run queue", ["host_runqueue"]),
    ("Host memory available", ["host_memory_available"]),
]

DATASOURCE = {"type": "prometheus", "uid": "alloca-prometheus"}

# How a series names itself. Driven by each panel's own metadata rather than by a table here,
# because the table was a second place to forget: a panel added to panels.json without a matching
# entry silently inherited a fixed legend, which is the defect this exists to prevent.
#
# A fixed legend is right for a single-series panel and actively misleading otherwise, and there
# are two ways a panel fans out:
#
#   * **explicitly**, when the query aggregates by a label — `outcomes` is `sum by (outcome)`, and
#     labelling all of them "Outcomes" hid exactly the distinction the panel exists to expose.
#     Such a panel names the label itself, in its `legend` field.
#   * **implicitly**, when the query preserves per-authority cardinality, so Prometheus returns one
#     series per shard group. That was invisible under PR2's single target; Iteration C made every
#     such panel four identically-labelled lines (ag-sept-pr4.md §3). These declare
#     `per_authority: true`.
#
# **Authority first.** `{{authority}} acquired`, not `acquired {{authority}}`, so every panel's
# legend sorts and reads by unit — the point is correlating one authority across graphs without
# relying on colour, and that only works if the identity is in the same place every time.
#
# `{{authority}}` renders empty on the PR2 single-instance path, which has no such label. Harmless
# and deliberate: one series needs no disambiguation, and `{{instance}}` would read as an address
# rather than as the identity that already exists in the placement, the scrape labels and the
# retained evidence.


def legend_for(panel: dict) -> str:
    """The legend a panel's series carry: unit-prefixed when it keeps per-authority cardinality."""
    declared = panel.get("legend")
    if panel.get("per_authority"):
        return f"{{{{authority}}}} {declared or panel['title']}"
    return declared or panel["title"]


def grafana_expr(panel: dict) -> str:
    """Substitute the repository token: the panel's declared range, else Grafana's macro.

    `$__rate_interval` is adaptive and normally right, but it is computed from the datasource's
    single `timeInterval` — which is 1s here, matching the service job. The host job scrapes at
    5s, so the macro resolves shorter than two of *its* scrape intervals and rate() over it
    returns nothing at all: the panel renders "No data" while the series is present and healthy
    (ag-sept-pr4.md §2.4, §3.14.1). A panel whose series is scraped on a different cadence
    declares its own window.
    """
    return panel["expr"].replace("$RANGE", panel.get("range", "$__rate_interval"))


def main() -> None:
    canonical = json.loads(PANELS.read_text())
    by_key = {p["key"]: p for p in canonical["panels"]}

    laid_out = {k for _, keys in LAYOUT for k in keys}
    if missing := set(by_key) - laid_out:
        raise SystemExit(
            f"panels.json defines {sorted(missing)} but LAYOUT places them on no graph; "
            "a panel nobody can see is the same as a panel that does not exist"
        )
    if unknown := laid_out - set(by_key):
        raise SystemExit(f"LAYOUT references unknown panels: {sorted(unknown)}")

    panels, pid, y = [], 1, 0
    for title, keys in LAYOUT:
        targets = [
            {
                "refId": chr(ord("A") + i),
                "datasource": DATASOURCE,
                "expr": grafana_expr(by_key[k]),
                "legendFormat": legend_for(by_key[k]),
                "range": True,
            }
            for i, k in enumerate(keys)
        ]
        panels.append({
            "id": pid,
            "type": "timeseries",
            "title": title,
            "datasource": DATASOURCE,
            "gridPos": {"h": 8, "w": 12, "x": (pid - 1) % 2 * 12, "y": y},
            "fieldConfig": {
                "defaults": {"custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never"}},
                "overrides": [],
            },
            "options": {"legend": {"displayMode": "list", "placement": "bottom", "showLegend": True}},
            "targets": targets,
        })
        pid += 1
        if pid % 2 == 1:
            y += 8

    dashboard = {
        "uid": "alloca-frontier",
        "title": "Alloca-Go — single-instance frontier",
        "description": (
            "AG-Sept diagnostic view. Generated by test/scripts/gen-dashboard.py from "
            "deploy/observability/panels.json — edit that file and re-run, never this one. "
            "Deliberately one dashboard with the scoped panels only (measurement-contract §6 "
            "excludes rich dashboards, alerting and plugins). This is the view you watch while a "
            "sweep runs; the report quotes the CSVs the exporter writes, not this."
        ),
        "tags": ["ag-sept", "generated"],
        "timezone": "utc",
        "editable": False,
        "schemaVersion": 39,
        "refresh": "5s",
        "time": {"from": "now-15m", "to": "now"},
        "panels": panels,
    }

    OUT.write_text(json.dumps(dashboard, indent=2) + "\n")
    print(f"wrote {OUT.relative_to(ROOT)}: {len(panels)} panels, "
          f"{sum(len(p['targets']) for p in panels)} targets")


if __name__ == "__main__":
    main()
