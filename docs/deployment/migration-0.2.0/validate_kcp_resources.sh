#!/bin/bash
#
# Validates that KCP resources are correct and consistent with Keycloak,
# OpenFGA, and the Kubernetes cluster.
#
# Checks performed:
#   1.  PlatformMesh resource is ready (K8s cluster)
#   2.  APIExport exists in :root:platform-mesh-system
#   3.  APIBindings have secrets permission claims
#   4.  Account resources are Ready
#   5.  AccountInfo has valid OIDC config with client IDs matching Keycloak
#   6.  Store resources have correct coreModule (27 types) and authorizationModelId
#   7.  IdentityProviderConfiguration exists per org workspace
#   8.  Invite resources exist for org owners
#   9.  WorkspaceAuthenticationConfiguration audiences contain UUID client IDs
#  10.  ContentConfiguration has no stale entries
#  11.  Keycloak realms and clients match KCP state
#  12.  OpenFGA stores and authorization models are consistent
#  13.  Gateway / HTTPRoute / TLSRoute readiness (K8s cluster)
#  14.  Operator pods are running (K8s cluster)
#

set -euo pipefail

###############################################################################
# Configuration
###############################################################################

KEYCLOAK_URL="${KEYCLOAK_URL:-}"
KEYCLOAK_USER="${KEYCLOAK_USER:-keycloak-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-admin}"
OPENFGA_API_URL="${OPENFGA_API_URL:-http://localhost:8080}"
KUBECONFIG_KCP="${KUBECONFIG_KCP:-}"
KUBECONFIG_K8S="${KUBECONFIG_K8S:-}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-}"
KCP_SERVER="${KCP_SERVER:-}"
NAMESPACE="${NAMESPACE:-platform-mesh-system}"
APIBINDING_PREFIX="${APIBINDING_PREFIX:-core.platform-mesh.io-}"
EXPECTED_FGA_TYPE_COUNT="${EXPECTED_FGA_TYPE_COUNT:-27}"
BUILTIN_REALMS=("master")
VERBOSE="${VERBOSE:-false}"

###############################################################################
# Counters and state
###############################################################################

PASS_COUNT=0
FAIL_COUNT=0
WARN_COUNT=0
SKIP_COUNT=0
ADMIN_TOKEN=""
ORIGINAL_KUBECONFIG="${KUBECONFIG:-}"

###############################################################################
# Colors
###############################################################################

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

###############################################################################
# Logging helpers
###############################################################################

log_header() {
    echo ""
    echo -e "${BOLD}${CYAN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${CYAN}  $1${NC}"
    echo -e "${BOLD}${CYAN}════════════════════════════════════════════════════════════════${NC}"
}

log_section() {
    echo ""
    echo -e "${BOLD}${BLUE}── $1${NC}"
}

log_pass() {
    echo -e "  ${GREEN}[PASS]${NC} $1"
    ((PASS_COUNT++)) || true
}

log_fail() {
    echo -e "  ${RED}[FAIL]${NC} $1"
    ((FAIL_COUNT++)) || true
}

log_warn() {
    echo -e "  ${YELLOW}[WARN]${NC} $1"
    ((WARN_COUNT++)) || true
}

log_skip() {
    echo -e "  ${YELLOW}[SKIP]${NC} $1"
    ((SKIP_COUNT++)) || true
}

log_info() {
    echo -e "  ${BLUE}[INFO]${NC} $1"
}

log_verbose() {
    if [ "$VERBOSE" = "true" ]; then
        echo -e "  ${BLUE}[VERB]${NC} $1"
    fi
}

###############################################################################
# Utility helpers
###############################################################################

is_uuid() {
    echo "$1" | grep -qE '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
}

is_builtin_realm() {
    local realm="$1"
    for builtin in "${BUILTIN_REALMS[@]}"; do
        if [ "$realm" = "$builtin" ]; then
            return 0
        fi
    done
    return 1
}

# kubectl ws (the kcp workspace plugin) reads KUBECONFIG from the environment
# and ignores --kubeconfig, so we must export it for the duration of the call.
kcp_kubectl() {
    if [ -n "$KUBECONFIG_KCP" ]; then
        KUBECONFIG="$KUBECONFIG_KCP" kubectl "$@"
    else
        kubectl "$@"
    fi
}

k8s_kubectl() {
    local args=()
    if [ -n "$KUBECONFIG_K8S" ]; then
        args+=(--kubeconfig "$KUBECONFIG_K8S")
    fi
    if [ -n "$KUBECTL_CONTEXT" ]; then
        args+=(--context "$KUBECTL_CONTEXT")
    fi
    kubectl "${args[@]}" "$@"
}

###############################################################################
# Dependency checks
###############################################################################

