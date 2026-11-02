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

# Runs the Playwright end-to-end tests (tests/) against the installation in the
# kind cluster: the root-shard smoke suite `task tests:portal-e2e:remote`.
# Every run registers its own organisation, named after TEST_RUN_ID, so the
# suite can run again on the same installation (after an upgrade, say).
#
# Usage: test.sh [test-run-id]    (default: a fresh id; TEST_RUN_ID also works)

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

run_id="${1:-${TEST_RUN_ID:-$(date +%s | tail -c 9)}}"
"$E2E_ROOT/hack/e2e/hosts.sh" ensure "$run_id"
[[ -f "$E2E_ROOT/.secret/kcp/admin.kubeconfig" ]] || die "no .secret/kcp/admin.kubeconfig: run install.sh (task e2e:install) first"

log "portal e2e, test run $run_id"
task -d "$E2E_ROOT" tests:portal-e2e:setup
# A single-node kind cluster provisions workspaces slower than dedicated
# hardware; give the organisation initializer more time.
ORG_READY_TIMEOUT_SECONDS="${ORG_READY_TIMEOUT_SECONDS:-600}" \
  task -d "$E2E_ROOT" tests:portal-e2e:remote TEST_RUN_ID="$run_id"
