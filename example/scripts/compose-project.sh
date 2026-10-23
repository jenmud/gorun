#!/bin/sh
# Lays down the app's compose project in /srv/app and validates it as the deploy user.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

DEPLOY_USER=deploy
APP_DIR=/srv/app

mkdir -p "$APP_DIR"

cat > "$APP_DIR/compose.yml" <<'COMPOSE'
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost/"]
      interval: 30s
      timeout: 5s
      retries: 3
COMPOSE

chown -R "$DEPLOY_USER:" "$APP_DIR"
su - "$DEPLOY_USER" -c "docker compose -f $APP_DIR/compose.yml config --quiet"
