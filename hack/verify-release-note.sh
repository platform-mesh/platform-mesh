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

# verify-release-note.sh – Verify that a pull request description contains a
# non-empty ```release-note block. The block holds either NONE (no user-facing
# change) or the note itself.
#
# Usage: verify-release-note.sh [PR]
#   The description is read from $PR_BODY if set, otherwise from the given PR
#   number (or the PR of the current branch) via `gh pr view`.

set -euo pipefail

if [[ -n "${PR_BODY+x}" ]]; then
  body="$PR_BODY"
else
  body="$(gh pr view ${1:+"$1"} --json body --jq .body)"
fi

# Contents of the first ```release-note block, CRs and blank lines dropped.
note="$(printf '%s\n' "$body" | tr -d '\r' | awk '
  /^[[:space:]]*```release-note[[:space:]]*$/ { inside = 1; found = 1; next }
  inside && /^[[:space:]]*```[[:space:]]*$/   { exit }
  inside && NF                                { print }
  END { if (!found) exit 2 }
')" || {
  echo "ERROR: no \`\`\`release-note block found in the PR description." >&2
  echo "Add one containing NONE, or a short note describing the user-facing change." >&2
  exit 1
}

if [[ -z "$note" ]]; then
  echo "ERROR: the \`\`\`release-note block is empty. Write NONE or a release note." >&2
  exit 1
fi

if [[ "$(echo "$note" | tr -d '[:space:]')" == "NONE" ]]; then
  echo "Release note: NONE"
else
  printf 'Release note:\n%s\n' "$note"
fi
