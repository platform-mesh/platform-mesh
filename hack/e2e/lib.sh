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

# Shared settings and helpers of the end-to-end flow in hack/e2e/ (see the
# README there). Source it; every script does:
#
#   source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
#
# State the steps hand to each other (registry address, build number, image
# digests, signing key) lives in $E2E_STATE (.e2e/ at the repository root,
# not tracked). `task e2e:down` removes it.

set -euo pipefail

E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_STATE="${E2E_STATE:-$E2E_ROOT/.e2e}"

# kind cluster and the registry container next to it on kind's docker network.
E2E_CLUSTER="${E2E_CLUSTER:-platform-mesh}"
E2E_KIND_IMAGE="${E2E_KIND_IMAGE:-kindest/node:v1.35.1}"
E2E_NETWORK="${E2E_NETWORK:-kind}"
# Subnets for the network when hack/e2e creates it (a fresh CI runner); an
# existing network, e.g. one kind created, is used as it is.
E2E_NETWORK_SUBNET="${E2E_NETWORK_SUBNET:-172.18.0.0/16}"
E2E_NETWORK_SUBNET6="${E2E_NETWORK_SUBNET6:-fc00:f853:ccd:e793::/64}"
E2E_REGISTRY_NAME="${E2E_REGISTRY_NAME:-platform-mesh-e2e-registry}"
E2E_REGISTRY_IMAGE="${E2E_REGISTRY_IMAGE:-registry:2.8.3}"
# The registry is also published on the loopback interface: `docker push` goes
# there, since Docker treats 127.0.0.0/8 as insecure without daemon config.
E2E_REGISTRY_HOST_PORT="${E2E_REGISTRY_HOST_PORT:-5001}"

# OCM: where the out-of-repo components (portal, iam-ui, ...) are copied from,
# and the name of the throwaway signature the snapshot is signed with.
E2E_MIRROR_FROM="${E2E_MIRROR_FROM:-ghcr.io/platform-mesh}"
E2E_SIGNATURE="${E2E_SIGNATURE:-platform-mesh-e2e}"
E2E_REPO_URL="${E2E_REPO_URL:-https://github.com/platform-mesh/platform-mesh}"
E2E_COMPONENT="github.com/platform-mesh/platform-mesh"

# Installation (installation/README.md, kind variant).
E2E_NAMESPACE="platform-mesh-system"
E2E_BASE_DOMAIN="portal.localhost"
E2E_GATEWAY_PORT="8443"
E2E_TRAEFIK_CLUSTER_IP="10.96.188.4"
E2E_RELEASE_CHARTS="oci://ghcr.io/platform-mesh/helm-charts"

source "$E2E_ROOT/hack/release/tools.sh"

log() { echo "[$(date '+%H:%M:%S')] e2e: $*" >&2; }
die() { echo "error: $*" >&2; exit 1; }

# state <name>: content of a state file, or an error naming the step that writes it.
state() {
  [[ -f "$E2E_STATE/$1" ]] || die "$E2E_STATE/$1 missing: run ${2:-the earlier steps} first (hack/e2e/README.md)"
  cat "$E2E_STATE/$1"
}
save_state() { mkdir -p "$E2E_STATE"; printf '%s\n' "$2" > "$E2E_STATE/$1"; }

# Registry addresses. The in-cluster address (the container's IP on the kind
# network) is the one recorded in images and component descriptors, with
# http:// so that the ocm CLI, the ocm-k8s-toolkit and the operator know the
# registry speaks plain HTTP. E2E_REGISTRY_URL overrides the detected address.
registry_url() { echo "${E2E_REGISTRY_URL:-$(state registry.url 'task e2e:registry')}"; }   # http://172.18.0.5:5000
registry_host() { local u; u="$(registry_url)"; echo "${u#http://}"; }                        # 172.18.0.5:5000
ocm_repo() { echo "$(registry_url)/platform-mesh"; }
chart_repo() { echo "$(registry_url)/platform-mesh/helm-charts"; }
push_registry() { echo "127.0.0.1:$E2E_REGISTRY_HOST_PORT"; }
# Images: <registry>/platform-mesh/platform-mesh/<component>:v<version>, as in the release.
IMAGE_REPO="platform-mesh/platform-mesh"

image_components() { require_tools yq; "$YQ" '.components | to_entries[] | select(.value.image) | .key' "$E2E_ROOT/ocm/components.yaml"; }

# Versions of this build (hack/e2e/version.sh).
image_version() { "$E2E_ROOT/hack/e2e/version.sh" --image; }
snapshot_version() { "$E2E_ROOT/hack/e2e/version.sh"; }

kube() { kubectl --context "kind-$E2E_CLUSTER" "$@"; }

# wait_for <timeout-seconds> <description> <command...>: poll until the command succeeds.
wait_for() {
  local timeout="$1" what="$2" start; shift 2
  start=$(date +%s)
  until "$@" >/dev/null 2>&1; do
    if (( $(date +%s) - start > timeout )); then
      die "timed out after ${timeout}s waiting for $what"
    fi
    sleep 10
  done
  log "$what"
}
