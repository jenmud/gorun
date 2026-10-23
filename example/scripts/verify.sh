#!/bin/sh
# Checks that every tool is installed and the shell profile is in place.
# Fails on the first missing piece. Runs as the SSH user (no sudo).
set -eu

for tool in docker htop btop git jj go nvim rg fd fzf tmux jq make; do
    command -v "$tool" >/dev/null || { echo "missing: $tool" >&2; exit 1; }
done

docker --version
docker compose version
go version
jj --version
nvim --version | head -1

grep -q 'alias vim=nvim' /etc/profile.d/editor.sh || { echo "vim alias not configured" >&2; exit 1; }
id deploy
