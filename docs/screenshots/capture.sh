#!/usr/bin/env bash
# Regenerates the README screenshots in docs/. Runs gorun in tmux, captures the screen
# with colours, renders it to HTML (ansi2html.py) and screenshots that with headless
# Chromium. Needs tmux, chromium, ImageMagick (magick), python3 and Go.
#
#   docs/screenshots/capture.sh
#
# The picker shots use example/Gorunfile (for its real addresses); the run and history
# shots use the demo Gorunfile in this directory, which only echoes and sleeps locally.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
docs="$repo/docs"
work=$(mktemp -d)
trap 'tmux kill-session -t gorun-shots 2>/dev/null || true; rm -rf "$work"' EXIT

go build -o "$work/gorun" "$repo/cmd"

# a fresh history and log for the shots; gorun only uses GORUN_DB_PATH if it exists
touch "$work/history.db"
env=(GORUN_SKIP_NATS=1 GORUN_LOG_FILE="$work/gorun.log" GORUN_DB_PATH="$work/history.db")

# gorun records the machine's hostname in run IDs and the history; run it under a
# made-up one (in an unprivileged user namespace) so the shots don't show yours
gorun="unshare --map-root-user --uts sh -c 'hostname deploy-box && exec \"\$0\" \"\$@\"' $work/gorun"

# start <cols> <rows> <dir> <args...>: run gorun in a fresh tmux session
start() {
	local cols=$1 rows=$2 dir=$3
	shift 3
	tmux kill-session -t gorun-shots 2>/dev/null || true
	tmux new-session -d -s gorun-shots -x "$cols" -y "$rows" \
		"cd '$dir' && env ${env[*]} $gorun $* ; sleep 600"
	sleep 1.5
}

# keys <key>...: send keys one at a time, with a pause so none get merged
keys() {
	for k in "$@"; do
		tmux send-keys -t gorun-shots "$k"
		sleep 0.15
	done
	sleep 0.5
}

# down <n>: press Down n times
down() {
	for _ in $(seq "$1"); do tmux send-keys -t gorun-shots Down; sleep 0.05; done
	sleep 0.5
}

# shot <name>: capture the screen to docs/<name>.png
shot() {
	local name=$1
	tmux capture-pane -e -p -t gorun-shots >"$work/$name.txt"
	# drop trailing blank lines, so the image ends with the content
	sed -i -e :a -e '/^\(\x1b\[[0-9;]*m\)*[[:space:]]*$/{$d;N;ba' -e '}' "$work/$name.txt"
	python3 "$here/ansi2html.py" "$work/$name.txt" "$work/$name.html"
	chromium --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
		--window-size=1400,2400 --screenshot="$work/$name-raw.png" "file://$work/$name.html" 2>/dev/null
	magick "$work/$name-raw.png" -trim +repage -bordercolor '#0b0b0d' -border 48 "$docs/$name.png"
	echo "docs/$name.png"
}

# the picker: groups and tags selected, one server expanded with a task left out
start 112 58 "$repo/example" Gorunfile
keys Down Space         # the eu group
down 13; keys Space     # the web tag
down 9; keys Right      # expand worker-eu-a
down 5; keys Space      # leave out neovim-install (and the tasks that need it)
shot screenshot-picker

# the picker's fuzzy filter
start 100 26 "$repo/example" Gorunfile
keys w p
shot screenshot-picker-filter

# a run: compact view part way, then the finished run with its failure
start 110 44 "$here" Gorunfile -r
sleep 3
shot screenshot
sleep 12
shot screenshot-failure

# verbose: part way, then finished with the failure open, then a task's output
start 110 52 "$here" Gorunfile -r -v
sleep 3
shot screenshot-verbose
sleep 12
shot screenshot-failure-verbose
down 29; keys Enter     # server1 › jj-config
shot screenshot-output

# a couple of smaller runs, so the history has some variety
(cd "$here" && env "${env[@]}" sh -c "$gorun Gorunfile -r --tag db" </dev/null >/dev/null)
(cd "$here" && env "${env[@]}" sh -c "$gorun Gorunfile -r --group eu" </dev/null >/dev/null)

# the History tab, then a run's details with a multi-line command
start 110 40 "$here" Gorunfile
keys BTab
shot screenshot-history
keys Up Up Up           # the full run with the failure
keys Enter
down 3; keys Enter      # expand server1
down 8; keys Enter      # its jj-config task (tasks are listed alphabetically), a multi-line script
shot screenshot-history-run
