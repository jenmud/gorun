#!/bin/sh
# Builds and runs a small Go program as the deploy user to prove the toolchain works.
# Safe to re-run. Runs as root (sudo = true in the Gorunfile).
set -eu

DEPLOY_USER=deploy
APP_DIR="/home/$DEPLOY_USER/src/hello"

mkdir -p "$APP_DIR"

cat > "$APP_DIR/go.mod" <<'GOMOD'
module hello

go 1.22
GOMOD

cat > "$APP_DIR/main.go" <<'GOSRC'
package main

import "fmt"

func main() {
	fmt.Println("hello from gorun")
}
GOSRC

chown -R "$DEPLOY_USER:" "/home/$DEPLOY_USER/src"
su - "$DEPLOY_USER" -c "cd $APP_DIR && go build -o hello . && ./hello"