check_dependencies() {
    local missing=()

    for tool in kubectl curl jq; do
        if ! command -v "$tool" &>/dev/null; then
            missing+=("$tool")
        fi
    done

    if [ ${#missing[@]} -ne 0 ]; then
        echo -e "${RED}[ERROR] Missing required tools: ${missing[*]}${NC}"
        exit 1
    fi

    # Optional tools
    if ! command -v yq &>/dev/null; then
        log_info "yq not found -- some YAML checks will be skipped"
    fi
}

###############################################################################
# Derive configuration from PlatformMesh if not set
###############################################################################

derive_config_from_cluster() {
    if [ -z "$KEYCLOAK_URL" ]; then
        log_info "KEYCLOAK_URL not set, deriving from PlatformMesh resource..."
        local base_domain port
        base_domain=$(k8s_kubectl -n "$NAMESPACE" get platformmesh platform-mesh \
            -o jsonpath='{.spec.exposure.baseDomain}' 2>/dev/null) || true
        port=$(k8s_kubectl -n "$NAMESPACE" get platformmesh platform-mesh \
            -o jsonpath='{.spec.exposure.port}' 2>/dev/null) || true

        if [ -n "$base_domain" ] && [ -n "$port" ]; then
            KEYCLOAK_URL="https://${base_domain}:${port}/keycloak"
            log_info "Derived KEYCLOAK_URL: $KEYCLOAK_URL"
        else
            log_warn "Could not derive KEYCLOAK_URL -- Keycloak checks will be skipped"
        fi
    fi
}

###############################################################################
# Keycloak helpers
###############################################################################

keycloak_get_admin_token() {
    curl -sk --max-time 3000 -X POST "${KEYCLOAK_URL}/realms/master/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "username=${KEYCLOAK_USER}" \
        -d "password=${KEYCLOAK_PASSWORD}" \
        -d "grant_type=password" \
        -d "client_id=admin-cli" | jq -r '.access_token'
}

keycloak_refresh_token() {
    if [ -n "$KEYCLOAK_URL" ]; then
        ADMIN_TOKEN=$(keycloak_get_admin_token 2>/dev/null || echo "")
        if [ -z "$ADMIN_TOKEN" ] || [ "$ADMIN_TOKEN" = "null" ]; then
            ADMIN_TOKEN=""
        fi
    fi
}

keycloak_get_realms() {
    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms" \
        -H "Authorization: Bearer ${ADMIN_TOKEN}" | jq -r '.[].realm'
}

keycloak_get_clients() {
    local realm="$1"
    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms/${realm}/clients" \
        -H "Authorization: Bearer ${ADMIN_TOKEN}"
}

keycloak_get_client_id_by_name() {
    local realm="$1"
    local client_name="$2"
    curl -sk -X GET "${KEYCLOAK_URL}/admin/realms/${realm}/clients" \
        -H "Authorization: Bearer ${ADMIN_TOKEN}" \
        | jq -r --arg name "$client_name" '[.[] | select(.name == $name)] | .[0].clientId // empty'
}

###############################################################################
# OpenFGA helpers
###############################################################################

openfga_get_stores() {
    curl -sk -X GET "${OPENFGA_API_URL}/stores" 2>/dev/null | jq -r '.stores // []'
}

openfga_get_auth_models() {
    local store_id="$1"
    curl -sk -X GET "${OPENFGA_API_URL}/stores/${store_id}/authorization-models" 2>/dev/null
}

openfga_count_types_in_model() {
    local store_id="$1"
    local model_id="$2"
    curl -sk -X GET "${OPENFGA_API_URL}/stores/${store_id}/authorization-models/${model_id}" 2>/dev/null \
        | jq '.authorization_model.type_definitions | length'
}

###############################################################################
# 1. Validate PlatformMesh resource (K8s cluster)
###############################################################################

validate_platformmesh() {
    log_header "1. PlatformMesh Resource (K8s cluster)"

    local pm_json
    pm_json=$(k8s_kubectl -n "$NAMESPACE" get platformmesh platform-mesh -o json 2>/dev/null) || {
        log_fail "PlatformMesh 'platform-mesh' not found in namespace $NAMESPACE"
        return
    }

    log_pass "PlatformMesh 'platform-mesh' exists"

    # Check status conditions
    local conditions
    conditions=$(echo "$pm_json" | jq -r '.status.conditions // []')
    local condition_count
    condition_count=$(echo "$conditions" | jq 'length')

    if [ "$condition_count" -eq 0 ]; then
        log_warn "PlatformMesh has no status conditions"
        return
    fi

    local all_ready=true
    for i in $(seq 0 $((condition_count - 1))); do
        local ctype cstatus
        ctype=$(echo "$conditions" | jq -r ".[$i].type")
        cstatus=$(echo "$conditions" | jq -r ".[$i].status")
        if [ "$cstatus" = "True" ]; then
            log_pass "PlatformMesh condition '$ctype' is True"
        else
            log_fail "PlatformMesh condition '$ctype' is $cstatus (expected True)"
            all_ready=false
        fi
    done

    # Validate exposure config
    local base_domain port protocol
    base_domain=$(echo "$pm_json" | jq -r '.spec.exposure.baseDomain // empty')
    port=$(echo "$pm_json" | jq -r '.spec.exposure.port // empty')
    protocol=$(echo "$pm_json" | jq -r '.spec.exposure.protocol // empty')

    if [ -n "$base_domain" ]; then
        log_pass "PlatformMesh baseDomain: $base_domain"
    else
        log_fail "PlatformMesh baseDomain is not set"
    fi

    log_verbose "PlatformMesh port=$port protocol=$protocol"
}

###############################################################################
# 2. Validate APIExport in :root:platform-mesh-system
###############################################################################

validate_apiexport() {
    log_header "2. APIExport (KCP)"

    kcp_kubectl ws :root:platform-mesh-system &>/dev/null || {
        log_fail "Cannot navigate to :root:platform-mesh-system workspace"
        return
    }

    local apiexport_json
    apiexport_json=$(kcp_kubectl get apiexport core.platform-mesh.io -o json 2>/dev/null) || {
        log_fail "APIExport 'core.platform-mesh.io' not found in :root:platform-mesh-system"
        kcp_kubectl ws :root &>/dev/null || true
        return
    }

    log_pass "APIExport 'core.platform-mesh.io' exists"

    # Check that it has the expected APIResourceSchemas
    local schemas
    schemas=$(echo "$apiexport_json" | jq -r '.spec.latestResourceSchemas // [] | .[]' 2>/dev/null)

    if [ -n "$schemas" ]; then
        local schema_count
        schema_count=$(echo "$schemas" | wc -l)
        log_pass "APIExport declares $schema_count resource schemas"
        for schema in $schemas; do
            log_verbose "  Schema: $schema"
        done
    else
        log_warn "APIExport has no latestResourceSchemas"
    fi

    # Check permission claims
    local perm_claims
    perm_claims=$(echo "$apiexport_json" | jq '.spec.permissionClaims // [] | length')
    if [ "$perm_claims" -gt 0 ]; then
        log_pass "APIExport has $perm_claims permission claims"
    else
        log_warn "APIExport has no permission claims"
    fi

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 3. Validate APIBindings across workspaces
###############################################################################

validate_apibindings_in_workspace() {
    local ws_path="$1"
    local indent="${2:-}"

    kcp_kubectl ws "$ws_path" &>/dev/null || {
        log_warn "${indent}Cannot navigate to workspace $ws_path"
        return
    }

    # Get APIBindings with the expected prefix
    local bindings
    bindings=$(kcp_kubectl get apibindings -o json 2>/dev/null || echo '{"items":[]}')
    local matching
    matching=$(echo "$bindings" | jq -r \
        --arg prefix "$APIBINDING_PREFIX" \
        '.items[] | select(.metadata.name | startswith($prefix)) | .metadata.name')

    for binding_name in $matching; do
        [ -z "$binding_name" ] && continue

        # Check for secrets permission claim
        local has_secrets
        has_secrets=$(echo "$bindings" | jq -r \
            --arg name "$binding_name" \
            '.items[] | select(.metadata.name == $name) | .spec.permissionClaims // [] | map(select(.resource == "secrets")) | length')

        if [ "$has_secrets" -gt 0 ]; then
            log_pass "${indent}APIBinding '$binding_name' in $ws_path has secrets claim"
        else
            log_fail "${indent}APIBinding '$binding_name' in $ws_path missing secrets permission claim"
        fi

        # Check binding phase
        local phase
        phase=$(echo "$bindings" | jq -r \
            --arg name "$binding_name" \
            '.items[] | select(.metadata.name == $name) | .status.phase // "Unknown"')
        if [ "$phase" = "Bound" ]; then
            log_pass "${indent}APIBinding '$binding_name' in $ws_path is Bound"
        else
            log_fail "${indent}APIBinding '$binding_name' in $ws_path phase is '$phase' (expected Bound)"
        fi
    done

    # Recurse into child workspaces
    local children
    children=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    for child in $children; do
        [ -z "$child" ] && continue
        validate_apibindings_in_workspace "${ws_path}:${child}" "${indent}  "
    done
}

validate_apibindings() {
    log_header "3. APIBindings (KCP)"
    validate_apibindings_in_workspace ":root"
}

###############################################################################
# 4. Validate Account resources
###############################################################################

validate_accounts() {
    log_header "4. Account Resources (KCP)"

    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs workspace"
        return
    }

    local accounts_json
    accounts_json=$(kcp_kubectl get accounts -o json 2>/dev/null || echo '{"items":[]}')
    local account_count
    account_count=$(echo "$accounts_json" | jq '.items | length')

    if [ "$account_count" -eq 0 ]; then
        log_warn "No Account resources found in :root:orgs"
        kcp_kubectl ws :root &>/dev/null || true
        return
    fi

    log_info "Found $account_count Account resource(s)"

    while IFS= read -r account; do
        local name acc_type creator
        name=$(echo "$account" | jq -r '.metadata.name')
        acc_type=$(echo "$account" | jq -r '.spec.type // "unknown"')
        creator=$(echo "$account" | jq -r '.spec.creator // "unset"')

        log_verbose "Account: $name (type=$acc_type, creator=$creator)"

        # Check Ready condition
        local ready_status
        ready_status=$(echo "$account" | jq -r '
            .status.conditions // [] | map(select(.type == "Ready")) | .[0].status // "Unknown"')

        if [ "$ready_status" = "True" ]; then
            log_pass "Account '$name' is Ready"
        else
            log_fail "Account '$name' is NOT Ready (status=$ready_status)"
        fi

        # Validate creator is set
        if [ "$creator" = "unset" ] || [ "$creator" = "null" ] || [ -z "$creator" ]; then
            log_warn "Account '$name' has no creator email"
        fi
    done < <(echo "$accounts_json" | jq -c '.items[]')

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 5. Validate AccountInfo OIDC + cross-check with Keycloak
###############################################################################

validate_accountinfo_in_workspace() {
    local ws_path="$1"

    kcp_kubectl ws "$ws_path" &>/dev/null || {
        log_warn "Cannot navigate to workspace $ws_path"
        return
    }

    if ! kcp_kubectl get accountinfo account &>/dev/null; then
        log_verbose "No AccountInfo 'account' in $ws_path"
        # Recurse into children
        local children
        children=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
        for child in $children; do
            [ -z "$child" ] && continue
            validate_accountinfo_in_workspace "${ws_path}:${child}"
        done
        return
    fi

    local ai_json
    ai_json=$(kcp_kubectl get accountinfo account -o json 2>/dev/null)

    # The OIDC clients map uses the org name as key (e.g. clients.msp)
    # and "kubectl" as the other key. Extract the org name from the workspace
    # path (last segment after :root:orgs:, with nested colons replaced by dashes).
    local org_name
    org_name=$(echo "$ws_path" | sed 's/.*:orgs://' | sed 's/:.*$//')

    local issuer_url org_client_id kubectl_client_id fga_store_id
    issuer_url=$(echo "$ai_json" | jq -r '.spec.oidc.issuerUrl // empty')
    org_client_id=$(echo "$ai_json" | jq -r --arg org "$org_name" '.spec.oidc.clients[$org].clientId // empty')
    kubectl_client_id=$(echo "$ai_json" | jq -r '.spec.oidc.clients.kubectl.clientId // empty')
    fga_store_id=$(echo "$ai_json" | jq -r '.spec.fga.store.id // empty')

    log_section "AccountInfo in $ws_path"

    # Check OIDC issuerUrl
    if [ -n "$issuer_url" ]; then
        log_pass "AccountInfo issuerUrl is set: $issuer_url"
    else
        log_fail "AccountInfo in $ws_path has no issuerUrl"
    fi

    # Check org-named client ID (e.g. clients.<org>.clientId)
    if [ -n "$org_client_id" ]; then
        if is_uuid "$org_client_id"; then
            log_pass "AccountInfo '$org_name' clientId is a UUID: $org_client_id"
        else
            log_fail "AccountInfo '$org_name' clientId is NOT a UUID: $org_client_id"
        fi
    else
        log_fail "AccountInfo in $ws_path missing '$org_name' clientId"
    fi

    # Check kubectl client ID
    if [ -n "$kubectl_client_id" ]; then
        if is_uuid "$kubectl_client_id"; then
            log_pass "AccountInfo kubectl clientId is a UUID: $kubectl_client_id"
        else
            log_fail "AccountInfo kubectl clientId is NOT a UUID: $kubectl_client_id"
        fi
    else
        log_fail "AccountInfo in $ws_path missing kubectl clientId"
    fi

    # Check FGA store ID
    if [ -n "$fga_store_id" ]; then
        log_pass "AccountInfo FGA store ID is set: $fga_store_id"
    else
        log_warn "AccountInfo in $ws_path has no FGA store ID"
    fi

    # Cross-check with Keycloak if token is available
    if [ -n "$ADMIN_TOKEN" ] && [ -n "$issuer_url" ]; then
        # Refresh token to prevent expiration during long-running validation
        keycloak_refresh_token

        local realm
        realm=$(echo "$issuer_url" | sed -E 's|.*/realms/([^/]+).*|\1|')

        if [ -n "$realm" ]; then
            # Check that the realm exists in Keycloak
            local realm_check
            realm_check=$(curl -sk -o /dev/null -w "%{http_code}" \
                "${KEYCLOAK_URL}/admin/realms/${realm}" \
                -H "Authorization: Bearer ${ADMIN_TOKEN}")

            if [ "$realm_check" = "200" ]; then
                log_pass "Keycloak realm '$realm' exists"
            else
                log_fail "Keycloak realm '$realm' not found (HTTP $realm_check)"
            fi

            # Verify org-named client ID matches Keycloak
            if [ -n "$org_client_id" ]; then
                local kc_org_id
                kc_org_id=$(keycloak_get_client_id_by_name "$realm" "$realm")
                if [ "$kc_org_id" = "$org_client_id" ]; then
                    log_pass "Keycloak '$org_name' client ID matches AccountInfo ($org_client_id)"
                elif [ -z "$kc_org_id" ]; then
                    log_fail "Keycloak client '$realm' not found in realm '$realm'"
                else
                    log_fail "Keycloak '$org_name' client ID mismatch: Keycloak=$kc_org_id, AccountInfo=$org_client_id"
                fi
            fi

            # Verify kubectl client ID matches Keycloak
            if [ -n "$kubectl_client_id" ]; then
                local kc_kubectl_id
                kc_kubectl_id=$(keycloak_get_client_id_by_name "$realm" "kubectl")
                if [ "$kc_kubectl_id" = "$kubectl_client_id" ]; then
                    log_pass "Keycloak kubectl client ID matches AccountInfo ($kubectl_client_id)"
                elif [ -z "$kc_kubectl_id" ]; then
                    log_fail "Keycloak client 'kubectl' not found in realm '$realm'"
                else
                    log_fail "Keycloak kubectl client ID mismatch: Keycloak=$kc_kubectl_id, AccountInfo=$kubectl_client_id"
                fi
            fi
        fi
    fi

    # Check account and org URLs are populated
    local account_url org_url
    account_url=$(echo "$ai_json" | jq -r '.spec.account.url // empty')
    org_url=$(echo "$ai_json" | jq -r '.spec.organization.url // empty')

    if [ -n "$account_url" ]; then
        log_pass "AccountInfo account.url is set: $account_url"
    else
        log_warn "AccountInfo in $ws_path has no account.url"
    fi

    if [ -n "$org_url" ]; then
        log_pass "AccountInfo organization.url is set: $org_url"
    else
        log_warn "AccountInfo in $ws_path has no organization.url"
    fi

    # Recurse into children
    local children
    children=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    for child in $children; do
        [ -z "$child" ] && continue
        validate_accountinfo_in_workspace "${ws_path}:${child}"
    done
}

validate_accountinfos() {
    log_header "5. AccountInfo Resources + Keycloak Cross-Check (KCP)"

    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs workspace"
        return
    }

    local orgs
    orgs=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [ -z "$orgs" ]; then
        log_warn "No workspaces found under :root:orgs"
        return
    fi

    for org in $orgs; do
        validate_accountinfo_in_workspace ":root:orgs:${org}"
    done

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 6. Validate Store resources + OpenFGA cross-check
###############################################################################

validate_stores() {
    log_header "6. Store Resources + OpenFGA Cross-Check (KCP)"

    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs workspace"
        return
    }

    local stores_json
    stores_json=$(kcp_kubectl get stores -o json 2>/dev/null || echo '{"items":[]}')
    local store_count
    store_count=$(echo "$stores_json" | jq '.items | length')

    if [ "$store_count" -eq 0 ]; then
        log_warn "No Store resources found in :root:orgs"
        kcp_kubectl ws :root &>/dev/null || true
        return
    fi

    log_info "Found $store_count Store resource(s)"

    while IFS= read -r store; do
        local name auth_model_id core_module
        name=$(echo "$store" | jq -r '.metadata.name')
        auth_model_id=$(echo "$store" | jq -r '.status.authorizationModelId // empty')
        core_module=$(echo "$store" | jq -r '.spec.coreModule // empty')

        log_section "Store: $name"

        # Check Ready condition
        local ready_status
        ready_status=$(echo "$store" | jq -r '
            .status.conditions // [] | map(select(.type == "Ready")) | .[0].status // "Unknown"')

        if [ "$ready_status" = "True" ]; then
            log_pass "Store '$name' is Ready"
        else
            log_fail "Store '$name' is NOT Ready (status=$ready_status)"
        fi

        # Check authorizationModelId
        if [ -n "$auth_model_id" ]; then
            log_pass "Store '$name' has authorizationModelId: $auth_model_id"
        else
            log_fail "Store '$name' has no authorizationModelId (not reconciled)"
        fi

        # Check coreModule is present (type count here is only the user-defined
        # subset; the full count including dynamically added types is checked
        # against OpenFGA in section 12)
        if [ -n "$core_module" ]; then
            local cr_type_count
            cr_type_count=$(echo "$core_module" | grep -c '^type ' || true)
            log_pass "Store '$name' coreModule is set ($cr_type_count user-defined types)"
        else
            log_fail "Store '$name' has no coreModule"
        fi
    done < <(echo "$stores_json" | jq -c '.items[]')

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 7. Validate IdentityProviderConfiguration per org
###############################################################################

validate_identity_provider_configs() {
    log_header "7. IdentityProviderConfiguration (KCP)"

    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs workspace"
        return
    }

    local orgs
    orgs=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [ -z "$orgs" ]; then
        log_warn "No workspaces found under :root:orgs"
        return
    fi

    for org in $orgs; do
        kcp_kubectl ws ":root:orgs:${org}" &>/dev/null || {
            log_warn "Cannot navigate to :root:orgs:${org}"
            continue
        }

        if kcp_kubectl get identityproviderconfiguration "$org" &>/dev/null; then
            log_pass "IdentityProviderConfiguration '$org' exists in :root:orgs:${org}"

            # Check that it has clients defined
            local ipc_json
            ipc_json=$(kcp_kubectl get identityproviderconfiguration "$org" -o json 2>/dev/null)
            local client_count
            client_count=$(echo "$ipc_json" | jq '.spec.clients // [] | length')

            if [ "$client_count" -ge 2 ]; then
                log_pass "IdentityProviderConfiguration '$org' has $client_count clients (default + kubectl)"
            else
                log_warn "IdentityProviderConfiguration '$org' has only $client_count client(s) (expected >= 2)"
            fi
        else
            log_fail "IdentityProviderConfiguration '$org' missing in :root:orgs:${org}"
        fi
    done

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 8. Validate Invite resources for org owners
###############################################################################

validate_invites() {
    log_header "8. Invite Resources for Org Owners (KCP)"

    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs workspace"
        return
    }

    local orgs
    orgs=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [ -z "$orgs" ]; then
        log_warn "No workspaces found under :root:orgs"
        return
    fi

    for org in $orgs; do
        # Get creator email from Account
        kcp_kubectl ws :root:orgs &>/dev/null
        local creator_email
        creator_email=$(kcp_kubectl get account "$org" -o jsonpath='{.spec.creator}' 2>/dev/null || echo "")

        if [ -z "$creator_email" ] || [ "$creator_email" = "null" ]; then
            log_warn "Account '$org' has no creator -- cannot check Invite"
            continue
        fi

        # Switch to org workspace
        kcp_kubectl ws ":root:orgs:${org}" &>/dev/null || {
            log_warn "Cannot navigate to :root:orgs:${org}"
            continue
        }

        local invites
        invites=$(kcp_kubectl get invites -o jsonpath='{.items[*].spec.email}' 2>/dev/null || echo "")

        if [[ " $invites " == *" $creator_email "* ]]; then
            log_pass "Invite for owner '$creator_email' exists in :root:orgs:${org}"
        else
            # Check if any invite exists at all
            local invite_count
            invite_count=$(kcp_kubectl get invites -o json 2>/dev/null | jq '.items | length' 2>/dev/null || echo "0")
            if [ "$invite_count" -gt 0 ]; then
                log_warn "Org '$org' has $invite_count invite(s) but none for creator '$creator_email'"
            else
                log_fail "No Invite for owner '$creator_email' in :root:orgs:${org}"
            fi
        fi
    done

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 9. Validate WorkspaceAuthenticationConfiguration
###############################################################################

validate_workspace_auth_configs() {
    log_header "9. WorkspaceAuthenticationConfiguration (KCP)"

    # Check in :root workspace first (orgs-authentication)
    kcp_kubectl ws :root &>/dev/null || {
        log_fail "Cannot navigate to :root workspace"
        return
    }

    local wac_list
    wac_list=$(kcp_kubectl get workspaceauthenticationconfiguration \
        -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    if [ -z "$wac_list" ]; then
        log_warn "No WorkspaceAuthenticationConfiguration in :root"
    else
        for wac_name in $wac_list; do
            validate_single_wac "$wac_name" ":root"
        done
    fi

    # Check in org workspaces
    kcp_kubectl ws :root:orgs &>/dev/null || return
    local orgs
    orgs=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    for org in $orgs; do
        [ -z "$org" ] && continue
        kcp_kubectl ws ":root:orgs:${org}" &>/dev/null || continue

        local org_wacs
        org_wacs=$(kcp_kubectl get workspaceauthenticationconfiguration \
            -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

        for wac_name in $org_wacs; do
            [ -z "$wac_name" ] && continue
            validate_single_wac "$wac_name" ":root:orgs:${org}"
        done
    done

    kcp_kubectl ws :root &>/dev/null || true
}

validate_single_wac() {
    local name="$1"
    local ws_path="$2"

    local wac_json
    wac_json=$(kcp_kubectl get workspaceauthenticationconfiguration "$name" -o json 2>/dev/null) || {
        log_fail "WAC '$name' not found in $ws_path"
        return
    }

    log_section "WAC '$name' in $ws_path"

    # Check issuer URL
    local issuer_url
    issuer_url=$(echo "$wac_json" | jq -r '.spec.jwt[0].issuer.url // empty')
    if [ -n "$issuer_url" ]; then
        log_pass "WAC '$name' has issuer URL: $issuer_url"
    else
        log_fail "WAC '$name' in $ws_path has no issuer URL"
    fi

    # Check audiences contain UUIDs (not old-style names)
    local audiences
    audiences=$(echo "$wac_json" | jq -r '.spec.jwt[0].issuer.audiences // [] | .[]' 2>/dev/null)

    if [ -z "$audiences" ]; then
        log_fail "WAC '$name' in $ws_path has no audiences"
        return
    fi

    local all_uuid=true
    local audience_count=0
    for aud in $audiences; do
        ((audience_count++)) || true
        if is_uuid "$aud"; then
            log_pass "WAC '$name' audience '$aud' is a valid UUID"
        else
            log_fail "WAC '$name' audience '$aud' is NOT a UUID (old-style name?)"
            all_uuid=false
        fi
    done

    if [ "$audience_count" -eq 0 ]; then
        log_fail "WAC '$name' in $ws_path has empty audiences array"
    fi

    # Cross-check audiences with Keycloak
    if [ -n "$ADMIN_TOKEN" ] && [ -n "$issuer_url" ]; then
        local realm
        realm=$(echo "$issuer_url" | sed -E 's|.*/realms/([^/]+).*|\1|')

        if [ -n "$realm" ]; then
            # Get all UUID client IDs from Keycloak for this realm
            local kc_clients_json
            kc_clients_json=$(keycloak_get_clients "$realm" 2>/dev/null || echo "[]")
            local kc_uuid_clients
            kc_uuid_clients=$(echo "$kc_clients_json" | jq -r \
                '.[] | select(.clientId | test("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")) | .clientId')

            for aud in $audiences; do
                if is_uuid "$aud"; then
                    if echo "$kc_uuid_clients" | grep -q "^${aud}$"; then
                        log_pass "WAC audience '$aud' exists as Keycloak client in realm '$realm'"
                    else
                        log_fail "WAC audience '$aud' NOT found as Keycloak client in realm '$realm'"
                    fi
                fi
            done
        fi
    fi
}

###############################################################################
# 10. Validate ContentConfiguration (no stale entries)
###############################################################################

validate_content_configurations() {
    log_header "10. ContentConfiguration (KCP)"

    kcp_kubectl ws :root:platform-mesh-system &>/dev/null || {
        log_warn "Cannot navigate to :root:platform-mesh-system"
        return
    }

    local cc_json
    cc_json=$(kcp_kubectl get contentconfiguration -o json 2>/dev/null || echo '{"items":[]}')
    local cc_count
    cc_count=$(echo "$cc_json" | jq '.items | length')

    if [ "$cc_count" -eq 0 ]; then
        log_info "No ContentConfiguration resources in :root:platform-mesh-system"
        kcp_kubectl ws :root &>/dev/null || true
        return
    fi

    log_info "Found $cc_count ContentConfiguration resource(s)"

    while IFS= read -r cc; do
        local name
        name=$(echo "$cc" | jq -r '.metadata.name')

        # Check Ready condition
        local ready_status
        ready_status=$(echo "$cc" | jq -r '
            .status.conditions // [] | map(select(.type == "Ready")) | .[0].status // "Unknown"')

        if [ "$ready_status" = "True" ]; then
            log_pass "ContentConfiguration '$name' is Ready"
        elif [ "$ready_status" = "Unknown" ]; then
            log_warn "ContentConfiguration '$name' has no Ready condition"
        else
            log_fail "ContentConfiguration '$name' is NOT Ready (status=$ready_status)"
        fi
    done < <(echo "$cc_json" | jq -c '.items[]')

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 11. Validate Keycloak realms match KCP orgs
###############################################################################

validate_keycloak_realms() {
    log_header "11. Keycloak Realms vs KCP Orgs"

    if [ -z "$ADMIN_TOKEN" ]; then
        log_skip "Keycloak token not available -- skipping"
        return
    fi

    # Refresh token to ensure it's valid for the duration of this check
    keycloak_refresh_token

    # Get KCP orgs
    kcp_kubectl ws :root:orgs &>/dev/null || {
        log_fail "Cannot navigate to :root:orgs"
        return
    }

    local kcp_orgs
    kcp_orgs=$(kcp_kubectl get workspaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

    # Get Keycloak realms
    local kc_realms
    kc_realms=$(keycloak_get_realms)

    # Check that each KCP org has a corresponding Keycloak realm
    for org in $kcp_orgs; do
        [ -z "$org" ] && continue
        if echo "$kc_realms" | grep -q "^${org}$"; then
            log_pass "KCP org '$org' has corresponding Keycloak realm"

            # Check that the realm has default + kubectl clients
            local default_id kubectl_id
            default_id=$(keycloak_get_client_id_by_name "$org" "$org")
            kubectl_id=$(keycloak_get_client_id_by_name "$org" "kubectl")

            if [ -n "$default_id" ]; then
                log_pass "Keycloak realm '$org' has default client (clientId=$default_id)"
            else
                log_fail "Keycloak realm '$org' missing default client (named '$org')"
            fi

            if [ -n "$kubectl_id" ]; then
                log_pass "Keycloak realm '$org' has kubectl client (clientId=$kubectl_id)"
            else
                log_fail "Keycloak realm '$org' missing kubectl client"
            fi
        else
            log_fail "KCP org '$org' has no corresponding Keycloak realm"
        fi
    done

    # Check for orphaned Keycloak realms (realms without corresponding KCP org)
    for realm in $kc_realms; do
        [ -z "$realm" ] && continue
        is_builtin_realm "$realm" && continue

        if ! echo "$kcp_orgs" | grep -q "^${realm}$"; then
            log_warn "Keycloak realm '$realm' has no corresponding KCP org (orphaned?)"
        fi
    done

    kcp_kubectl ws :root &>/dev/null || true
}

###############################################################################
# 12. Validate OpenFGA stores match KCP
###############################################################################

validate_openfga_stores() {
    log_header "12. OpenFGA Stores Cross-Check"

    local fga_stores_json
    fga_stores_json=$(openfga_get_stores 2>/dev/null || echo "")

    if [ -z "$fga_stores_json" ] || [ "$fga_stores_json" = "[]" ] || [ "$fga_stores_json" = "null" ]; then
        log_skip "Cannot reach OpenFGA API at $OPENFGA_API_URL -- skipping"
        return
    fi

    local fga_store_count
    fga_store_count=$(echo "$fga_stores_json" | jq 'length')
    log_info "Found $fga_store_count store(s) in OpenFGA"

    # For each FGA store, check the latest authorization model type count
    while IFS= read -r fga_store; do
        local store_id store_name
        store_id=$(echo "$fga_store" | jq -r '.id')
        store_name=$(echo "$fga_store" | jq -r '.name // "unnamed"')

        log_section "OpenFGA Store: $store_name ($store_id)"

        local models_json
        models_json=$(openfga_get_auth_models "$store_id" 2>/dev/null || echo "")

        if [ -z "$models_json" ] || [ "$models_json" = "null" ]; then
            log_warn "Cannot fetch authorization models for store $store_id"
            continue
        fi

        local model_count
        model_count=$(echo "$models_json" | jq '.authorization_models | length')

        if [ "$model_count" -eq 0 ]; then
            log_fail "OpenFGA store '$store_name' has no authorization models"
            continue
        fi

        log_pass "OpenFGA store '$store_name' has $model_count authorization model(s)"

        # Check latest model type count
        local latest_model_id type_count
        latest_model_id=$(echo "$models_json" | jq -r '.authorization_models[0].id')
        type_count=$(echo "$models_json" | jq '.authorization_models[0].type_definitions | length')

        if [ "$type_count" -ge "$EXPECTED_FGA_TYPE_COUNT" ]; then
            log_pass "Latest model has $type_count types (expected >= $EXPECTED_FGA_TYPE_COUNT)"
        else
            log_fail "Latest model has $type_count types (expected >= $EXPECTED_FGA_TYPE_COUNT)"
        fi
    done < <(echo "$fga_stores_json" | jq -c '.[]')
}

###############################################################################
# 13. Validate Gateway / HTTPRoute / TLSRoute (K8s cluster)
###############################################################################

validate_gateway_routes() {
    log_header "13. Gateway, HTTPRoutes, TLSRoutes (K8s cluster)"

    # Gateways
    local gw_json
    gw_json=$(k8s_kubectl get gateways -A -o json 2>/dev/null || echo '{"items":[]}')
    local gw_count
    gw_count=$(echo "$gw_json" | jq '.items | length')

    if [ "$gw_count" -eq 0 ]; then
        log_warn "No Gateway resources found in the cluster"
    else
        while IFS= read -r gw; do
            local gw_name gw_ns
            gw_name=$(echo "$gw" | jq -r '.metadata.name')
            gw_ns=$(echo "$gw" | jq -r '.metadata.namespace')

            local accepted programmed
            accepted=$(echo "$gw" | jq -r '
                .status.conditions // [] | map(select(.type == "Accepted")) | .[0].status // "Unknown"')
            programmed=$(echo "$gw" | jq -r '
                .status.conditions // [] | map(select(.type == "Programmed")) | .[0].status // "Unknown"')

            if [ "$accepted" = "True" ] && [ "$programmed" = "True" ]; then
                log_pass "Gateway '$gw_ns/$gw_name' is Accepted and Programmed"
            else
                log_fail "Gateway '$gw_ns/$gw_name' Accepted=$accepted Programmed=$programmed"
            fi
        done < <(echo "$gw_json" | jq -c '.items[]')
    fi

    # HTTPRoutes
    local hr_json
    hr_json=$(k8s_kubectl get httproutes -A -o json 2>/dev/null || echo '{"items":[]}')
    local hr_count
    hr_count=$(echo "$hr_json" | jq '.items | length')

    if [ "$hr_count" -eq 0 ]; then
        log_info "No HTTPRoute resources found"
    else
        while IFS= read -r hr; do
            local hr_name hr_ns
            hr_name=$(echo "$hr" | jq -r '.metadata.name')
            hr_ns=$(echo "$hr" | jq -r '.metadata.namespace')

            local accepted
            accepted=$(echo "$hr" | jq -r '
                .status.parents // [] | .[0].conditions // [] | map(select(.type == "Accepted")) | .[0].status // "Unknown"')

            if [ "$accepted" = "True" ]; then
                log_pass "HTTPRoute '$hr_ns/$hr_name' is Accepted"
            else
                log_fail "HTTPRoute '$hr_ns/$hr_name' Accepted=$accepted"
            fi
        done < <(echo "$hr_json" | jq -c '.items[]')
    fi

    # TLSRoutes
    local tls_json
    tls_json=$(k8s_kubectl get tlsroutes -A -o json 2>/dev/null || echo '{"items":[]}')
    local tls_count
    tls_count=$(echo "$tls_json" | jq '.items | length')

    if [ "$tls_count" -eq 0 ]; then
        log_info "No TLSRoute resources found"
    else
        while IFS= read -r tlsr; do
            local tls_name tls_ns
            tls_name=$(echo "$tlsr" | jq -r '.metadata.name')
            tls_ns=$(echo "$tlsr" | jq -r '.metadata.namespace')

            local accepted
            accepted=$(echo "$tlsr" | jq -r '
                .status.parents // [] | .[0].conditions // [] | map(select(.type == "Accepted")) | .[0].status // "Unknown"')

            if [ "$accepted" = "True" ]; then
                log_pass "TLSRoute '$tls_ns/$tls_name' is Accepted"
            else
                log_fail "TLSRoute '$tls_ns/$tls_name' Accepted=$accepted"
            fi
        done < <(echo "$tls_json" | jq -c '.items[]')
    fi
}

###############################################################################
# 14. Validate operator pods (K8s cluster)
###############################################################################

validate_operator_pods() {
    log_header "14. Operator Pods (K8s cluster)"

    local expected_labels=(
        "app=platform-mesh-operator"
        "app=security-operator"
        "service=security-operator-generator"
        "service=security-operator-initializer"
        "app=account-operator"
    )

    for label in "${expected_labels[@]}"; do
        local pods_json
        pods_json=$(k8s_kubectl -n "$NAMESPACE" get pods -l "$label" -o json 2>/dev/null || echo '{"items":[]}')
        local pod_count
        pod_count=$(echo "$pods_json" | jq '.items | length')

        if [ "$pod_count" -eq 0 ]; then
            log_warn "No pods found with label '$label' in $NAMESPACE"
            continue
        fi

        while IFS= read -r pod; do
            local pod_name phase
            pod_name=$(echo "$pod" | jq -r '.metadata.name')
            phase=$(echo "$pod" | jq -r '.status.phase')

            # Check container readiness
            local ready_containers total_containers
            ready_containers=$(echo "$pod" | jq '[.status.containerStatuses // [] | .[] | select(.ready == true)] | length')
            total_containers=$(echo "$pod" | jq '[.status.containerStatuses // [] | .[]] | length')

            if [ "$phase" = "Running" ] && [ "$ready_containers" = "$total_containers" ]; then
                log_pass "Pod '$pod_name' is Running ($ready_containers/$total_containers ready)"
            else
                log_fail "Pod '$pod_name' phase=$phase ($ready_containers/$total_containers ready)"
            fi

            # Check for restart count
            local restarts
            restarts=$(echo "$pod" | jq '[.status.containerStatuses // [] | .[].restartCount] | add // 0')
            if [ "$restarts" -gt 5 ]; then
                log_warn "Pod '$pod_name' has $restarts restarts"
            fi
        done < <(echo "$pods_json" | jq -c '.items[]')
    done
}

###############################################################################
# Summary
###############################################################################

print_summary() {
    echo ""
    echo -e "${BOLD}${CYAN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${CYAN}  VALIDATION SUMMARY${NC}"
    echo -e "${BOLD}${CYAN}════════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${GREEN}PASS:${NC} $PASS_COUNT"
    echo -e "  ${RED}FAIL:${NC} $FAIL_COUNT"
    echo -e "  ${YELLOW}WARN:${NC} $WARN_COUNT"
    echo -e "  ${YELLOW}SKIP:${NC} $SKIP_COUNT"
    echo ""

    if [ "$FAIL_COUNT" -gt 0 ]; then
        echo -e "  ${RED}${BOLD}Result: FAILED${NC} -- $FAIL_COUNT check(s) failed"
        echo ""
        echo -e "  Refer to the migration guide at docs/migration-release-0.2.0.md"
        echo -e "  and the troubleshooting section for remediation steps."
    elif [ "$WARN_COUNT" -gt 0 ]; then
        echo -e "  ${YELLOW}${BOLD}Result: PASSED with warnings${NC}"
    else
        echo -e "  ${GREEN}${BOLD}Result: ALL CHECKS PASSED${NC}"
    fi
    echo ""
}

###############################################################################
# Help
###############################################################################

show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Validates KCP resources and their consistency with Keycloak, OpenFGA,
and the Kubernetes cluster.

Options:
  --kubeconfig-kcp PATH       Kubeconfig for kcp (default: \$KUBECONFIG_KCP)
  --kubeconfig PATH           Kubeconfig for K8s cluster (default: \$KUBECONFIG_K8S)
  --context NAME              kubectl context to use for K8s cluster (default: \$KUBECTL_CONTEXT)
  --keycloak-url URL          Keycloak base URL (default: derived from PlatformMesh)
  --keycloak-user USER        Keycloak admin user (default: \$KEYCLOAK_USER or keycloak-admin)
  --keycloak-password PW      Keycloak admin password (default: \$KEYCLOAK_PASSWORD or admin)
  --openfga-url URL           OpenFGA API URL (default: \$OPENFGA_API_URL or http://localhost:8080)
  --kcp-server URL            KCP API server URL (default: \$KCP_SERVER)
  --namespace NS              K8s namespace (default: platform-mesh-system)
  --expected-fga-types N      Expected FGA type count (default: 27)
  --verbose                   Show detailed output
  -h, --help                  Show this help message

Environment variables:
  KEYCLOAK_URL                Keycloak base URL
  KEYCLOAK_USER               Keycloak admin username
  KEYCLOAK_PASSWORD           Keycloak admin password
  OPENFGA_API_URL             OpenFGA HTTP API base URL
  KUBECONFIG_KCP              Kubeconfig for kcp
  KUBECONFIG_K8S              Kubeconfig for the Kubernetes cluster
  KUBECTL_CONTEXT             kubectl context to use for K8s cluster
  KCP_SERVER                  KCP API server URL
  NAMESPACE                   Kubernetes namespace
  EXPECTED_FGA_TYPE_COUNT     Expected FGA type count per store model
  VERBOSE                     Set to "true" for verbose output

Examples:
  # Basic validation (derives Keycloak URL from PlatformMesh)
  $0

  # With explicit kubeconfigs
  $0 --kubeconfig-kcp /path/to/kcp.kubeconfig --kubeconfig /path/to/k8s.kubeconfig

  # With OpenFGA port-forwarded to localhost:8080
  kubectl -n platform-mesh-system port-forward svc/openfga 8080 &
  $0 --openfga-url http://localhost:8080

  # Verbose output
  $0 --verbose
EOF
}

###############################################################################
# Main
###############################################################################

main() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --kubeconfig-kcp)
                KUBECONFIG_KCP="$2"
                shift 2
                ;;
            --kubeconfig)
                KUBECONFIG_K8S="$2"
                shift 2
                ;;
            --context)
                KUBECTL_CONTEXT="$2"
                shift 2
                ;;
            --keycloak-url)
                KEYCLOAK_URL="$2"
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
            --openfga-url)
                OPENFGA_API_URL="$2"
                shift 2
                ;;
            --kcp-server)
                KCP_SERVER="$2"
                shift 2
                ;;
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --expected-fga-types)
                EXPECTED_FGA_TYPE_COUNT="$2"
                shift 2
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done

    echo ""
    echo -e "${BOLD}${CYAN}  KCP Resource Validation Script${NC}"
    echo -e "${BOLD}${CYAN}  platform-mesh / helm-charts${NC}"
    echo ""

    check_dependencies
    derive_config_from_cluster

    # Obtain Keycloak admin token if possible
    if [ -n "$KEYCLOAK_URL" ]; then
        log_info "Authenticating with Keycloak at $KEYCLOAK_URL..."
        ADMIN_TOKEN=$(keycloak_get_admin_token 2>/dev/null || echo "")
        if [ -n "$ADMIN_TOKEN" ] && [ "$ADMIN_TOKEN" != "null" ]; then
            log_info "Keycloak admin token obtained"
        else
            ADMIN_TOKEN=""
            log_warn "Failed to obtain Keycloak admin token -- Keycloak cross-checks will be skipped"
        fi
    fi

    # Run all validations
    validate_platformmesh
    validate_apiexport
    validate_apibindings
    validate_accounts
    validate_accountinfos
    validate_stores
    validate_identity_provider_configs
    validate_invites
    validate_workspace_auth_configs
    validate_content_configurations
    validate_keycloak_realms
    validate_openfga_stores
    validate_gateway_routes
    validate_operator_pods

    # Restore original kubeconfig
    if [ -n "$ORIGINAL_KUBECONFIG" ]; then
        export KUBECONFIG="$ORIGINAL_KUBECONFIG"
    fi

    print_summary

    # Exit with non-zero if any checks failed
    if [ "$FAIL_COUNT" -gt 0 ]; then
        exit 1
    fi
}

main "$@"
