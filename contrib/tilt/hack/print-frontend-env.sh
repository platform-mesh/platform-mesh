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

# Prints the environment for running the portal outside the cluster against
# the Tilt environment, in a form that is both a .env file and sourceable sh.
#
# The values are read from the running portal Deployment, so they always
# match what Tilt deployed. In-cluster file paths are swapped for their local
# copies under contrib/tilt/.secret, and in-cluster-only URLs are commented
# out. The browser calls the APIs cross-origin, so start Tilt with
# --frontend-cors (see installation/frontend-dev-cors-values.yaml).
#
# KIND_CLUSTER_NAME selects the cluster (default platform-mesh).

die() { echo "$@" >&2; exit 1; }

ctx="kind-${KIND_CLUSTER_NAME:-platform-mesh}"
ns="platform-mesh-system"
secret="$(git rev-parse --show-toplevel)/contrib/tilt/.secret"
tab="$(printf '\t')"

env="$(kubectl --context "$ctx" -n "$ns" get deployment portal \
    -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}{"\t"}{.value}{"\n"}{end}' 2>/dev/null)" \
    || die "reading the portal Deployment in $ctx failed; is Tilt up?"
test -n "$env" || die "the portal Deployment in $ctx has no environment"

echo "# Portal environment for a local run against $ctx."
echo "# Generated from the portal Deployment by contrib/tilt/hack/print-frontend-env.sh."
echo "$env" | while IFS="$tab" read -r name value; do
    case "$name" in
        # Describe the in-cluster container, not a local run.
        PORT | IMAGE_TAG | IMAGE_NAME | KUBECONFIG) continue ;;
        NODE_EXTRA_CA_CERTS) value="$secret/ca.crt" ;;
        KUBECONFIG_KCP) value="$secret/kcp-admin.kubeconfig" ;;
    esac
    test -n "$value" || continue
    case "$value" in
        *.svc.cluster.local*) printf "# in-cluster only, port-forward to use: %s='%s'\n" "$name" "$value" ;;
        *) printf "%s='%s'\n" "$name" "$value" ;;
    esac
done
