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

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load .env from repo root if present
ENV_FILE="$SCRIPT_DIR/../.env"
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck source=/dev/null
  source "$ENV_FILE"
  set +a
fi

KCP_KUBECONFIG="$PORTAL_DIRECTORY/upstream/.secret/kcp/admin.kubeconfig"
KCP_WORKSPACE_PATH="root:providers:search"

OUTPUT_KUBECONFIG="$SCRIPT_DIR/../admin.kubeconfig"

echo "Checking for Kind cluster 'platform-mesh'..."
if [ -n "${KIND_EXPERIMENTAL_PROVIDER:-}" ]; then
  export KIND_EXPERIMENTAL_PROVIDER
fi
if ! kind get clusters 2>/dev/null | grep -q "^platform-mesh$"; then
  echo "Error: Kind cluster 'platform-mesh' not found"
  echo "Available clusters:"
  kind get clusters 2>/dev/null || echo "(none)"
  exit 1
fi

if [ ! -f "$KCP_KUBECONFIG" ]; then
    echo "Error: kcp kubeconfig not found at $KCP_KUBECONFIG"
    exit 1
fi

# Derive the base server URL from the existing default cluster entry and append the workspace path
BASE_SERVER="$(yq eval '.clusters[] | select(.name == "default") | .cluster.server' "$KCP_KUBECONFIG")"
if [ -z "$BASE_SERVER" ]; then
  echo "Error: could not find 'default' cluster entry in $KCP_KUBECONFIG"
  exit 1
fi
# Strip any existing /clusters/... suffix so we always start from the shard root
BASE_URL="${BASE_SERVER%%/clusters/*}"
KCP_SERVER="${BASE_URL}/clusters/${KCP_WORKSPACE_PATH}"

echo "Copying kubeconfig and setting server to $KCP_SERVER..."

cp "$KCP_KUBECONFIG" "$OUTPUT_KUBECONFIG"
yq eval -i '(.clusters[] | select(.name == "default") | .cluster.server) = "'"$KCP_SERVER"'"' "$OUTPUT_KUBECONFIG"

echo ""
echo "Successfully wrote kubeconfig to $OUTPUT_KUBECONFIG"
