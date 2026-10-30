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
# Allow "latest" to resolve to the newest published release.
case "$VERSION" in
  ""|latest|Latest|LATEST) VERSION="" ;;
esac
if [ -z "$VERSION" ]; then
  # releases/latest is the newest release that isn't a pre-release (such as an old beta)
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi
# No hardcoded fallback: if the version cannot be resolved, skip the prebuilt
# download and build from the latest source instead.

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
  # Wrapper that keeps the OTELC build's startup logs off the terminal. They are
  # written to stdout before main runs, so only the environment can quiet them.
  cat > "${INSTALL_DIR}/gorun" <<'EOF'
#!/bin/sh
DIR="$(cd "$(dirname "$0")" && pwd)"
# Telemetry export is opt-in. Without an endpoint or exporter, disable OTLP so
# an instrumented build does not try localhost:4318 and log connection errors.
case "${OTEL_EXPORTER_OTLP_ENDPOINT:-}${OTEL_EXPORTER_OTLP_TRACES_ENDPOINT:-}${OTEL_EXPORTER_OTLP_METRICS_ENDPOINT:-}${OTEL_EXPORTER_OTLP_LOGS_ENDPOINT:-}${OTEL_TRACES_EXPORTER:-}${OTEL_METRICS_EXPORTER:-}${OTEL_LOGS_EXPORTER:-}" in
  "") export OTEL_TRACES_EXPORTER=none OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=none ;;
esac
export OTEL_LOG_LEVEL="${OTEL_LOG_LEVEL:-warn}"
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
  if [ -n "$VERSION" ]; then
    git clone --depth 1 --branch "$VERSION" "https://github.com/${REPO}.git" repo 2>/dev/null || true
  else
    git clone --depth 1 "https://github.com/${REPO}.git" repo 2>/dev/null || true
  fi
fi
if [ -d repo ]; then
  cd repo
else
  if [ -n "$VERSION" ]; then
    curl -fsSL "https://github.com/${REPO}/archive/refs/tags/${VERSION}.tar.gz" -o src.tar.gz 2>/dev/null || \
    curl -fsSL "https://github.com/${REPO}/archive/refs/tags/${VERSION#v}.tar.gz" -o src.tar.gz 2>/dev/null
  else
    curl -fsSL "https://github.com/${REPO}/archive/refs/heads/master.tar.gz" -o src.tar.gz 2>/dev/null || \
    curl -fsSL "https://github.com/${REPO}/archive/refs/heads/main.tar.gz" -o src.tar.gz 2>/dev/null
  fi
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
# Telemetry export is opt-in. Without an endpoint or exporter, disable OTLP so
# an instrumented build does not try localhost:4318 and log connection errors.
case "${OTEL_EXPORTER_OTLP_ENDPOINT:-}${OTEL_EXPORTER_OTLP_TRACES_ENDPOINT:-}${OTEL_EXPORTER_OTLP_METRICS_ENDPOINT:-}${OTEL_EXPORTER_OTLP_LOGS_ENDPOINT:-}${OTEL_TRACES_EXPORTER:-}${OTEL_METRICS_EXPORTER:-}${OTEL_LOGS_EXPORTER:-}" in
  "") export OTEL_TRACES_EXPORTER=none OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=none ;;
esac
for a in "$@"; do
  case "$a" in
    -version|--version|-help|--help|-h|-dump|--dump) export OTEL_LOG_LEVEL=error ;;
  esac
done
exec "$DIR/gorun-real" "$@"
EOF
chmod +x "${INSTALL_DIR}/gorun"
