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

# verify-go-version.sh – Verify that every go.mod in the repo has the minimum
# required Go version based on its dependencies.
#
# The "go" directive in go.mod should be the *minimum* Go version required to
# build the module, not the version that happens to be installed. This script
# ensures developers don't blindly bump the directive when upgrading Go.
#
# Usage: verify-go-version.sh [--fix]
#   --fix    Update go.mod files to use the correct minimum version

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

fix_mode=false
if [[ "${1:-}" == "--fix" ]]; then
  fix_mode=true
fi

# Find all go.mod files, excluding vendor directories
mapfile -t gomod_files < <(find . -name go.mod -type f ! -path '*/vendor/*' | sort)

if [[ ${#gomod_files[@]} -eq 0 ]]; then
  echo "No go.mod files found."
  exit 0
fi

echo "Checking ${#gomod_files[@]} go.mod file(s)..."
echo

errors=0

for gomod in "${gomod_files[@]}"; do
  module_dir=$(dirname "$gomod")
  module_name=$(grep "^module " "$gomod" | awk '{print $2}')
  declared_version=$(grep "^go " "$gomod" | awk '{print $2}')

  if [[ -z "$declared_version" ]]; then
    echo "WARNING: $gomod has no 'go' directive, skipping"
    continue
  fi

  # Get the maximum Go version required by all transitive dependencies.
  # GOWORK=off ensures we check this module in isolation, not influenced by
  # the workspace's go.work file.
  #
  # We exclude the current module itself from the list, as its declared version
  # would otherwise always be the maximum (making the check useless).
  golist_output=""
  golist_error=""
  golist_output=$(cd "$module_dir" && GOWORK=off go list -m -f '{{.Path}} {{.GoVersion}}' all 2>&1) || golist_error="$?"

  if [[ -n "$golist_error" ]]; then
    rel_path="${gomod#./}"
    # Check for specific error patterns that indicate missing go.sum entries
    if echo "$golist_output" | grep -q "missing go.sum entry"; then
      echo "WARNING: $rel_path has incomplete go.sum, run 'go mod tidy' first"
    else
      echo "WARNING: $rel_path failed go list: ${golist_output%%$'\n'*}"
    fi
    ((errors++)) || true
    continue
  fi

  # Exclude the current module and find the max version among dependencies
  # Use { grep ... || true; } to handle the case where grep filters out all lines
  max_dep_version=$(echo "$golist_output" | { grep -v "^$module_name " || true; } | awk '{print $2}' | sort -V | tail -1)

  if [[ -z "$max_dep_version" ]]; then
    # No dependencies with Go version (e.g., module with no deps)
    continue
  fi

  # Find which module requires the max version (for the error message)
  max_dep_module=$(echo "$golist_output" | { grep -v "^$module_name " || true; } | awk -v ver="$max_dep_version" '$2 == ver {print $1; exit}')
  if [[ -z "$max_dep_module" ]]; then
    max_dep_module="(unknown)"
  fi

  # Check if declared version is higher than the maximum required
  # A version is "too high" if declared > max_required
  if [[ $(printf '%s\n%s' "$max_dep_version" "$declared_version" | sort -V | tail -1) == "$declared_version" && "$declared_version" != "$max_dep_version" ]]; then
    ((errors++)) || true

    rel_path="${gomod#./}"
    echo "ERROR: $rel_path"
    echo "  Module:           $module_name"
    echo "  Declared version: go $declared_version"
    echo "  Required minimum: go $max_dep_version"
    echo "  Reason:           Highest dependency requirement is from $max_dep_module"
    echo "  Suggestion:       Change 'go $declared_version' to 'go $max_dep_version' in $rel_path"
    echo

    if $fix_mode; then
      # Use sed to update the go directive
      sed -i "s/^go $declared_version$/go $max_dep_version/" "$gomod"
      echo "  FIXED: Updated $rel_path to go $max_dep_version"
      echo
    fi
  fi
done

if [[ $errors -gt 0 ]]; then
  if $fix_mode; then
    echo "Fixed $errors go.mod file(s) with unnecessarily high Go versions."
    echo "Please review the changes and run 'task tidy' to make sure everything is clean."
  else
    echo "Found $errors go.mod file(s) with unnecessarily high Go versions."
    echo "The 'go' directive should reflect the minimum required version, not the installed version."
    echo
    echo "Run 'hack/verify-go-version.sh --fix' to automatically fix these issues."
    exit 1
  fi
fi

echo "All go.mod files have correct minimum Go versions."
