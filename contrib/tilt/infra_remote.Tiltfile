# Copyright 2026 The Platform Mesh Authors.
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

# infra_remote.Tiltfile — infrastructure that requires fetching remote charts
# and Tilt extensions. Split out of the main Tiltfile so `TILT_NO_INFRA=1` can
# render the local manifests + kcp CRs entirely offline (the ext:// loads below
# fetch at eval time).

load('ext://cert_manager', 'deploy_cert_manager')
load('ext://helm_remote', 'helm_remote')

# cert-manager — issues the self-signed CA the kcp shards use.
deploy_cert_manager(version='v1.19.2')

# Envoy Gateway controller — programs the `platform-mesh` Gateway (Gateway CR in
# manifests/gateway.yaml). Production uses traefik; envoy here keeps parity with
# the kcp reference and is enough to prove the routing.
namespace_yaml = 'apiVersion: v1\nkind: Namespace\nmetadata:\n  name: envoy-gateway-system\n'
k8s_yaml(blob(namespace_yaml))
helm_remote(
    'gateway-helm',
    repo_url='oci://registry-1.docker.io/envoyproxy',
    repo_name='gateway-helm',
    release_name='envoy',
    namespace='envoy-gateway-system',
    version='v1.7.0',
)

# kcp-operator — reconciles the RootShard/Shard/FrontProxy CRs that deploy_kcp
# emits. Pinned to a released version.
#
# The operator's own config/manager kustomization pins `newTag: e2e` — a
# floating CI tag, not a release (`:e2e` is not even a stable image). We pull the
# base at the release ref and override the image tag to the same release via a
# generated overlay, so we run a reproducible `:vX.Y.Z` image. `kubectl -k`
# resolves the remote base natively; Tilt's builtin kustomize() does not fetch
# remote URLs. Override the version with KCP_OPERATOR_VERSION.
KCP_OPERATOR_VERSION = os.getenv('KCP_OPERATOR_VERSION', 'v0.8.2')
local_resource(
    'kcp-operator',
    cmd='''set -eo pipefail
tmp=$(mktemp -d)
cat > "$tmp/kustomization.yaml" <<EOF
resources:
  - https://github.com/kcp-dev/kcp-operator/config/default?ref={v}
images:
  - name: ghcr.io/kcp-dev/kcp-operator
    newTag: {v}
EOF
kubectl apply --server-side -k "$tmp"
rm -rf "$tmp"'''.format(v=KCP_OPERATOR_VERSION),
    labels=['infra'],
    allow_parallel=True,
)

# Ingress port-forward to the gateway so root.kcp.localhost:8443 is reachable
# from the host. Mirrors the kcp reference port-forward loop.
local_resource(
    'ingress',
    serve_cmd='kubectl -n envoy-gateway-system wait gateway/platform-mesh --for=condition=Programmed --timeout=5m 2>/dev/null; ' +
              'while true; do svc=$(kubectl -n envoy-gateway-system get svc -l gateway.envoyproxy.io/owning-gateway-name=platform-mesh -o jsonpath="{.items[0].metadata.name}" 2>/dev/null); ' +
              '[ -n "$svc" ] && kubectl -n envoy-gateway-system port-forward "svc/$svc" 8443:8443 || true; sleep 2; done',
    labels=['infra'],
    allow_parallel=True,
)
