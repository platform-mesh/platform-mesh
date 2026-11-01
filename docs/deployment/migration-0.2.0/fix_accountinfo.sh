#!/bin/bash
set -euo pipefail

# Script to patch AccountInfo objects for all workspaces under :root:orgs.
# Updates OIDC configuration, issuer URL, URL hostnames, and syncs client
# IDs from Keycloak so they match the currently configured clients.

# Keycloak configuration (KEYCLOAK_URL derived from PlatformMesh if not set)
KEYCLOAK_URL="${KEYCLOAK_URL:-}"
ISSUER_BASE_URL="${ISSUER_BASE_URL:-}"
KEYCLOAK_USER="${KEYCLOAK_USER:-keycloak-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-admin}"
OLD_HOST="${OLD_HOST:-kcp.api.portal.dev.local}"
NEW_HOST="${NEW_HOST:-localhost}"

# Global variables
ADMIN_TOKEN=""
DRY_RUN=false
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
KUBECONFIG_K8S="${KUBECONFIG_K8S:-}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

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

# Get the clientId (UUID) for a client by its display name in a realm.
# The security-operator creates clients with UUIDs as clientId and the
# human-readable identifier in the 'name' field, so we match on .name.
get_client_id() {
    local token="$1"
    local realm="$2"
    local client_name="$3"

    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms/${realm}/clients" \
        -H "Authorization: Bearer ${token}" \
        | jq -r --arg name "$client_name" '[.[] | select(.name == $name)] | .[0].clientId // empty'
}

