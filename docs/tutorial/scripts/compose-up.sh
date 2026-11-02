#!/bin/sh
# Start or update the stack, then reload caddy in case the Caddyfile changed.
set -eu

cd /opt/myapp
docker compose up -d --remove-orphans
docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile
