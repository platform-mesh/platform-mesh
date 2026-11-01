#!/bin/bash

# Script to traverse all KCP workspaces starting from :root and patch APIBinding resources
# with name prefix "core.platform-mesh.io-" to add a permission claim for secrets.

set -euo pipefail

# Configuration - can be overridden via environment variables
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-${REPO_ROOT}/.secret/kcp/admin.kubeconfig}"
KCP_SERVER="${KCP_SERVER:-https://localhost:8443}"
APIBINDING_PREFIX="core.platform-mesh.io-"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi

    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed or not in PATH"
        exit 1
    fi

    if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
        log_error "Kubeconfig not found at: ${KUBECONFIG_PATH}"
        exit 1
    fi
}

# Permission claim JSON to add
PERMISSION_CLAIM='{
  "group": "",
  "identityHash": "",
  "resource": "secrets",
  "selector": {
    "matchAll": true
  },
  "state": "Accepted",
  "verbs": ["*"]
}'

# Check if secrets permission claim already exists in the APIBinding
has_secrets_permission_claim() {
    local workspace_path=$1
    local apibinding_name=$2
    local server_url="${KCP_SERVER}/clusters/${workspace_path}"

    local existing_claims
    existing_claims=$(kubectl --kubeconfig="${KUBECONFIG_PATH}" get apibinding "${apibinding_name}" \
        --server="${server_url}" \
        -o jsonpath='{.spec.permissionClaims}' 2>/dev/null || echo "[]")

    if [[ -z "${existing_claims}" || "${existing_claims}" == "null" ]]; then
        return 1  # No claims exist
    fi

    # Check if secrets claim already exists
    local has_secrets
    has_secrets=$(echo "${existing_claims}" | jq '[.[] | select(.resource == "secrets")] | length')

    if [[ "${has_secrets}" -gt 0 ]]; then
        return 0  # Secrets claim exists
    fi
    return 1  # No secrets claim
}

# Patch APIBinding to add secrets permission claim
patch_apibinding() {
    local workspace_path=$1
    local apibinding_name=$2
    local server_url="${KCP_SERVER}/clusters/${workspace_path}"

    # Check if permissionClaims array exists
    local existing_claims
    existing_claims=$(kubectl --kubeconfig="${KUBECONFIG_PATH}" get apibinding "${apibinding_name}" \
        --server="${server_url}" \
        -o jsonpath='{.spec.permissionClaims}' 2>/dev/null || echo "")

    local patch_json
    if [[ -z "${existing_claims}" || "${existing_claims}" == "null" ]]; then
        # permissionClaims doesn't exist, create it with the claim
        patch_json=$(jq -n --argjson claim "${PERMISSION_CLAIM}" '[{"op": "add", "path": "/spec/permissionClaims", "value": [$claim]}]')
    else
        # permissionClaims exists, append to it
        patch_json=$(jq -n --argjson claim "${PERMISSION_CLAIM}" '[{"op": "add", "path": "/spec/permissionClaims/-", "value": $claim}]')
    fi

    if kubectl --kubeconfig="${KUBECONFIG_PATH}" patch apibinding "${apibinding_name}" \
        --server="${server_url}" \
        --type='json' \
        -p="${patch_json}" 2>/dev/null; then
        log_success "Patched APIBinding '${apibinding_name}' in workspace '${workspace_path}'"
        return 0
    else
        log_error "Failed to patch APIBinding '${apibinding_name}' in workspace '${workspace_path}'"
        return 1
    fi
}

# Process APIBindings in a workspace
process_apibindings_in_workspace() {
    local workspace_path=$1
    local server_url="${KCP_SERVER}/clusters/${workspace_path}"

    log_info "Processing workspace: ${workspace_path}"

    # Get all APIBindings
    local apibindings_json
    apibindings_json=$(kubectl --kubeconfig="${KUBECONFIG_PATH}" get apibindings \
        --server="${server_url}" \
        -o json 2>/dev/null || echo '{"items":[]}')

    # Filter APIBindings with the prefix
    local matching_apibindings
    matching_apibindings=$(echo "${apibindings_json}" | jq -r \
        --arg prefix "${APIBINDING_PREFIX}" \
        '.items[] | select(.metadata.name | startswith($prefix)) | .metadata.name')

    if [[ -z "${matching_apibindings}" ]]; then
        log_info "  No APIBindings with prefix '${APIBINDING_PREFIX}' found"
        return 0
    fi

    # Process each matching APIBinding
    while IFS= read -r apibinding_name; do
        if [[ -n "${apibinding_name}" ]]; then
            if has_secrets_permission_claim "${workspace_path}" "${apibinding_name}"; then
                log_warning "  APIBinding '${apibinding_name}' already has secrets permission claim, skipping"
            else
                log_info "  Patching APIBinding '${apibinding_name}'..."
                patch_apibinding "${workspace_path}" "${apibinding_name}" || true
            fi
        fi
    done <<< "${matching_apibindings}"
}

# Recursively traverse workspaces
traverse_workspace() {
    local workspace_path=$1
    local server_url="${KCP_SERVER}/clusters/${workspace_path}"

    # Process APIBindings in current workspace
    process_apibindings_in_workspace "${workspace_path}"

    # Get child workspaces
    local workspaces_json
    workspaces_json=$(kubectl --kubeconfig="${KUBECONFIG_PATH}" get workspaces \
        --server="${server_url}" \
        -o json 2>/dev/null || echo '{"items":[]}')

    local child_workspaces
    child_workspaces=$(echo "${workspaces_json}" | jq -r '.items[].metadata.name // empty')

    if [[ -z "${child_workspaces}" ]]; then
        return 0
    fi

    # Recursively process each child workspace
    while IFS= read -r child_name; do
        if [[ -n "${child_name}" ]]; then
            local child_path="${workspace_path}:${child_name}"
            traverse_workspace "${child_path}"
        fi
    done <<< "${child_workspaces}"
}

# Main
main() {
    log_info "Starting KCP workspace traversal for APIBinding patching"
    log_info "Kubeconfig: ${KUBECONFIG_PATH}"
    log_info "KCP Server: ${KCP_SERVER}"
    log_info "APIBinding prefix: ${APIBINDING_PREFIX}"
    echo ""

    check_prerequisites

    # Start traversal from root
    traverse_workspace "root"

    echo ""
    log_success "Workspace traversal completed"
}

main "$@"
