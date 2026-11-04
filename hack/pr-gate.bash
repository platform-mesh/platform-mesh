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

# Waits for all other checks on a pull request or merge queue commit and
# fails if any of them failed.
#
# Environment:
#   GH_TOKEN         token for the gh cli
#   GH_REPO          owner/repo
#   PR               pull request number (pull_request events)
#   MERGE_GROUP_SHA  queue commit (merge_group events), takes precedence over PR
#   SELF_JOB         name of the gate job, excluded from the checks

set -euo pipefail

# checks on the PR as a stream of {name, bucket}
pr_checks() {
    gh pr checks "$PR" --json bucket,name --jq '.[]'
}

# checks on the merge queue commit as a stream of {name, bucket}, like pr_checks
merge_group_checks() {
    gh api --paginate "repos/$GH_REPO/commits/$MERGE_GROUP_SHA/check-runs?per_page=100" \
        --jq '.check_runs[]
            | {
                name,
                bucket: (
                    if .status != "completed" then "pending"
                    elif .conclusion == "success" then "pass"
                    elif .conclusion == "skipped" or .conclusion == "neutral" then "skipping"
                    else "fail"
                    end
                )
            }'
    # combined status carries the latest status per context
    gh api --paginate "repos/$GH_REPO/commits/$MERGE_GROUP_SHA/status?per_page=100" \
        --jq '.statuses[]
            | {
                name: .context,
                bucket: (
                    if .state == "pending" then "pending"
                    elif .state == "success" then "pass"
                    else "fail"
                    end
                )
            }'
}

# drops this job and renovate/ checks (e.g. stability-days)
filter_checks() {
    jq 'select(.name != env.SELF_JOB) | select(.name | startswith("renovate/") | not)'
}

relevant_checks() {
    if [[ -n "${MERGE_GROUP_SHA:-}" ]]; then
        merge_group_checks
    else
        pr_checks
    fi | filter_checks
}

pending_checks() {
    relevant_checks \
        | jq 'select(.bucket as $val | ["pass", "skipping", "fail"] | index($val) | not)'
}

are_checks_pending() {
    [[ "$(pending_checks | wc -l)" -gt 0 ]]
}

failed_checks() {
    relevant_checks | jq 'select(.bucket == "fail")'
}

are_any_failed() {
    [[ "$(failed_checks | wc -l)" -gt 0 ]]
}

while true; do
    if are_any_failed; then
        echo Some jobs failed, reporting failure
        exit 1
    fi
    if are_checks_pending; then
        echo Some jobs are still running, sleeping
        sleep 1
        continue
    fi
    break
done

echo All jobs finished, checking for failures
if are_any_failed; then
    echo Some jobs failed, reporting failure
    exit 1
fi
echo No jobs failed, success!
