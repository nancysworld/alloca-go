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

# The dashboard's sections, and which canonical panels share a graph inside each.
#
# Sections became necessary once the view spanned four different subjects: a reader looking at
# "Process CPU" had no way to know whose process it was, and the answer — the service, as opposed
# to the host two sections below — is exactly the distinction the panel exists to support. The
# grouping matches docs/operations/dashboards.md so the screen and the reading guide are walked in
# the same order.
#
# Panels only share an axis when they share a *unit*; the generator refuses otherwise. Resident
# memory once sat beside CPU, goroutine count and GC pause — tens of millions against values below
# ten — so memory set the axis and flattened the other three into a line along the bottom.
SECTIONS = [
    ("Demand and outcome", [
        ("Throughput and goodput (req/s)", ["throughput", "goodput", "replay_rate"]),
        ("Latency", ["latency_p50", "latency_p95", "latency_p99"]),
        ("Outcomes (req/s)", ["outcomes"]),
    ]),
    # Occupancy, lifecycle and acquire cost are three different questions, and the first version
    # of this put them on one graph with a redundant per-unit constant (`pool_max`) on top. On a
    # four-unit rung that rendered as a dozen identically-labelled lines plus four flat ones: the
    # panel existed and answered nothing.
    #
    # Occupancy says whether the pool is populated and saturated. Lifecycle says whether it is
    # churning underneath a steady population — which is where PostgreSQL enters, since
    # construction connects. Acquire cost says what an acquire is paying. §3.13.1's discriminator
    # reads across the last two, so they must be separable at a glance.
    #
    # `pool_constructing` sits with occupancy, not lifecycle: it is a connection *count*, and a
    # connection being built is a state the population is in.
    ("Database pool", [
        ("Pool occupancy (conns)", ["pool_in_use", "pool_total"]),
        ("Pool lifecycle (conns/s)", ["pool_new_conns", "pool_destroys"]),
        ("Pool acquire concurrency (s/s)", ["pool_acquire_wait", "pool_empty_acquire_wait"]),
        # Its own graph: seconds-per-second and seconds-per-acquire differ by three orders of
        # magnitude, so one axis would flatten the series that discriminated the degraded cell.
        ("Pool mean acquire duration", ["pool_acquire_mean"]),
    ]),
    # Each title names the service explicitly. "Process CPU" was ambiguous the moment a host
    # section existed beside it, and CPU carries its unit in the title because "cores" is the one
    # thing a reader most often assumes wrongly — it is not a percentage.
    ("Service process (alloca-go units)", [
        ("Service CPU (cores)", ["process_cpu"]),
        ("Service goroutines", ["go_goroutines"]),
        ("Service GC pause p75", ["go_gc_pause"]),
        ("Service resident memory", ["process_memory"]),
    ]),
    # VAL-NEG-7's host view (ag-sept-pr4.md §2.1, §2.5). Four graphs rather than one because the
    # quantities that answer §3.12 are small next to the ones that do not: CPU steal against total
    # busy is a rounding error on a shared axis, and it is the series that would say whether the
    # host was descheduled.
    #
    # The collector set is deliberately wider than these panels (netdev and filesystem are
    # collected and not plotted). Collect broadly, panel narrowly: the snapshot retains everything
    # scraped, so a series nobody thought to plot is still recoverable — which is exactly how the
    # pool population question was answered without re-running a cell (§3.13.1).
    #
    # **The two disk graphs are here because PR4b's result turned on them** (maintainer decision,
    # 2026-08-19). The gap that exposed was not in the collector set: diskstats was scraped and
    # reachable in Prometheus the whole time, so "recoverable from the snapshot" held — but the
    # *cell's* retained panels are what a report cites, and a series absent from them got quoted
    # from a live query instead (ag-sept-pr4.md §3.31). Defining the panels closes the evidence
    # half; plotting them closes the other half, which TestDashboardIsDeliberatelySmall's own
    # comment names: an unplotted series is *recoverable*, not *noticed*, and the next degraded
    # cell has to be read by someone who does not yet know the storage path is a candidate.
    #
    # So the rule needs its second half stated: collect broadly, panel narrowly — and promote a
    # series to a panel the moment a claim rests on it, because a snapshot nobody ships is not
    # evidence a reader can check.
    ("Host (the machine every unit shares)", [
        ("Host CPU busy (cores)", ["host_cpu_busy"]),
        ("Host CPU stolen and blocked (cores)", ["host_cpu_steal"]),
        ("Host run queue (tasks)", ["host_runqueue"]),
        ("Host memory available", ["host_memory_available"]),
        ("Host disk throughput", ["host_disk_write_bytes", "host_disk_read_bytes"]),
        # One graph on purpose. Utilisation near 1.0 means "never idle", not "saturated", and only
        # the queue depth beside it distinguishes the two: PR4b measured ~1.0 at every topology
        # while G4 extracted three times G1's write bandwidth at three to four times the queue
        # depth. Separating them would let a reader take utilisation for a ceiling, which is the
        # misreading this pairing exists to prevent.
        ("Host disk utilisation and queue depth", ["host_disk_util", "host_disk_queue"]),
    ]),
    # PostgreSQL's own view (ag-sept-pr4.md §3.16). The section exists for the first graph: every
    # other instrument in this dashboard can say a backend was slow, and only `wait_event_type` can
    # say what it was blocked on. The rest bound the mechanisms that were named and refuted (§3.15)
    # so the refutations stay re-derivable from a retained cell rather than from container logs.
    #
    # The two checkpoint series share one graph on purpose. The trigger is the distinction that
    # matters — timed at G4, WAL-requested at G1 where one database absorbs all four organisations'
    # writes — and a single summed series would destroy exactly the thing worth reading.
    #
    # No dead-tuple graph, deliberately: `n_dead_tup` comes from the stat_user_tables collector,
    # which hangs a scrape indefinitely under a table lock and is disabled for that reason.
    ("PostgreSQL (per authority)", [
        ("Backends by wait event type", ["pg_wait_events"]),
        ("Active backends", ["pg_backends_active"]),
        ("Checkpoints started (cumulative)", ["pg_checkpoints_timed", "pg_checkpoints_req"]),
        ("Buffers written by backends (per second)", ["pg_buffers_backend"]),
        ("Longest open transaction", ["pg_long_transactions"]),
    ]),
]

