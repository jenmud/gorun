#!/bin/sh
# Installs Go and puts each user's $GOPATH/bin on the PATH for login shells.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

apk add --no-cache go

cat > /etc/profile.d/go.sh <<'PROFILE'
export GOPATH="$HOME/go"
export PATH="$PATH:$GOPATH/bin"
PROFILE

go version
