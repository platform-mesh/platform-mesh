#!/bin/bash

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

set -e

# Token file path - kubeconfig reads from this file
TOKEN_FILE="/tmp/token"

echo "[setup] Terminal session starting..."
echo "[setup] Waiting for authentication token from client..."

# Disable echo to prevent token from being displayed
stty -echo 2>/dev/null || true

# Read token from stdin (sent by portal as first INPUT after ttyd handshake)
# Using timeout to avoid hanging forever if no token is sent
if ! read -r -t 30 TOKEN; then
    stty echo 2>/dev/null || true
    echo "[setup] ERROR: Timeout waiting for authentication token" >&2
    exit 1
fi

# Re-enable echo
stty echo 2>/dev/null || true

echo "[setup] Token received (length: ${#TOKEN})"

if [ -z "${TOKEN}" ]; then
    echo "[setup] ERROR: Empty authentication token" >&2
    exit 1
fi

# Validate user identity if EXPECTED_USER_ID is set
if [ -n "${EXPECTED_USER_ID}" ]; then
    echo "[setup] Validating user identity..."

    # Decode JWT payload (base64url encoded middle section)
    JWT_PAYLOAD_B64=$(echo "${TOKEN}" | cut -d'.' -f2 | tr '_-' '/+')

    # Add padding if needed
    case $((${#JWT_PAYLOAD_B64} % 4)) in
        2) JWT_PAYLOAD_B64="${JWT_PAYLOAD_B64}==" ;;
        3) JWT_PAYLOAD_B64="${JWT_PAYLOAD_B64}=" ;;
    esac

    JWT_PAYLOAD=$(echo "${JWT_PAYLOAD_B64}" | base64 -d 2>/dev/null || true)

    if [ -z "${JWT_PAYLOAD}" ]; then
        echo "[setup] ERROR: Invalid token format - could not decode JWT" >&2
        exit 1
    fi

    # Extract user identity from JWT
    TOKEN_USER=$(echo "${JWT_PAYLOAD}" | jq -r '.sub // empty' 2>/dev/null || true)

    if [ -z "${TOKEN_USER}" ]; then
        echo "[setup] ERROR: Could not extract user identity from token" >&2
        exit 1
    fi

    if [ "${TOKEN_USER}" != "${EXPECTED_USER_ID}" ]; then
        echo "[setup] ERROR: Access denied - user mismatch" >&2
        echo "[setup] Expected: ${EXPECTED_USER_ID:0:8}..., Got: ${TOKEN_USER:0:8}..." >&2
        exit 1
    fi

    echo "[setup] User verified: ${TOKEN_USER}"
else
    echo "[setup] No EXPECTED_USER_ID set, skipping user validation"
fi

# Validate required environment variables
if [ -z "${KCP_WORKSPACE_URL}" ]; then
    echo "[setup] ERROR: KCP_WORKSPACE_URL not set" >&2
    exit 1
fi

if [ -z "${KCP_CA_DATA}" ]; then
    echo "[setup] ERROR: KCP_CA_DATA not set" >&2
    exit 1
fi

# Write token to file (for token refresh support)
echo -n "${TOKEN}" > "${TOKEN_FILE}"
chmod 600 "${TOKEN_FILE}"

echo "[setup] Creating kubeconfig for: ${KCP_WORKSPACE_URL}"

# Create kubeconfig with inline token and CA certificate
export KUBECONFIG=/tmp/kubeconfig
cat > "${KUBECONFIG}" << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${KCP_WORKSPACE_URL}
    certificate-authority-data: ${KCP_CA_DATA}
  name: kcp
contexts:
- context:
    cluster: kcp
    user: user
  name: default
current-context: default
users:
- name: user
  user:
    token: ${TOKEN}
EOF
chmod 600 "${KUBECONFIG}"

# Clear token from environment
unset TOKEN

# Ensure HOME and KUBECONFIG are properly set for interactive tools like k9s
export HOME=/home/terminal
export KUBECONFIG=/tmp/kubeconfig

# Create config directories for k9s (needed with read-only root filesystem)
mkdir -p "${HOME}/.config/k9s" "${HOME}/.cache" "${HOME}/.local/share/k9s"

# Define function to update token (called by portal when token refreshes)
# Usage: __update_token__ <new_token>
__update_token__() {
    local new_token="$1"
    if [ -n "${new_token}" ]; then
        echo -n "${new_token}" > "${TOKEN_FILE}"
        # Update kubeconfig with new token
        sed -i "s|token: .*|token: ${new_token}|" "${KUBECONFIG}"
        # Clear the command line so user doesn't see the token
        printf "\033[1A\033[2K"
    fi
}
export -f __update_token__

# Ignore token update commands in history
export HISTIGNORE="__update_token__*"

echo "[setup] Connected to kcp workspace: ${KCP_WORKSPACE_URL}"
echo "[setup] KUBECONFIG=${KUBECONFIG}"
echo ""
echo "Welcome to the Platform Mesh Terminal!"
echo ""
echo "Available tools:"
echo "  - kubectl (alias: k)  - Kubernetes CLI"
echo "  - k9s                 - Terminal UI (requires /version endpoint access)"
echo ""
echo "Note: This terminal session will be automatically cleaned up after 2 hours."
echo ""

# Start interactive login shell (sources /etc/profile.d/)
exec /bin/bash --login
