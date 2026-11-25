#!/usr/bin/env bash
# Captures the assistant for video 10 in $OUT (default: ./build/cap), by driving gorun -serve with Playwright.
#   GORUN_SRC=/path/to/gorun PW=/path/with/playwright-core ./record.sh
# The fleet is record/Gorunfile: the web fleet on local stand-ins that only sleep. Runs under a made-up
# host and user name, in UTC. The built-in model answers for real; it comes from gorun's cache folder
# (GORUN_CACHE_DIR, default ~/.cache/gorun), downloaded there if it isn't yet.
# Needs Go, unshare, chromium, node with playwright-core and, for speed, a GPU.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
cache=${GORUN_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/gorun}
work=/tmp/gorun-demo; mkdir -p "$work"; trap 'pgrep -x gorun | xargs -r kill 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)
cp "$here/record/Gorunfile" "$work/"
port=$((20000 + RANDOM % 20000))
(cd "$work" && unshare --map-root-user --uts sh -c "hostname laptop && exec env HOME=/home/dev USER=dev LOGNAME=dev GORUN_CACHE_DIR='$cache' GORUN_DB_PATH='$work/h.db' GORUN_LOG_FILE='$work/g.log' GORUN_ADDRESS=127.0.0.1:$port '$work/gorun' -serve Gorunfile" > "$work/srv.log" 2>&1 &)
for _ in $(seq 40); do curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.5; done
cp "$here/capture.mjs" "$PW/capture-assistant.mjs"
(cd "$PW" && node capture-assistant.mjs "http://127.0.0.1:$port" "$out")
cp "$work/srv.log" "$out/serve-start.txt"
