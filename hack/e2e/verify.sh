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

# Checks the snapshot in the local registry the way a consumer sees it
# (hack/release/verify-graph.sh, the same checks the release workflow runs
# against ghcr.io): the graph resolves, every signature verifies, every chart
# pulls and templates.
#
# Usage: verify.sh

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

export OCM_CONFIG="$E2E_STATE/ocmconfig"
[[ -f "$OCM_CONFIG" ]] || die "no $OCM_CONFIG: run ocm.sh (task e2e:ocm) first"

exec "$E2E_ROOT/hack/release/verify-graph.sh" \
  --version "$(state version 'task e2e:ocm')" --image-version "$(image_version)" \
  --ocm-repo "$(ocm_repo)" --chart-repo "$(chart_repo)" --signature "$E2E_SIGNATURE"
