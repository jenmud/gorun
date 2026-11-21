#!/usr/bin/env bash
# Captures video 8 in $OUT (default: ./build/cap): gorun in scripts and CI. Everything really runs, as a
# made-up user and host, on the stand-in fleet in demo/Gorunfile (local servers that only sleep). The CI
# run sets the variables GitHub Actions sets, so the history shows it as a CI run; nothing runs on GitHub.
#   PW=/path/with/playwright-core ./record.sh
# Needs Go, unshare, chromium and node with playwright-core.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$here/../..
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); web=$work/web; mkdir -p "$web" "$work/bin"
trap 'pkill -x gorun 2>/dev/null || true; rm -rf "$work"' EXIT
(cd "$src" && go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o "$work/bin/gorun" ./cmd)
cp "$here/demo/Gorunfile" "$web/"

# as_dev <program> <args>: as dev on laptop, in /home/dev/deploys, and the process is called gorun
as_dev() {
  WEB=$web unshare --map-root-user --uts --mount bash -c '
    hostname laptop
    printf "dev:x:0:0::/home/dev:/bin/bash\n" > /tmp/passwd.dev && mount --bind /tmp/passwd.dev /etc/passwd
    mount -t tmpfs tmpfs /home && mkdir -p /home/dev/deploys && mount --bind "$WEB" /home/dev/deploys
    cd /home/dev/deploys && export HOME=/home/dev USER=dev LOGNAME=dev GORUN_DB_PATH=/home/dev/deploys/h.db GORUN_LOG_FILE=/home/dev/deploys/gorun.log
    [ -n "$KEEP_NATS" ] || export GORUN_SKIP_NATS=1
    exec -a gorun "$@"' _ "$@"
}
G=$work/bin/gorun
# cap <name> <args...>: the output goes to <name>.txt and the exit code to <name>.rc
cap() { local n=$1; shift; set +e; "$@" > "$out/$n.txt" 2>&1 < /dev/null; echo $? > "$out/$n.rc"; set -e; }
export DEPLOY_TOKEN=s3cret-token-123

cap nopicker as_dev "$G" Gorunfile
cap check-before as_dev "$G" Gorunfile -r --plain --check
cap run-ok as_dev "$G" Gorunfile -r --plain
cap check-after as_dev "$G" Gorunfile -r --plain --check
rm -f "$web/migrated"
SMOKE_FAIL=1 cap run-fail as_dev "$G" Gorunfile -r --plain
cap list-prod as_dev "$G" Gorunfile --list-servers --tag prod --plain
rm -f "$web/migrated"
cap run-staging as_dev "$G" Gorunfile -r --plain --tag staging
grep -c "$DEPLOY_TOKEN" "$web/gorun.log" > "$out/token-in-log.txt" || true
rm -f "$web/migrated"
# the same run, as a CI job would see it (the variables GitHub Actions sets)
GITHUB_ACTIONS=true GITHUB_ACTOR=octocat GITHUB_SERVER_URL=https://github.com GITHUB_REPOSITORY=acme/deploys GITHUB_RUN_ID=8421 \
  GORUN_LABELS="ticket=CHG-42,team=ops" cap run-ci as_dev "$G" Gorunfile -r --plain --tag prod

# the history, in the web UI
port=$((20000 + RANDOM % 20000))
(KEEP_NATS=1 GORUN_ADDRESS=127.0.0.1:$port as_dev "$G" -serve Gorunfile > "$work/serve.log" 2>&1 &)
for _ in $(seq 60); do curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.5; done
cp "$here/ciweb.mjs" "$PW/ciweb.mjs"; (cd "$PW" && node ciweb.mjs "http://127.0.0.1:$port" "$out")
