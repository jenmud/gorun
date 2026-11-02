#!/bin/sh
# Log root's docker in to the registry, so compose can pull the private image.
# REGISTRY_USER and REGISTRY_TOKEN come from the task's envvars.
set -eu

printf '%s' "$REGISTRY_TOKEN" | docker login ghcr.io -u "$REGISTRY_USER" --password-stdin
