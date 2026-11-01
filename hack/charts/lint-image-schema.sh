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

# lint-image-schema.sh — enforce the common image schema (PR #1980) in first-party charts.
#
# Why a source-level check (and not helm lint): the failure mode that breaks air-gap is a
# HARDCODED image literal in a template (e.g. `image: busybox:1.37`). helm lint validates
# values (a literal has no value to validate) and its rules are not pluggable, so it cannot
# see this. `values.schema.json` and helm-unittest work on values / rendered output and are
# equally blind to it. Hence a check over template SOURCE.
#
# Rule: every scalar `image:` value and every `RELATED_IMAGE_*` env value in a first-party
# chart template must render through the `common.image` helper, which exposes the separate
# registry/repository paths OCM needs to localize. Multiline `image:` blocks (structured CR /
# subchart config) are out of scope.
#
# Opt out for a justified exception by adding a trailing comment on the line:
#   image: "some/third-party:tag"  # image-schema:allow <reason, e.g. link to issue>
#
# Usage: lint-image-schema.sh [CHARTS_DIR]   (default: <repo>/charts)

set -euo pipefail

charts_dir="${1:-}"
if [ -z "$charts_dir" ]; then
  charts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/charts"
fi

if [ ! -d "$charts_dir" ]; then
  echo "lint-image-schema: charts dir not found: $charts_dir" >&2
  exit 2
fi

# Collect every file in scope first, then lint them in a single awk run.
# Scope: each chart's own templates/ — never vendored subcharts (charts/*/charts/),
# never the helper's home chart (common).
files=()
while IFS= read -r file; do
  files+=("$file")
done < <(
  find "$charts_dir" -mindepth 1 -maxdepth 1 -type d ! -name common | sort | while IFS= read -r chart_dir; do
    [ -d "$chart_dir/templates" ] || continue
    find "$chart_dir/templates" -name '*.yaml' -type f | sort
  done
)

violations=""
if [ "${#files[@]}" -gt 0 ]; then
  violations="$(
    awk '
      FNR == 1 { prev = "" }   # never carry line state across file boundaries
      # explicit, reviewed opt-out on the line
      /image-schema:allow/ { prev = $0; next }
      # scalar "image:" with a value on the same line (skip empty map/list)
      /^[[:space:]]*image:[[:space:]]*[^[:space:]#]/ &&
        $0 !~ /image:[[:space:]]*(\{\}|\[\])[[:space:]]*$/ {
        if ($0 !~ /common\.image/) printf "%s:%d:%s\n", FILENAME, FNR, $0
      }
      # a RELATED_IMAGE_* env value must also use common.image
      (prev ~ /name:[[:space:]]*RELATED_IMAGE/) && /^[[:space:]]*value:/ {
        if ($0 !~ /common\.image/) printf "%s:%d:%s\n", FILENAME, FNR, $0
      }
      { prev = $0 }
    ' "${files[@]}"
  )"
fi

if [ -n "$violations" ]; then
  echo "$violations"
  echo ""
  echo "ERROR: the image references above do not use the common.image helper."
  echo "First-party chart images must render via common.image (registry/repository/tag/digest)"
  echo "so OCM can localize them for air-gap (#2024, PR #1980)."
  echo "Fix: split the value schema and use '{{ include \"common.image\" ... }}'."
  echo "Justified exception: add a trailing '# image-schema:allow <reason>' to the line."
  exit 1
fi

echo "image-schema lint: OK (all first-party chart images use common.image)"
