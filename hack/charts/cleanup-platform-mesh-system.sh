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

set -euo pipefail

namespace=platform-mesh-system
timeout=${CLEANUP_TIMEOUT:-15m}
confirm=false

usage() {
  cat <<'EOF'
Usage: cleanup-platform-mesh-system.sh [--yes]

Deletes platform-mesh-system and every namespaced resource in it. Managed
HelmReleases are uninstalled first, with the infra HelmRelease removed last.

Options:
  --yes     Skip the interactive confirmation.
  -h, --help
            Show this help.

Environment:
  CLEANUP_TIMEOUT  Timeout used for each deletion wait (default: 15m).
  KUBECONFIG       Kubeconfig used by kubectl and Helm.
EOF
}

while (($# > 0)); do
  case "$1" in
    --yes)
      confirm=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

for command in kubectl helm; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "Required command not found: ${command}" >&2
    exit 1
  fi
done

context=$(kubectl config current-context)

echo "Kubernetes context: ${context}"
echo "Namespace to delete: ${namespace}"
echo
echo "WARNING: This permanently deletes the namespace and all resources in it."

if [[ "${confirm}" != true ]]; then
  read -r -p "Type '${namespace}' to continue: " response
  if [[ "${response}" != "${namespace}" ]]; then
    echo "Cleanup cancelled."
    exit 1
  fi
fi

if ! kubectl get namespace "${namespace}" >/dev/null 2>&1; then
  echo "Namespace ${namespace} does not exist; nothing to do."
  exit 0
fi

# Stop PMO before deleting its generated HelmReleases, otherwise reconciliation
# can recreate them while cleanup is in progress.
if kubectl --namespace "${namespace}" get deployment platform-mesh-operator >/dev/null 2>&1; then
  echo "Scaling Platform Mesh Operator down..."
  kubectl --namespace "${namespace}" scale deployment platform-mesh-operator --replicas=0
  kubectl --namespace "${namespace}" rollout status deployment platform-mesh-operator --timeout="${timeout}"
fi

delete_helmrelease() {
  local release=$1

  echo "Deleting HelmRelease ${release}..."
  kubectl --namespace "${namespace}" delete helmrelease "${release}" --wait=false
  kubectl --namespace "${namespace}" wait \
    --for=delete \
    "helmrelease/${release}" \
    --timeout="${timeout}"
}

force_delete_helmrelease() {
  local release=$1

  echo "Removing finalizers from HelmRelease ${release} because its APIs are already gone..."
  kubectl --namespace "${namespace}" delete helmrelease "${release}" \
    --ignore-not-found \
    --wait=false
  kubectl --namespace "${namespace}" patch helmrelease "${release}" \
    --type=merge \
    --patch '{"metadata":{"finalizers":[]}}' \
    2>/dev/null || true
  kubectl --namespace "${namespace}" wait \
    --for=delete \
    "helmrelease/${release}" \
    --timeout="${timeout}"
}

if kubectl get customresourcedefinition helmreleases.helm.toolkit.fluxcd.io >/dev/null 2>&1; then
  mapfile -t helmreleases < <(
    kubectl --namespace "${namespace}" get helmrelease \
      --output jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'
  )

  for release in "${helmreleases[@]}"; do
    [[ -n "${release}" ]] || continue
    case "${release}" in
      infra|kubernetes-graphql-gateway)
        continue
        ;;
    esac

    # An interrupted cleanup may already have removed the ClusterAccess CRD
    # with kubernetes-graphql-gateway. Helm cannot then decode the
    # ClusterAccess stored in the virtual-workspaces release manifest.
    if [[ "${release}" == virtual-workspaces ]] &&
      ! kubectl get customresourcedefinition clusteraccesses.gateway.platform-mesh.io >/dev/null 2>&1; then
      force_delete_helmrelease "${release}"
    else
      delete_helmrelease "${release}"
    fi
  done

  # virtual-workspaces creates ClusterAccess objects whose finalizers are
  # handled by kubernetes-graphql-gateway. Keep the gateway running until all
  # other application releases, including virtual-workspaces, are gone.
  if kubectl --namespace "${namespace}" get helmrelease kubernetes-graphql-gateway >/dev/null 2>&1; then
    delete_helmrelease kubernetes-graphql-gateway
  fi

  # infra creates KCP, databases, Keycloak, and other dependencies required by
  # the remaining releases, so it must be uninstalled only after all of them.
  if kubectl --namespace "${namespace}" get helmrelease infra >/dev/null 2>&1; then
    delete_helmrelease infra
  fi
fi

delete_controller_resources() {
  local resource_type=$1
  local resource_label=$2
  local -a resources=()

  if ! kubectl get customresourcedefinition "${resource_type}" >/dev/null 2>&1; then
    return
  fi

  mapfile -t resources < <(
    kubectl --namespace "${namespace}" get "${resource_type}" \
      --output name \
      --ignore-not-found
  )
  if ((${#resources[@]} == 0)); then
    return
  fi

  echo "Deleting ${resource_label} resources..."
  kubectl --namespace "${namespace}" delete "${resources[@]}" --wait=false
  for resource in "${resources[@]}"; do
    kubectl --namespace "${namespace}" wait \
      --for=delete \
      "${resource}" \
      --timeout="${timeout}"
  done
}

# These resources have controller-specific finalizers. Delete them while the
# Flux source-controller and OCM controller from prerequisites are still alive.
delete_controller_resources ocirepositories.source.toolkit.fluxcd.io OCIRepository
delete_controller_resources resources.delivery.ocm.software "OCM Resource"
delete_controller_resources components.delivery.ocm.software "OCM Component"
delete_controller_resources repositories.delivery.ocm.software "OCM Repository"

# PMO is stopped, so remove its finalizers after all managed releases have been
# uninstalled. This prevents the PlatformMesh resource from blocking teardown.
mapfile -t platform_meshes < <(
  kubectl --namespace "${namespace}" get platformmesh \
    --output name \
    --ignore-not-found 2>/dev/null || true
)
for platform_mesh in "${platform_meshes[@]}"; do
  [[ -n "${platform_mesh}" ]] || continue
  echo "Removing finalizers from ${platform_mesh}..."
  kubectl --namespace "${namespace}" patch "${platform_mesh}" \
    --type=merge \
    --patch '{"metadata":{"finalizers":[]}}'
  kubectl --namespace "${namespace}" delete "${platform_mesh}" --ignore-not-found
done

uninstall_release() {
  local release=$1

  if helm status "${release}" --namespace "${namespace}" >/dev/null 2>&1; then
    echo "Uninstalling Helm release ${release}..."
    helm uninstall "${release}" --namespace "${namespace}" --timeout "${timeout}"
  fi
}

uninstall_release platform-mesh-operator
uninstall_release platform-mesh-prerequisites

echo "Deleting namespace ${namespace}..."
kubectl delete namespace "${namespace}" --wait=false
kubectl wait --for=delete "namespace/${namespace}" --timeout="${timeout}"

echo "Cleanup complete."
