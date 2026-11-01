#!/bin/bash
#
# Lists all APIResourceSchemas and AuthorizationModels across all KCP workspaces.
#
# This script recursively traverses the KCP workspace tree and finds:
# - All APIExport resources and their exported APIResourceSchemas
# - All AuthorizationModel resources
#
# Usage:
#   ./list_apiresourceschemas.sh [OPTIONS]
#
# Options:
#   -d, --details    Show detailed information about each resource
#   -h, --help       Show this help message
#

set -euo pipefail

###############################################################################
# Configuration
###############################################################################

KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
KCP_SERVER="${KCP_SERVER:-}"
SHOW_DETAILS=false

###############################################################################
# Color codes for output
###############################################################################

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

###############################################################################
# Helper functions
###############################################################################

log_info() {
    echo -e "${CYAN}[INFO]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_header() {
    echo ""
    echo -e "${BOLD}${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}${BLUE}$1${NC}"
    echo -e "${BOLD}${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

kcp_kubectl() {
    if [ -n "$KUBECONFIG_KCP" ]; then
        KUBECONFIG="$KUBECONFIG_KCP" kubectl "$@"
    elif [ -n "$KCP_SERVER" ]; then
        kubectl --server="$KCP_SERVER" "$@"
    else
        kubectl "$@"
    fi
}

###############################################################################
# Main functions
###############################################################################

list_apiexports_in_workspace() {
    local ws_path="$1"
    local indent="${2:-}"
    local show_details="${3:-false}"

    # Navigate to workspace
    # Handle special case for root workspace
    local ws_target="$ws_path"
    if [ "$ws_path" = ":root" ]; then
        ws_target=":"
    fi

    kcp_kubectl ws "$ws_target" &>/dev/null || {
        log_error "${indent}Cannot navigate to workspace $ws_path"
        return
    }

    # Get all APIExports in this workspace
    local apiexports_json
    apiexports_json=$(kcp_kubectl get apiexports -o json 2>/dev/null || echo '{"items":[]}')
    local export_count
    export_count=$(echo "$apiexports_json" | jq '.items | length')

    # Get all AuthorizationModels in this workspace
    local authmodels_json
    authmodels_json=$(kcp_kubectl get authorizationmodels -o json 2>/dev/null || echo '{"items":[]}')
    local authmodel_count
    authmodel_count=$(echo "$authmodels_json" | jq '.items | length')

    # Only print workspace header if there's content to show
    if [ "$export_count" -gt 0 ] || [ "$authmodel_count" -gt 0 ]; then
        echo -e "${indent}${BOLD}Workspace: ${CYAN}$ws_path${NC}"

        # Process APIExports
        if [ "$export_count" -gt 0 ]; then
            while IFS= read -r export_json; do
                local export_name
                export_name=$(echo "$export_json" | jq -r '.metadata.name')

                echo -e "${indent}  ${BOLD}${GREEN}APIExport:${NC} $export_name"

                # Get latest resource schemas
                local schemas
                schemas=$(echo "$export_json" | jq -r '.spec.latestResourceSchemas // [] | .[]')

                if [ -z "$schemas" ]; then
                    echo -e "${indent}    ${YELLOW}(no APIResourceSchemas)${NC}"
                else
                    while IFS= read -r schema; do
                        [ -z "$schema" ] && continue

                        if [ "$show_details" = "true" ]; then
                            # Get APIResourceSchema details
                            local schema_json
                            schema_json=$(kcp_kubectl get apiresourceschema "$schema" -o json 2>/dev/null || echo '{}')

                            if [ "$schema_json" != "{}" ]; then
                                local group version kind plural
                                group=$(echo "$schema_json" | jq -r '.spec.group // ""')
                                version=$(echo "$schema_json" | jq -r '.spec.versions[0].name // ""')
                                kind=$(echo "$schema_json" | jq -r '.spec.names.kind // ""')
                                plural=$(echo "$schema_json" | jq -r '.spec.names.plural // ""')

                                echo -e "${indent}    - ${BOLD}$schema${NC}"
                                echo -e "${indent}      Group: $group, Version: $version, Kind: $kind, Plural: $plural"
                            else
                                echo -e "${indent}    - $schema ${YELLOW}(not found)${NC}"
                            fi
                        else
                            echo -e "${indent}    - $schema"
                        fi
                    done <<< "$schemas"
                fi

                echo ""
            done < <(echo "$apiexports_json" | jq -c '.items[]')
        fi

        # Process AuthorizationModels
        if [ "$authmodel_count" -gt 0 ]; then
            echo -e "${indent}  ${BOLD}${GREEN}AuthorizationModels:${NC}"
            while IFS= read -r authmodel_json; do
                local authmodel_name
                authmodel_name=$(echo "$authmodel_json" | jq -r '.metadata.name')

                if [ "$show_details" = "true" ]; then
                    local store_id model_id type_count
                    store_id=$(echo "$authmodel_json" | jq -r '.spec.storeId // "unknown"')
                    model_id=$(echo "$authmodel_json" | jq -r '.spec.authorizationModelId // "unknown"')
                    type_count=$(echo "$authmodel_json" | jq -r '.spec.typeDefinitions | length // 0')

                    echo -e "${indent}    - ${BOLD}$authmodel_name${NC}"
                    echo -e "${indent}      StoreId: $store_id, ModelId: $model_id, Types: $type_count"
                else
                    echo -e "${indent}    - $authmodel_name"
                fi
            done < <(echo "$authmodels_json" | jq -c '.items[]')
            echo ""
        fi
    fi

    # Recurse into child workspaces
    local children
    children=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    for child in $children; do
        [ -z "$child" ] && continue
        # Build child workspace path
        local child_path
        if [ "$ws_path" = ":root" ] || [ "$ws_path" = "root" ]; then
            child_path=":root:${child}"
        else
            child_path="${ws_path}:${child}"
        fi
        list_apiexports_in_workspace "$child_path" "${indent}  " "$show_details"
    done
}

check_dependencies() {
    local missing_deps=()

    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi

    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi

    if [ ${#missing_deps[@]} -gt 0 ]; then
        log_error "Missing required dependencies: ${missing_deps[*]}"
        log_error "Please install them and try again."
        exit 1
    fi
}

show_help() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Lists all APIResourceSchemas and AuthorizationModels across all KCP workspaces.

This script recursively traverses workspaces and displays:
  - APIExports with their exported APIResourceSchemas
  - AuthorizationModel resources

Options:
  -d, --details    Show detailed information about each resource
                   - APIResourceSchemas: group, version, kind, plural
                   - AuthorizationModels: storeId, modelId, type count
  -h, --help       Show this help message

Environment Variables:
  KUBECONFIG_KCP   Path to KCP kubeconfig file
  KCP_SERVER       KCP server URL (alternative to KUBECONFIG_KCP)

Examples:
  # List all resources (names only)
  $0

  # List all resources with details
  $0 --details

  # Use specific kubeconfig
  KUBECONFIG_KCP=~/.kube/kcp-config $0
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -d|--details)
                SHOW_DETAILS=true
                shift
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
}

main() {
    parse_args "$@"

    log_header "KCP APIResourceSchemas & AuthorizationModels Inventory"
    echo ""

    check_dependencies

    log_info "Scanning all workspaces for APIExports and AuthorizationModels..."
    if [ "$SHOW_DETAILS" = "true" ]; then
        log_info "Detailed mode enabled"
    fi
    echo ""

    # Start from root workspace
    list_apiexports_in_workspace ":root" "" "$SHOW_DETAILS"

    # Return to root workspace
    kcp_kubectl ws : &>/dev/null || true

    echo ""
    log_info "Scan complete"
}

###############################################################################
# Entry point
###############################################################################

main "$@"
