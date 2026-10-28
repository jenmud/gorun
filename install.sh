#!/bin/sh
set -e

REPO="jenda/gorun"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="gorun"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l) ARCH="arm" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
if command -v curl >/dev/null 2>&1; then
  TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')"
else
  echo "curl is required"; exit 1
fi

if [ -z "$TAG" ]; then
  echo "Could not determine latest release tag"; exit 1
fi

# Construct download URL (assumes release assets named as gorun-$OS-$ARCH)
ASSET="${BINARY_NAME}-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

echo "Downloading ${URL}..."
TMP="$(mktemp)"
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BINARY_NAME}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "$TMP" "${INSTALL_DIR}/${BINARY_NAME}"
fi

echo "gorun installed to ${INSTALL_DIR}/${BINARY_NAME}"
