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

# Removes the kind cluster, the registry and the state directory.
#
# Usage: down.sh

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

"$E2E_ROOT/hack/e2e/kind.sh" down || true
"$E2E_ROOT/hack/e2e/registry.sh" down || true
rm -rf "$E2E_STATE"
log "removed $E2E_STATE"
