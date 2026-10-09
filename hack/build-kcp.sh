#!/usr/bin/env bash

# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Builds the kcp envtest binaries from a kcp-dev/kcp commit that has no release yet.
# Usage: hack/build-kcp.sh COMMIT BIN_DIR

set -euo pipefail

commit="$1"
bin_dir="$2"
tools=(kcp sharded-test-server kcp-front-proxy cache-server)

missing=false
for tool in "${tools[@]}"; do
  [ -x "$bin_dir/$tool-$commit" ] || missing=true
done
if [ "$missing" = false ]; then
  exit 0
fi

src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT

git -C "$src" init --quiet
git -C "$src" fetch --quiet --depth 1 https://github.com/kcp-dev/kcp.git "$commit"
git -C "$src" checkout --quiet FETCH_HEAD

mkdir -p "$bin_dir"
for tool in "${tools[@]}"; do
  echo "Building $tool from kcp-dev/kcp@$commit"
  (cd "$src" && GOWORK=off go build -o "$bin_dir/$tool-$commit" "./cmd/$tool")
done
