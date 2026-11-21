#!/usr/bin/env bash
# Captures the gorun screens for video 2 as colour text frames in $OUT (default: ./build/cap).
#   GORUN_SRC=/path/to/gorun ./record.sh      # build and capture with that release
# The picker frames come from the real demo Gorunfile (SSH addresses; nothing is contacted). The
# runs use record/Gorunfile: the same tasks, local, only sleeping; web-3's smoke test fails on
# purpose. Steps 1 and 2 are really run. Everything runs under a made-up host and user name.
# Needs tmux, Go and unshare.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); trap 'tmux kill-session -t vid2 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)

# g <db> <args...> is gorun with its own history database, as dev@laptop
g() { local db=$1; shift
  unshare --map-root-user --uts --mount sh -c 'hostname laptop && printf "dev:x:0:0::/home/dev:/bin/bash\n" > /tmp/passwd.dev && mount --bind /tmp/passwd.dev /etc/passwd && exec env HOME=/home/dev USER=dev LOGNAME=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH="$0" GORUN_LOG_FILE="$1" "$2" "${@:3}"' \
    "$db" "$work/g.log" "$work/gorun" "$@"; }
tmuxrun() { local dir=$1 db=$2 cols=$3 rows=$4; shift 4; tmux kill-session -t vid2 2>/dev/null || true
  tmux new-session -d -s vid2 -x "$cols" -y "$rows" -e "GDB=$db" "cd '$dir' && $(declare -f g); work='$work'; g '$db' $*; sleep 300"; sleep 1.5; }
snap() { tmux capture-pane -e -p -t vid2 > "$out/$1.txt"; }
keys() { for k in "$@"; do tmux send-keys -t vid2 "$k"; sleep 0.25; done; sleep 0.5; }
text() { local t=$1; for ((i = 0; i < ${#t}; i++)); do tmux send-keys -t vid2 -l "${t:$i:1}"; sleep 0.15; done; sleep 0.4; }
# goto <text>: move down until the cursor row (the one marked ▶) contains <text>
goto() { for _ in $(seq 14); do
    [[ $(tmux capture-pane -p -t vid2 | grep '▶') == *"$1"* ]] && return 0; keys Down; done; return 1; }

steps=$work/steps.db; hist=$work/hist.db; touch "$steps" "$hist"
rm -rf /tmp/gorun-demo

# steps 1 and 2, really run
tmuxrun "$here/steps/step1" "$steps" 100 12 Gorunfile; snap s1-picker
keys Space; snap s1-picked
keys Enter; sleep 1.5; snap s1-done
tmuxrun "$here/steps/step2" "$steps" 100 18 Gorunfile; keys Space Enter; sleep 2; snap s2-done
(cd "$here/steps/step1" && g "$steps" Gorunfile --run --plain -v) > "$out/s1-plain.txt" 2>&1 || true
(cd "$here/steps/step2" && g "$steps" Gorunfile --list-tasks --plain) > "$out/s2-tasks.txt" 2>&1 || true
(cd "$here/steps/step2" && g "$steps" Gorunfile --list-servers --plain) > "$out/s2-servers.txt" 2>&1 || true
(cd "$here/demo" && g "$steps" Gorunfile --list-servers --plain) > "$out/list-servers.txt" 2>&1 || true
(cd "$here/demo" && g "$steps" Gorunfile --list-bundles --plain) > "$out/list-bundles.txt" 2>&1 || true

# the picker, on the demo fleet
d=$here/demo
tmuxrun "$d" "$steps" 104 30 Gorunfile; snap picker
keys Up Up Right; snap picker-groups
keys Down Down Down Down Down Space; snap picker-groups-web
tmuxrun "$d" "$steps" 104 30 Gorunfile
keys /; text web; snap picker-filter
keys Enter M-s Down Down; snap picker-selected
keys Tab Down Space; snap picker-tasks
keys Tab Right; snap picker-details
keys Left 2; snap tab-tasks
keys 3 Space; snap tab-bundles
keys 4; snap tab-groups

# a run, watched live: compact, a server's tasks, the verbose view and a task's details
r=$here/record
tmuxrun "$r" "$hist" 104 34 Gorunfile; keys M-a
sleep 3; snap live-compact
keys Down l; snap live-expanded
keys v; snap live-verbose
keys g g Down Enter; snap live-output
sleep 25; snap live-verbose-failed
# the same run, compact, left alone to the end
tmuxrun "$r" "$hist" 104 34 Gorunfile; keys M-a; sleep 27; snap live-failed
# two more runs for the history
for t in "--tag staging" "--tag db"; do (cd "$r" && g "$hist" Gorunfile -r --plain $t) >/dev/null 2>&1 </dev/null || true; done

# history: the list, a run, its failed task
tmuxrun "$r" "$hist" 104 30 Gorunfile; keys 5; snap history
keys Up Up; keys Enter; snap history-run
goto web-3; keys Enter; snap history-server
goto smoke; keys Enter; snap history-task
