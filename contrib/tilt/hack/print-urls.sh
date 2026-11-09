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

# Prints the URLs of the Tilt environment.
#
# Hostnames are <x>.pm.127-0-0-1.sslip.io, as in tiltfiles/settings.Tiltfile:
# sslip.io answers the host loopback, where kind publishes the traefik
# NodePort. The deployed PlatformMesh records what is actually running and is
# preferred once Tilt has deployed it, so this also works before `tilt up`.
#
# KIND_CLUSTER_NAME selects the cluster (default platform-mesh), PORT the
# NodePort (default 31000, PORT in settings.Tiltfile) and TILT_PORT the
# Tilt UI port (default 10350).

ctx="kind-${KIND_CLUSTER_NAME:-platform-mesh}"

_pm_kubectl() {
    kubectl --context "$ctx" -n "${PLATFORM_NAMESPACE:-platform-mesh-system}" "$@"
}

base="$(_pm_kubectl get platformmesh platform-mesh -o jsonpath='{.spec.exposure.baseDomain}' 2> /dev/null)"
test -n "$base" || base="pm.127-0-0-1.sslip.io"

port="${PORT:-$(_pm_kubectl get platformmesh platform-mesh -o jsonpath='{.spec.exposure.port}' 2> /dev/null)}"
test -n "$port" || port=31000

base="$base:$port"
secret="$(git rev-parse --show-toplevel)/contrib/tilt/.secret"

cat <<EOF

Platform Mesh (available once Tilt has finished deploying):

  Portal          https://$base/
  Keycloak admin  https://$base/keycloak/admin/
  kcp API         https://kcp.api.$base
  Tilt UI         http://localhost:${TILT_PORT:-10350}/

  CA certificate  $secret/ca.crt
  kcp kubeconfig  $secret/kcp-admin.kubeconfig
  Environment     . contrib/tilt/env.sh

EOF
