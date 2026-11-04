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
# Hostnames are derived like tiltfiles/settings.Tiltfile does: the kind
# node's InternalIP, dashed, under sslip.io, on the traefik NodePort.
#
# KIND_CLUSTER_NAME selects the cluster (default platform-mesh), PORT the
# NodePort (default 31000, PORT in settings.Tiltfile) and TILT_PORT the
# Tilt UI port (default 10350).

die() { echo "$@" >&2; exit 1; }

ctx="kind-${KIND_CLUSTER_NAME:-platform-mesh}"
port="${PORT:-31000}"

ip="$(kubectl --context "$ctx" get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')" \
    || die "reading the node InternalIP of $ctx failed"
test -n "$ip" || die "node of $ctx has no InternalIP"

base="pm.$(echo "$ip" | tr . -).sslip.io:$port"
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
