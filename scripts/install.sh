#!/bin/sh
set -e

REPO="jenmud/gorun"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="gorun"

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
  VERSION="v0.0.1-beta.5"
fi

VERSION_NO_V="${VERSION#v}"

# Choose which variant to install as "gorun".
# By default the OpenTelemetry-instrumented (otel) build is used.
# Setting INSTALL_VARIANTS enables an interactive prompt.
# Default to the OpenTelemetry-instrumented build.
# INSTALL_VARIANTS=1 selects the standard (non-OTELC) build.
case "${INSTALL_VARIANTS:-}" in
  1|true|yes) VARIANT="normal" ;;
  *) VARIANT="otel" ;;
esac

# Map variant to release asset name.
if [ "$VARIANT" = "otel" ]; then
  ASSET_BASE="gorun-otel"
else
  ASSET_BASE="gorun"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

ASSET="${ASSET_BASE}_${VERSION_NO_V}_${OS_TITLE}_${ARCH_NAME}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

if curl -fsSL "$URL" -o "${TMPDIR}/${ASSET}" 2>/dev/null; then
  tar -xzf "${TMPDIR}/${ASSET}" -C "$TMPDIR"
  BIN_PATH="$(find "$TMPDIR" -type f \( -name "gorun" -o -name "gorun-otel" \) | head -n 1)"
  if [ -z "$BIN_PATH" ]; then
    echo "binary not found in archive"
    exit 1
  fi
  chmod +x "$BIN_PATH"
  mv "$BIN_PATH" "${INSTALL_DIR}/gorun-real"
  # Wrapper so `gorun -version` stays clean even for the OTELC build.
  cat > "${INSTALL_DIR}/gorun" <<'EOF'
#!/bin/sh
DIR="$(cd "$(dirname "$0")" && pwd)"
for a in "$@"; do
  case "$a" in
    -version|--version|-help|--help|-h|-dump|--dump) export OTEL_LOG_LEVEL=error ;;
  esac
done
exec "$DIR/gorun-real" "$@"
EOF
  chmod +x "${INSTALL_DIR}/gorun"
  echo "Installed ${BINARY_NAME} (${VARIANT}) ${VERSION} to ${INSTALL_DIR}/${BINARY_NAME}"
  exit 0
fi

echo "No prebuilt binary found for ${OS}/${ARCH} (${VERSION}); building from source..."
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

if [ "$VARIANT" = "otel" ]; then
  if command -v go >/dev/null 2>&1 && (go tool otelc >/dev/null 2>&1 || go help tool 2>/dev/null | grep -q otelc); then
    go tool otelc go build -o "$BINARY_NAME" ./cmd
  else
    echo "otelc tool not available; building standard version instead"
    go build -o "$BINARY_NAME" ./cmd
  fi
else
  go build -o "$BINARY_NAME" ./cmd
fi
chmod +x "$BINARY_NAME"
mv "$BINARY_NAME" "${INSTALL_DIR}/gorun-real"

echo "gorun (${VARIANT}) installed to ${INSTALL_DIR}/${BINARY_NAME}"

# Wrapper so `gorun -version` stays clean even for the OTELC build.
cat > "${INSTALL_DIR}/gorun" <<'EOF'
#!/bin/sh
DIR="$(cd "$(dirname "$0")" && pwd)"
for a in "$@"; do
  case "$a" in
    -version|--version|-help|--help|-h|-dump|--dump) export OTEL_LOG_LEVEL=error ;;
  esac
done
exec "$DIR/gorun-real" "$@"
EOF
chmod +x "${INSTALL_DIR}/gorun"
