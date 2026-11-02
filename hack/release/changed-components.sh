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

# Maps a list of changed files to the components and charts that need CI.
# Prints GitHub Actions output lines (key=<json array>):
#
#   components       components (including libraries) to verify/test
#   e2eComponents    subset with e2e tests
#   imageComponents  subset that produce an image
#   charts           chart names (directories under charts/) that changed
#
# Usage:
#   changed-components.sh "<newline separated changed files>"
#   changed-components.sh ""          # empty list means: everything
#
# A change to a library component (apis, golang-commons, subroutines), to
# go.work(.sum), to ocm/components.yaml or to anything under .github/ selects
# all components, since the others depend on them. A change to charts/common or a *-crds chart selects all charts,
# since other charts depend on them.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

changed="${1-}"
registry=ocm/components.yaml

all_components=$(yq '.components | keys | .[]' "$registry")
all_charts=$(for f in charts/*/Chart.yaml; do [ -f "$f" ] && basename "$(dirname "$f")"; done)

selected_components=""
selected_charts=""

if [ -z "$changed" ]; then
  selected_components="$all_components"
  selected_charts="$all_charts"
else
  # Paths that affect every Go component.
  shared_re='^(go\.work|go\.work\.sum|ocm/components\.yaml|\.github/|Taskfile\.yaml|tools/)'
  lib_paths=$(yq '.components | to_entries | map(select(.value.library == true)) | .[].value.path' "$registry")
  for p in $lib_paths; do shared_re="$shared_re|^$p/"; done

  if grep -qE "$shared_re" <<<"$changed"; then
    selected_components="$all_components"
  else
    for c in $all_components; do
      path=$(yq ".components.\"$c\".path" "$registry")
      if grep -qE "^$path/" <<<"$changed"; then
        selected_components+="$c"$'\n'
      fi
    done
  fi

  if grep -qE '^charts/(common|[a-z0-9-]+-crds)/|^charts/Taskfile\.yaml|^charts/\.docs-templates/' <<<"$changed"; then
    selected_charts="$all_charts"
  else
    selected_charts=$({ grep -E '^charts/[^/]+/' <<<"$changed" || true; } | cut -d/ -f2 | sort -u | while read -r c; do
      [ -f "charts/$c/Chart.yaml" ] && echo "$c" || true
    done)
  fi
fi

to_json() { { grep -v '^$' || true; } | sort -u | jq -R . | jq -s -c .; }

e2e=""
images=""
for c in $selected_components; do
  [ "$(yq ".components.\"$c\".e2e != false" "$registry")" = "true" ] && e2e+="$c"$'\n'
  [ "$(yq ".components.\"$c\".image // \"\"" "$registry")" != "" ] && images+="$c"$'\n'
done

echo "components=$(to_json <<<"$selected_components")"
echo "e2eComponents=$(to_json <<<"$e2e")"
echo "imageComponents=$(to_json <<<"$images")"
echo "charts=$(to_json <<<"$selected_charts")"
