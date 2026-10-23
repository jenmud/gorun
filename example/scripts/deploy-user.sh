#!/bin/sh
# Creates the unprivileged deploy user and adds it to the docker group.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

DEPLOY_USER=deploy

id "$DEPLOY_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash "$DEPLOY_USER"
usermod -aG docker "$DEPLOY_USER"
