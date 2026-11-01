#!/bin/bash
#
# Script to update WorkspaceAuthenticationConfiguration resources
# Updates audiences with client IDs from Keycloak
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

# Global variables
ADMIN_TOKEN=""
DEBUG="${DEBUG:-false}"
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
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

# Derive Keycloak URL from the PlatformMesh resource in the regular k8s cluster.
# Uses KUBECONFIG_K8S when set so the lookup targets the host cluster, not kcp.
get_keycloak_url_from_cluster() {
    local kc_args=()
    if [ -n "$KUBECONFIG_K8S" ]; then
        kc_args+=(--kubeconfig "$KUBECONFIG_K8S")
    fi

    local base_domain port
    base_domain=$(kubectl "${kc_args[@]}" -n platform-mesh-system get platformmesh platform-mesh \
        -o jsonpath='{.spec.exposure.baseDomain}' 2>/dev/null) || return 1
    port=$(kubectl "${kc_args[@]}" -n platform-mesh-system get platformmesh platform-mesh \
        -o jsonpath='{.spec.exposure.port}' 2>/dev/null) || return 1

    if [ -z "$base_domain" ] || [ -z "$port" ]; then
        return 1
    fi

    echo "https://${base_domain}:${port}/keycloak"
}

# Check for required tools
check_dependencies() {
    local missing=()

    if ! command -v kubectl &> /dev/null; then
        missing+=("kubectl")
    fi

    if ! command -v yq &> /dev/null; then
        missing+=("yq")
    fi

    if ! command -v jq &> /dev/null; then
        missing+=("jq")
    fi

    if ! command -v curl &> /dev/null; then
        missing+=("curl")
    fi

    if [ ${#missing[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing[*]}"
        log_error "Please install them before running this script."
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

# Get all custom client IDs (UUIDs) from a realm
# Custom clients have clientId values that are UUIDs (not built-in clients like account, admin-cli, etc.)
get_custom_client_ids() {
    local token="$1"
    local realm="$2"

    local response
    response=$(curl -sk -X GET "${KEYCLOAK_URL}/admin/realms/${realm}/clients/" \
        -H "Authorization: Bearer ${token}")

    if [ "$DEBUG" = "true" ]; then
        echo "[DEBUG] Keycloak clients response for realm '$realm':" >&2
        echo "$response" | jq -r '.[].clientId' >&2
    fi

    # Filter clients whose clientId is a UUID pattern and return their clientId (which IS the audience value)
    echo "$response" | jq -r '.[] | select(.clientId | test("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")) | .clientId'
}

# Extract realm name from issuer URL
# e.g., https://portal.localhost:8443/keycloak/realms/default -> default
extract_realm_from_url() {
    local url="$1"
    echo "$url" | sed -E 's|.*/realms/([^/]+).*|\1|'
}

# Backup a resource before modifying
backup_resource() {
    local name="$1"
    local backup_dir="${BACKUP_DIR:-./wac-backups}"
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)

    mkdir -p "$backup_dir"

    local backup_file="${backup_dir}/${name}_${timestamp}.yaml"
    kubectl get workspaceauthenticationconfiguration "$name" -o yaml > "$backup_file"
    log_info "Backed up $name to $backup_file"
}

# Check if audiences contain old-style names (not UUIDs)
has_old_style_audiences() {
    local resource="$1"
    local audiences
    audiences=$(echo "$resource" | yq -r '.spec.jwt[0].issuer.audiences[]' 2>/dev/null || echo "")

    for audience in $audiences; do
        # Check if audience is NOT a UUID (UUIDs have format xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)
        if ! echo "$audience" | grep -qE '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'; then
            return 0
        fi
    done
    return 1
}

# Check if a resource needs updating
needs_update() {
    local name="$1"
    local resource
    resource=$(kubectl get workspaceauthenticationconfiguration "$name" -o yaml)

    # Check if audiences contain old-style names instead of UUIDs
    if has_old_style_audiences "$resource"; then
        return 0
    fi

    return 1
}

# Fix a single WorkspaceAuthenticationConfiguration resource
fix_resource() {
    local name="$1"
    local dry_run="${2:-false}"

    log_info "Processing WorkspaceAuthenticationConfiguration: $name"

    # Get the current resource
    local resource
    resource=$(kubectl get workspaceauthenticationconfiguration "$name" -o yaml)

    # Check if audiences need updating
    if ! has_old_style_audiences "$resource"; then
        log_info "Resource $name is already up to date, skipping."
        return 0
    fi

    # Create the updated resource
    local updated_resource="$resource"

    # Update audiences with client UUIDs from Keycloak
    log_info "  - Updating audiences with client UUIDs from Keycloak"

    # Extract realm from issuer URL
    local issuer_url
    issuer_url=$(echo "$resource" | yq -r '.spec.jwt[0].issuer.url')
    local realm
    realm=$(extract_realm_from_url "$issuer_url")
    log_info "    Realm: $realm"

    # Fetch custom client IDs from Keycloak
    # Custom clients have UUID-style clientId values (the clientId IS the audience)
    local client_ids
    client_ids=$(get_custom_client_ids "$ADMIN_TOKEN" "$realm")

    if [ -z "$client_ids" ]; then
        log_warn "    No custom clients found in Keycloak realm '$realm'"
    else
        log_info "    Found custom client IDs:"
        for cid in $client_ids; do
            log_info "      - $cid"
        done
    fi

    # Build new audiences array from client IDs
    local new_audiences="[]"
    for cid in $client_ids; do
        new_audiences=$(echo "$new_audiences" | jq --arg uuid "$cid" '. + [$uuid]')
    done

    if [ "$new_audiences" != "[]" ]; then
        # Convert YAML to JSON, update with jq, then back to YAML
        updated_resource=$(echo "$updated_resource" | yq -o=json | jq --argjson audiences "$new_audiences" '
            .spec.jwt[0].issuer.audiences = $audiences
        ' | yq -P)
    else
        log_warn "    No client UUIDs found, keeping existing audiences"
    fi

    # Remove resourceVersion and other server-managed fields for apply
    updated_resource=$(echo "$updated_resource" | yq '
        del(.metadata.resourceVersion) |
        del(.metadata.uid) |
        del(.metadata.creationTimestamp) |
        del(.metadata.generation) |
        del(.status)
    ')

    if [ "$dry_run" = "true" ]; then
        log_info "Dry run - would apply the following changes:"
        echo "$updated_resource"
        return 0
    fi

    # Backup before applying
    backup_resource "$name"

    # Apply the updated resource
    echo "$updated_resource" | kubectl apply -f -

    log_info "Successfully updated $name"
}

# Main function
main() {
    local dry_run=false
    local specific_name=""

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --debug)
                DEBUG=true
                shift
                ;;
            --dry-run)
                dry_run=true
                shift
                ;;
            --name)
                specific_name="$2"
                shift 2
                ;;
            --backup-dir)
                export BACKUP_DIR="$2"
                shift 2
                ;;
            --keycloak-user)
                KEYCLOAK_USER="$2"
                shift 2
                ;;
            --keycloak-password)
                KEYCLOAK_PASSWORD="$2"
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
                echo "Update WorkspaceAuthenticationConfiguration resources by:"
                echo "  - Updating audiences with client UUIDs from Keycloak"
                echo ""
                echo "Options:"
                echo "  --debug              Enable debug output"
                echo "  --dry-run            Show what would be changed without applying"
                echo "  --name NAME          Only process a specific resource"
                echo "  --backup-dir DIR     Directory for backups (default: ./wac-backups)"
                echo "  --keycloak-user USER Keycloak admin username (default: \$KEYCLOAK_USER or keycloak-admin)"
                echo "  --keycloak-password  Keycloak admin password (default: \$KEYCLOAK_PASSWORD or admin)"
                echo "  --kubeconfig PATH    Kubeconfig for the Kubernetes cluster (default: \$KUBECONFIG_K8S or \$KUBECONFIG)"
                echo "  --kubeconfig-kcp PATH Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)"
                echo "  -h, --help           Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  KEYCLOAK_URL         Keycloak base URL"
                echo "  KEYCLOAK_USER        Keycloak admin username"
                echo "  KEYCLOAK_PASSWORD    Keycloak admin password"
                echo "  KUBECONFIG_K8S       Kubeconfig for the Kubernetes cluster"
                echo "  KUBECONFIG_KCP       Kubeconfig for kcp"
                echo "  BACKUP_DIR           Directory for backups"
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
        log_info "Using k8s kubeconfig: $KUBECONFIG_K8S"
    fi

    if [ "$dry_run" = "true" ]; then
        log_warn "Running in dry-run mode - no changes will be applied"
    fi

    # Derive Keycloak URL from PlatformMesh resource if not explicitly set.
    # This must happen before switching to the kcp kubeconfig because
    # PlatformMesh lives in the regular Kubernetes cluster.
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

    # Switch to kcp kubeconfig for all subsequent kubectl commands
    if [ -n "$KUBECONFIG_KCP" ]; then
        export KUBECONFIG="$KUBECONFIG_KCP"
        log_info "Using kcp kubeconfig: $KUBECONFIG_KCP"
    fi

    # Get Keycloak admin token
    log_info "Fetching Keycloak admin token from $KEYCLOAK_URL..."
    ADMIN_TOKEN=$(get_admin_token)
    if [ -z "$ADMIN_TOKEN" ] || [ "$ADMIN_TOKEN" = "null" ]; then
        log_error "Failed to get Keycloak admin token. Check your credentials."
        exit 1
    fi
    log_info "Successfully obtained Keycloak admin token"

    if [ -n "$specific_name" ]; then
        # Process specific resource
        if needs_update "$specific_name"; then
            fix_resource "$specific_name" "$dry_run"
        else
            log_info "Resource $specific_name does not need updating"
        fi
    else
        # Process all WorkspaceAuthenticationConfiguration resources
        local resources
        resources=$(kubectl get workspaceauthenticationconfiguration -o jsonpath='{.items[*].metadata.name}')

        if [ -z "$resources" ]; then
            log_warn "No WorkspaceAuthenticationConfiguration resources found"
            exit 0
        fi

        local updated_count=0
        local skipped_count=0

        for name in $resources; do
            if needs_update "$name"; then
                fix_resource "$name" "$dry_run"
                ((updated_count++))
            else
                log_info "Resource $name is already up to date, skipping."
                ((skipped_count++))
            fi
        done

        echo ""
        log_info "Summary: Updated $updated_count, Skipped $skipped_count"
    fi
}

main "$@"
