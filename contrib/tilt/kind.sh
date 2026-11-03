#!/usr/bin/env sh

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

# Prepares the kind cluster the Tilt environment deploys into.
#
# - creates the cluster if absent
# - exports its kubeconfig
# - makes CoreDNS answer *.sslip.io itself

log() { echo ">>> $*"; }
die() { echo "$@" >&2; exit 1; }
installed() { command -v "$1" > /dev/null 2>&1; }

cluster_name="${KIND_CLUSTER_NAME:-platform-mesh}"

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
for tool in "$kind" kubectl helm "${TILT:-tilt}"; do
    installed "$tool" || die "$tool not found"
done

if ! "$kind" get clusters 2> /dev/null | grep -qx "$cluster_name"; then
    log "creating kind cluster $cluster_name"
    "$kind" create cluster --name "$cluster_name" || die "kind create cluster failed"
fi

"$kind" export kubeconfig --name "$cluster_name" || die "kind export kubeconfig failed"
ctx="kind-$cluster_name"

corefile="$(kubectl --context "$ctx" -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}')" \
    || die "reading coredns Corefile failed"

if echo "$corefile" | grep -q sslip.io; then
    log "coredns already resolves sslip.io"
    exit 0
fi

log "adding sslip.io server block to coredns"
tmp="$(mktemp -d)" || die "mktemp failed"
finish() { rm -rf "$tmp"; }
trap finish EXIT INT

printf '%s%s\n' "$coredns_sslip_block" "$corefile" > "$tmp/Corefile" || die "writing Corefile failed"
kubectl --context "$ctx" -n kube-system create configmap coredns \
    --from-file=Corefile="$tmp/Corefile" --dry-run=client -o yaml \
    | kubectl --context "$ctx" apply -f - || die "updating coredns configmap failed"
kubectl --context "$ctx" -n kube-system rollout restart deployment/coredns || die "restarting coredns failed"
kubectl --context "$ctx" -n kube-system rollout status deployment/coredns --timeout=180s || die "coredns did not become ready"