# Flattened, for the checks and the generator body that do not care about sections.
LAYOUT = [graph for _, graphs in SECTIONS for graph in graphs]

DATASOURCE = {"type": "prometheus", "uid": "alloca-prometheus"}

# This repository's unit names mapped onto Grafana's.
#
# `unit` in panels.json stays repository-owned and human-readable, because it is also what the CSV
# export and the prose call the series. This table is the translation, in one place, so adding a
# panel does not mean learning Grafana's identifier list.
#
# **Exactly one of the title and the axis states the unit, never both.** Repeating "cores" down
# every gridline is noise once the title says it, so most panels put the unit in the title and take
# "short" — plain numbers, with K/M only where the magnitude needs it.
#
# Two units go the other way, because Grafana's formatting does real work rather than appending a
# word, and it *scales*:
#
#   bytes -> 22.9 MiB, not 24000000
#   s     -> 100 µs,   not 0.0001
#
# For those, the axis is the one that states it and the title does not. The scaled prefix is also
# the honest label: a title reading "(s)" above an axis reading "150 µs" names a unit the reader is
# not looking at, and "(bytes)" above "22 MiB" is the same mistake three orders of magnitude up.
# So a panel whose GRAFANA_UNIT is "bytes" or "s" carries no unit suffix in SECTIONS.
GRAFANA_UNIT = {
    "bytes": "bytes",
    # Bytes per second. One of the units that scales its own axis — "18.1 MiB/s" rather than
    # 19000000 — so it joins SCALING_UNITS automatically and the disk throughput title carries no
    # unit suffix.
    "Bps": "Bps",
    "s": "s",
    "req/s": "short",
    "cores": "short",
    "conns": "short",
    "conns/s": "short",
    "count": "short",
    "tasks": "short",
    "s/s": "short",
    "backends": "short",
    "checkpoints/s": "short",
    "buffers/s": "short",
    "ratio": "percentunit",
    # Dimensionless, and deliberately not percentunit. Disk utilisation happens to sit near 1.0,
    # but average queue depth shares its graph and runs to 5; rendering that as "500%" would name
    # a quantity nobody measured.
    "none": "short",
}

