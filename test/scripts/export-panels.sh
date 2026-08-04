#!/usr/bin/env bash
#
# Export a cell's evidence: every canonical panel as a CSV, plus a Prometheus TSDB snapshot.
#
# The Grafana dashboard is the diagnostic view; this is what a report quotes. A screenshot is
# not re-derivable, and re-querying later cannot survive the retention window — a CSV beside
# the snapshot it came from can be checked by anyone, which is what measurement-contract §5.3
# means by quoting from raw artifacts.
#
# Queries come from deploy/observability/panels.json, the same source the dashboard is
# generated from, with $RANGE substituted for the literal window recorded in that file. The
# window is written into the cell directory because a rate over 5s and a rate over 60s are
# different measurements and a CSV that does not say which is not reproducible.
#
#   export-panels.sh <cell-dir> <start-rfc3339> <end-rfc3339>
#
# The window is the *measured phase*, not the whole cell: warm-up traffic must not appear in a
# figure the report quotes.
set -euo pipefail

CELL="${1:?usage: export-panels.sh <cell-dir> <start> <end>}"
START="${2:?missing measured-phase start (RFC3339)}"
END="${3:?missing measured-phase end (RFC3339)}"

PROM="${PROM_URL:-http://localhost:9091}"
PANELS="${PANELS_JSON:-deploy/observability/panels.json}"
CONTAINER="${PROM_CONTAINER:-alloca-prometheus}"

mkdir -p "$CELL/panels"

RANGE=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['export_range'])" "$PANELS")

# Step is the resolution of the exported series, and it is deliberately finer than the rate
# range. Matching step to range would give non-overlapping windows — tidier in principle — but
# a 60s cell at a 15s range then yields four points, which cannot show the shape the export
# exists to preserve: a pool saturating before throughput flattens, or latency climbing while
# goodput holds. The headline scalars come from run.json regardless; these files are for the
# curve.
#
# Consecutive points therefore share samples, which smooths the series. That is a property of
# the export, not of the measurement, so it is recorded in index.json beside the data rather
# than left for a reader to infer.
STEP="${EXPORT_STEP:-5s}"

echo "export: window ${START} .. ${END}, rate range [${RANGE}], step ${STEP}"

python3 - "$PANELS" "$CELL" "$PROM" "$START" "$END" "$RANGE" "$STEP" <<'PY'
import csv, datetime, json, re, sys, urllib.parse, urllib.request

panels_path, cell, prom, start, end, rng, step = sys.argv[1:8]
panels = json.load(open(panels_path))["panels"]


def seconds(d):
    m = re.fullmatch(r"(\d+)(ms|s|m)", d)
    if not m:
        raise SystemExit(f"cannot parse duration {d!r}")
    return int(m.group(1)) * {"ms": 0.001, "s": 1, "m": 60}[m.group(2)]


# Range queries start one full rate window *after* the measured phase opens.
#
# rate(x[15s]) evaluated at T covers [T-15s, T]. Evaluated at the measured phase's start it
# therefore reads samples from before it — and the fixture re-seed resets database rows, not
# the service's Prometheus counters, so those samples are warm-up traffic. The opening points
# of the CSV would describe the warm-up while the file claims to describe the measured phase.
#
# Shifting the first evaluation by one range makes every exported point wholly contained in the
# measured window. It costs the leading points, which were the rate window filling rather than
# anything the service did — the same artifact that made the first value of every committed
# series read 0.0.
#
# Both windows are recorded below: `window` is the cell's measured phase and stays the
# authority for what was measured; `query_window` is what these files actually cover.
query_start = (datetime.datetime.fromisoformat(start.replace("Z", "+00:00"))
               + datetime.timedelta(seconds=seconds(rng)))
query_start_s = query_start.strftime("%Y-%m-%dT%H:%M:%SZ")
if query_start >= datetime.datetime.fromisoformat(end.replace("Z", "+00:00")):
    raise SystemExit(
        f"measured window {start}..{end} is shorter than one rate range ({rng}), so no exported "
        f"point could be free of pre-window samples; lengthen WINDOW or lower export_range")

