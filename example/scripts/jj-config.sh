#!/bin/sh
# Configures jj for the deploy user (identity and nvim as the editor).
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

DEPLOY_USER=deploy

su - "$DEPLOY_USER" -c '
    jj config set --user user.name "Deploy Bot"
    jj config set --user user.email "deploy@example.com"
    jj config set --user ui.editor nvim
'
