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

LOCAL_BIN="$SCRIPT_DIR/../bin/kcp"          # destination path
BUILD_DIR="$SCRIPT_DIR/../bin/build"          # destination path

rm -rf "$BUILD_DIR"

git clone --depth=1 git@github.com:kcp-dev/kcp.git $BUILD_DIR
cd "$BUILD_DIR"
make build

mv "$BUILD_DIR/bin/kcp" "$LOCAL_BIN"
chmod +x "$LOCAL_BIN"

rm -rf "$BUILD_DIR"
