# PostgreSQL-side sampling, 2026-08-04

The service exposes its own pool state but nothing about the PostgreSQL process
(`../README.md` §6). These samples are the first look inside the database, taken while the
`../plateau-repeat/` cells and a throwaway diagnostic pass were running.

**They are diagnostic evidence, not a measured deliverable.** Nothing in the report's tables
is derived from them; they are cited only where the report names *what* PostgreSQL was doing.
Read the caveats before quoting anything here.

## Method

`probe.sh` and `probe2.sh` poll from a short-lived `psql` session every 2 seconds:

| file | query | what it answers |
|---|---|---|
| `waits.csv` | `pg_stat_activity` grouped by `wait_event_type`/`wait_event`, `state='active'` | what backends are blocked on |
| `states.csv` | `pg_stat_activity` grouped by `state` | active vs idle-in-transaction vs idle |
| `vac.csv` | `pg_stat_user_tables` | dead tuples, autovacuum counts and last-run times |
| `ckpt2.csv` | `pg_stat_bgwriter` | checkpoints, timed vs requested, buffers written |
| `rtt.csv` | wall time of `docker exec … SELECT 1` | coarse host responsiveness |

Timestamps are UTC `HH:MM:SS`, comparable to the `window` in each cell's
`panels/index.json`.

## Four caveats, all of which matter

1. **Sampling, not accounting.** A 2-second poll of `pg_stat_activity` gives the *proportion
   of samples* in which a backend was in a state. It is not time-weighted, and a wait shorter
   than the interval can be missed entirely. Percentages here are "share of observed active
   backends", never "share of time".
2. **The probe perturbs what it measures.** Each sample spawns a `docker exec` and opens a
   connection. Small, but not zero, and it runs during measured windows.
3. **`rtt.csv` does not traverse the connection path the service uses.** `docker exec` runs
   `psql` *inside* the container over a unix socket, so it measures container-exec spawn plus
   an in-container query — not the host's port-forward to `localhost:15432`. It is a coarse
   host-responsiveness signal and nothing more. It was originally added to test a
   connection-path hypothesis and does not test it.
4. **`state='active'` excludes `idle in transaction`.** `waits.csv` therefore describes only
   backends currently executing; `states.csv` is what shows the rest.

## What they established

**At the plateau, the wait is `LWLock:WALWrite`.** In both clean cells it is the largest single
category — 41.0% at `c=128/pool=40` (3883.7 req/s) and 36.8% at `c=64/pool=80` (4113.3 req/s),
with `LWLock:BufferContent` second at 22.5% and 32.7%. That is a database serialising on its
write-ahead log, and it is the evidence behind the report's §5 bottleneck conclusion.

**The §4 anomaly is not autovacuum and not a checkpoint.** Both were the leading suspects and
both are ruled out by these files:

- autovacuum ran *inside* a clean 3883.2 req/s window (`vac.csv`, `last_autovacuum` 11:04:35–36
  against window 11:04:18–11:04:49), and the collapsed window that followed had its autovacuum
  finish *before* the window opened;
- the one checkpoint captured mid-sweep (`ckpt2.csv`, 10:40:03–10:40:08, requested) wrote 154
  buffers and coincided with the *fastest* cell of that pass.

**Collapsed windows look host-starved, not database-blocked.** They are dominated by backends
in `RUNNING` — 77.1% and 59.9% of active samples, against 18.0% and 12.9% in the clean cells —
meaning backends were executing rather than waiting on any lock, while producing a third of the
throughput. The service's own CPU halves at the same time, and so does the generator's. Nothing
here identifies the cause; it narrows it to something that slows every process on the host at
once, which is why §4 still does not name one.
