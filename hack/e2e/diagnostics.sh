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

# Dumps what one needs to see why an installation did not come up: the
# PlatformMesh resource, the OCM and Flux objects, pods, events, node and the
# logs of every pod in the platform namespace. CI uploads the directory when
# a job fails.
#
# Usage: diagnostics.sh [<dir>]   (default $E2E_STATE/diagnostics)

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

dir="${1:-$E2E_STATE/diagnostics}"
mkdir -p "$dir"
ns="$E2E_NAMESPACE"
log "writing diagnostics to $dir"
kube get platformmesh -n "$ns" -o yaml > "$dir/platformmesh.yaml" 2>&1 || true
kube describe platformmesh platform-mesh -n "$ns" > "$dir/platformmesh-describe.txt" 2>&1 || true
kube get component,resources.delivery.ocm.software,repositories.delivery.ocm.software -n "$ns" -o yaml > "$dir/ocm.yaml" 2>&1 || true
kube get helmreleases,ocirepositories,helmrepositories -A -o wide > "$dir/flux.txt" 2>&1 || true
kube get helmreleases -A -o yaml > "$dir/helmreleases.yaml" 2>&1 || true
kube get pods -A -o wide > "$dir/pods.txt" 2>&1 || true
kube describe node > "$dir/node-describe.txt" 2>&1 || true
kube get events -A --sort-by=.lastTimestamp > "$dir/events.txt" 2>&1 || true
for p in $(kube get pods -n "$ns" -o name 2>/dev/null); do
  safe="${p//\//_}"
  kube logs -n "$ns" "$p" --all-containers --tail=500 > "$dir/${ns}_${safe}.log" 2>&1 || true
done
for p in $(kube get pods -n ocm-system -o name 2>/dev/null) $(kube get pods -n flux-system -o name 2>/dev/null); do
  safe="${p//\//_}"
  kube logs "$p" --all-containers --tail=300 -n "$(kube get "$p" -A -o jsonpath='{.metadata.namespace}' 2>/dev/null || echo default)" > "$dir/controllers_${safe}.log" 2>&1 || true
done
