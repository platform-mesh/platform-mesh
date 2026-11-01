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

# Self-test for lint-image-schema.sh. Builds fixture charts covering each case and asserts
# the linter's exit code. Run: hack/lint-image-schema.test.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LINTER="$SCRIPT_DIR/lint-image-schema.sh"
fails=0

# make_chart <root> <name> <template-body>
make_chart() {
  local root="$1" name="$2" body="$3"
  mkdir -p "$root/$name/templates"
  printf '%s\n' "$body" > "$root/$name/templates/deployment.yaml"
}

# assert <description> <expected-exit> <charts-dir>
assert() {
  local desc="$1" want="$2" dir="$3"
  "$LINTER" "$dir" >/dev/null 2>&1
  local got=$?
  if [ "$got" -eq "$want" ]; then
    echo "PASS: $desc (exit $got)"
  else
    echo "FAIL: $desc (want exit $want, got $got)"
    fails=$((fails + 1))
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# 1. good — uses common.image → OK (exit 0)
good="$tmp/good"
make_chart "$good" "svc" '        image: {{ include "common.image" . }}'
assert "common.image passes" 0 "$good"

# 2. hardcoded literal → FAIL (exit 1)  [the historical busybox/kubectl bug]
lit="$tmp/lit"
make_chart "$lit" "svc" '        image: busybox:1.37'
assert "hardcoded literal fails" 1 "$lit"

# 3. non-common concatenation → FAIL (exit 1)
conc="$tmp/conc"
make_chart "$conc" "svc" '        image: {{ .Values.image.repository }}:{{ .Values.image.tag }}'
assert "non-common concatenation fails" 1 "$conc"

# 4. justified opt-out marker → OK (exit 0)
allow="$tmp/allow"
make_chart "$allow" "svc" '        image: some/third-party:tag  # image-schema:allow needs upstream fix'
assert "opt-out marker passes" 0 "$allow"

# 5. RELATED_IMAGE_* value without common.image → FAIL (exit 1)
rel="$tmp/rel"
make_chart "$rel" "svc" '            - name: RELATED_IMAGE_X
              value: ghcr.io/foo/bar:1.0'
assert "RELATED_IMAGE literal fails" 1 "$rel"

# 6. multiline image block (structured CR field, tag only) → OK (exit 0)  [kcp pattern]
multi="$tmp/multi"
make_chart "$multi" "svc" '  image:
    tag: {{ .Values.kcp.image.tag }}'
assert "multiline image block ignored" 0 "$multi"

# 7. the common chart itself is excluded → OK even with a bad literal
comm="$tmp/comm"
make_chart "$comm" "common" '        image: busybox:1.37'
assert "common chart excluded" 0 "$comm"

echo ""
if [ "$fails" -ne 0 ]; then
  echo "lint-image-schema.test: $fails test(s) FAILED"
  exit 1
fi
echo "lint-image-schema.test: all tests passed"
