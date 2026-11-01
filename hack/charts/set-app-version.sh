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

# Pins the image of a component from another repository (portal, iam-ui, …)
# by setting appVersion in charts/<chart>/Chart.yaml. Charts of components
# built here (listed with `chart:` in ocm/components.yaml) are refused; their
# appVersion is the Platform Mesh version (docs/release/process.md).
#
# Usage: set-app-version.sh <chart> <appVersion>
#
# Exit codes:
#   0  appVersion set, or already at the requested value (no-op)
#   1  unknown or in-house chart, malformed version, or downgrade

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=../release/tools.sh
source hack/release/tools.sh
require_tools yq

[ "$#" -eq 2 ] || { echo "Usage: $0 <chart> <appVersion>" >&2; exit 1; }
chart="$1"
new="$2"

if ! [[ "$chart" =~ ^[a-zA-Z0-9_-]+$ ]]; then
  echo "::error::invalid chart name: $chart" >&2
  exit 1
fi
chart_yaml="charts/$chart/Chart.yaml"
if [ ! -f "$chart_yaml" ]; then
  echo "::error::$chart_yaml not found" >&2
  exit 1
fi
if [ -n "$(chart="$chart" "$YQ" '.components[] | select(.chart == "charts/" + strenv(chart))' ocm/components.yaml)" ]; then
  echo "::error::$chart is built in this repository; its appVersion is the Platform Mesh version" >&2
  exit 1
fi
if ! [[ "$new" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  echo "::error::invalid appVersion: $new" >&2
  exit 1
fi

old="$("$YQ" -r '.appVersion' "$chart_yaml")"
if [ "${old#v}" = "${new#v}" ]; then
  echo "appVersion of $chart already $old; nothing to do"
  exit 0
fi
# Compare MAJOR.MINOR.PATCH only; the same triplet with a different
# pre-release suffix (v1.2.3-rc.1 -> v1.2.3) is a legitimate change.
old_triplet="${old#v}"; old_triplet="${old_triplet%%[-+]*}"
new_triplet="${new#v}"; new_triplet="${new_triplet%%[-+]*}"
if [ "$old_triplet" != "$new_triplet" ] \
  && [ "$(printf '%s\n%s\n' "$old_triplet" "$new_triplet" | sort -V | tail -n1)" = "$old_triplet" ]; then
  echo "::error::appVersion downgrade refused: $old -> $new" >&2
  exit 1
fi

new="$new" "$YQ" -i '.appVersion = strenv(new)' "$chart_yaml"
echo "$chart_yaml: appVersion $old -> $new"
