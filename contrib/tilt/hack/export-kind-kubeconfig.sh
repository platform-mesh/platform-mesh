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

# Writes a kubeconfig holding only the kind cluster to contrib/tilt/.secret/kind.kubeconfig,
# read-write only for the user. Run after kind is up, in every path that starts
# the Tilt environment (tilt:up, tilt:ci).
#
# KIND_CLUSTER_NAME selects the cluster (default platform-mesh).

die() { echo "$@" >&2; exit 1; }

ctx="kind-${KIND_CLUSTER_NAME:-platform-mesh}"
secret="$(git rev-parse --show-toplevel)/contrib/tilt/.secret"
kind_kubeconfig="$secret/kind.kubeconfig"

(
    umask 077
    mkdir -p "$secret" || exit 1
    tmp="$(mktemp "$kind_kubeconfig.XXXXXX")" || exit 1
    kubectl config view --context "$ctx" --minify --flatten > "$tmp" \
        && mv "$tmp" "$kind_kubeconfig" \
        || { rm -f "$tmp"; exit 1; }
) || die "writing $kind_kubeconfig failed"
