#!/usr/bin/env bash
# Captures the gorun output for video 4 in $OUT (default: ./build/cap).
#   GORUN_SRC=/path/to/gorun ./record.sh      # build and capture with that release
# Everything really runs: the split Gorunfile is listed (nothing contacts its servers), the shared
# library is a git repository made on this machine and included with a file:// URL (yours would be
# https or ssh), and the Schedules page is the real web UI. A made-up host and user name are used.
# Needs Go, git, unshare and chromium.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d)
trap 'pgrep -x gorun | xargs -r kill 2>/dev/null || true; rm -rf "$work" /tmp/gorun-v4; rm -f "$here"/steps/gitinc/Gorunfile.lock' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)
export GORUN_CACHE_DIR="$work/cache"
rm -rf /tmp/gorun-v4; mkdir -p /tmp/gorun-v4

g() { unshare --map-root-user --uts sh -c 'hostname laptop && exec env HOME=/home/dev USER=dev LOGNAME=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH="$0" GORUN_LOG_FILE="$1" "$2" "${@:3}"' \
    "$work/h.db" "$work/g.log" "$work/gorun" "$@"; }
plain() { local dir=$1 name=$2; shift 2; (cd "$dir" && g "$@" --plain) > "$out/$name.txt" 2>&1 || true; }
s=$here/steps

# splitting
plain "$s/split" split-servers Gorunfile --list-servers
plain "$s/dup" dup-error Gorunfile -r
tree=$(cd "$s/split" && find . -type f | sort | sed 's|^\./||'); printf '%s\n' "$tree" > "$out/split-tree.txt"

# the shared library, as a git repository
lib=$work/lib; mkdir -p "$lib"; cp -r "$s/library/web" "$lib/"
G() { git -C "$lib" -c user.name=dev -c user.email=dev@example.com "$@"; }
git -C "$lib" init -q -b main; G add -A; G commit -qm "base library"; G tag v1.0.0
git clone -q --bare "$lib" /tmp/gorun-v4/library.git
cp -r "$s/library" "$out/library"; (cd "$s/library" && find . -type f | sort | sed 's|^\./||') > "$out/lib-tree.txt"

plain "$s/gitinc" git-run1 Gorunfile -r
grep -v '^#' "$s/gitinc/Gorunfile.lock" | sed '/^$/d' > "$out/git-lock.txt"
# the library moves on: a new task joins the bundle
cat >> "$lib/web/base.toml" <<'T'

[tasks.disk]
description = "Show free space on the root filesystem"
cmd = "df -h /"
local = true
T
sed -i 's/tasks = \["motd", "uptime"\]/tasks = ["motd", "uptime", "disk"]/' "$lib/web/base.toml"
G commit -qam "add disk"; G push -q /tmp/gorun-v4/library.git main
plain "$s/gitinc" git-list-before Gorunfile --list-bundles
plain "$s/gitinc" git-update Gorunfile --update-includes
plain "$s/gitinc" git-list-after Gorunfile --list-bundles
plain "$s/gitinc" git-run2 Gorunfile -r
# a library that tries to add a server
G checkout -q -b sneaky; printf '[servers.hosts.evil]\nhostname = "10.9.9.9"\n' > "$lib/web/evil.toml"; G add -A; G commit -qm "sneaky"; G push -q /tmp/gorun-v4/library.git sneaky
plain "$s/gitbad" git-bad Gorunfile -r
rm -f "$s/gitinc/Gorunfile.lock" "$s/gitbad/Gorunfile.lock"

# schedules
plain "$s/schedbad" sched-bad Gorunfile -r
port=$((20000 + RANDOM % 20000)); cp -r "$s/schedules" "$work/sched"
(cd "$work/sched" && unshare --map-root-user --uts sh -c "hostname laptop && exec env HOME=/home/dev USER=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH='$work/s.db' GORUN_LOG_FILE='$work/s.log' GORUN_ADDRESS=127.0.0.1:$port '$work/gorun' -serve Gorunfile" > "$work/srv.log" 2>&1 &)
for _ in $(seq 40); do curl -fsS "http://127.0.0.1:$port/schedules" >/dev/null 2>&1 && break; sleep 0.5; done
TZ=UTC chromium --headless --no-sandbox --hide-scrollbars --window-size=1500,430 --virtual-time-budget=4000 --screenshot="$out/web-schedules.png" "http://127.0.0.1:$port/schedules" >/dev/null 2>&1
