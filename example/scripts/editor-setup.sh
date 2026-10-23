#!/bin/sh
# Makes neovim the default editor and aliases vim/vi to it for login shells.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

cat > /etc/profile.d/editor.sh <<'PROFILE'
alias vim=nvim
alias vi=nvim
export EDITOR=nvim
export VISUAL=nvim
PROFILE