# Recursive function to process workspaces and patch AccountInfo
# Arguments: $1 = workspace path (e.g., :root:orgs:default)
process_workspace() {
    local ws_path="$1"
    local indent="$2"

    log_info "${indent}Processing workspace: $ws_path"

    # Navigate to the workspace
    kubectl ws "$ws_path"

    # Extract realm name from the workspace path (last segment after :root:orgs:)
    # e.g., :root:orgs:default -> default, :root:orgs:default:testaccount -> testaccount
    local realm_name
    realm_name=$(echo "$ws_path" | sed 's/.*:orgs://' | tr ':' '-')
    # For nested paths like default:testaccount, use the full path with dashes
    # But for Keycloak realm lookup, we need to determine the actual realm
    local keycloak_realm
    keycloak_realm=$(echo "$ws_path" | sed 's/.*:orgs://' | sed 's/:.*$//')

    # Check if AccountInfo named "account" exists in this workspace
    if kubectl get accountinfo account &>/dev/null; then
        log_info "${indent}  Found AccountInfo 'account', patching..."

        # Get current URLs and replace hostname
        local account_url org_url parent_url
        account_url=$(kubectl get accountinfo account -o jsonpath='{.spec.account.url}' 2>/dev/null || echo "")
        org_url=$(kubectl get accountinfo account -o jsonpath='{.spec.organization.url}' 2>/dev/null || echo "")
        parent_url=$(kubectl get accountinfo account -o jsonpath='{.spec.parentAccount.url}' 2>/dev/null || echo "")

        log_info "${indent}  Current account.url: '$account_url'"
        log_info "${indent}  Current organization.url: '$org_url'"
        log_info "${indent}  Current parentAccount.url: '$parent_url'"

        # Patch each URL field individually using JSON Patch for precise updates
        # This avoids issues with merge patch and empty values

        if [[ -n "$account_url" && "$account_url" == *"$OLD_HOST"* ]]; then
            local new_account_url
            new_account_url="${account_url//$OLD_HOST/$NEW_HOST}"
            if [ "$DRY_RUN" = "true" ]; then
                log_warn "${indent}  Dry run - would patch account.url: $account_url -> $new_account_url"
            else
                log_info "${indent}  Patching account.url: $account_url -> $new_account_url"
                kubectl patch accountinfo account --type=json \
                    -p "[{\"op\": \"replace\", \"path\": \"/spec/account/url\", \"value\": \"${new_account_url}\"}]"
            fi
        fi

        if [[ -n "$org_url" && "$org_url" == *"$OLD_HOST"* ]]; then
            local new_org_url
            new_org_url="${org_url//$OLD_HOST/$NEW_HOST}"
            if [ "$DRY_RUN" = "true" ]; then
                log_warn "${indent}  Dry run - would patch organization.url: $org_url -> $new_org_url"
            else
                log_info "${indent}  Patching organization.url: $org_url -> $new_org_url"
                kubectl patch accountinfo account --type=json \
                    -p "[{\"op\": \"replace\", \"path\": \"/spec/organization/url\", \"value\": \"${new_org_url}\"}]"
            fi
        fi

        if [[ -n "$parent_url" && "$parent_url" == *"$OLD_HOST"* ]]; then
            local new_parent_url
            new_parent_url="${parent_url//$OLD_HOST/$NEW_HOST}"
            if [ "$DRY_RUN" = "true" ]; then
                log_warn "${indent}  Dry run - would patch parentAccount.url: $parent_url -> $new_parent_url"
            else
                log_info "${indent}  Patching parentAccount.url: $parent_url -> $new_parent_url"
                kubectl patch accountinfo account --type=json \
                    -p "[{\"op\": \"replace\", \"path\": \"/spec/parentAccount/url\", \"value\": \"${new_parent_url}\"}]"
            fi
        fi

        # Fetch client IDs from Keycloak using the top-level realm.
        # The realm-named client matches the workspace name (e.g. realm
        # "myorg1" has a client named "myorg1"). 'kubectl' is always fixed.
        log_info "${indent}  Fetching client IDs from Keycloak for realm: $keycloak_realm"
        local default_client_id kubectl_client_id
        default_client_id=$(get_client_id "$ADMIN_TOKEN" "$keycloak_realm" "$keycloak_realm")
        kubectl_client_id=$(get_client_id "$ADMIN_TOKEN" "$keycloak_realm" "kubectl")

        if [[ -z "$default_client_id" ]]; then
            log_warn "${indent}  Client '$keycloak_realm' not found in Keycloak realm $keycloak_realm"
        else
            log_info "${indent}  Found '$keycloak_realm' client ID: $default_client_id"
        fi

        if [[ -z "$kubectl_client_id" ]]; then
            log_warn "${indent}  Client 'kubectl' not found in Keycloak realm $keycloak_realm"
        else
            log_info "${indent}  Found 'kubectl' client ID: $kubectl_client_id"
        fi

        # Build OIDC patch with issuerUrl and any resolved client IDs
        local oidc_patch='{"spec":{"oidc":{"issuerUrl":"'"${ISSUER_BASE_URL}/${keycloak_realm}"'"'

        if [[ -n "$default_client_id" && -n "$kubectl_client_id" ]]; then
            oidc_patch+=',"clients":{"default":{"clientId":"'"${default_client_id}"'"},"kubectl":{"clientId":"'"${kubectl_client_id}"'"}}'
        elif [[ -n "$default_client_id" ]]; then
            oidc_patch+=',"clients":{"default":{"clientId":"'"${default_client_id}"'"}}'
        elif [[ -n "$kubectl_client_id" ]]; then
            oidc_patch+=',"clients":{"kubectl":{"clientId":"'"${kubectl_client_id}"'"}}'
        fi

        oidc_patch+='}}}'

        if [ "$DRY_RUN" = "true" ]; then
            log_warn "${indent}  Dry run - would patch OIDC configuration:"
            echo "$oidc_patch" | jq .
        else
            log_info "${indent}  Patching OIDC configuration and client IDs..."
            kubectl patch accountinfo account --type=merge -p "$oidc_patch"
            log_info "${indent}  Successfully patched AccountInfo"
        fi
    else
        log_info "${indent}  No AccountInfo 'account' found, skipping patch..."
    fi

    # Get child workspaces and process them recursively
    local children
    children=$(kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [[ -n "$children" ]]; then
        log_info "${indent}  Found child workspaces: $children"
        for child in $children; do
            process_workspace "${ws_path}:${child}" "${indent}  "
        done
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
            --keycloak-user)
                KEYCLOAK_USER="$2"
                shift 2
                ;;
            --keycloak-password)
                KEYCLOAK_PASSWORD="$2"
                shift 2
                ;;
            --issuer-base-url)
                ISSUER_BASE_URL="$2"
                shift 2
                ;;
            --old-host)
                OLD_HOST="$2"
                shift 2
                ;;
            --new-host)
                NEW_HOST="$2"
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
                echo "Patch AccountInfo objects for all workspaces under :root:orgs."
                echo "Updates OIDC configuration, issuer URL, URL hostnames, and syncs"
                echo "client IDs from Keycloak."
                echo ""
                echo "Options:"
                echo "  --dry-run                Show what would be done without making changes"
                echo "  --keycloak-user USER     Keycloak admin username (default: \$KEYCLOAK_USER or keycloak-admin)"
                echo "  --keycloak-password PW   Keycloak admin password (default: \$KEYCLOAK_PASSWORD or admin)"
                echo "  --issuer-base-url URL    OIDC issuer base URL (default: \$ISSUER_BASE_URL or \$KEYCLOAK_URL/realms)"
                echo "  --old-host HOST          Hostname to replace in URLs (default: \$OLD_HOST or kcp.api.portal.dev.local)"
                echo "  --new-host HOST          Replacement hostname (default: \$NEW_HOST or localhost)"
                echo "  --kubeconfig PATH        Kubeconfig for the Kubernetes cluster (default: \$KUBECONFIG_K8S or \$KUBECONFIG)"
                echo "  --kubeconfig-kcp PATH    Kubeconfig for kcp (default: \$KUBECONFIG_KCP or \$KUBECONFIG)"
                echo "  -h, --help               Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  KEYCLOAK_URL             Keycloak base URL"
                echo "  KEYCLOAK_USER            Keycloak admin username"
                echo "  KEYCLOAK_PASSWORD        Keycloak admin password"
                echo "  ISSUER_BASE_URL          OIDC issuer base URL"
                echo "  OLD_HOST                 Hostname to replace"
                echo "  NEW_HOST                 Replacement hostname"
                echo "  KUBECONFIG_K8S           Kubeconfig for the Kubernetes cluster"
                echo "  KUBECONFIG_KCP           Kubeconfig for kcp"
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

    if [ "$DRY_RUN" = "true" ]; then
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

    # Derive ISSUER_BASE_URL from KEYCLOAK_URL if not explicitly set
    if [ -z "$ISSUER_BASE_URL" ]; then
        ISSUER_BASE_URL="${KEYCLOAK_URL}/realms"
        log_info "Derived ISSUER_BASE_URL: $ISSUER_BASE_URL"
    fi

    # Get Keycloak admin token
    log_info "Fetching Keycloak admin token from $KEYCLOAK_URL..."
    ADMIN_TOKEN=$(get_admin_token)
    if [[ -z "$ADMIN_TOKEN" || "$ADMIN_TOKEN" == "null" ]]; then
        log_error "Failed to get Keycloak admin token. Check your credentials."
        exit 1
    fi
    log_info "Successfully obtained admin token"

    # Start recursive processing from :root:orgs
    log_info "=== Starting recursive AccountInfo patching from :root:orgs ==="

    # First, get all top-level orgs
    kubectl ws :root:orgs
    TOP_LEVEL_ORGS=$(kubectl get workspaces -o jsonpath='{.items[*].metadata.name}')

    if [[ -z "$TOP_LEVEL_ORGS" ]]; then
        log_warn "No workspaces found under :root:orgs"
        exit 0
    fi

    log_info "Found top-level orgs: $TOP_LEVEL_ORGS"

    for org in $TOP_LEVEL_ORGS; do
        process_workspace ":root:orgs:${org}" ""
    done

    # Return to root workspace
    kubectl ws :root

    log_info "=== Done patching all AccountInfo objects recursively (OIDC and URL) ==="
}

main "$@"
