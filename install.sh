#!/bin/sh
set -e

REPO="jenmud/gorun"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="gorun"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armhf) ARCH="arm" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

if [ "$OS" = "darwin" ]; then
  OS="darwin"
else
  OS="linux"
fi

if command -v curl >/dev/null 2>&1; then
  :
else
  echo "curl is required"
  exit 1
fi

echo "Downloading latest release..."
TMP="$(mktemp)"
# Try to download from releases - look for common naming
ASSET="${BINARY_NAME}_${OS}_${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"

if ! curl -fsSL "$URL" -o "$TMP" 2>/dev/null; then
  # try without underscore style
  ASSET2="${BINARY_NAME}-${OS}-${ARCH}"
  URL2="https://github.com/${REPO}/releases/latest/download/${ASSET2}"
  if ! curl -fsSL "$URL2" -o "$TMP" 2>/dev/null; then
    echo "Could not download binary from latest release."
    echo "Please download from https://github.com/${REPO}/releases/latest"
    exit 1
  fi
fi

chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BINARY_NAME}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "$TMP" "${INSTALL_DIR}/${BINARY_NAME}"
fi

echo "gorun installed to ${INSTALL_DIR}/${BINARY_NAME}"
