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

# Wraps kcp's apigen so that APIResourceSchema names identify the API, not the
# commit that generated them.
#
# apigen names every schema it (re)generates `v<yymmdd>-<git HEAD>.<resource>.<group>`
# and only keeps a previous name when it finds an unchanged schema in --output-dir.
# The commit hash says nothing about the API and changes on every commit, so an
# unchanged schema that is regenerated gets a new name and the APIExport that
# references it churns with it.
#
# This wrapper runs apigen and then rewrites the names it stamped in this run to
# `v<yymmdd>-<hash>` where <hash> is the first 8 hex chars of the SHA-256 of the
# schema's spec (as canonical JSON). The same API always yields the same hash,
# so regenerating an unchanged schema is a no-op, and a changed API gets a new
# name as before. Names apigen reused from a previous run are left untouched.
#
# Usage: APIGEN_BIN=<apigen> YQ=<yq> apigen.sh <apigen args...>
# Wired up as the `APIGEN` var in tools/Taskfile.yaml; components call it as
# they would call apigen itself.

set -euo pipefail

: "${APIGEN_BIN:?set APIGEN_BIN to the apigen binary}"
: "${YQ:?set YQ to the yq binary}"

outputDir=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[$i]}" in
    --output-dir) outputDir="${args[$((i + 1))]}" ;;
    --output-dir=*) outputDir="${args[$i]#--output-dir=}" ;;
  esac
done
[[ -n "$outputDir" ]] || { echo "apigen.sh: --output-dir is required" >&2; exit 1; }

"$APIGEN_BIN" "$@"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi
}

# Exactly the prefix apigen stamped on everything it generated in this run.
today="$(date +%y%m%d)"
generated="v${today}-$(git rev-parse --short HEAD)"

for schema in "$outputDir"/apiresourceschema-*.yaml; do
  [[ -f "$schema" ]] || continue
  name="$("$YQ" '.metadata.name' "$schema")"
  [[ "${name%%.*}" == "$generated" ]] || continue # reused from a previous run

  hash="$("$YQ" -o=json -I=0 '.spec' "$schema" | sha256 | cut -c1-8)"
  new="v${today}-${hash}.${name#*.}"
  [[ "$new" != "$name" ]] || continue

  echo ">> apigen.sh: ${name} -> ${new}"
  # The schema itself and every APIExport referencing it live in outputDir.
  for manifest in "$outputDir"/*.yaml; do
    sed "s/${name//./\\.}/${new}/g" "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
  done
done
