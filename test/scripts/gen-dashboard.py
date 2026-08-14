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
    ("Database pool acquire cost", ["pool_acquire_wait", "pool_empty_acquire_wait"]),
    ("Process CPU and Go runtime", ["process_cpu", "go_goroutines", "go_gc_pause"]),
    ("Process resident memory", ["process_memory"]),
]

DATASOURCE = {"type": "prometheus", "uid": "alloca-prometheus"}

# Panels whose query returns more than one series, and how each series names itself.
#
# A fixed legend is right for a single-series panel and actively misleading for these. There are
# two ways a panel fans out, and only the first was handled originally:
#
#   * **explicitly**, when the query aggregates by a label — `outcomes` is `sum by (outcome)`, and
#     labelling all of them "Outcomes" hid exactly the distinction the panel exists to expose;
#   * **implicitly**, when the query is not aggregated at all, so Prometheus returns one series
#     per scraped target. That was invisible under PR2, which scraped one target. Iteration C
#     scrapes one per shard group, and every unaggregated panel silently became four
#     identically-labelled lines (ag-sept-pr4.md §3).
#
# So an implicit-fan-out panel names its unit, and a panel carrying several metrics also names
# which metric each line is — `{{authority}}` alone on the occupancy graph would give three lines
# per unit that all read "authority-1".
#
# `{{authority}}` renders empty on the PR2 single-instance path, which has no such label. That is
# harmless there and deliberate: one series needs no disambiguation, and the alternative
# (`{{instance}}`) reads as an address rather than as the thing the reader is comparing.
MULTI_SERIES_LEGEND = {
    "outcomes": "{{outcome}}",

    "pool_in_use": "acquired {{authority}}",
    "pool_idle": "idle {{authority}}",
    "pool_total": "total {{authority}}",
    "pool_constructing": "constructing {{authority}}",
    "pool_new_conns": "new {{authority}}",
    "pool_destroys": "destroyed {{authority}}",
    "pool_acquire_wait": "acquire {{authority}}",
    "pool_empty_acquire_wait": "empty-acquire {{authority}}",

    "process_cpu": "cpu {{authority}}",
    "process_memory": "rss {{authority}}",
    "go_goroutines": "goroutines {{authority}}",
    "go_gc_pause": "gc p75 {{authority}}",
}


def grafana_expr(expr: str) -> str:
    """Substitute the repository token with Grafana's adaptive macro."""
    return expr.replace("$RANGE", "$__rate_interval")


def legend_for(key: str, title: str) -> str:
    """The legend a series should carry: its label value when the query fans out, else the title."""
    return MULTI_SERIES_LEGEND.get(key, title)


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
                "expr": grafana_expr(by_key[k]["expr"]),
                "legendFormat": legend_for(k, by_key[k]["title"]),
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
