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

# The kind cluster Platform Mesh is installed into (hack/e2e/kind-config.yaml),
# with containerd on every node configured to pull from the plain-HTTP
# registry of registry.sh. The kubeconfig context is kind-<E2E_CLUSTER>.
#
# Usage: kind.sh up|down

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
require_tools kind

case "${1:-}" in
  up)
    registry_host="$(registry_host)"
    if "$KIND" get clusters 2>/dev/null | grep -qx "$E2E_CLUSTER"; then
      log "kind cluster $E2E_CLUSTER exists"
    else
      log "creating kind cluster $E2E_CLUSTER ($E2E_KIND_IMAGE)"
      "$KIND" create cluster --name "$E2E_CLUSTER" --image "$E2E_KIND_IMAGE" --config "$E2E_ROOT/hack/e2e/kind-config.yaml"
    fi
    # containerd hosts.toml: the registry speaks HTTP. kind enables the
    # certs.d config_path by default, so no containerd config patch is needed.
    for node in $("$KIND" get nodes --name "$E2E_CLUSTER"); do
      docker exec "$node" mkdir -p "/etc/containerd/certs.d/$registry_host"
      docker exec -i "$node" sh -c "cat > /etc/containerd/certs.d/$registry_host/hosts.toml" <<TOML
server = "http://$registry_host"

[host."http://$registry_host"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
TOML
    done
    log "nodes pull from http://$registry_host"
    kube cluster-info >/dev/null
    ;;
  down)
    "$KIND" delete cluster --name "$E2E_CLUSTER"
    ;;
  *) echo "usage: $0 up|down" >&2; exit 1 ;;
esac
