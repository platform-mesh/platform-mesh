#!/bin/bash
#
# Script to fix the APIExport in kcp after the 0.2.0 migration.
# Deletes the core.platform-mesh.io APIExport in :root:platform-mesh-system
# and restarts the platform-mesh-operator pods so it gets recreated with
# the updated schema.
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
KUBECONFIG_K8S="${KUBECONFIG_K8S:-}"
NAMESPACE="platform-mesh-system"

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

    for tool in kubectl; do
        if ! command -v "$tool" &> /dev/null; then
            missing+=("$tool")
        fi
    done

    if [ ${#missing[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing[*]}"
        exit 1
    fi
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
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --kubeconfig)
                KUBECONFIG_K8S="$2"
                shift 2
                ;;
            --kubeconfig-kcp)
                KUBECONFIG_KCP="$2"
                shift 2
                ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Delete the core.platform-mesh.io APIExport in kcp and restart"
                echo "the platform-mesh-operator pods so the APIExport is recreated."
                echo ""
                echo "Options:"
                echo "  --dry-run              Show what would be done without making changes"
                echo "  --namespace NS         Kubernetes namespace (default: platform-mesh-system)"
                echo "  --kubeconfig PATH      Kubeconfig for the Kubernetes cluster (default: \$KUBECONFIG_K8S or \$KUBECONFIG)"
                echo "  --kubeconfig-kcp PATH  Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)"
                echo "  -h, --help             Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  KUBECONFIG_K8S         Kubeconfig for the Kubernetes cluster"
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

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Running in dry-run mode - no changes will be applied"
    fi

    # --- kcp: delete the APIExport ---
    log_info "=== Deleting APIExport in kcp ==="

    local kcp_args=()
    if [ -n "$KUBECONFIG_KCP" ]; then
        kcp_args+=(--kubeconfig "$KUBECONFIG_KCP")
        log_info "Using kcp kubeconfig: $KUBECONFIG_KCP"
    fi

    log_info "Navigating to :root:platform-mesh-system workspace..."
    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Dry run - would run: kubectl ${kcp_args[*]+"${kcp_args[*]}"} ws :root:platform-mesh-system"
        log_warn "Dry run - would run: kubectl ${kcp_args[*]+"${kcp_args[*]}"} delete apiexport core.platform-mesh.io"
    else
        kubectl "${kcp_args[@]+"${kcp_args[@]}"}" ws :root:platform-mesh-system
        log_info "Deleting APIExport core.platform-mesh.io..."
        kubectl "${kcp_args[@]+"${kcp_args[@]}"}" delete apiexport core.platform-mesh.io
        log_info "APIExport deleted"
    fi

    # --- k8s: restart the operator ---
    log_info "=== Restarting platform-mesh-operator pods ==="

    local k8s_args=()
    if [ -n "$KUBECONFIG_K8S" ]; then
        k8s_args+=(--kubeconfig "$KUBECONFIG_K8S")
        log_info "Using k8s kubeconfig: $KUBECONFIG_K8S"
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Dry run - would run: kubectl ${k8s_args[*]+"${k8s_args[*]}"} -n $NAMESPACE delete pod -l app=platform-mesh-operator"
    else
        kubectl "${k8s_args[@]+"${k8s_args[@]}"}" -n "$NAMESPACE" delete pod -l app=platform-mesh-operator
        log_info "platform-mesh-operator pods restarted"
    fi

    echo ""
    log_info "=== APIExport fix complete ==="
}

main "$@"
