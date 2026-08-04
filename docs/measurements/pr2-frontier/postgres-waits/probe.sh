#!/bin/bash
# Short-lived connection per sample: no long transaction, so nothing here holds back vacuum.
SP="$(dirname "$0")"
echo "ts,wait_event_type,wait_event,n" > "$SP/waits.csv"
echo "ts,ckpt_req,ckpt_timed,buffers_ckpt,bgwriter_clean" > "$SP/ckpt2.csv"
while true; do
  now=$(date -u +%H:%M:%S)
  docker exec alloca-pg psql -U alloca -d alloca -tAF, -c \
    "SELECT coalesce(wait_event_type,'RUNNING'), coalesce(wait_event,'-'), count(*)
     FROM pg_stat_activity WHERE datname='alloca' AND state='active' AND pid<>pg_backend_pid()
     GROUP BY 1,2 ORDER BY 3 DESC;" 2>/dev/null | sed "s/^/$now,/" >> "$SP/waits.csv"
  docker exec alloca-pg psql -U alloca -d alloca -tAF, -c \
    "SELECT checkpoints_req,checkpoints_timed,buffers_checkpoint,buffers_clean FROM pg_stat_bgwriter;" \
    2>/dev/null | sed "s/^/$now,/" >> "$SP/ckpt2.csv"
  sleep 2
done
