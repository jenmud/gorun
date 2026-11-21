#!/usr/bin/env bash
# Captures the gorun screens used in the video as colour text frames in $OUT (default: ./build/cap).
# GORUN_SRC is the gorun source tree to build (default: this repo); use the latest release.
# The picker comes from the real demo Gorunfile; the runs use record/Gorunfile, which has the same
# tasks and checks, but only sleeps and touches marker files, on local "servers". Needs tmux and Go.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); trap 'tmux kill-session -t vid 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)
touch "$work/h.db"
env="GORUN_SKIP_NATS=1 GORUN_DB_PATH=$work/h.db GORUN_LOG_FILE=$work/g.log"
start() { local dir=$1; shift; tmux kill-session -t vid 2>/dev/null || true
  tmux new-session -d -s vid -x 104 -y 28 "cd '$dir' && env $env $work/gorun Gorunfile $*; sleep 300"; sleep 1.5; }
snap() { tmux capture-pane -e -p -t vid > "$out/$1.txt"; }
markers=/tmp/gorun-video; find "$markers" -mindepth 1 -delete 2>/dev/null || true

# a dry run on fresh servers: every task would run
start "$here/record" -r --check; sleep 3; snap check-fresh

# the picker, then a real run
start "$here/demo"; snap picker
tmux send-keys -t vid M-s; sleep .6; snap picker-all
start "$here/record"; tmux send-keys -t vid M-a; sleep .4
for i in 2 4 7 10 14; do sleep $((i == 2 ? 2 : i == 4 ? 2 : i == 7 ? 3 : i == 10 ? 3 : 4)); snap live-$i; done
sleep 4; snap live-done

# the same run again: every check passes, nothing runs
start "$here/record" -r; sleep 3; snap rerun-done
# and a dry run again: nothing would run
start "$here/record" -r --check; sleep 3; snap check-done
