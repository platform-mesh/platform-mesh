#!/bin/bash

# Script to create Invite resources for organization owners
# For each org workspace under :root:orgs, looks up the creator email from
# the Account resource in :root:orgs and creates an Invite in that org's workspace.

set -euo pipefail

# Configuration - can be overridden via environment variables
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
DRY_RUN="${DRY_RUN:-false}"
DEBUG="${DEBUG:-false}"

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

# Get creator email from Account resource in :root:orgs
get_creator_email() {
    local org_name="$1"

    local account_yaml
    account_yaml=$(kubectl get account "$org_name" -o yaml 2>&1) || {
        log_error "  Failed to get Account resource '$org_name': $account_yaml"
        return 1
    }

    local creator_email
    creator_email=$(echo "$account_yaml" | yq -r '.spec.creator' 2>&1) || {
        log_error "  Failed to extract creator email from Account '$org_name'"
        return 1
    }

    if [ -z "$creator_email" ] || [ "$creator_email" = "null" ]; then
        log_warn "  Account '$org_name' has no creator email, skipping"
        return 1
    fi

    echo "$creator_email"
}

# Generate Invite resource name from email (use part before @)
generate_invite_name() {
    local email="$1"
    echo "$email" | sed 's/@.*//'
}

# Generate Invite manifest
generate_invite_manifest() {
    local invite_name="$1"
    local email="$2"

    cat <<EOF
apiVersion: core.platform-mesh.io/v1alpha1
kind: Invite
metadata:
  name: ${invite_name}
spec:
  email: ${email}
EOF
}

# Check if Invite already exists in the current workspace by name
has_invite() {
    local invite_name="$1"

    kubectl get invite "$invite_name" &>/dev/null
    return $?
}

# Check if Invite with same email already exists in the current workspace
has_invite_with_email() {
    local email="$1"

    local invites
    invites=$(kubectl get invite -o jsonpath='{.items[*].spec.email}' 2>/dev/null || echo "")

    if [[ " $invites " == *" $email "* ]]; then
        return 0  # Email exists
    fi
    return 1  # Email doesn't exist
}

# Create Invite resource for org owner in org workspace
create_invite_for_owner() {
    local workspace_path="$1"
    local org_name="$2"

    log_info "Processing org workspace: ${workspace_path}"

    # First, get creator email from Account resource (need to be in :root:orgs for this)
    # Switch to :root:orgs to read Account
    kubectl ws :root:orgs &>/dev/null || {
        log_error "  Failed to navigate to :root:orgs to read Account"
        return 1
    }

    local creator_email
    if ! creator_email=$(get_creator_email "$org_name"); then
        return 0  # Continue to next org instead of failing
    fi

    log_info "  Found creator email: ${creator_email}"

    # Now switch to org workspace to create the Invite
    kubectl ws "${workspace_path}" &>/dev/null || {
        log_error "  Failed to navigate to workspace: ${workspace_path}"
        return 1
    }

    # Generate invite name from email
    local invite_name
    invite_name=$(generate_invite_name "$creator_email")

    log_debug "  Invite name: ${invite_name}"

    # Check if invite with same name already exists
    if has_invite "${invite_name}"; then
        log_warn "  Invite '${invite_name}' already exists, skipping"
        return 0
    fi

    # Check if invite with same email already exists
    if has_invite_with_email "${creator_email}"; then
        log_warn "  Invite with email '${creator_email}' already exists, skipping"
        return 0
    fi

    log_info "  Creating Invite for '${creator_email}'..."

    local manifest
    manifest=$(generate_invite_manifest "$invite_name" "$creator_email")

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "  Dry run - would create Invite:"
        echo "$manifest" | yq '.' || echo "$manifest"
        return 0
    fi

    if echo "$manifest" | kubectl apply -f - 2>/dev/null; then
        log_success "  Created Invite '${invite_name}' in ${workspace_path}"
        return 0
    else
        log_error "  Failed to create Invite '${invite_name}' in ${workspace_path}"
        return 1
    fi
}

# Show help
show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Create Invite resources for organization owners in all org workspaces under :root:orgs.
Looks up the creator email from Account resources in :root:orgs and creates
corresponding Invite resources in each org's workspace.

Options:
  --dry-run                    Show what would be done without making changes
  --debug                      Enable debug output
  --kubeconfig-kcp PATH        Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)
  -h, --help                   Show this help message

Environment variables:
  KUBECONFIG_KCP               Kubeconfig for kcp
  DRY_RUN                      Set to "true" for dry-run mode
  DEBUG                        Set to "true" for debug output

Example:
  # Create invites for all org owners
  $0

  # Dry run to preview invites
  $0 --dry-run

  # Using environment variables
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

    # Create Invite resources for each org owner
    for org in $top_level_orgs; do
        create_invite_for_owner ":root:orgs:${org}" "$org" || true
    done

    # Return to root workspace
    kubectl ws :root &>/dev/null || true

    echo ""
    log_success "Completed creating Invite resources for org owners"
}

main "$@"
