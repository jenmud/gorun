#!/bin/sh
# Installs the Docker CLI and compose plugin and makes sure the docker group exists.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

apk add --no-cache docker-cli docker-cli-compose

getent group docker >/dev/null || groupadd docker
