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

# Installs Platform Mesh into the kind cluster the way installation/README.md
# describes it (prerequisites chart in two passes, then the operator chart with
# the kind profile), waits until the platform serves the portal, and writes
# the kcp admin kubeconfig the Playwright tests use.
#
# Usage:
#   install.sh                  the snapshot: charts from this checkout, the
#                               OCM component from the local registry (ocm.sh)
#   install.sh --release [V]    the published release V (default: the latest,
#                               latest-release.sh): released charts and the
#                               component from ghcr.io, as a user installs it
#
# Both are `helm upgrade --install`, so running the snapshot variant over a
# release installation is the upgrade path: the operator chart switches the
# OCM Repository to the local registry and the Component to the snapshot, and
# the ocm-k8s-toolkit rolls every chart to it.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
require_tools helm kind

mode=snapshot; release=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --release) mode=release; if [[ -n "${2:-}" && "$2" != --* ]]; then release="$2"; shift; fi; shift ;;
    *) echo "usage: $0 [--release [version]]" >&2; exit 1 ;;
  esac
done

# Fewer CPU requests than the charts' defaults, so the stack fits a CI runner
# or a laptop.
prerequisites_values=(
  --set keycloak-operator.watchNamespaces="$E2E_NAMESPACE"
  --set keycloak-operator.resources.requests.cpu=100m
  --set flux2.helmController.resources.requests.cpu=50m
  --set flux2.kustomizeController.resources.requests.cpu=50m
  --set flux2.sourceController.resources.requests.cpu=50m
  --set flux2.notificationController.resources.requests.cpu=50m
  --set flux2.imageAutomationController.create=false
  --set flux2.imageReflectionController.create=false
  --set ocm-k8s-toolkit.resources.requests.cpu=50m
  --set etcd-druid.resources.requests.cpu=25m
)
traefik_values=(
  --set traefik.enabled=true
  --set traefik.service.type=NodePort
  --set traefik.service.spec.type=NodePort
  --set traefik.service.spec.clusterIP="$E2E_TRAEFIK_CLUSTER_IP"
  --set traefik.ports.websecure.exposedPort="$E2E_GATEWAY_PORT"
  --set traefik.ports.websecure.nodePort=31000
)
operator_values=(
  --set ocm.enabled=true
  --set ocm.profile=kind
  --set gateway.baseDomain="$E2E_BASE_DOMAIN"
  --set gateway.port="$E2E_GATEWAY_PORT"
  --set gateway.traefikClusterIP="$E2E_TRAEFIK_CLUSTER_IP"
  --set kcp.frontProxyPort="$E2E_GATEWAY_PORT"
  --set certificates.selfSigned.enabled=true
)

case "$mode" in
  snapshot)
    version="$(state version 'task e2e:ocm')"
    image_version="$(image_version)"
    prerequisites_chart=("$E2E_ROOT/charts/platform-mesh-prerequisites")
    operator_chart=("$E2E_ROOT/charts/platform-mesh-operator")
    # The chart in git has appVersion 0.0.0, so the operator image is named explicitly.
    operator_values+=(
      --set-string ocm.version="$version"
      --set ocm.repository="$(ocm_repo)"
      --set ocm.signatureName="$E2E_SIGNATURE"
      --set ocm.installSigningCertificate=false
      --set image.registry="$(registry_host)"
      --set image.repository="$IMAGE_REPO/platform-mesh-operator"
      --set image.tag="v$image_version"
    )
    log "resolving chart dependencies"
    "$E2E_ROOT/hack/charts/resolve-dependencies.sh" >/dev/null
    log "installing snapshot $version from $(ocm_repo)"
    ;;
  release)
    version="${release:-$("$E2E_ROOT/hack/e2e/latest-release.sh")}"
    prerequisites_chart=("$E2E_RELEASE_CHARTS/platform-mesh-prerequisites" --version "$version")
    operator_chart=("$E2E_RELEASE_CHARTS/platform-mesh-operator" --version "$version")
    operator_values+=(--set-string ocm.version="$version")
    log "installing release $version from $E2E_RELEASE_CHARTS"
    ;;
