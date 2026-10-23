#!/bin/sh
# Installs the base packages every server needs. Safe to re-run.
# Runs as root (sudo = true in the Gorunfile).
set -eu

if ! command -v apk >/dev/null 2>&1; then
    echo "unsupported distro: this script targets Alpine (apk)" >&2
    exit 1
fi

apk update
apk add --no-cache ca-certificates curl tzdata shadow bash
