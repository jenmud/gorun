#!/usr/bin/env bash
# Captures the gorun output for video 3 in $OUT (default: ./build/cap).
#   GORUN_SRC=/path/to/gorun ./record.sh      # build and capture with that release
# steps/ are small Gorunfiles that really run on this machine (checks, a failing verify, a secret
# from .env, templates), under a made-up host and user name. The fleet screens come from
# record/Gorunfile: the demo's tasks and checks on local "servers" that only sleep and touch marker
# files, so the dry run, the first run, the rerun and a change can be recorded without real hosts.
# Needs tmux, Go and unshare.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=${GORUN_SRC:-$here/../..}
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); trap 'tmux kill-session -t vid3 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -o "$work/gorun" ./cmd)

# g <args...>: gorun as dev@laptop, with its own history database
g() { unshare --map-root-user --uts sh -c 'hostname laptop && exec env HOME=/home/dev USER=dev LOGNAME=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH="$0" GORUN_LOG_FILE="$1" "$2" "${@:3}"' \
    "$work/h.db" "$work/g.log" "$work/gorun" "$@"; }
plain() { local dir=$1 name=$2; shift 2; (cd "$dir" && g "$@" --plain) > "$out/$name.txt" 2>&1 || true; }

marker=/tmp/gorun-demo; rm -rf "$marker"
# checks: first run, rerun, dry run, forced run
s=$here/steps/checks
plain "$s" c-run1 Gorunfile -r; plain "$s" c-run2 Gorunfile -r; plain "$s" c-dry Gorunfile -r --check; plain "$s" c-force Gorunfile -r --force
# a verify that fails
rm -rf "$marker"; plain "$here/steps/verify" v-fail Gorunfile -r
# a secret, from .env
mkdir -p "$work/sec"; cp "$here/steps/secrets/Gorunfile" "$work/sec/"; printf 'REGISTRY_TOKEN=ghp_demo_not_a_real_token\n' > "$work/sec/.env"
cp "$work/sec/.env" "$out/secrets-env.txt"
plain "$work/sec" secrets-run Gorunfile -r -v
grep -q ghp_demo "$out/secrets-run.txt" && { echo "the secret leaked into the output" >&2; exit 1; }
# templates
rm -rf "$marker"; plain "$here/steps/templates" templates-run Gorunfile -r -v
rm -rf "$marker"

# the fleet, on the stand-in
tmuxrun() { local dir=$1 file=$2; shift 2; tmux kill-session -t vid3 2>/dev/null || true
  tmux new-session -d -s vid3 -x 104 -y 34 -e "X=1" "cd '$dir' && $(declare -f g); work='$work'; g $file $*; sleep 300"; sleep 1.2; }
snap() { tmux capture-pane -e -p -t vid3 > "$out/$1.txt"; }
r=$here/record; rm -rf /tmp/gorun-video
sed 's/site-v1/site-v2/g' "$r/Gorunfile" > "$r/.v2.Gorunfile"; trap 'rm -f "$r/.v2.Gorunfile"; tmux kill-session -t vid3 2>/dev/null || true; rm -rf "$work"' EXIT
tmuxrun "$r" Gorunfile -r --check; sleep 2; snap f-check-fresh
tmuxrun "$r" Gorunfile -r; sleep 3; snap f-run-mid; sleep 12; snap f-run-done
tmuxrun "$r" Gorunfile -r -v; sleep 1.5; snap f-rerun-mid; sleep 6; snap f-rerun-done
tmuxrun "$r" .v2.Gorunfile -r -v; sleep 1.5; snap f-change-mid; sleep 8; snap f-change-done
tmuxrun "$r" .v2.Gorunfile -r --check; sleep 2; snap f-check-done
