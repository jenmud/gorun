#!/bin/sh
# Install docker and the compose plugin. The task's check skips this when they are already there.
set -eu

# a fresh cloud VM may still be installing updates
cloud-init status --wait >/dev/null 2>&1 || true

curl -fsSL https://get.docker.com | sh
