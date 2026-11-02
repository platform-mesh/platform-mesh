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

# Sets up the HTTPBin provider workspace with its APIExport.
#
# - waits for root:providers and the provider WorkspaceType
# - creates workspace root:providers:httpbin-provider
# - applies manifests/httpbin-provider into it
#
# Runs before the PlatformMesh is Ready: its extraDefaultAPIBindings reference
# this export, so platform-mesh-operator cannot finish the kcp setup until the
# export exists.
#
# KCP_URL selects the kcp front proxy. Needs .secret/kcp-admin.kubeconfig.

die() { echo "$@" >&2; exit 1; }

test -n "$KCP_URL" || die "KCP_URL is not set"

hack="$(cd "$(dirname "$0")" && pwd)" || die "resolving script directory failed"
cd "$hack/.." || die "changing to $hack/.. failed"

workspace=root:providers:httpbin-provider
admin_kubeconfig=.secret/kcp-admin.kubeconfig
kcp() {
    ws="$1"
    shift
    kubectl --kubeconfig "$admin_kubeconfig" --server "$KCP_URL/clusters/$ws" "$@"
}

# --for=jsonpath errors out on a missing object instead of waiting for it.
kcp root wait workspace/providers --for=create --timeout=15m \
    || die "workspace root:providers was not created"
kcp root wait workspace/providers --for=jsonpath='{.status.phase}'=Ready --timeout=15m \
    || die "workspace root:providers did not become Ready"
kcp root wait workspacetype/provider --for=create --timeout=15m \
    || die "WorkspaceType root:provider was not created"

# Admission resolves the type from an informer that lags the create slightly.
for _ in $(seq 30); do
    kcp root:providers apply -f manifests/httpbin-workspace.yaml && break
    sleep 2
done
kcp root:providers get workspace httpbin-provider > /dev/null \
    || die "creating workspace $workspace failed"
kcp root:providers wait workspace/httpbin-provider --for=jsonpath='{.status.phase}'=Ready --timeout=5m \
    || die "workspace $workspace did not become Ready"

kcp "$workspace" apply -k manifests/httpbin-provider \
    || die "applying manifests/httpbin-provider failed"