manifest = []
for p in panels:
    expr = p["expr"].replace("$RANGE", rng)
    if "$" in expr:
        raise SystemExit(f"panel {p['key']}: unsubstituted variable in {expr}")

    # Instant selectors carry no range, so nothing bleeds in and they keep the full window.
    q_start = query_start_s if "[" in p["expr"] else start
    url = prom + "/api/v1/query_range?" + urllib.parse.urlencode(
        {"query": expr, "start": q_start, "end": end, "step": step}
    )
    with urllib.request.urlopen(url, timeout=30) as r:
        body = json.load(r)
    if body.get("status") != "success":
        raise SystemExit(f"panel {p['key']}: {body.get('error')}")

    series = body["data"]["result"]
    out = f"{cell}/panels/{p['key']}.csv"
    with open(out, "w", newline="") as f:
        # LF, not the csv module's default CRLF: these files are committed as evidence, and a
        # CRLF artifact makes git rewrite them on every checkout and shows the whole file as
        # changed in a diff that should show one number moving.
        w = csv.writer(f, lineterminator="\n")
        # Series labels are kept as a column rather than flattened away: `outcomes` returns one
        # series per outcome, and a CSV that dropped the label would silently sum business
        # refusals into failures — the exact conflation measurement-contract §3 forbids.
        w.writerow(["timestamp", "labels", "value"])
        points = 0
        for s in series:
            labels = ",".join(f"{k}={v}" for k, v in sorted(s["metric"].items())
                              if k != "__name__")
            for ts, val in s["values"]:
                w.writerow([ts, labels, val])
                points += 1

    manifest.append({"key": p["key"], "title": p["title"], "unit": p.get("unit", ""),
                     "expr": expr, "file": f"panels/{p['key']}.csv",
                     "queried_from": q_start,
                     "series": len(series), "points": points})
    if points == 0:
        # Some panels are legitimately empty: replay_rate is zero for every workload except
        # the replay control, since the others derive a unique key per logical request. The
        # note is still worth printing — a panel returning nothing looks exactly like a
        # service doing nothing, and which one it is depends on the workload.
        print(f"  note: {p['key']} returned no data in this window")

# The query set that produced these files, resolved. Without it a CSV is a column of numbers
# whose meaning has to be reconstructed from a dashboard that may have moved on.
with open(f"{cell}/panels/index.json", "w") as f:
    json.dump({"window": {"start": start, "end": end},
               "query_window": {"start": query_start_s, "end": end},
               "rate_range": rng, "step": step,
               "_note": ("step is finer than rate_range, so consecutive points share samples "
                         "and the series is smoothed; this is a property of the export, not "
                         "of the measurement"),
               "_query_window_note": (
                   "window is the measured phase and is the authority for what was measured. "
                   "Range queries are evaluated only from window.start + rate_range, because "
                   "rate(x[R]) at time T reads samples from T-R and the fixture re-seed resets "
                   "database rows but not the service's counters — so earlier evaluations would "
                   "mix warm-up traffic into a file that claims to describe the measured phase. "
                   "Instant selectors carry no range and keep the full window; each panel "
                   "records its own queried_from."),
               "panels": manifest}, f, indent=2)
    f.write("\n")

print(f"export: {len(manifest)} panels -> {cell}/panels/")
PY

# TSDB snapshot: a hard-linked copy of the blocks, so it is cheap and complete. It is what
# makes the CSVs checkable after the retention window has dropped the original series.
SNAP=$(curl -sf -XPOST "$PROM/api/v1/admin/tsdb/snapshot" \
  | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['name'])")
docker cp "$CONTAINER:/prometheus/snapshots/$SNAP" "$CELL/tsdb-snapshot" >/dev/null
echo "export: tsdb snapshot $SNAP -> $CELL/tsdb-snapshot"
