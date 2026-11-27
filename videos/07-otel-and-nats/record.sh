#!/usr/bin/env bash
# Captures video 7 in $OUT (default: ./build/cap): Grafana's traces, metrics and logs for runs of the
# instrumented gorun, and how gorun uses NATS, embedded and external. Everything really runs, as a
# made-up user and host, on the stand-in fleet in record/Gorunfile (local servers that only sleep).
# Starts Docker containers on localhost (grafana/otel-lgtm and nats) and removes them afterwards.
#   PW=/path/with/playwright-core ./record.sh
# Needs Go, Docker, unshare, node with playwright-core and chromium; the instrumented build is made
# with `make build-otel` (or set GORUN_OTEL_BIN).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
src=$here/../..
out=${OUT:-$here/build/cap}; mkdir -p "$out"
work=$(mktemp -d); web=$work/web; mkdir -p "$web" "$work/bin"
cleanup() { pkill -x gorun-otel 2>/dev/null || true; pkill -x gorun 2>/dev/null || true; pkill -x natssub 2>/dev/null || true
  docker rm -f gorun-lgtm gorun-nats >/dev/null 2>&1 || true; rm -rf "$work"; }
trap cleanup EXIT
otel_bin=${GORUN_OTEL_BIN:-$src/builds/gorun-otel-CTI}
[[ -x $otel_bin ]] || (cd "$src" && make build-otel)
cp "$otel_bin" "$work/bin/gorun-otel"
(cd "$src" && go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o "$work/bin/gorun" ./cmd)
(cd "$here/natssub" && go build -o "$work/bin/natssub" .)
cp "$here/record/Gorunfile" "$web/"

# as_dev <program> <args>: as dev on laptop, in /home/dev/web, and the process is called gorun
as_dev() {
  WEB=$web unshare --map-root-user --uts --mount bash -c '
    hostname laptop
    printf "dev:x:0:0::/home/dev:/bin/bash\n" > /tmp/passwd.dev && mount --bind /tmp/passwd.dev /etc/passwd
    mount -t tmpfs tmpfs /home && mkdir -p /home/dev/web && mount --bind "$WEB" /home/dev/web
    cd /home/dev/web && export HOME=/home/dev USER=dev LOGNAME=dev GORUN_DB_PATH=/home/dev/web/h.db GORUN_LOG_FILE=/home/dev/web/gorun.log
    exec -a gorun "$@"' _ "$@"
}
wait_http() { for _ in $(seq 60); do curl -fsS "$1" >/dev/null 2>&1 && return 0; sleep 0.5; done; return 1; }
pw() { cp "$here/$1" "$PW/$1"; (cd "$PW" && node "$@"); }
stop_serve() { pkill -INT -x gorun-otel 2>/dev/null || true; pkill -INT -x gorun 2>/dev/null || true; sleep 3; }

# --- OpenTelemetry: gorun -serve sends traces, metrics and logs to Grafana's collector -----------------
docker rm -f gorun-lgtm >/dev/null 2>&1 || true
docker run -d --name gorun-lgtm -p 127.0.0.1:3000:3000 -p 127.0.0.1:4317:4317 -p 127.0.0.1:4318:4318 \
  grafana/otel-lgtm:latest >/dev/null
wait_http http://127.0.0.1:3000/api/health; sleep 15

port=$((20000 + RANDOM % 20000))
(OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 OTEL_METRIC_EXPORT_INTERVAL=3000 GORUN_NAME=gorun GORUN_ADDRESS=127.0.0.1:$port \
  as_dev "$work/bin/gorun-otel" -serve Gorunfile > "$work/otel-serve.log" 2>&1 &)
wait_http "http://127.0.0.1:$port/"
pw runweb.mjs "http://127.0.0.1:$port" 3
sleep 8
pw grafana.mjs http://127.0.0.1:3000 "$out"
stop_serve
[[ ${KEEP:-} == 1 ]] && { trap - EXIT; echo "Grafana left running on :3000; remove with: docker rm -f gorun-lgtm"; exit 0; }
docker rm -f gorun-lgtm >/dev/null
rm -f "$web"/h.db*

# --- NATS, embedded: gorun -serve starts its own server; the CLI and a subscriber use it ------------
port=$((20000 + RANDOM % 20000))
(GORUN_ADDRESS=127.0.0.1:$port as_dev "$work/bin/gorun" -serve Gorunfile > "$work/embedded.log" 2>&1 &)
wait_http "http://127.0.0.1:$port/"; sleep 1
(as_dev "$work/bin/natssub" > "$out/natssub.txt" 2>&1 &)
sleep 1
as_dev "$work/bin/gorun" Gorunfile -r --plain --server 'web-*' --task disk > "$out/cli-run.txt" 2>&1 || true
sleep 1
pw natsweb.mjs "http://127.0.0.1:$port" "$out" embedded
cp "$work/embedded.log" "$out/serve-embedded.txt"
pkill -x natssub || true; stop_serve

# --- NATS, external: gorun -serve joins a server that is already running ----------------------------
docker rm -f gorun-nats >/dev/null 2>&1 || true
docker run -d --name gorun-nats -p 127.0.0.1:4222:4222 nats:2.10-alpine >/dev/null; sleep 2
port=$((20000 + RANDOM % 20000))
(GORUN_NATS_CLIENT_URL=nats://127.0.0.1:4222 GORUN_ADDRESS=127.0.0.1:$port as_dev "$work/bin/gorun" -serve Gorunfile > "$work/external.log" 2>&1 &)
wait_http "http://127.0.0.1:$port/"; sleep 1
pw natsweb.mjs "http://127.0.0.1:$port" "$out" external
cp "$work/external.log" "$out/serve-external.txt"
