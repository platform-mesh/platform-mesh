#!/bin/bash

# Script to create IdentityProviderConfiguration resources in all org workspaces
# Creates missing IdentityProviderConfiguration resources under :root:orgs with
# default client configurations for portal and kubectl.

set -euo pipefail

# Configuration - can be overridden via environment variables
PORTAL_HOST="${PORTAL_HOST:-localhost}"
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
DRY_RUN="${DRY_RUN:-false}"
DEBUG="${DEBUG:-false}"
SECRET_NAMESPACE="${SECRET_NAMESPACE:-default}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_debug() {
    if [ "$DEBUG" = "true" ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

# Check prerequisites
check_prerequisites() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi

    if ! command -v yq &> /dev/null; then
        log_error "yq is not installed or not in PATH"
        exit 1
    fi
}

# Generate IdentityProviderConfiguration manifest
generate_manifest() {
    local workspace_name="$1"
    local portal_host="$2"

    # Build redirect URIs - always include localhost + portal host URIs
    local default_client_redirects='    - http://localhost:8000/callback*
    - http://localhost:4300/callback*'
    local default_client_logout_redirects='    - http://localhost:8000/logout*'

    # Add portal host URIs if not localhost
    if [ -n "$portal_host" ] && [ "$portal_host" != "localhost" ]; then
        default_client_redirects+='
    - https://'"${portal_host}"'/*
    - https://'"${workspace_name}"'.'"${portal_host}"'/*'
        default_client_logout_redirects+='
    - https://'"${portal_host}"'/logout*'
    fi

    # Generate workspace-specific secret names
    local secret_name_default="portal-client-secret-${workspace_name}-${workspace_name}"
    local secret_name_kubectl="portal-client-secret-${workspace_name}-kubectl"

    cat <<EOF
apiVersion: core.platform-mesh.io/v1alpha1
kind: IdentityProviderConfiguration
metadata:
  name: ${workspace_name}
spec:
  clients:
  - clientName: default
    clientType: confidential
    postLogoutRedirectUris:
${default_client_logout_redirects}
    redirectUris:
${default_client_redirects}
    secretRef:
      name: ${secret_name_default}
      namespace: ${SECRET_NAMESPACE}
  - clientName: kubectl
    clientType: public
    redirectUris:
    - http://localhost:8000
    - http://localhost:18000
    secretRef:
      name: ${secret_name_kubectl}
      namespace: ${SECRET_NAMESPACE}
EOF
}

# Check if IdentityProviderConfiguration already exists in the current workspace
has_identity_provider_config() {
    local workspace_name="$1"

    kubectl get identityproviderconfiguration "$workspace_name" &>/dev/null
    return $?
}

# Create IdentityProviderConfiguration for an org in its own workspace
create_org_identity_provider_config() {
    local workspace_path="$1"
    local workspace_name="$2"

    log_info "Processing org workspace: ${workspace_path}"

    # Navigate to the org workspace
    kubectl ws "${workspace_path}" &>/dev/null || {
        log_error "  Failed to navigate to workspace: ${workspace_path}"
        return 1
    }

    if has_identity_provider_config "${workspace_name}"; then
        log_warn "  IdentityProviderConfiguration '${workspace_name}' already exists, skipping"
        return 0
    fi

    log_info "  Creating IdentityProviderConfiguration..."

    local manifest
    manifest=$(generate_manifest "$workspace_name" "$PORTAL_HOST")

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "  Dry run - would create IdentityProviderConfiguration:"
        echo "$manifest" | yq '.' || echo "$manifest"
        return 0
    fi

    if echo "$manifest" | kubectl apply -f - 2>/dev/null; then
        log_success "  Created IdentityProviderConfiguration '${workspace_name}' in ${workspace_path}"
        return 0
    else
        log_error "  Failed to create IdentityProviderConfiguration '${workspace_name}' in ${workspace_path}"
        return 1
    fi
}

# Show help
show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Create IdentityProviderConfiguration resources in all org workspaces under :root:orgs.
Creates missing IdentityProviderConfiguration resources with default client configurations
for portal and kubectl access.

The script automatically generates workspace-specific secret names in the format:
  - portal-client-secret-<workspace>-default
  - portal-client-secret-<workspace>-kubectl

Options:
  --dry-run                    Show what would be done without making changes
  --debug                      Enable debug output
  --portal-host HOST           Portal hostname for redirect URIs (default: \$PORTAL_HOST or localhost)
  --secret-namespace NS        Secret namespace (default: \$SECRET_NAMESPACE or default)
  --kubeconfig-kcp PATH        Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)
  -h, --help                   Show this help message

Environment variables:
  PORTAL_HOST                  Portal hostname for redirect URIs
  SECRET_NAMESPACE             Secret namespace
  KUBECONFIG_KCP               Kubeconfig for kcp
  DRY_RUN                      Set to "true" for dry-run mode
  DEBUG                        Set to "true" for debug output

Example:
  # Create resources in all org workspaces with portal host
  PORTAL_HOST=portal.example.com $0

  # Dry run with custom portal host
  $0 --dry-run --portal-host portal.example.com

  # Using environment variables
  export PORTAL_HOST=portal.example.com
  export KUBECONFIG_KCP=/path/to/kcp/kubeconfig
  $0
EOF
}

# Main
main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --debug)
                DEBUG=true
                shift
                ;;
            --portal-host)
                PORTAL_HOST="$2"
                shift 2
                ;;
            --secret-namespace)
                SECRET_NAMESPACE="$2"
                shift 2
                ;;
            --kubeconfig-kcp)
                KUBECONFIG_KCP="$2"
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done

    check_prerequisites

    if [ -n "$KUBECONFIG_KCP" ]; then
        export KUBECONFIG="$KUBECONFIG_KCP"
        log_info "Using kcp kubeconfig: $KUBECONFIG_KCP"
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Running in dry-run mode - no changes will be applied"
    fi

    log_info "Portal host: $PORTAL_HOST"
    log_info "Secret namespace: ${SECRET_NAMESPACE}"
    log_info "Secret names will be generated per workspace: portal-client-secret-<workspace>-{default,kubectl}"
    echo ""

    # Navigate to :root:orgs to get list of org workspaces
    log_info "Navigating to :root:orgs to list org workspaces"

    if ! kubectl ws :root:orgs &>/dev/null; then
        log_error "Failed to navigate to :root:orgs workspace"
        exit 1
    fi

    # Get all top-level orgs
    local top_level_orgs
    top_level_orgs=$(kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [[ -z "$top_level_orgs" ]]; then
        log_warn "No workspaces found under :root:orgs"
        exit 0
    fi

    log_info "Found top-level orgs: $top_level_orgs"
    echo ""

    # Create IdentityProviderConfiguration in each org workspace
    for org in $top_level_orgs; do
        create_org_identity_provider_config ":root:orgs:${org}" "$org"
    done

    # Return to root workspace
    kubectl ws :root &>/dev/null || true

    echo ""
    log_success "Completed creating IdentityProviderConfiguration resources"
}

main "$@"
