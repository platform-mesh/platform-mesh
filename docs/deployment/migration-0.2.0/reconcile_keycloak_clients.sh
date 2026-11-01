#!/bin/bash
#
# Script to reconcile Keycloak clients for the 0.2.0 migration.
# For each user-created realm, deletes the realm-named client (matching
# the kcp workspace name under :root:orgs) and the 'kubectl' client,
# then restarts the security-operator pods so they re-create them with
# the updated configuration.
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Keycloak configuration (KEYCLOAK_URL derived from PlatformMesh if not set)
KEYCLOAK_URL="${KEYCLOAK_URL:-}"
KEYCLOAK_USER="${KEYCLOAK_USER:-keycloak-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-admin}"

# Built-in realms to skip
BUILTIN_REALMS=("master")

# Global variables
ADMIN_TOKEN=""
DRY_RUN=false
NAMESPACE="platform-mesh-system"
KUBECONFIG_K8S="${KUBECONFIG_K8S:-}"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Derive Keycloak URL from the PlatformMesh resource in the cluster
get_keycloak_url_from_cluster() {
    local base_domain port
    base_domain=$(kubectl -n platform-mesh-system get platformmesh platform-mesh \
        -o jsonpath='{.spec.exposure.baseDomain}' 2>/dev/null) || return 1
    port=$(kubectl -n platform-mesh-system get platformmesh platform-mesh \
        -o jsonpath='{.spec.exposure.port}' 2>/dev/null) || return 1

    if [ -z "$base_domain" ] || [ -z "$port" ]; then
        return 1
    fi

    echo "https://${base_domain}:${port}/keycloak"
}

