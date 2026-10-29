#!/bin/sh
set -e

REPO="jenmud/gorun"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="gorun"
OTEL_BINARY_NAME="gorun-otel"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux) OS=linux; OS_TITLE=Linux ;;
  darwin) OS=darwin; OS_TITLE=Darwin ;;
  *) echo "unsupported OS: $OS"; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64; ARCH_NAME=x86_64 ;;
  aarch64|arm64) ARCH=arm64; ARCH_NAME=arm64 ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

mkdir -p "$INSTALL_DIR"

VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi
if [ -z "$VERSION" ]; then
  VERSION="v0.0.1-beta.1"
fi

VERSION_NO_V="${VERSION#v}"

# Which variants to install (both, normal, otel). Prompt when interactive.
INSTALL_VARIANTS="${INSTALL_VARIANTS:-}"
if [ -z "$INSTALL_VARIANTS" ]; then
  if [ -t 0 ]; then
    printf "Install which binaries? [both/normal/otel] (default: both): "
    read -r answer
    case "$answer" in
      normal|otel|both) INSTALL_VARIANTS="$answer" ;;
      "") INSTALL_VARIANTS="both" ;;
      *) INSTALL_VARIANTS="both" ;;
    esac
  else
    INSTALL_VARIANTS="both"
  fi
fi

case "$INSTALL_VARIANTS" in
  normal) INSTALL_NORMAL=1 ;;
  otel) INSTALL_OTEL=1 ;;
  *) INSTALL_NORMAL=1; INSTALL_OTEL=1 ;;
esac

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

install_asset() {
  name="$1"
  asset="$2"
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
  if curl -fsSL "$url" -o "${TMPDIR}/${asset}" 2>/dev/null; then
    tar -xzf "${TMPDIR}/${asset}" -C "$TMPDIR"
    BIN_PATH="$(find "$TMPDIR" -type f -name "$name" | head -n 1)"
    if [ -z "$BIN_PATH" ]; then
      echo "binary ${name} not found in archive"
      return 1
    fi
    chmod +x "$BIN_PATH"
    mv "$BIN_PATH" "${INSTALL_DIR}/${name}"
    echo "Installed ${name} ${VERSION} to ${INSTALL_DIR}/${name}"
    return 0
  fi
  return 1
}

# Normal build (all platforms).
if [ -n "$INSTALL_NORMAL" ]; then
ASSET="gorun_${VERSION_NO_V}_${OS_TITLE}_${ARCH_NAME}.tar.gz"
if ! install_asset "$BINARY_NAME" "$ASSET"; then
  echo "No prebuilt binary found for ${OS}/${ARCH} (${VERSION}); building from source..."
  BUILD_FROM_SOURCE=1
fi

fi

# OpenTelemetry-instrumented build (Linux x86_64 only in releases).
if [ -n "$INSTALL_OTEL" ]; then
OTEL_ASSET="gorun-otel_${VERSION_NO_V}_${OS_TITLE}_${ARCH_NAME}.tar.gz"
if install_asset "$OTEL_BINARY_NAME" "$OTEL_ASSET"; then
  :
else
  if [ "$OS" = "linux" ] && [ "$ARCH" = "amd64" ]; then
    BUILD_OTEL_FROM_SOURCE=1
  fi
fi
fi

if [ -n "$BUILD_FROM_SOURCE" ] || [ -n "$BUILD_OTEL_FROM_SOURCE" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build from source. Please install Go or download a prebuilt binary from https://github.com/${REPO}/releases"
    exit 1
  fi

  SRC_TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR" "$SRC_TMPDIR"' EXIT
  cd "$SRC_TMPDIR"
  if command -v git >/dev/null 2>&1; then
    git clone --depth 1 "https://github.com/${REPO}.git" repo 2>/dev/null || true
  fi
  if [ -d repo ]; then
    cd repo
  else
    curl -fsSL "https://github.com/${REPO}/archive/refs/heads/master.tar.gz" -o src.tar.gz 2>/dev/null || \
    curl -fsSL "https://github.com/${REPO}/archive/refs/heads/main.tar.gz" -o src.tar.gz 2>/dev/null
    tar xzf src.tar.gz
    cd gorun-* 2>/dev/null || cd */ 2>/dev/null
  fi

  if [ -n "$BUILD_FROM_SOURCE" ] && [ -n "$INSTALL_NORMAL" ]; then
    go build -o "$BINARY_NAME" ./cmd
    chmod +x "$BINARY_NAME"
    mv "$BINARY_NAME" "${INSTALL_DIR}/${BINARY_NAME}"
    echo "gorun installed to ${INSTALL_DIR}/${BINARY_NAME}"
  fi

  if [ -n "$BUILD_OTEL_FROM_SOURCE" ] && [ -n "$INSTALL_OTEL" ]; then
    if command -v go >/dev/null 2>&1 && go tool otelc 2>/dev/null; then
      go tool otelc go build -o "$OTEL_BINARY_NAME" ./cmd
      chmod +x "$OTEL_BINARY_NAME"
      mv "$OTEL_BINARY_NAME" "${INSTALL_DIR}/${OTEL_BINARY_NAME}"
      echo "gorun-otel installed to ${INSTALL_DIR}/${OTEL_BINARY_NAME}"
    else
      echo "OpenTelemetry tool (otelc) not available; skipping gorun-otel build"
    fi
  fi
fi
