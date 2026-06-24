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
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SECRET_DIR="$PROJECT_ROOT/.secret"
KCP_KUBECONFIG="$PROJECT_ROOT/../helm-charts/.secret/kcp/admin.kubeconfig"
KCP_SERVER="https://localhost:8443/clusters/root:platform-mesh-system"

KUBECONFIG_YAML="$SECRET_DIR/operator.yaml"

echo "Checking for Kind cluster 'platform-mesh'..."

if ! kind get clusters | grep -q "^platform-mesh$"; then
    echo "Error: Kind cluster 'platform-mesh' not found"
    echo "Available clusters:"
    kind get clusters
    exit 1
fi

echo ""
echo "Retrieving credentials from KCP kubeconfig..."

if [ ! -f "$KCP_KUBECONFIG" ]; then
    echo "Error: KCP kubeconfig not found at $KCP_KUBECONFIG"
    exit 1
fi

CA_DATA=$(yq eval '.clusters[] | select(.name == "workspace.kcp.io/current") | .cluster.certificate-authority-data' "$KCP_KUBECONFIG")
CLIENT_CERT_DATA=$(yq eval '.users[] | select(.name == "kcp-admin") | .user.client-certificate-data' "$KCP_KUBECONFIG")
CLIENT_KEY_DATA=$(yq eval '.users[] | select(.name == "kcp-admin") | .user.client-key-data' "$KCP_KUBECONFIG")

if [ "$CA_DATA" == "null" ] || [ -z "$CA_DATA" ]; then
    echo "Error: Failed to extract certificate-authority-data from kubeconfig"
    exit 1
fi

if [ "$CLIENT_CERT_DATA" == "null" ] || [ -z "$CLIENT_CERT_DATA" ]; then
    echo "Error: Failed to extract client-certificate-data from kubeconfig"
    exit 1
fi

if [ "$CLIENT_KEY_DATA" == "null" ] || [ -z "$CLIENT_KEY_DATA" ]; then
    echo "Error: Failed to extract client-key-data from kubeconfig"
    exit 1
fi

if [ ! -f "$KUBECONFIG_YAML" ]; then
  echo "Error: operator.yaml not found at $KUBECONFIG_YAML"
  exit 1
fi

echo "Updating certificate-authority-data and user info in $KUBECONFIG_YAML"
yq eval ".clusters[0].cluster.certificate-authority-data = \"$CA_DATA\"" -i "$KUBECONFIG_YAML"
yq eval ".users[0].user.client-certificate-data = \"$CLIENT_CERT_DATA\"" -i "$KUBECONFIG_YAML"
yq eval ".users[0].user.client-key-data = \"$CLIENT_KEY_DATA\"" -i "$KUBECONFIG_YAML"

echo "Updating server URL in $KUBECONFIG_YAML"
yq eval ".clusters[0].cluster.server = \"$KCP_SERVER\"" -i "$KUBECONFIG_YAML"

echo ""
echo "Successfully updated kubeconfig data in $KUBECONFIG_YAML"