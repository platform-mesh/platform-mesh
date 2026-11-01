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

# Runs `helm dependency update` for every chart in a directory, dependencies
# first. Charts in this repository depend on each other through file://
# references (common, *-crds, keycloak-operator, ...), and Helm does not
# resolve those transitively: it copies the referenced directory as it is on
# disk. A chart two levels up (platform-mesh-prerequisites -> keycloak-operator
# -> common) therefore only gets a complete sub-chart if keycloak-operator's
# own dependencies were fetched before. Hence the ordering.
#
# Usage: resolve-dependencies.sh [<charts-dir>]   (default charts/)
#
# Third-party dependencies are pulled from their registries, so this needs
# network access. Nothing it writes (charts/*/charts/, Chart.lock) is tracked.
# `helm dependency update` refreshes every configured repository index first,
# per chart. Instead the few classic (http) repositories charts depend on are
# fetched once up front, and the updates run with --skip-refresh.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dir="${1:-$ROOT/charts}"

# shellcheck source=../release/tools.sh
source "$ROOT/hack/release/tools.sh"
require_tools helm yq

# name -> directory, and the file:// dependencies of each chart
declare -A path deps
for chart_yaml in "$dir"/*/Chart.yaml; do
  name="$("$YQ" -r '.name' "$chart_yaml")"
  path[$name]="$(dirname "$chart_yaml")"
  deps[$name]="$("$YQ" -r '.dependencies[]? | select(.repository | test("^file://")) | .name' "$chart_yaml" | tr '\n' ' ')"
done

# Classic chart repositories (OCI and file:// need no index).
for repo in $("$YQ" -r '.dependencies[]? | select(.repository | test("^https?://")) | .repository' "$dir"/*/Chart.yaml | sort -u); do
  "$HELM" repo add --force-update "dep-$(echo -n "$repo" | sha256sum | cut -c1-12)" "$repo" >/dev/null
done

declare -A done
resolve() {
  local name="$1" dep
  [[ -n "${done[$name]:-}" ]] && return
  done[$name]=in-progress
  for dep in ${deps[$name]}; do
    [[ -n "${path[$dep]:-}" ]] || { echo "$name: file:// dependency $dep is not in $dir" >&2; exit 1; }
    [[ "${done[$dep]:-}" == "in-progress" ]] && { echo "dependency cycle at $name -> $dep" >&2; exit 1; }
    resolve "$dep"
  done
  if "$YQ" -e '.dependencies' "${path[$name]}/Chart.yaml" >/dev/null 2>&1; then
    "$HELM" dependency update "${path[$name]}" --skip-refresh >/dev/null
  fi
  done[$name]=yes
}

for name in "${!path[@]}"; do
  resolve "$name"
done
