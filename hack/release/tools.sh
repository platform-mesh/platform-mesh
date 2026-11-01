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

# Source this file and call `require_tools <tool>...` to install the pinned
# versions of the named tools (via `task tools:<tool>`, versions in
# tools/Taskfile.yaml) and get their paths in upper-cased variables:
#
#   source "$(git rev-parse --show-toplevel)/hack/release/tools.sh"
#   require_tools ocm helm yq
#   "$OCM" version; "$HELM" version; "$YQ" --version
#
# Set RELEASE_TOOLS_FROM_PATH=true to skip the installation and use whatever
# is on PATH (for environments without task or network).

# retry <n> <cmd...>: run a command up to n times with a short pause. GHCR
# occasionally answers "tag: not found" right after a successful upload to a
# freshly created package; a second attempt succeeds.
retry() {
  local attempts="$1" i; shift
  for ((i = 1; i <= attempts; i++)); do
    "$@" && return 0
    if (( i < attempts )); then
      echo "attempt $i/$attempts failed: $*; retrying in 10s" >&2
      sleep 10
    fi
  done
  return 1
}

require_tools() {
  local root tool var path
  root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
  for tool in "$@"; do
    var="$(echo "$tool" | tr 'a-z-' 'A-Z_')"
    if [ "${RELEASE_TOOLS_FROM_PATH:-false}" = "true" ]; then
      path="$(command -v "$tool")" || { echo "error: $tool is required" >&2; return 1; }
    else
      command -v task >/dev/null || { echo "error: 'task' (https://taskfile.dev) is required to install $tool; install it or set RELEASE_TOOLS_FROM_PATH=true" >&2; return 1; }
      path="$(UGET_PRINT_PATH=absolute task -d "$root" "tools:$tool")" || return 1
    fi
    export "$var=$path"
  done
}
