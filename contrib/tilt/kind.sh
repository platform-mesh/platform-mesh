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
# - creates the cluster if absent, publishing the gateway NodePort on the host
# - exports its kubeconfig
# - makes CoreDNS answer sslip.io with the node's address

log() { echo ">>> $*"; }
die() { echo "$@" >&2; exit 1; }
installed() { command -v "$1" > /dev/null 2>&1; }

cluster_name="${KIND_CLUSTER_NAME:-platform-mesh}"
kind_config="$(git rev-parse --show-toplevel)/contrib/tilt/kind.yaml"

node_port="$(grep -o 'hostPort: [0-9]\{1,\}' "$kind_config" | cut -d ' ' -f2)"
test -n "$node_port" || die "no hostPort in $kind_config"

kind="${KIND:-kind}"
for tool in "$kind" kubectl helm "${TILT:-tilt}"; do
    installed "$tool" || die "$tool not found"
done

if ! "$kind" get clusters 2> /dev/null | grep -qx "$cluster_name"; then
    log "creating kind cluster $cluster_name"
    "$kind" create cluster --name "$cluster_name" --config "$kind_config" || die "kind create cluster failed"
fi

# The host port kind published for the node's NodePort, empty if none
published_port() {
    node="$("${kind}" get nodes --name "$cluster_name" 2> /dev/null | head -n 1)"
    test -n "$node" || return 0
    for rt in docker podman; do
        installed "$rt" || continue
        "$rt" inspect "$node" > /dev/null 2>&1 || continue
        "$rt" port "$node" "${node_port}/tcp" 2> /dev/null
        return
    done
}

# ensure that the kind cluster publishes the gateway port
test -n "$(published_port)" || die \
    "kind cluster $cluster_name publishes no host port $node_port; recreate it: $kind delete cluster --name $cluster_name"

"$kind" export kubeconfig --name "$cluster_name" || die "kind export kubeconfig failed"
ctx="kind-$cluster_name"

# Local image registry. Tilt pushes to localhost:${registry_port}; each kind
# node resolves that via /etc/containerd/certs.d/localhost:${registry_port}/hosts.toml
# to the kind-registry container on the kind network.
# See https://kind.sigs.k8s.io/docs/user/local-registry/.
registry_name="kind-registry"
registry_port=5001
registry_runtime=""
for rt in docker podman; do
    installed "$rt" && "$rt" info > /dev/null 2>&1 && registry_runtime="$rt" && break
done
test -n "$registry_runtime" || die "need docker or podman for the local registry"

if ! "$registry_runtime" inspect -f '{{.State.Running}}' "$registry_name" 2> /dev/null | grep -qx true; then
    log "starting local registry $registry_name on localhost:$registry_port"
    "$registry_runtime" rm -f "$registry_name" > /dev/null 2>&1 || true
    "$registry_runtime" run -d --restart=always \
        -p "127.0.0.1:${registry_port}:5000" \
        --network kind \
        --name "$registry_name" docker.io/library/registry:2 > /dev/null \
        || die "starting $registry_name failed"
fi

# Write the per-node hosts.toml once per `kind.sh` run. The directory name must
# match the registry host:port Tilt pushes to so containerd picks it up.
for node in $("$kind" get nodes --name "$cluster_name"); do
    "$registry_runtime" exec "$node" sh -c "
        mkdir -p /etc/containerd/certs.d/localhost:${registry_port} &&
        printf '[host.\"http://%s:5000\"]\n' '${registry_name}' > /etc/containerd/certs.d/localhost:${registry_port}/hosts.toml
    " || die "writing hosts.toml on node $node failed"
done

# Advertise the registry endpoint per the local-registry discovery KEP (https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry).
kubectl --context "$ctx" apply -f - > /dev/null <<EOF || die "applying local-registry-hosting configmap failed"
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${registry_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

node_ip="$(kubectl --context "$ctx" get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')" \
    || die "reading the node InternalIP of $cluster_name failed"
case "$node_ip" in
    ("" | *" "*) die "the kind node of $cluster_name needs exactly one InternalIP, got '$node_ip'";;
esac

# A for sslip.io is answered with the node IP, which routes to the
# gateway so the pm.127-0-0-1.sslip.io resolves within the cluster
# without hostaliases.
# For the host this resolved to 127.0.0.1 via sslip.io itself.
# AAAA is answered empty since the cluster has no IPv6.
coredns_sslip_block='sslip.io:53 {
    errors
    template IN A sslip.io {
        match "[.]sslip[.]io[.]$"
        answer "{{ .Name }} 60 IN A __NODE_IP__"
    }
    template IN AAAA sslip.io {
        rcode NOERROR
    }
}'
coredns_sslip_block="$(echo "$coredns_sslip_block" | sed "s/__NODE_IP__/$node_ip/")"

corefile="$(kubectl --context "$ctx" -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}')" \
    || die "reading coredns Corefile failed"

# A block of an earlier run is dropped rather than left in front of this one:
# CoreDNS refuses to serve a zone twice and the blocks answer different addresses.
stripped="$(echo "$corefile" | awk '
    /^sslip[.]io:53 \{/ {skip = 1; next}
    skip && /^\}/ {skip = 0; next}
    skip {next}
    {print}')"

patched="$(printf '%s\n%s' "$coredns_sslip_block" "$stripped")"
if test "$patched" = "$corefile"; then
    log "coredns already resolves sslip.io to $node_ip"
    exit 0
fi

log "making coredns resolve sslip.io to $node_ip"
tmp="$(mktemp -d)" || die "mktemp failed"
finish() { rm -rf "$tmp"; }
trap finish EXIT INT

printf '%s\n%s\n' "$coredns_sslip_block" "$stripped" > "$tmp/Corefile" || die "writing Corefile failed"
kubectl --context "$ctx" -n kube-system create configmap coredns \
    --from-file=Corefile="$tmp/Corefile" --dry-run=client -o yaml \
    | kubectl --context "$ctx" apply -f - || die "updating coredns configmap failed"
kubectl --context "$ctx" -n kube-system rollout restart deployment/coredns || die "restarting coredns failed"
kubectl --context "$ctx" -n kube-system rollout status deployment/coredns --timeout=180s || die "coredns did not become ready"
