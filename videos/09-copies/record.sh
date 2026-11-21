#!/usr/bin/env bash
# Captures video 9 in $OUT (default: ./build/cap): copying files with gorun, to three real SSH servers.
# The servers are three of the recipes lab's containers (recipes/docker-compose.yml), started here and
# stopped afterwards. Everything really runs, as a made-up user and host.
#   ./record.sh                       (SIZE_MB=600 sets the release's size)
# Needs Go, Docker, tmux and unshare.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$here/../..
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); web=$work/web; mkdir -p "$web/build" "$work/bin"
lab=$src/recipes/docker-compose.yml
cleanup() { tmux kill-session -t vid9 2>/dev/null || true
  docker compose -f "$lab" stop ssh-server-1 ssh-server-2 ssh-server-3 >/dev/null 2>&1 || true
  docker compose -f "$lab" rm -f ssh-server-1 ssh-server-2 ssh-server-3 >/dev/null 2>&1 || true
  rm -rf "$work"; }
trap cleanup EXIT
(cd "$src" && go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o "$work/bin/gorun" ./cmd)
docker compose -f "$lab" up -d ssh-server-1 ssh-server-2 ssh-server-3 >/dev/null 2>&1
cp -r "$here/demo/Gorunfile" "$here/demo/templates" "$here/demo/site" "$web/"
cp "$src/recipes/ssh-keys/id_ecdsa" "$web/id_ecdsa"; chmod 600 "$web/id_ecdsa"
dd if=/dev/urandom of="$web/build/release.tar.gz" bs=1048576 count="${SIZE_MB:-600}" 2>/dev/null
(cd "$web/build" && sha256sum release.tar.gz > release.sha256)
for _ in $(seq 40); do (echo > /dev/tcp/127.0.0.1/2223) 2>/dev/null && break; sleep 1; done; sleep 6

# the shell script that runs gorun as dev on laptop, in /home/dev/deploys
cat > "$work/as_dev.sh" <<'S'
#!/usr/bin/env bash
unshare --map-root-user --uts --mount bash -c '
  hostname laptop
  printf "dev:x:0:0::/home/dev:/bin/bash\n" > /tmp/passwd.dev && mount --bind /tmp/passwd.dev /etc/passwd
  mount -t tmpfs tmpfs /home && mkdir -p /home/dev/deploys && mount --bind "$WEB" /home/dev/deploys
  cd /home/dev/deploys && export HOME=/home/dev USER=dev LOGNAME=dev GORUN_SKIP_NATS=1 GORUN_DB_PATH=/home/dev/deploys/h.db GORUN_LOG_FILE=/home/dev/deploys/gorun.log
  exec -a gorun "$@"' _ "$@"
S
chmod +x "$work/as_dev.sh"; export WEB=$web
G=$work/bin/gorun
# cap <name> <args...>: the output goes to <name>.txt
cap() { local n=$1; shift; set +e; "$work/as_dev.sh" "$G" "$@" > "$out/$n.txt" 2>&1 < /dev/null; set -e; }

cap cleanup Gorunfile -r --plain --task cleanup
cap config Gorunfile -r --plain -v --task config
cap site Gorunfile -r --plain -v --task site
cap release Gorunfile -r --plain --task release
cap rerun Gorunfile -r --plain
echo '/* a new rule */ footer { color: #888; }' >> "$web/site/css/app.css"
cap check-after-edit Gorunfile -r --plain -v --check
cap run-after-edit Gorunfile -r --plain

# the live view, while the release uploads
cap cleanup2 Gorunfile -r --plain --task cleanup
tmux kill-session -t vid9 2>/dev/null || true
tmux new-session -d -s vid9 -x 124 -y 20 "WEB=$web $work/as_dev.sh $G Gorunfile -r --task release; sleep 60"
for i in $(seq 1 14); do sleep 0.7; tmux capture-pane -e -p -t vid9 > "$out/tui-$i.txt"; done
