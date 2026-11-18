#!/bin/sh
# Installs gorun: the latest release's prebuilt binary, or a build from source when there is
# none for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | sh
#
# VERSION=v1.2.3 installs that release (default: the latest). INSTALL_DIR picks where it goes
# (default: ~/.local/bin). INSTALL_VARIANTS=1 installs the standard build instead of the
# OpenTelemetry-instrumented one; the instrumented one is only built for Linux x86_64, so other
# machines get the standard build either way.
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
case "$VERSION" in
  ""|latest|Latest|LATEST) VERSION="" ;;
esac
if [ -z "$VERSION" ]; then
  # releases/latest is the newest release that isn't a pre-release
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
fi
if [ -z "$VERSION" ]; then
  # the API limits how often it answers; the release page redirects to the tag without one
  VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null |
    sed -n 's|.*/releases/tag/\(.*\)$|\1|p')"
fi
if [ -z "$VERSION" ]; then
  echo "Could not find the latest release; building the newest source instead."
fi
VERSION_NO_V="${VERSION#v}"

case "${INSTALL_VARIANTS:-}" in
  1|true|yes) VARIANT="normal" ;;
  *) VARIANT="otel" ;;
esac

TMPDIR="$(mktemp -d)"
SRC_TMPDIR=""
trap 'rm -rf "$TMPDIR" ${SRC_TMPDIR:+"$SRC_TMPDIR"}' EXIT

# write_wrapper installs "gorun", which runs gorun-real with the OpenTelemetry build's startup
# logs kept off the terminal: they are written before main runs, so only the environment can
# quiet them.
write_wrapper() {
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
}

# installed says what was installed, as the binary itself reports it.
installed() {
  v="$("${INSTALL_DIR}/gorun" -version 2>/dev/null | tail -n 1)" || true
  echo "Installed ${BINARY_NAME} ($1) ${v:-$VERSION} to ${INSTALL_DIR}/${BINARY_NAME}"
}

# download_release installs the release's archive named $1, and fails if it isn't published.
download_release() {
  asset="$1_${VERSION_NO_V}_${OS_TITLE}_${ARCH_NAME}.tar.gz"
  curl -fsSL "https://github.com/${REPO}/releases/download/${VERSION}/${asset}" -o "${TMPDIR}/${asset}" 2>/dev/null || return 1
  tar -xzf "${TMPDIR}/${asset}" -C "$TMPDIR"
  bin="$(find "$TMPDIR" -type f \( -name "gorun" -o -name "gorun-otel" \) | head -n 1)"
  if [ -z "$bin" ]; then
    echo "binary not found in ${asset}"
    exit 1
  fi
  chmod +x "$bin"
  mv "$bin" "${INSTALL_DIR}/gorun-real"
  write_wrapper
}

if [ -n "$VERSION" ]; then
  if [ "$VARIANT" = "otel" ] && download_release gorun-otel; then
    installed otel
    exit 0
  fi
  if [ "$VARIANT" = "otel" ]; then
    echo "No OpenTelemetry build of ${VERSION} for ${OS}/${ARCH}; installing the standard build."
    VARIANT="normal"
  fi
  if download_release gorun; then
    installed "$VARIANT"
    exit 0
  fi
  echo "No prebuilt binary of ${VERSION} for ${OS}/${ARCH}; building from source..."
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build from source. Please install Go or download a prebuilt binary from https://github.com/${REPO}/releases"
  exit 1
fi

SRC_TMPDIR="$(mktemp -d)"
cd "$SRC_TMPDIR"
if command -v git >/dev/null 2>&1; then
  if [ -n "$VERSION" ]; then
    git clone -q --depth 1 --branch "$VERSION" "https://github.com/${REPO}.git" repo 2>/dev/null || true
  else
    git clone -q --depth 1 "https://github.com/${REPO}.git" repo 2>/dev/null || true
  fi
fi
if [ -d repo ]; then
  cd repo
else
  if [ -n "$VERSION" ]; then
    curl -fsSL "https://github.com/${REPO}/archive/refs/tags/${VERSION}.tar.gz" -o src.tar.gz
  else
    curl -fsSL "https://github.com/${REPO}/archive/refs/heads/master.tar.gz" -o src.tar.gz
  fi
  tar xzf src.tar.gz
  cd gorun-* 2>/dev/null || cd */ 2>/dev/null
fi

# A release's version is stamped in; a build of the newest source reports the commit it came from.
LDFLAGS="-X main.version=devel"
if [ -n "$VERSION" ]; then
  LDFLAGS="-X main.version=${VERSION}"
elif [ -d .git ]; then
  LDFLAGS="-X main.version=devel-$(git rev-parse --short=12 HEAD)"
fi
if [ "$VARIANT" = "otel" ] && go tool otelc >/dev/null 2>&1; then
  go tool otelc go build -ldflags "$LDFLAGS" -o "$BINARY_NAME" ./cmd
else
  if [ "$VARIANT" = "otel" ]; then
    echo "otelc tool not available; building the standard version instead"
    VARIANT="normal"
  fi
  go build -ldflags "$LDFLAGS" -o "$BINARY_NAME" ./cmd
fi
chmod +x "$BINARY_NAME"
mv "$BINARY_NAME" "${INSTALL_DIR}/gorun-real"
write_wrapper
installed "$VARIANT"
