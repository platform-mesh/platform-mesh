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

# Prepares the kind cluster the Tilt environment deploys into: checks the
# tools Tilt needs and runs hack/kind.sh up (create if absent, export the
# kubeconfig, make CoreDNS answer *.sslip.io itself).
#
# KIND and KUBECTL select the binaries, KIND_CLUSTER_NAME the cluster
# (default platform-mesh), TILT the tilt binary to check for.

set -euo pipefail

die() { echo "$@" >&2; exit 1; }
installed() { command -v "$1" > /dev/null 2>&1; }

for tool in "${KIND:-kind}" "${KUBECTL:-kubectl}" helm "${TILT:-tilt}"; do
    installed "$tool" || die "$tool not found"
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec "$root/hack/kind.sh" up --name "${KIND_CLUSTER_NAME:-platform-mesh}" "$@"
