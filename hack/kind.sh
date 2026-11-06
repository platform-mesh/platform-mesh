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

# Manages the kind clusters used for local development and e2e tests: the
# Tilt environment, the platform-mesh-operator kind suite and the deployer
# suite all go through this script.
#
#   hack/kind.sh up   [--name N] [--kubeconfig PATH] [--config FILE] [--image IMG]
#   hack/kind.sh down [--name N] [--kubeconfig PATH]
#
# up is idempotent:
# - creates the cluster if absent (--config and --image only apply then)
# - exports its kubeconfig (to PATH when --kubeconfig is given, otherwise to
#   kind's default, $KUBECONFIG or ~/.kube/config)
# - makes CoreDNS answer *.sslip.io itself, once
#
# Environment:
#   KIND               kind binary (default: kind on PATH)
#   KUBECTL            kubectl binary (default: kubectl on PATH)
#   KIND_CLUSTER_NAME  default for --name (default: platform-mesh)

set -euo pipefail

log() { echo ">>> $*"; }
die() { echo "$@" >&2; exit 1; }
installed() { command -v "$1" > /dev/null 2>&1; }

usage() {
    cat >&2 << 'USAGE'
usage: hack/kind.sh up   [--name N] [--kubeconfig PATH] [--config FILE] [--image IMG]
       hack/kind.sh down [--name N] [--kubeconfig PATH]
USAGE
    exit 2
}

# kind forwards DNS to the host resolver, which commonly drops answers that
# point at private IPs (rebinding protection). Resolve sslip.io in-cluster.
coredns_sslip_block='sslip.io:53 {
    errors
    template IN A sslip.io {
        match "[.-](?P<a>[0-9]{1,3})-(?P<b>[0-9]{1,3})-(?P<c>[0-9]{1,3})-(?P<d>[0-9]{1,3})[.]sslip[.]io[.]$"
        answer "{{ .Name }} 60 IN A {{ .Group.a }}.{{ .Group.b }}.{{ .Group.c }}.{{ .Group.d }}"
        fallthrough
    }
    forward . /etc/resolv.conf
}
'

kind="${KIND:-kind}"
kubectl="${KUBECTL:-kubectl}"

cluster_exists() {
    "$kind" get clusters 2> /dev/null | grep -qx "$cluster_name"
}

# kubectl against the cluster, through the exported kubeconfig.
kc() {
    "$kubectl" ${kubeconfig_args[@]+"${kubeconfig_args[@]}"} --context "kind-$cluster_name" "$@"
}

patch_coredns() {
    local corefile tmp
    corefile="$(kc -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}')" \
        || die "reading coredns Corefile failed"

    if grep -q sslip.io <<< "$corefile"; then
        log "coredns already resolves sslip.io"
        return 0
    fi

    log "adding sslip.io server block to coredns"
    tmp="$(mktemp -d)" || die "mktemp failed"
    # shellcheck disable=SC2064 # expand $tmp now
    trap "rm -rf '$tmp'" EXIT INT

    printf '%s%s\n' "$coredns_sslip_block" "$corefile" > "$tmp/Corefile" || die "writing Corefile failed"
    kc -n kube-system create configmap coredns \
        --from-file=Corefile="$tmp/Corefile" --dry-run=client -o yaml \
        | kc apply -f - || die "updating coredns configmap failed"
    kc -n kube-system rollout restart deployment/coredns || die "restarting coredns failed"
    kc -n kube-system rollout status deployment/coredns --timeout=180s || die "coredns did not become ready"
}

cmd_up() {
    installed "$kubectl" || die "$kubectl not found"

    if cluster_exists; then
        log "kind cluster $cluster_name exists"
    else
        log "creating kind cluster $cluster_name"
        local args=(create cluster --name "$cluster_name" ${kubeconfig_args[@]+"${kubeconfig_args[@]}"})
        if [[ -n "$config" ]]; then args+=(--config "$config"); fi
        if [[ -n "$image" ]]; then args+=(--image "$image"); fi
        "$kind" "${args[@]}" || die "kind create cluster failed"
    fi

    "$kind" export kubeconfig --name "$cluster_name" ${kubeconfig_args[@]+"${kubeconfig_args[@]}"} || die "kind export kubeconfig failed"
    patch_coredns
}

cmd_down() {
    if ! cluster_exists; then
        log "kind cluster $cluster_name does not exist"
        return 0
    fi
    log "deleting kind cluster $cluster_name"
    "$kind" delete cluster --name "$cluster_name" ${kubeconfig_args[@]+"${kubeconfig_args[@]}"} || die "kind delete cluster failed"
}

[[ $# -ge 1 ]] || usage
action="$1"
shift

cluster_name="${KIND_CLUSTER_NAME:-platform-mesh}"
kubeconfig=""
config=""
image=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --name) [[ $# -ge 2 ]] || usage; cluster_name="$2"; shift 2 ;;
        --name=*) cluster_name="${1#*=}"; shift ;;
        --kubeconfig) [[ $# -ge 2 ]] || usage; kubeconfig="$2"; shift 2 ;;
        --kubeconfig=*) kubeconfig="${1#*=}"; shift ;;
        --config) [[ $# -ge 2 ]] || usage; config="$2"; shift 2 ;;
        --config=*) config="${1#*=}"; shift ;;
        --image) [[ $# -ge 2 ]] || usage; image="$2"; shift 2 ;;
        --image=*) image="${1#*=}"; shift ;;
        -h | --help) usage ;;
        *) echo "unknown argument: $1" >&2; usage ;;
    esac
done

kubeconfig_args=()
if [[ -n "$kubeconfig" ]]; then kubeconfig_args=(--kubeconfig "$kubeconfig"); fi

installed "$kind" || die "$kind not found"

case "$action" in
    up) cmd_up ;;
    down) cmd_down ;;
    *) usage ;;
esac
