#!/bin/sh
# A pretend frontend dev server.
echo "  VITE v6.0.1  ready in 312 ms"
echo "  Local:   http://localhost:5173/"
i=0
while true; do
  i=$((i + 1))
  echo "$(date '+%H:%M:%S') [vite] hmr update /src/components/Header.tsx"
  sleep 6
done
