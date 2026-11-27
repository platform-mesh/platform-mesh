#!/usr/bin/env bash

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

# Talks to the access virtual workspace (chart access-vw) of the Tilt
# environment with curl, through the kcp front proxy at /services/access.
#
#   hack/access-vw.sh [review]            POST a SelfClusterAccessReview: the workspaces the caller can access
#   hack/access-vw.sh list                GET the same review as a list of one, with the resource version to watch from
#   hack/access-vw.sh watch [VERSION]     follow the review until interrupted: it is sent again when it changes
#                                         (checked every 10s); from VERSION of a list, or starting with the review as it is
#   hack/access-vw.sh discovery           GET the API resources the virtual workspace serves
#   hack/access-vw.sh bind <workspace>    bind the access APIExport in a workspace (e.g. root:orgs:bob)
#   hack/access-vw.sh unbind <workspace>  remove that binding again
#
# What a review answers depends on the provider the workspace runs with
# (operators/account-operator/virtual-workspace/README.md):
#
# - rbac: only workspaces that bind access.platform-mesh.io are indexed, so a
#   review is empty until `bind` ran for at least one workspace the caller has
#   a (Cluster)RoleBinding in.
# - openfga: the accounts of one organization the caller can access. The
#   review (also list and watch) has to name a workspace of the organization with --cluster, and the
#   caller has to be a user OpenFGA knows, so not the kcp admin: use --as or
#   --token. bind and unbind do not apply.
#
#   --cluster WORKSPACE  address the review to a workspace, by path or cluster
#                        id (e.g. root:orgs:bob); the front proxy then also
#                        accepts tokens of that workspace's identity provider
#
# The caller is the kcp admin (client certificate) unless told otherwise:
#
#   --token TOKEN        authenticate with a bearer token (OIDC ID token) instead
#   --as USER            impersonate USER (as the kcp admin)
#   --as-group GROUP     impersonate GROUP, repeatable; needs --as
#   --kubeconfig PATH    kcp admin kubeconfig (default: the one of the Tilt
#                        environment, re-exported from the kind cluster)
#
# The curl command is printed to stderr, ready to copy; the response goes to
# stdout. The client certificate is unpacked to contrib/tilt/.secret/access-vw.
#
# Environment:
#   KIND_CONTEXT        kube context of the kind cluster (default: kind-platform-mesh)
#   PLATFORM_NAMESPACE  namespace of the installation (default: platform-mesh-system)

set -euo pipefail

die() { echo "$@" >&2; exit 1; }

GROUP_VERSION="access.platform-mesh.io/v1alpha1"
EXPORT_PATH="root:access:controllers"
EXPORT_NAME="access.platform-mesh.io"

repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
secret_dir="$repo/contrib/tilt/.secret"

kubeconfig=""
token=""
cluster=""
as=""
as_groups=()
args=()
while [ $# -gt 0 ]; do
    case "$1" in
        --kubeconfig) kubeconfig="${2:?--kubeconfig needs a path}"; shift 2 ;;
        --token) token="${2:?--token needs a token}"; shift 2 ;;
        --as) as="${2:?--as needs a user}"; shift 2 ;;
        --as-group) as_groups+=("${2:?--as-group needs a group}"); shift 2 ;;
        --cluster) cluster="${2:?--cluster needs a workspace}"; shift 2 ;;
        -h | --help) awk 'NR >= 17 && !/^#/ { exit } NR >= 17 { sub(/^# ?/, ""); print }' "$0"; exit 0 ;;
        -*) die "unknown flag $1" ;;
        *) args+=("$1"); shift ;;
    esac
done
set -- "${args[@]+"${args[@]}"}"
cmd="${1:-review}"

