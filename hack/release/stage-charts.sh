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

# Copies charts/ to a staging directory and writes the real versions into the
# copies. In git every chart has `version: 0.0.0` (and the charts of components
# built in this repository `appVersion: 0.0.0`); the version only exists at
# build time, so that a change can be cherry-picked between branches without
# carrying version information along (RFC 015).
#
#   version      -> every chart (and every file:// dependency pin)
#   appVersion   -> only charts listed with `chart:` in ocm/components.yaml,
#                   i.e. those whose image is built from this repository. The
#                   image tag is "v<image-version>" (image tags keep the v, as
#                   they always did); wrapper charts keep the upstream version
#                   they declare.
#
# Usage: stage-charts.sh <staging-dir> <version> [<image-version>]
#
# <image-version> defaults to <version>; a working-tree build that does not
# build images passes the release whose images it reuses.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

dest="${1:?usage: $0 <staging-dir> <version> [<image-version>]}"
version="${2:?usage: $0 <staging-dir> <version> [<image-version>]}"
image_version="${3:-$version}"
image_tag="v${image_version#v}"

source hack/release/tools.sh
require_tools yq

rm -rf "$dest"
mkdir -p "$dest"
cp -R charts/. "$dest/"
# Leftovers of local `helm dependency update` runs; resolved again below.
rm -rf "$dest"/*/charts "$dest"/*/Chart.lock "$dest"/*/*.tgz

inhouse="$("$YQ" '.components[] | select(has("chart")) | .chart' ocm/components.yaml | sed 's#^charts/##')"

for chart_yaml in "$dest"/*/Chart.yaml; do
  name="$(basename "$(dirname "$chart_yaml")")"
  export version image_tag
  "$YQ" -i '.version = strenv(version)' "$chart_yaml"
  if grep -qx "$name" <<<"$inhouse"; then
    "$YQ" -i '.appVersion = strenv(image_tag)' "$chart_yaml"
  fi
  if "$YQ" -e '.dependencies' "$chart_yaml" >/dev/null 2>&1; then
    "$YQ" -i '(.dependencies[] | select(.repository | test("^file://"))).version = strenv(version)' "$chart_yaml"
  fi
done

# file:// dependencies now point at the staged copies at $version. Resolve
# them dependencies-first (Helm copies file:// charts as they are on disk, so
# a sub-chart's own dependencies must be in place before it is copied).
"$ROOT/hack/charts/resolve-dependencies.sh" "$dest"

echo "$dest"