# Check for required tools
check_dependencies() {
    local missing=()

    for tool in kubectl curl jq; do
        if ! command -v "$tool" &> /dev/null; then
            missing+=("$tool")
        fi
    done

    if [ ${#missing[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing[*]}"
        exit 1
    fi
}

# Get Keycloak admin token
get_admin_token() {
    curl -sk -X POST "${KEYCLOAK_URL}/realms/master/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "username=${KEYCLOAK_USER}" \
        -d "password=${KEYCLOAK_PASSWORD}" \
        -d "grant_type=password" \
        -d "client_id=admin-cli" | jq -r '.access_token'
}

# List all realms
get_realms() {
    local token="$1"

    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms" \
        -H "Authorization: Bearer ${token}" | jq -r '.[].realm'
}

# Get the internal UUID of a client by its display name in a realm.
# The security-operator creates clients with UUIDs as clientId and the
# human-readable identifier in the 'name' field, so we match on .name.
get_client_uuid() {
    local token="$1"
    local realm="$2"
    local client_name="$3"

    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms/${realm}/clients" \
        -H "Authorization: Bearer ${token}" \
        | jq -r --arg name "$client_name" '[.[] | select(.name == $name)] | .[0].id // empty'
}

# Delete a client by its internal UUID
delete_client() {
    local token="$1"
    local realm="$2"
    local client_uuid="$3"

    local http_code
    http_code=$(curl -sk -o /dev/null -w "%{http_code}" -X DELETE \
        "${KEYCLOAK_URL}/admin/realms/${realm}/clients/${client_uuid}" \
        -H "Authorization: Bearer ${token}")

    if [ "$http_code" = "204" ]; then
        return 0
    else
        log_error "  Delete returned HTTP $http_code"
        return 1
    fi
}

# Check if a realm is built-in
is_builtin_realm() {
    local realm="$1"
    for builtin in "${BUILTIN_REALMS[@]}"; do
        if [ "$realm" = "$builtin" ]; then
            return 0
        fi
    done
    return 1
}

# Restart security-operator pods
restart_security_operator() {
    log_info "Restarting security-operator pods..."

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Dry run - would delete pods:"
        log_warn "  kubectl -n $NAMESPACE delete pod -l app=security-operator"
        log_warn "  kubectl -n $NAMESPACE delete pod -l service=security-operator-generator"
        log_warn "  kubectl -n $NAMESPACE delete pod -l service=security-operator-initializer"
        return 0
    fi

    kubectl -n "$NAMESPACE" delete pod -l app=security-operator
    kubectl -n "$NAMESPACE" delete pod -l service=security-operator-generator
    kubectl -n "$NAMESPACE" delete pod -l service=security-operator-initializer

    log_info "Security-operator pods restarted"
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
            --keycloak-user)
                KEYCLOAK_USER="$2"
                shift 2
                ;;
            --keycloak-password)
                KEYCLOAK_PASSWORD="$2"
                shift 2
                ;;
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --kubeconfig)
                KUBECONFIG_K8S="$2"
                shift 2
                ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Delete the realm-named client and 'kubectl' client from each"
                echo "user-created realm in Keycloak, then restart security-operator"
                echo "pods to reconcile."
                echo ""
                echo "Options:"
                echo "  --dry-run              Show what would be done without making changes"
                echo "  --keycloak-user USER   Keycloak admin username (default: \$KEYCLOAK_USER or keycloak-admin)"
                echo "  --keycloak-password PW Keycloak admin password (default: \$KEYCLOAK_PASSWORD or admin)"
                echo "  --namespace NS         Kubernetes namespace (default: platform-mesh-system)"
                echo "  --kubeconfig PATH      Kubeconfig for the Kubernetes cluster (default: \$KUBECONFIG_K8S or \$KUBECONFIG)"
                echo "  -h, --help             Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  KEYCLOAK_URL           Keycloak base URL"
                echo "  KEYCLOAK_USER          Keycloak admin username"
                echo "  KEYCLOAK_PASSWORD      Keycloak admin password"
                echo "  KUBECONFIG_K8S         Kubeconfig for the Kubernetes cluster"
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done

    check_dependencies

    if [ -n "$KUBECONFIG_K8S" ]; then
        export KUBECONFIG="$KUBECONFIG_K8S"
        log_info "Using kubeconfig: $KUBECONFIG_K8S"
    fi

    if [ "$DRY_RUN" = "true" ]; then
        log_warn "Running in dry-run mode - no changes will be applied"
    fi

    # Derive Keycloak URL from PlatformMesh resource if not explicitly set
    if [ -z "$KEYCLOAK_URL" ]; then
        log_info "KEYCLOAK_URL not set, reading from PlatformMesh resource..."
        KEYCLOAK_URL=$(get_keycloak_url_from_cluster) || true
        if [ -z "$KEYCLOAK_URL" ]; then
            log_error "Failed to derive Keycloak URL from PlatformMesh resource."
            log_error "Set KEYCLOAK_URL explicitly."
            exit 1
        fi
        log_info "Derived Keycloak URL: $KEYCLOAK_URL"
    fi

    # Get Keycloak admin token
    log_info "Fetching Keycloak admin token from $KEYCLOAK_URL..."
    ADMIN_TOKEN=$(get_admin_token)
    if [ -z "$ADMIN_TOKEN" ] || [ "$ADMIN_TOKEN" = "null" ]; then
        log_error "Failed to get Keycloak admin token. Check your credentials."
        exit 1
    fi
    log_info "Successfully obtained Keycloak admin token"

    # List all realms
    log_info "Listing Keycloak realms..."
    local realms
    realms=$(get_realms "$ADMIN_TOKEN")

    if [ -z "$realms" ]; then
        log_warn "No realms found"
        exit 0
    fi

    local deleted_count=0
    local skipped_count=0

    # Process each realm
    while IFS= read -r realm; do
        if is_builtin_realm "$realm"; then
            log_info "Skipping built-in realm: $realm"
            continue
        fi

        log_info "Processing realm: $realm"

        # The realm-named client matches the kcp workspace name (e.g. realm
        # "myorg1" has a client called "myorg1"). 'kubectl' is always fixed.
        local clients_to_delete=("$realm" "kubectl")

        for client_name in "${clients_to_delete[@]}"; do
            local client_uuid
            client_uuid=$(get_client_uuid "$ADMIN_TOKEN" "$realm" "$client_name")

            if [ -z "$client_uuid" ]; then
                log_info "  Client '$client_name' not found in realm '$realm', skipping"
                ((skipped_count++))
                continue
            fi

            if [ "$DRY_RUN" = "true" ]; then
                log_warn "  Dry run - would delete client '$client_name' (id: $client_uuid) from realm '$realm'"
                ((deleted_count++))
            else
                log_info "  Deleting client '$client_name' (id: $client_uuid) from realm '$realm'..."
                if delete_client "$ADMIN_TOKEN" "$realm" "$client_uuid"; then
                    log_info "  Successfully deleted client '$client_name' from realm '$realm'"
                    ((deleted_count++))
                else
                    log_error "  Failed to delete client '$client_name' from realm '$realm'"
                fi
            fi
        done
    done <<< "$realms"

    echo ""
    log_info "Summary: Deleted $deleted_count clients, Skipped $skipped_count"
    echo ""

    # Restart security-operator pods
    restart_security_operator

    echo ""
    log_info "=== Keycloak client reconciliation complete ==="
}

main "$@"