if [ ${#as_groups[@]} -gt 0 ] && [ -z "$as" ]; then
    die "--as-group needs --as"
fi

# The admin kubeconfig Tilt exported goes stale when the cluster is recreated.
if [ -z "$kubeconfig" ]; then
    kubeconfig="$secret_dir/kcp-admin.kubeconfig"
    KUBE_CONTEXT="${KIND_CONTEXT:-kind-platform-mesh}" NAMESPACE="${PLATFORM_NAMESPACE:-platform-mesh-system}" \
        "$repo/contrib/tilt/hack/export-secret.sh" kubeconfig-kcp-admin kubeconfig "$kubeconfig" \
        || die "no kcp admin kubeconfig; is the Tilt environment up (task tilt:up -- vw)?"
fi

kc() { kubectl --kubeconfig "$kubeconfig" "$@"; }
field() { kc config view --raw --minify --flatten -o jsonpath="$1"; }

# https://<front proxy>, without the /clusters/<workspace> of the context.
server="$(field '{.clusters[0].cluster.server}')"
server="${server%%/clusters/*}"
url="$server/services/access${cluster:+/clusters/$cluster}/apis/$GROUP_VERSION"

# curl wants the credentials of the kubeconfig as files.
umask 077
mkdir -p "$secret_dir/access-vw"
# Relative where possible: shorter in the printed command.
creds="$secret_dir/access-vw"
creds="${creds#"$PWD/"}"
field '{.clusters[0].cluster.certificate-authority-data}' | base64 -d > "$creds/ca.crt"
field '{.users[0].user.client-certificate-data}' | base64 -d > "$creds/client.crt"
field '{.users[0].user.client-key-data}' | base64 -d > "$creds/client.key"

# Prints the command, then runs it.
run_curl() {
    local c=(curl -sS --cacert "$creds/ca.crt")
    if [ -n "$token" ]; then
        c+=(-H "Authorization: Bearer $token")
    else
        c+=(--cert "$creds/client.crt" --key "$creds/client.key")
    fi
    if [ -n "$as" ]; then
        c+=(-H "Impersonate-User: $as")
    fi
    local g
    for g in "${as_groups[@]+"${as_groups[@]}"}"; do
        c+=(-H "Impersonate-Group: $g")
    done
    c+=("$@")
    local a line="+"
    for a in "${c[@]}"; do
        case "$a" in
            *[!A-Za-z0-9_./:=@%+-]*) line+=" '${a//\'/\'\\\'\'}'" ;;
            *) line+=" $a" ;;
        esac
    done
    echo "$line" >&2
    # Print the HTTP status when it is not a success: a gateway error has no body.
    "${c[@]}" -w '%{stderr}%{onerror}HTTP %{http_code}\n' --fail-with-body
    echo
}

binding() {
    cat <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: $EXPORT_NAME
spec:
  reference:
    export:
      path: $EXPORT_PATH
      name: $EXPORT_NAME
  permissionClaims:
$(for r in clusterrolebindings rolebindings clusterroles roles; do
        cat <<CLAIM
    - group: rbac.authorization.k8s.io
      resource: $r
      verbs: [get, list, watch]
      state: Accepted
      selector:
        matchAll: true
CLAIM
    done)
EOF
}

case "$cmd" in
    review)
        run_curl -X POST -H "Content-Type: application/json" \
            -d "{\"apiVersion\":\"$GROUP_VERSION\",\"kind\":\"SelfClusterAccessReview\"}" \
            "$url/selfclusteraccessreviews"
        ;;
    list)
        run_curl "$url/selfclusteraccessreviews"
        ;;
    watch)
        # -N: print each event as it arrives.
        run_curl -N "$url/selfclusteraccessreviews?watch=true${2:+&resourceVersion=$2}"
        ;;
    discovery)
        run_curl "$url"
        ;;
    bind)
        ws="${2:?usage: $0 bind <workspace>}"
        binding | kc --server "$server/clusters/$ws" apply -f -
        kc --server "$server/clusters/$ws" wait "apibinding/$EXPORT_NAME" --for=condition=Ready --timeout=1m
        ;;
    unbind)
        ws="${2:?usage: $0 unbind <workspace>}"
        kc --server "$server/clusters/$ws" delete apibinding "$EXPORT_NAME"
        ;;
    *)
        die "unknown command $cmd (review, list, watch, discovery, bind, unbind)"
        ;;
esac
