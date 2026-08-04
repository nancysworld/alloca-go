#!/bin/bash
SP="$(dirname "$0")"
echo "ts,relname,n_live,n_dead,autovac_count,last_autovac,autoanalyze_count" > "$SP/vac.csv"
echo "ts,state,n" > "$SP/states.csv"
echo "ts,rtt_ms" > "$SP/rtt.csv"
while true; do
  now=$(date -u +%H:%M:%S)
  docker exec alloca-pg psql -U alloca -d alloca -tAF, -c \
    "SELECT relname,n_live_tup,n_dead_tup,autovacuum_count,coalesce(to_char(last_autovacuum,'HH24:MI:SS'),'-'),autoanalyze_count
     FROM pg_stat_user_tables ORDER BY relname;" 2>/dev/null | sed "s/^/$now,/" >> "$SP/vac.csv"
  docker exec alloca-pg psql -U alloca -d alloca -tAF, -c \
    "SELECT coalesce(state,'null'),count(*) FROM pg_stat_activity WHERE datname='alloca' GROUP BY 1;" \
    2>/dev/null | sed "s/^/$now,/" >> "$SP/states.csv"
  s=$(date +%s%N)
  docker exec alloca-pg psql -U alloca -d alloca -tAc "SELECT 1;" >/dev/null 2>&1
  echo "$now,$(( ($(date +%s%N)-s)/1000000 ))" >> "$SP/rtt.csv"
  sleep 2
done
