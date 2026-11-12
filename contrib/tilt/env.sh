# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Exports the environment to reach the Tilt installation. Source it:
#
#   . contrib/tilt/env.sh && task tests:portal-e2e
#
# KIND_CONTEXT selects the cluster (default kind-platform-mesh).

_tilt_repo="$(git rev-parse --show-toplevel)" || return 1
_tilt_secret="$_tilt_repo/contrib/tilt/.secret"

KIND_CONTEXT="${KIND_CONTEXT:-kind-platform-mesh}"
export KIND_CONTEXT

PLATFORM_NAMESPACE="${PLATFORM_NAMESPACE:-platform-mesh-system}"
export PLATFORM_NAMESPACE

_pm_kubectl() {
    kubectl --context "$KIND_CONTEXT" -n "$PLATFORM_NAMESPACE" "$@"
}
# Fail early, with a hint, when there is no Tilt environment to talk to.
if ! _pm_kubectl get platformmesh platform-mesh > /dev/null 2>&1; then
    echo "Tilt environment not detected: no PlatformMesh platform-mesh in $PLATFORM_NAMESPACE of context $KIND_CONTEXT; run 'task tilt:up' or 'task tilt:ci'" >&2
    return 1
fi

BASE_DOMAIN="$(_pm_kubectl get platformmesh platform-mesh -o jsonpath='{.spec.exposure.baseDomain}')" || return 1
export BASE_DOMAIN

_tilt_port="$(_pm_kubectl get platformmesh platform-mesh -o jsonpath='{.spec.exposure.port}')" || return 1

if [ -z "$BASE_DOMAIN" ] || [ -z "$_tilt_port" ]; then
    echo "PlatformMesh exposure.baseDomain or exposure.port is empty" >&2
fi

_tilt_encoded="$(_pm_kubectl get secret keycloak-admin -o jsonpath='{.data.username}')" || return 1
KEYCLOAK_ADMIN_USER="$(echo "$_tilt_encoded" | base64 -d)" || return 1
export KEYCLOAK_ADMIN_USER

_tilt_encoded="$(_pm_kubectl get secret keycloak-admin -o jsonpath='{.data.password}')" || return 1
KEYCLOAK_ADMIN_PASSWORD="$(echo "$_tilt_encoded" | base64 -d)" || return 1
export KEYCLOAK_ADMIN_PASSWORD

if [ -z "$KEYCLOAK_ADMIN_USER" ] || [ -z "$KEYCLOAK_ADMIN_PASSWORD" ]; then
    echo "secret keycloak-admin has no username or password" >&2
fi

PORTAL_BASE_URL="https://$BASE_DOMAIN:$_tilt_port/"
export PORTAL_BASE_URL

KEYCLOAK_BASE_URL="${PORTAL_BASE_URL}keycloak"
export KEYCLOAK_BASE_URL

KCP_URL="https://kcp.api.$BASE_DOMAIN:$_tilt_port"
export KCP_URL

# Re-export the kcp admin kubeconfig and root CA from the cluster: Tilt
# exports them once, but the cluster's address (the kind node IP) can change
# between runs and the copies go stale.
KUBE_CONTEXT="$KIND_CONTEXT" NAMESPACE="$PLATFORM_NAMESPACE" \
    "$_tilt_repo/contrib/tilt/hack/export-secret.sh" kubeconfig-kcp-admin kubeconfig "$_tilt_secret/kcp-admin.kubeconfig" || return 1
KUBE_CONTEXT="$KIND_CONTEXT" NAMESPACE="$PLATFORM_NAMESPACE" \
    "$_tilt_repo/contrib/tilt/hack/export-secret.sh" root-ca tls.crt "$_tilt_secret/kcp-root-ca.crt" || return 1
ADMIN_KUBECONFIG="$_tilt_secret/kcp-admin.kubeconfig"
export ADMIN_KUBECONFIG

HTTPBIN_PROVIDER_KUBECONFIG="$_tilt_secret/httpbin-provider.kubeconfig"
export HTTPBIN_PROVIDER_KUBECONFIG

KCP_ROOT_CA_PATH="${KCP_ROOT_CA_PATH:-$_tilt_secret/kcp-root-ca.crt}"
export KCP_ROOT_CA_PATH

MKCERT_CA_PATH="${MKCERT_CA_PATH:-$_tilt_secret/ca.crt}"
export MKCERT_CA_PATH

unset _tilt_repo _tilt_secret _tilt_port _tilt_encoded
