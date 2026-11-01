#!/bin/bash
#
# Script to update FGA Store resources with the latest core module model
# from the security-operator chart. Patches spec.coreModule on all Store
# resources in the :root:orgs workspace and clears the stale
# authorizationModelId from their status so the security-operator can
# reconcile them cleanly.
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Global variables
DRY_RUN=false
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"

# The current core module model from the security-operator chart
CORE_MODULE='
module core

type user

type role
  relations
    define assignee: [user,user:*]

type core_platform-mesh_io_account
  relations
    define parent: [core_platform-mesh_io_account]

    define owner: [role#assignee] or owner from parent
    define member: [role#assignee] or owner

    define get: member
    define update: member
    define patch: member
    define delete: owner

    define create_core_platform-mesh_io_accounts: member
    define list_core_platform-mesh_io_accounts: member
    define watch_core_platform-mesh_io_accounts: member

    # org and account specific
    define watch: member

    define create_core_platform-mesh_io_accountinfos: member
    define list_core_platform-mesh_io_accountinfos: member
    define watch_core_platform-mesh_io_accountinfos: member

    define list_core_kcp_io_logicalclusters: member
    define watch_core_kcp_io_logicalclusters: member

    # IAM specific
    define manage_iam_roles: owner
    define get_iam_roles: member
    define get_iam_users: member

type core_platform-mesh_io_accountinfo
  relations
    define parent: [core_platform-mesh_io_account]

    define member: member from parent
    define owner: owner from parent

    define get: member
    define watch: member

    # IAM specific
    define manage_iam_roles: owner
    define get_iam_roles: member
    define get_iam_users: member

type core_kcp_io_logicalcluster
  relations
    define parent: [core_platform-mesh_io_account]

    define member: member from parent

    define get: member
    define watch: member
'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check for required tools
check_dependencies() {
    local missing=()

    for tool in kubectl jq; do
        if ! command -v "$tool" &> /dev/null; then
            missing+=("$tool")
        fi
    done

    if [ ${#missing[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing[*]}"
        exit 1
    fi
}

# Patch a single Store resource with the new core module
patch_store() {
    local store_name="$1"

    local current_module
    current_module=$(kubectl get store "$store_name" -o jsonpath='{.spec.coreModule}' 2>/dev/null || echo "")

    if [ "$current_module" = "$CORE_MODULE" ]; then
        log_info "  Store '$store_name' already up to date, skipping"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "  Dry run - would patch Store '$store_name' with new coreModule"
        return 0
    fi

    local patch
    patch=$(jq -n --arg model "$CORE_MODULE" '{"spec": {"coreModule": $model}}')

    kubectl patch store "$store_name" --type=merge -p "$patch"
    log_info "  Successfully patched Store '$store_name'"
}

# Clear the stale authorizationModelId from a Store's status
clear_authorization_model_id() {
    local store_name="$1"

    local model_id
    model_id=$(kubectl get store "$store_name" -o jsonpath='{.status.authorizationModelId}' 2>/dev/null || echo "")

    if [ -z "$model_id" ]; then
        log_info "  Store '$store_name' has no authorizationModelId, skipping"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "  Dry run - would clear authorizationModelId '$model_id' from Store '$store_name'"
        return 0
    fi

    kubectl patch store "$store_name" --type=json \
        -p '[{"op": "remove", "path": "/status/authorizationModelId"}]' --subresource=status
    log_info "  Cleared authorizationModelId from Store '$store_name'"
}

# Main function
main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --kubeconfig-kcp)
                KUBECONFIG_KCP="$2"
                shift 2
                ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Update FGA Store resources in :root:orgs with the latest core module"
                echo "model from the security-operator chart and clear stale"
                echo "authorizationModelId from their status."
                echo ""
                echo "Options:"
                echo "  --dry-run              Show what would be done without making changes"
                echo "  --kubeconfig-kcp PATH  Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)"
                echo "  -h, --help             Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  KUBECONFIG_KCP         Kubeconfig for kcp"
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done

    check_dependencies

    if [ -n "$KUBECONFIG_KCP" ]; then
        export KUBECONFIG="$KUBECONFIG_KCP"
        log_info "Using kubeconfig: $KUBECONFIG_KCP"
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Running in dry-run mode - no changes will be applied"
    fi

    # Navigate to :root:orgs workspace
    log_info "Navigating to :root:orgs workspace..."
    kubectl ws :root:orgs

    # List all Store resources
    local stores
    stores=$(kubectl get stores -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [ -z "$stores" ]; then
        log_warn "No Store resources found in :root:orgs"
        kubectl ws :root
        exit 0
    fi

    log_info "Found stores: $stores"

    local updated_count=0

    for store_name in $stores; do
        log_info "Processing Store: $store_name"
        patch_store "$store_name"
        clear_authorization_model_id "$store_name"
        ((updated_count++))
    done

    # Return to root workspace
    kubectl ws :root

    echo ""
    log_info "Summary: Processed $updated_count stores"
    log_info "=== FGA Store model update complete ==="
}

main "$@"
