#!/bin/sh
# A pretend API: logs a request every so often, with the odd slow query.
i=0
while true; do
  i=$((i + 1))
  ts=$(date '+%Y-%m-%d %H:%M:%S')
  case $((i % 23)) in
    9) echo "$ts WARN  slow query on users table (412ms)" ;;
    17) echo "$ts INFO  POST /api/login 200 $((i % 30 + 20))ms" ;;
    *) echo "$ts INFO  GET /api/users 200 $((i * 7 % 40 + 8))ms" ;;
  esac
  sleep 2.5
done
