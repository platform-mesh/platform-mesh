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

# The hostnames the tests and kubectl reach the kind installation under must
# resolve to 127.0.0.1 on this machine. Browsers resolve *.localhost
# themselves, the OS does not everywhere (macOS), so they go to /etc/hosts.
#
# Usage: hosts.sh ensure <test-run-id>    add what is missing (asks for sudo)
#        hosts.sh check  <test-run-id>    list what is missing, exit 1 if any

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cmd="${1:-}"; run_id="${2:?usage: $0 ensure|check <test-run-id>}"
names=("$E2E_BASE_DOMAIN" "kcp.api.$E2E_BASE_DOMAIN" "root.kcp.$E2E_BASE_DOMAIN" "org-root-$run_id.$E2E_BASE_DOMAIN")

resolves() {
  if command -v getent >/dev/null; then getent hosts "$1" >/dev/null
  else dscacheutil -q host -a name "$1" 2>/dev/null | grep -q ip_address; fi
}

missing=()
for n in "${names[@]}"; do resolves "$n" || missing+=("$n"); done
if [[ ${#missing[@]} -eq 0 ]]; then
  log "hosts resolve: ${names[*]}"
  exit 0
fi
case "$cmd" in
  check) printf 'missing in /etc/hosts: 127.0.0.1 %s\n' "${missing[*]}" >&2; exit 1 ;;
  ensure)
    log "adding to /etc/hosts (sudo): ${missing[*]}"
    printf '127.0.0.1 %s\n' "${missing[@]}" | sudo tee -a /etc/hosts >/dev/null
    ;;
  *) echo "usage: $0 ensure|check <test-run-id>" >&2; exit 1 ;;
esac
