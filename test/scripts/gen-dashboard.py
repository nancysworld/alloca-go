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
    # Three pool graphs, not one, because the single "pool pressure" graph could not answer the
    # question PR4a §3.13.1 asked of it. Population, cost-per-acquire and blocked-waiting are
    # different quantities in different units, and plotting in-use against a *maximum* invited
    # reading a ceiling as a population. `pool_new_conns` shares the population axis rather than
    # taking a fourth graph: it is connection churn, it is small, and the dashboard's 8-graph cap
    # is a scope bound worth spending deliberately.
    ("Database pool population", ["pool_in_use", "pool_idle", "pool_total", "pool_max", "pool_new_conns"]),
    ("Database pool acquire cost", ["pool_acquire_wait", "pool_empty_acquire_wait"]),
    ("Database pool mean acquire duration", ["pool_acquire_mean"]),
    ("Process CPU and Go runtime", ["process_cpu", "go_goroutines", "go_gc_pause"]),
    ("Process resident memory", ["process_memory"]),
]

DATASOURCE = {"type": "prometheus", "uid": "alloca-prometheus"}

# Panels whose query returns one series per label value, and the label that names them.
#
# A fixed legend is right for a single-series panel and actively misleading for these: the
# `outcomes` query returns one series per outcome, and labelling all of them "Outcomes" hid
# exactly the distinction — admitted success against business refusal against timeout — that
# the panel exists to expose.
MULTI_SERIES_LEGEND = {"outcomes": "{{outcome}}"}


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
