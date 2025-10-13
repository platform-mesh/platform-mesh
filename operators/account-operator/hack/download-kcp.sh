#!/usr/bin/env bash
set -euo pipefail
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
  DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
  SOURCE="$(readlink "$SOURCE")"
  [[ "$SOURCE" != /* ]] && SOURCE="$DIR/$SOURCE"
done
SCRIPT_DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
echo "$SCRIPT_DIR"

BIN_DIR="$SCRIPT_DIR/../bin"
mkdir -p "$BIN_DIR"

# Allow overriding the version via environment while defaulting to Taskfile value.
KCP_VERSION="${KCP_VERSION:-0.27.1}"
if [[ "$KCP_VERSION" != v* ]]; then
  KCP_VERSION="v${KCP_VERSION}"
fi

BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kcp-build.XXXXXX")"
trap 'rm -rf "$BUILD_DIR"' EXIT

git clone --depth=1 --branch "$KCP_VERSION" https://github.com/kcp-dev/kcp.git "$BUILD_DIR"

pushd "$BUILD_DIR" >/dev/null
  # Build kcp directly to avoid repository make targets that enforce a specific Go patch version.
  GOWORK=off GOTOOLCHAIN=auto go build -o "$BIN_DIR/kcp" ./cmd/kcp
popd >/dev/null
