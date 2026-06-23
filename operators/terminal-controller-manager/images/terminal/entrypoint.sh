#!/bin/bash

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

set -e

# Validate required environment variables
if [ -z "${KCP_WORKSPACE_URL}" ]; then
    echo "ERROR: KCP_WORKSPACE_URL environment variable is not set" >&2
    exit 1
fi

# Start ttyd on port 8080
# - Runs setup.sh which will read token from first WebSocket message
# - setup.sh creates kubeconfig and starts interactive bash
exec ttyd \
    --port 8080 \
    --writable \
    --max-clients 1 \
    /usr/local/bin/setup.sh
