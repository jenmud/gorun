#!/usr/bin/env bash
# Captures the output for video 6 in $OUT (default: ./build/cap). Every program in demo/ is really run
# against the gorun beside it, under a made-up host and user name. The two example programs from
# ../../examples are run too: rollout in tmux, Service Deck with Playwright.
#   PW=/path/with/playwright-core ./record.sh
# Needs Go, tmux, unshare, chromium and node with playwright-core.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$here/../..
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); trap 'tmux kill-session -t vid6 2>/dev/null || true; pgrep -x servicedeck | xargs -r kill 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$here/demo" && for p in hello events pick hist encode; do go build -o "$work/$p" "./$p"; done)
(cd "$src/examples/rollout" && go build -o "$work/rollout" .)
(cd "$src/examples/servicedeck" && go build -o "$work/servicedeck" .)

as_dev() { unshare --map-root-user --uts sh -c 'hostname laptop && exec env HOME=/home/dev USER=dev LOGNAME=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH="$0" GORUN_LOG_FILE="$1" "${@:2}"' "$work/h.db" "$work/g.log" "$@"; }

for p in hello events pick; do (cd "$here/demo" && as_dev "$work/$p") > "$out/$p.txt" 2>&1 || true; done
(cd "$here/demo" && as_dev "$work/hist") >/dev/null 2>&1 || true  # one earlier run, for the list
(cd "$here/demo" && as_dev "$work/hist") > "$out/hist.txt" 2>&1 || true
(cd "$here/demo" && as_dev "$work/encode") | sed -n '/web-3/,$p' > "$out/encode.txt"
(cd "$here/demo" && as_dev "$work/encode") | head -12 > "$out/encode-head.txt"
(cd "$here/demo" && as_dev go version) > "$out/gover.txt" 2>&1 || true

# rollout, in a real terminal
rm -rf /tmp/gorun-roll; mkdir -p /tmp/gorun-roll; cp "$src/examples/rollout/Gorunfile" /tmp/gorun-roll/
tmux kill-session -t vid6 2>/dev/null || true
tmux new-session -d -s vid6 -x 100 -y 24 "cd /tmp/gorun-roll && HOME=$work GORUN_LOG_FILE=$work/roll.log $work/rollout; sleep 60"
sleep 2.6
for _ in 1 2 3 4 5; do tmux capture-pane -e -p -t vid6 > "$out/rollout-mid.txt"
  [[ $(grep -c rollout "$out/rollout-mid.txt") == 1 ]] && break; sleep 0.3; done
sleep 6; tmux capture-pane -e -p -t vid6 > "$out/rollout-done.txt"
rm -rf /tmp/gorun-roll

# Service Deck, in a browser
port=8097
(cd "$src/examples/servicedeck" && "$work/servicedeck" -addr 127.0.0.1:$port > "$work/deck.log" 2>&1 &)
for _ in $(seq 30); do curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.5; done
cp "$here/capture.mjs" "$PW/capture6.mjs"
(cd "$PW" && node capture6.mjs "http://127.0.0.1:$port" "$out")
