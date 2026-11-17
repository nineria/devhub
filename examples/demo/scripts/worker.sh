#!/bin/sh
# A pretend job worker. One job fails a little after the start, so the page shows an error count and a red row.
echo "$(date '+%Y-%m-%d %H:%M:%S') INFO  worker started, 4 queues"
i=0
while true; do
  i=$((i + 1))
  ts=$(date '+%Y-%m-%d %H:%M:%S')
  if [ "$i" -eq 14 ]; then
    echo "$ts ERROR job $((4818 + i)) failed: upstream timeout after 30s (will retry)"
  elif [ $((i % 17)) -eq 9 ]; then
    echo "$ts WARN  queue emails is 3x slower than usual"
  else
    echo "$ts INFO  job $((4818 + i)) done in $((i * 13 % 900 + 50))ms"
  fi
  sleep 3
done
