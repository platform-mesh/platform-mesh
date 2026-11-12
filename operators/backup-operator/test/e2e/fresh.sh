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

# Backup on the current kind cluster, delete it, install Platform Mesh again and restore there.
set -euo pipefail

DIR=$(cd "$(dirname "$0")" && pwd)
CLUSTER=${CLUSTER:-platform-mesh}
E2E_WORK=${E2E_WORK:-$(mktemp -d)}
export E2E_WORK
run() { E2E_FRESH=$1 go test -count=1 -tags e2e -timeout 90m -v -run TestFreshCluster "$DIR"; }

echo "work dir $E2E_WORK"
run backup
kind delete cluster --name "$CLUSTER"
"$DIR/setup/setup.sh"
run restore
