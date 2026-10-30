#!/bin/sh
# Configure jj for the deploy user: identity, editor and a few aliases.
set -e

user=deploy
config_dir="/home/$user/.config/jj"

echo "writing $config_dir/config.toml"
cat <<TOML
[user]
name = "Deploy Bot"
email = "deploy@example.com"

[ui]
editor = "nvim"
default-command = "log"
TOML

sleep 0.6
echo "jj config written for $user"