esac

helm_() { "$HELM" --kube-context "kind-$E2E_CLUSTER" "$@"; }

log "prerequisites, pass 1 (controllers and CRDs)"
helm_ upgrade --install platform-mesh-prerequisites "${prerequisites_chart[@]}" \
  --namespace "$E2E_NAMESPACE" --create-namespace "${prerequisites_values[@]}" --wait --timeout 15m >/dev/null
log "prerequisites, pass 2 (Traefik)"
helm_ upgrade platform-mesh-prerequisites "${prerequisites_chart[@]}" \
  --namespace "$E2E_NAMESPACE" --reset-values "${prerequisites_values[@]}" "${traefik_values[@]}" --wait --timeout 15m >/dev/null

log "platform-mesh-operator"
# No --wait: the chart applies the PlatformMesh resource; readiness is gated below.
helm_ upgrade --install platform-mesh-operator "${operator_chart[@]}" \
  --namespace "$E2E_NAMESPACE" "${operator_values[@]}" >/dev/null

if [[ "$mode" == snapshot ]]; then
  # The certificate the Component verifies the snapshot's signature with. It
  # replaces the chart's own secret (ocm.installSigningCertificate=false) and
  # is applied after helm, which would otherwise remove it as unmanaged.
  kube -n "$E2E_NAMESPACE" create secret generic ocm-signing-public-cert \
    --from-file="$E2E_SIGNATURE=$E2E_STATE/signing.crt" --dry-run=client -o yaml | kube apply -f - >/dev/null
fi

# ---------------------------------------------------------------------------
# Wait until the version is the one running
# ---------------------------------------------------------------------------
component_version() { [[ "$(kube -n "$E2E_NAMESPACE" get components.delivery.ocm.software platform-mesh -o jsonpath='{.status.component.version}' 2>/dev/null)" == "$1" ]]; }
# Flux records the deployed chart version in the release history (newer
# versions no longer fill lastAppliedRevision).
helmrelease_at() { local v; v="$(kube -n "$E2E_NAMESPACE" get helmrelease "$1" -o jsonpath='{.status.history[0].chartVersion}' 2>/dev/null)"; [[ "${v%%+*}" == "$2" ]]; }

wait_for 600 "OCM Component resolved $version" component_version "$version"
log "waiting for PlatformMesh to become ready (up to 30m)"
kube -n "$E2E_NAMESPACE" wait --for=condition=Ready platformmeshes/platform-mesh --timeout=30m >/dev/null
# Ready confirms the operator's subroutines; the charts built from this
# repository must have rolled to the version too (account-operator and portal
# stand in for all of them), and Flux must have finished with the portal.
wait_for 1200 "account-operator chart at $version" helmrelease_at account-operator "$version"
wait_for 1200 "portal chart at $version" helmrelease_at portal "$version"
kube -n "$E2E_NAMESPACE" wait --for=condition=Ready helmreleases/portal helmreleases/kubernetes-graphql-gateway --timeout=15m >/dev/null
kube -n "$E2E_NAMESPACE" rollout status deployment/portal --timeout=5m >/dev/null
kube -n "$E2E_NAMESPACE" rollout status deployment/kubernetes-graphql-gateway --timeout=5m >/dev/null

# The admin client certificate is rotated on every install; regenerate the
# kubeconfig the tests read (<repo>/.secret/kcp/admin.kubeconfig). The script
# uses kubectl's current context, so it gets a kubeconfig of its own.
"$KIND" export kubeconfig --name "$E2E_CLUSTER" --kubeconfig "$E2E_STATE/kubeconfig" >/dev/null
(cd "$E2E_ROOT" && KUBECONFIG="$E2E_STATE/kubeconfig" ./tests/create-kcp-admin-kubeconfig.sh >/dev/null)
log "Platform Mesh $version is ready at https://$E2E_BASE_DOMAIN:$E2E_GATEWAY_PORT/"
