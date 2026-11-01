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

# Prints the third-party version pins from ocm/versions.yaml as KEY=value
# lines, one per line, suitable for
#
#   source <(hack/release/export-versions.sh)        # in scripts (exports)
#   hack/release/export-versions.sh >> "$GITHUB_ENV"  # in workflows
#
# Usage: export-versions.sh [--export] [<versions-file>]
#   --export   prefix every line with "export " (for sourcing in scripts that
#              must pass the variables on to child processes such as ocm)

set -euo pipefail

prefix=""
if [[ "${1-}" == "--export" ]]; then
  prefix="export "
  shift
fi
file="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/ocm/versions.yaml}"

yq -r 'to_entries | .[] | select(.key | test("^[A-Z][A-Z0-9_]*$")) | .key + "=" + .value' "$file" \
  | sed "s/^/${prefix}/"
