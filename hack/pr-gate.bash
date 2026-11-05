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

# Waits for the other workflows on a commit and fails if any of them failed.
#
# Environment:
#   GH_TOKEN       token for the gh cli
#   GH_REPO        owner/repo
#   SHA            commit under test (PR head or merge queue commit)
#   SELF_WORKFLOW  name of the gate workflow, excluded

set -euo pipefail

# latest run per workflow on the commit as a stream of {name, bucket}
#
# 1. Take the newest of each job (including concurrency-cancelled ore rerun)
# 2. Filter the pr-gate job out
# 3. Set the bukcet value pending/pass/fail
runs() {
    gh run list --commit "$SHA" --limit 100 --json name,status,conclusion,createdAt,databaseId \
        --jq '
            group_by(.name)
            | map(max_by([.createdAt, .databaseId]))
            | .[]
            | select(.name != env.SELF_WORKFLOW)
            | {
                name,
                bucket: (
                    if .status != "completed" then "pending"
                    elif .conclusion == "success" or .conclusion == "skipped" or .conclusion == "neutral" then "pass"
                    else "fail"
                    end
                )
            }'
}

while true; do
    runs="$(runs)"
    if jq -e 'select(.bucket == "fail")' <<< "$runs" > /dev/null; then
        echo Failed workflows:
        jq -r 'select(.bucket == "fail") | "  \(.name)"' <<< "$runs"
        exit 1
    fi
    if jq -e 'select(.bucket == "pending")' <<< "$runs" > /dev/null; then
        echo Waiting for:
        jq -r 'select(.bucket == "pending") | "  \(.name)"' <<< "$runs"
        sleep 5
        continue
    fi
    break
done

echo All workflows passed
