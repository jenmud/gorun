#!/bin/sh
# Install docker and the compose plugin, unless they are already there.
set -eu

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
	echo "docker is already installed"
	exit 0
fi

# a fresh cloud VM may still be installing updates
cloud-init status --wait >/dev/null 2>&1 || true

curl -fsSL https://get.docker.com | sh