# The Grafana units that scale their own axis, and so own the unit label rather than sharing it
# with the title. Derived from the table rather than listed beside it, so a unit added above lands
# on the right side of the rule without anyone remembering this line exists: "short" prints the
# number alone, and "percentunit" repeats a sign per gridline, which is the noise case. Everything
# else rewrites the magnitude — 22.9 MiB, 100 µs — and that scaled form is what the reader sees.
SCALING_UNITS = {g for g in GRAFANA_UNIT.values() if g not in {"short", "percentunit"}}

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

    # A panel may be exported without being plotted, but it has to say so. "Collect broadly, panel
    # narrowly" already governs which *metrics* are scraped; this is the same rule one level down,
    # for which retained series earn screen space. `pool_idle` is the worked example: it is exactly
    # `total - acquired`, so plotting it added a third oscillating line per unit and no
    # information — while the CSV still wants it, because that is the series §3.13.1 reads.
    #
    # Silence is not enough. An undeclared panel missing from the layout is the "panel nobody can
    # see" defect, so the omission is declared per panel and checked here.
    laid_out = {k for _, keys in LAYOUT for k in keys}
    exported_only = {p["key"] for p in canonical["panels"] if p.get("display") is False}
    if overlap := laid_out & exported_only:
        raise SystemExit(
            f"{sorted(overlap)} set display:false but are placed on a graph; one of the two is wrong"
        )
    if missing := set(by_key) - laid_out - exported_only:
        raise SystemExit(
            f"panels.json defines {sorted(missing)} but LAYOUT places them on no graph; "
            "a panel nobody can see is the same as a panel that does not exist. Set "
            "\"display\": false if it is meant to be exported and not plotted."
        )
    if unknown := laid_out - set(by_key):
        raise SystemExit(f"LAYOUT references unknown panels: {sorted(unknown)}")

    panels, pid, y = [], 1, 0
    for section, graphs in SECTIONS:
        # A Grafana row: a full-width header that names what the graphs under it are about. The
        # view spans four subjects now, and "Process CPU" alone could not say whose process.
        panels.append({
            "id": pid,
            "type": "row",
            "title": section,
            "collapsed": False,
            "gridPos": {"h": 1, "w": 24, "x": 0, "y": y},
            "panels": [],
        })
        pid += 1
        y += 1
        column = 0

        for title, keys in graphs:
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
            # One axis, so one unit. Panels sharing a graph must already share a scale, and a
            # graph whose series disagreed about what the numbers *are* could not be labelled at
            # all — which is what "Process CPU and Go runtime" was: cores, a goroutine count and a
            # duration on one unlabelled axis, where the count set the scale and flattened the
            # other two.
            units = {by_key[k]["unit"] for k in keys}
            if len(units) > 1:
                raise SystemExit(
                    f"panel {title!r} plots {sorted(units)} on one axis; a shared axis needs a "
                    "shared unit, or the axis label is a lie and the smallest series is invisible"
                )
            unit = GRAFANA_UNIT.get(next(iter(units)))
            if unit is None:
                raise SystemExit(
                    f"panel {title!r} uses unit {next(iter(units))!r}, which GRAFANA_UNIT does "
                    "not map; add it rather than letting the axis render raw numbers"
                )
            # The half of the one-unit rule that has no exceptions: a scaling axis already names
            # the unit, in the scaled form the reader is actually looking at, so the title must
            # not name it again in the base form. Checked rather than only described, because the
            # titles were written by hand above and a suffix is exactly the kind of thing that
            # gets copied from the panel beside it. The other half is deliberately not checked —
            # "Service goroutines" is right to carry no "(count)".
            if unit in SCALING_UNITS and title.rstrip().endswith(")"):
                raise SystemExit(
                    f"panel {title!r} states a unit in its title while its axis scales "
                    f"({unit!r}: 22.9 MiB, 100 µs). The axis is the honest label — a title "
                    "reading '(s)' above an axis reading '150 µs' names a unit nobody is "
                    "looking at. Drop the suffix from SECTIONS."
                )

            panels.append({
                "id": pid,
                "type": "timeseries",
                "title": title,
                "datasource": DATASOURCE,
                "gridPos": {"h": 8, "w": 12, "x": column * 12, "y": y},
                "fieldConfig": {
                    "defaults": {
                        "unit": unit,
                        "custom": {
                            "lineWidth": 1, "fillOpacity": 8, "showPoints": "never",
                        },
                    },
                    "overrides": [],
                },
                "options": {
                    "legend": {
                        "displayMode": "list", "placement": "bottom", "showLegend": True,
                    },
                },
                "targets": targets,
            })
            pid += 1
            # Two graphs per row of the grid; a section starts a fresh line rather than continuing
            # the previous one's, so a section boundary is visible as well as titled.
            column += 1
            if column == 2:
                column = 0
                y += 8
        if column:
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
    graphs = [p for p in panels if p["type"] != "row"]
    print(f"wrote {OUT.relative_to(ROOT)}: {len(SECTIONS)} sections, {len(graphs)} graphs, "
          f"{sum(len(p['targets']) for p in graphs)} targets")


if __name__ == "__main__":
    main()
