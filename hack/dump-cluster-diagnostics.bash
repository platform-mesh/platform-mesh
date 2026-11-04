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

# Dumps the state of a Platform Mesh installation for post-mortem analysis.
#
#   dump-cluster-diagnostics.bash [<output-dir>]   (default runtime-artifacts/)
#
# KUBE_CONTEXT selects the cluster (default: current context),
# PLATFORM_NAMESPACE the namespace (default platform-mesh-system),
# ADMIN_KUBECONFIG a kcp admin kubeconfig to dump the kcp side as well.
#
# Every command may fail (a resource type may not be installed); its output
# then lands in <file>.error and the dump continues.

log() { echo ">>> $*"; }
die() { echo "$@" >&2; exit 1; }

out="${1:-runtime-artifacts}"
ns="${PLATFORM_NAMESPACE:-platform-mesh-system}"

declare -a kubectl_args=()
test -n "${KUBE_CONTEXT:-}" && kubectl_args+=( --context "$KUBE_CONTEXT" )

# dump <file> <kubectl args...>: one command, one file; on failure the
# output goes to <file>.error instead so .json files stay valid JSON.
dump() {
    local file="$out/$1"
    shift
    if ! kubectl "${kubectl_args[@]}" "$@" > "$file" 2>&1; then
        mv "$file" "$file.error"
    fi
}

mkdir -p "$out" || die "creating $out failed"

# nodes, events, pods, workloads and current logs of every namespace
log "cluster-info dump"
kubectl "${kubectl_args[@]}" cluster-info dump --all-namespaces --output-directory="$out/cluster-info" > "$out/cluster-info-dump.log" 2>&1 || true

log "platform mesh in $ns"
dump platformmesh.json -n "$ns" get platformmesh -o json
dump ocm.json -n "$ns" get component,resources.delivery.ocm.software -o json
dump flux.json get helmreleases,ocirepositories -A -o json
dump kcp-operator.json -n "$ns" get rootshard,shard,frontproxy,cacheserver,kubeconfig,etcd -o json
dump cert-manager.json -n "$ns" get certificate,issuer -o json
dump gateway.json -n "$ns" get gateway,httproute -o json
dump gateway-all.json get gatewayclass,gateway,httproute,tlsroute,tcproute -A -o json
dump databases.json -n "$ns" get cluster.postgresql.cnpg.io,database.postgresql.cnpg.io,keycloak -o json

log "previous logs in $ns"
for p in $(kubectl "${kubectl_args[@]}" -n "$ns" get pods -o name 2>/dev/null); do
    file="$out/${ns}_${p//\//_}.previous.log"
    kubectl "${kubectl_args[@]}" -n "$ns" logs "$p" --all-containers --previous --tail=500 > "$file" 2>/dev/null || rm -f "$file"
done

if test -n "${ADMIN_KUBECONFIG:-}" && test -f "$ADMIN_KUBECONFIG"; then
    log "kcp via $ADMIN_KUBECONFIG"
    base="$(kubectl --kubeconfig "$ADMIN_KUBECONFIG" config view --minify -o jsonpath='{.clusters[0].cluster.server}' | sed 's|/clusters/root$||')"
    # kcp <file> <workspace> <kubectl args...>
    kcp() {
        local file="$out/$1" ws="$2"
        shift 2
        if ! kubectl --kubeconfig "$ADMIN_KUBECONFIG" --server "$base/clusters/$ws" "$@" -o json > "$file" 2>&1; then
            mv "$file" "$file.error"
        fi
    }
    kcp kcp-root.json root get workspaces,apiexports,apibindings
    kcp kcp-platform-mesh-system.json root:platform-mesh-system get apiexports,apiexportendpointslices,apibindings
    kcp kcp-providers.json root:providers get workspaces
    kcp kcp-orgs.json root:orgs get workspaces,apiexportpolicies
fi

log "wrote $out"
