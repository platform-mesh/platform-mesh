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

# Copies spec.permissionClaims and spec.identity of a deployed APIExport
# manifest (apis/static-assets/kcp or apis/static-assets/features) into the
# apigen-generated APIExport of a component (apis/static-assets/resources), so
# the generated export carries exactly what the platform deploys. apigen only
# rewrites spec.resources, so the copied fields survive regeneration; running
# this from `task generate` keeps them in step, and verify.yml fails when the
# result is not committed.
#
# usage: sync-export-claims.sh <generated-export.yaml> <deployed-export.yaml>
#
# YQ selects the yq binary (default: yq on PATH).

set -euo pipefail

if [ $# -ne 2 ]; then
    echo "usage: $0 <generated-export.yaml> <deployed-export.yaml>" >&2
    exit 2
fi
gen=$1
dep=$2
yq=${YQ:-yq}

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

# The deployed manifests are Go templates: drop the control lines (feature
# toggles) and quote the expressions so the file parses as YAML. The copied
# values keep the expression as a string.
sed -E -e '/^\s*\{\{-? *(if|else|end|range|with)\b.*\}\}\s*$/d' -e "s/([:[:space:]])(\{\{[^}]*\}\})/\1'\2'/g" "$dep" > "$tmp"

toplevel=$(git rev-parse --show-toplevel)
NOTE="Copied from ${dep#"$toplevel"/} by hack/sync-export-claims.sh (task generate). Do not edit here."
export NOTE

# Comment block at the top of the generated file (the license header).
header=$(awk '/^#/ || /^$/ {print; next} {exit}' "$gen")

body=$("$yq" eval-all '
  (select(fileIndex == 1).spec.permissionClaims) as $claims
  | (select(fileIndex == 1).spec.identity) as $identity
  | select(fileIndex == 0)
  | with(select($identity != null); .spec.identity = $identity)
  | with(select($identity == null); del(.spec.identity))
  | with(select($claims != null); .spec.permissionClaims = $claims | .spec.permissionClaims head_comment = strenv(NOTE))
  | with(select($claims == null); del(.spec.permissionClaims))
' "$gen" "$tmp" | sed '/^#/d')

printf '%s\n%s\n' "$header" "$body" > "$gen"
