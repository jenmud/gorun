#!/bin/sh
set -e

REPO="jenmud/gorun"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="gorun"

if command -v curl >/dev/null 2>&1; then
  :
else
  echo "curl is required"
  exit 1
fi

mkdir -p "$INSTALL_DIR"
echo "Installing gorun from source (no GitHub releases found)..."
TMPDIR="$(mktemp -d)"
cd "$TMPDIR"
if command -v git >/dev/null 2>&1; then
  git clone --depth 1 "https://github.com/${REPO}.git" repo 2>/dev/null || true
fi
if [ -d repo ]; then
  cd repo
else
  # try to download tarball
  curl -fsSL "https://github.com/${REPO}/archive/refs/heads/master.tar.gz" -o src.tar.gz 2>/dev/null || \
  curl -fsSL "https://github.com/${REPO}/archive/refs/heads/main.tar.gz" -o src.tar.gz 2>/dev/null
  tar xzf src.tar.gz
  cd gorun-* 2>/dev/null || cd */ 2>/dev/null
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build from source. Please install Go or download a prebuilt binary from https://github.com/${REPO}/releases"
  exit 1
fi

go build -o "$BINARY_NAME" ./app/tui
chmod +x "$BINARY_NAME"

mv "$BINARY_NAME" "${INSTALL_DIR}/${BINARY_NAME}"

echo "gorun installed to ${INSTALL_DIR}/${BINARY_NAME}"
rm -rf "$TMPDIR"
