#!/usr/bin/env bash
# Captures the web UI for video 5 in $OUT (default: ./build/cap), by driving gorun -serve with Playwright.
#   GORUN_SRC=/path/to/gorun PW=/path/with/playwright-core ./record.sh
# It starts with no Gorunfile, so gorun keeps them in its history database; capture.mjs imports
# record/base.toml and record/Gorunfile (the web fleet on local stand-ins that only sleep) into it.
# Runs under a made-up host and user name, in UTC. Needs Go, unshare, chromium and node with playwright-core.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=/tmp/gorun-demo; mkdir -p "$work"; trap 'pgrep -x gorun | xargs -r kill 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)
port=$((20000 + RANDOM % 20000))
(cd "$work" && unshare --map-root-user --uts sh -c "hostname laptop && exec env HOME=/home/dev USER=dev LOGNAME=dev XDG_CACHE_HOME=/home/dev/.cache GORUN_CACHE_DIR=/home/dev/.cache/gorun GORUN_DB_PATH='$work/h.db' GORUN_LOG_FILE='$work/g.log' GORUN_ADDRESS=127.0.0.1:$port '$work/gorun' -serve" > "$work/srv.log" 2>&1 &)
for _ in $(seq 40); do curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.5; done
cp "$here/capture.mjs" "$PW/capture.mjs"
(cd "$PW" && node capture.mjs "http://127.0.0.1:$port" "$out" "$here/record")
cp "$work/srv.log" "$out/serve-start.txt"
